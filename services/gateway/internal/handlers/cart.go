package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	orderv1 "github.com/roastco/ecommerce/gen/order/v1"
	"github.com/roastco/ecommerce/pkg/middleware"
)

// GetCart handles GET /v1/cart.
func (d *Deps) GetCart(w http.ResponseWriter, r *http.Request) {
	buyerID := mustClaims(r).UserID

	cart, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.Cart, error) {
		return d.Order.GetCart(ctx, &orderv1.GetCartRequest{BuyerId: buyerID})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusOK, cart)
}

type addToCartRequest struct {
	ProductID string `json:"product_id"`
	Quantity  int32  `json:"quantity"`
}

// AddToCart handles POST /v1/cart/items.
func (d *Deps) AddToCart(w http.ResponseWriter, r *http.Request) {
	var req addToCartRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}
	buyerID := mustClaims(r).UserID

	cart, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.Cart, error) {
		return d.Order.AddToCart(ctx, &orderv1.AddToCartRequest{
			BuyerId:   buyerID,
			ProductId: req.ProductID,
			Quantity:  req.Quantity,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusOK, cart)
}

type updateCartItemRequest struct {
	Quantity int32 `json:"quantity"`
}

// UpdateCartItem handles PUT /v1/cart/items/{productID}. Quantity 0 removes
// the item, per order.proto's UpdateCartItemRequest comment.
func (d *Deps) UpdateCartItem(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "productID")
	var req updateCartItemRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}
	buyerID := mustClaims(r).UserID

	cart, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.Cart, error) {
		return d.Order.UpdateCartItem(ctx, &orderv1.UpdateCartItemRequest{
			BuyerId:   buyerID,
			ProductId: productID,
			Quantity:  req.Quantity,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusOK, cart)
}
