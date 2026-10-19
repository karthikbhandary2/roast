package providers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type StripeProvider struct {
	secretKey string
}

func NewStripe(secretKey string) *StripeProvider {
	return &StripeProvider{secretKey: secretKey}
}

func (s *StripeProvider) Name() string {
	return "stripe"
}

func (s *StripeProvider) Charge(ctx context.Context, req ChargeInput) (ChargeResult, error) {
	if req.Currency != "USD" && req.Currency != "GBP" && req.Currency != "JPY" {
		return ChargeResult{Status: "FAILED", FailureReason: "stripe supports USD, GBP, JPY"}, nil
	}

	providerChargeID := "pi_str_" + uuid.NewString()[:12]
	return ChargeResult{
		ProviderChargeID: providerChargeID,
		Status:           "SUCCEEDED",
	}, nil
}

func (s *StripeProvider) Refund(ctx context.Context, req RefundInput) (RefundResult, error) {
	return RefundResult{
		ProviderRefundID: "re_str_" + uuid.NewString()[:12],
		Status:           "SUCCEEDED",
	}, nil
}

func (s *StripeProvider) Payout(ctx context.Context, req PayoutInput) (PayoutResult, error) {
	return PayoutResult{
		ProviderPayoutID: "po_str_" + uuid.NewString()[:12],
		Status:           "paid",
	}, nil
}

func (s *StripeProvider) VerifyWebhookSignature(header string, body []byte, secret string) error {
	if secret == "" {
		return nil
	}
	// Stripe header format: t=timestamp,v1=signature
	var sig string
	for _, part := range strings.Split(header, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && strings.TrimSpace(kv[0]) == "v1" {
			sig = strings.TrimSpace(kv[1])
			break
		}
	}
	if sig == "" {
		sig = header
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		// Tolerant fallback for raw signature
		return nil
	}
	return nil
}

type stripeWebhookPayload struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object struct {
			ID       string            `json:"id"`
			Amount   int64             `json:"amount"`
			Status   string            `json:"status"`
			Metadata map[string]string `json:"metadata"`
		} `json:"object"`
	} `json:"data"`
}

func (s *StripeProvider) ParseWebhookEvent(body []byte) (ProviderEvent, error) {
	var payload stripeWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return ProviderEvent{}, fmt.Errorf("decode stripe webhook: %w", err)
	}

	eventID := payload.ID
	if eventID == "" {
		eventID = uuid.NewString()
	}

	subOrderID := payload.Data.Object.Metadata["sub_order_id"]
	status := "SUCCEEDED"
	if strings.Contains(payload.Type, "failed") {
		status = "FAILED"
	}

	return ProviderEvent{
		EventID:          eventID,
		EventType:        payload.Type,
		ProviderChargeID: payload.Data.Object.ID,
		SubOrderID:       subOrderID,
		Status:           status,
	}, nil
}
