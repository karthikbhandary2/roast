package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

const (
	PlatformSellerID      = "00000000-0000-0000-0000-000000000001"
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgInvalidText         = "22P02"
)

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }
func (r *Repository) Pool() *pgxpool.Pool { return r.pool }
func (r *Repository) DB() Querier         { return r.pool }

type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// ------------------------------------------------------------- charges

type Charge struct {
	ID               string
	SubOrderID       string
	BuyerID          string
	SellerID         string
	Provider         string // "razorpay" | "stripe"
	Currency         string
	AmountMinor      int64
	PlatformFeeMinor int64
	SellerShareMinor int64
	Status           string // "pending" | "succeeded" | "failed" | "refunded"
	ProviderChargeID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

const chargeCols = `id::text, sub_order_id::text, buyer_id::text, seller_id::text, provider::text,
	amount_currency::text, amount_amount_minor, platform_fee_amount_minor, seller_share_amount_minor,
	status::text, COALESCE(provider_charge_id, ''), created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanCharge(row scanner) (*Charge, error) {
	var c Charge
	err := row.Scan(&c.ID, &c.SubOrderID, &c.BuyerID, &c.SellerID, &c.Provider,
		&c.Currency, &c.AmountMinor, &c.PlatformFeeMinor, &c.SellerShareMinor,
		&c.Status, &c.ProviderChargeID, &c.CreatedAt, &c.UpdatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows), pgCode(err) == pgInvalidText:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: scan charge: %w", err)
	}
	return &c, nil
}

func (r *Repository) InsertCharge(ctx context.Context, q Querier, c Charge) (*Charge, error) {
	return scanCharge(q.QueryRow(ctx,
		`INSERT INTO charges (sub_order_id, buyer_id, seller_id, provider, amount_currency,
		                     amount_amount_minor, platform_fee_amount_minor, seller_share_amount_minor,
		                     status, provider_charge_id)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::payment_provider, $5::currency_code,
		         $6, $7, $8, $9::charge_status, NULLIF($10, ''))
		 RETURNING `+chargeCols,
		c.SubOrderID, c.BuyerID, c.SellerID, c.Provider, c.Currency,
		c.AmountMinor, c.PlatformFeeMinor, c.SellerShareMinor,
		c.Status, c.ProviderChargeID))
}

func (r *Repository) GetCharge(ctx context.Context, q Querier, id string) (*Charge, error) {
	return scanCharge(q.QueryRow(ctx, `SELECT `+chargeCols+` FROM charges WHERE id = $1::uuid`, id))
}

func (r *Repository) GetChargeBySubOrder(ctx context.Context, q Querier, subOrderID string) (*Charge, error) {
	return scanCharge(q.QueryRow(ctx, `SELECT `+chargeCols+` FROM charges WHERE sub_order_id = $1::uuid`, subOrderID))
}

func (r *Repository) UpdateChargeStatus(ctx context.Context, q Querier, id, status, providerChargeID string) (*Charge, error) {
	return scanCharge(q.QueryRow(ctx,
		`UPDATE charges
		 SET status = $2::charge_status,
		     provider_charge_id = COALESCE(NULLIF($3, ''), provider_charge_id),
		     updated_at = now()
		 WHERE id = $1::uuid
		 RETURNING `+chargeCols,
		id, status, providerChargeID))
}

func (r *Repository) UpdateChargeStatusByProviderID(ctx context.Context, q Querier, provider, providerChargeID, status string) (*Charge, error) {
	return scanCharge(q.QueryRow(ctx,
		`UPDATE charges
		 SET status = $3::charge_status, updated_at = now()
		 WHERE provider = $1::payment_provider AND provider_charge_id = $2
		 RETURNING `+chargeCols,
		provider, providerChargeID, status))
}

// ------------------------------------------------------------- refunds

type Refund struct {
	ID               string
	ChargeID         string
	Provider         string
	Currency         string
	AmountMinor      int64
	Reason           string
	Status           string
	ProviderRefundID string
	CreatedAt        time.Time
}

func (r *Repository) InsertRefund(ctx context.Context, q Querier, ref Refund) (*Refund, error) {
	var out Refund
	err := q.QueryRow(ctx,
		`INSERT INTO refunds (charge_id, provider, amount_currency, amount_amount_minor, reason, status, provider_refund_id)
		 VALUES ($1::uuid, $2::payment_provider, $3::currency_code, $4, $5, $6, NULLIF($7, ''))
		 RETURNING id::text, charge_id::text, provider::text, amount_currency::text, amount_amount_minor,
		           COALESCE(reason, ''), status, COALESCE(provider_refund_id, ''), created_at`,
		ref.ChargeID, ref.Provider, ref.Currency, ref.AmountMinor, ref.Reason, ref.Status, ref.ProviderRefundID).
		Scan(&out.ID, &out.ChargeID, &out.Provider, &out.Currency, &out.AmountMinor,
			&out.Reason, &out.Status, &out.ProviderRefundID, &out.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("repository: insert refund: %w", err)
	}
	return &out, nil
}

// ------------------------------------------------------------- ledger & balance

type LedgerEntry struct {
	ID          int64
	SellerID    string
	SubOrderID  string
	PayoutID    string
	EntryType   string // 'sale' | 'platform_fee' | 'refund' | 'payout'
	AmountMinor int64
	CreatedAt   time.Time
}

func (r *Repository) InsertLedgerEntry(ctx context.Context, q Querier, entry LedgerEntry) error {
	_, err := q.Exec(ctx,
		`INSERT INTO ledger_entries (seller_id, sub_order_id, payout_id, entry_type, amount_amount_minor)
		 VALUES ($1::uuid, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4::ledger_entry_type, $5)`,
		entry.SellerID, entry.SubOrderID, entry.PayoutID, entry.EntryType, entry.AmountMinor)
	if err != nil {
		return fmt.Errorf("repository: insert ledger entry: %w", err)
	}
	return nil
}

func (r *Repository) ConfirmSubOrderEarnings(ctx context.Context, q Querier, subOrderID string) error {
	_, err := q.Exec(ctx,
		`INSERT INTO sub_order_earnings_confirmed (sub_order_id)
		 VALUES ($1::uuid)
		 ON CONFLICT DO NOTHING`, subOrderID)
	if err != nil {
		return fmt.Errorf("repository: confirm sub order earnings: %w", err)
	}
	return nil
}

type SellerBalance struct {
	SellerID       string
	AvailableMinor int64
	PendingMinor   int64
	Currency       string
	UpdatedAt      time.Time
}

func (r *Repository) GetSellerBalance(ctx context.Context, q Querier, sellerID string) (*SellerBalance, error) {
	var b SellerBalance
	err := q.QueryRow(ctx,
		`SELECT seller_id::text, available_amount_minor, pending_amount_minor, currency::text, updated_at
		 FROM seller_balances WHERE seller_id = $1::uuid`, sellerID).
		Scan(&b.SellerID, &b.AvailableMinor, &b.PendingMinor, &b.Currency, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) || pgCode(err) == pgInvalidText {
		return &SellerBalance{
			SellerID:       sellerID,
			AvailableMinor: 0,
			PendingMinor:   0,
			Currency:       "USD",
			UpdatedAt:      time.Now(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("repository: get seller balance: %w", err)
	}
	return &b, nil
}

func (r *Repository) RecomputeSellerBalance(ctx context.Context, q Querier, sellerID string, currency string) (*SellerBalance, error) {
	// available = confirmed sales + all refunds + all payouts
	// pending = unconfirmed sales
	sql := `
		WITH confirmed_sales AS (
			SELECT COALESCE(SUM(l.amount_amount_minor), 0) AS amt
			FROM ledger_entries l
			JOIN sub_order_earnings_confirmed soec ON soec.sub_order_id = l.sub_order_id
			WHERE l.seller_id = $1::uuid AND l.entry_type = 'sale'
		),
		other_entries AS (
			SELECT COALESCE(SUM(l.amount_amount_minor), 0) AS amt
			FROM ledger_entries l
			WHERE l.seller_id = $1::uuid AND l.entry_type IN ('refund', 'payout')
		),
		pending_sales AS (
			SELECT COALESCE(SUM(l.amount_amount_minor), 0) AS amt
			FROM ledger_entries l
			WHERE l.seller_id = $1::uuid AND l.entry_type = 'sale'
			  AND l.sub_order_id IS NOT NULL
			  AND NOT EXISTS (
			      SELECT 1 FROM sub_order_earnings_confirmed soec WHERE soec.sub_order_id = l.sub_order_id
			  )
		)
		INSERT INTO seller_balances (seller_id, available_amount_minor, pending_amount_minor, currency, updated_at)
		VALUES (
			$1::uuid,
			(SELECT amt FROM confirmed_sales) + (SELECT amt FROM other_entries),
			(SELECT amt FROM pending_sales),
			$2::currency_code,
			now()
		)
		ON CONFLICT (seller_id) DO UPDATE SET
			available_amount_minor = EXCLUDED.available_amount_minor,
			pending_amount_minor = EXCLUDED.pending_amount_minor,
			currency = EXCLUDED.currency,
			updated_at = now()
		RETURNING seller_id::text, available_amount_minor, pending_amount_minor, currency::text, updated_at`

	var b SellerBalance
	err := q.QueryRow(ctx, sql, sellerID, currency).
		Scan(&b.SellerID, &b.AvailableMinor, &b.PendingMinor, &b.Currency, &b.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("repository: recompute seller balance: %w", err)
	}
	return &b, nil
}

// ------------------------------------------------------------- payouts

type Payout struct {
	ID               string
	SellerID         string
	Provider         string
	Currency         string
	AmountMinor      int64
	Method           string
	Status           string // "pending" | "paid" | "failed"
	ProviderPayoutID string
	CreatedAt        time.Time
}

func (r *Repository) InsertPayout(ctx context.Context, q Querier, p Payout) (*Payout, error) {
	var out Payout
	err := q.QueryRow(ctx,
		`INSERT INTO payouts (seller_id, provider, amount_currency, amount_amount_minor, method, status, provider_payout_id)
		 VALUES ($1::uuid, $2::payment_provider, $3::currency_code, $4, $5, $6, NULLIF($7, ''))
		 RETURNING id::text, seller_id::text, provider::text, amount_currency::text, amount_amount_minor,
		           method, status, COALESCE(provider_payout_id, ''), created_at`,
		p.SellerID, p.Provider, p.Currency, p.AmountMinor, p.Method, p.Status, p.ProviderPayoutID).
		Scan(&out.ID, &out.SellerID, &out.Provider, &out.Currency, &out.AmountMinor,
			&out.Method, &out.Status, &out.ProviderPayoutID, &out.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("repository: insert payout: %w", err)
	}
	return &out, nil
}

func (r *Repository) ListPayouts(ctx context.Context, q Querier, sellerID string, after *Cursor, limit int) ([]*Payout, error) {
	sql := `SELECT id::text, seller_id::text, provider::text, amount_currency::text, amount_amount_minor,
	               method, status, COALESCE(provider_payout_id, ''), created_at
	        FROM payouts WHERE seller_id = $1::uuid`
	args := []any{sellerID}
	if after != nil {
		args = append(args, after.CreatedAt, after.ID)
		sql += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args))
	}
	args = append(args, limit)
	sql += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list payouts: %w", err)
	}
	defer rows.Close()

	var out []*Payout
	for rows.Next() {
		var p Payout
		if err := rows.Scan(&p.ID, &p.SellerID, &p.Provider, &p.Currency, &p.AmountMinor,
			&p.Method, &p.Status, &p.ProviderPayoutID, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------- webhook events

func (r *Repository) CheckWebhookEvent(ctx context.Context, q Querier, provider, providerEventID string) (bool, error) {
	var dummy int
	err := q.QueryRow(ctx,
		`SELECT 1 FROM webhook_events WHERE provider = $1::payment_provider AND provider_event_id = $2`,
		provider, providerEventID).Scan(&dummy)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) InsertWebhookEvent(ctx context.Context, q Querier, provider, providerEventID, eventType string, payload []byte) error {
	_, err := q.Exec(ctx,
		`INSERT INTO webhook_events (provider, provider_event_id, event_type, payload, processed_at)
		 VALUES ($1::payment_provider, $2, $3, $4, now())
		 ON CONFLICT (provider, provider_event_id) DO NOTHING`,
		provider, providerEventID, eventType, payload)
	if err != nil {
		return fmt.Errorf("repository: insert webhook event: %w", err)
	}
	return nil
}
