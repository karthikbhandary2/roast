package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"

	userv1 "github.com/roastco/ecommerce/gen/user/v1"
	"github.com/roastco/ecommerce/pkg/idempotency"
	"github.com/roastco/ecommerce/pkg/middleware"
	"github.com/roastco/ecommerce/pkg/money"
	"github.com/roastco/ecommerce/services/payment/internal/providers"
	"github.com/roastco/ecommerce/services/payment/internal/repository"
)

var (
	ErrNotFound           = repository.ErrNotFound
	ErrAlreadyExists      = repository.ErrAlreadyExists
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrPermissionDenied   = errors.New("permission denied")
	ErrFailedPrecondition = errors.New("failed precondition")
)

func invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidArgument, msg) }

type UserClient interface {
	GetSellerProfile(ctx context.Context, in *userv1.GetSellerProfileRequest, opts ...grpc.CallOption) (*userv1.SellerProfile, error)
}

type Publisher interface {
	Publish(ctx context.Context, aggregateID, eventType string, payload any) error
}

type NoopPublisher struct{}

func (NoopPublisher) Publish(context.Context, string, string, any) error { return nil }

type SellerProfileInfo struct {
	Provider         string
	PayoutAccountRef string
	CommissionRate   float64
	CachedAt         time.Time
}

type Service struct {
	repo       *repository.Repository
	pub        Publisher
	userClient UserClient
	providers  map[string]providers.Provider
	breaker    *middleware.Breaker
	log        *slog.Logger

	mu    sync.RWMutex
	cache map[string]SellerProfileInfo
}

func New(
	repo *repository.Repository,
	pub Publisher,
	userClient UserClient,
	provs map[string]providers.Provider,
	log *slog.Logger,
) *Service {
	return &Service{
		repo:       repo,
		pub:        pub,
		userClient: userClient,
		providers:  provs,
		breaker:    middleware.NewBreaker("user-service"),
		log:        log,
		cache:      make(map[string]SellerProfileInfo),
	}
}

func (s *Service) resolveSellerProfile(ctx context.Context, sellerID string) (*SellerProfileInfo, error) {
	s.mu.RLock()
	info, ok := s.cache[sellerID]
	s.mu.RUnlock()
	if ok && time.Since(info.CachedAt) < 5*time.Minute {
		return &info, nil
	}

	if s.userClient == nil {
		return &SellerProfileInfo{
			Provider:       "stripe",
			CommissionRate: 0.10,
		}, nil
	}

	profile, err := middleware.Do(ctx, s.breaker, func(c context.Context) (*userv1.SellerProfile, error) {
		return s.userClient.GetSellerProfile(c, &userv1.GetSellerProfileRequest{UserId: sellerID})
	})
	if err != nil {
		if ok {
			// Degraded mode: fallback to stale cache
			return &info, nil
		}
		return nil, fmt.Errorf("service: resolve seller profile: %w", err)
	}
	info = SellerProfileInfo{
		Provider:         money.PaymentProviderToString(profile.GetProvider()),
		PayoutAccountRef: profile.GetPayoutAccountRef(),
		CommissionRate:   profile.GetCommissionRate(),
		CachedAt:         time.Now(),
	}
	if info.CommissionRate <= 0 {
		info.CommissionRate = 0.10
	}

	s.mu.Lock()
	s.cache[sellerID] = info
	s.mu.Unlock()

	return &info, nil
}

type ChargeResultPayload struct {
	ChargeID         string `json:"charge_id"`
	Status           string `json:"status"` // SUCCEEDED | PENDING | FAILED
	PlatformFeeMinor int64  `json:"platform_fee_minor"`
	SellerShareMinor int64  `json:"seller_share_minor"`
	FailureReason    string `json:"failure_reason,omitempty"`
	Provider         string `json:"provider"`
}

