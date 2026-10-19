package grpcserver

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	paymentv1 "github.com/roastco/ecommerce/gen/payment/v1"
	"github.com/roastco/ecommerce/pkg/money"
	"github.com/roastco/ecommerce/services/payment/internal/service"
)

type Server struct {
	paymentv1.UnimplementedPaymentServiceServer
	svc *service.Service
	log *slog.Logger
}

func New(svc *service.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

func (s *Server) fail(err error) error {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return status.Error(codes.NotFound, "not found")
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

func (s *Server) ChargePayment(ctx context.Context, req *paymentv1.ChargePaymentRequest) (*paymentv1.ChargePaymentResponse, error) {
	curr, amt, err := money.ProtoToMoney(req.GetAmount())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid amount: %v", err)
	}

	key := req.GetMetadata().GetIdempotencyKey()
	if key == "" {
		key = req.GetSubOrderId() + ":charge"
	}

	methodStr := "card"
	if req.GetPaymentMethod() == paymentv1.PaymentMethod_PAYMENT_METHOD_UPI {
		methodStr = "upi"
	} else if req.GetPaymentMethod() == paymentv1.PaymentMethod_PAYMENT_METHOD_NET_BANKING {
		methodStr = "net_banking"
	}

	res, err := s.svc.ChargePayment(
		ctx,
		key,
		req.GetSubOrderId(),
		req.GetBuyerId(),
		req.GetSellerId(),
		curr,
		amt,
		req.GetPaymentMethodToken(),
		methodStr,
	)
	if err != nil {
		return nil, s.fail(err)
	}

	chargeStatus := paymentv1.ChargeStatus_CHARGE_STATUS_SUCCEEDED
	if res.Status == "PENDING" {
		chargeStatus = paymentv1.ChargeStatus_CHARGE_STATUS_PENDING
	} else if res.Status == "FAILED" {
		chargeStatus = paymentv1.ChargeStatus_CHARGE_STATUS_FAILED
	}

	return &paymentv1.ChargePaymentResponse{
		ChargeId:      res.ChargeID,
		Status:        chargeStatus,
		PlatformFee:   money.MoneyToProto(curr, res.PlatformFeeMinor),
		SellerShare:   money.MoneyToProto(curr, res.SellerShareMinor),
		FailureReason: res.FailureReason,
		Provider:      money.StringToPaymentProvider(res.Provider),
	}, nil
}

func (s *Server) RefundPayment(ctx context.Context, req *paymentv1.RefundPaymentRequest) (*paymentv1.RefundPaymentResponse, error) {
	key := req.GetMetadata().GetIdempotencyKey()
	if key == "" {
		key = req.GetChargeId() + ":refund"
	}

	if err := s.svc.RefundPayment(ctx, key, req.GetChargeId(), req.GetReason()); err != nil {
		return nil, s.fail(err)
	}
	return &paymentv1.RefundPaymentResponse{
		Status: commonv1.SagaStepStatus_SAGA_STEP_STATUS_COMPLETED,
	}, nil
}

func (s *Server) GetSellerBalance(ctx context.Context, req *paymentv1.GetSellerBalanceRequest) (*paymentv1.SellerBalance, error) {
	b, err := s.svc.GetSellerBalance(ctx, req.GetSellerId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &paymentv1.SellerBalance{
		SellerId:  b.SellerID,
		Available: money.MoneyToProto(b.Currency, b.AvailableMinor),
		Pending:   money.MoneyToProto(b.Currency, b.PendingMinor),
	}, nil
}

func (s *Server) RequestPayout(ctx context.Context, req *paymentv1.RequestPayoutRequest) (*paymentv1.Payout, error) {
	p, err := s.svc.RequestPayout(ctx, req.GetSellerId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &paymentv1.Payout{
		Id:        p.ID,
		SellerId:  p.SellerID,
		Amount:    money.MoneyToProto(p.Currency, p.AmountMinor),
		Method:    p.Method,
		Status:    p.Status,
		Provider:  money.StringToPaymentProvider(p.Provider),
		CreatedAt: timestamppb.New(p.CreatedAt),
	}, nil
}

func (s *Server) ListPayouts(ctx context.Context, req *paymentv1.ListPayoutsRequest) (*paymentv1.ListPayoutsResponse, error) {
	pageReq := req.GetPage()
	pageSize := 20
	pageToken := ""
	if pageReq != nil {
		pageSize = int(pageReq.GetPageSize())
		pageToken = pageReq.GetPageToken()
	}

	payouts, nextToken, err := s.svc.ListPayouts(ctx, req.GetSellerId(), pageSize, pageToken)
	if err != nil {
		return nil, s.fail(err)
	}

	var protoList []*paymentv1.Payout
	for _, p := range payouts {
		protoList = append(protoList, &paymentv1.Payout{
			Id:        p.ID,
			SellerId:  p.SellerID,
			Amount:    money.MoneyToProto(p.Currency, p.AmountMinor),
			Method:    p.Method,
			Status:    p.Status,
			Provider:  money.StringToPaymentProvider(p.Provider),
			CreatedAt: timestamppb.New(p.CreatedAt),
		})
	}

	return &paymentv1.ListPayoutsResponse{
		Payouts: protoList,
		Page: &commonv1.PageResponse{
			NextPageToken: nextToken,
		},
	}, nil
}
