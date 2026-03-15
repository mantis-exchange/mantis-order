package main

import (
	"context"
	"log"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/mantis-exchange/mantis-order/internal/config"
	"github.com/mantis-exchange/mantis-order/internal/consumer"
	"github.com/mantis-exchange/mantis-order/internal/model"
	"github.com/mantis-exchange/mantis-order/internal/service"
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
	orderService := service.NewOrderService(repo)
	_ = orderService // Will be used by gRPC server

	// Start trade event consumer
	tradeConsumer := consumer.NewTradeConsumer(repo, cfg.KafkaBrokers)
	go tradeConsumer.Start()

	// Start gRPC server
	lis, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := grpc.NewServer()
	// TODO: Register order service gRPC handlers

	log.Printf("mantis-order gRPC server starting on :%s", cfg.Port)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("gRPC server error: %v", err)
	}
}
