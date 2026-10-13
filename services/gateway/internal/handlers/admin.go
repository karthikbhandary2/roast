package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	orderv1 "github.com/roastco/ecommerce/gen/order/v1"
	productv1 "github.com/roastco/ecommerce/gen/product/v1"
	userv1 "github.com/roastco/ecommerce/gen/user/v1"
	"github.com/roastco/ecommerce/pkg/middleware"
)

// AdminInventory handles GET /v1/admin/inventory - all products, no
// seller_id filter (ListProductsRequest treats an empty SellerId as "no
// filter" - product.proto's comment on that field).
func (d *Deps) AdminInventory(w http.ResponseWriter, r *http.Request) {
	resp, err := middleware.Do(authContext(r), d.ProductBreaker, func(ctx context.Context) (*productv1.ListProductsResponse, error) {
		return d.Product.ListProducts(ctx, &productv1.ListProductsRequest{
			Page: &commonv1.PageRequest{PageSize: pageSize(r, 50), PageToken: r.URL.Query().Get("page_token")},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeList(w, http.StatusOK, resp.Products, resp.Page.GetNextPageToken())
}

// AdminOrders handles GET /v1/admin/orders.
func (d *Deps) AdminOrders(w http.ResponseWriter, r *http.Request) {
	resp, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.ListAllOrdersResponse, error) {
		return d.Order.ListAllOrders(ctx, &orderv1.ListAllOrdersRequest{
			Page: &commonv1.PageRequest{PageSize: pageSize(r, 50), PageToken: r.URL.Query().Get("page_token")},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeList(w, http.StatusOK, resp.Orders, resp.Page.GetNextPageToken())
}

var sellerStatusFromString = map[string]userv1.SellerStatus{
	"pending_review": userv1.SellerStatus_SELLER_STATUS_PENDING_REVIEW,
	"active":         userv1.SellerStatus_SELLER_STATUS_ACTIVE,
	"suspended":      userv1.SellerStatus_SELLER_STATUS_SUSPENDED,
	"rejected":       userv1.SellerStatus_SELLER_STATUS_REJECTED,
}

// AdminListSellers handles GET /v1/admin/sellers?status=pending_review -
// the admin "sellers awaiting review" queue (LLD.md §2.2, §3.6).
func (d *Deps) AdminListSellers(w http.ResponseWriter, r *http.Request) {
	statusFilter := sellerStatusFromString[r.URL.Query().Get("status")] // zero value = unspecified = no filter

	resp, err := middleware.Do(authContext(r), d.UserBreaker, func(ctx context.Context) (*userv1.ListSellersResponse, error) {
		return d.User.ListSellers(ctx, &userv1.ListSellersRequest{
			StatusFilter: statusFilter,
			Page:         &commonv1.PageRequest{PageSize: pageSize(r, 50), PageToken: r.URL.Query().Get("page_token")},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeList(w, http.StatusOK, resp.Sellers, resp.Page.GetNextPageToken())
}

type reviewSellerRequest struct {
	Status string `json:"status"` // "active" | "rejected" | "suspended"
	Reason string `json:"reason"`
}

// AdminReviewSeller handles POST /v1/admin/sellers/{userID}/review -
// approve, reject, or suspend a seller (LLD.md §2.2, §3.6, §3.10).
func (d *Deps) AdminReviewSeller(w http.ResponseWriter, r *http.Request) {
	targetUserID := chi.URLParam(r, "userID")
	var req reviewSellerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}
	adminID := mustClaims(r).UserID

	profile, err := middleware.Do(authContext(r), d.UserBreaker, func(ctx context.Context) (*userv1.SellerProfile, error) {
		return d.User.UpdateSellerStatus(ctx, &userv1.UpdateSellerStatusRequest{
			UserId:       targetUserID,
			Status:       sellerStatusFromString[req.Status],
			Reason:       req.Reason,
			AdminUserId:  adminID,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusOK, profile)
}
