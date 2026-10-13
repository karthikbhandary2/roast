// Package tracing wires up OpenTelemetry so a single request/saga can be
// followed across service boundaries: the API Gateway starts a trace, and
// every gRPC hop (via interceptors registered alongside this) carries the
// trace context in metadata so Order, Product, Payment, and Notification's
// spans for one checkout all land under the same trace ID in Jaeger - the
// "distributed tracing" piece of the observability goals.
package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// Init points the SDK at Jaeger's OTLP gRPC endpoint (the jaeger container
// in docker-compose.yml) and registers a global tracer + W3C trace-context
// propagator. Call once at service startup; call the returned shutdown
// func on graceful shutdown so buffered spans get flushed.
func Init(ctx context.Context, serviceName, otlpEndpoint string) (shutdown func(context.Context) error, err error) {
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(otlpEndpoint),
		otlptracegrpc.WithInsecure(), // TLS termination handled by the mesh/ingress in a real deployment, not here
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: create exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: build resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp.Shutdown, nil
}

// Tracer is a convenience accessor so services don't each import
// go.opentelemetry.io/otel just to call otel.Tracer(name).
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
