// Package postgres wraps pgx's connection pool and provides the one helper
// every money- or stock-critical service needs: RunIdempotent, which
// combines a state-changing write with its idempotency-key record in a
// single transaction (see pkg/idempotency for the reasoning).
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct {
	*pgxpool.Pool
}

func NewPool(ctx context.Context, dsn string) (*Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &Pool{pool}, nil
}
