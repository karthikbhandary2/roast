// Package middleware holds the gateway's HTTP-layer middleware. Distinct
// from pkg/middleware, which is gRPC-layer (used by internal services) -
// the two share the JWT parsing logic conceptually but operate on
// different request types (http.Request vs. gRPC metadata), so they don't
// share code directly.
package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims mirrors pkg/middleware.Claims (same JWT shape, issued by
// UserService.Authenticate) - duplicated rather than imported because this
// package deliberately has no gRPC dependency.
type Claims struct {
	UserID string   `json:"user_id"`
	Roles  []string `json:"roles"`
	jwt.RegisteredClaims
}

func (c *Claims) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

type ctxKey struct{ name string }

var (
	claimsKey    = ctxKey{"claims"}
	requestIDKey = ctxKey{"request_id"}
)

// RequestLogger logs method, path, status, and latency for every request,
// and assigns a request ID (from X-Request-ID if the client sent one, or a
// generated one) that handlers can thread into downstream gRPC calls as
// part of the saga's correlation_id story - see LLD.md §1.
func RequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				reqID = fmt.Sprintf("%d", time.Now().UnixNano())
			}
			ctx := context.WithValue(r.Context(), requestIDKey, reqID)
			r = r.WithContext(ctx)
			w.Header().Set("X-Request-ID", reqID)

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", reqID,
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// RequireAuth validates the Authorization: Bearer <token> header and
// attaches Claims to the request context. It only checks the token's
// signature and expiry (stateless) - the tokens_valid_after cutoff for
// suspended sellers (LLD.md §2.2 item 4) is enforced downstream, in
// UserService's RequireRole check, not here, since that check needs a
// Redis round-trip this blanket middleware is deliberately kept free of
// (see LLD.md §4.3 for why the gateway doesn't do that per-request).
func RequireAuth(jwtSecret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Bearer ") {
				writeUnauthorized(w, r, "missing bearer token")
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			claims := &Claims{}
			_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return jwtSecret, nil
			})
			if err != nil {
				writeUnauthorized(w, r, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeUnauthorized uses the same error shape (and the same "Unauthenticated"
// code name) as handlers.writeError, so clients see one format everywhere.
func writeUnauthorized(w http.ResponseWriter, r *http.Request, msg string) {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":           "Unauthenticated",
			"message":        msg,
			"correlation_id": RequestIDFromContext(r.Context()),
		},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write(append(body, '\n'))
}

// ClaimsFromContext retrieves the authenticated caller's claims. Only
// meaningful on routes behind RequireAuth - callers on public routes
// should not call this.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

// RequestIDFromContext retrieves the per-request ID set by RequestLogger,
// used as part of the correlation_id passed into saga-initiating calls.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}