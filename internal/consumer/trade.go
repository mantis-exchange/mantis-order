package consumer

import (
	"log"

	"github.com/mantis-exchange/mantis-order/internal/model"
)

// TradeConsumer listens for trade events from Kafka and updates order status.
type TradeConsumer struct {
	repo    *model.OrderRepo
	brokers string
}

func NewTradeConsumer(repo *model.OrderRepo, brokers string) *TradeConsumer {
	return &TradeConsumer{repo: repo, brokers: brokers}
}

// Start begins consuming trade events. Placeholder — will integrate with Kafka.
func (c *TradeConsumer) Start() {
	log.Printf("trade consumer started (brokers: %s) — placeholder, no Kafka connection yet", c.brokers)

	// TODO: Connect to Kafka, consume trade events, and for each trade:
	// 1. Look up maker and taker orders
	// 2. Update filled_quantity
	// 3. Update status (PARTIALLY_FILLED or FILLED)
	// 4. Settle balances (credit buyer, debit seller)
}
