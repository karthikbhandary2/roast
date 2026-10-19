package grpcserver

import (
	"context"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notificationv1 "github.com/roastco/ecommerce/gen/notification/v1"
	"github.com/roastco/ecommerce/services/notification/internal/service"
)

type Server struct {
	notificationv1.UnimplementedNotificationServiceServer
	svc *service.Service
	log *slog.Logger
}

func New(svc *service.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

func (s *Server) SendOrderConfirmation(ctx context.Context, req *notificationv1.SendOrderConfirmationRequest) (*notificationv1.SendResponse, error) {
	n, err := s.svc.Enqueue(ctx, req.GetBuyerId(), "order_confirmation", req.GetOrderId(), "email")
	if err != nil {
		s.log.Error("enqueue order confirmation failed", "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}
	return &notificationv1.SendResponse{
		Queued:         true,
		NotificationId: n.ID,
	}, nil
}

func (s *Server) SendShippingUpdate(ctx context.Context, req *notificationv1.SendShippingUpdateRequest) (*notificationv1.SendResponse, error) {
	n, err := s.svc.Enqueue(ctx, req.GetBuyerId(), "shipping_update", req.GetSubOrderId(), "email")
	if err != nil {
		s.log.Error("enqueue shipping update failed", "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}
	return &notificationv1.SendResponse{
		Queued:         true,
		NotificationId: n.ID,
	}, nil
}

func (s *Server) SendLowStockAlert(ctx context.Context, req *notificationv1.SendLowStockAlertRequest) (*notificationv1.SendResponse, error) {
	n, err := s.svc.Enqueue(ctx, req.GetSellerId(), "low_stock_alert", req.GetProductId(), "email")
	if err != nil {
		s.log.Error("enqueue low stock alert failed", "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}
	return &notificationv1.SendResponse{
		Queued:         true,
		NotificationId: n.ID,
	}, nil
}

func (s *Server) SendPayoutNotification(ctx context.Context, req *notificationv1.SendPayoutNotificationRequest) (*notificationv1.SendResponse, error) {
	n, err := s.svc.Enqueue(ctx, req.GetSellerId(), "payout_notification", req.GetPayoutId(), "email")
	if err != nil {
		s.log.Error("enqueue payout notification failed", "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}
	return &notificationv1.SendResponse{
		Queued:         true,
		NotificationId: n.ID,
	}, nil
}
