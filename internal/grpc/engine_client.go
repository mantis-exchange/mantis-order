package grpc

import (
	"context"
	"fmt"
	"time"

	pb "github.com/mantis-exchange/mantis-order/pkg/proto/mantis"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type EngineClient struct {
	conn   *grpc.ClientConn
	engine pb.MatchingEngineClient
}

func NewEngineClient(addr string) (*EngineClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to matching engine at %s: %w", addr, err)
	}

	return &EngineClient{
		conn:   conn,
		engine: pb.NewMatchingEngineClient(conn),
	}, nil
}

func (c *EngineClient) Close() error {
	return c.conn.Close()
}

func (c *EngineClient) SubmitOrder(ctx context.Context, req *pb.SubmitOrderRequest) (*pb.SubmitOrderResponse, error) {
	return c.engine.SubmitOrder(ctx, req)
}

func (c *EngineClient) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	return c.engine.CancelOrder(ctx, req)
}
