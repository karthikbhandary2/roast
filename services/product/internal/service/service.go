package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orderv1 "github.com/roastco/ecommerce/gen/order/v1"
	userv1 "github.com/roastco/ecommerce/gen/user/v1"
	"github.com/roastco/ecommerce/pkg/idempotency"
	"github.com/roastco/ecommerce/pkg/money"
	"github.com/roastco/ecommerce/services/product/internal/repository"
	"google.golang.org/grpc"
)

var (
	ErrNotFound           = repository.ErrNotFound
	ErrAlreadyExists      = repository.ErrAlreadyExists
	ErrInvalidArgument    = errors.New("invalid argument")
	ErrPermissionDenied   = errors.New("permission denied")
	ErrFailedPrecondition = errors.New("failed precondition")
)

func invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidArgument, msg) }

type UserClient interface {
	GetSellerProfile(ctx context.Context, in *userv1.GetSellerProfileRequest, opts ...grpc.CallOption) (*userv1.SellerProfile, error)
}

type OrderClient interface {
	VerifyPurchase(ctx context.Context, in *orderv1.VerifyPurchaseRequest, opts ...grpc.CallOption) (*orderv1.VerifyPurchaseResponse, error)
}

type Publisher interface {
	Publish(ctx context.Context, aggregateID, eventType string, payload any) error
}

type NoopPublisher struct{}

func (NoopPublisher) Publish(context.Context, string, string, any) error { return nil }

type Config struct {
	ObjectStorageEndpoint string
	ObjectStorageBucket   string
	ReservationHoldTTL    time.Duration
}

type Service struct {
	repo        *repository.Repository
	cfg         Config
	pub         Publisher
	userClient  UserClient
	orderClient OrderClient
	log         *slog.Logger
}

func New(
	repo *repository.Repository,
	cfg Config,
	pub Publisher,
	userClient UserClient,
	orderClient OrderClient,
	log *slog.Logger,
) *Service {
	if cfg.ReservationHoldTTL <= 0 {
		cfg.ReservationHoldTTL = 10 * time.Minute
	}
	return &Service{
		repo:        repo,
		cfg:         cfg,
		pub:         pub,
		userClient:  userClient,
		orderClient: orderClient,
		log:         log,
	}
}

// ---------------------------------------------------------------- listings

type CreateListingInput struct {
	SellerID     string
	Name         string
	Description  string
	Currency     string
	PriceMinor   int64
	BatchLabel   string
	InitialStock int32
}

func (s *Service) validateCurrencyForSeller(ctx context.Context, sellerID, currency string) error {
	if s.userClient == nil {
		return nil
	}
	profile, err := s.userClient.GetSellerProfile(ctx, &userv1.GetSellerProfileRequest{UserId: sellerID})
	if err != nil {
		return fmt.Errorf("service: verify seller profile: %w", err)
	}
	providerStr := money.PaymentProviderToString(profile.GetProvider())
	if !money.ValidateCurrencyForProvider(currency, providerStr) {
		return fmt.Errorf("%w: currency %s is not permitted for seller on %s", ErrFailedPrecondition, currency, providerStr)
	}
	return nil
}

func (s *Service) CreateListing(ctx context.Context, in CreateListingInput) (*repository.Product, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return nil, invalid("name must be 1-200 characters")
	}
	if in.PriceMinor < 0 {
		return nil, invalid("price cannot be negative")
	}
	if in.InitialStock < 0 {
		return nil, invalid("initial_stock cannot be negative")
	}
	if in.Currency != money.USD && in.Currency != money.INR && in.Currency != money.GBP && in.Currency != money.JPY {
		return nil, invalid("unsupported currency")
	}

	if err := s.validateCurrencyForSeller(ctx, in.SellerID, in.Currency); err != nil {
		return nil, err
	}

	p, err := s.repo.InsertProduct(ctx, s.repo.DB(), repository.NewProduct{
		SellerID:    in.SellerID,
		Name:        in.Name,
		Description: strings.TrimSpace(in.Description),
		Currency:    in.Currency,
		PriceMinor:  in.PriceMinor,
		BatchLabel:  strings.TrimSpace(in.BatchLabel),
		Stock:       in.InitialStock,
	})
	if err != nil {
		return nil, err
	}

	_ = s.pub.Publish(ctx, p.ID, "product.created", p)
	return p, nil
}

type UpdateListingInput struct {
	ProductID   string
	SellerID    string
	Name        string
	Description string
	Currency    string
	PriceMinor  int64
	ImageURLs   []string
	Status      string
}

