//go:build !grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package trace provides tracing functionality.
// In ZAP mode (default), tracing is disabled - no OpenTelemetry dependencies.
// Use -tags=grpc to enable full OpenTelemetry tracing support.
package trace

import (
	"context"
	"io"
)

// Config holds tracing configuration
type Config struct {
	ExporterConfig  ExporterConfig `json:"exporterConfig"`
	TraceSampleRate float64        `json:"traceSampleRate"`
	AppName         string         `json:"appName"`
	Version         string         `json:"version"`
}

// ExporterConfig holds exporter configuration
type ExporterConfig struct {
	Type     ExporterType      `json:"type"`
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers"`
	Insecure bool              `json:"insecure"`
}

// ExporterType represents the type of trace exporter
type ExporterType byte

const (
	Disabled ExporterType = iota
	GRPC
	HTTP
	ZAP
)

func (t ExporterType) String() string {
	switch t {
	case Disabled:
		return "disabled"
	case GRPC:
		return "grpc"
	case HTTP:
		return "http"
	case ZAP:
		return "zap"
	default:
		return "unknown"
	}
}

func (t ExporterType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.String() + `"`), nil
}

func (t *ExporterType) UnmarshalJSON(b []byte) error {
	s := string(b)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	switch s {
	case "disabled", "null", "":
		*t = Disabled
	case "grpc":
		*t = GRPC
	case "http":
		*t = HTTP
	case "zap":
		*t = ZAP
	}
	return nil
}

// ExporterTypeFromString parses an exporter type string
func ExporterTypeFromString(s string) (ExporterType, error) {
	switch s {
	case "disabled", "":
		return Disabled, nil
	case "grpc":
		return GRPC, nil
	case "http":
		return HTTP, nil
	case "zap":
		return ZAP, nil
	default:
		return Disabled, nil
	}
}

// Tracer interface for tracing operations
type Tracer interface {
	io.Closer
	Start(ctx context.Context, spanName string, opts ...SpanStartOption) (context.Context, Span)
}

// Span represents a trace span
type Span interface {
	End(options ...SpanEndOption)
	SetStatus(code StatusCode, description string)
	RecordError(err error, options ...EventOption)
	AddEvent(name string, options ...EventOption)
	SetAttributes(kv ...KeyValue)
}

// SpanStartOption configures span creation
type SpanStartOption interface {
	applySpanStart()
}

type spanStartOption struct{}

func (spanStartOption) applySpanStart() {}

// WithAttributes returns a SpanStartOption
func WithAttributes(...KeyValue) SpanStartOption {
	return spanStartOption{}
}

// SpanEndOption configures span ending
type SpanEndOption interface{}

// EventOption configures events
type EventOption interface{}

// KeyValue represents a key-value attribute pair (otel compatible name)
type KeyValue struct {
	Key   string
	Value Value
}

// Value wraps attribute values
type Value struct {
	v interface{}
}

// String creates a string attribute
func String(key, value string) KeyValue {
	return KeyValue{Key: key, Value: Value{v: value}}
}

// Int creates an int attribute
func Int(key string, value int) KeyValue {
	return KeyValue{Key: key, Value: Value{v: value}}
}

// Int64 creates an int64 attribute
func Int64(key string, value int64) KeyValue {
	return KeyValue{Key: key, Value: Value{v: value}}
}

// Bool creates a bool attribute
func Bool(key string, value bool) KeyValue {
	return KeyValue{Key: key, Value: Value{v: value}}
}

// Stringer creates a string attribute from a fmt.Stringer
func Stringer(key string, value interface{ String() string }) KeyValue {
	return KeyValue{Key: key, Value: Value{v: value.String()}}
}

// StatusCode represents span status
type StatusCode int

const (
	StatusUnset StatusCode = iota
	StatusOK
	StatusError
)

// Noop is a no-op tracer
var Noop Tracer = &noopTracer{}

type noopTracer struct{}

func (noopTracer) Close() error { return nil }

func (noopTracer) Start(ctx context.Context, _ string, _ ...SpanStartOption) (context.Context, Span) {
	return ctx, noopSpan{}
}

type noopSpan struct{}

func (noopSpan) End(...SpanEndOption)         {}
func (noopSpan) SetStatus(StatusCode, string) {}
func (noopSpan) RecordError(error, ...EventOption) {}
func (noopSpan) AddEvent(string, ...EventOption)   {}
func (noopSpan) SetAttributes(...KeyValue)         {}

// New creates a new tracer (returns Noop in ZAP mode)
func New(Config) (Tracer, error) {
	return Noop, nil
}
