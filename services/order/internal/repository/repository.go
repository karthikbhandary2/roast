package repository

import (
	"context"
	"encoding/json"
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
	pgUniqueViolation = "23505"
	pgInvalidText     = "22P02"
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

// ------------------------------------------------------------- domain models

type Order struct {
	ID              string
	BuyerID         string
	ShippingAddress json.RawMessage
	TotalCurrency   string
	TotalMinor      int64
	Status          string // 'processing' | 'confirmed' | 'partially_failed' | 'failed'
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SubOrders       []*SubOrder
}

type SubOrder struct {
	ID               string
	ParentOrderID    string
	SellerID         string
	SubtotalCurrency string
	SubtotalMinor    int64
	Status           string // 'reserving'|'charging'|'awaiting_payment'|'confirmed'|'compensating'|'failed'|'shipped'|'delivered'
	ReservationID    string
	ChargeID         string
	TrackingNumber   string
	FailureReason    string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Items            []SubOrderItem
}

type SubOrderItem struct {
	SubOrderID   string
	ProductID    string
	Quantity     int32
	UnitCurrency string
	UnitMinor    int64
}

const (
	orderCols = `id::text, buyer_id::text, shipping_address, total_currency::text, total_amount_minor, status::text, created_at, updated_at`
	subCols   = `id::text, parent_order_id::text, seller_id::text, subtotal_currency::text, subtotal_amount_minor,
	             status::text, COALESCE(reservation_id::text, ''), COALESCE(charge_id::text, ''),
	             COALESCE(tracking_number, ''), COALESCE(failure_reason, ''), created_at, updated_at`
)

type scanner interface{ Scan(dest ...any) error }

func scanOrder(row scanner) (*Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.BuyerID, &o.ShippingAddress, &o.TotalCurrency, &o.TotalMinor, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows), pgCode(err) == pgInvalidText:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: scan order: %w", err)
	}
	return &o, nil
}

func scanSubOrder(row scanner) (*SubOrder, error) {
	var s SubOrder
	err := row.Scan(&s.ID, &s.ParentOrderID, &s.SellerID, &s.SubtotalCurrency, &s.SubtotalMinor,
		&s.Status, &s.ReservationID, &s.ChargeID, &s.TrackingNumber, &s.FailureReason, &s.CreatedAt, &s.UpdatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows), pgCode(err) == pgInvalidText:
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("repository: scan sub-order: %w", err)
	}
	return &s, nil
}

// ------------------------------------------------------------- orders

type NewSubOrderInput struct {
	SellerID string
	Currency string
	Subtotal int64
	Items    []SubOrderItem
}

func (r *Repository) CreateOrderWithSubOrders(
	ctx context.Context, q Querier,
	buyerID string, address json.RawMessage, currency string, totalMinor int64,
	subOrders []NewSubOrderInput,
) (*Order, error) {
	ord, err := scanOrder(q.QueryRow(ctx,
		`INSERT INTO orders (buyer_id, shipping_address, total_currency, total_amount_minor, status)
		 VALUES ($1::uuid, $2, $3::currency_code, $4, 'processing')
		 RETURNING `+orderCols,
		buyerID, address, currency, totalMinor))
	if err != nil {
		return nil, fmt.Errorf("repository: insert order: %w", err)
	}

	for _, soInput := range subOrders {
		sub, err := scanSubOrder(q.QueryRow(ctx,
			`INSERT INTO sub_orders (parent_order_id, seller_id, subtotal_currency, subtotal_amount_minor, status)
			 VALUES ($1::uuid, $2::uuid, $3::currency_code, $4, 'reserving')
			 RETURNING `+subCols,
			ord.ID, soInput.SellerID, soInput.Currency, soInput.Subtotal))
		if err != nil {
			return nil, fmt.Errorf("repository: insert sub_order: %w", err)
		}

		for _, item := range soInput.Items {
			_, err := q.Exec(ctx,
				`INSERT INTO sub_order_items (sub_order_id, product_id, quantity, unit_price_currency, unit_price_amount_minor)
				 VALUES ($1::uuid, $2::uuid, $3, $4::currency_code, $5)`,
				sub.ID, item.ProductID, item.Quantity, item.UnitCurrency, item.UnitMinor)
			if err != nil {
				return nil, fmt.Errorf("repository: insert sub_order_item: %w", err)
			}
			item.SubOrderID = sub.ID
			sub.Items = append(sub.Items, item)
		}
		ord.SubOrders = append(ord.SubOrders, sub)
	}

	return ord, nil
}

