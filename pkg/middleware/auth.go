package middleware

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Claims is the payload of every access token issued by User Service's
// Authenticate RPC. Kept minimal on purpose: roles are enough for every
// authorization check in this system (buyer/seller/admin), so there's no
// need to smuggle profile data into the token that would go stale.
type Claims struct {
	UserID string   `json:"user_id"`
	Roles  []string `json:"roles"`
	jwt.RegisteredClaims
}

type ctxKey struct{ name string }

var claimsKey = ctxKey{"claims"}

// AuthUnaryInterceptor validates the JWT the API Gateway attached as gRPC
// metadata ("authorization" header) and puts the parsed Claims on the
// context. Internal services trust this interceptor rather than
// re-validating against User Service on every call - the gateway is the
// only thing that talks to buyers/sellers directly, so it's the only place
// a raw token ever needs full validation against User Service.
func AuthUnaryInterceptor(secret []byte) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}
		tokens := md.Get("authorization")
		if len(tokens) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization token")
		}

		// The gateway forwards the caller's full "Authorization: Bearer <jwt>"
		// header value, so strip the scheme before parsing.
		raw := strings.TrimSpace(strings.TrimPrefix(tokens[0], "Bearer "))

		claims := &Claims{}
		_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return secret, nil
		})
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
		}

		ctx = context.WithValue(ctx, claimsKey, claims)
		return handler(ctx, req)
	}
}

// ClaimsFromContext retrieves the authenticated caller's claims, set by
// AuthUnaryInterceptor. The bool is false for RPCs that don't require auth
// (health checks) or were never routed through the interceptor.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

// RequireRole is a small helper for handlers to enforce role checks
// (e.g. only a seller may call MarkSubOrderShipped) without repeating the
// same context-extraction boilerplate in every RPC method.
func RequireRole(ctx context.Context, role string) error {
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "no authenticated caller")
	}
	for _, r := range claims.Roles {
		if r == role {
			return nil
		}
	}
	return status.Errorf(codes.PermissionDenied, "requires role %q", role)
}
