package service

import (
	"context"
	"testing"
	"time"

	"github.com/roastco/ecommerce/services/search/internal/repository"
)

func TestSearchProducts(t *testing.T) {
	repo := repository.New("") // in-memory mode
	svc := New(repo)
	ctx := context.Background()

	_ = repo.IndexProduct(ctx, repository.ProductDoc{
		ProductID:   "prod-1",
		SellerID:    "seller-1",
		Name:        "Ethiopia Yirgacheffe",
		Description: "Light floral roast",
		RoastLevel:  "light",
		Origin:      "Ethiopia",
		InStock:     true,
		UpdatedAt:   time.Now(),
	})
	_ = repo.IndexProduct(ctx, repository.ProductDoc{
		ProductID:   "prod-2",
		SellerID:    "seller-2",
		Name:        "Colombia Supremo",
		Description: "Medium dark roast",
		RoastLevel:  "medium",
		Origin:      "Colombia",
		InStock:     false,
		UpdatedAt:   time.Now(),
	})

	t.Run("Query match", func(t *testing.T) {
		out, err := svc.SearchProducts(ctx, SearchInput{
			Query: "Ethiopia",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out.Hits) != 1 {
			t.Fatalf("expected 1 hit, got %d", len(out.Hits))
		}
		if out.Hits[0].ProductID != "prod-1" {
			t.Errorf("expected prod-1, got %s", out.Hits[0].ProductID)
		}
	})

	t.Run("In stock only", func(t *testing.T) {
		out, err := svc.SearchProducts(ctx, SearchInput{
			InStockOnly: true,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out.Hits) != 1 {
			t.Fatalf("expected 1 in-stock hit, got %d", len(out.Hits))
		}
	})

	t.Run("Facets", func(t *testing.T) {
		out, err := svc.SearchProducts(ctx, SearchInput{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out.Hits) != 2 {
			t.Fatalf("expected 2 hits, got %d", len(out.Hits))
		}
		if out.Facets["roast_level"]["light"] != 1 {
			t.Errorf("expected 1 light roast in facets, got %d", out.Facets["roast_level"]["light"])
		}
	})
}
