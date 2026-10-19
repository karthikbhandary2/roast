package handlers

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	orderv1 "github.com/roastco/ecommerce/gen/order/v1"
	paymentv1 "github.com/roastco/ecommerce/gen/payment/v1"
	productv1 "github.com/roastco/ecommerce/gen/product/v1"
	userv1 "github.com/roastco/ecommerce/gen/user/v1"
	"github.com/roastco/ecommerce/pkg/middleware"
)

var providerFromString = map[string]commonv1.PaymentProvider{
	"phonepe":  commonv1.PaymentProvider_PAYMENT_PROVIDER_RAZORPAY,
	"razorpay": commonv1.PaymentProvider_PAYMENT_PROVIDER_RAZORPAY,
	"stripe":   commonv1.PaymentProvider_PAYMENT_PROVIDER_STRIPE,
}

type sellerOnboardRequest struct {
	BusinessName     string `json:"business_name"`
	Provider         string `json:"provider"` // "razorpay" | "stripe"
	PayoutMethod     string `json:"payout_method"`
	PayoutAccountRef string `json:"payout_account_ref"`
	ForceReonboard   bool   `json:"force_reonboard"`
}

// SellerOnboard handles POST /v1/seller/onboarding - LLD.md §2.2, §3.6.
func (d *Deps) SellerOnboard(w http.ResponseWriter, r *http.Request) {
	var req sellerOnboardRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}
	userID := mustClaims(r).UserID

	resp, err := middleware.Do(authContext(r), d.UserBreaker, func(ctx context.Context) (*userv1.RegisterSellerResponse, error) {
		return d.User.RegisterSeller(ctx, &userv1.RegisterSellerRequest{
			UserId:           userID,
			BusinessName:     req.BusinessName,
			Provider:         providerFromString[req.Provider],
			PayoutMethod:     req.PayoutMethod,
			PayoutAccountRef: req.PayoutAccountRef,
			ForceReonboard:   req.ForceReonboard,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusCreated, resp.Profile)
}

// SellerDashboard handles GET /v1/seller/dashboard. This is a COMPOSED
// endpoint, not a pure proxy - it aggregates two downstream calls into one
// response, which is a legitimate gateway responsibility (avoiding a
// multi-request round trip from the client for one screen). Rating is
// intentionally omitted - no seller-level rating aggregate is designed yet
// (reviews are per-product; see LLD.md §9 for related open items).
func (d *Deps) SellerDashboard(w http.ResponseWriter, r *http.Request) {
	sellerID := mustClaims(r).UserID

	balance, err := middleware.Do(authContext(r), d.PaymentBreaker, func(ctx context.Context) (*paymentv1.SellerBalance, error) {
		return d.Payment.GetSellerBalance(ctx, &paymentv1.GetSellerBalanceRequest{SellerId: sellerID})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	awaitingFulfillment, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.ListSubOrdersForSellerResponse, error) {
		return d.Order.ListSubOrdersForSeller(ctx, &orderv1.ListSubOrdersForSellerRequest{
			SellerId:     sellerID,
			StatusFilter: orderv1.SubOrderStatus_SUB_ORDER_STATUS_CONFIRMED,
			Page:         &commonv1.PageRequest{PageSize: 50},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	listings, err := middleware.Do(authContext(r), d.ProductBreaker, func(ctx context.Context) (*productv1.ListProductsResponse, error) {
		return d.Product.ListProducts(ctx, &productv1.ListProductsRequest{
			SellerId: sellerID,
			Page:     &commonv1.PageRequest{PageSize: 100},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	lowStockCount := 0
	for _, p := range listings.Products {
		if p.Status == productv1.ProductStatus_PRODUCT_STATUS_LOW_STOCK {
			lowStockCount++
		}
	}

	writeData(w, http.StatusOK, map[string]any{
		"pending_payout":               balance.Pending,
		"available_payout":             balance.Available,
		"orders_awaiting_fulfillment":  len(awaitingFulfillment.SubOrders),
		"listings_low_on_stock":        lowStockCount,
	})
}

// SellerListings handles GET /v1/seller/listings.
func (d *Deps) SellerListings(w http.ResponseWriter, r *http.Request) {
	sellerID := mustClaims(r).UserID

	resp, err := middleware.Do(authContext(r), d.ProductBreaker, func(ctx context.Context) (*productv1.ListProductsResponse, error) {
		return d.Product.ListProducts(ctx, &productv1.ListProductsRequest{
			SellerId: sellerID,
			Page:     &commonv1.PageRequest{PageSize: pageSize(r, 20), PageToken: r.URL.Query().Get("page_token")},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeList(w, http.StatusOK, resp.Products, resp.Page.GetNextPageToken())
}

type createListingRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Price        struct {
		CurrencyCode string `json:"currency_code"`
		AmountMinor  int64  `json:"amount_minor"`
	} `json:"price"`
	BatchLabel   string `json:"batch_label"`
	InitialStock int32  `json:"initial_stock"`
}

var currencyFromString = map[string]commonv1.CurrencyCode{
	"USD": commonv1.CurrencyCode_CURRENCY_CODE_USD,
	"INR": commonv1.CurrencyCode_CURRENCY_CODE_INR,
	"GBP": commonv1.CurrencyCode_CURRENCY_CODE_GBP,
	"JPY": commonv1.CurrencyCode_CURRENCY_CODE_JPY,
}

// CreateListing handles POST /v1/seller/listings. Product Service checks
// price currency against the seller's provider - LLD.md §2.3, §4.6.
func (d *Deps) CreateListing(w http.ResponseWriter, r *http.Request) {
	var req createListingRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}
	sellerID := mustClaims(r).UserID

	product, err := middleware.Do(authContext(r), d.ProductBreaker, func(ctx context.Context) (*productv1.Product, error) {
		return d.Product.CreateListing(ctx, &productv1.CreateListingRequest{
			SellerId:    sellerID,
			Name:        req.Name,
			Description: req.Description,
			Price: &commonv1.Money{
				CurrencyCode: currencyFromString[req.Price.CurrencyCode],
				AmountMinor:  req.Price.AmountMinor,
			},
			BatchLabel:   req.BatchLabel,
			InitialStock: req.InitialStock,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusCreated, product)
}

type imageUploadURLRequest struct {
	ContentType string `json:"content_type"`
}

// ImageUploadURL handles POST /v1/seller/listings/images/upload-url -
// LLD.md §2.3, §3.7. The returned upload_url is PUT directly to object
// storage by the client; this call never sees image bytes.
func (d *Deps) ImageUploadURL(w http.ResponseWriter, r *http.Request) {
	var req imageUploadURLRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}
	sellerID := mustClaims(r).UserID

	resp, err := middleware.Do(authContext(r), d.ProductBreaker, func(ctx context.Context) (*productv1.GetImageUploadURLResponse, error) {
		return d.Product.GetImageUploadURL(ctx, &productv1.GetImageUploadURLRequest{
			SellerId:    sellerID,
			ContentType: req.ContentType,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusOK, resp)
}

// SellerOrders handles GET /v1/seller/orders - sub-orders belonging to this
// seller only, across all buyers' parent orders (LLD.md service table).
func (d *Deps) SellerOrders(w http.ResponseWriter, r *http.Request) {
	sellerID := mustClaims(r).UserID

	resp, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.ListSubOrdersForSellerResponse, error) {
		return d.Order.ListSubOrdersForSeller(ctx, &orderv1.ListSubOrdersForSellerRequest{
			SellerId: sellerID,
			Page:     &commonv1.PageRequest{PageSize: pageSize(r, 20), PageToken: r.URL.Query().Get("page_token")},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeList(w, http.StatusOK, resp.SubOrders, resp.Page.GetNextPageToken())
}

type markShippedRequest struct {
	TrackingNumber string `json:"tracking_number"`
}

// MarkShipped handles POST /v1/seller/orders/{subOrderID}/ship.
func (d *Deps) MarkShipped(w http.ResponseWriter, r *http.Request) {
	subOrderID := chi.URLParam(r, "subOrderID")
	var req markShippedRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, errInvalidJSON)
		return
	}
	sellerID := mustClaims(r).UserID

	subOrder, err := middleware.Do(authContext(r), d.OrderBreaker, func(ctx context.Context) (*orderv1.SubOrder, error) {
		return d.Order.MarkSubOrderShipped(ctx, &orderv1.MarkSubOrderShippedRequest{
			SubOrderId:     subOrderID,
			SellerId:       sellerID,
			TrackingNumber: req.TrackingNumber,
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeData(w, http.StatusOK, subOrder)
}

// SellerPayouts handles GET /v1/seller/payouts.
func (d *Deps) SellerPayouts(w http.ResponseWriter, r *http.Request) {
	sellerID := mustClaims(r).UserID

	resp, err := middleware.Do(authContext(r), d.PaymentBreaker, func(ctx context.Context) (*paymentv1.ListPayoutsResponse, error) {
		return d.Payment.ListPayouts(ctx, &paymentv1.ListPayoutsRequest{
			SellerId: sellerID,
			Page:     &commonv1.PageRequest{PageSize: pageSize(r, 20), PageToken: r.URL.Query().Get("page_token")},
		})
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeList(w, http.StatusOK, resp.Payouts, resp.Page.GetNextPageToken())
}
