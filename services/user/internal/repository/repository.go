// Package repository is the User Service's SQL layer: queries and
// transactions only, no business rules. Rules (who may do what, which status
// transitions are legal) live in the service package; where a transaction
// needs one, it is passed in as a callback.
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
	ErrEmailTaken    = errors.New("email already registered")
	ErrAlreadyExists = errors.New("already exists")
	ErrInvalidState  = errors.New("invalid state transition")
)

// Postgres error codes translated into the sentinel errors above.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgInvalidText         = "22P02" // e.g. a malformed UUID in a ::uuid cast
)

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

type User struct {
	ID               string
	Email            string
	PasswordHash     string
	DisplayName      string
	Roles            []string
	TokensValidAfter *time.Time
	CreatedAt        time.Time
}

type SellerProfile struct {
	UserID           string
	BusinessName     string
	Provider         string // "razorpay" | "stripe"
	PayoutMethod     string
	PayoutAccountRef string
	CommissionRate   float64
	Status           string // "pending_review" | "active" | "suspended" | "rejected"
	CreatedAt        time.Time
}

type SellerApplication struct {
	UserID           string
	BusinessName     string
	Provider         string
	PayoutMethod     string
	PayoutAccountRef string
}

// Cursor is the keyset position for ListSellers: rows are ordered by
// (created_at, user_id), and the next page starts strictly after this pair.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ---------------------------------------------------------------- users

const userSelect = `
SELECT u.id::text, u.email, u.password_hash, u.display_name, u.tokens_valid_after, u.created_at,
       COALESCE(array_agg(r.role::text) FILTER (WHERE r.role IS NOT NULL), '{}')
FROM users u
LEFT JOIN user_roles r ON r.user_id = u.id`

func (r *Repository) queryUser(ctx context.Context, where string, arg any) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx, userSelect+" WHERE "+where+" GROUP BY u.id", arg).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.TokensValidAfter, &u.CreatedAt, &u.Roles)
	switch {
	case errors.Is(err, pgx.ErrNoRows), pgCode(err) == pgInvalidText:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: query user: %w", err)
	}
	return &u, nil
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return r.queryUser(ctx, "u.email = $1", email)
}

func (r *Repository) GetUserByID(ctx context.Context, id string) (*User, error) {
	return r.queryUser(ctx, "u.id = $1::uuid", id)
}

// CreateUser inserts the user and its default "buyer" role atomically.
func (r *Repository) CreateUser(ctx context.Context, email, passwordHash, displayName string) (*User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("repository: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var id string
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ($1, $2, $3) RETURNING id::text`,
		email, passwordHash, displayName).Scan(&id)
	if pgCode(err) == pgUniqueViolation {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, fmt.Errorf("repository: insert user: %w", err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role) VALUES ($1::uuid, 'buyer')`, id); err != nil {
		return nil, fmt.Errorf("repository: insert role: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("repository: commit: %w", err)
	}
	return r.GetUserByID(ctx, id)
}

func (r *Repository) UpdateDisplayName(ctx context.Context, id, name string) (*User, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET display_name = $2, updated_at = now() WHERE id = $1::uuid`, id, name)
	if pgCode(err) == pgInvalidText {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: update display name: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return r.GetUserByID(ctx, id)
}

// ------------------------------------------------------------- sessions

func (r *Repository) StoreRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1::uuid, $2, $3)`,
		userID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("repository: store refresh token: %w", err)
	}
	return nil
}

