package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/types/known/emptypb"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	productv1 "github.com/roastco/ecommerce/gen/product/v1"
	"github.com/roastco/ecommerce/pkg/middleware"
)

type createReviewRequest struct {
	ProductID string `json:"product_id"`
	Rating    int32  `json:"rating"`
	Comment   string `json:"comment"`
}

// CreateReview handles POST /v1/reviews. Product Service verifies purchase
// via a call to Order Service before accepting this - LLD.md §3.8.
func (d *Deps) CreateReview(w http.ResponseWriter, r *http.Request) {
	var req createReviewRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}
	buyerID := mustClaims(r).UserID

	review, err := middleware.Do(authContext(r), d.ProductBreaker, func(ctx context.Context) (*productv1.Review, error) {
		return d.Product.CreateReview(ctx, &productv1.CreateReviewRequest{
			ProductId: req.ProductID,
			BuyerId:   buyerID,
			Rating:    req.Rating,
			Comment:   req.Comment,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusCreated, review)
}

type updateReviewRequest struct {
	Rating  int32  `json:"rating"`
	Comment string `json:"comment"`
}

// UpdateReview handles PUT /v1/reviews/{reviewID}.
func (d *Deps) UpdateReview(w http.ResponseWriter, r *http.Request) {
	reviewID := chi.URLParam(r, "reviewID")
	var req updateReviewRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}
	buyerID := mustClaims(r).UserID

	review, err := middleware.Do(authContext(r), d.ProductBreaker, func(ctx context.Context) (*productv1.Review, error) {
		return d.Product.UpdateReview(ctx, &productv1.UpdateReviewRequest{
			ReviewId: reviewID,
			BuyerId:  buyerID,
			Rating:   req.Rating,
			Comment:  req.Comment,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusOK, review)
}

// DeleteReview handles DELETE /v1/reviews/{reviewID}.
func (d *Deps) DeleteReview(w http.ResponseWriter, r *http.Request) {
	reviewID := chi.URLParam(r, "reviewID")
	buyerID := mustClaims(r).UserID

	_, err := middleware.Do(authContext(r), d.ProductBreaker, func(ctx context.Context) (*emptypb.Empty, error) {
		return d.Product.DeleteReview(ctx, &productv1.DeleteReviewRequest{
			ReviewId: reviewID,
			BuyerId:  buyerID,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListReviews handles GET /v1/products/{productID}/reviews. Public - reading
// reviews needs no auth, same as browsing the product itself. Plain
// paginated read; no cross-service call (LLD.md §2.3).
func (d *Deps) ListReviews(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "productID")

	resp, err := middleware.Do(r.Context(), d.ProductBreaker, func(ctx context.Context) (*productv1.ListReviewsResponse, error) {
		return d.Product.ListReviews(ctx, &productv1.ListReviewsRequest{
			ProductId: productID,
			Page: &commonv1.PageRequest{
				PageSize:  pageSize(r, 20),
				PageToken: r.URL.Query().Get("page_token"),
			},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeList(w, http.StatusOK, resp.Reviews, resp.Page.GetNextPageToken())
}
