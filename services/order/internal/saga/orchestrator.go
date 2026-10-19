package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	paymentv1 "github.com/roastco/ecommerce/gen/payment/v1"
	productv1 "github.com/roastco/ecommerce/gen/product/v1"
	"github.com/roastco/ecommerce/pkg/money"
	"github.com/roastco/ecommerce/pkg/redisclient"
	"github.com/roastco/ecommerce/services/order/internal/repository"
)

var (
	ErrNotFound           = repository.ErrNotFound
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrFailedPrecondition = errors.New("failed precondition")
	ErrPermissionDenied   = errors.New("permission denied")
)

type ProductClient interface {
	GetProduct(ctx context.Context, in *productv1.GetProductRequest, opts ...grpc.CallOption) (*productv1.Product, error)
	ReserveInventory(ctx context.Context, in *productv1.ReserveInventoryRequest, opts ...grpc.CallOption) (*productv1.ReserveInventoryResponse, error)
	ReleaseInventory(ctx context.Context, in *productv1.ReleaseInventoryRequest, opts ...grpc.CallOption) (*productv1.ReleaseInventoryResponse, error)
	ConfirmReservation(ctx context.Context, in *productv1.ConfirmReservationRequest, opts ...grpc.CallOption) (*productv1.ConfirmReservationResponse, error)
}

type PaymentClient interface {
	ChargePayment(ctx context.Context, in *paymentv1.ChargePaymentRequest, opts ...grpc.CallOption) (*paymentv1.ChargePaymentResponse, error)
	RefundPayment(ctx context.Context, in *paymentv1.RefundPaymentRequest, opts ...grpc.CallOption) (*paymentv1.RefundPaymentResponse, error)
}

type Publisher interface {
	Publish(ctx context.Context, aggregateID, eventType string, payload any) error
}

type NoopPublisher struct{}

func (NoopPublisher) Publish(context.Context, string, string, any) error { return nil }

type Orchestrator struct {
	repo    *repository.Repository
	redis   *redisclient.Client
	product ProductClient
	payment PaymentClient
	pub     Publisher
	log     *slog.Logger
	cartTTL time.Duration
}

func NewOrchestrator(
	repo *repository.Repository,
	redis *redisclient.Client,
	product ProductClient,
	payment PaymentClient,
	pub Publisher,
	log *slog.Logger,
) *Orchestrator {
	return &Orchestrator{
		repo:    repo,
		redis:   redis,
		product: product,
		payment: payment,
		pub:     pub,
		log:     log,
		cartTTL: 10 * time.Minute,
	}
}

// ------------------------------------------------------------- cart

func (o *Orchestrator) AddToCart(ctx context.Context, buyerID, productID string, quantity int32) ([]redisclient.CartItem, time.Time, error) {
	if quantity <= 0 {
		return nil, time.Time{}, fmt.Errorf("%w: quantity must be > 0", ErrInvalidArgument)
	}

	prod, err := o.product.GetProduct(ctx, &productv1.GetProductRequest{ProductId: productID})
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("orchestrator: get product: %w", err)
	}
	if prod.GetStockAvailable() < quantity {
		return nil, time.Time{}, fmt.Errorf("%w: insufficient stock available", ErrFailedPrecondition)
	}

	curr, amt, err := money.ProtoToMoney(prod.GetPrice())
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("orchestrator: invalid product price: %w", err)
	}

	items, err := o.redis.GetCart(ctx, buyerID)
	if err != nil {
		return nil, time.Time{}, err
	}

	found := false
	for i, item := range items {
		if item.ProductID == productID {
			items[i].Quantity += quantity
			items[i].UnitPrice = amt
			items[i].Currency = curr
			found = true
			break
		}
	}
	if !found {
		items = append(items, redisclient.CartItem{
			ProductID: productID,
			SellerID:  prod.GetSellerId(),
			Quantity:  quantity,
			UnitPrice: amt,
			Currency:  curr,
		})
	}

	if err := o.redis.SetCart(ctx, buyerID, items, o.cartTTL); err != nil {
		return nil, time.Time{}, err
	}

	expiresAt := time.Now().Add(o.cartTTL)
	return items, expiresAt, nil
}

