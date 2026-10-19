package providers

import (
	"context"
	"errors"
)

var (
	ErrInvalidSignature = errors.New("invalid webhook signature")
	ErrUnsupportedMethod = errors.New("unsupported payment method")
)

type ChargeInput struct {
	SubOrderID             string
	AmountMinor            int64
	Currency               string
	PaymentMethod          string // "card" | "upi" | "net_banking"
	PaymentMethodToken     string
	SellerPayoutAccountRef string
}

type ChargeResult struct {
	ProviderChargeID string
	Status           string // "SUCCEEDED" | "PENDING" | "FAILED"
	FailureReason    string
}

type RefundInput struct {
	ProviderChargeID string
	AmountMinor      int64
	Currency         string
	Reason           string
}

type RefundResult struct {
	ProviderRefundID string
	Status           string
	FailureReason    string
}

type PayoutInput struct {
	SellerPayoutAccountRef string
	AmountMinor            int64
	Currency               string
	Method                 string
}

type PayoutResult struct {
	ProviderPayoutID string
	Status           string
}

type ProviderEvent struct {
	EventID          string
	EventType        string // "charge.succeeded" | "charge.failed"
	ProviderChargeID string
	SubOrderID       string
	Status           string // "SUCCEEDED" | "FAILED"
}

type Provider interface {
	Name() string
	Charge(ctx context.Context, req ChargeInput) (ChargeResult, error)
	Refund(ctx context.Context, req RefundInput) (RefundResult, error)
	Payout(ctx context.Context, req PayoutInput) (PayoutResult, error)
	VerifyWebhookSignature(header string, body []byte, secret string) error
	ParseWebhookEvent(body []byte) (ProviderEvent, error)
}
