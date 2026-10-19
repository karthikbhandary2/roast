// Package grpcserver is the User Service's transport layer. It decodes proto
// requests, enforces who may call what, calls the service package, and maps
// the result (or error) back to proto / gRPC status codes. No business rules
// live here. (The directory is internal/grpc, but the package is named
// grpcserver so it doesn't collide with google.golang.org/grpc.)
package grpcserver

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	userv1 "github.com/roastco/ecommerce/gen/user/v1"
	"github.com/roastco/ecommerce/pkg/middleware"
	"github.com/roastco/ecommerce/services/user/internal/repository"
	"github.com/roastco/ecommerce/services/user/internal/service"
)

// publicMethods are callable without a token: login/registration, plus
// read-only lookups used by other internal services (Product, Payment, Order).
var publicMethods = map[string]bool{
	"/user.v1.UserService/Register":         true,
	"/user.v1.UserService/Authenticate":     true,
	"/user.v1.UserService/ValidateToken":    true,
	"/user.v1.UserService/GetSellerProfile": true,
	"/user.v1.UserService/GetUser":          true,
}

// AuthInterceptor requires a valid JWT on every method except publicMethods,
// reusing the shared interceptor from pkg/middleware for the actual check.
// For publicMethods, if a token is present, it extracts claims into context
// so methods like GetSellerProfile can still detect isOwnerOrAdmin.
func AuthInterceptor(secret []byte) grpc.UnaryServerInterceptor {
	auth := middleware.AuthUnaryInterceptor(secret)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if publicMethods[info.FullMethod] {
			// Try attaching claims if token is provided, but don't fail if absent
			_, _ = auth(ctx, req, info, func(c context.Context, _ any) (any, error) {
				ctx = c
				return nil, nil
			})
			return handler(ctx, req)
		}
		return auth(ctx, req, info, handler)
	}
}

type Server struct {
	userv1.UnimplementedUserServiceServer
	svc *service.Service
	log *slog.Logger
}

func New(svc *service.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

// ---------------------------------------------------------------- users

func (s *Server) Register(ctx context.Context, req *userv1.RegisterRequest) (*userv1.RegisterResponse, error) {
	u, err := s.svc.Register(ctx, req.GetEmail(), req.GetPassword(), req.GetDisplayName())
	if err != nil {
		return nil, s.fail(err)
	}
	return &userv1.RegisterResponse{User: toProtoUser(u)}, nil
}

func (s *Server) Authenticate(ctx context.Context, req *userv1.AuthenticateRequest) (*userv1.AuthenticateResponse, error) {
	t, err := s.svc.Authenticate(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, s.fail(err)
	}
	return &userv1.AuthenticateResponse{
		AccessToken:      t.AccessToken,
		RefreshToken:     t.RefreshToken,
		ExpiresInSeconds: int64(t.ExpiresIn.Seconds()),
	}, nil
}

func (s *Server) Logout(ctx context.Context, req *userv1.LogoutRequest) (*userv1.LogoutResponse, error) {
	if err := selfOrAdmin(ctx, req.GetUserId()); err != nil {
		return nil, err
	}
	if err := s.svc.Logout(ctx, req.GetUserId()); err != nil {
		return nil, s.fail(err)
	}
	return &userv1.LogoutResponse{Success: true}, nil
}

func (s *Server) ValidateToken(ctx context.Context, req *userv1.ValidateTokenRequest) (*userv1.ValidateTokenResponse, error) {
	info, err := s.svc.ValidateToken(ctx, req.GetAccessToken())
	if err != nil {
		return nil, s.fail(err)
	}
	resp := &userv1.ValidateTokenResponse{Valid: info.Valid, UserId: info.UserID}
	for _, r := range info.Roles {
		resp.Roles = append(resp.Roles, roleFromString(r))
	}
	return resp, nil
}

func (s *Server) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.User, error) {
	if err := selfOrAdmin(ctx, req.GetUserId()); err != nil {
		return nil, err
	}
	u, err := s.svc.GetUser(ctx, req.GetUserId())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoUser(u), nil
}

func (s *Server) UpdateProfile(ctx context.Context, req *userv1.UpdateProfileRequest) (*userv1.User, error) {
	if err := selfOrAdmin(ctx, req.GetUserId()); err != nil {
		return nil, err
	}
	u, err := s.svc.UpdateProfile(ctx, req.GetUserId(), req.GetDisplayName())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoUser(u), nil
}

// -------------------------------------------------------------- sellers

func (s *Server) RegisterSeller(ctx context.Context, req *userv1.RegisterSellerRequest) (*userv1.RegisterSellerResponse, error) {
	if err := selfOrAdmin(ctx, req.GetUserId()); err != nil {
		return nil, err
	}
	p, err := s.svc.RegisterSeller(ctx, service.RegisterSellerInput{
		UserID:           req.GetUserId(),
		BusinessName:     req.GetBusinessName(),
		Provider:         providerToString[req.GetProvider()],
		PayoutMethod:     req.GetPayoutMethod(),
		PayoutAccountRef: req.GetPayoutAccountRef(),
		ForceReonboard:   req.GetForceReonboard(),
	})
	if err != nil {
		return nil, s.fail(err)
	}
	return &userv1.RegisterSellerResponse{Profile: toProtoSeller(p, false)}, nil
}

// GetSellerProfile is callable by any authenticated caller, because Product
// and Payment look up a seller's provider while acting for a buyer. The
// payout account reference is only returned to the seller themselves or an
// admin.
func (s *Server) GetSellerProfile(ctx context.Context, req *userv1.GetSellerProfileRequest) (*userv1.SellerProfile, error) {
	p, err := s.svc.GetSellerProfile(ctx, req.GetUserId())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoSeller(p, selfOrAdmin(ctx, req.GetUserId()) != nil), nil
}

