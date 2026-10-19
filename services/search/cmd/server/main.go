package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	searchv1 "github.com/roastco/ecommerce/gen/search/v1"
	"github.com/roastco/ecommerce/pkg/config"
	"github.com/roastco/ecommerce/pkg/logging"
	"github.com/roastco/ecommerce/pkg/tracing"
	grpcserver "github.com/roastco/ecommerce/services/search/internal/grpc"
	"github.com/roastco/ecommerce/services/search/internal/indexer"
	"github.com/roastco/ecommerce/services/search/internal/repository"
	"github.com/roastco/ecommerce/services/search/internal/service"
)

func main() {
	log := logging.New("search-service", config.String("LOG_LEVEL", "info"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Init(ctx, "search-service", config.String("OTLP_ENDPOINT", "jaeger:4317"))
	if err != nil {
		log.Warn("tracing init failed", "error", err)
	} else {
		defer shutdownTracing(context.Background())
	}

	esURL := config.String("ELASTICSEARCH_URL", "http://elasticsearch:9200")
	repo := repository.New(esURL)
	_ = repo.EnsureIndex(ctx)

	brokers := config.String("KAFKA_BROKERS", "")
	if brokers != "" {
		idx := indexer.New(repo, strings.Split(brokers, ","), log)
		idx.Start(ctx)
	}

	svc := service.New(repo)

	srv := grpc.NewServer()
	searchv1.RegisterSearchServiceServer(srv, grpcserver.New(svc, log))
	reflection.Register(srv)

	grpcAddr := config.String("GRPC_ADDR", ":9003")
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Error("listen failed", "addr", grpcAddr, "error", err)
		os.Exit(1)
	}

	go func() {
		<-ctx.Done()
		log.Info("shutting down gRPC server")
		srv.GracefulStop()
	}()

	log.Info("search-service starting", "addr", grpcAddr)
	if err := srv.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		log.Error("serve failed", "error", err)
		os.Exit(1)
	}
}