func (o *Orchestrator) UpdateCartItem(ctx context.Context, buyerID, productID string, quantity int32) ([]redisclient.CartItem, time.Time, error) {
	items, err := o.redis.GetCart(ctx, buyerID)
	if err != nil {
		return nil, time.Time{}, err
	}

	var updated []redisclient.CartItem
	for _, item := range items {
		if item.ProductID == productID {
			if quantity > 0 {
				item.Quantity = quantity
				updated = append(updated, item)
			}
		} else {
			updated = append(updated, item)
		}
	}

	ttl, _ := o.redis.CartTTL(ctx, buyerID)
	if ttl <= 0 {
		ttl = o.cartTTL
	}

	if err := o.redis.SetCart(ctx, buyerID, updated, ttl); err != nil {
		return nil, time.Time{}, err
	}

	return updated, time.Now().Add(ttl), nil
}

func (o *Orchestrator) GetCart(ctx context.Context, buyerID string) ([]redisclient.CartItem, time.Time, error) {
	items, err := o.redis.GetCart(ctx, buyerID)
	if err != nil {
		return nil, time.Time{}, err
	}
	ttl, _ := o.redis.CartTTL(ctx, buyerID)
	if ttl <= 0 {
		ttl = o.cartTTL
	}
	return items, time.Now().Add(ttl), nil
}

// ------------------------------------------------------------- checkout saga

type CheckoutInput struct {
	IdempotencyKey     string
	BuyerID            string
	ShippingAddress    json.RawMessage
	PaymentMethodToken string
	PaymentMethod      paymentv1.PaymentMethod
}

func (o *Orchestrator) Checkout(ctx context.Context, in CheckoutInput) (*repository.Order, error) {
	cartItems, err := o.redis.GetCart(ctx, in.BuyerID)
	if err != nil {
		return nil, err
	}
	if len(cartItems) == 0 {
		return nil, fmt.Errorf("%w: cart is empty", ErrFailedPrecondition)
	}

	// Group cart items by seller
	bySeller := make(map[string][]redisclient.CartItem)
	for _, item := range cartItems {
		bySeller[item.SellerID] = append(bySeller[item.SellerID], item)
	}

	var subOrderInputs []repository.NewSubOrderInput
	var totalOrderMinor int64
	primaryCurrency := cartItems[0].Currency

	for sellerID, items := range bySeller {
		var subtotal int64
		var repoItems []repository.SubOrderItem
		sellerCurrency := items[0].Currency

		for _, item := range items {
			itemSubtotal := int64(item.Quantity) * item.UnitPrice
			subtotal += itemSubtotal
			repoItems = append(repoItems, repository.SubOrderItem{
				ProductID:    item.ProductID,
				Quantity:     item.Quantity,
				UnitCurrency: item.Currency,
				UnitMinor:    item.UnitPrice,
			})
		}
		totalOrderMinor += subtotal
		subOrderInputs = append(subOrderInputs, repository.NewSubOrderInput{
			SellerID: sellerID,
			Currency: sellerCurrency,
			Subtotal: subtotal,
			Items:    repoItems,
		})
	}

	tx, err := o.repo.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: begin checkout tx: %w", err)
	}
	defer tx.Rollback(ctx)

	order, err := o.repo.CreateOrderWithSubOrders(
		ctx, tx, in.BuyerID, in.ShippingAddress, primaryCurrency, totalOrderMinor, subOrderInputs,
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("orchestrator: commit checkout rows: %w", err)
	}

	// Fan-out: run 1 goroutine per sub-order saga
	var wg sync.WaitGroup
	for _, sub := range order.SubOrders {
		wg.Add(1)
		go func(so *repository.SubOrder) {
			defer wg.Done()
			o.runSubOrderSaga(context.Background(), so, in.BuyerID, in.PaymentMethodToken, in.PaymentMethod)
		}(sub)
	}
	wg.Wait()

	_ = o.redis.DeleteCart(ctx, in.BuyerID)

	// Re-load the order with updated sub-order statuses
	finalOrder, err := o.repo.GetOrder(ctx, o.repo.DB(), order.ID)
	if err != nil {
		return order, nil
	}
	return finalOrder, nil
}

