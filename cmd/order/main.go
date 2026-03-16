package main

import (
	"context"
	"log"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/mantis-exchange/mantis-order/internal/client"
	"github.com/mantis-exchange/mantis-order/internal/config"
	"github.com/mantis-exchange/mantis-order/internal/consumer"
	ordergrpc "github.com/mantis-exchange/mantis-order/internal/grpc"
	"github.com/mantis-exchange/mantis-order/internal/model"
	"github.com/mantis-exchange/mantis-order/internal/mq"
	pb "github.com/mantis-exchange/mantis-order/pkg/proto/mantis"
)

func main() {
	cfg := config.Load()

	// Connect to PostgreSQL
	pool, err := pgxpool.New(context.Background(), cfg.DBURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	repo := model.NewOrderRepo(pool)

	// Connect to matching engine
	engineClient, err := ordergrpc.NewEngineClient(cfg.MatchingEngineAddr)
	if err != nil {
		log.Fatalf("failed to connect to matching engine: %v", err)
	}
	defer engineClient.Close()

	// Create account client
	accountClient := client.NewAccountClient(cfg.AccountServiceAddr)

	// Create Kafka producer
	producer := mq.NewProducer(cfg.KafkaBrokers)
	defer producer.Close()

	// Start trade event consumer
	tradeConsumer := consumer.NewTradeConsumer(repo, accountClient, cfg.KafkaBrokers)
	go tradeConsumer.Start()

	// Create gRPC server
	orderServer := ordergrpc.NewOrderServer(repo, engineClient, accountClient, producer)

	lis, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := grpc.NewServer()
	pb.RegisterOrderServiceServer(srv, orderServer)

	log.Printf("mantis-order gRPC server starting on :%s", cfg.Port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("gRPC server error: %v", err)
	}
}