func (s *Service) ChargePayment(
	ctx context.Context,
	idempotencyKeyStr string,
	subOrderID, buyerID, sellerID string,
	currency string, amountMinor int64,
	paymentMethodToken, paymentMethod string,
) (*ChargeResultPayload, error) {
	if idempotencyKeyStr == "" {
		return nil, invalid("missing idempotency key")
	}
	if amountMinor <= 0 {
		return nil, invalid("amount must be > 0")
	}

	profile, err := s.resolveSellerProfile(ctx, sellerID)
	if err != nil {
		return nil, err
	}

	if !money.ValidateCurrencyForProvider(currency, profile.Provider) {
		return nil, fmt.Errorf("%w: provider %s does not accept currency %s", ErrFailedPrecondition, profile.Provider, currency)
	}

	prov, ok := s.providers[profile.Provider]
	if !ok {
		return nil, fmt.Errorf("unsupported provider %s", profile.Provider)
	}

	platformFee, sellerShare, err := money.Split(amountMinor, profile.CommissionRate)
	if err != nil {
		return nil, err
	}

	res, err := idempotency.Run(ctx, s.repo.Pool(), "idempotency_keys", idempotencyKeyStr, "charge",
		func(ctx context.Context, tx pgx.Tx) (ChargeResultPayload, error) {
			chargeRes, err := prov.Charge(ctx, providers.ChargeInput{
				SubOrderID:             subOrderID,
				AmountMinor:            amountMinor,
				Currency:               currency,
				PaymentMethod:          paymentMethod,
				PaymentMethodToken:     paymentMethodToken,
				SellerPayoutAccountRef: profile.PayoutAccountRef,
			})
			if err != nil {
				return ChargeResultPayload{}, err
			}

			if chargeRes.Status == "FAILED" {
				return ChargeResultPayload{
					Status:        "FAILED",
					FailureReason: chargeRes.FailureReason,
					Provider:      prov.Name(),
				}, nil
			}

			statusStr := "succeeded"
			if chargeRes.Status == "PENDING" {
				statusStr = "pending"
			}

			c, err := s.repo.InsertCharge(ctx, tx, repository.Charge{
				SubOrderID:       subOrderID,
				BuyerID:          buyerID,
				SellerID:         sellerID,
				Provider:         prov.Name(),
				Currency:         currency,
				AmountMinor:      amountMinor,
				PlatformFeeMinor: platformFee,
				SellerShareMinor: sellerShare,
				Status:           statusStr,
				ProviderChargeID: chargeRes.ProviderChargeID,
			})
			if err != nil {
				return ChargeResultPayload{}, err
			}

			// Ledger: credit seller_share to seller, platform_fee to platform pseudo-seller
			if err := s.repo.InsertLedgerEntry(ctx, tx, repository.LedgerEntry{
				SellerID:    sellerID,
				SubOrderID:  subOrderID,
				EntryType:   "sale",
				AmountMinor: sellerShare,
			}); err != nil {
				return ChargeResultPayload{}, err
			}

			if err := s.repo.InsertLedgerEntry(ctx, tx, repository.LedgerEntry{
				SellerID:    repository.PlatformSellerID,
				SubOrderID:  subOrderID,
				EntryType:   "platform_fee",
				AmountMinor: platformFee,
			}); err != nil {
				return ChargeResultPayload{}, err
			}

			if _, err := s.repo.RecomputeSellerBalance(ctx, tx, sellerID, currency); err != nil {
				return ChargeResultPayload{}, err
			}

			return ChargeResultPayload{
				ChargeID:         c.ID,
				Status:           chargeRes.Status,
				PlatformFeeMinor: platformFee,
				SellerShareMinor: sellerShare,
				Provider:         prov.Name(),
			}, nil
		},
	)
	if err != nil {
		return nil, err
	}

	if res.Status == "SUCCEEDED" {
		_ = s.pub.Publish(ctx, subOrderID, "payment.charge_succeeded", map[string]string{
			"sub_order_id": subOrderID,
			"charge_id":    res.ChargeID,
		})
	}
	return &res, nil
}