func (r *Repository) GetOrder(ctx context.Context, q Querier, id string) (*Order, error) {
	ord, err := scanOrder(q.QueryRow(ctx, `SELECT `+orderCols+` FROM orders WHERE id = $1::uuid`, id))
	if err != nil {
		return nil, err
	}
	if err := r.loadSubOrders(ctx, q, []*Order{ord}); err != nil {
		return nil, err
	}
	return ord, nil
}

func (r *Repository) GetSubOrder(ctx context.Context, q Querier, id string) (*SubOrder, error) {
	so, err := scanSubOrder(q.QueryRow(ctx, `SELECT `+subCols+` FROM sub_orders WHERE id = $1::uuid`, id))
	if err != nil {
		return nil, err
	}
	if err := r.loadSubOrderItems(ctx, q, []*SubOrder{so}); err != nil {
		return nil, err
	}
	return so, nil
}

func (r *Repository) loadSubOrders(ctx context.Context, q Querier, ords []*Order) error {
	if len(ords) == 0 {
		return nil
	}
	ids := make([]string, len(ords))
	byID := make(map[string]*Order, len(ords))
	for i, o := range ords {
		ids[i] = o.ID
		byID[o.ID] = o
	}

	rows, err := q.Query(ctx,
		`SELECT `+subCols+` FROM sub_orders WHERE parent_order_id = ANY($1::text[]::uuid[]) ORDER BY created_at`, ids)
	if err != nil {
		return fmt.Errorf("repository: load sub_orders: %w", err)
	}
	defer rows.Close()

	var allSubs []*SubOrder
	for rows.Next() {
		so, err := scanSubOrder(rows)
		if err != nil {
			return err
		}
		if ord, ok := byID[so.ParentOrderID]; ok {
			ord.SubOrders = append(ord.SubOrders, so)
		}
		allSubs = append(allSubs, so)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	return r.loadSubOrderItems(ctx, q, allSubs)
}

func (r *Repository) loadSubOrderItems(ctx context.Context, q Querier, subs []*SubOrder) error {
	if len(subs) == 0 {
		return nil
	}
	ids := make([]string, len(subs))
	byID := make(map[string]*SubOrder, len(subs))
	for i, s := range subs {
		ids[i] = s.ID
		byID[s.ID] = s
	}

	rows, err := q.Query(ctx,
		`SELECT sub_order_id::text, product_id::text, quantity, unit_price_currency::text, unit_price_amount_minor
		 FROM sub_order_items WHERE sub_order_id = ANY($1::text[]::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("repository: load items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item SubOrderItem
		if err := rows.Scan(&item.SubOrderID, &item.ProductID, &item.Quantity, &item.UnitCurrency, &item.UnitMinor); err != nil {
			return err
		}
		if so, ok := byID[item.SubOrderID]; ok {
			so.Items = append(so.Items, item)
		}
	}
	return rows.Err()
}

func (r *Repository) ListOrdersForBuyer(ctx context.Context, q Querier, buyerID string, after *Cursor, limit int) ([]*Order, error) {
	sql := `SELECT ` + orderCols + ` FROM orders WHERE buyer_id = $1::uuid`
	args := []any{buyerID}
	if after != nil {
		args = append(args, after.CreatedAt, after.ID)
		sql += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args))
	}
	args = append(args, limit)
	sql += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list buyer orders: %w", err)
	}
	defer rows.Close()

	var out []*Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.loadSubOrders(ctx, q, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) ListSubOrdersForSeller(
	ctx context.Context, q Querier, sellerID string, statusFilter string, after *Cursor, limit int,
) ([]*SubOrder, error) {
	sql := `SELECT ` + subCols + ` FROM sub_orders WHERE seller_id = $1::uuid`
	args := []any{sellerID}
	if statusFilter != "" {
		args = append(args, statusFilter)
		sql += fmt.Sprintf(" AND status = $%d::sub_order_status", len(args))
	}
	if after != nil {
		args = append(args, after.CreatedAt, after.ID)
		sql += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args))
	}
	args = append(args, limit)
	sql += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list seller sub-orders: %w", err)
	}
	defer rows.Close()

	var out []*SubOrder
	for rows.Next() {
		s, err := scanSubOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.loadSubOrderItems(ctx, q, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) ListAllOrders(
	ctx context.Context, q Querier, statusFilter string, after *Cursor, limit int,
) ([]*Order, error) {
	sql := `SELECT ` + orderCols + ` FROM orders WHERE true`
	var args []any
	if statusFilter != "" {
		args = append(args, statusFilter)
		sql += fmt.Sprintf(" AND status = $%d::order_status", len(args))
	}
	if after != nil {
		args = append(args, after.CreatedAt, after.ID)
		sql += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d::uuid)", len(args)-1, len(args))
	}
	args = append(args, limit)
	sql += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: list all orders: %w", err)
	}
	defer rows.Close()

	var out []*Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.loadSubOrders(ctx, q, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) UpdateSubOrderStatus(
	ctx context.Context, q Querier, subOrderID, status, reservationID, chargeID, failureReason string,
) (*SubOrder, error) {
	return scanSubOrder(q.QueryRow(ctx,
		`UPDATE sub_orders
		 SET status = $2::sub_order_status,
		     reservation_id = COALESCE(NULLIF($3, '')::uuid, reservation_id),
		     charge_id = COALESCE(NULLIF($4, '')::uuid, charge_id),
		     failure_reason = COALESCE(NULLIF($5, ''), failure_reason),
		     updated_at = now()
		 WHERE id = $1::uuid
		 RETURNING `+subCols,
		subOrderID, status, reservationID, chargeID, failureReason))
}

// CASResumeSubOrder atomically changes status from 'awaiting_payment' to 'charging'.
// If 0 rows affected, another caller won the CAS.
func (r *Repository) CASResumeSubOrder(ctx context.Context, q Querier, subOrderID string) (*SubOrder, error) {
	return scanSubOrder(q.QueryRow(ctx,
		`UPDATE sub_orders
		 SET status = 'charging', updated_at = now()
		 WHERE id = $1::uuid AND status = 'awaiting_payment'
		 RETURNING `+subCols,
		subOrderID))
}

func (r *Repository) MarkSubOrderShipped(
	ctx context.Context, q Querier, subOrderID, sellerID, trackingNumber string,
) (*SubOrder, error) {
	so, err := scanSubOrder(q.QueryRow(ctx,
		`UPDATE sub_orders
		 SET tracking_number = $3, status = 'shipped', updated_at = now()
		 WHERE id = $1::uuid AND seller_id = $2::uuid AND status = 'confirmed'
		 RETURNING `+subCols,
		subOrderID, sellerID, trackingNumber))
	if err != nil {
		return nil, err
	}
	_ = r.loadSubOrderItems(ctx, q, []*SubOrder{so})
	return so, nil
}

func (r *Repository) ConfirmDeliveryByBuyer(
	ctx context.Context, q Querier, subOrderID, buyerID string,
) (*SubOrder, error) {
	so, err := scanSubOrder(q.QueryRow(ctx,
		`UPDATE sub_orders
		 SET status = 'delivered', updated_at = now()
		 WHERE id = $1::uuid AND status = 'shipped'
		   AND parent_order_id IN (SELECT id FROM orders WHERE buyer_id = $2::uuid)
		 RETURNING `+subCols,
		subOrderID, buyerID))
	if err != nil {
		return nil, err
	}
	_ = r.loadSubOrderItems(ctx, q, []*SubOrder{so})
	return so, nil
}

func (r *Repository) ConfirmDeliveryByCarrier(
	ctx context.Context, q Querier, trackingNumber string,
) (*SubOrder, error) {
	so, err := scanSubOrder(q.QueryRow(ctx,
		`UPDATE sub_orders
		 SET status = 'delivered', updated_at = now()
		 WHERE tracking_number = $1 AND status = 'shipped'
		 RETURNING `+subCols,
		trackingNumber))
	if err != nil {
		return nil, err
	}
	_ = r.loadSubOrderItems(ctx, q, []*SubOrder{so})
	return so, nil
}

func (r *Repository) RecomputeParentOrderStatus(ctx context.Context, q Querier, parentOrderID string) (string, error) {
	rows, err := q.Query(ctx, `SELECT status::text FROM sub_orders WHERE parent_order_id = $1::uuid`, parentOrderID)
	if err != nil {
		return "", fmt.Errorf("repository: read sub_order statuses: %w", err)
	}
	defer rows.Close()

	var statuses []string
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			return "", err
		}
		statuses = append(statuses, st)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	if len(statuses) == 0 {
		return "failed", nil
	}

	hasNonTerminal := false
	allConfirmed := true
	allFailed := true

	for _, s := range statuses {
		switch s {
		case "reserving", "charging", "awaiting_payment", "compensating":
			hasNonTerminal = true
		case "confirmed", "shipped", "delivered":
			allFailed = false
		case "failed":
			allConfirmed = false
		default:
			hasNonTerminal = true
		}
	}

	var newStatus string
	if hasNonTerminal {
		newStatus = "processing"
	} else if allConfirmed {
		newStatus = "confirmed"
	} else if allFailed {
		newStatus = "failed"
	} else {
		newStatus = "partially_failed"
	}

	_, err = q.Exec(ctx,
		`UPDATE orders SET status = $2::order_status, updated_at = now() WHERE id = $1::uuid`,
		parentOrderID, newStatus)
	if err != nil {
		return "", fmt.Errorf("repository: update parent status: %w", err)
	}

	return newStatus, nil
}

func (r *Repository) VerifyPurchase(ctx context.Context, q Querier, buyerID, productID string) (bool, string, error) {
	var subOrderID string
	err := q.QueryRow(ctx,
		`SELECT so.id::text FROM sub_orders so
		 JOIN sub_order_items soi ON soi.sub_order_id = so.id
		 JOIN orders o ON o.id = so.parent_order_id
		 WHERE o.buyer_id = $1::uuid AND soi.product_id = $2::uuid
		   AND so.status IN ('confirmed', 'shipped', 'delivered')
		 LIMIT 1`, buyerID, productID).Scan(&subOrderID)
	if errors.Is(err, pgx.ErrNoRows) || pgCode(err) == pgInvalidText {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("repository: verify purchase: %w", err)
	}
	return true, subOrderID, nil
}

func (r *Repository) LogSagaEvent(
	ctx context.Context, q Querier, subOrderID, step, status, correlationID string, payload any,
) error {
	var payloadBytes []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err == nil {
			payloadBytes = b
		}
	}
	_, err := q.Exec(ctx,
		`INSERT INTO saga_events (sub_order_id, step, status, correlation_id, payload)
		 VALUES ($1::uuid, $2, $3, $4::uuid, $5)`,
		subOrderID, step, status, correlationID, payloadBytes)
	if err != nil {
		return fmt.Errorf("repository: log saga event: %w", err)
	}
	return nil
}

func (r *Repository) SweepAwaitingPayment(ctx context.Context, q Querier, cutoff time.Time) ([]string, error) {
	rows, err := q.Query(ctx,
		`SELECT id::text FROM sub_orders
		 WHERE status = 'awaiting_payment' AND created_at < $1`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("repository: sweep awaiting payment: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) SweepAutoDelivered(ctx context.Context, q Querier, cutoff time.Time) ([]string, error) {
	rows, err := q.Query(ctx,
		`UPDATE sub_orders
		 SET status = 'delivered', updated_at = now()
		 WHERE status = 'shipped' AND updated_at < $1
		 RETURNING id::text`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("repository: sweep auto delivered: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) CheckShippingWebhookEvent(ctx context.Context, q Querier, carrier, eventID string) (bool, error) {
	var dummy int
	err := q.QueryRow(ctx,
		`SELECT 1 FROM shipping_webhook_events WHERE carrier = $1 AND provider_event_id = $2`,
		carrier, eventID).Scan(&dummy)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) InsertShippingWebhookEvent(
	ctx context.Context, q Querier, carrier, eventID, eventType string, payload []byte,
) error {
	_, err := q.Exec(ctx,
		`INSERT INTO shipping_webhook_events (carrier, provider_event_id, event_type, payload, processed_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (carrier, provider_event_id) DO NOTHING`,
		carrier, eventID, eventType, payload)
	if err != nil {
		return fmt.Errorf("repository: insert shipping webhook: %w", err)
	}
	return nil
}
