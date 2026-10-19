package money

import (
	"errors"
	"fmt"
	"math"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
)

var (
	ErrUnsupportedCurrency = errors.New("unsupported currency")
	ErrCurrencyMismatch    = errors.New("currency mismatch")
	ErrNegativeAmount      = errors.New("negative amount")
)

// Supported currencies matching common.proto and Postgres enums.
const (
	USD = "USD"
	INR = "INR"
	GBP = "GBP"
	JPY = "JPY"
)

// ScaleFactor returns the number of minor units in one major unit.
// JPY has no minor unit (scale factor = 1).
// USD, INR, GBP use cents/paise/pence (scale factor = 100).
func ScaleFactor(currency string) (int64, error) {
	switch currency {
	case JPY:
		return 1, nil
	case USD, INR, GBP:
		return 100, nil
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnsupportedCurrency, currency)
	}
}

// DecimalPlaces returns the number of decimal digits for display.
func DecimalPlaces(currency string) int {
	if currency == JPY {
		return 0
	}
	return 2
}

// Split calculates platform fee and seller share based on a commission rate (e.g. 0.10 for 10%).
// Platform fee is rounded to nearest minor unit; seller share is the remainder.
// Invariant: platformFee + sellerShare == amountMinor.
func Split(amountMinor int64, commissionRate float64) (platformFee int64, sellerShare int64, err error) {
	if amountMinor < 0 {
		return 0, 0, ErrNegativeAmount
	}
	if commissionRate < 0 || commissionRate > 1 {
		return 0, 0, errors.New("commission rate must be between 0.0 and 1.0")
	}

	feeFloat := float64(amountMinor) * commissionRate
	platformFee = int64(math.Round(feeFloat))
	if platformFee > amountMinor {
		platformFee = amountMinor
	}
	sellerShare = amountMinor - platformFee
	return platformFee, sellerShare, nil
}

// ValidateCurrencyForProvider enforces provider-currency constraints:
// Razorpay -> INR only.
// Stripe -> USD, GBP, JPY.
func ValidateCurrencyForProvider(currency string, provider string) bool {
	switch provider {
	case "phonepe", "razorpay":
		return currency == INR
	case "stripe":
		return currency == USD || currency == GBP || currency == JPY
	default:
		return false
	}
}

// Format returns a human-readable representation of money.
func Format(currency string, amountMinor int64) string {
	switch currency {
	case JPY:
		return fmt.Sprintf("¥%d", amountMinor)
	case USD:
		return fmt.Sprintf("$%.2f", float64(amountMinor)/100.0)
	case INR:
		return fmt.Sprintf("₹%.2f", float64(amountMinor)/100.0)
	case GBP:
		return fmt.Sprintf("£%.2f", float64(amountMinor)/100.0)
	default:
		return fmt.Sprintf("%s %d", currency, amountMinor)
	}
}

// ProtoToMoney converts a commonv1.Money proto to string currency and minor amount.
func ProtoToMoney(m *commonv1.Money) (string, int64, error) {
	if m == nil {
		return "", 0, errors.New("nil money proto")
	}
	curr := CurrencyCodeToString(m.GetCurrencyCode())
	if curr == "" {
		return "", 0, ErrUnsupportedCurrency
	}
	return curr, m.GetAmountMinor(), nil
}

// MoneyToProto converts string currency and minor amount into commonv1.Money proto.
func MoneyToProto(currency string, amountMinor int64) *commonv1.Money {
	return &commonv1.Money{
		CurrencyCode: StringToCurrencyCode(currency),
		AmountMinor:  amountMinor,
	}
}

// CurrencyCodeToString converts commonv1.CurrencyCode enum to string.
func CurrencyCodeToString(c commonv1.CurrencyCode) string {
	switch c {
	case commonv1.CurrencyCode_CURRENCY_CODE_USD:
		return USD
	case commonv1.CurrencyCode_CURRENCY_CODE_INR:
		return INR
	case commonv1.CurrencyCode_CURRENCY_CODE_GBP:
		return GBP
	case commonv1.CurrencyCode_CURRENCY_CODE_JPY:
		return JPY
	default:
		return ""
	}
}

// StringToCurrencyCode converts string to commonv1.CurrencyCode enum.
func StringToCurrencyCode(s string) commonv1.CurrencyCode {
	switch s {
	case USD:
		return commonv1.CurrencyCode_CURRENCY_CODE_USD
	case INR:
		return commonv1.CurrencyCode_CURRENCY_CODE_INR
	case GBP:
		return commonv1.CurrencyCode_CURRENCY_CODE_GBP
	case JPY:
		return commonv1.CurrencyCode_CURRENCY_CODE_JPY
	default:
		return commonv1.CurrencyCode_CURRENCY_CODE_UNSPECIFIED
	}
}

// PaymentProviderToString converts commonv1.PaymentProvider to string.
func PaymentProviderToString(p commonv1.PaymentProvider) string {
	switch p {
	case commonv1.PaymentProvider_PAYMENT_PROVIDER_RAZORPAY:
		return "phonepe"
	case commonv1.PaymentProvider_PAYMENT_PROVIDER_STRIPE:
		return "stripe"
	default:
		return ""
	}
}

// StringToPaymentProvider converts string to commonv1.PaymentProvider.
func StringToPaymentProvider(s string) commonv1.PaymentProvider {
	switch s {
	case "phonepe", "razorpay":
		return commonv1.PaymentProvider_PAYMENT_PROVIDER_RAZORPAY
	case "stripe":
		return commonv1.PaymentProvider_PAYMENT_PROVIDER_STRIPE
	default:
		return commonv1.PaymentProvider_PAYMENT_PROVIDER_UNSPECIFIED
	}
}
