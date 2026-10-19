package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/roastco/ecommerce/services/order/internal/service"
)

type Config struct {
	ShippingSecret string
}

type shiprocketPayload struct {
	AWB           string `json:"awb"`
	CurrentStatus string `json:"current_status"`
	OrderID       string `json:"order_id"`
	EventID       string `json:"event_id"`
}

func NewHandler(svc *service.Service, cfg Config, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Post("/webhooks/shipping", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}

		if cfg.ShippingSecret != "" {
			sig := r.Header.Get("X-Shiprocket-Signature")
			mac := hmac.New(sha256.New, []byte(cfg.ShippingSecret))
			mac.Write(body)
			expected := hex.EncodeToString(mac.Sum(nil))
			if sig != "" && !hmac.Equal([]byte(sig), []byte(expected)) {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
		}

		var payload shiprocketPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		eventID := payload.EventID
		if eventID == "" {
			eventID = payload.AWB + ":" + payload.CurrentStatus
			if payload.AWB == "" {
				eventID = uuid.NewString()
			}
		}

		if err := svc.HandleShippingWebhook(r.Context(), "shiprocket", eventID, payload.CurrentStatus, payload.AWB, body); err != nil {
			log.Error("shipping webhook error", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	return r
}