func (s *Service) UpdateListing(ctx context.Context, in UpdateListingInput) (*repository.Product, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return nil, invalid("name must be 1-200 characters")
	}
	if in.PriceMinor < 0 {
		return nil, invalid("price cannot be negative")
	}

	if in.Currency != "" {
		if err := s.validateCurrencyForSeller(ctx, in.SellerID, in.Currency); err != nil {
			return nil, err
		}
	}

	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	existing, err := s.repo.GetProduct(ctx, tx, in.ProductID, true)
	if err != nil {
		return nil, err
	}
	if existing.SellerID != in.SellerID {
		return nil, ErrPermissionDenied
	}

	existingImagesMap := make(map[string]bool)
	for _, u := range existing.ImageURLs {
		existingImagesMap[u] = true
	}

	existing.Name = in.Name
	existing.Description = strings.TrimSpace(in.Description)
	if in.Currency != "" {
		existing.Currency = in.Currency
	}
	existing.PriceMinor = in.PriceMinor
	if in.Status != "" {
		existing.Status = in.Status
	}

	updated, err := s.repo.UpdateProduct(ctx, tx, existing)
	if err != nil {
		return nil, err
	}

	if err := s.repo.ReplaceImages(ctx, tx, in.ProductID, in.ImageURLs); err != nil {
		return nil, err
	}
	updated.ImageURLs = in.ImageURLs

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("service: commit update: %w", err)
	}

	_ = s.pub.Publish(ctx, updated.ID, "product.updated", updated)

	for _, u := range in.ImageURLs {
		if !existingImagesMap[u] {
			_ = s.pub.Publish(ctx, updated.ID, "product.image_uploaded", map[string]string{
				"product_id": updated.ID,
				"seller_id":  updated.SellerID,
				"url":        u,
			})
		}
	}

	return updated, nil
}

func (s *Service) GetProduct(ctx context.Context, id string) (*repository.Product, error) {
	return s.repo.GetProduct(ctx, s.repo.DB(), id, false)
}

func (s *Service) ListProducts(
	ctx context.Context, sellerID string, inStockOnly, includeDrafts bool, pageSize int, pageToken string,
) ([]*repository.Product, string, error) {
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

	rows, err := s.repo.ListProducts(ctx, s.repo.DB(), repository.ListFilter{
		SellerID:      sellerID,
		IncludeDrafts: includeDrafts,
		InStockOnly:   inStockOnly,
		After:         after,
		Limit:         pageSize + 1,
	})
	if err != nil {
		return nil, "", err
	}

	next := ""
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		next = encodeCursor(repository.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return rows, next, nil
}

// ------------------------------------------------------------- image upload

type ImageUploadURLResponse struct {
	UploadURL        string
	PublicURL        string
	ExpiresInSeconds int64
}

func (s *Service) GetImageUploadURL(ctx context.Context, sellerID, contentType string) (*ImageUploadURLResponse, error) {
	switch contentType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, invalid("content_type must be image/jpeg, image/png, or image/webp")
	}

	imageID := uuid.NewString()
	ext := ".jpg"
	if contentType == "image/png" {
		ext = ".png"
	} else if contentType == "image/webp" {
		ext = ".webp"
	}

	endpoint := s.cfg.ObjectStorageEndpoint
	if endpoint == "" {
		endpoint = "http://minio:9000"
	}
	bucket := s.cfg.ObjectStorageBucket
	if bucket == "" {
		bucket = "product-images"
	}

	path := fmt.Sprintf("%s/%s/%s%s", bucket, sellerID, imageID, ext)
	uploadURL := fmt.Sprintf("%s/%s?upload_id=%s", endpoint, path, uuid.NewString())
	publicURL := fmt.Sprintf("%s/%s", endpoint, path)

	return &ImageUploadURLResponse{
		UploadURL:        uploadURL,
		PublicURL:        publicURL,
		ExpiresInSeconds: 900,
	}, nil
}

// ------------------------------------------------------------- saga steps

type ReservationResult struct {
	ReservationID string `json:"reservation_id"`
	Status        string `json:"status"` // COMPLETED | FAILED
	FailureReason string `json:"failure_reason,omitempty"`
}

type ReservationLineInput struct {
	ProductID string
	Quantity  int32
}

