package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ProductDoc struct {
	ProductID      string    `json:"product_id"`
	SellerID       string    `json:"seller_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	PriceMinor     int64     `json:"price_amount_minor"`
	Currency       string    `json:"price_currency"`
	BatchLabel     string    `json:"batch_label"`
	RoastLevel     string    `json:"roast_level"`
	Origin         string    `json:"origin"`
	InStock        bool      `json:"in_stock"`
	StockAvailable int32     `json:"stock_available"`
	Status         string    `json:"status"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SearchFilter struct {
	Query       string
	Filters     map[string]string
	InStockOnly bool
	Limit       int
	Offset      int
}

type SearchResult struct {
	Hits   []ProductDoc
	Facets map[string]map[string]int
	Total  int
}

type Repository struct {
	esURL      string
	httpClient *http.Client

	// In-memory store fallback for standalone testing or offline dev
	mu     sync.RWMutex
	memory map[string]ProductDoc
}

func New(esURL string) *Repository {
	return &Repository{
		esURL:      strings.TrimRight(esURL, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		memory:     make(map[string]ProductDoc),
	}
}

func (r *Repository) EnsureIndex(ctx context.Context) error {
	if r.esURL == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, r.esURL+"/products", nil)
	if err != nil {
		return err
	}
	resp, err := r.httpClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
	}

	mapping := `{
		"settings": { "number_of_shards": 1, "number_of_replicas": 0 },
		"mappings": {
			"properties": {
				"product_id": { "type": "keyword" },
				"seller_id": { "type": "keyword" },
				"name": { "type": "text" },
				"description": { "type": "text" },
				"price_amount_minor": { "type": "long" },
				"price_currency": { "type": "keyword" },
				"batch_label": { "type": "keyword" },
				"roast_level": { "type": "keyword" },
				"origin": { "type": "keyword" },
				"in_stock": { "type": "boolean" },
				"stock_available": { "type": "integer" },
				"status": { "type": "keyword" },
				"updated_at": { "type": "date" }
			}
		}
	}`
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, r.esURL+"/products", strings.NewReader(mapping))
	if err != nil {
		return err
	}
	putReq.Header.Set("Content-Type", "application/json")
	putResp, err := r.httpClient.Do(putReq)
	if err != nil {
		return nil // Ignore network failure on setup (memory fallback will be active)
	}
	_ = putResp.Body.Close()
	return nil
}

func (r *Repository) IndexProduct(ctx context.Context, doc ProductDoc) error {
	r.mu.Lock()
	r.memory[doc.ProductID] = doc
	r.mu.Unlock()

	if r.esURL == "" {
		return nil
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/products/_doc/%s", r.esURL, doc.ProductID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil // fallback memory succeeds
	}
	_ = resp.Body.Close()
	return nil
}

func (r *Repository) UpdateStock(ctx context.Context, productID string, stockAvailable int32, inStock bool) error {
	r.mu.Lock()
	if doc, ok := r.memory[productID]; ok {
		doc.StockAvailable = stockAvailable
		doc.InStock = inStock
		r.memory[productID] = doc
	}
	r.mu.Unlock()

	if r.esURL == "" {
		return nil
	}

	updateBody := map[string]any{
		"doc": map[string]any{
			"stock_available": stockAvailable,
			"in_stock":        inStock,
		},
	}
	data, _ := json.Marshal(updateBody)
	url := fmt.Sprintf("%s/products/_update/%s", r.esURL, productID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil
	}
	_ = resp.Body.Close()
	return nil
}

func (r *Repository) DeleteProduct(ctx context.Context, productID string) error {
	r.mu.Lock()
	delete(r.memory, productID)
	r.mu.Unlock()

	if r.esURL == "" {
		return nil
	}

	url := fmt.Sprintf("%s/products/_doc/%s", r.esURL, productID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil
	}
	_ = resp.Body.Close()
	return nil
}

