package grpc

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mantis-exchange/mantis-order/internal/client"
	"github.com/mantis-exchange/mantis-order/internal/model"
	"github.com/mantis-exchange/mantis-order/internal/mq"
	pb "github.com/mantis-exchange/mantis-order/pkg/proto/mantis"
)

type OrderServer struct {
	pb.UnimplementedOrderServiceServer
	repo     *model.OrderRepo
	engine   *EngineClient
	account  *client.AccountClient
	producer *mq.Producer
	risk     *client.RiskClient
}

func NewOrderServer(
	repo *model.OrderRepo,
	engine *EngineClient,
	account *client.AccountClient,
	producer *mq.Producer,
	risk *client.RiskClient,
) *OrderServer {
	return &OrderServer{
		repo:     repo,
		engine:   engine,
		account:  account,
		producer: producer,
		risk:     risk,
	}
}

func (s *OrderServer) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.PlaceOrderResponse, error) {
	// Validate
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.Symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}
	if req.Quantity == "" {
		return nil, status.Error(codes.InvalidArgument, "quantity is required")
	}
	if req.OrderType == pb.OrderType_ORDER_TYPE_LIMIT && req.Price == "" {
		return nil, status.Error(codes.InvalidArgument, "price is required for limit orders")
	}

	userID, err := uuid.Parse(req.UserId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user_id")
	}

	// Determine freeze asset and amount
	parts := strings.SplitN(req.Symbol, "-", 2)
	if len(parts) != 2 {
		return nil, status.Error(codes.InvalidArgument, "invalid symbol format, expected BASE-QUOTE")
	}
	baseAsset, quoteAsset := parts[0], parts[1]

	var freezeAsset, freezeAmount string
	side := sideToModel(req.Side)
	isMarketOrder := req.OrderType == pb.OrderType_ORDER_TYPE_MARKET

	if !isMarketOrder {
		// Limit orders: freeze the exact amount
		if side == model.SideBuy {
			freezeAsset = quoteAsset
			freezeAmount, err = multiplyDecimals(req.Price, req.Quantity)
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "invalid price/quantity: %v", err)
			}
		} else {
			freezeAsset = baseAsset
			freezeAmount = req.Quantity
		}
	} else {
		// Market orders: freeze sell quantity; for market buy, skip freeze
		// (settlement handles deduction at actual execution price)
		if side == model.SideSell {
			freezeAsset = baseAsset
			freezeAmount = req.Quantity
		}
	}

	// Risk check
	if s.risk != nil {
		price, _ := strconv.ParseFloat(req.Price, 64)
		qty, _ := strconv.ParseFloat(req.Quantity, 64)
		if price > 0 { // Skip risk price check for market orders
			result, err := s.risk.CheckOrder(ctx, client.OrderCheckRequest{
				Symbol:   req.Symbol,
				Side:     strings.ToLower(string(side)),
				Price:    price,
				Quantity: qty,
			})
			if err == nil && !result.Allowed {
				return nil, status.Errorf(codes.FailedPrecondition, "risk check failed: %s", result.Reason)
			}
		}
	}

	// Step 1: Freeze balance (skip for market buy — no known price)
	if freezeAmount != "" {
		if err := s.account.FreezeBalance(ctx, req.UserId, freezeAsset, freezeAmount); err != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to freeze balance: %v", err)
		}
	}

	// Step 2: Persist order to DB
	orderID := uuid.New()
	now := time.Now()
	order := &model.Order{
		ID:             orderID,
		UserID:         userID,
		Symbol:         req.Symbol,
		Side:           side,
		Type:           orderTypeToModel(req.OrderType),
		Price:          req.Price,
		Quantity:       req.Quantity,
		FilledQuantity: "0",
		Status:         model.StatusNew,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.repo.Create(ctx, order); err != nil {
		// Rollback: unfreeze
		_ = s.account.UnfreezeBalance(ctx, req.UserId, freezeAsset, freezeAmount)
		return nil, status.Errorf(codes.Internal, "failed to create order: %v", err)
	}

	// Step 3: Submit to matching engine with our order_id
	engineResp, err := s.engine.SubmitOrder(ctx, &pb.SubmitOrderRequest{
		Symbol:      req.Symbol,
		Side:        req.Side,
		OrderType:   req.OrderType,
		TimeInForce: req.TimeInForce,
		Price:       req.Price,
		Quantity:    req.Quantity,
		OrderId:     orderID.String(),
	})
	if err != nil {
		if freezeAmount != "" {
			_ = s.account.UnfreezeBalance(ctx, req.UserId, freezeAsset, freezeAmount)
		}
		_ = s.repo.UpdateStatus(ctx, orderID, model.StatusCancelled, "0")
		return nil, status.Errorf(codes.Internal, "matching engine error: %v", err)
	}

	// Step 4: Publish trades to Kafka
	// Note: Order status updates are handled by the trade consumer to avoid
	// double-counting filled quantities (consumer is the single source of truth).
	if len(engineResp.GetTrades()) > 0 {
		tradeMessages := make([]mq.TradeMessage, 0, len(engineResp.GetTrades()))
		for _, t := range engineResp.GetTrades() {
			tradeMessages = append(tradeMessages, mq.TradeMessage{
				ID:           t.GetId(),
				Symbol:       t.GetSymbol(),
				Price:        t.GetPrice(),
				Quantity:     t.GetQuantity(),
				MakerOrderID: t.GetMakerOrderId(),
				TakerOrderID: t.GetTakerOrderId(),
				MakerSide:    makerSideString(t.GetMakerSide()),
				CreatedAt:    t.GetCreatedAt(),
			})
		}
		if err := s.producer.PublishTrades(ctx, tradeMessages); err != nil {
			log.Printf("failed to publish trades to Kafka: %v", err)
		}
	}

	// Build response
	return &pb.PlaceOrderResponse{
		Order:  engineResp.GetOrder(),
		Trades: engineResp.GetTrades(),
	}, nil
}