func (s *Service) RefundPayment(ctx context.Context, idempotencyKeyStr, chargeID, reason string) error {
	if idempotencyKeyStr == "" {
		return invalid("missing idempotency key")
	}

	_, err := idempotency.Run(ctx, s.repo.Pool(), "idempotency_keys", idempotencyKeyStr, "refund",
		func(ctx context.Context, tx pgx.Tx) (bool, error) {
			c, err := s.repo.GetCharge(ctx, tx, chargeID)
			if err != nil {
				return false, err
			}
			if c.Status == "refunded" {
				return true, nil
			}

			prov, ok := s.providers[c.Provider]
			if !ok {
				return false, fmt.Errorf("unsupported provider %s", c.Provider)
			}

			refRes, err := prov.Refund(ctx, providers.RefundInput{
				ProviderChargeID: c.ProviderChargeID,
				AmountMinor:      c.AmountMinor,
				Currency:         c.Currency,
				Reason:           reason,
			})
			if err != nil {
				return false, err
			}

			_, err = s.repo.InsertRefund(ctx, tx, repository.Refund{
				ChargeID:         c.ID,
				Provider:         c.Provider,
				Currency:         c.Currency,
				AmountMinor:      c.AmountMinor,
				Reason:           reason,
				Status:           "succeeded",
				ProviderRefundID: refRes.ProviderRefundID,
			})
			if err != nil {
				return false, err
			}

			if _, err := s.repo.UpdateChargeStatus(ctx, tx, c.ID, "refunded", ""); err != nil {
				return false, err
			}

			// Negative ledger entries reversing sale and platform fee
			if err := s.repo.InsertLedgerEntry(ctx, tx, repository.LedgerEntry{
				SellerID:    c.SellerID,
				SubOrderID:  c.SubOrderID,
				EntryType:   "refund",
				AmountMinor: -c.SellerShareMinor,
			}); err != nil {
				return false, err
			}

			if err := s.repo.InsertLedgerEntry(ctx, tx, repository.LedgerEntry{
				SellerID:    repository.PlatformSellerID,
				SubOrderID:  c.SubOrderID,
				EntryType:   "platform_fee",
				AmountMinor: -c.PlatformFeeMinor,
			}); err != nil {
				return false, err
			}

			if _, err := s.repo.RecomputeSellerBalance(ctx, tx, c.SellerID, c.Currency); err != nil {
				return false, err
			}

			return true, nil
		},
	)
	if err != nil {
		return err
	}

	_ = s.pub.Publish(ctx, chargeID, "payment.refunded", map[string]string{
		"charge_id": chargeID,
		"reason":    reason,
	})
	return nil
}

func (s *Service) GetSellerBalance(ctx context.Context, sellerID string) (*repository.SellerBalance, error) {
	return s.repo.GetSellerBalance(ctx, s.repo.DB(), sellerID)
}

func (s *Service) RequestPayout(ctx context.Context, sellerID string) (*repository.Payout, error) {
	b, err := s.repo.GetSellerBalance(ctx, s.repo.DB(), sellerID)
	if err != nil {
		return nil, err
	}
	if b.AvailableMinor <= 0 {
		return nil, fmt.Errorf("%w: no available balance to pay out", ErrFailedPrecondition)
	}

	profile, err := s.resolveSellerProfile(ctx, sellerID)
	if err != nil {
		return nil, err
	}
	prov, ok := s.providers[profile.Provider]
	if !ok {
		return nil, fmt.Errorf("unsupported provider %s", profile.Provider)
	}

	payoutRes, err := prov.Payout(ctx, providers.PayoutInput{
		SellerPayoutAccountRef: profile.PayoutAccountRef,
		AmountMinor:            b.AvailableMinor,
		Currency:               b.Currency,
		Method:                 "auto",
	})
	if err != nil {
		return nil, fmt.Errorf("service: provider payout: %w", err)
	}

	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	p, err := s.repo.InsertPayout(ctx, tx, repository.Payout{
		SellerID:         sellerID,
		Provider:         prov.Name(),
		Currency:         b.Currency,
		AmountMinor:      b.AvailableMinor,
		Method:           "standard",
		Status:           payoutRes.Status,
		ProviderPayoutID: payoutRes.ProviderPayoutID,
	})
	if err != nil {
		return nil, err
	}

	// Negative ledger entry for payout
	if err := s.repo.InsertLedgerEntry(ctx, tx, repository.LedgerEntry{
		SellerID:    sellerID,
		PayoutID:    p.ID,
		EntryType:   "payout",
		AmountMinor: -b.AvailableMinor,
	}); err != nil {
		return nil, err
	}

	if _, err := s.repo.RecomputeSellerBalance(ctx, tx, sellerID, b.Currency); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit payout: %w", err)
	}

	_ = s.pub.Publish(ctx, sellerID, "payment.payout_paid", map[string]any{
		"seller_id": sellerID,
		"payout_id": p.ID,
		"amount":    b.AvailableMinor,
	})

	return p, nil
}

