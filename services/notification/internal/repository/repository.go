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

type Notification struct {
	ID              string
	RecipientUserID string
	Channel         string
	Type            string
	RelatedEntityID string
	Status          string
	Attempts        int32
	LastError       string
	CreatedAt       time.Time
	SentAt          *time.Time
}

const notifCols = `id::text, recipient_user_id::text, channel::text, notification_type,
	related_entity_id::text, status::text, attempts, COALESCE(last_error, ''), created_at, sent_at`

func (r *Repository) EnqueueNotification(
	ctx context.Context, q Querier, recipientUserID, notifType, relatedEntityID, channel string,
) (*Notification, error) {
	if channel == "" {
		channel = "email"
	}
	var n Notification
	err := q.QueryRow(ctx,
		`INSERT INTO notifications (recipient_user_id, channel, notification_type, related_entity_id, status)
		 VALUES ($1::uuid, $2::notification_channel, $3, $4::uuid, 'queued')
		 ON CONFLICT (notification_type, related_entity_id, recipient_user_id) DO UPDATE SET updated_at = now() RETURNING `+notifCols,
		recipientUserID, channel, notifType, relatedEntityID).
		Scan(&n.ID, &n.RecipientUserID, &n.Channel, &n.Type, &n.RelatedEntityID,
			&n.Status, &n.Attempts, &n.LastError, &n.CreatedAt, &n.SentAt)
	if err != nil {
		// If conflict happened and DO NOTHING:
		var existing Notification
		qErr := q.QueryRow(ctx,
			`SELECT `+notifCols+` FROM notifications
			 WHERE notification_type = $1 AND related_entity_id = $2::uuid AND recipient_user_id = $3::uuid`,
			notifType, relatedEntityID, recipientUserID).
			Scan(&existing.ID, &existing.RecipientUserID, &existing.Channel, &existing.Type, &existing.RelatedEntityID,
				&existing.Status, &existing.Attempts, &existing.LastError, &existing.CreatedAt, &existing.SentAt)
		if qErr == nil {
			return &existing, nil
		}
		return nil, fmt.Errorf("repository: insert notification: %w", err)
	}
	return &n, nil
}

func (r *Repository) GetQueuedNotifications(ctx context.Context, q Querier, limit int) ([]*Notification, error) {
	rows, err := q.Query(ctx,
		`SELECT `+notifCols+` FROM notifications
		 WHERE status = 'queued' AND attempts < 5
		 ORDER BY created_at ASC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: get queued notifications: %w", err)
	}
	defer rows.Close()

	var out []*Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.RecipientUserID, &n.Channel, &n.Type, &n.RelatedEntityID,
			&n.Status, &n.Attempts, &n.LastError, &n.CreatedAt, &n.SentAt); err != nil {
			return nil, err
		}
		out = append(out, &n)
	}
	return out, rows.Err()
}

func (r *Repository) MarkSent(ctx context.Context, q Querier, id string) error {
	_, err := q.Exec(ctx,
		`UPDATE notifications
		 SET status = 'sent', sent_at = now()
		 WHERE id = $1::uuid`, id)
	return err
}

func (r *Repository) MarkFailed(ctx context.Context, q Querier, id string, lastError string) error {
	_, err := q.Exec(ctx,
		`UPDATE notifications
		 SET attempts = attempts + 1,
		     last_error = $2,
		     status = CASE WHEN attempts + 1 >= 5 THEN 'failed'::notification_status ELSE 'queued'::notification_status END
		 WHERE id = $1::uuid`, id, lastError)
	return err
}
