package consumer

import (
	"context"
	"encoding/json"
	"log"
	"math/big"
	"strings"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"

	"github.com/mantis-exchange/mantis-order/internal/client"
	"github.com/mantis-exchange/mantis-order/internal/model"
	"github.com/mantis-exchange/mantis-order/internal/mq"
)

type TradeConsumer struct {
	repo     *model.OrderRepo
	account  *client.AccountClient
	brokers  string
	makerFee string
	takerFee string
}

func NewTradeConsumer(repo *model.OrderRepo, account *client.AccountClient, brokers, makerFee, takerFee string) *TradeConsumer {
	return &TradeConsumer{repo: repo, account: account, brokers: brokers, makerFee: makerFee, takerFee: takerFee}
}

func (c *TradeConsumer) Start() {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     strings.Split(c.brokers, ","),
		Topic:       mq.TradeTopic,
		GroupID:     "mantis-order-settlement",
		MinBytes:    1,
		MaxBytes:    10e6,
		StartOffset: kafka.FirstOffset,
	})
	defer reader.Close()

	log.Printf("order settlement consumer started (brokers: %s, topic: %s)", c.brokers, mq.TradeTopic)

	for {
		msg, err := reader.ReadMessage(context.Background())
		if err != nil {
			log.Printf("consumer read error: %v", err)
			continue
		}

		var trade mq.TradeMessage
		if err := json.Unmarshal(msg.Value, &trade); err != nil {
			log.Printf("failed to unmarshal trade: %v", err)
			continue
		}

		c.processTrade(context.Background(), trade)
	}
}

func (c *TradeConsumer) processTrade(ctx context.Context, trade mq.TradeMessage) {
	// Idempotency: skip if already settled
	settled, err := c.repo.IsTradeSettled(ctx, trade.ID)
	if err != nil {
		log.Printf("failed to check trade settlement status %s: %v", trade.ID, err)
		return
	}
	if settled {
		log.Printf("trade %s already settled, skipping", trade.ID)
		return
	}

	makerOrderID, err := uuid.Parse(trade.MakerOrderID)
	if err != nil {
		log.Printf("invalid maker_order_id: %s", trade.MakerOrderID)
		return
	}

	takerOrderID, err := uuid.Parse(trade.TakerOrderID)
	if err != nil {
		log.Printf("invalid taker_order_id: %s", trade.TakerOrderID)
		return
	}

	// Get both orders
	makerOrder, err := c.repo.GetByID(ctx, makerOrderID)
	if err != nil {
		log.Printf("maker order %s not found: %v", makerOrderID, err)
		return
	}

	takerOrder, err := c.repo.GetByID(ctx, takerOrderID)
	if err != nil {
		log.Printf("taker order %s not found: %v", takerOrderID, err)
		return
	}

	// Parse symbol to get base/quote assets
	parts := strings.SplitN(trade.Symbol, "-", 2)
	if len(parts) != 2 {
		log.Printf("invalid symbol: %s", trade.Symbol)
		return
	}
	baseAsset, quoteAsset := parts[0], parts[1]

	// Determine buyer and seller
	var buyerOrder, sellerOrder *model.Order
	if trade.MakerSide == "BUY" {
		buyerOrder = makerOrder
		sellerOrder = takerOrder
	} else {
		buyerOrder = takerOrder
		sellerOrder = makerOrder
	}

	// Calculate quote amount = price * quantity
	quoteAmount := multiplyStrings(trade.Price, trade.Quantity)

	// Determine fees - maker gets lower fee, taker gets higher
	var buyerFeeRate, sellerFeeRate string
	if trade.MakerSide == "BUY" {
		buyerFeeRate = c.makerFee
		sellerFeeRate = c.takerFee
	} else {
		buyerFeeRate = c.takerFee
		sellerFeeRate = c.makerFee
	}

	// Buyer receives base asset minus fee
	buyerBaseFee := multiplyStrings(trade.Quantity, buyerFeeRate)
	buyerBaseReceive := subtractStrings(trade.Quantity, buyerBaseFee)

	// Seller receives quote asset minus fee
	sellerQuoteFee := multiplyStrings(quoteAmount, sellerFeeRate)
	sellerQuoteReceive := subtractStrings(quoteAmount, sellerQuoteFee)

	// Settle buyer: DeductFrozen(quote, price*qty) + Credit(base, qty - fee)
	if err := c.account.DeductFrozenBalance(ctx, buyerOrder.UserID.String(), quoteAsset, quoteAmount); err != nil {
		log.Printf("failed to deduct frozen for buyer %s: %v", buyerOrder.UserID, err)
	}
	if err := c.account.CreditBalance(ctx, buyerOrder.UserID.String(), baseAsset, buyerBaseReceive); err != nil {
		log.Printf("failed to credit base for buyer %s: %v", buyerOrder.UserID, err)
	}

	// Settle seller: DeductFrozen(base, qty) + Credit(quote, price*qty - fee)
	if err := c.account.DeductFrozenBalance(ctx, sellerOrder.UserID.String(), baseAsset, trade.Quantity); err != nil {
		log.Printf("failed to deduct frozen for seller %s: %v", sellerOrder.UserID, err)
	}
	if err := c.account.CreditBalance(ctx, sellerOrder.UserID.String(), quoteAsset, sellerQuoteReceive); err != nil {
		log.Printf("failed to credit quote for seller %s: %v", sellerOrder.UserID, err)
	}

	if buyerBaseFee != "0" || sellerQuoteFee != "0" {
		log.Printf("fees collected: buyer=%s %s, seller=%s %s", buyerBaseFee, baseAsset, sellerQuoteFee, quoteAsset)
	}

	// Update order filled quantities and statuses
	c.updateOrderAfterTrade(ctx, makerOrderID, trade.Quantity)
	c.updateOrderAfterTrade(ctx, takerOrderID, trade.Quantity)

	// Mark trade as settled for idempotency
	if err := c.repo.MarkTradeSettled(ctx, trade.ID); err != nil {
		log.Printf("failed to mark trade %s as settled: %v", trade.ID, err)
	}

	log.Printf("settled trade %s: %s %s @ %s", trade.ID, trade.Quantity, trade.Symbol, trade.Price)
}

