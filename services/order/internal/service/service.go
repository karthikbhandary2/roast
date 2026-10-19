package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	orderv1 "github.com/roastco/ecommerce/gen/order/v1"
	paymentv1 "github.com/roastco/ecommerce/gen/payment/v1"
	"github.com/roastco/ecommerce/pkg/redisclient"
	"github.com/roastco/ecommerce/services/order/internal/repository"
	"github.com/roastco/ecommerce/services/order/internal/saga"
)

var (
	ErrNotFound           = repository.ErrNotFound
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrPermissionDenied   = errors.New("permission denied")
	ErrFailedPrecondition = errors.New("failed precondition")
)

func invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidArgument, msg) }

type Service struct {
	repo         *repository.Repository
	orchestrator *saga.Orchestrator
	pub          saga.Publisher
	redis        *redisclient.Client
	log          *slog.Logger
}

func New(
	repo *repository.Repository,
	orchestrator *saga.Orchestrator,
	pub saga.Publisher,
	redis *redisclient.Client,
	log *slog.Logger,
) *Service {
	return &Service{
		repo:         repo,
		orchestrator: orchestrator,
		pub:          pub,
		redis:        redis,
		log:          log,
	}
}

// ------------------------------------------------------------- cart

func (s *Service) AddToCart(ctx context.Context, buyerID, productID string, quantity int32) ([]redisclient.CartItem, time.Time, error) {
	return s.orchestrator.AddToCart(ctx, buyerID, productID, quantity)
}

func (s *Service) UpdateCartItem(ctx context.Context, buyerID, productID string, quantity int32) ([]redisclient.CartItem, time.Time, error) {
	return s.orchestrator.UpdateCartItem(ctx, buyerID, productID, quantity)
}

func (s *Service) GetCart(ctx context.Context, buyerID string) ([]redisclient.CartItem, time.Time, error) {
	return s.orchestrator.GetCart(ctx, buyerID)
}

// ------------------------------------------------------------- checkout

func (s *Service) Checkout(
	ctx context.Context,
	key, buyerID string,
	address json.RawMessage,
	paymentToken string,
	paymentMethod paymentv1.PaymentMethod,
) (*repository.Order, error) {
	return s.orchestrator.Checkout(ctx, saga.CheckoutInput{
		IdempotencyKey:     key,
		BuyerID:            buyerID,
		ShippingAddress:    address,
		PaymentMethodToken: paymentToken,
		PaymentMethod:      paymentMethod,
	})
}

func (s *Service) GetOrder(ctx context.Context, id string) (*repository.Order, error) {
	return s.repo.GetOrder(ctx, s.repo.DB(), id)
}

