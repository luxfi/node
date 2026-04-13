//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"context"
	"fmt"
	"io"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	oteltrace "go.opentelemetry.io/otel/trace"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// Type aliases for otel compatibility
type (
	KeyValue        = attribute.KeyValue
	SpanStartOption = oteltrace.SpanStartOption
	SpanEndOption   = oteltrace.SpanEndOption
	Span            = oteltrace.Span
	StatusCode      = codes.Code
)

// Re-export otel status codes
const (
	StatusUnset = codes.Unset
	StatusOK    = codes.Ok
	StatusError = codes.Error
)

// EventOption is a type alias for span event options
type EventOption = oteltrace.EventOption

// Helper functions wrapping otel attribute constructors

// String creates a string attribute
func String(key, value string) KeyValue {
	return attribute.String(key, value)
}

// Int creates an int attribute
func Int(key string, value int) KeyValue {
	return attribute.Int(key, value)
}

// Int64 creates an int64 attribute
func Int64(key string, value int64) KeyValue {
	return attribute.Int64(key, value)
}

// Bool creates a bool attribute
func Bool(key string, value bool) KeyValue {
	return attribute.Bool(key, value)
}

// Stringer creates a string attribute from a fmt.Stringer
func Stringer(key string, value fmt.Stringer) KeyValue {
	return attribute.Stringer(key, value)
}

// WithAttributes returns a SpanStartOption with the given attributes
func WithAttributes(kv ...KeyValue) SpanStartOption {
	return oteltrace.WithAttributes(kv...)
}

const (
	tracerExportTimeout = 10 * time.Second
	// [tracerProviderShutdownTimeout] is longer than [tracerExportTimeout] so
	// in-flight exports can finish before the tracer provider shuts down.
	tracerProviderShutdownTimeout = 15 * time.Second
)

type Config struct {
	ExporterConfig `json:"exporterConfig"`

	// The fraction of traces to sample.
	// If >= 1 always samples.
	// If <= 0 never samples.
	TraceSampleRate float64 `json:"traceSampleRate"`

	AppName string `json:"appName"`
	Version string `json:"version"`
}

type Tracer interface {
	oteltrace.Tracer
	io.Closer
}

type tracer struct {
	oteltrace.Tracer

	tp *sdktrace.TracerProvider
}

func (t *tracer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), tracerProviderShutdownTimeout)
	defer cancel()
	return t.tp.Shutdown(ctx)
}

func New(config Config) (Tracer, error) {
	if config.ExporterConfig.Type == Disabled {
		return Noop, nil
	}

	exporter, err := newExporter(config.ExporterConfig)
	if err != nil {
		return nil, err
	}

	tracerProviderOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(exporter, sdktrace.WithExportTimeout(tracerExportTimeout)),
		sdktrace.WithResource(resource.NewWithAttributes(semconv.SchemaURL,
			attribute.String("version", config.Version),
			semconv.ServiceNameKey.String(config.AppName),
		)),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(config.TraceSampleRate)),
	}

	tracerProvider := sdktrace.NewTracerProvider(tracerProviderOpts...)
	return &tracer{
		Tracer: tracerProvider.Tracer(config.AppName),
		tp:     tracerProvider,
	}, nil
}
