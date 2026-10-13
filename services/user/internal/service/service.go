// Package service holds the User Service's business rules: validation,
// password hashing, token issuing, and which seller-status transitions are
// legal. It talks to Postgres only through the repository package and
// returns plain errors; the grpc package maps those onto status codes.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/roastco/ecommerce/pkg/middleware"
	"github.com/roastco/ecommerce/services/user/internal/repository"
)

var (
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrFailedPrecondition = errors.New("failed precondition")
	ErrUnimplemented      = errors.New("not implemented")
)

func invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidArgument, msg) }

type Config struct {
	JWTSecret       []byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	BcryptCost      int
}

// Publisher is satisfied by *kafkaclient.Producer. Events are best-effort:
// they're sent after the database transaction commits, so a failed publish is
// logged and never fails the request (see LLD.md §3.3 and §9 item 3).
type Publisher interface {
	Publish(ctx context.Context, aggregateID, eventType string, payload any) error
}

// NoopPublisher is used when KAFKA_BROKERS isn't set.
type NoopPublisher struct{}

func (NoopPublisher) Publish(context.Context, string, string, any) error { return nil }

type Service struct {
	repo      *repository.Repository
	cfg       Config
	pub       Publisher
	log       *slog.Logger
	dummyHash []byte // compared against when an email is unknown, so unknown and wrong-password cost the same time
}

func New(repo *repository.Repository, cfg Config, pub Publisher, log *slog.Logger) *Service {
	dummy, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), cfg.BcryptCost)
	return &Service{repo: repo, cfg: cfg, pub: pub, log: log, dummyHash: dummy}
}

// ---------------------------------------------------------------- users

func normalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

func validateEmail(e string) error {
	if len(e) > 254 {
		return invalid("email is too long")
	}
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e {
		return invalid("email is not valid")
	}
	return nil
}

func validateDisplayName(n string) error {
	if n == "" || len(n) > 100 {
		return invalid("display_name must be 1-100 characters")
	}
	return nil
}

func (s *Service) Register(ctx context.Context, email, password, displayName string) (*repository.User, error) {
	email = normalizeEmail(email)
	displayName = strings.TrimSpace(displayName)
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if err := validateDisplayName(displayName); err != nil {
		return nil, err
	}
	// bcrypt only uses the first 72 bytes of a password.
	if len(password) < 8 || len(password) > 72 {
		return nil, invalid("password must be 8-72 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cfg.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("service: hash password: %w", err)
	}
	return s.repo.CreateUser(ctx, email, string(hash), displayName)
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
}

func (s *Service) Authenticate(ctx context.Context, email, password string) (*TokenPair, error) {
	u, err := s.repo.GetUserByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, repository.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return nil, ErrInvalidCredentials // never reveal which emails exist
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}

	access, err := s.issueAccessToken(u)
	if err != nil {
		return nil, err
	}
	refresh, refreshHash, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	if err := s.repo.StoreRefreshToken(ctx, u.ID, refreshHash, time.Now().Add(s.cfg.RefreshTokenTTL)); err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresIn: s.cfg.AccessTokenTTL}, nil
}