func (s *OrderServer) CancelOrder(ctx context.Context, req *pb.CancelOrderByUserRequest) (*pb.CancelOrderByUserResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.OrderId == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id is required")
	}

	orderID, err := uuid.Parse(req.OrderId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid order_id")
	}

	// Get order from DB to verify ownership
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "order not found")
	}

	if order.UserID.String() != req.UserId {
		return nil, status.Error(codes.PermissionDenied, "order does not belong to user")
	}

	if order.Status == model.StatusFilled || order.Status == model.StatusCancelled {
		return nil, status.Errorf(codes.FailedPrecondition, "order cannot be cancelled (status: %s)", order.Status)
	}

	// Cancel on engine
	symbol := req.Symbol
	if symbol == "" {
		symbol = order.Symbol
	}
	engineResp, err := s.engine.CancelOrder(ctx, &pb.CancelOrderRequest{
		Symbol:  symbol,
		OrderId: req.OrderId,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "matching engine error: %v", err)
	}

	// Update order status in DB
	if err := s.repo.UpdateStatus(ctx, orderID, model.StatusCancelled, order.FilledQuantity); err != nil {
		log.Printf("failed to update cancelled order %s: %v", orderID, err)
	}

	// Unfreeze remaining balance
	parts := strings.SplitN(order.Symbol, "-", 2)
	if len(parts) == 2 {
		baseAsset, quoteAsset := parts[0], parts[1]
		remaining, _ := subtractDecimals(order.Quantity, order.FilledQuantity)

		if isPositive(remaining) {
			if order.Side == model.SideBuy {
				unfreezeAmount, _ := multiplyDecimals(order.Price, remaining)
				if isPositive(unfreezeAmount) {
					_ = s.account.UnfreezeBalance(ctx, req.UserId, quoteAsset, unfreezeAmount)
				}
			} else {
				_ = s.account.UnfreezeBalance(ctx, req.UserId, baseAsset, remaining)
			}
		}
	}

	return &pb.CancelOrderByUserResponse{
		Success: engineResp.GetSuccess(),
		Order:   engineResp.GetOrder(),
	}, nil
}

func (s *OrderServer) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.GetOrderResponse, error) {
	if req.OrderId == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id is required")
	}

	orderID, err := uuid.Parse(req.OrderId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid order_id")
	}

	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "order not found")
	}

	return &pb.GetOrderResponse{
		Order: modelToProto(order),
	}, nil
}

func (s *OrderServer) ListOrders(ctx context.Context, req *pb.ListOrdersRequest) (*pb.ListOrdersResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	userID, err := uuid.Parse(req.UserId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid user_id")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
	}

	orders, err := s.repo.ListByUser(ctx, userID, limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list orders: %v", err)
	}

	pbOrders := make([]*pb.Order, 0, len(orders))
	for _, o := range orders {
		pbOrders = append(pbOrders, modelToProto(&o))
	}

	return &pb.ListOrdersResponse{
		Orders: pbOrders,
		Total:  int32(len(pbOrders)),
	}, nil
}

