// Package repository is the Product Service's SQL layer: queries only, no
// business rules. Every method takes a Querier, which is either the pool or a
// transaction, so the service layer decides what runs inside which transaction
// (that is what lets reservations share one transaction with pkg/idempotency).
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

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// Pool is used by the service layer to start transactions and to hand to
// pkg/idempotency.
func (r *Repository) Pool() *pgxpool.Pool { return r.pool }

// DB is the pool as a Querier, for calls that don't need a transaction.
func (r *Repository) DB() Querier { return r.pool }

// Cursor is the keyset position for paginated lists, newest first: the next
// page starts strictly after (CreatedAt, ID) in descending order.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// ------------------------------------------------------------- products

type Product struct {
	ID            string
	SellerID      string
	Name          string
	Description   string
	Currency      string // "USD" | "INR" | "GBP" | "JPY"
	PriceMinor    int64
	BatchLabel    string
	StockTotal    int32
	StockReserved int32
	Status        string // stored: "draft" | "live". low_stock / sold_out are derived on read.
	ImageURLs     []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// StockAvailable is derived, never stored, so it can't drift from the two
// counts it comes from.
func (p *Product) StockAvailable() int32 { return p.StockTotal - p.StockReserved }

type NewProduct struct {
	SellerID    string
	Name        string
	Description string
	Currency    string
	PriceMinor  int64
	BatchLabel  string
	Stock       int32
}

const productCols = `id::text, seller_id::text, name, description, price_currency::text, price_amount_minor,
	COALESCE(batch_label, ''), stock_total, stock_reserved, status::text, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanProduct(row scanner) (*Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.SellerID, &p.Name, &p.Description, &p.Currency, &p.PriceMinor,
		&p.BatchLabel, &p.StockTotal, &p.StockReserved, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows), pgCode(err) == pgInvalidText:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: scan product: %w", err)
	}
	return &p, nil
}

func (r *Repository) InsertProduct(ctx context.Context, q Querier, n NewProduct) (*Product, error) {
	return scanProduct(q.QueryRow(ctx,
		`INSERT INTO products (seller_id, name, description, price_currency, price_amount_minor,
		                       batch_label, stock_total, status)
		 VALUES ($1::uuid, $2, $3, $4::currency_code, $5, NULLIF($6, ''), $7, 'live')
		 RETURNING `+productCols,
		n.SellerID, n.Name, n.Description, n.Currency, n.PriceMinor, n.BatchLabel, n.Stock))
}

// GetProduct loads a product with its images. With forUpdate it takes a row
// lock, which is only meaningful inside a transaction.
func (r *Repository) GetProduct(ctx context.Context, q Querier, id string, forUpdate bool) (*Product, error) {
	sql := `SELECT ` + productCols + ` FROM products WHERE id = $1::uuid`
	if forUpdate {
		sql += " FOR UPDATE"
	}
	p, err := scanProduct(q.QueryRow(ctx, sql, id))
	if err != nil {
		return nil, err
	}
	if err := r.loadImages(ctx, q, []*Product{p}); err != nil {
		return nil, err
	}
	return p, nil
}

func (r *Repository) UpdateProduct(ctx context.Context, q Querier, p *Product) (*Product, error) {
	return scanProduct(q.QueryRow(ctx,
		`UPDATE products
		 SET name = $2, description = $3, price_currency = $4::currency_code,
		     price_amount_minor = $5, status = $6::product_status, updated_at = now()
		 WHERE id = $1::uuid
		 RETURNING `+productCols,
		p.ID, p.Name, p.Description, p.Currency, p.PriceMinor, p.Status))
}

// ReplaceImages swaps a product's whole image list; position is the index.
func (r *Repository) ReplaceImages(ctx context.Context, q Querier, productID string, urls []string) error {
	if _, err := q.Exec(ctx, `DELETE FROM product_images WHERE product_id = $1::uuid`, productID); err != nil {
		return fmt.Errorf("repository: clear images: %w", err)
	}
	for i, u := range urls {
		if _, err := q.Exec(ctx,
			`INSERT INTO product_images (product_id, position, url) VALUES ($1::uuid, $2, $3)`,
			productID, int16(i), u); err != nil {
			return fmt.Errorf("repository: insert image: %w", err)
		}
	}
	return nil
}

func (r *Repository) loadImages(ctx context.Context, q Querier, ps []*Product) error {
	if len(ps) == 0 {
		return nil
	}
	ids := make([]string, len(ps))
	byID := make(map[string]*Product, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
		byID[p.ID] = p
	}
	// []string is sent as text[] and cast on the server: pgx can't encode a
	// []string straight into uuid[].
	rows, err := q.Query(ctx,
		`SELECT product_id::text, url FROM product_images
		 WHERE product_id = ANY($1::text[]::uuid[]) ORDER BY product_id, position`, ids)
	if err != nil {
		return fmt.Errorf("repository: load images: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pid, url string
		if err := rows.Scan(&pid, &url); err != nil {
			return fmt.Errorf("repository: scan image: %w", err)
		}
		byID[pid].ImageURLs = append(byID[pid].ImageURLs, url)
	}
	return rows.Err()
}

type ListFilter struct {
	SellerID      string
	IncludeDrafts bool
	InStockOnly   bool
	After         *Cursor
	Limit         int
}

// ListProducts returns newest first. The caller asks for limit+1 rows to learn
// whether another page exists.
func (r *Repository) ListProducts(ctx context.Context, q Querier, f ListFilter) ([]*Product, error) {
	sql := `SELECT ` + productCols + ` FROM products WHERE true`
	var args []any
	if f.SellerID != "" {
		args = append(args, f.SellerID)
		sql += fmt.Sprintf(" AND seller_id = $%d::uuid", len(args))
	}
	if !f.IncludeDrafts {
		sql += " AND status <> 'draft'"
	}
	if f.InStockOnly {
		sql += " AND stock_total - stock_reserved > 0"
	}
	if f.After != nil {
		args = append(args, f.After.CreatedAt, f.After.ID)
		sql += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args))
	}
	args = append(args, f.Limit)
	sql += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list products: %w", err)
	}
	var out []*Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: list products: %w", err)
	}
	if err := r.loadImages(ctx, q, out); err != nil {
		return nil, err
	}
	return out, nil
}

// LockProducts acquires row-level locks on the specified product IDs in deterministic order.
func (r *Repository) LockProducts(ctx context.Context, q Querier, ids []string) (map[string]*Product, error) {
	if len(ids) == 0 {
		return make(map[string]*Product), nil
	}
	rows, err := q.Query(ctx,
		`SELECT `+productCols+` FROM products
		 WHERE id = ANY($1::text[]::uuid[])
		 ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return nil, fmt.Errorf("repository: lock products: %w", err)
	}
	defer rows.Close()

	byID := make(map[string]*Product, len(ids))
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		byID[p.ID] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: lock products: %w", err)
	}
	return byID, nil
}

