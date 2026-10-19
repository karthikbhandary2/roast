package grpcserver

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	productv1 "github.com/roastco/ecommerce/gen/product/v1"
	"github.com/roastco/ecommerce/pkg/money"
	"github.com/roastco/ecommerce/services/product/internal/repository"
	"github.com/roastco/ecommerce/services/product/internal/service"
)

type Server struct {
	productv1.UnimplementedProductServiceServer
	svc *service.Service
	log *slog.Logger
}

func New(svc *service.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

func (s *Server) fail(err error) error {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return status.Error(codes.NotFound, "product or review not found")
	case errors.Is(err, service.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, "already exists")
	case errors.Is(err, service.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, service.ErrPermissionDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, service.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		s.log.Error("internal error", "error", err)
		return status.Error(codes.Internal, "internal server error")
	}
}

func toProtoStatus(storedStatus string, available int32) productv1.ProductStatus {
	if storedStatus == "draft" {
		return productv1.ProductStatus_PRODUCT_STATUS_DRAFT
	}
	if available <= 0 {
		return productv1.ProductStatus_PRODUCT_STATUS_SOLD_OUT
	}
	if available <= 5 {
		return productv1.ProductStatus_PRODUCT_STATUS_LOW_STOCK
	}
	return productv1.ProductStatus_PRODUCT_STATUS_LIVE
}

func toProtoProduct(p *repository.Product) *productv1.Product {
	avail := p.StockAvailable()
	return &productv1.Product{
		Id:             p.ID,
		SellerId:       p.SellerID,
		Name:           p.Name,
		Description:    p.Description,
		Price:          money.MoneyToProto(p.Currency, p.PriceMinor),
		BatchLabel:     p.BatchLabel,
		StockAvailable: avail,
		StockReserved:  p.StockReserved,
		Status:         toProtoStatus(p.Status, avail),
		UpdatedAt:      timestamppb.New(p.UpdatedAt),
		ImageUrls:      p.ImageURLs,
	}
}

func toProtoReview(r *repository.Review) *productv1.Review {
	return &productv1.Review{
		Id:         r.ID,
		ProductId:  r.ProductID,
		BuyerId:    r.BuyerID,
		SubOrderId: r.SubOrderID,
		Rating:     r.Rating,
		Comment:    r.Comment,
		CreatedAt:  timestamppb.New(r.CreatedAt),
	}
}

func (s *Server) CreateListing(ctx context.Context, req *productv1.CreateListingRequest) (*productv1.Product, error) {
	curr, amt, err := money.ProtoToMoney(req.GetPrice())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid price: %v", err)
	}
	p, err := s.svc.CreateListing(ctx, service.CreateListingInput{
		SellerID:     req.GetSellerId(),
		Name:         req.GetName(),
		Description:  req.GetDescription(),
		Currency:     curr,
		PriceMinor:   amt,
		BatchLabel:   req.GetBatchLabel(),
		InitialStock: req.GetInitialStock(),
	})
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoProduct(p), nil
}

func (s *Server) UpdateListing(ctx context.Context, req *productv1.UpdateListingRequest) (*productv1.Product, error) {
	var curr string
	var amt int64
	if req.GetPrice() != nil {
		c, a, err := money.ProtoToMoney(req.GetPrice())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid price: %v", err)
		}
		curr = c
		amt = a
	}
	statusStr := ""
	if req.GetStatus() == productv1.ProductStatus_PRODUCT_STATUS_DRAFT {
		statusStr = "draft"
	} else if req.GetStatus() == productv1.ProductStatus_PRODUCT_STATUS_LIVE {
		statusStr = "live"
	}

	p, err := s.svc.UpdateListing(ctx, service.UpdateListingInput{
		ProductID:   req.GetProductId(),
		SellerID:    req.GetSellerId(),
		Name:        req.GetName(),
		Description: req.GetDescription(),
		Currency:    curr,
		PriceMinor:  amt,
		ImageURLs:   req.GetImageUrls(),
		Status:      statusStr,
	})
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoProduct(p), nil
}

func (s *Server) GetProduct(ctx context.Context, req *productv1.GetProductRequest) (*productv1.Product, error) {
	p, err := s.svc.GetProduct(ctx, req.GetProductId())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoProduct(p), nil
}

func (s *Server) ListProducts(ctx context.Context, req *productv1.ListProductsRequest) (*productv1.ListProductsResponse, error) {
	pageReq := req.GetPage()
	pageSize := 20
	pageToken := ""
	if pageReq != nil {
		pageSize = int(pageReq.GetPageSize())
		pageToken = pageReq.GetPageToken()
	}

	products, nextToken, err := s.svc.ListProducts(
		ctx,
		req.GetSellerId(),
		req.GetInStockOnly(),
		req.GetSellerId() != "", // sellers can see their own drafts
		pageSize,
		pageToken,
	)
	if err != nil {
		return nil, s.fail(err)
	}

	var protoList []*productv1.Product
	for _, p := range products {
		protoList = append(protoList, toProtoProduct(p))
	}
	return &productv1.ListProductsResponse{
		Products: protoList,
		Page: &commonv1.PageResponse{
			NextPageToken: nextToken,
		},
	}, nil
}

