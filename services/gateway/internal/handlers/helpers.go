package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	gatewaymw "github.com/roastco/ecommerce/services/gateway/internal/middleware"
)

// errInvalidJSON is returned by every handler when decodeJSON fails -
// maps to 400 via writeError's codes.InvalidArgument case.
var errInvalidJSON = status.Error(codes.InvalidArgument, "malformed request body")

// errMissingIdempotencyKey guards Checkout - LLD.md §1 requires one.
var errMissingIdempotencyKey = status.Error(codes.InvalidArgument, "missing Idempotency-Key header")

// errStreamingUnsupported is the (practically unreachable, since every real
// net/http ResponseWriter implements http.Flusher) guard for OrderEvents.
var errStreamingUnsupported = status.Error(codes.Internal, "streaming unsupported")

// envelope is the standard response shape - LLD.md §2.1.1. Page is omitted
// (via omitempty) on non-list responses.
type envelope struct {
	Data  any            `json:"data,omitempty"`
	Links map[string]link `json:"_links,omitempty"`
	Page  *pageInfo      `json:"page,omitempty"`
}

type link struct {
	Href   string `json:"href"`
	Method string `json:"method,omitempty"` // omitted for GET, the implied default
}

type pageInfo struct {
	NextPageToken string `json:"next_page_token,omitempty"`
}

func writeData(w http.ResponseWriter, status int, data any) {
	writeEnvelope(w, status, envelope{Data: data})
}

func writeDataWithLinks(w http.ResponseWriter, status int, data any, links map[string]link) {
	writeEnvelope(w, status, envelope{Data: data, Links: links})
}

func writeList(w http.ResponseWriter, status int, data any, nextPageToken string) {
	var page *pageInfo
	if nextPageToken != "" {
		page = &pageInfo{NextPageToken: nextPageToken}
	}
	writeEnvelope(w, status, envelope{Data: data, Page: page})
}

// protoJSON renders proto messages the canonical way: enums as their names
// ("ROLE_BUYER"), timestamps as RFC 3339, and snake_case keys. NOTE: int64
// fields (e.g. money's amount_minor) come out as JSON strings ("1900"), per
// the protobuf JSON spec - that also keeps them safe from JavaScript's
// 2^53 number limit, but clients must parse them.
var protoJSON = protojson.MarshalOptions{UseProtoNames: true}

// toJSON renders a handler payload. Proto messages use protojson; slices and
// maps are walked so proto messages nested inside them are rendered the same
// way; anything else (plain structs, numbers) falls through to encoding/json.
func toJSON(v any) (json.RawMessage, error) {
	if v == nil {
		return json.RawMessage("null"), nil
	}
	if m, ok := v.(proto.Message); ok {
		return protoJSON.Marshal(m)
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		parts := make([]json.RawMessage, rv.Len())
		for i := range parts {
			p, err := toJSON(rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			parts[i] = p
		}
		return json.Marshal(parts) // an empty or nil slice becomes [], never null
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			out := make(map[string]json.RawMessage, rv.Len())
			iter := rv.MapRange()
			for iter.Next() {
				p, err := toJSON(iter.Value().Interface())
				if err != nil {
					return nil, err
				}
				out[iter.Key().String()] = p
			}
			return json.Marshal(out)
		}
	}
	return json.Marshal(v)
}

// writeJSON buffers the body before writing anything, so an encoding failure
// can still become a clean 500. HTML escaping is off so a message like
// "active -> active" isn't mangled into "active -\u003e active".
func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"Internal","message":"internal error"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func writeEnvelope(w http.ResponseWriter, status int, e envelope) {
	out := struct {
		Data  json.RawMessage `json:"data,omitempty"`
		Links map[string]link `json:"_links,omitempty"`
		Page  *pageInfo       `json:"page,omitempty"`
	}{Links: e.Links, Page: e.Page}

	if e.Data != nil {
		raw, err := toJSON(e.Data)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]any{"code": "Internal", "message": "internal error"},
			})
			return
		}
		out.Data = raw
	}
	writeJSON(w, status, out)
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// writeError maps a gRPC status error to the HTTP status + error body
// shape from LLD.md §4.4. The client never sees the raw internal error
// text for anything mapped to 500 - only a generic message plus the
// request ID, which is what support actually needs to find the real error
// in the logs.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	st, ok := status.FromError(err)
	if !ok {
		st = status.New(codes.Internal, "internal error")
	}

	httpStatus := http.StatusInternalServerError
	message := st.Message()

	switch st.Code() {
	case codes.InvalidArgument:
		httpStatus = http.StatusBadRequest
	case codes.Unauthenticated:
		httpStatus = http.StatusUnauthorized
	case codes.PermissionDenied:
		httpStatus = http.StatusForbidden
	case codes.NotFound:
		httpStatus = http.StatusNotFound
	case codes.AlreadyExists, codes.FailedPrecondition:
		httpStatus = http.StatusConflict
	case codes.ResourceExhausted:
		httpStatus = http.StatusTooManyRequests
	case codes.Unavailable:
		httpStatus = http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		httpStatus = http.StatusGatewayTimeout
	default:
		httpStatus = http.StatusInternalServerError
		message = "internal error" // never leak raw internal error text to the client
	}

	writeJSON(w, httpStatus, map[string]any{
		"error": map[string]any{
			"code":           st.Code().String(),
			"message":        message,
			"correlation_id": gatewaymw.RequestIDFromContext(r.Context()),
		},
	})
}

// authContext forwards the caller's bearer token and request ID as gRPC
// metadata, so internal services' AuthUnaryInterceptor (pkg/middleware)
// can validate the same token the gateway already checked, and saga logs
// can be cross-referenced against the gateway's own request log - see
// LLD.md §1 "Correlation IDs".
func authContext(r *http.Request) context.Context {
	md := metadata.Pairs(
		"authorization", r.Header.Get("Authorization"),
		"x-correlation-id", gatewaymw.RequestIDFromContext(r.Context()),
	)
	return metadata.NewOutgoingContext(r.Context(), md)
}

func mustClaims(r *http.Request) *gatewaymw.Claims {
	claims, _ := gatewaymw.ClaimsFromContext(r.Context())
	return claims // only called on routes behind RequireAuth, where this is always set
}

func pageSize(r *http.Request, def int32) int32 {
	v := r.URL.Query().Get("page_size")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return int32(n)
}