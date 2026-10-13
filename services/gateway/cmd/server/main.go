// Command gateway is the public REST entrypoint. It terminates HTTP from
// browsers/mobile clients, validates JWTs, and fans requests out to the
// internal gRPC services - it holds no business logic and no database of
// its own.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/roastco/ecommerce/pkg/config"
	"github.com/roastco/ecommerce/pkg/logging"
	"github.com/roastco/ecommerce/pkg/tracing"
	"github.com/roastco/ecommerce/services/gateway/internal/handlers"
	"github.com/roastco/ecommerce/services/gateway/internal/router"
)

func main() {
	log := logging.New("gateway", config.String("LOG_LEVEL", "info"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Init(ctx, "gateway", config.String("OTLP_ENDPOINT", "jaeger:4317"))
	if err != nil {
		log.Error("failed to init tracing", "error", err)
		os.Exit(1)
	}
	defer shutdownTracing(context.Background())

	deps, err := handlers.DialDownstreams(ctx, handlers.DownstreamAddrs{
		UserService:         config.String("USER_SERVICE_ADDR", "user-service:9001"),
		ProductService:      config.String("PRODUCT_SERVICE_ADDR", "product-service:9002"),
		SearchService:       config.String("SEARCH_SERVICE_ADDR", "search-service:9003"),
		OrderService:        config.String("ORDER_SERVICE_ADDR", "order-service:9004"),
		PaymentService:      config.String("PAYMENT_SERVICE_ADDR", "payment-service:9005"),
		NotificationService: config.String("NOTIFICATION_SERVICE_ADDR", "notification-service:9006"),
		RedisAddr:           config.String("REDIS_ADDR", "redis:6379"),
		RedisPassword:       config.String("REDIS_PASSWORD", ""),
		RedisDB:             config.Int("REDIS_DB", 0),
	})
	if err != nil {
		log.Error("failed to dial downstream services", "error", err)
		os.Exit(1)
	}
	defer deps.Close()

	mux := router.New(deps, log, []byte(config.MustString("JWT_SECRET")), config.String("ALLOWED_ORIGIN", "*"))

	srv := &http.Server{
		Addr:              config.String("HTTP_ADDR", ":8080"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("gateway listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
	}
}
