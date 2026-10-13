package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	productv1 "github.com/roastco/ecommerce/gen/product/v1"
	searchv1 "github.com/roastco/ecommerce/gen/search/v1"
	"github.com/roastco/ecommerce/pkg/middleware"
)

// ListProducts handles GET /v1/products.
func (d *Deps) ListProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	resp, err := middleware.Do(r.Context(), d.ProductBreaker, func(ctx context.Context) (*productv1.ListProductsResponse, error) {
		return d.Product.ListProducts(ctx, &productv1.ListProductsRequest{
			SellerId:    q.Get("seller_id"),
			InStockOnly: q.Get("in_stock_only") == "true",
			Page: &commonv1.PageRequest{
				PageSize:  pageSize(r, 20),
				PageToken: q.Get("page_token"),
			},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeList(w, http.StatusOK, resp.Products, resp.Page.GetNextPageToken())
}

// GetProduct handles GET /v1/products/{productID}.
func (d *Deps) GetProduct(w http.ResponseWriter, r *http.Request) {
	productID := chi.URLParam(r, "productID")

	product, err := middleware.Do(r.Context(), d.ProductBreaker, func(ctx context.Context) (*productv1.Product, error) {
		return d.Product.GetProduct(ctx, &productv1.GetProductRequest{ProductId: productID})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeDataWithLinks(w, http.StatusOK, product, map[string]link{
		"self":    {Href: "/v1/products/" + productID},
		"reviews": {Href: "/v1/products/" + productID + "/reviews"},
	})
}

// SearchCatalog handles GET /v1/search. Falls back to ListProducts if Search's
// breaker is open - LLD.md §4.2 "degraded mode".
func (d *Deps) SearchCatalog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filters := map[string]string{}
	for _, key := range []string{"roast_level", "origin"} {
		if v := q.Get(key); v != "" {
			filters[key] = v
		}
	}

	resp, err := middleware.Do(r.Context(), d.SearchBreaker, func(ctx context.Context) (*searchv1.SearchProductsResponse, error) {
		return d.Search.SearchProducts(ctx, &searchv1.SearchProductsRequest{
			Query:       q.Get("q"),
			Filters:     filters,
			InStockOnly: q.Get("in_stock_only") == "true",
			Page: &commonv1.PageRequest{
				PageSize:  pageSize(r, 20),
				PageToken: q.Get("page_token"),
			},
		})
	})
	if err != nil {
		if middleware.IsCircuitOpen(err) {
			d.ListProducts(w, r) // degraded: lose relevance ranking/facets, keep basic browsing working
			return
		}
		writeError(w, r, err)
		return
	}

	writeEnvelope(w, http.StatusOK, envelope{
		Data: map[string]any{"hits": resp.Hits, "facets": resp.Facets},
		Page: &pageInfo{NextPageToken: resp.Page.GetNextPageToken()},
	})
}
