package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/roastco/ecommerce/services/search/internal/repository"
)

type Service struct {
	repo *repository.Repository
}

func New(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

type SearchInput struct {
	Query       string
	Filters     map[string]string
	InStockOnly bool
	PageSize    int
	PageToken   string
}

type SearchOutput struct {
	Hits          []repository.ProductDoc
	Facets        map[string]map[string]int
	NextPageToken string
}

func (s *Service) SearchProducts(ctx context.Context, in SearchInput) (*SearchOutput, error) {
	if in.PageSize <= 0 {
		in.PageSize = 20
	}
	if in.PageSize > 100 {
		in.PageSize = 100
	}

	offset := 0
	if in.PageToken != "" {
		off, err := decodeOffset(in.PageToken)
		if err == nil {
			offset = off
		}
	}

	res, err := s.repo.Search(ctx, repository.SearchFilter{
		Query:       strings.TrimSpace(in.Query),
		Filters:     in.Filters,
		InStockOnly: in.InStockOnly,
		Limit:       in.PageSize,
		Offset:      offset,
	})
	if err != nil {
		return nil, err
	}

	next := ""
	if offset+len(res.Hits) < res.Total {
		next = encodeOffset(offset + len(res.Hits))
	}

	return &SearchOutput{
		Hits:          res.Hits,
		Facets:        res.Facets,
		NextPageToken: next,
	}, nil
}

func encodeOffset(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeOffset(token string) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("invalid page token")
	}
	return strconv.Atoi(string(raw))
}