func (o *Orchestrator) runSubOrderSaga(
	ctx context.Context,
	sub *repository.SubOrder,
	buyerID string,
	paymentToken string,
	paymentMethod paymentv1.PaymentMethod,
) {
	correlationID := uuid.NewString()
	_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "reserve_inventory", "started", correlationID, nil)

	var lines []*productv1.ReservationLine
	for _, it := range sub.Items {
		lines = append(lines, &productv1.ReservationLine{
			ProductId: it.ProductID,
			Quantity:  it.Quantity,
		})
	}

	reserveKey := fmt.Sprintf("%s:reserve", sub.ID)
	reserveResp, err := o.product.ReserveInventory(ctx, &productv1.ReserveInventoryRequest{
		Metadata: &commonv1.RequestMetadata{
			IdempotencyKey: reserveKey,
			CorrelationId:  correlationID,
		},
		SubOrderId: sub.ID,
		SellerId:   sub.SellerID,
		Lines:      lines,
	})

	if err != nil || reserveResp.GetStatus() == commonv1.SagaStepStatus_SAGA_STEP_STATUS_FAILED {
		failReason := "reservation_failed"
		if reserveResp != nil && reserveResp.GetFailureReason() != "" {
			failReason = reserveResp.GetFailureReason()
		}
		_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "reserve_inventory", "failed", correlationID, map[string]string{"error": failReason})
		_, _ = o.repo.UpdateSubOrderStatus(ctx, o.repo.DB(), sub.ID, "failed", "", "", failReason)
		_, _ = o.repo.RecomputeParentOrderStatus(ctx, o.repo.DB(), sub.ParentOrderID)
		o.publishOrderStatus(ctx, sub.ParentOrderID, sub.ID, "failed")
		return
	}

	_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "reserve_inventory", "completed", correlationID, nil)
	reservationID := reserveResp.GetReservationId()
	sub.ReservationID = reservationID
	_, _ = o.repo.UpdateSubOrderStatus(ctx, o.repo.DB(), sub.ID, "charging", reservationID, "", "")

	chargeKey := fmt.Sprintf("%s:charge", sub.ID)
	chargeResp, err := o.payment.ChargePayment(ctx, &paymentv1.ChargePaymentRequest{
		Metadata: &commonv1.RequestMetadata{
			IdempotencyKey: chargeKey,
			CorrelationId:  correlationID,
		},
		SubOrderId:         sub.ID,
		BuyerId:            buyerID,
		SellerId:           sub.SellerID,
		Amount:             money.MoneyToProto(sub.SubtotalCurrency, sub.SubtotalMinor),
		PaymentMethodToken: paymentToken,
		PaymentMethod:      paymentMethod,
	})

	if err != nil || chargeResp.GetStatus() == paymentv1.ChargeStatus_CHARGE_STATUS_FAILED {
		failReason := "charge_failed"
		if err != nil {
			failReason = fmt.Sprintf("charge_rpc_error: %v", err)
			o.log.Error("orchestrator: charge_payment RPC failed", "error", err, "sub_order_id", sub.ID)
		} else if chargeResp != nil && chargeResp.GetFailureReason() != "" {
			failReason = chargeResp.GetFailureReason()
		}
		_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "charge_payment", "failed", correlationID, map[string]string{"error": failReason})

		// COMPENSATE: release inventory
		releaseKey := fmt.Sprintf("%s:release", sub.ID)
		_, _ = o.product.ReleaseInventory(ctx, &productv1.ReleaseInventoryRequest{
			Metadata: &commonv1.RequestMetadata{
				IdempotencyKey: releaseKey,
				CorrelationId:  correlationID,
			},
			ReservationId: reservationID,
		})
		_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "reserve_inventory", "compensated", correlationID, nil)

		_, _ = o.repo.UpdateSubOrderStatus(ctx, o.repo.DB(), sub.ID, "failed", reservationID, "", failReason)
		_, _ = o.repo.RecomputeParentOrderStatus(ctx, o.repo.DB(), sub.ParentOrderID)
		o.publishOrderStatus(ctx, sub.ParentOrderID, sub.ID, "failed")
		return
	}

	chargeID := chargeResp.GetChargeId()
	sub.ChargeID = chargeID

	if chargeResp.GetStatus() == paymentv1.ChargeStatus_CHARGE_STATUS_PENDING {
		// Asynchronous (e.g. UPI)
		_, _ = o.repo.UpdateSubOrderStatus(ctx, o.repo.DB(), sub.ID, "awaiting_payment", reservationID, chargeID, "")
		_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "charge_payment", "pending", correlationID, nil)
		o.publishOrderStatus(ctx, sub.ParentOrderID, sub.ID, "awaiting_payment")
		return
	}

	// Synchronously succeeded
	_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "charge_payment", "completed", correlationID, nil)
	o.finishSubOrderSaga(ctx, sub, reservationID, chargeID, correlationID)
}

