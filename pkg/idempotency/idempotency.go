// Package idempotency implements the pattern this project relies on for
// every saga step that touches money or stock: the idempotency-key record
// is written in the SAME transaction as the state change it protects, so a
// retried call with the same key can never observe "the charge happened
// but we forgot we'd already charged it" or vice versa.
//
// Redis is NOT used here on purpose - see pkg/redisclient's doc comment.
package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrAlreadyProcessed is returned by Check when a key has been seen before;
// callers should decode Existing themselves and return it as-is rather than
// re-running the operation.
var ErrAlreadyProcessed = errors.New("idempotency: request already processed")

type Result struct {
	Existing json.RawMessage // set only when ErrAlreadyProcessed is returned
}

// Run executes fn inside a transaction that is idempotency-key-guarded:
//   - if `key` was already recorded (in table `idempotencyTable`), fn is
//     never called, and the previously stored response is returned instead
//   - otherwise fn runs, and its returned response is recorded against
//     `key` in the same transaction that fn used to make its writes, so the
//     two either both commit or both roll back together
//
// fn receives the transaction to use for its own writes - it must not open
// its own transaction or the atomicity guarantee is lost.
func Run[T any](
	ctx context.Context,
	pool *pgxpool.Pool,
	idempotencyTable string,
	key string,
	requestType string,
	fn func(ctx context.Context, tx pgx.Tx) (T, error),
) (T, error) {
	var zero T

	tx, err := pool.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("idempotency: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op if already committed

	var existing []byte
	checkSQL := fmt.Sprintf(`SELECT response_payload FROM %s WHERE key = $1`, idempotencyTable)
	err = tx.QueryRow(ctx, checkSQL, key).Scan(&existing)
	if err == nil {
		// Seen before - do not re-run fn. Decode the stored response as T
		// and hand it back so the caller gets exactly what the first call
		// returned, not a freshly computed (and possibly different) result.
		var result T
		if unmarshalErr := json.Unmarshal(existing, &result); unmarshalErr != nil {
			return zero, fmt.Errorf("idempotency: decode stored response: %w", unmarshalErr)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return zero, fmt.Errorf("idempotency: check existing key: %w", err)
	}

	// Not seen before - run the actual operation on this same transaction.
	result, err := fn(ctx, tx)
	if err != nil {
		return zero, err // rollback via defer; nothing was recorded
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return zero, fmt.Errorf("idempotency: encode response: %w", err)
	}

	insertSQL := fmt.Sprintf(
		`INSERT INTO %s (key, request_type, response_payload) VALUES ($1, $2, $3)`,
		idempotencyTable,
	)
	if _, err := tx.Exec(ctx, insertSQL, key, requestType, payload); err != nil {
		return zero, fmt.Errorf("idempotency: record key: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("idempotency: commit: %w", err)
	}
	return result, nil
}
