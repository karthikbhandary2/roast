package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type simulateUPIRequest struct {
	SubOrderID string `json:"sub_order_id"`
	Action     string `json:"action"` // "approve" | "decline"
}

// RazorpayWebhookProxy proxies inbound webhooks from external Razorpay callbacks
// to the internal Payment Service's HTTP webhook listener.
func (d *Deps) RazorpayWebhookProxy(w http.ResponseWriter, r *http.Request) {
	if d.PaymentHTTPAddr == "" {
		writeError(w, r, fmt.Errorf("payment webhook downstream not configured"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}

	targetURL := d.PaymentHTTPAddr + "/webhooks/razorpay"
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		writeError(w, r, err)
		return
	}

	for k, v := range r.Header {
		proxyReq.Header[k] = v
	}

	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// SimulateUPIPayment enables interactive frontend UPI testing and local sandbox
// simulation. Given a sub_order_id, it triggers an asynchronous payment webhook
// that publishes the corresponding Kafka event to complete or fail the order saga.
func (d *Deps) SimulateUPIPayment(w http.ResponseWriter, r *http.Request) {
	var req simulateUPIRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}

	if req.SubOrderID == "" {
		writeError(w, r, fmt.Errorf("sub_order_id is required"))
		return
	}

	status := "captured"
	eventName := "payment.captured"
	if req.Action == "decline" {
		status = "failed"
		eventName = "payment.failed"
	}

	simulatedID := "pay_rzp_sim_" + uuid.NewString()[:8]
	payload := map[string]any{
		"entity": "event",
		"event":  eventName,
		"payload": map[string]any{
			"payment": map[string]any{
				"entity": map[string]any{
					"id":     simulatedID,
					"amount": 55000,
					"status": status,
					"notes": map[string]string{
						"sub_order_id": req.SubOrderID,
					},
				},
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		writeError(w, r, err)
		return
	}

	targetURL := d.PaymentHTTPAddr + "/webhooks/razorpay"
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(payloadBytes))
	if err != nil {
		writeError(w, r, err)
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		writeError(w, r, fmt.Errorf("payment service responded with %d: %s", resp.StatusCode, string(respBody)))
		return
	}

	writeData(w, http.StatusOK, map[string]any{
		"status":       "simulated",
		"action":       req.Action,
		"sub_order_id": req.SubOrderID,
		"charge_id":    simulatedID,
	})
}

// PaymentConfig returns public payment gateway configuration (e.g. Razorpay Key ID).
func (d *Deps) PaymentConfig(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, map[string]any{
		"razorpay_key_id": d.RazorpayKeyID,
	})
}

type createRazorpayOrderRequest struct {
	Amount   int64             `json:"amount"`
	Currency string            `json:"currency"`
	Receipt  string            `json:"receipt"`
	Notes    map[string]string `json:"notes"`
}

// CreateRazorpayOrder calls Razorpay's Orders API so Checkout modal has an authentic order_id,
// which is required by Razorpay to display UPI (GPay, PhonePe, Paytm, QR).
func (d *Deps) CreateRazorpayOrder(w http.ResponseWriter, r *http.Request) {
	var req createRazorpayOrderRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}

	if req.Amount <= 0 {
		req.Amount = 100
	}
	if req.Currency == "" {
		req.Currency = "INR"
	}

	orderReqBody, err := json.Marshal(map[string]any{
		"amount":   req.Amount,
		"currency": req.Currency,
		"receipt":  req.Receipt,
		"notes":    req.Notes,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://api.razorpay.com/v1/orders", bytes.NewReader(orderReqBody))
	if err != nil {
		writeError(w, r, err)
		return
	}

	httpReq.SetBasicAuth(d.RazorpayKeyID, d.RazorpayKeySecret)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		writeError(w, r, fmt.Errorf("razorpay api error: %s", string(respBody)))
		return
	}

	var rzpOrder map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rzpOrder); err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusOK, rzpOrder)
}