func (r *Repository) Search(ctx context.Context, filter SearchFilter) (*SearchResult, error) {
	if r.esURL != "" {
		res, err := r.searchES(ctx, filter)
		if err == nil {
			return res, nil
		}
	}
	return r.searchMemory(filter)
}

func (r *Repository) searchES(ctx context.Context, filter SearchFilter) (*SearchResult, error) {
	queryMust := []any{}
	if filter.Query != "" {
		queryMust = append(queryMust, map[string]any{
			"multi_match": map[string]any{
				"query":  filter.Query,
				"fields": []string{"name^2", "description", "origin", "roast_level"},
			},
		})
	} else {
		queryMust = append(queryMust, map[string]any{"match_all": map[string]any{}})
	}

	for k, v := range filter.Filters {
		if v != "" {
			queryMust = append(queryMust, map[string]any{
				"term": map[string]any{k: v},
			})
		}
	}

	if filter.InStockOnly {
		queryMust = append(queryMust, map[string]any{
			"term": map[string]any{"in_stock": true},
		})
	}

	esQuery := map[string]any{
		"from": filter.Offset,
		"size": filter.Limit,
		"query": map[string]any{
			"bool": map[string]any{
				"must": queryMust,
			},
		},
		"aggs": map[string]any{
			"roast_level": map[string]any{"terms": map[string]any{"field": "roast_level"}},
			"origin":      map[string]any{"terms": map[string]any{"field": "origin"}},
		},
	}

	bodyBytes, _ := json.Marshal(esQuery)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.esURL+"/products/_search", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("es search returned %d", resp.StatusCode)
	}

	var esResp struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Hits []struct {
				Source ProductDoc `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
		Aggregations map[string]struct {
			Buckets []struct {
				Key      string `json:"key"`
				DocCount int    `json:"doc_count"`
			} `json:"buckets"`
		} `json:"aggregations"`
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &esResp); err != nil {
		return nil, err
	}

	result := &SearchResult{
		Facets: make(map[string]map[string]int),
		Total:  esResp.Hits.Total.Value,
	}
	for _, h := range esResp.Hits.Hits {
		result.Hits = append(result.Hits, h.Source)
	}

	for aggName, aggData := range esResp.Aggregations {
		bucketMap := make(map[string]int)
		for _, b := range aggData.Buckets {
			bucketMap[b.Key] = b.DocCount
		}
		result.Facets[aggName] = bucketMap
	}

	return result, nil
}

func (r *Repository) searchMemory(filter SearchFilter) (*SearchResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matching []ProductDoc
	roastBuckets := make(map[string]int)
	originBuckets := make(map[string]int)

	qLower := strings.ToLower(filter.Query)

	for _, doc := range r.memory {
		if filter.InStockOnly && !doc.InStock {
			continue
		}

		matchedFilter := true
		for k, v := range filter.Filters {
			if v == "" {
				continue
			}
			if k == "roast_level" && !strings.EqualFold(doc.RoastLevel, v) {
				matchedFilter = false
				break
			}
			if k == "origin" && !strings.EqualFold(doc.Origin, v) {
				matchedFilter = false
				break
			}
		}
		if !matchedFilter {
			continue
		}

		if qLower != "" {
			nameLower := strings.ToLower(doc.Name)
			descLower := strings.ToLower(doc.Description)
			if !strings.Contains(nameLower, qLower) && !strings.Contains(descLower, qLower) {
				continue
			}
		}

		matching = append(matching, doc)
		if doc.RoastLevel != "" {
			roastBuckets[doc.RoastLevel]++
		}
		if doc.Origin != "" {
			originBuckets[doc.Origin]++
		}
	}

	total := len(matching)
	start := filter.Offset
	if start > total {
		start = total
	}
	end := start + filter.Limit
	if end > total {
		end = total
	}

	return &SearchResult{
		Hits: matching[start:end],
		Facets: map[string]map[string]int{
			"roast_level": roastBuckets,
			"origin":      originBuckets,
		},
		Total: total,
	}, nil
}