func (c *TradeConsumer) updateOrderAfterTrade(ctx context.Context, orderID uuid.UUID, tradeQty string) {
	order, err := c.repo.GetByID(ctx, orderID)
	if err != nil {
		return
	}

	newFilled := addStrings(order.FilledQuantity, tradeQty)
	var newStatus model.OrderStatus
	if compareStrings(newFilled, order.Quantity) >= 0 {
		newStatus = model.StatusFilled
	} else {
		newStatus = model.StatusPartiallyFilled
	}

	_ = c.repo.UpdateStatus(ctx, orderID, newStatus, newFilled)
}

func multiplyStrings(a, b string) string {
	fa, _, _ := new(big.Float).SetPrec(128).Parse(a, 10)
	fb, _, _ := new(big.Float).SetPrec(128).Parse(b, 10)
	if fa == nil || fb == nil {
		return "0"
	}
	return trimZeros(new(big.Float).SetPrec(128).Mul(fa, fb).Text('f', 18))
}

func subtractStrings(a, b string) string {
	fa, _, _ := new(big.Float).SetPrec(128).Parse(a, 10)
	fb, _, _ := new(big.Float).SetPrec(128).Parse(b, 10)
	if fa == nil || fb == nil {
		return "0"
	}
	result := new(big.Float).SetPrec(128).Sub(fa, fb)
	if result.Sign() < 0 {
		return "0"
	}
	return trimZeros(result.Text('f', 18))
}

func addStrings(a, b string) string {
	fa, _, _ := new(big.Float).SetPrec(128).Parse(a, 10)
	fb, _, _ := new(big.Float).SetPrec(128).Parse(b, 10)
	if fa == nil || fb == nil {
		return "0"
	}
	return trimZeros(new(big.Float).SetPrec(128).Add(fa, fb).Text('f', 18))
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

func compareStrings(a, b string) int {
	fa, _, _ := new(big.Float).SetPrec(128).Parse(a, 10)
	fb, _, _ := new(big.Float).SetPrec(128).Parse(b, 10)
	if fa == nil || fb == nil {
		return 0
	}
	return fa.Cmp(fb)
}
