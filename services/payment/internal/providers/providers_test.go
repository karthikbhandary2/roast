package providers

import (
	"context"
	"testing"
)

func TestRazorpayCharge(t *testing.T) {
	prov := NewRazorpay("key", "secret")
	ctx := context.Background()

	t.Run("INR Card Charge Succeeded", func(t *testing.T) {
		res, err := prov.Charge(ctx, ChargeInput{
			SubOrderID:    "sub-1",
			AmountMinor:   50000,
			Currency:      "INR",
			PaymentMethod: "card",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != "SUCCEEDED" {
			t.Errorf("expected SUCCEEDED, got %s", res.Status)
		}
	})

	t.Run("INR UPI Charge Pending", func(t *testing.T) {
		res, err := prov.Charge(ctx, ChargeInput{
			SubOrderID:    "sub-1",
			AmountMinor:   50000,
			Currency:      "INR",
			PaymentMethod: "upi",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != "PENDING" {
			t.Errorf("expected PENDING, got %s", res.Status)
		}
	})

	t.Run("Non-INR Fails", func(t *testing.T) {
		res, err := prov.Charge(ctx, ChargeInput{
			SubOrderID:    "sub-1",
			AmountMinor:   50000,
			Currency:      "USD",
			PaymentMethod: "card",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != "FAILED" {
			t.Errorf("expected FAILED, got %s", res.Status)
		}
	})
}

func TestStripeCharge(t *testing.T) {
	prov := NewStripe("secret")
	ctx := context.Background()

	t.Run("USD Card Succeeded", func(t *testing.T) {
		res, err := prov.Charge(ctx, ChargeInput{
			SubOrderID:    "sub-2",
			AmountMinor:   2500,
			Currency:      "USD",
			PaymentMethod: "card",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != "SUCCEEDED" {
			t.Errorf("expected SUCCEEDED, got %s", res.Status)
		}
	})

	t.Run("INR Fails on Stripe", func(t *testing.T) {
		res, err := prov.Charge(ctx, ChargeInput{
			SubOrderID:    "sub-2",
			AmountMinor:   2500,
			Currency:      "INR",
			PaymentMethod: "card",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Status != "FAILED" {
			t.Errorf("expected FAILED, got %s", res.Status)
		}
	})
}
