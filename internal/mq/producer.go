package mq

import (
	"context"
	"encoding/json"
	"log"

	"github.com/segmentio/kafka-go"
)

const TradeTopic = "mantis.trades"

type TradeMessage struct {
	ID           string `json:"id"`
	Symbol       string `json:"symbol"`
	Price        string `json:"price"`
	Quantity     string `json:"quantity"`
	MakerOrderID string `json:"maker_order_id"`
	TakerOrderID string `json:"taker_order_id"`
	MakerSide    string `json:"maker_side"`
	CreatedAt    int64  `json:"created_at"`
}

type Producer struct {
	writer *kafka.Writer
}

func NewProducer(brokers string) *Producer {
	return &Producer{
		writer: &kafka.Writer{
			Addr:     kafka.TCP(brokers),
			Topic:    TradeTopic,
			Balancer: &kafka.Hash{},
		},
	}
}

func (p *Producer) PublishTrades(ctx context.Context, trades []TradeMessage) error {
	if len(trades) == 0 {
		return nil
	}

	messages := make([]kafka.Message, 0, len(trades))
	for _, t := range trades {
		value, err := json.Marshal(t)
		if err != nil {
			log.Printf("failed to marshal trade %s: %v", t.ID, err)
			continue
		}
		messages = append(messages, kafka.Message{
			Key:   []byte(t.Symbol),
			Value: value,
		})
	}

	return p.writer.WriteMessages(ctx, messages...)
}

func (p *Producer) Close() error {
	return p.writer.Close()
}
