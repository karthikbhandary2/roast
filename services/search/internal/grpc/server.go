package grpcserver

import (
	"context"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/roastco/ecommerce/gen/common/v1"
	searchv1 "github.com/roastco/ecommerce/gen/search/v1"
	"github.com/roastco/ecommerce/pkg/money"
	"github.com/roastco/ecommerce/services/search/internal/service"
)

type Server struct {
	searchv1.UnimplementedSearchServiceServer
	svc *service.Service
	log *slog.Logger
}

func New(svc *service.Service, log *slog.Logger) *Server {
	return &Server{svc: svc, log: log}
}

func (s *Server) SearchProducts(ctx context.Context, req *searchv1.SearchProductsRequest) (*searchv1.SearchProductsResponse, error) {
	pageReq := req.GetPage()
	pageSize := 20
	pageToken := ""
	if pageReq != nil {
		pageSize = int(pageReq.GetPageSize())
		pageToken = pageReq.GetPageToken()
	}

	out, err := s.svc.SearchProducts(ctx, service.SearchInput{
		Query:       req.GetQuery(),
		Filters:     req.GetFilters(),
		InStockOnly: req.GetInStockOnly(),
		PageSize:    pageSize,
		PageToken:   pageToken,
	})
	if err != nil {
		s.log.Error("search failed", "error", err)
		return nil, status.Error(codes.Internal, "internal server error")
	}

	var protoHits []*searchv1.SearchHit
	for _, h := range out.Hits {
		protoHits = append(protoHits, &searchv1.SearchHit{
			ProductId:      h.ProductID,
			SellerId:       h.SellerID,
			Name:           h.Name,
			Price:          money.MoneyToProto(h.Currency, h.PriceMinor),
			RoastLevel:     h.RoastLevel,
			Origin:         h.Origin,
			InStock:        h.InStock,
			RelevanceScore: 1.0,
		})
	}

	var protoFacets []*searchv1.Facet
	for facetName, facetValues := range out.Facets {
		var values []*searchv1.FacetValue
		for val, count := range facetValues {
			values = append(values, &searchv1.FacetValue{
				Value: val,
				Count: int32(count),
			})
		}
		protoFacets = append(protoFacets, &searchv1.Facet{
			Name:   facetName,
			Values: values,
		})
	}

	return &searchv1.SearchProductsResponse{
		Hits:   protoHits,
		Facets: protoFacets,
		Page: &commonv1.PageResponse{
			NextPageToken: out.NextPageToken,
		},
	}, nil
}
