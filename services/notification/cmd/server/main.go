package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	notificationv1 "github.com/roastco/ecommerce/gen/notification/v1"
	"github.com/roastco/ecommerce/pkg/config"
	"github.com/roastco/ecommerce/pkg/kafkaclient"
	"github.com/roastco/ecommerce/pkg/logging"
	"github.com/roastco/ecommerce/pkg/postgres"
	"github.com/roastco/ecommerce/pkg/tracing"
	grpcserver "github.com/roastco/ecommerce/services/notification/internal/grpc"
	"github.com/roastco/ecommerce/services/notification/internal/repository"
	"github.com/roastco/ecommerce/services/notification/internal/service"
)

func main() {
	log := logging.New("notification-service", config.String("LOG_LEVEL", "info"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Init(ctx, "notification-service", config.String("OTLP_ENDPOINT", "jaeger:4317"))
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

	repo := repository.New(pool.Pool)
	svc := service.New(repo, log)

	// Start background dispatch worker
	workerInterval := config.Duration("WORKER_INTERVAL", 5*time.Second)
	svc.StartWorker(ctx, workerInterval)

	brokers := config.String("KAFKA_BROKERS", "")
	if brokers != "" {
		brokerList := strings.Split(brokers, ",")

		// Helper to consume a topic
		startConsumer := func(topic string) {
			consumer := kafkaclient.NewConsumer(brokerList, topic, "notification-dispatcher")
			go func() {
				defer consumer.Close()
				err := consumer.Consume(ctx, func(cctx context.Context, evt kafkaclient.Event) error {
					var payload map[string]any
					_ = json.Unmarshal(evt.Payload, &payload)

					switch evt.Type {
					case "order.sub_order_confirmed":
						subOrderID, _ := payload["sub_order_id"].(string)
						buyerID, _ := payload["buyer_id"].(string)
						return svc.HandleOrderConfirmed(cctx, subOrderID, buyerID)
					case "order.shipped":
						subOrderID, _ := payload["sub_order_id"].(string)
						buyerID, _ := payload["buyer_id"].(string)
						trackingNumber, _ := payload["tracking_number"].(string)
						return svc.HandleShippingUpdate(cctx, subOrderID, buyerID, trackingNumber)
					case "inventory.changed":
						productID, _ := payload["product_id"].(string)
						sellerID, _ := payload["seller_id"].(string)
						stock, _ := payload["stock_available"].(float64)
						if stock <= 5 {
							return svc.HandleLowStock(cctx, sellerID, productID, int32(stock))
						}
					case "payment.payout_paid":
						sellerID, _ := payload["seller_id"].(string)
						payoutID, _ := payload["payout_id"].(string)
						return svc.HandlePayout(cctx, sellerID, payoutID)
					case "user.seller_active", "user.seller_suspended", "user.seller_rejected":
						sellerID, _ := payload["user_id"].(string)
						reason, _ := payload["reason"].(string)
						return svc.HandleSellerStatus(cctx, sellerID, evt.Type, reason)
					}
					return nil
				})
				if err != nil && !errors.Is(err, context.Canceled) {
					log.Warn("notification consumer exited", "topic", topic, "error", err)
				}
			}()
		}

		startConsumer("order.events")
		startConsumer("payment.events")
		startConsumer("inventory.events")
		startConsumer("user.events")
	}

	// gRPC server
	srv := grpc.NewServer()
	notificationv1.RegisterNotificationServiceServer(srv, grpcserver.New(svc, log))
	reflection.Register(srv)

	grpcAddr := config.String("GRPC_ADDR", ":9006")
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

	log.Info("notification-service starting", "addr", grpcAddr)
	if err := srv.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		log.Error("serve failed", "error", err)
		os.Exit(1)
	}
}