func (o *Orchestrator) finishSubOrderSaga(
	ctx context.Context, sub *repository.SubOrder, reservationID, chargeID, correlationID string,
) {
	confirmKey := fmt.Sprintf("%s:confirm", sub.ID)
	_, err := o.product.ConfirmReservation(ctx, &productv1.ConfirmReservationRequest{
		Metadata: &commonv1.RequestMetadata{
			IdempotencyKey: confirmKey,
			CorrelationId:  correlationID,
		},
		ReservationId: reservationID,
	})

	if err != nil {
		// Confirmation failed -> compensate both payment and inventory in reverse order
		refundKey := fmt.Sprintf("%s:refund", sub.ID)
		_, _ = o.payment.RefundPayment(ctx, &paymentv1.RefundPaymentRequest{
			Metadata: &commonv1.RequestMetadata{
				IdempotencyKey: refundKey,
				CorrelationId:  correlationID,
			},
			ChargeId: chargeID,
			Reason:   "confirmation_failed",
		})
		_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "charge_payment", "compensated", correlationID, nil)

		releaseKey := fmt.Sprintf("%s:release", sub.ID)
		_, _ = o.product.ReleaseInventory(ctx, &productv1.ReleaseInventoryRequest{
			Metadata: &commonv1.RequestMetadata{
				IdempotencyKey: releaseKey,
				CorrelationId:  correlationID,
			},
			ReservationId: reservationID,
		})
		_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "reserve_inventory", "compensated", correlationID, nil)

		_, _ = o.repo.UpdateSubOrderStatus(ctx, o.repo.DB(), sub.ID, "failed", reservationID, chargeID, "confirmation_failed")
	} else {
		_, _ = o.repo.UpdateSubOrderStatus(ctx, o.repo.DB(), sub.ID, "confirmed", reservationID, chargeID, "")
		_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "confirm_order", "completed", correlationID, nil)

		_ = o.pub.Publish(ctx, sub.ID, "order.sub_order_confirmed", map[string]string{
			"sub_order_id":   sub.ID,
			"correlation_id": correlationID,
		})
	}

	_, _ = o.repo.RecomputeParentOrderStatus(ctx, o.repo.DB(), sub.ParentOrderID)
	o.publishOrderStatus(ctx, sub.ParentOrderID, sub.ID, "confirmed")
}

func (o *Orchestrator) ResumeSubOrderSaga(ctx context.Context, subOrderID, outcome string) error {
	// CAS update: only matches if status == 'awaiting_payment'
	sub, err := o.repo.CASResumeSubOrder(ctx, o.repo.DB(), subOrderID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil // Already resumed by another trigger
	}
	if err != nil {
		return err
	}

	correlationID := uuid.NewString()

	if outcome == "succeeded" {
		o.finishSubOrderSaga(ctx, sub, sub.ReservationID, sub.ChargeID, correlationID)
	} else {
		releaseKey := fmt.Sprintf("%s:release", sub.ID)
		_, _ = o.product.ReleaseInventory(ctx, &productv1.ReleaseInventoryRequest{
			Metadata: &commonv1.RequestMetadata{
				IdempotencyKey: releaseKey,
				CorrelationId:  correlationID,
			},
			ReservationId: sub.ReservationID,
		})
		_ = o.repo.LogSagaEvent(ctx, o.repo.DB(), sub.ID, "reserve_inventory", "compensated", correlationID, map[string]string{"outcome": outcome})

		_, _ = o.repo.UpdateSubOrderStatus(ctx, o.repo.DB(), sub.ID, "failed", sub.ReservationID, sub.ChargeID, outcome)
		_, _ = o.repo.RecomputeParentOrderStatus(ctx, o.repo.DB(), sub.ParentOrderID)
		o.publishOrderStatus(ctx, sub.ParentOrderID, sub.ID, "failed")
	}
	return nil
}

func (o *Orchestrator) publishOrderStatus(ctx context.Context, orderID, subOrderID, status string) {
	if o.redis == nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"sub_order_id": subOrderID,
		"status":       status,
	})
	_ = o.redis.PublishOrderStatus(ctx, orderID, payload)
}

// ------------------------------------------------------------- sweepers

func (o *Orchestrator) SweepAwaitingPayment(ctx context.Context) error {
	cutoff := time.Now().Add(-10 * time.Minute)
	ids, err := o.repo.SweepAwaitingPayment(ctx, o.repo.DB(), cutoff)
	if err != nil {
		return err
	}
	for _, id := range ids {
		_ = o.ResumeSubOrderSaga(ctx, id, "timeout")
	}
	return nil
}

func (o *Orchestrator) SweepAutoDelivered(ctx context.Context) error {
	cutoff := time.Now().Add(-14 * 24 * time.Hour)
	_, err := o.repo.SweepAutoDelivered(ctx, o.repo.DB(), cutoff)
	return err
}