func (s *Server) UpdateSellerPayoutInfo(ctx context.Context, req *userv1.UpdateSellerPayoutInfoRequest) (*userv1.SellerProfile, error) {
	if err := selfOrAdmin(ctx, req.GetUserId()); err != nil {
		return nil, err
	}
	p, err := s.svc.UpdateSellerPayoutInfo(ctx, req.GetUserId(), req.GetPayoutMethod(), req.GetPayoutAccountRef())
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoSeller(p, false), nil
}

// UpdateSellerStatus is admin-only. The audited admin id comes from the
// caller's verified token, never from the request body, so it can't be spoofed.
func (s *Server) UpdateSellerStatus(ctx context.Context, req *userv1.UpdateSellerStatusRequest) (*userv1.SellerProfile, error) {
	if err := middleware.RequireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	claims, _ := middleware.ClaimsFromContext(ctx)
	p, err := s.svc.UpdateSellerStatus(ctx, req.GetUserId(), sellerStatusToString[req.GetStatus()], req.GetReason(), claims.UserID)
	if err != nil {
		return nil, s.fail(err)
	}
	return toProtoSeller(p, false), nil
}

func (s *Server) ListSellers(ctx context.Context, req *userv1.ListSellersRequest) (*userv1.ListSellersResponse, error) {
	if err := middleware.RequireRole(ctx, "admin"); err != nil {
		return nil, err
	}
	rows, next, err := s.svc.ListSellers(ctx, sellerStatusToString[req.GetStatusFilter()],
		int(req.GetPage().GetPageSize()), req.GetPage().GetPageToken())
	if err != nil {
		return nil, s.fail(err)
	}
	resp := &userv1.ListSellersResponse{Page: &commonv1.PageResponse{NextPageToken: next}}
	for i := range rows {
		resp.Sellers = append(resp.Sellers, toProtoSeller(&rows[i], false))
	}
	return resp, nil
}

// ------------------------------------------------------------ authz util

// selfOrAdmin allows the caller to act on their own user id, or any id if
// they hold the admin role. It returns a ready-made gRPC status error.
func selfOrAdmin(ctx context.Context, targetUserID string) error {
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "no authenticated caller")
	}
	if claims.UserID == targetUserID {
		return nil
	}
	return middleware.RequireRole(ctx, "admin")
}

// fail maps service/repository errors to gRPC status codes. Anything
// unrecognised is logged in full and returned as a generic Internal error.
func (s *Server) fail(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, service.ErrInvalidCredentials):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, service.ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, service.ErrUnimplemented):
		return status.Error(codes.Unimplemented, err.Error())
	case errors.Is(err, repository.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, repository.ErrEmailTaken), errors.Is(err, repository.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	}
	s.log.Error("internal error", "error", err)
	return status.Error(codes.Internal, "internal error")
}

// ----------------------------------------------------- proto conversions

var roleToString = map[userv1.Role]string{
	userv1.Role_ROLE_BUYER:  "buyer",
	userv1.Role_ROLE_SELLER: "seller",
	userv1.Role_ROLE_ADMIN:  "admin",
}

func roleFromString(s string) userv1.Role {
	for r, name := range roleToString {
		if name == s {
			return r
		}
	}
	return userv1.Role_ROLE_UNSPECIFIED
}

// An unset enum (UNSPECIFIED) maps to "" - no filter on lists, and rejected
// as invalid by the service on writes.
var sellerStatusToString = map[userv1.SellerStatus]string{
	userv1.SellerStatus_SELLER_STATUS_PENDING_REVIEW: "pending_review",
	userv1.SellerStatus_SELLER_STATUS_ACTIVE:         "active",
	userv1.SellerStatus_SELLER_STATUS_SUSPENDED:      "suspended",
	userv1.SellerStatus_SELLER_STATUS_REJECTED:       "rejected",
}

func sellerStatusFromString(s string) userv1.SellerStatus {
	for st, name := range sellerStatusToString {
		if name == s {
			return st
		}
	}
	return userv1.SellerStatus_SELLER_STATUS_UNSPECIFIED
}

var providerToString = map[commonv1.PaymentProvider]string{
	commonv1.PaymentProvider_PAYMENT_PROVIDER_RAZORPAY: "phonepe",
	commonv1.PaymentProvider_PAYMENT_PROVIDER_STRIPE:   "stripe",
}

func providerFromString(s string) commonv1.PaymentProvider {
	if s == "phonepe" || s == "razorpay" {
		return commonv1.PaymentProvider_PAYMENT_PROVIDER_RAZORPAY
	}
	for p, name := range providerToString {
		if name == s {
			return p
		}
	}
	return commonv1.PaymentProvider_PAYMENT_PROVIDER_UNSPECIFIED
}

func toProtoUser(u *repository.User) *userv1.User {
	out := &userv1.User{
		Id:          u.ID,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		CreatedAt:   timestamppb.New(u.CreatedAt),
	}
	for _, r := range u.Roles {
		out.Roles = append(out.Roles, roleFromString(r))
	}
	return out
}

func toProtoSeller(p *repository.SellerProfile, redactPayoutRef bool) *userv1.SellerProfile {
	ref := p.PayoutAccountRef
	if redactPayoutRef {
		ref = ""
	}
	return &userv1.SellerProfile{
		UserId:           p.UserID,
		BusinessName:     p.BusinessName,
		Provider:         providerFromString(p.Provider),
		PayoutMethod:     p.PayoutMethod,
		PayoutAccountRef: ref,
		CommissionRate:   p.CommissionRate,
		Status:           sellerStatusFromString(p.Status),
	}
}
