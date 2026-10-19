package webhook

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/roastco/ecommerce/services/payment/internal/service"
)

type Config struct {
	RazorpaySecret string
	StripeSecret   string
	PhonePeSecret  string
}

func NewHandler(svc *service.Service, cfg Config, log *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Post("/webhooks/phonepe", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		sig := r.Header.Get("X-VERIFY")

		if err := svc.HandleWebhook(r.Context(), "phonepe", sig, body, cfg.PhonePeSecret); err != nil {
			if errors.Is(err, service.ErrPermissionDenied) {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
			log.Error("phonepe webhook failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	r.Post("/webhooks/razorpay", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		sig := r.Header.Get("X-Razorpay-Signature")

		if err := svc.HandleWebhook(r.Context(), "razorpay", sig, body, cfg.RazorpaySecret); err != nil {
			if errors.Is(err, service.ErrPermissionDenied) {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
			log.Error("razorpay webhook failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	r.Post("/webhooks/stripe", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		sig := r.Header.Get("Stripe-Signature")

		if err := svc.HandleWebhook(r.Context(), "stripe", sig, body, cfg.StripeSecret); err != nil {
			if errors.Is(err, service.ErrPermissionDenied) {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
			log.Error("stripe webhook failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	return r
}