func (s *Server) GetImageUploadURL(ctx context.Context, req *productv1.GetImageUploadURLRequest) (*productv1.GetImageUploadURLResponse, error) {
	res, err := s.svc.GetImageUploadURL(ctx, req.GetSellerId(), req.GetContentType())
	if err != nil {
		return nil, s.fail(err)
	}
	return &productv1.GetImageUploadURLResponse{
		UploadUrl:        res.UploadURL,
		PublicUrl:        res.PublicURL,
		ExpiresInSeconds: res.ExpiresInSeconds,
	}, nil
}

func (s *Server) ReserveInventory(ctx context.Context, req *productv1.ReserveInventoryRequest) (*productv1.ReserveInventoryResponse, error) {
	var lines []service.ReservationLineInput
	for _, l := range req.GetLines() {
		lines = append(lines, service.ReservationLineInput{
			ProductID: l.GetProductId(),
			Quantity:  l.GetQuantity(),
		})
	}

	key := req.GetMetadata().GetIdempotencyKey()
	if key == "" {
		key = req.GetSubOrderId() + ":reserve"
	}

	res, err := s.svc.ReserveInventory(ctx, key, req.GetSubOrderId(), req.GetSellerId(), lines)
	if err != nil {
		return nil, s.fail(err)
	}

	stepStatus := commonv1.SagaStepStatus_SAGA_STEP_STATUS_COMPLETED
	if res.Status == "FAILED" {
		stepStatus = commonv1.SagaStepStatus_SAGA_STEP_STATUS_FAILED
	}

	return &productv1.ReserveInventoryResponse{
		ReservationId: res.ReservationID,
		Status:        stepStatus,
		FailureReason: res.FailureReason,
	}, nil
}

func (s *Server) ReleaseInventory(ctx context.Context, req *productv1.ReleaseInventoryRequest) (*productv1.ReleaseInventoryResponse, error) {
	key := req.GetMetadata().GetIdempotencyKey()
	if key == "" {
		key = req.GetReservationId() + ":release"
	}
	if err := s.svc.ReleaseInventory(ctx, key, req.GetReservationId()); err != nil {
		return nil, s.fail(err)
	}
	return &productv1.ReleaseInventoryResponse{
		Status: commonv1.SagaStepStatus_SAGA_STEP_STATUS_COMPLETED,
	}, nil
}

func (s *Server) ConfirmReservation(ctx context.Context, req *productv1.ConfirmReservationRequest) (*productv1.ConfirmReservationResponse, error) {
	key := req.GetMetadata().GetIdempotencyKey()
	if key == "" {
		key = req.GetReservationId() + ":confirm"
	}
	if err := s.svc.ConfirmReservation(ctx, key, req.GetReservationId()); err != nil {
		return nil, s.fail(err)
	}
	return &productv1.ConfirmReservationResponse{
		Status: commonv1.SagaStepStatus_SAGA_STEP_STATUS_COMPLETED,
	}, nil
}

func (s *Server) CreateReview(ctx context.Context, req *productv1.CreateReviewRequest) (*productv1.Review, error) {
	rev, err := s.svc.CreateReview(ctx, req.GetProductId(), req.GetBuyerId(), req.GetRating(), req.GetComment())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoReview(rev), nil
}

func (s *Server) UpdateReview(ctx context.Context, req *productv1.UpdateReviewRequest) (*productv1.Review, error) {
	rev, err := s.svc.UpdateReview(ctx, req.GetReviewId(), req.GetBuyerId(), req.GetRating(), req.GetComment())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoReview(rev), nil
}

func (s *Server) DeleteReview(ctx context.Context, req *productv1.DeleteReviewRequest) (*emptypb.Empty, error) {
	if err := s.svc.DeleteReview(ctx, req.GetReviewId(), req.GetBuyerId()); err != nil {
		return nil, s.fail(err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) ListReviews(ctx context.Context, req *productv1.ListReviewsRequest) (*productv1.ListReviewsResponse, error) {
	pageReq := req.GetPage()
	pageSize := 20
	pageToken := ""
	if pageReq != nil {
		pageSize = int(pageReq.GetPageSize())
		pageToken = pageReq.GetPageToken()
	}

	reviews, nextToken, err := s.svc.ListReviews(ctx, req.GetProductId(), pageSize, pageToken)
	if err != nil {
		return nil, s.fail(err)
	}

	var protoList []*productv1.Review
	for _, r := range reviews {
		protoList = append(protoList, toProtoReview(r))
	}
	return &productv1.ListReviewsResponse{
		Reviews: protoList,
		Page: &commonv1.PageResponse{
			NextPageToken: nextToken,
		},
	}, nil
}
