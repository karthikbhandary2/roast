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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	paymentv1 "github.com/roastco/ecommerce/gen/payment/v1"
	userv1 "github.com/roastco/ecommerce/gen/user/v1"
	"github.com/roastco/ecommerce/pkg/config"
	"github.com/roastco/ecommerce/pkg/kafkaclient"
	"github.com/roastco/ecommerce/pkg/logging"
	"github.com/roastco/ecommerce/pkg/middleware"
	"github.com/roastco/ecommerce/pkg/postgres"
	"github.com/roastco/ecommerce/pkg/tracing"
	grpcserver "github.com/roastco/ecommerce/services/payment/internal/grpc"
	"github.com/roastco/ecommerce/services/payment/internal/providers"
	"github.com/roastco/ecommerce/services/payment/internal/repository"
	"github.com/roastco/ecommerce/services/payment/internal/service"
	"github.com/roastco/ecommerce/services/payment/internal/webhook"
)

func main() {
	log := logging.New("payment-service", config.String("LOG_LEVEL", "info"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Init(ctx, "payment-service", config.String("OTLP_ENDPOINT", "jaeger:4317"))
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
	brokers := config.String("KAFKA_BROKERS", "")
	if brokers != "" {
		producer := kafkaclient.NewProducer(strings.Split(brokers, ","), "payment.events")
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
	userClient := userv1.NewUserServiceClient(userConn)

	provs := map[string]providers.Provider{
		"phonepe": providers.NewPhonePe(
			config.String("PHONEPE_MERCHANT_ID", "PGTESTPAYUAT86"),
			config.String("PHONEPE_SALT_KEY", "96434309-7796-489d-8924-ab56988a6076"),
			config.String("PHONEPE_SALT_INDEX", "1"),
			config.String("PHONEPE_HOST_URL", "https://api-preprod.phonepe.com/apis/pg-sandbox"),
		),
		"razorpay": providers.NewRazorpay(config.String("RAZORPAY_KEY_ID", ""), config.String("RAZORPAY_KEY_SECRET", "")),
		"stripe":   providers.NewStripe(config.String("STRIPE_SECRET_KEY", "")),
	}

	repo := repository.New(pool.Pool)
	svc := service.New(repo, pub, userClient, provs, log)

	// Kafka consumer for order.events -> payment-earnings-updater
	if brokers != "" {
		consumer := kafkaclient.NewConsumer(strings.Split(brokers, ","), "order.events", "payment-earnings-updater")
		defer consumer.Close()

		go func() {
			log.Info("starting payment-earnings-updater consumer")
			err := consumer.Consume(ctx, func(cctx context.Context, evt kafkaclient.Event) error {
				if evt.Type == "order.sub_order_confirmed" {
					var payload struct {
						SubOrderID string `json:"sub_order_id"`
					}
					if err := json.Unmarshal(evt.Payload, &payload); err == nil && payload.SubOrderID != "" {
						return svc.HandleSubOrderConfirmed(cctx, payload.SubOrderID)
					}
				}
				return nil
			})
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("consumer loop exited", "error", err)
			}
		}()
	}

	// HTTP Webhook server
	webhookHandler := webhook.NewHandler(svc, webhook.Config{
		PhonePeSecret:  config.String("PHONEPE_SALT_KEY", "96434309-7796-489d-8924-ab56988a6076"),
		RazorpaySecret: config.String("RAZORPAY_WEBHOOK_SECRET", ""),
		StripeSecret:   config.String("STRIPE_WEBHOOK_SECRET", ""),
	}, log)

	httpAddr := config.String("HTTP_ADDR", ":9105")
	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: webhookHandler,
	}

	go func() {
		log.Info("payment webhook listener starting", "addr", httpAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http server failed", "error", err)
		}
	}()

	// gRPC server
	srv := grpc.NewServer()
	paymentv1.RegisterPaymentServiceServer(srv, grpcserver.New(svc, log))
	reflection.Register(srv)

	grpcAddr := config.String("GRPC_ADDR", ":9005")
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

	log.Info("payment-service starting", "addr", grpcAddr)
	if err := srv.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		log.Error("serve failed", "error", err)
		os.Exit(1)
	}
}