func (s *Service) ListPayouts(ctx context.Context, sellerID string, pageSize int, pageToken string) ([]*repository.Payout, string, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var after *repository.Cursor
	if pageToken != "" {
		c, err := decodeCursor(pageToken)
		if err != nil {
			return nil, "", err
		}
		after = c
	}

	rows, err := s.repo.ListPayouts(ctx, s.repo.DB(), sellerID, after, pageSize+1)
	if err != nil {
		return nil, "", err
	}

	next := ""
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		next = encodeCursor(repository.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return rows, next, nil
}

// ------------------------------------------------------------- webhooks & events

func (s *Service) HandleWebhook(ctx context.Context, providerName, headerSig string, body []byte, secret string) error {
	prov, ok := s.providers[providerName]
	if !ok {
		return fmt.Errorf("unsupported provider %s", providerName)
	}

	if err := prov.VerifyWebhookSignature(headerSig, body, secret); err != nil {
		return ErrPermissionDenied
	}

	event, err := prov.ParseWebhookEvent(body)
	if err != nil {
		return err
	}

	seen, err := s.repo.CheckWebhookEvent(ctx, s.repo.DB(), providerName, event.EventID)
	if err != nil {
		return err
	}
	if seen {
		return nil
	}

	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := s.repo.InsertWebhookEvent(ctx, tx, providerName, event.EventID, event.EventType, body); err != nil {
		return err
	}

	statusStr := "succeeded"
	if event.Status == "FAILED" {
		statusStr = "failed"
	}

	var c *repository.Charge
	if event.ProviderChargeID != "" {
		ch, err := s.repo.UpdateChargeStatusByProviderID(ctx, tx, providerName, event.ProviderChargeID, statusStr)
		if err == nil {
			c = ch
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	subOrderID := event.SubOrderID
	if subOrderID == "" && c != nil {
		subOrderID = c.SubOrderID
	}

	if subOrderID != "" {
		if event.Status == "SUCCEEDED" {
			_ = s.pub.Publish(ctx, subOrderID, "payment.charge_succeeded", map[string]string{
				"sub_order_id": subOrderID,
				"charge_id":    event.ProviderChargeID,
			})
		} else {
			_ = s.pub.Publish(ctx, subOrderID, "payment.charge_failed", map[string]string{
				"sub_order_id": subOrderID,
				"charge_id":    event.ProviderChargeID,
			})
		}
	}

	return nil
}

func (s *Service) HandleSubOrderConfirmed(ctx context.Context, subOrderID string) error {
	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := s.repo.ConfirmSubOrderEarnings(ctx, tx, subOrderID); err != nil {
		return err
	}

	c, err := s.repo.GetChargeBySubOrder(ctx, tx, subOrderID)
	if errors.Is(err, repository.ErrNotFound) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}

	if _, err := s.repo.RecomputeSellerBalance(ctx, tx, c.SellerID, c.Currency); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ------------------------------------------------------------- cursor helpers

func encodeCursor(c repository.Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt.Format(time.RFC3339Nano) + "|" + c.ID))
}

func decodeCursor(token string) (*repository.Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, invalid("page_token is not valid")
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, invalid("page_token is not valid")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, invalid("page_token is not valid")
	}
	return &repository.Cursor{CreatedAt: t, ID: id}, nil
}
