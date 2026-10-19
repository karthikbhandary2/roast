package money

import (
	"testing"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		name        string
		amountMinor int64
		rate        float64
		wantFee     int64
		wantShare   int64
	}{
		{"10% of 1000", 1000, 0.10, 100, 900},
		{"10% of 999", 999, 0.10, 100, 899},
		{"15% of 5900", 5900, 0.15, 885, 5015},
		{"0% fee", 1000, 0.0, 0, 1000},
		{"100% fee", 1000, 1.0, 1000, 0},
		{"JPY whole yen split", 5000, 0.10, 500, 4500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fee, share, err := Split(tt.amountMinor, tt.rate)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fee != tt.wantFee {
				t.Errorf("got fee %d, want %d", fee, tt.wantFee)
			}
			if share != tt.wantShare {
				t.Errorf("got share %d, want %d", share, tt.wantShare)
			}
			if fee+share != tt.amountMinor {
				t.Errorf("invariant violated: %d + %d != %d", fee, share, tt.amountMinor)
			}
		})
	}
}

func TestValidateCurrencyForProvider(t *testing.T) {
	if !ValidateCurrencyForProvider(INR, "razorpay") {
		t.Errorf("expected INR to be valid for razorpay")
	}
	if ValidateCurrencyForProvider(USD, "razorpay") {
		t.Errorf("expected USD to be invalid for razorpay")
	}
	if !ValidateCurrencyForProvider(USD, "stripe") {
		t.Errorf("expected USD to be valid for stripe")
	}
	if !ValidateCurrencyForProvider(GBP, "stripe") {
		t.Errorf("expected GBP to be valid for stripe")
	}
	if !ValidateCurrencyForProvider(JPY, "stripe") {
		t.Errorf("expected JPY to be valid for stripe")
	}
	if ValidateCurrencyForProvider(INR, "stripe") {
		t.Errorf("expected INR to be invalid for stripe")
	}
}