func (s *Service) ListOrdersForBuyer(ctx context.Context, buyerID string, pageSize int, pageToken string) ([]*repository.Order, string, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var after *repository.Cursor
	if pageToken != "" {
		c, err := decodeCursor(pageToken)
		if err != nil {
			return nil, "", err
		}
		after = c
	}

	orders, err := s.repo.ListOrdersForBuyer(ctx, s.repo.DB(), buyerID, after, pageSize+1)
	if err != nil {
		return nil, "", err
	}

	next := ""
	if len(orders) > pageSize {
		orders = orders[:pageSize]
		last := orders[len(orders)-1]
		next = encodeCursor(repository.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return orders, next, nil
}

func (s *Service) ListSubOrdersForSeller(
	ctx context.Context, sellerID string, statusFilter orderv1.SubOrderStatus, pageSize int, pageToken string,
) ([]*repository.SubOrder, string, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var after *repository.Cursor
	if pageToken != "" {
		c, err := decodeCursor(pageToken)
		if err != nil {
			return nil, "", err
		}
		after = c
	}

	statusStr := ""
	switch statusFilter {
	case orderv1.SubOrderStatus_SUB_ORDER_STATUS_RESERVING:
		statusStr = "reserving"
	case orderv1.SubOrderStatus_SUB_ORDER_STATUS_CHARGING:
		statusStr = "charging"
	case orderv1.SubOrderStatus_SUB_ORDER_STATUS_CONFIRMED:
		statusStr = "confirmed"
	case orderv1.SubOrderStatus_SUB_ORDER_STATUS_COMPENSATING:
		statusStr = "compensating"
	case orderv1.SubOrderStatus_SUB_ORDER_STATUS_FAILED:
		statusStr = "failed"
	case orderv1.SubOrderStatus_SUB_ORDER_STATUS_SHIPPED:
		statusStr = "shipped"
	case orderv1.SubOrderStatus_SUB_ORDER_STATUS_DELIVERED:
		statusStr = "delivered"
	case orderv1.SubOrderStatus_SUB_ORDER_STATUS_AWAITING_PAYMENT:
		statusStr = "awaiting_payment"
	}

	subs, err := s.repo.ListSubOrdersForSeller(ctx, s.repo.DB(), sellerID, statusStr, after, pageSize+1)
	if err != nil {
		return nil, "", err
	}

	next := ""
	if len(subs) > pageSize {
		subs = subs[:pageSize]
		last := subs[len(subs)-1]
		next = encodeCursor(repository.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return subs, next, nil
}

func (s *Service) ListAllOrders(
	ctx context.Context, statusFilter orderv1.OrderStatus, pageSize int, pageToken string,
) ([]*repository.Order, string, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var after *repository.Cursor
	if pageToken != "" {
		c, err := decodeCursor(pageToken)
		if err != nil {
			return nil, "", err
		}
		after = c
	}

	statusStr := ""
	switch statusFilter {
	case orderv1.OrderStatus_ORDER_STATUS_PROCESSING:
		statusStr = "processing"
	case orderv1.OrderStatus_ORDER_STATUS_CONFIRMED:
		statusStr = "confirmed"
	case orderv1.OrderStatus_ORDER_STATUS_PARTIALLY_FAILED:
		statusStr = "partially_failed"
	case orderv1.OrderStatus_ORDER_STATUS_FAILED:
		statusStr = "failed"
	}

	orders, err := s.repo.ListAllOrders(ctx, s.repo.DB(), statusStr, after, pageSize+1)
	if err != nil {
		return nil, "", err
	}

	next := ""
	if len(orders) > pageSize {
		orders = orders[:pageSize]
		last := orders[len(orders)-1]
		next = encodeCursor(repository.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return orders, next, nil
}

func (s *Service) MarkSubOrderShipped(ctx context.Context, subOrderID, sellerID, trackingNumber string) (*repository.SubOrder, error) {
	trackingNumber = strings.TrimSpace(trackingNumber)
	if trackingNumber == "" {
		return nil, invalid("tracking_number is required")
	}

	so, err := s.repo.MarkSubOrderShipped(ctx, s.repo.DB(), subOrderID, sellerID, trackingNumber)
	if err != nil {
		return nil, err
	}

	_ = s.pub.Publish(ctx, subOrderID, "order.shipped", map[string]string{
		"sub_order_id":    subOrderID,
		"tracking_number": trackingNumber,
	})

	if s.redis != nil {
		payload, _ := json.Marshal(map[string]string{
			"sub_order_id": subOrderID,
			"status":       "shipped",
		})
		_ = s.redis.PublishOrderStatus(ctx, so.ParentOrderID, payload)
	}

	return so, nil
}

func (s *Service) ConfirmDelivery(ctx context.Context, subOrderID, buyerID string) (*repository.SubOrder, error) {
	so, err := s.repo.ConfirmDeliveryByBuyer(ctx, s.repo.DB(), subOrderID, buyerID)
	if err != nil {
		return nil, err
	}

	_ = s.pub.Publish(ctx, subOrderID, "order.sub_order_delivered", map[string]string{
		"sub_order_id": subOrderID,
	})

	_, _ = s.repo.RecomputeParentOrderStatus(ctx, s.repo.DB(), so.ParentOrderID)

	if s.redis != nil {
		payload, _ := json.Marshal(map[string]string{
			"sub_order_id": subOrderID,
			"status":       "delivered",
		})
		_ = s.redis.PublishOrderStatus(ctx, so.ParentOrderID, payload)
	}

	return so, nil
}

func (s *Service) ConfirmDeliveryByCarrier(ctx context.Context, trackingNumber string) (*repository.SubOrder, error) {
	so, err := s.repo.ConfirmDeliveryByCarrier(ctx, s.repo.DB(), trackingNumber)
	if err != nil {
		return nil, err
	}

	_ = s.pub.Publish(ctx, so.ID, "order.sub_order_delivered", map[string]string{
		"sub_order_id":    so.ID,
		"tracking_number": trackingNumber,
	})

	_, _ = s.repo.RecomputeParentOrderStatus(ctx, s.repo.DB(), so.ParentOrderID)

	if s.redis != nil {
		payload, _ := json.Marshal(map[string]string{
			"sub_order_id": so.ID,
			"status":       "delivered",
		})
		_ = s.redis.PublishOrderStatus(ctx, so.ParentOrderID, payload)
	}

	return so, nil
}

func (s *Service) VerifyPurchase(ctx context.Context, buyerID, productID string) (bool, string, error) {
	return s.repo.VerifyPurchase(ctx, s.repo.DB(), buyerID, productID)
}

func (s *Service) HandleShippingWebhook(ctx context.Context, carrier, eventID, currentStatus, awb string, raw []byte) error {
	seen, err := s.repo.CheckShippingWebhookEvent(ctx, s.repo.DB(), carrier, eventID)
	if err != nil {
		return err
	}
	if seen {
		return nil
	}

	_ = s.repo.InsertShippingWebhookEvent(ctx, s.repo.DB(), carrier, eventID, currentStatus, raw)

	if strings.EqualFold(currentStatus, "Delivered") {
		_, err := s.ConfirmDeliveryByCarrier(ctx, awb)
		return err
	}
	return nil
}

// ------------------------------------------------------------- helpers

func encodeCursor(c repository.Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt.Format(time.RFC3339Nano) + "|" + c.ID))
}

func decodeCursor(token string) (*repository.Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, invalid("page_token is not valid")
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, invalid("page_token is not valid")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, invalid("page_token is not valid")
	}
	return &repository.Cursor{CreatedAt: t, ID: id}, nil
}
