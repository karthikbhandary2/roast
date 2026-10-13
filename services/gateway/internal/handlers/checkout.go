package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	orderv1 "github.com/roastco/ecommerce/gen/order/v1"
	paymentv1 "github.com/roastco/ecommerce/gen/payment/v1"
	gatewaymw "github.com/roastco/ecommerce/services/gateway/internal/middleware"

	"github.com/roastco/ecommerce/pkg/middleware"
)

type addressDTO struct {
	FullName   string `json:"full_name"`
	Line1      string `json:"line1"`
	Line2      string `json:"line2"`
	City       string `json:"city"`
	PostalCode string `json:"postal_code"`
	CountryCode string `json:"country_code"`
	Phone      string `json:"phone"`
}

type checkoutRequest struct {
	ShippingAddress   addressDTO `json:"shipping_address"`
	PaymentMethodToken string    `json:"payment_method_token"`
	PaymentMethod     string     `json:"payment_method"` // "card" | "upi" | "net_banking"
}

var paymentMethodFromString = map[string]paymentv1.PaymentMethod{
	"card":        paymentv1.PaymentMethod_PAYMENT_METHOD_CARD,
	"upi":         paymentv1.PaymentMethod_PAYMENT_METHOD_UPI,
	"net_banking": paymentv1.PaymentMethod_PAYMENT_METHOD_NET_BANKING,
}

// Checkout handles POST /v1/checkout. Requires an Idempotency-Key header -
// LLD.md §1 "Idempotency keys", §3.2.
func (d *Deps) Checkout(w http.ResponseWriter, r *http.Request) {
	var req checkoutRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, r, errMissingIdempotencyKey)
		return
	}

	buyerID := mustClaims(r).UserID
	correlationID := gatewaymw.RequestIDFromContext(r.Context())

	resp, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.CheckoutResponse, error) {
		return d.Order.Checkout(ctx, &orderv1.CheckoutRequest{
			Metadata: &commonv1.RequestMetadata{
				IdempotencyKey: idempotencyKey,
				CorrelationId:  correlationID,
			},
			BuyerId: buyerID,
			ShippingAddress: &commonv1.Address{
				FullName:    req.ShippingAddress.FullName,
				Line1:       req.ShippingAddress.Line1,
				Line2:       req.ShippingAddress.Line2,
				City:        req.ShippingAddress.City,
				PostalCode:  req.ShippingAddress.PostalCode,
				CountryCode: req.ShippingAddress.CountryCode,
				Phone:       req.ShippingAddress.Phone,
			},
			PaymentMethodToken: req.PaymentMethodToken,
			PaymentMethod:      paymentMethodFromString[req.PaymentMethod], // zero value (UNSPECIFIED) if unrecognized - Payment Service rejects that itself
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeDataWithLinks(w, http.StatusCreated, resp.Order, map[string]link{
		"self":   {Href: "/v1/orders/" + resp.Order.Id},
		"events": {Href: "/v1/orders/" + resp.Order.Id + "/events"},
	})
}

// ListOrders handles GET /v1/orders.
func (d *Deps) ListOrders(w http.ResponseWriter, r *http.Request) {
	buyerID := mustClaims(r).UserID

	resp, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.ListOrdersForBuyerResponse, error) {
		return d.Order.ListOrdersForBuyer(ctx, &orderv1.ListOrdersForBuyerRequest{
			BuyerId: buyerID,
			Page:    &commonv1.PageRequest{PageSize: pageSize(r, 20), PageToken: r.URL.Query().Get("page_token")},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeList(w, http.StatusOK, resp.Orders, resp.Page.GetNextPageToken())
}

// GetOrder handles GET /v1/orders/{orderID}.
func (d *Deps) GetOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderID")

	order, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.Order, error) {
		return d.Order.GetOrder(ctx, &orderv1.GetOrderRequest{OrderId: orderID})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeDataWithLinks(w, http.StatusOK, order, map[string]link{
		"self":   {Href: "/v1/orders/" + orderID},
		"events": {Href: "/v1/orders/" + orderID + "/events"},
	})
}

// ConfirmDelivery handles POST /v1/orders/{orderID}/sub-orders/{subOrderID}/confirm-delivery.
// Buyer-initiated fallback - LLD.md §2.4/§3.9.
func (d *Deps) ConfirmDelivery(w http.ResponseWriter, r *http.Request) {
	subOrderID := chi.URLParam(r, "subOrderID")
	buyerID := mustClaims(r).UserID

	subOrder, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.SubOrder, error) {
		return d.Order.ConfirmDelivery(ctx, &orderv1.ConfirmDeliveryRequest{
			SubOrderId: subOrderID,
			BuyerId:    buyerID,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusOK, subOrder)
}

// OrderEvents handles GET /v1/orders/{orderID}/events as a Server-Sent
// Events stream, relaying Order Service's Redis Pub/Sub publishes -
// LLD.md §2.4/§3.11. Polling GET /v1/orders/{orderID} remains the
// correctness fallback if this connection drops.
func (d *Deps) OrderEvents(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderID")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, errStreamingUnsupported)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context() // cancelled automatically when the client disconnects
	messages, closeSub := d.Redis.SubscribeOrderStatus(ctx, orderID)
	defer closeSub()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
