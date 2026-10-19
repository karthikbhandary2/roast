// Package handlers implements every gateway route. Each handler is a
// method on *Deps so it has direct access to the typed gRPC clients and
// their breakers without a closure-returning-closure layer. Handlers
// decode the request, call a downstream service through its breaker,
// map the result (or error) to the standard envelope, and nothing else -
// no business logic lives here, only REST<->gRPC translation.
package handlers

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	notificationv1 "github.com/roastco/ecommerce/gen/notification/v1"
	orderv1 "github.com/roastco/ecommerce/gen/order/v1"
	paymentv1 "github.com/roastco/ecommerce/gen/payment/v1"
	productv1 "github.com/roastco/ecommerce/gen/product/v1"
	searchv1 "github.com/roastco/ecommerce/gen/search/v1"
	userv1 "github.com/roastco/ecommerce/gen/user/v1"

	"github.com/roastco/ecommerce/pkg/middleware"
	"github.com/roastco/ecommerce/pkg/redisclient"
)

type DownstreamAddrs struct {
	UserService         string
	ProductService      string
	SearchService       string
	OrderService        string
	PaymentService      string
	NotificationService string
	PaymentHTTPAddr     string
	RazorpayKeyID       string
	RazorpayKeySecret   string
	RedisAddr           string
	RedisPassword       string
	RedisDB             int
}

// Deps holds one typed gRPC client + circuit breaker pair per internal
// service, plus the Redis client used for the SSE order-status relay
// (LLD.md §2.4/§3.11). Handlers receive this rather than raw
// *grpc.ClientConn so they can't accidentally bypass a breaker.
type Deps struct {
	User         userv1.UserServiceClient
	Product      productv1.ProductServiceClient
	Search       searchv1.SearchServiceClient
	Order        orderv1.OrderServiceClient
	Payment      paymentv1.PaymentServiceClient
	Notification notificationv1.NotificationServiceClient
	Redis        *redisclient.Client

	PaymentHTTPAddr   string
	RazorpayKeyID     string
	RazorpayKeySecret string

	UserBreaker    *middleware.Breaker
	ProductBreaker *middleware.Breaker
	SearchBreaker  *middleware.Breaker
	OrderBreaker   *middleware.Breaker
	PaymentBreaker *middleware.Breaker

	conns []*grpc.ClientConn
}

func (d *Deps) Close() {
	for _, c := range d.conns {
		_ = c.Close()
	}
	if d.Redis != nil {
		_ = d.Redis.Close()
	}
}

// DialDownstreams opens one gRPC connection per internal service and
// connects Redis. In docker-compose, service DNS names resolve within the
// compose network, so no separate service-discovery layer is needed at
// this project's scale - see README.md §11 for how this would change
// under Kubernetes.
func DialDownstreams(ctx context.Context, addrs DownstreamAddrs) (*Deps, error) {
	dial := func(addr string) (*grpc.ClientConn, error) {
		// Internal traffic stays on the private compose/cluster network,
		// so plaintext is acceptable here - the gateway is the TLS
		// termination point for external traffic.
		return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	userConn, err := dial(addrs.UserService)
	if err != nil {
		return nil, fmt.Errorf("handlers: dial user service: %w", err)
	}
	productConn, err := dial(addrs.ProductService)
	if err != nil {
		return nil, fmt.Errorf("handlers: dial product service: %w", err)
	}
	searchConn, err := dial(addrs.SearchService)
	if err != nil {
		return nil, fmt.Errorf("handlers: dial search service: %w", err)
	}
	orderConn, err := dial(addrs.OrderService)
	if err != nil {
		return nil, fmt.Errorf("handlers: dial order service: %w", err)
	}
	paymentConn, err := dial(addrs.PaymentService)
	if err != nil {
		return nil, fmt.Errorf("handlers: dial payment service: %w", err)
	}
	notificationConn, err := dial(addrs.NotificationService)
	if err != nil {
		return nil, fmt.Errorf("handlers: dial notification service: %w", err)
	}

	redis := redisclient.New(addrs.RedisAddr, addrs.RedisPassword, addrs.RedisDB)
	if err := redis.Ping(ctx); err != nil {
		return nil, fmt.Errorf("handlers: connect redis: %w", err)
	}

	return &Deps{
		User:         userv1.NewUserServiceClient(userConn),
		Product:      productv1.NewProductServiceClient(productConn),
		Search:       searchv1.NewSearchServiceClient(searchConn),
		Order:        orderv1.NewOrderServiceClient(orderConn),
		Payment:      paymentv1.NewPaymentServiceClient(paymentConn),
		Notification: notificationv1.NewNotificationServiceClient(notificationConn),
		Redis:        redis,

		PaymentHTTPAddr:   addrs.PaymentHTTPAddr,
		RazorpayKeyID:     addrs.RazorpayKeyID,
		RazorpayKeySecret: addrs.RazorpayKeySecret,

		UserBreaker:    middleware.NewBreaker("user-service"),
		ProductBreaker: middleware.NewBreaker("product-service"),
		SearchBreaker:  middleware.NewBreaker("search-service"),
		OrderBreaker:   middleware.NewBreaker("order-service"),
		PaymentBreaker: middleware.NewBreaker("payment-service"),

		conns: []*grpc.ClientConn{userConn, productConn, searchConn, orderConn, paymentConn, notificationConn},
	}, nil
}
