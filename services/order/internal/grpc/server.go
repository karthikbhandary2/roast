package grpcserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	orderv1 "github.com/roastco/ecommerce/gen/order/v1"
	"github.com/roastco/ecommerce/pkg/money"
	"github.com/roastco/ecommerce/pkg/redisclient"
	"github.com/roastco/ecommerce/services/order/internal/repository"
	"github.com/roastco/ecommerce/services/order/internal/service"
)

type Server struct {
	orderv1.UnimplementedOrderServiceServer
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

func toProtoOrderStatus(s string) orderv1.OrderStatus {
	switch s {
	case "processing":
		return orderv1.OrderStatus_ORDER_STATUS_PROCESSING
	case "confirmed":
		return orderv1.OrderStatus_ORDER_STATUS_CONFIRMED
	case "partially_failed":
		return orderv1.OrderStatus_ORDER_STATUS_PARTIALLY_FAILED
	case "failed":
		return orderv1.OrderStatus_ORDER_STATUS_FAILED
	default:
		return orderv1.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func toProtoSubOrderStatus(s string) orderv1.SubOrderStatus {
	switch s {
	case "reserving":
		return orderv1.SubOrderStatus_SUB_ORDER_STATUS_RESERVING
	case "charging":
		return orderv1.SubOrderStatus_SUB_ORDER_STATUS_CHARGING
	case "awaiting_payment":
		return orderv1.SubOrderStatus_SUB_ORDER_STATUS_AWAITING_PAYMENT
	case "confirmed":
		return orderv1.SubOrderStatus_SUB_ORDER_STATUS_CONFIRMED
	case "compensating":
		return orderv1.SubOrderStatus_SUB_ORDER_STATUS_COMPENSATING
	case "failed":
		return orderv1.SubOrderStatus_SUB_ORDER_STATUS_FAILED
	case "shipped":
		return orderv1.SubOrderStatus_SUB_ORDER_STATUS_SHIPPED
	case "delivered":
		return orderv1.SubOrderStatus_SUB_ORDER_STATUS_DELIVERED
	default:
		return orderv1.SubOrderStatus_SUB_ORDER_STATUS_UNSPECIFIED
	}
}

func toProtoCart(buyerID string, items []redisclient.CartItem, expiresAt time.Time) *orderv1.Cart {
	var protoItems []*orderv1.CartItem
	for _, it := range items {
		protoItems = append(protoItems, &orderv1.CartItem{
			ProductId: it.ProductID,
			SellerId:  it.SellerID,
			Quantity:  it.Quantity,
			UnitPrice: money.MoneyToProto(it.Currency, it.UnitPrice),
		})
	}
	return &orderv1.Cart{
		BuyerId:   buyerID,
		Items:     protoItems,
		ExpiresAt: timestamppb.New(expiresAt),
	}
}

func toProtoSubOrder(so *repository.SubOrder) *orderv1.SubOrder {
	var items []*orderv1.CartItem
	for _, it := range so.Items {
		items = append(items, &orderv1.CartItem{
			ProductId: it.ProductID,
			SellerId:  so.SellerID,
			Quantity:  it.Quantity,
			UnitPrice: money.MoneyToProto(it.UnitCurrency, it.UnitMinor),
		})
	}
	return &orderv1.SubOrder{
		Id:            so.ID,
		ParentOrderId: so.ParentOrderID,
		SellerId:      so.SellerID,
		Items:         items,
		Subtotal:      money.MoneyToProto(so.SubtotalCurrency, so.SubtotalMinor),
		Status:        toProtoSubOrderStatus(so.Status),
		ReservationId: so.ReservationID,
		FailureReason: so.FailureReason,
	}
}

func toProtoOrder(o *repository.Order) *orderv1.Order {
	var addr commonv1.Address
	if len(o.ShippingAddress) > 0 {
		_ = json.Unmarshal(o.ShippingAddress, &addr)
	}

	var subs []*orderv1.SubOrder
	for _, s := range o.SubOrders {
		subs = append(subs, toProtoSubOrder(s))
	}

	return &orderv1.Order{
		Id:              o.ID,
		BuyerId:         o.BuyerID,
		ShippingAddress: &addr,
		SubOrders:       subs,
		Total:           money.MoneyToProto(o.TotalCurrency, o.TotalMinor),
		Status:          toProtoOrderStatus(o.Status),
		CreatedAt:       timestamppb.New(o.CreatedAt),
	}
}

// ------------------------------------------------------------- cart

func (s *Server) AddToCart(ctx context.Context, req *orderv1.AddToCartRequest) (*orderv1.Cart, error) {
	items, exp, err := s.svc.AddToCart(ctx, req.GetBuyerId(), req.GetProductId(), req.GetQuantity())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoCart(req.GetBuyerId(), items, exp), nil
}

func (s *Server) UpdateCartItem(ctx context.Context, req *orderv1.UpdateCartItemRequest) (*orderv1.Cart, error) {
	items, exp, err := s.svc.UpdateCartItem(ctx, req.GetBuyerId(), req.GetProductId(), req.GetQuantity())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoCart(req.GetBuyerId(), items, exp), nil
}

func (s *Server) GetCart(ctx context.Context, req *orderv1.GetCartRequest) (*orderv1.Cart, error) {
	items, exp, err := s.svc.GetCart(ctx, req.GetBuyerId())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoCart(req.GetBuyerId(), items, exp), nil
}

// ------------------------------------------------------------- checkout

func (s *Server) Checkout(ctx context.Context, req *orderv1.CheckoutRequest) (*orderv1.CheckoutResponse, error) {
	addrBytes, err := json.Marshal(req.GetShippingAddress())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid shipping address: %v", err)
	}

	key := req.GetMetadata().GetIdempotencyKey()
	ord, err := s.svc.Checkout(
		ctx,
		key,
		req.GetBuyerId(),
		addrBytes,
		req.GetPaymentMethodToken(),
		req.GetPaymentMethod(),
	)
	if err != nil {
		return nil, s.fail(err)
	}

	return &orderv1.CheckoutResponse{
		Order: toProtoOrder(ord),
	}, nil
}

func (s *Server) GetOrder(ctx context.Context, req *orderv1.GetOrderRequest) (*orderv1.Order, error) {
	ord, err := s.svc.GetOrder(ctx, req.GetOrderId())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoOrder(ord), nil
}

func (s *Server) ListOrdersForBuyer(ctx context.Context, req *orderv1.ListOrdersForBuyerRequest) (*orderv1.ListOrdersForBuyerResponse, error) {
	pageReq := req.GetPage()
	pageSize := 20
	pageToken := ""
	if pageReq != nil {
		pageSize = int(pageReq.GetPageSize())
		pageToken = pageReq.GetPageToken()
	}

	orders, nextToken, err := s.svc.ListOrdersForBuyer(ctx, req.GetBuyerId(), pageSize, pageToken)
	if err != nil {
		return nil, s.fail(err)
	}

	var protoList []*orderv1.Order
	for _, o := range orders {
		protoList = append(protoList, toProtoOrder(o))
	}
	return &orderv1.ListOrdersForBuyerResponse{
		Orders: protoList,
		Page: &commonv1.PageResponse{
			NextPageToken: nextToken,
		},
	}, nil
}

func (s *Server) ListSubOrdersForSeller(
	ctx context.Context, req *orderv1.ListSubOrdersForSellerRequest,
) (*orderv1.ListSubOrdersForSellerResponse, error) {
	pageReq := req.GetPage()
	pageSize := 20
	pageToken := ""
	if pageReq != nil {
		pageSize = int(pageReq.GetPageSize())
		pageToken = pageReq.GetPageToken()
	}

	subs, nextToken, err := s.svc.ListSubOrdersForSeller(ctx, req.GetSellerId(), req.GetStatusFilter(), pageSize, pageToken)
	if err != nil {
		return nil, s.fail(err)
	}

	var protoList []*orderv1.SubOrder
	for _, so := range subs {
		protoList = append(protoList, toProtoSubOrder(so))
	}
	return &orderv1.ListSubOrdersForSellerResponse{
		SubOrders: protoList,
		Page: &commonv1.PageResponse{
			NextPageToken: nextToken,
		},
	}, nil
}

func (s *Server) MarkSubOrderShipped(ctx context.Context, req *orderv1.MarkSubOrderShippedRequest) (*orderv1.SubOrder, error) {
	so, err := s.svc.MarkSubOrderShipped(ctx, req.GetSubOrderId(), req.GetSellerId(), req.GetTrackingNumber())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoSubOrder(so), nil
}

func (s *Server) ListAllOrders(ctx context.Context, req *orderv1.ListAllOrdersRequest) (*orderv1.ListAllOrdersResponse, error) {
	pageReq := req.GetPage()
	pageSize := 20
	pageToken := ""
	if pageReq != nil {
		pageSize = int(pageReq.GetPageSize())
		pageToken = pageReq.GetPageToken()
	}

	orders, nextToken, err := s.svc.ListAllOrders(ctx, req.GetStatusFilter(), pageSize, pageToken)
	if err != nil {
		return nil, s.fail(err)
	}

	var protoList []*orderv1.Order
	for _, o := range orders {
		protoList = append(protoList, toProtoOrder(o))
	}
	return &orderv1.ListAllOrdersResponse{
		Orders: protoList,
		Page: &commonv1.PageResponse{
			NextPageToken: nextToken,
		},
	}, nil
}

func (s *Server) ConfirmDelivery(ctx context.Context, req *orderv1.ConfirmDeliveryRequest) (*orderv1.SubOrder, error) {
	so, err := s.svc.ConfirmDelivery(ctx, req.GetSubOrderId(), req.GetBuyerId())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoSubOrder(so), nil
}

func (s *Server) VerifyPurchase(ctx context.Context, req *orderv1.VerifyPurchaseRequest) (*orderv1.VerifyPurchaseResponse, error) {
	purchased, subOrderID, err := s.svc.VerifyPurchase(ctx, req.GetBuyerId(), req.GetProductId())
	if err != nil {
		return nil, s.fail(err)
	}
	return &orderv1.VerifyPurchaseResponse{
		Purchased:  purchased,
		SubOrderId: subOrderID,
	}, nil
}