// PhonePeWebhookProxy proxies inbound webhooks from PhonePe callbacks
// to the internal Payment Service's HTTP webhook listener.
func (d *Deps) PhonePeWebhookProxy(w http.ResponseWriter, r *http.Request) {
	if d.PaymentHTTPAddr == "" {
		writeError(w, r, fmt.Errorf("payment webhook downstream not configured"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}

	targetURL := d.PaymentHTTPAddr + "/webhooks/phonepe"
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		writeError(w, r, err)
		return
	}

	for k, v := range r.Header {
		proxyReq.Header[k] = v
	}

	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

type initiatePhonePeRequest struct {
	Amount     int64  `json:"amount"` // in minor units (paise)
	OrderID    string `json:"order_id"`
	SubOrderID string `json:"sub_order_id"`
}

// InitiatePhonePePayment calls PhonePe's standard checkout pay endpoint
// and returns the official payment redirect URL.
func (d *Deps) InitiatePhonePePayment(w http.ResponseWriter, r *http.Request) {
	var req initiatePhonePeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}

	if req.Amount <= 0 {
		req.Amount = 100
	}

	merchantID := "PGTESTPAYUAT86"
	saltKey := "96434309-7796-489d-8924-ab56988a6076"
	saltIndex := "1"
	hostURL := "https://api-preprod.phonepe.com/apis/pg-sandbox"

	cleanID := strings.ReplaceAll(req.SubOrderID, "-", "")
	if len(cleanID) > 20 {
		cleanID = cleanID[:20]
	}
	merchantTransactionID := "MT_" + cleanID
	userID := "USER_" + cleanID

	redirectURL := fmt.Sprintf("http://localhost:5173/orders/%s/confirmation", req.OrderID)
	if req.OrderID == "" {
		redirectURL = "http://localhost:5173/orders"
	}
	callbackURL := "https://maverick-numeral-dinghy.ngrok-free.dev/v1/webhooks/phonepe"

	payload := map[string]any{
		"merchantId":            merchantID,
		"merchantTransactionId": merchantTransactionID,
		"merchantUserId":        userID,
		"amount":                req.Amount,
		"redirectUrl":           redirectURL,
		"redirectMode":          "REDIRECT",
		"callbackUrl":           callbackURL,
		"paymentInstrument": map[string]any{
			"type": "PAY_PAGE",
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		writeError(w, r, err)
		return
	}

	b64Payload := base64.StdEncoding.EncodeToString(payloadBytes)
	dataToHash := b64Payload + "/pg/v1/pay" + saltKey
	h := sha256.Sum256([]byte(dataToHash))
	checksum := hex.EncodeToString(h[:]) + "###" + saltIndex

	reqBody, _ := json.Marshal(map[string]string{"request": b64Payload})
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, hostURL+"/pg/v1/pay", bytes.NewReader(reqBody))
	if err != nil {
		writeError(w, r, err)
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-VERIFY", checksum)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		writeError(w, r, fmt.Errorf("phonepe api error (%d): %s", resp.StatusCode, string(respBody)))
		return
	}

	var phonepeResp struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Data    struct {
			MerchantID            string `json:"merchantId"`
			MerchantTransactionID string `json:"merchantTransactionId"`
			InstrumentResponse    struct {
				Type        string `json:"type"`
				RedirectInfo struct {
					URL    string `json:"url"`
					Method string `json:"method"`
				} `json:"redirectInfo"`
			} `json:"instrumentResponse"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &phonepeResp); err != nil {
		writeError(w, r, fmt.Errorf("failed to parse phonepe response: %w", err))
		return
	}

	redirectURLFromPhonePe := phonepeResp.Data.InstrumentResponse.RedirectInfo.URL
	if redirectURLFromPhonePe == "" {
		writeError(w, r, fmt.Errorf("no redirect url returned by phonepe: %s", string(respBody)))
		return
	}

	writeData(w, http.StatusOK, map[string]any{
		"redirect_url":            redirectURLFromPhonePe,
		"merchant_transaction_id": merchantTransactionID,
		"status":                  phonepeResp.Code,
	})
}


