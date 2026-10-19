package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/roastco/ecommerce/pkg/kafkaclient"
	"github.com/roastco/ecommerce/services/search/internal/repository"
)

type Indexer struct {
	repo    *repository.Repository
	brokers []string
	log     *slog.Logger
}

func New(repo *repository.Repository, brokers []string, log *slog.Logger) *Indexer {
	return &Indexer{
		repo:    repo,
		brokers: brokers,
		log:     log,
	}
}

func (idx *Indexer) Start(ctx context.Context) {
	if len(idx.brokers) == 0 {
		return
	}

	// 1. Consumer for product.events
	go func() {
		c := kafkaclient.NewConsumer(idx.brokers, "product.events", "search-indexer")
		defer c.Close()

		err := c.Consume(ctx, func(cctx context.Context, evt kafkaclient.Event) error {
			switch evt.Type {
			case "product.created", "product.updated":
				var p struct {
					ID            string    `json:"id"`
					SellerID      string    `json:"seller_id"`
					Name          string    `json:"name"`
					Description   string    `json:"description"`
					Currency      string    `json:"price_currency"`
					PriceMinor    int64     `json:"price_amount_minor"`
					BatchLabel    string    `json:"batch_label"`
					StockTotal    int32     `json:"stock_total"`
					StockReserved int32     `json:"stock_reserved"`
					Status        string    `json:"status"`
					UpdatedAt     time.Time `json:"updated_at"`
				}
				if err := json.Unmarshal(evt.Payload, &p); err != nil {
					return nil // bad message, don't block
				}
				stockAvail := p.StockTotal - p.StockReserved
				inStock := stockAvail > 0

				// Extract roast_level and origin if present in description or name
				roast := extractRoastLevel(p.Name, p.Description)
				origin := extractOrigin(p.Name, p.Description)

				return idx.repo.IndexProduct(cctx, repository.ProductDoc{
					ProductID:      p.ID,
					SellerID:       p.SellerID,
					Name:           p.Name,
					Description:    p.Description,
					PriceMinor:     p.PriceMinor,
					Currency:       p.Currency,
					BatchLabel:     p.BatchLabel,
					RoastLevel:     roast,
					Origin:         origin,
					InStock:        inStock,
					StockAvailable: stockAvail,
					Status:         p.Status,
					UpdatedAt:      p.UpdatedAt,
				})

			case "product.deleted":
				var p struct {
					ProductID string `json:"product_id"`
				}
				if err := json.Unmarshal(evt.Payload, &p); err == nil && p.ProductID != "" {
					return idx.repo.DeleteProduct(cctx, p.ProductID)
				}
			}
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			idx.log.Warn("product.events indexer loop ended", "error", err)
		}
	}()

	// 2. Consumer for inventory.events
	go func() {
		c := kafkaclient.NewConsumer(idx.brokers, "inventory.events", "search-indexer")
		defer c.Close()

		err := c.Consume(ctx, func(cctx context.Context, evt kafkaclient.Event) error {
			if evt.Type == "inventory.changed" {
				var p struct {
					ProductID      string `json:"product_id"`
					StockAvailable int32  `json:"stock_available"`
				}
				if err := json.Unmarshal(evt.Payload, &p); err == nil && p.ProductID != "" {
					inStock := p.StockAvailable > 0
					return idx.repo.UpdateStock(cctx, p.ProductID, p.StockAvailable, inStock)
				}
			}
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			idx.log.Warn("inventory.events indexer loop ended", "error", err)
		}
	}()
}

func extractRoastLevel(name, desc string) string {
	combined := strings.ToLower(name + " " + desc)
	switch {
	case strings.Contains(combined, "light"):
		return "light"
	case strings.Contains(combined, "medium"):
		return "medium"
	case strings.Contains(combined, "dark"):
		return "dark"
	default:
		return "medium"
	}
}

func extractOrigin(name, desc string) string {
	origins := []string{"Ethiopia", "Colombia", "Guatemala", "Kenya", "Brazil", "Costa Rica", "India", "Yemen"}
	combined := name + " " + desc
	for _, o := range origins {
		if strings.Contains(strings.ToLower(combined), strings.ToLower(o)) {
			return o
		}
	}
	return "Single Origin"
}
