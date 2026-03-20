package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/mantis-exchange/mantis-order/internal/model"
)

type OrderService struct {
	repo *model.OrderRepo
}

func NewOrderService(repo *model.OrderRepo) *OrderService {
	return &OrderService{repo: repo}
}

type PlaceOrderRequest struct {
	UserID      uuid.UUID
	Symbol      string
	Side        model.OrderSide
	Type        model.OrderType
	Price       string
	Quantity    string
	TimeInForce string
}

func (s *OrderService) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*model.Order, error) {
	// Validate
	if req.Symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if req.Quantity == "" {
		return nil, fmt.Errorf("quantity is required")
	}
	if req.Type == model.TypeLimit && req.Price == "" {
		return nil, fmt.Errorf("price is required for limit orders")
	}

	now := time.Now()
	order := &model.Order{
		ID:             uuid.New(),
		UserID:         req.UserID,
		Symbol:         req.Symbol,
		Side:           req.Side,
		Type:           req.Type,
		Price:          req.Price,
		Quantity:       req.Quantity,
		FilledQuantity: "0",
		Status:         model.StatusNew,
		StopPrice:      "",
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// TODO: Freeze balance via account service

	// Persist order
	if err := s.repo.Create(ctx, order); err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	// TODO: Submit to matching engine via gRPC
	log.Printf("order %s submitted: %s %s %s @ %s", order.ID, order.Side, order.Quantity, order.Symbol, order.Price)

	return order, nil
}

func (s *OrderService) CancelOrder(ctx context.Context, orderID uuid.UUID) (*model.Order, error) {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}

	if order.Status == model.StatusFilled || order.Status == model.StatusCancelled {
		return nil, fmt.Errorf("order %s cannot be cancelled (status: %s)", orderID, order.Status)
	}

	// TODO: Send cancel to matching engine via gRPC

	if err := s.repo.UpdateStatus(ctx, orderID, model.StatusCancelled, order.FilledQuantity); err != nil {
		return nil, fmt.Errorf("failed to update order status: %w", err)
	}

	// TODO: Unfreeze remaining balance

	order.Status = model.StatusCancelled
	return order, nil
}

func (s *OrderService) GetOrder(ctx context.Context, orderID uuid.UUID) (*model.Order, error) {
	return s.repo.GetByID(ctx, orderID)
}

func (s *OrderService) ListOrders(ctx context.Context, userID uuid.UUID, limit int) ([]model.Order, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.repo.ListByUser(ctx, userID, limit)
}