// Helper functions

func sideToModel(s pb.Side) model.OrderSide {
	switch s {
	case pb.Side_SIDE_BUY:
		return model.SideBuy
	case pb.Side_SIDE_SELL:
		return model.SideSell
	default:
		return model.SideBuy
	}
}

func sideToProto(s model.OrderSide) pb.Side {
	switch s {
	case model.SideBuy:
		return pb.Side_SIDE_BUY
	case model.SideSell:
		return pb.Side_SIDE_SELL
	default:
		return pb.Side_SIDE_UNSPECIFIED
	}
}

func orderTypeToModel(t pb.OrderType) model.OrderType {
	switch t {
	case pb.OrderType_ORDER_TYPE_LIMIT:
		return model.TypeLimit
	case pb.OrderType_ORDER_TYPE_MARKET:
		return model.TypeMarket
	default:
		return model.TypeLimit
	}
}

func orderTypeToProto(t model.OrderType) pb.OrderType {
	switch t {
	case model.TypeLimit:
		return pb.OrderType_ORDER_TYPE_LIMIT
	case model.TypeMarket:
		return pb.OrderType_ORDER_TYPE_MARKET
	default:
		return pb.OrderType_ORDER_TYPE_UNSPECIFIED
	}
}

func orderStatusFromProto(s pb.OrderStatus) model.OrderStatus {
	switch s {
	case pb.OrderStatus_ORDER_STATUS_NEW:
		return model.StatusNew
	case pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED:
		return model.StatusPartiallyFilled
	case pb.OrderStatus_ORDER_STATUS_FILLED:
		return model.StatusFilled
	case pb.OrderStatus_ORDER_STATUS_CANCELLED:
		return model.StatusCancelled
	default:
		return model.StatusNew
	}
}

func orderStatusToProto(s model.OrderStatus) pb.OrderStatus {
	switch s {
	case model.StatusNew:
		return pb.OrderStatus_ORDER_STATUS_NEW
	case model.StatusPartiallyFilled:
		return pb.OrderStatus_ORDER_STATUS_PARTIALLY_FILLED
	case model.StatusFilled:
		return pb.OrderStatus_ORDER_STATUS_FILLED
	case model.StatusCancelled:
		return pb.OrderStatus_ORDER_STATUS_CANCELLED
	default:
		return pb.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func makerSideString(s pb.Side) string {
	switch s {
	case pb.Side_SIDE_BUY:
		return "BUY"
	case pb.Side_SIDE_SELL:
		return "SELL"
	default:
		return "UNKNOWN"
	}
}

func modelToProto(o *model.Order) *pb.Order {
	return &pb.Order{
		Id:             o.ID.String(),
		Symbol:         o.Symbol,
		Side:           sideToProto(o.Side),
		OrderType:      orderTypeToProto(o.Type),
		Price:          o.Price,
		Quantity:       o.Quantity,
		FilledQuantity: o.FilledQuantity,
		Status:         orderStatusToProto(o.Status),
		CreatedAt:      o.CreatedAt.UnixMilli(),
	}
}

func multiplyDecimals(a, b string) (string, error) {
	fa, _, err := new(big.Float).SetPrec(128).Parse(a, 10)
	if err != nil {
		return "", fmt.Errorf("invalid decimal: %s", a)
	}
	fb, _, err := new(big.Float).SetPrec(128).Parse(b, 10)
	if err != nil {
		return "", fmt.Errorf("invalid decimal: %s", b)
	}
	result := new(big.Float).SetPrec(128).Mul(fa, fb)
	return trimZeros(result.Text('f', 18)), nil
}

func isPositive(s string) bool {
	f, _, _ := new(big.Float).SetPrec(128).Parse(s, 10)
	return f != nil && f.Sign() > 0
}

func subtractDecimals(a, b string) (string, error) {
	fa, _, err := new(big.Float).SetPrec(128).Parse(a, 10)
	if err != nil {
		return "", fmt.Errorf("invalid decimal: %s", a)
	}
	fb, _, err := new(big.Float).SetPrec(128).Parse(b, 10)
	if err != nil {
		return "", fmt.Errorf("invalid decimal: %s", b)
	}
	result := new(big.Float).SetPrec(128).Sub(fa, fb)
	return trimZeros(result.Text('f', 18)), nil
}

// trimZeros removes unnecessary trailing zeros from a decimal string.
// "5000.000000000000000000" -> "5000", "0.100000000000000000" -> "0.1"
func trimZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}
