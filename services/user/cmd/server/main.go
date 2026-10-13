// Command user runs the User Service: identity, login tokens, and seller
// onboarding/approval, exposed over gRPC.
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
	"google.golang.org/grpc/reflection"

	userv1 "github.com/roastco/ecommerce/gen/user/v1"
	"github.com/roastco/ecommerce/pkg/config"
	"github.com/roastco/ecommerce/pkg/kafkaclient"
	"github.com/roastco/ecommerce/pkg/logging"
	"github.com/roastco/ecommerce/pkg/postgres"
	"github.com/roastco/ecommerce/pkg/tracing"
	grpcserver "github.com/roastco/ecommerce/services/user/internal/grpc"
	"github.com/roastco/ecommerce/services/user/internal/repository"
	"github.com/roastco/ecommerce/services/user/internal/service"
)

func main() {
	log := logging.New("user-service", config.String("LOG_LEVEL", "info"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Init(ctx, "user-service", config.String("OTLP_ENDPOINT", "jaeger:4317"))
	if err != nil {
		log.Error("failed to init tracing", "error", err)
		os.Exit(1)
	}
	defer shutdownTracing(context.Background())

	pool, err := postgres.NewPool(ctx, config.MustString("DATABASE_URL"))
	if err != nil {
		log.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Events are optional: without KAFKA_BROKERS the service runs fine and
	// simply doesn't announce seller approvals.
	var pub service.Publisher = service.NoopPublisher{}
	if brokers := config.String("KAFKA_BROKERS", ""); brokers != "" {
		producer := kafkaclient.NewProducer(strings.Split(brokers, ","), "user.events")
		defer producer.Close()
		pub = producer
	}

	jwtSecret := []byte(config.MustString("JWT_SECRET"))
	svc := service.New(repository.New(pool.Pool), service.Config{
		JWTSecret:       jwtSecret,
		AccessTokenTTL:  config.Duration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: config.Duration("REFRESH_TOKEN_TTL", 30*24*time.Hour),
		BcryptCost:      config.Int("BCRYPT_COST", 12),
	}, pub, log)

	lis, err := net.Listen("tcp", config.String("GRPC_ADDR", ":9001"))
	if err != nil {
		log.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	srv := grpc.NewServer(grpc.UnaryInterceptor(grpcserver.AuthInterceptor(jwtSecret)))
	userv1.RegisterUserServiceServer(srv, grpcserver.New(svc, log))
	reflection.Register(srv) // lets grpcurl list and call methods without the .proto files

	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		srv.GracefulStop()
	}()

	log.Info("user service listening", "addr", lis.Addr().String())
	if err := srv.Serve(lis); err != nil {
		log.Error("grpc server error", "error", err)
		os.Exit(1)
	}
}
