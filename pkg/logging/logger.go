// Package logging provides a structured JSON logger, built on the standard
// library's log/slog rather than a third-party logging library - this is
// one of the few concerns where the stdlib is genuinely sufficient, so
// pulling in a dependency for it would just be extra supply-chain surface
// for no real benefit.
package logging

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey struct{}

// New builds the base logger for a service. serviceName is attached to
// every log line so centralized logging (e.g. a Loki/ELK stack fed by
// Docker Compose's log driver) can filter by service without parsing text.
func New(serviceName, level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler).With("service", serviceName)
}

// WithContext attaches a logger (already carrying request-scoped fields
// like correlation_id) to a context, so downstream code can retrieve it
// without threading a *slog.Logger through every function signature.
func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext retrieves the request-scoped logger, falling back to the
// default logger if none was attached - callers never need a nil check.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// WithCorrelationID returns a derived logger that tags every subsequent
// line with the saga's correlation_id, matching the order.saga_events
// table so a log line can be cross-referenced with its DB audit trail.
func WithCorrelationID(l *slog.Logger, correlationID string) *slog.Logger {
	return l.With("correlation_id", correlationID)
}
