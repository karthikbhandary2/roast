package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	orderv1 "github.com/roastco/ecommerce/gen/order/v1"
	productv1 "github.com/roastco/ecommerce/gen/product/v1"
	userv1 "github.com/roastco/ecommerce/gen/user/v1"
	"github.com/roastco/ecommerce/pkg/config"
	"github.com/roastco/ecommerce/pkg/kafkaclient"
	"github.com/roastco/ecommerce/pkg/logging"
	"github.com/roastco/ecommerce/pkg/middleware"
	"github.com/roastco/ecommerce/pkg/postgres"
	"github.com/roastco/ecommerce/pkg/tracing"
	grpcserver "github.com/roastco/ecommerce/services/product/internal/grpc"
	"github.com/roastco/ecommerce/services/product/internal/repository"
	"github.com/roastco/ecommerce/services/product/internal/service"
)

func main() {
	log := logging.New("product-service", config.String("LOG_LEVEL", "info"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Init(ctx, "product-service", config.String("OTLP_ENDPOINT", "jaeger:4317"))
	if err != nil {
		log.Warn("tracing init failed", "error", err)
	} else {
		defer shutdownTracing(context.Background())
	}

	pool, err := postgres.NewPool(ctx, config.MustString("DATABASE_URL"))
	if err != nil {
		log.Error("connect postgres failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	var pub service.Publisher = service.NoopPublisher{}
	if brokers := config.String("KAFKA_BROKERS", ""); brokers != "" {
		producer := kafkaclient.NewProducer(strings.Split(brokers, ","), "product.events")
		defer producer.Close()
		pub = producer
	}

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(middleware.ForwardMetadataUnaryClientInterceptor()),
	}
	userConn, err := grpc.NewClient(config.String("USER_SERVICE_ADDR", "user-service:9001"), dialOpts...)
	if err != nil {
		log.Warn("dial user service failed", "error", err)
	} else {
		defer userConn.Close()
	}

	orderConn, err := grpc.NewClient(config.String("ORDER_SERVICE_ADDR", "order-service:9004"), dialOpts...)
	if err != nil {
		log.Warn("dial order service failed", "error", err)
	} else {
		defer orderConn.Close()
	}

	userClient := userv1.NewUserServiceClient(userConn)
	orderClient := orderv1.NewOrderServiceClient(orderConn)

	repo := repository.New(pool.Pool)
	svc := service.New(repo, service.Config{
		ObjectStorageEndpoint: config.String("OBJECT_STORAGE_ENDPOINT", "http://minio:9000"),
		ObjectStorageBucket:   config.String("OBJECT_STORAGE_BUCKET", "product-images"),
		ReservationHoldTTL:    config.Duration("RESERVATION_HOLD_TTL", 10*time.Minute),
	}, pub, userClient, orderClient, log)

	// Background sweeper for expired reservations
	sweepInterval := config.Duration("SWEEP_INTERVAL", 30*time.Second)
	go func() {
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := svc.SweepExpiredReservations(ctx); err != nil {
					log.Warn("reservation sweeper failed", "error", err)
				}
			}
		}
	}()

	srv := grpc.NewServer()
	productv1.RegisterProductServiceServer(srv, grpcserver.New(svc, log))
	reflection.Register(srv)

	lis, err := net.Listen("tcp", config.String("GRPC_ADDR", ":9002"))
	if err != nil {
		log.Error("listen failed", "error", err)
		os.Exit(1)
	}

	go func() {
		<-ctx.Done()
		log.Info("shutting down gRPC server")
		srv.GracefulStop()
	}()

	log.Info("product-service starting", "addr", config.String("GRPC_ADDR", ":9002"))
	if err := srv.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		log.Error("serve failed", "error", err)
		os.Exit(1)
	}
}