// -------------------------------------------------------- reservations

type ReservationLine struct {
	ProductID string
	Quantity  int32
}

type Reservation struct {
	ID         string
	SubOrderID string
	SellerID   string
	Status     string // 'active' | 'confirmed' | 'released'
	CreatedAt  time.Time
	ExpiresAt  time.Time
	Lines      []ReservationLine
}

func (r *Repository) CreateReservation(
	ctx context.Context, q Querier, subOrderID, sellerID string, expiresAt time.Time, lines []ReservationLine,
) (string, error) {
	var reservationID string
	err := q.QueryRow(ctx,
		`INSERT INTO inventory_reservations (sub_order_id, seller_id, status, expires_at)
		 VALUES ($1::uuid, $2::uuid, 'active', $3)
		 RETURNING id::text`, subOrderID, sellerID, expiresAt).Scan(&reservationID)
	if err != nil {
		if pgCode(err) == pgUniqueViolation {
			return "", ErrAlreadyExists
		}
		return "", fmt.Errorf("repository: insert reservation: %w", err)
	}

	for _, line := range lines {
		_, err := q.Exec(ctx,
			`INSERT INTO reservation_lines (reservation_id, product_id, quantity)
			 VALUES ($1::uuid, $2::uuid, $3)`,
			reservationID, line.ProductID, line.Quantity)
		if err != nil {
			return "", fmt.Errorf("repository: insert reservation line: %w", err)
		}

		_, err = q.Exec(ctx,
			`UPDATE products
			 SET stock_reserved = stock_reserved + $2, updated_at = now()
			 WHERE id = $1::uuid`,
			line.ProductID, line.Quantity)
		if err != nil {
			return "", fmt.Errorf("repository: update stock reserved: %w", err)
		}
	}

	return reservationID, nil
}

