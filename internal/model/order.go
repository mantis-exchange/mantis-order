package model

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OrderSide string
type OrderType string
type OrderStatus string

const (
	SideBuy  OrderSide = "BUY"
	SideSell OrderSide = "SELL"

	TypeLimit  OrderType = "LIMIT"
	TypeMarket OrderType = "MARKET"

	StatusNew             OrderStatus = "NEW"
	StatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	StatusFilled          OrderStatus = "FILLED"
	StatusCancelled       OrderStatus = "CANCELLED"
)

type Order struct {
	ID             uuid.UUID   `json:"id"`
	UserID         uuid.UUID   `json:"user_id"`
	Symbol         string      `json:"symbol"`
	Side           OrderSide   `json:"side"`
	Type           OrderType   `json:"type"`
	Price          string      `json:"price"`
	Quantity       string      `json:"quantity"`
	FilledQuantity string      `json:"filled_quantity"`
	Status         OrderStatus `json:"status"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

type OrderRepo struct {
	pool *pgxpool.Pool
}

func NewOrderRepo(pool *pgxpool.Pool) *OrderRepo {
	return &OrderRepo{pool: pool}
}

func (r *OrderRepo) Create(ctx context.Context, o *Order) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO orders (id, user_id, symbol, side, type, price, quantity, filled_quantity, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		o.ID, o.UserID, o.Symbol, o.Side, o.Type, o.Price, o.Quantity, o.FilledQuantity, o.Status, o.CreatedAt, o.UpdatedAt,
	)
	return err
}

func (r *OrderRepo) GetByID(ctx context.Context, id uuid.UUID) (*Order, error) {
	o := &Order{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, symbol, side, type, price, quantity, filled_quantity, status, created_at, updated_at
		 FROM orders WHERE id = $1`, id,
	).Scan(&o.ID, &o.UserID, &o.Symbol, &o.Side, &o.Type, &o.Price, &o.Quantity, &o.FilledQuantity, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (r *OrderRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status OrderStatus, filledQty string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE orders SET status = $1, filled_quantity = $2, updated_at = $3 WHERE id = $4`,
		status, filledQty, time.Now(), id,
	)
	return err
}

// IsTradeSettled checks if a trade has already been settled.
func (r *OrderRepo) IsTradeSettled(ctx context.Context, tradeID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM settled_trades WHERE trade_id = $1)`, tradeID,
	).Scan(&exists)
	return exists, err
}

// MarkTradeSettled records a trade as settled.
func (r *OrderRepo) MarkTradeSettled(ctx context.Context, tradeID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO settled_trades (trade_id) VALUES ($1) ON CONFLICT DO NOTHING`, tradeID,
	)
	return err
}

func (r *OrderRepo) ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]Order, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, symbol, side, type, price, quantity, filled_quantity, status, created_at, updated_at
		 FROM orders WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`, userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.Symbol, &o.Side, &o.Type, &o.Price, &o.Quantity, &o.FilledQuantity, &o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, nil
}
