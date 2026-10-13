// Package middleware holds cross-cutting request/call wrappers shared by
// every service: the circuit breaker here, and the gRPC auth interceptor in
// auth.go.
package middleware

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sony/gobreaker"
)

// Breaker wraps a downstream dependency (another service's gRPC client, an
// HTTP call to a payment provider, etc.) with a circuit breaker: after
// enough consecutive failures it "opens" and fails fast for a cooldown
// period instead of piling up slow, doomed calls against a struggling
// dependency - the failure-handling pattern from the project's tech goals,
// separate from the Saga's compensating transactions (which undo *committed*
// work; the breaker exists to avoid *attempting* work against something
// that's already down).
type Breaker struct {
	cb *gobreaker.CircuitBreaker
}

// NewBreaker builds a named circuit breaker. name shows up in metrics/logs
// (e.g. "payment-service-grpc"), so pick something that identifies the
// specific downstream dependency being protected.
func NewBreaker(name string) *Breaker {
	settings := gobreaker.Settings{
		Name:        name,
		MaxRequests: 3,                // allowed through per half-open trial
		Interval:    30 * time.Second, // rolling window for the failure count
		Timeout:     15 * time.Second, // how long to stay open before trying again
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Trip after 5 consecutive failures, or a >=50% failure rate
			// once there's enough volume to make that rate meaningful.
			return counts.ConsecutiveFailures >= 5 ||
				(counts.Requests >= 10 && float64(counts.TotalFailures)/float64(counts.Requests) >= 0.5)
		},
	}
	return &Breaker{cb: gobreaker.NewCircuitBreaker(settings)}
}

// ErrCircuitOpen is returned (wrapped) when the breaker is open and a call
// is rejected without even attempting the downstream request.
var ErrCircuitOpen = gobreaker.ErrOpenState

// Do runs fn through the breaker. Use it to wrap exactly the downstream
// call, not surrounding business logic, so the breaker's failure count
// reflects the dependency's health and nothing else.
func Do[T any](ctx context.Context, b *Breaker, fn func(context.Context) (T, error)) (T, error) {
	result, err := b.cb.Execute(func() (interface{}, error) {
		return fn(ctx)
	})
	if err != nil {
		var zero T
		if err == gobreaker.ErrOpenState {
			return zero, fmt.Errorf("%s: %w", b.cb.Name(), ErrCircuitOpen)
		}
		return zero, err
	}
	return result.(T), nil
}

// State exposes the breaker's current state for a health/readiness endpoint
// or a Prometheus gauge (open/half-open/closed).
func (b *Breaker) State() string {
	return b.cb.State().String()
}

// IsCircuitOpen reports whether err is (or wraps) ErrCircuitOpen, letting a
// caller distinguish "this dependency is open, consider a fallback" from
// any other failure - used by the gateway's Search->ListProducts degraded
// mode (LLD.md §4.2).
func IsCircuitOpen(err error) bool {
	return errors.Is(err, ErrCircuitOpen)
}
