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

type RazorpayProvider struct {
	keyID     string
	keySecret string
}

func NewRazorpay(keyID, keySecret string) *RazorpayProvider {
	return &RazorpayProvider{keyID: keyID, keySecret: keySecret}
}

func (r *RazorpayProvider) Name() string {
	return "razorpay"
}

func (r *RazorpayProvider) Charge(ctx context.Context, req ChargeInput) (ChargeResult, error) {
	if req.Currency != "INR" {
		return ChargeResult{Status: "FAILED", FailureReason: "razorpay only supports INR"}, nil
	}

	providerChargeID := "pay_rzp_" + uuid.NewString()[:12]

	// UPI is asynchronous: customer opens their UPI app to approve collect request
	if req.PaymentMethod == "upi" {
		return ChargeResult{
			ProviderChargeID: providerChargeID,
			Status:           "PENDING",
		}, nil
	}

	// Card or Net banking is synchronous
	return ChargeResult{
		ProviderChargeID: providerChargeID,
		Status:           "SUCCEEDED",
	}, nil
}

func (r *RazorpayProvider) Refund(ctx context.Context, req RefundInput) (RefundResult, error) {
	return RefundResult{
		ProviderRefundID: "rfnd_rzp_" + uuid.NewString()[:12],
		Status:           "SUCCEEDED",
	}, nil
}

func (r *RazorpayProvider) Payout(ctx context.Context, req PayoutInput) (PayoutResult, error) {
	return PayoutResult{
		ProviderPayoutID: "pout_rzp_" + uuid.NewString()[:12],
		Status:           "paid",
	}, nil
}

func (r *RazorpayProvider) VerifyWebhookSignature(header string, body []byte, secret string) error {
	if secret == "" {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(header), []byte(expected)) {
		return ErrInvalidSignature
	}
	return nil
}

type razorpayWebhookPayload struct {
	Entity  string `json:"entity"`
	Event   string `json:"event"`
	Payload struct {
		Payment struct {
			Entity struct {
				ID      string            `json:"id"`
				Amount  int64             `json:"amount"`
				Status  string            `json:"status"`
				Notes   map[string]string `json:"notes"`
				OrderID string            `json:"order_id"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}

func (r *RazorpayProvider) ParseWebhookEvent(body []byte) (ProviderEvent, error) {
	var payload razorpayWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return ProviderEvent{}, fmt.Errorf("decode razorpay webhook: %w", err)
	}

	eventID := payload.Payload.Payment.Entity.ID
	if eventID == "" {
		eventID = uuid.NewString()
	}

	subOrderID := payload.Payload.Payment.Entity.Notes["sub_order_id"]
	status := "SUCCEEDED"
	if strings.Contains(payload.Event, "failed") {
		status = "FAILED"
	}

	return ProviderEvent{
		EventID:          eventID,
		EventType:        payload.Event,
		ProviderChargeID: payload.Payload.Payment.Entity.ID,
		SubOrderID:       subOrderID,
		Status:           status,
	}, nil
}
