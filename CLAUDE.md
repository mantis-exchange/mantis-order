# mantis-order

Mantis Exchange order lifecycle management service.

## Architecture

- `internal/model/order.go` — Order model + PostgreSQL CRUD via pgx
- `internal/service/order.go` — Business logic: PlaceOrder, CancelOrder, GetOrder, ListOrders
- `internal/consumer/trade.go` — Kafka consumer placeholder for trade event processing
- `internal/config/config.go` — Config from environment variables
- `cmd/order/main.go` — Entry point, gRPC server on port 50052

## Flow

1. Gateway receives order request → forwards to order service
2. Order service validates → freezes balance → persists to DB → submits to matching engine
3. Matching engine produces trade events → Kafka
4. Trade consumer updates order status and settles balances

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `50052` | gRPC server port |
| `DB_URL` | `postgres://mantis:mantis@localhost:5432/mantis_order?sslmode=disable` | PostgreSQL connection |
| `MATCHING_ENGINE_ADDR` | `localhost:50051` | Matching engine gRPC address |
| `KAFKA_BROKERS` | `localhost:9092` | Kafka broker addresses |

## Build & Run

```bash
go build -o mantis-order ./cmd/order
./mantis-order
```