func (r *Repository) GetReservation(ctx context.Context, q Querier, id string) (*Reservation, error) {
	var res Reservation
	err := q.QueryRow(ctx,
		`SELECT id::text, sub_order_id::text, seller_id::text, status::text, created_at, expires_at
		 FROM inventory_reservations WHERE id = $1::uuid`, id).
		Scan(&res.ID, &res.SubOrderID, &res.SellerID, &res.Status, &res.CreatedAt, &res.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) || pgCode(err) == pgInvalidText {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("repository: get reservation: %w", err)
	}

	rows, err := q.Query(ctx,
		`SELECT product_id::text, quantity FROM reservation_lines WHERE reservation_id = $1::uuid`, id)
	if err != nil {
		return nil, fmt.Errorf("repository: get reservation lines: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var line ReservationLine
		if err := rows.Scan(&line.ProductID, &line.Quantity); err != nil {
			return nil, err
		}
		res.Lines = append(res.Lines, line)
	}
	return &res, rows.Err()
}

func (r *Repository) ReleaseReservation(ctx context.Context, q Querier, id string) ([]string, error) {
	res, err := r.GetReservation(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if res.Status != "active" {
		// Already released or confirmed, no-op
		return nil, nil
	}

	var productIDs []string
	for _, line := range res.Lines {
		_, err := q.Exec(ctx,
			`UPDATE products
			 SET stock_reserved = stock_reserved - $2, updated_at = now()
			 WHERE id = $1::uuid`,
			line.ProductID, line.Quantity)
		if err != nil {
			return nil, fmt.Errorf("repository: release stock: %w", err)
		}
		productIDs = append(productIDs, line.ProductID)
	}

	_, err = q.Exec(ctx,
		`UPDATE inventory_reservations SET status = 'released' WHERE id = $1::uuid`, id)
	if err != nil {
		return nil, fmt.Errorf("repository: update reservation status to released: %w", err)
	}
	return productIDs, nil
}

func (r *Repository) ConfirmReservation(ctx context.Context, q Querier, id string) ([]string, error) {
	res, err := r.GetReservation(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if res.Status == "confirmed" {
		return nil, nil
	}
	if res.Status != "active" {
		return nil, fmt.Errorf("cannot confirm reservation in status %q", res.Status)
	}

	var productIDs []string
	for _, line := range res.Lines {
		_, err := q.Exec(ctx,
			`UPDATE products
			 SET stock_total = stock_total - $2, stock_reserved = stock_reserved - $2, updated_at = now()
			 WHERE id = $1::uuid`,
			line.ProductID, line.Quantity)
		if err != nil {
			return nil, fmt.Errorf("repository: confirm stock deduction: %w", err)
		}
		productIDs = append(productIDs, line.ProductID)
	}

	_, err = q.Exec(ctx,
		`UPDATE inventory_reservations SET status = 'confirmed' WHERE id = $1::uuid`, id)
	if err != nil {
		return nil, fmt.Errorf("repository: update reservation status to confirmed: %w", err)
	}
	return productIDs, nil
}

func (r *Repository) SweepExpiredReservations(ctx context.Context, q Querier) ([]string, error) {
	rows, err := q.Query(ctx,
		`SELECT id::text FROM inventory_reservations
		 WHERE status = 'active' AND expires_at < now()
		 LIMIT 50`)
	if err != nil {
		return nil, fmt.Errorf("repository: query expired reservations: %w", err)
	}
	defer rows.Close()

	var expiredIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		expiredIDs = append(expiredIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var affectedProductIDs []string
	for _, id := range expiredIDs {
		pids, err := r.ReleaseReservation(ctx, q, id)
		if err != nil {
			return nil, err
		}
		affectedProductIDs = append(affectedProductIDs, pids...)
	}
	return affectedProductIDs, nil
}

// ------------------------------------------------------------- reviews

type Review struct {
	ID         string
	ProductID  string
	BuyerID    string
	SubOrderID string
	Rating     int32
	Comment    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const reviewCols = `id::text, product_id::text, buyer_id::text, sub_order_id::text, rating, comment, created_at, updated_at`

func scanReview(row scanner) (*Review, error) {
	var rev Review
	err := row.Scan(&rev.ID, &rev.ProductID, &rev.BuyerID, &rev.SubOrderID, &rev.Rating, &rev.Comment, &rev.CreatedAt, &rev.UpdatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows), pgCode(err) == pgInvalidText:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: scan review: %w", err)
	}
	return &rev, nil
}

func (r *Repository) CreateReview(
	ctx context.Context, q Querier, productID, buyerID, subOrderID string, rating int32, comment string,
) (*Review, error) {
	rev, err := scanReview(q.QueryRow(ctx,
		`INSERT INTO reviews (product_id, buyer_id, sub_order_id, rating, comment)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5)
		 RETURNING `+reviewCols,
		productID, buyerID, subOrderID, rating, comment))
	if err != nil {
		if pgCode(err) == pgUniqueViolation {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return rev, nil
}

func (r *Repository) GetReview(ctx context.Context, q Querier, reviewID string) (*Review, error) {
	return scanReview(q.QueryRow(ctx, `SELECT `+reviewCols+` FROM reviews WHERE id = $1::uuid`, reviewID))
}

func (r *Repository) UpdateReview(
	ctx context.Context, q Querier, reviewID, buyerID string, rating int32, comment string,
) (*Review, error) {
	rev, err := scanReview(q.QueryRow(ctx,
		`UPDATE reviews
		 SET rating = $3, comment = $4, updated_at = now()
		 WHERE id = $1::uuid AND buyer_id = $2::uuid
		 RETURNING `+reviewCols,
		reviewID, buyerID, rating, comment))
	return rev, err
}

func (r *Repository) DeleteReview(ctx context.Context, q Querier, reviewID, buyerID string) error {
	tag, err := q.Exec(ctx,
		`DELETE FROM reviews WHERE id = $1::uuid AND buyer_id = $2::uuid`, reviewID, buyerID)
	if err != nil {
		return fmt.Errorf("repository: delete review: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) ListReviews(
	ctx context.Context, q Querier, productID string, after *Cursor, limit int,
) ([]*Review, error) {
	sql := `SELECT ` + reviewCols + ` FROM reviews WHERE product_id = $1::uuid`
	args := []any{productID}
	if after != nil {
		args = append(args, after.CreatedAt, after.ID)
		sql += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args))
	}
	args = append(args, limit)
	sql += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list reviews: %w", err)
	}
	defer rows.Close()

	var out []*Review
	for rows.Next() {
		rev, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}