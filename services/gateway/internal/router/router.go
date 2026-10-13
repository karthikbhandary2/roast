// Package router builds the gateway's HTTP route table. Route groups
// mirror the screens: auth, catalog/search, cart, checkout, orders,
// reviews, seller, admin. All business logic lives in internal/handlers -
// this file is wiring only.
package router

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/roastco/ecommerce/services/gateway/internal/handlers"
	gatewaymw "github.com/roastco/ecommerce/services/gateway/internal/middleware"
)

// New builds the full route table.
func New(deps *handlers.Deps, log *slog.Logger, jwtSecret []byte, allowedOrigin string) http.Handler {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{allowedOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "Idempotency-Key", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
	}))
	r.Use(gatewaymw.RequestLogger(log))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	r.Route("/v1", func(r chi.Router) {
		// --- Public: auth + browsing, no token required ---
		r.Post("/auth/register", deps.Register)
		r.Post("/auth/login", deps.Login)
		r.Get("/products", deps.ListProducts)
		r.Get("/products/{productID}", deps.GetProduct)
		r.Get("/products/{productID}/reviews", deps.ListReviews)
		r.Get("/search", deps.SearchCatalog)

		// --- Requires a valid JWT ---
		r.Group(func(r chi.Router) {
			r.Use(gatewaymw.RequireAuth(jwtSecret))

			r.Post("/auth/logout", deps.Logout)

			r.Get("/cart", deps.GetCart)
			r.Post("/cart/items", deps.AddToCart)
			r.Put("/cart/items/{productID}", deps.UpdateCartItem)

			r.Post("/checkout", deps.Checkout)
			r.Get("/orders", deps.ListOrders)
			r.Get("/orders/{orderID}", deps.GetOrder)
			r.Get("/orders/{orderID}/events", deps.OrderEvents) // SSE
			r.Post("/orders/{orderID}/sub-orders/{subOrderID}/confirm-delivery", deps.ConfirmDelivery)

			r.Post("/reviews", deps.CreateReview)
			r.Put("/reviews/{reviewID}", deps.UpdateReview)
			r.Delete("/reviews/{reviewID}", deps.DeleteReview)

			// --- Seller-only. RequireAuth only checks the token is valid;
			// the role check happens downstream via pkg/middleware.RequireRole
			// against the forwarded JWT, inside the service that owns the
			// RPC - see LLD.md §2.2 "Token revocation" for why the gateway
			// itself doesn't also do a stale-role check here. ---
			r.Route("/seller", func(r chi.Router) {
				r.Post("/onboarding", deps.SellerOnboard)
				r.Get("/dashboard", deps.SellerDashboard)
				r.Get("/listings", deps.SellerListings)
				r.Post("/listings", deps.CreateListing)
				r.Post("/listings/images/upload-url", deps.ImageUploadURL)
				r.Get("/orders", deps.SellerOrders)
				r.Post("/orders/{subOrderID}/ship", deps.MarkShipped)
				r.Get("/payouts", deps.SellerPayouts)
			})

			// --- Admin-only, same role-check-happens-downstream pattern. ---
			r.Route("/admin", func(r chi.Router) {
				r.Get("/inventory", deps.AdminInventory)
				r.Get("/orders", deps.AdminOrders)
				r.Get("/sellers", deps.AdminListSellers)
				r.Post("/sellers/{userID}/review", deps.AdminReviewSeller)
			})
		})
	})

	return r
}
