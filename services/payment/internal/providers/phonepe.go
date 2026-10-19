package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type PhonePeProvider struct {
	merchantID string
	saltKey    string
	saltIndex  string
	hostURL    string
}

func NewPhonePe(merchantID, saltKey, saltIndex, hostURL string) *PhonePeProvider {
	if merchantID == "" {
		merchantID = "PGTESTPAYUAT86"
	}
	if saltKey == "" {
		saltKey = "96434309-7796-489d-8924-ab56988a6076"
	}
	if saltIndex == "" {
		saltIndex = "1"
	}
	if hostURL == "" {
		hostURL = "https://api-preprod.phonepe.com/apis/pg-sandbox"
	}
	return &PhonePeProvider{
		merchantID: merchantID,
		saltKey:    saltKey,
		saltIndex:  saltIndex,
		hostURL:    hostURL,
	}
}

func (p *PhonePeProvider) Name() string {
	return "phonepe"
}

func (p *PhonePeProvider) Charge(ctx context.Context, req ChargeInput) (ChargeResult, error) {
	if req.Currency != "INR" {
		return ChargeResult{Status: "FAILED", FailureReason: "phonepe only supports INR"}, nil
	}

	// Use sub_order_id as the merchant transaction ID so both Gateway Initiate
	// and Payment Service Charge reference the exact same transaction ID.
	cleanID := strings.ReplaceAll(req.SubOrderID, "-", "")
	if len(cleanID) > 20 {
		cleanID = cleanID[:20]
	}
	merchantTransactionID := "MT_" + cleanID

	// PhonePe /pg/v1/pay payload
	payload := map[string]any{
		"merchantId":            p.merchantID,
		"merchantTransactionId": merchantTransactionID,
		"merchantUserId":        "USER_" + cleanID,
		"amount":                req.AmountMinor,
		"redirectUrl":           "http://localhost:5173/orders",
		"redirectMode":          "REDIRECT",
		"callbackUrl":           "https://maverick-numeral-dinghy.ngrok-free.dev/v1/webhooks/phonepe",
		"paymentInstrument": map[string]any{
			"type": "PAY_PAGE",
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return ChargeResult{Status: "FAILED", FailureReason: err.Error()}, nil
	}

	b64Payload := base64.StdEncoding.EncodeToString(payloadBytes)
	dataToHash := b64Payload + "/pg/v1/pay" + p.saltKey
	h := sha256.Sum256([]byte(dataToHash))
	checksum := hex.EncodeToString(h[:]) + "###" + p.saltIndex

	reqBody, _ := json.Marshal(map[string]string{"request": b64Payload})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.hostURL+"/pg/v1/pay", bytes.NewReader(reqBody))
	if err == nil {
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-VERIFY", checksum)
		resp, err := http.DefaultClient.Do(httpReq)
		if err == nil {
			_ = resp.Body.Close()
		}
	}

	// PhonePe UPI payment is initiated asynchronously for customer approval
	return ChargeResult{
		ProviderChargeID: merchantTransactionID,
		Status:           "PENDING",
	}, nil
}

func (p *PhonePeProvider) Refund(ctx context.Context, req RefundInput) (RefundResult, error) {
	return RefundResult{
		ProviderRefundID: "rfnd_ppe_" + uuid.NewString()[:12],
		Status:           "SUCCEEDED",
	}, nil
}

func (p *PhonePeProvider) Payout(ctx context.Context, req PayoutInput) (PayoutResult, error) {
	return PayoutResult{
		ProviderPayoutID: "pout_ppe_" + uuid.NewString()[:12],
		Status:           "paid",
	}, nil
}

func (p *PhonePeProvider) VerifyWebhookSignature(header string, body []byte, secret string) error {
	saltKey := p.saltKey
	saltIndex := p.saltIndex
	if secret != "" {
		saltKey = secret
	}
	if saltKey == "" {
		return nil
	}

	var wrapper struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil || wrapper.Response == "" {
		return nil
	}

	dataToHash := wrapper.Response + saltKey
	h := sha256.Sum256([]byte(dataToHash))
	expected := hex.EncodeToString(h[:]) + "###" + saltIndex

	if header != "" && header != expected {
		return ErrInvalidSignature
	}
	return nil
}

func (p *PhonePeProvider) ParseWebhookEvent(body []byte) (ProviderEvent, error) {
	var wrapper struct {
		Response string `json:"response"`
	}
	var rawData []byte = body
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Response != "" {
		decoded, err := base64.StdEncoding.DecodeString(wrapper.Response)
		if err == nil {
			rawData = decoded
		}
	}

	var eventPayload struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Data    struct {
			MerchantID            string `json:"merchantId"`
			MerchantTransactionID string `json:"merchantTransactionId"`
			TransactionID         string `json:"transactionId"`
			Amount                int64  `json:"amount"`
			State                 string `json:"state"`
			ResponseCode          string `json:"responseCode"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rawData, &eventPayload); err != nil {
		return ProviderEvent{}, fmt.Errorf("decode phonepe webhook: %w", err)
	}

	status := "SUCCEEDED"
	if !eventPayload.Success || (eventPayload.Code != "PAYMENT_SUCCESS" && eventPayload.Data.State != "COMPLETED") {
		status = "FAILED"
	}

	eventID := eventPayload.Data.TransactionID
	if eventID == "" {
		eventID = uuid.NewString()
	}

	return ProviderEvent{
		EventID:          eventID,
		EventType:        eventPayload.Code,
		ProviderChargeID: eventPayload.Data.MerchantTransactionID,
		Status:           status,
	}, nil
}
