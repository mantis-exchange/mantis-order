package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

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

	repo := model.NewOrderRepo(pool)

	// Connect to matching engine
	engineClient, err := ordergrpc.NewEngineClient(cfg.MatchingEngineAddr)
	if err != nil {
		log.Fatalf("failed to connect to matching engine: %v", err)
	}

	// Create account client
	accountClient := client.NewAccountClient(cfg.AccountServiceAddr)

	// Create risk client
	riskClient := client.NewRiskClient(cfg.RiskServiceAddr)

	// Create Kafka producer
	producer := mq.NewProducer(cfg.KafkaBrokers)

	// Start trade event consumer
	tradeConsumer := consumer.NewTradeConsumer(repo, accountClient, cfg.KafkaBrokers, cfg.MakerFeePct, cfg.TakerFeePct)
	go tradeConsumer.Start()

	// Create gRPC server
	orderServer := ordergrpc.NewOrderServer(repo, engineClient, accountClient, producer, riskClient)

	lis, err := net.Listen("tcp", ":"+cfg.Port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv := grpc.NewServer()
	pb.RegisterOrderServiceServer(srv, orderServer)

	log.Printf("mantis-order gRPC server starting on :%s", cfg.Port)

	go func() {
		if err := srv.Serve(lis); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down order service...")

	srv.GracefulStop()
	engineClient.Close()
	producer.Close()
	pool.Close()
	log.Println("order service stopped")
}