func (s *Service) ReserveInventory(
	ctx context.Context, idempotencyKeyStr string, subOrderID, sellerID string, lines []ReservationLineInput,
) (*ReservationResult, error) {
	if idempotencyKeyStr == "" {
		return nil, invalid("missing idempotency key")
	}
	if len(lines) == 0 {
		return nil, invalid("empty reservation lines")
	}

	idMap := make(map[string]bool)
	var pids []string
	for _, l := range lines {
		if l.Quantity <= 0 {
			return nil, invalid("quantity must be > 0")
		}
		if !idMap[l.ProductID] {
			idMap[l.ProductID] = true
			pids = append(pids, l.ProductID)
		}
	}
	sort.Strings(pids)

	res, err := idempotency.Run(ctx, s.repo.Pool(), "idempotency_keys", idempotencyKeyStr, "reserve",
		func(ctx context.Context, tx pgx.Tx) (ReservationResult, error) {
			prods, err := s.repo.LockProducts(ctx, tx, pids)
			if err != nil {
				return ReservationResult{}, err
			}

			for _, l := range lines {
				p, ok := prods[l.ProductID]
				if !ok {
					return ReservationResult{Status: "FAILED", FailureReason: "product_not_found:" + l.ProductID}, nil
				}
				if p.StockAvailable() < l.Quantity {
					return ReservationResult{Status: "FAILED", FailureReason: "insufficient_stock:" + l.ProductID}, nil
				}
			}

			var repoLines []repository.ReservationLine
			for _, l := range lines {
				repoLines = append(repoLines, repository.ReservationLine{
					ProductID: l.ProductID,
					Quantity:  l.Quantity,
				})
			}

			expiresAt := time.Now().Add(s.cfg.ReservationHoldTTL)
			resID, err := s.repo.CreateReservation(ctx, tx, subOrderID, sellerID, expiresAt, repoLines)
			if err != nil {
				return ReservationResult{}, err
			}

			return ReservationResult{
				ReservationID: resID,
				Status:        "COMPLETED",
			}, nil
		},
	)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (s *Service) ReleaseInventory(ctx context.Context, idempotencyKeyStr, reservationID string) error {
	if idempotencyKeyStr == "" {
		return invalid("missing idempotency key")
	}
	_, err := idempotency.Run(ctx, s.repo.Pool(), "idempotency_keys", idempotencyKeyStr, "release",
		func(ctx context.Context, tx pgx.Tx) (bool, error) {
			pids, err := s.repo.ReleaseReservation(ctx, tx, reservationID)
			if err != nil {
				return false, err
			}
			for _, pid := range pids {
				_ = s.pub.Publish(ctx, pid, "inventory.changed", map[string]string{"product_id": pid})
			}
			return true, nil
		},
	)
	return err
}

func (s *Service) ConfirmReservation(ctx context.Context, idempotencyKeyStr, reservationID string) error {
	if idempotencyKeyStr == "" {
		return invalid("missing idempotency key")
	}
	_, err := idempotency.Run(ctx, s.repo.Pool(), "idempotency_keys", idempotencyKeyStr, "confirm",
		func(ctx context.Context, tx pgx.Tx) (bool, error) {
			pids, err := s.repo.ConfirmReservation(ctx, tx, reservationID)
			if err != nil {
				return false, err
			}
			for _, pid := range pids {
				_ = s.pub.Publish(ctx, pid, "inventory.changed", map[string]string{"product_id": pid})
			}
			return true, nil
		},
	)
	return err
}

func (s *Service) SweepExpiredReservations(ctx context.Context) error {
	pids, err := s.repo.SweepExpiredReservations(ctx, s.repo.Pool())
	if err != nil {
		return err
	}
	for _, pid := range pids {
		_ = s.pub.Publish(ctx, pid, "inventory.changed", map[string]string{"product_id": pid})
	}
	return nil
}

// ------------------------------------------------------------- reviews

func (s *Service) CreateReview(
	ctx context.Context, productID, buyerID string, rating int32, comment string,
) (*repository.Review, error) {
	if rating < 1 || rating > 5 {
		return nil, invalid("rating must be between 1 and 5")
	}

	if s.orderClient != nil {
		vpResp, err := s.orderClient.VerifyPurchase(ctx, &orderv1.VerifyPurchaseRequest{
			BuyerId:   buyerID,
			ProductId: productID,
		})
		if err != nil {
			return nil, fmt.Errorf("service: verify purchase: %w", err)
		}
		if !vpResp.GetPurchased() {
			return nil, ErrPermissionDenied
		}
		return s.repo.CreateReview(ctx, s.repo.DB(), productID, buyerID, vpResp.GetSubOrderId(), rating, strings.TrimSpace(comment))
	}

	return s.repo.CreateReview(ctx, s.repo.DB(), productID, buyerID, uuid.Nil.String(), rating, strings.TrimSpace(comment))
}

func (s *Service) UpdateReview(
	ctx context.Context, reviewID, buyerID string, rating int32, comment string,
) (*repository.Review, error) {
	if rating < 1 || rating > 5 {
		return nil, invalid("rating must be between 1 and 5")
	}
	return s.repo.UpdateReview(ctx, s.repo.DB(), reviewID, buyerID, rating, strings.TrimSpace(comment))
}

func (s *Service) DeleteReview(ctx context.Context, reviewID, buyerID string) error {
	return s.repo.DeleteReview(ctx, s.repo.DB(), reviewID, buyerID)
}

func (s *Service) ListReviews(
	ctx context.Context, productID string, pageSize int, pageToken string,
) ([]*repository.Review, string, error) {
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

	rows, err := s.repo.ListReviews(ctx, s.repo.DB(), productID, after, pageSize+1)
	if err != nil {
		return nil, "", err
	}

	next := ""
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		next = encodeCursor(repository.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return rows, next, nil
}

// ------------------------------------------------------------- helpers

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