func (s *Service) issueAccessToken(u *repository.User) (string, error) {
	now := time.Now()
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	jti, err := randomHex(16)
	if err != nil {
		return "", err
	}
	claims := &middleware.Claims{
		UserID: u.ID,
		Roles:  roles,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID,
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.AccessTokenTTL)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.cfg.JWTSecret)
	if err != nil {
		return "", fmt.Errorf("service: sign token: %w", err)
	}
	return signed, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("service: random: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// newRefreshToken returns the opaque token (given to the client once) and its
// SHA-256 hash (the only thing stored). SHA-256 rather than bcrypt because
// the token is already 256 bits of randomness; slow hashing adds nothing.
func newRefreshToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("service: random: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

// Logout is "log out everywhere": all refresh tokens revoked, and access
// tokens issued before now() fail the cutoff in ValidateToken.
func (s *Service) Logout(ctx context.Context, userID string) error {
	return s.repo.RevokeAllSessions(ctx, userID)
}

type TokenInfo struct {
	Valid  bool
	UserID string
	Roles  []string
}

// ValidateToken checks signature, expiry, and the tokens_valid_after cutoff.
// A bad token is Valid=false, not an error. Roles come from the database, so
// they are current even if the token's own roles claim is stale.
//
// NOTE: only this RPC enforces the cutoff today. The gRPC auth interceptor in
// pkg/middleware does not, so a revoked access token still passes the
// interceptor until it expires. See the known-gaps list in the hand-off notes.
func (s *Service) ValidateToken(ctx context.Context, token string) (*TokenInfo, error) {
	claims := &middleware.Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return s.cfg.JWTSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return &TokenInfo{}, nil
	}

	u, err := s.repo.GetUserByID(ctx, claims.UserID)
	if errors.Is(err, repository.ErrNotFound) {
		return &TokenInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	// Compare at whole-second precision: JWT iat has no sub-second part, so a
	// token minted in the same second as the cutoff stays valid.
	if u.TokensValidAfter != nil && claims.IssuedAt != nil &&
		claims.IssuedAt.Time.Before(u.TokensValidAfter.Truncate(time.Second)) {
		return &TokenInfo{}, nil
	}
	return &TokenInfo{Valid: true, UserID: u.ID, Roles: u.Roles}, nil
}

func (s *Service) GetUser(ctx context.Context, id string) (*repository.User, error) {
	return s.repo.GetUserByID(ctx, id)
}

func (s *Service) UpdateProfile(ctx context.Context, id, displayName string) (*repository.User, error) {
	displayName = strings.TrimSpace(displayName)
	if err := validateDisplayName(displayName); err != nil {
		return nil, err
	}
	return s.repo.UpdateDisplayName(ctx, id, displayName)
}

// -------------------------------------------------------------- sellers

type RegisterSellerInput struct {
	UserID           string
	BusinessName     string
	Provider         string // "razorpay" | "stripe"
	PayoutMethod     string
	PayoutAccountRef string
	ForceReonboard   bool
}

func (s *Service) RegisterSeller(ctx context.Context, in RegisterSellerInput) (*repository.SellerProfile, error) {
	if in.ForceReonboard {
		// Needs PaymentService.GetSellerBalance to prove nothing is stranded
		// on the old provider (LLD.md §2.2). Payment Service doesn't exist yet.
		return nil, fmt.Errorf("%w: provider switching needs Payment Service, which isn't built yet", ErrUnimplemented)
	}
	in.BusinessName = strings.TrimSpace(in.BusinessName)
	if in.BusinessName == "" || len(in.BusinessName) > 120 {
		return nil, invalid("business_name must be 1-120 characters")
	}
	if in.Provider != "razorpay" && in.Provider != "stripe" {
		return nil, invalid("provider must be razorpay or stripe")
	}
	if strings.TrimSpace(in.PayoutMethod) == "" || strings.TrimSpace(in.PayoutAccountRef) == "" {
		return nil, invalid("payout_method and payout_account_ref are required")
	}
	return s.repo.ApplyAsSeller(ctx, repository.SellerApplication{
		UserID:           in.UserID,
		BusinessName:     in.BusinessName,
		Provider:         in.Provider,
		PayoutMethod:     strings.TrimSpace(in.PayoutMethod),
		PayoutAccountRef: strings.TrimSpace(in.PayoutAccountRef),
	})
}

func (s *Service) GetSellerProfile(ctx context.Context, userID string) (*repository.SellerProfile, error) {
	return s.repo.GetSellerProfile(ctx, userID)
}

// UpdateSellerPayoutInfo changes payout details within the seller's existing
// provider; the provider itself is never changed here.
func (s *Service) UpdateSellerPayoutInfo(ctx context.Context, userID, method, ref string) (*repository.SellerProfile, error) {
	method, ref = strings.TrimSpace(method), strings.TrimSpace(ref)
	if method == "" || ref == "" {
		return nil, invalid("payout_method and payout_account_ref are required")
	}
	return s.repo.UpdateSellerPayoutInfo(ctx, userID, method, ref)
}

// allowedTransition is the seller-status state machine from LLD.md §2.2.
// rejected is terminal here: a rejected seller reapplies via RegisterSeller.
func allowedTransition(from, to string) bool {
	switch from {
	case "pending_review":
		return to == "active" || to == "rejected"
	case "active":
		return to == "suspended"
	case "suspended":
		return to == "active"
	}
	return false
}

func (s *Service) UpdateSellerStatus(ctx context.Context, userID, to, reason, adminID string) (*repository.SellerProfile, error) {
	switch to {
	case "active":
	case "suspended", "rejected":
		if strings.TrimSpace(reason) == "" {
			return nil, invalid("a reason is required when suspending or rejecting a seller")
		}
	default:
		return nil, invalid("status must be active, suspended, or rejected")
	}

	profile, err := s.repo.ChangeSellerStatus(ctx, userID, to, strings.TrimSpace(reason), adminID, allowedTransition)
	if errors.Is(err, repository.ErrInvalidState) {
		return nil, fmt.Errorf("%w: %v", ErrFailedPrecondition, err)
	}
	if err != nil {
		return nil, err
	}

	// After commit, best-effort. Notification Service will consume these.
	if perr := s.pub.Publish(ctx, userID, "user.seller_"+to,
		map[string]string{"user_id": userID, "reason": reason}); perr != nil {
		s.log.Warn("publish failed", "event", "user.seller_"+to, "user_id", userID, "error", perr)
	}
	return profile, nil
}

// ListSellers pages through seller profiles. pageToken is opaque to callers.
func (s *Service) ListSellers(ctx context.Context, status string, pageSize int, pageToken string) ([]repository.SellerProfile, string, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	var after *repository.Cursor
	if pageToken != "" {
		c, err := decodeCursor(pageToken)
		if err != nil {
			return nil, "", err
		}
		after = c
	}

	rows, err := s.repo.ListSellers(ctx, status, after, pageSize+1) // +1 reveals whether a next page exists
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		next = encodeCursor(repository.Cursor{CreatedAt: last.CreatedAt, ID: last.UserID})
	}
	return rows, next, nil
}

func encodeCursor(c repository.Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt.Format(time.RFC3339Nano) + "|" + c.ID))
}

func decodeCursor(token string) (*repository.Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, invalid("page_token is not valid")
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, invalid("page_token is not valid")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, invalid("page_token is not valid")
	}
	return &repository.Cursor{CreatedAt: t, ID: id}, nil
}