// RevokeAllSessions is "log out everywhere": every outstanding refresh token
// is revoked and tokens_valid_after is set to now(), so access tokens issued
// before this moment fail the cutoff check (see service.ValidateToken).
func (r *Repository) RevokeAllSessions(ctx context.Context, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID); err != nil {
		if pgCode(err) == pgInvalidText {
			return ErrNotFound
		}
		return fmt.Errorf("repository: revoke refresh tokens: %w", err)
	}
	tag, err := tx.Exec(ctx,
		`UPDATE users SET tokens_valid_after = now(), updated_at = now() WHERE id = $1::uuid`, userID)
	if err != nil {
		return fmt.Errorf("repository: set tokens_valid_after: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// -------------------------------------------------------------- sellers

const sellerCols = `user_id::text, business_name, provider::text, payout_method, payout_account_ref,
	commission_rate::float8, status::text, created_at`

func scanSeller(row pgx.Row) (*SellerProfile, error) {
	var p SellerProfile
	err := row.Scan(&p.UserID, &p.BusinessName, &p.Provider, &p.PayoutMethod, &p.PayoutAccountRef,
		&p.CommissionRate, &p.Status, &p.CreatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows), pgCode(err) == pgInvalidText:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: scan seller: %w", err)
	}
	return &p, nil
}

func (r *Repository) GetSellerProfile(ctx context.Context, userID string) (*SellerProfile, error) {
	return scanSeller(r.pool.QueryRow(ctx,
		`SELECT `+sellerCols+` FROM seller_profiles WHERE user_id = $1::uuid`, userID))
}

// ApplyAsSeller creates a pending_review profile, or re-opens a rejected one.
// Any other existing status is ErrAlreadyExists. The audit row is written in
// the same transaction.
func (r *Repository) ApplyAsSeller(ctx context.Context, a SellerApplication) (*SellerProfile, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("repository: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// oldStatus stays nil when the user has no profile yet.
	var oldStatus *string
	err = tx.QueryRow(ctx,
		`SELECT status::text FROM seller_profiles WHERE user_id = $1::uuid FOR UPDATE`, a.UserID).Scan(&oldStatus)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		if pgCode(err) == pgInvalidText {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("repository: lock seller profile: %w", err)
	}

	reason := "application submitted"
	var row pgx.Row
	switch {
	case oldStatus == nil:
		row = tx.QueryRow(ctx,
			`INSERT INTO seller_profiles (user_id, business_name, provider, payout_method, payout_account_ref)
			 VALUES ($1::uuid, $2, $3::payment_provider, $4, $5)
			 RETURNING `+sellerCols,
			a.UserID, a.BusinessName, a.Provider, a.PayoutMethod, a.PayoutAccountRef)
	case *oldStatus == "rejected":
		reason = "reapplication after rejection"
		row = tx.QueryRow(ctx,
			`UPDATE seller_profiles
			 SET business_name = $2, provider = $3::payment_provider, payout_method = $4,
			     payout_account_ref = $5, status = 'pending_review', updated_at = now()
			 WHERE user_id = $1::uuid
			 RETURNING `+sellerCols,
			a.UserID, a.BusinessName, a.Provider, a.PayoutMethod, a.PayoutAccountRef)
	default:
		return nil, ErrAlreadyExists
	}

	profile, err := scanSeller(row)
	switch {
	case pgCode(err) == pgUniqueViolation:
		return nil, ErrAlreadyExists
	case pgCode(err) == pgForeignKeyViolation:
		return nil, ErrNotFound // user_id doesn't exist
	case err != nil:
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO seller_status_history (user_id, old_status, new_status, reason)
		 VALUES ($1::uuid, $2::seller_status, 'pending_review', $3)`,
		a.UserID, oldStatus, reason); err != nil {
		return nil, fmt.Errorf("repository: insert status history: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("repository: commit: %w", err)
	}
	return profile, nil
}

func (r *Repository) UpdateSellerPayoutInfo(ctx context.Context, userID, method, ref string) (*SellerProfile, error) {
	return scanSeller(r.pool.QueryRow(ctx,
		`UPDATE seller_profiles SET payout_method = $2, payout_account_ref = $3, updated_at = now()
		 WHERE user_id = $1::uuid RETURNING `+sellerCols, userID, method, ref))
}

// ChangeSellerStatus moves a seller to a new status in one transaction:
// lock the row, ask allowed(from, to), update, write the audit row, and
// adjust the "seller" role and the token cutoff to match.
func (r *Repository) ChangeSellerStatus(
	ctx context.Context, userID, to, reason, adminID string, allowed func(from, to string) bool,
) (*SellerProfile, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("repository: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var from string
	err = tx.QueryRow(ctx,
		`SELECT status::text FROM seller_profiles WHERE user_id = $1::uuid FOR UPDATE`, userID).Scan(&from)
	if errors.Is(err, pgx.ErrNoRows) || pgCode(err) == pgInvalidText {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: lock seller profile: %w", err)
	}
	if !allowed(from, to) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrInvalidState, from, to)
	}

	profile, err := scanSeller(tx.QueryRow(ctx,
		`UPDATE seller_profiles SET status = $2::seller_status, updated_at = now()
		 WHERE user_id = $1::uuid RETURNING `+sellerCols, userID, to))
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO seller_status_history (user_id, old_status, new_status, reason, changed_by_admin_id)
		 VALUES ($1::uuid, $2::seller_status, $3::seller_status, $4, NULLIF($5::text, '')::uuid)`,
		userID, from, to, reason, adminID); err != nil {
		return nil, fmt.Errorf("repository: insert status history: %w", err)
	}

	switch to {
	case "active":
		_, err = tx.Exec(ctx,
			`INSERT INTO user_roles (user_id, role) VALUES ($1::uuid, 'seller') ON CONFLICT DO NOTHING`, userID)
	case "suspended", "rejected":
		if _, err = tx.Exec(ctx,
			`DELETE FROM user_roles WHERE user_id = $1::uuid AND role = 'seller'`, userID); err == nil {
			_, err = tx.Exec(ctx,
				`UPDATE users SET tokens_valid_after = now(), updated_at = now() WHERE id = $1::uuid`, userID)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("repository: adjust role/tokens: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("repository: commit: %w", err)
	}
	return profile, nil
}

// ListSellers returns up to limit rows after the cursor (nil = first page),
// optionally filtered by status ("" = all). The caller asks for limit+1 to
// learn whether another page exists.
func (r *Repository) ListSellers(ctx context.Context, status string, after *Cursor, limit int) ([]SellerProfile, error) {
	q := `SELECT ` + sellerCols + ` FROM seller_profiles WHERE true`
	var args []any
	if status != "" {
		args = append(args, status)
		q += fmt.Sprintf(" AND status = $%d::seller_status", len(args))
	}
	if after != nil {
		args = append(args, after.CreatedAt, after.ID)
		q += fmt.Sprintf(" AND (created_at, user_id) > ($%d, $%d::uuid)", len(args)-1, len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(" ORDER BY created_at, user_id LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list sellers: %w", err)
	}
	defer rows.Close()

	var out []SellerProfile
	for rows.Next() {
		var p SellerProfile
		if err := rows.Scan(&p.UserID, &p.BusinessName, &p.Provider, &p.PayoutMethod, &p.PayoutAccountRef,
			&p.CommissionRate, &p.Status, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("repository: scan seller row: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
