package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	orderv1 "github.com/roastco/ecommerce/gen/order/v1"
	paymentv1 "github.com/roastco/ecommerce/gen/payment/v1"
	productv1 "github.com/roastco/ecommerce/gen/product/v1"
	"github.com/roastco/ecommerce/pkg/config"
	"github.com/roastco/ecommerce/pkg/kafkaclient"
	"github.com/roastco/ecommerce/pkg/logging"
	"github.com/roastco/ecommerce/pkg/middleware"
	"github.com/roastco/ecommerce/pkg/postgres"
	"github.com/roastco/ecommerce/pkg/redisclient"
	"github.com/roastco/ecommerce/pkg/tracing"
	grpcserver "github.com/roastco/ecommerce/services/order/internal/grpc"
	"github.com/roastco/ecommerce/services/order/internal/repository"
	"github.com/roastco/ecommerce/services/order/internal/saga"
	"github.com/roastco/ecommerce/services/order/internal/service"
	"github.com/roastco/ecommerce/services/order/internal/webhook"
)

func main() {
	log := logging.New("order-service", config.String("LOG_LEVEL", "info"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Init(ctx, "order-service", config.String("OTLP_ENDPOINT", "jaeger:4317"))
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

	redis := redisclient.New(
		config.String("REDIS_URL", config.String("REDIS_ADDR", "localhost:6380")),
		config.String("REDIS_PASSWORD", ""),
		config.Int("REDIS_DB", 0),
	)
	defer redis.Close()

	var pub saga.Publisher = saga.NoopPublisher{}
	brokers := config.String("KAFKA_BROKERS", "")
	if brokers != "" {
		producer := kafkaclient.NewProducer(strings.Split(brokers, ","), "order.events")
		defer producer.Close()
		pub = producer
	}

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(middleware.ForwardMetadataUnaryClientInterceptor()),
	}

	prodConn, err := grpc.NewClient(config.String("PRODUCT_SERVICE_ADDR", "product-service:9002"), dialOpts...)
	if err != nil {
		log.Warn("dial product service failed", "error", err)
	} else {
		defer prodConn.Close()
	}

	payConn, err := grpc.NewClient(config.String("PAYMENT_SERVICE_ADDR", "payment-service:9005"), dialOpts...)
	if err != nil {
		log.Warn("dial payment service failed", "error", err)
	} else {
		defer payConn.Close()
	}

	prodClient := productv1.NewProductServiceClient(prodConn)
	payClient := paymentv1.NewPaymentServiceClient(payConn)

	repo := repository.New(pool.Pool)
	orchestrator := saga.NewOrchestrator(repo, redis, prodClient, payClient, pub, log)
	svc := service.New(repo, orchestrator, pub, redis, log)

	// Kafka consumer: order-saga-resumer on payment.events
	if brokers != "" {
		consumer := kafkaclient.NewConsumer(strings.Split(brokers, ","), "payment.events", "order-saga-resumer")
		defer consumer.Close()

		go func() {
			log.Info("starting order-saga-resumer consumer")
			err := consumer.Consume(ctx, func(cctx context.Context, evt kafkaclient.Event) error {
				var payload struct {
					SubOrderID string `json:"sub_order_id"`
				}
				if err := json.Unmarshal(evt.Payload, &payload); err == nil && payload.SubOrderID != "" {
					if evt.Type == "payment.charge_succeeded" {
						return orchestrator.ResumeSubOrderSaga(cctx, payload.SubOrderID, "succeeded")
					} else if evt.Type == "payment.charge_failed" {
						return orchestrator.ResumeSubOrderSaga(cctx, payload.SubOrderID, "failed")
					}
				}
				return nil
			})
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("order-saga-resumer exited", "error", err)
			}
		}()
	}

	// Sweepers
	sweepInterval := config.Duration("SWEEP_INTERVAL", 30*time.Second)
	go func() {
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := orchestrator.SweepAwaitingPayment(ctx); err != nil {
					log.Warn("awaiting payment sweep failed", "error", err)
				}
				if err := orchestrator.SweepAutoDelivered(ctx); err != nil {
					log.Warn("auto delivered sweep failed", "error", err)
				}
			}
		}
	}()

	// HTTP Webhook server for shipping
	shippingHandler := webhook.NewHandler(svc, webhook.Config{
		ShippingSecret: config.String("SHIPPING_WEBHOOK_SECRET", ""),
	}, log)

	httpAddr := config.String("HTTP_ADDR", ":9104")
	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: shippingHandler,
	}

	go func() {
		log.Info("order shipping webhook listener starting", "addr", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server failed", "error", err)
		}
	}()

	// gRPC server
	srv := grpc.NewServer()
	orderv1.RegisterOrderServiceServer(srv, grpcserver.New(svc, log))
	reflection.Register(srv)

	grpcAddr := config.String("GRPC_ADDR", ":9004")
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Error("listen failed", "addr", grpcAddr, "error", err)
		os.Exit(1)
	}

	go func() {
		<-ctx.Done()
		log.Info("shutting down servers")
		srv.GracefulStop()
		_ = httpServer.Shutdown(context.Background())
	}()

	log.Info("order-service starting", "addr", grpcAddr)
	if err := srv.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		log.Error("serve failed", "error", err)
		os.Exit(1)
	}
}
