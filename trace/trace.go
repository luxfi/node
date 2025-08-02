// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"errors"
	"time"
)

// Config contains configuration for distributed tracing
type Config struct {
	// Enabled controls whether tracing is enabled
	Enabled bool `json:"enabled"`

	// TracingEndpoint is the endpoint for the tracing collector
	TracingEndpoint string `json:"endpoint"`

	// TracingInsecure controls whether to use insecure connection
	TracingInsecure bool `json:"insecure"`

	// TracingSampleRate controls the sampling rate (0.0 to 1.0)
	TracingSampleRate float64 `json:"sampleRate"`

	// TraceSampleRate is an alias for TracingSampleRate
	TraceSampleRate float64 `json:"traceSampleRate"`

	// TracingExporter specifies the exporter type (e.g., "otlp", "jaeger")
	TracingExporter string `json:"exporter"`

	// ExporterConfig contains exporter-specific configuration
	ExporterConfig ExporterConfig `json:"exporterConfig"`

	// ServiceName is the name of the service for tracing
	ServiceName string `json:"serviceName"`

	// ServiceVersion is the version of the service
	ServiceVersion string `json:"serviceVersion"`

	// AppName is an alias for ServiceName
	AppName string `json:"appName"`

	// Version is an alias for ServiceVersion
	Version string `json:"version"`

	// BufferSize is the size of the span buffer
	BufferSize int `json:"bufferSize"`

	// ExportTimeout is the timeout for exporting spans
	ExportTimeout time.Duration `json:"exportTimeout"`
}

// ExporterConfig contains exporter-specific configuration
type ExporterConfig struct {
	// Type is the type of exporter
	Type ExporterType `json:"type"`
	
	// Endpoint is the endpoint for the exporter
	Endpoint string `json:"endpoint"`
	
	// Insecure controls whether to use insecure connection
	Insecure bool `json:"insecure"`
	
	// Headers are additional headers to send
	Headers map[string]string `json:"headers"`
	
	// OTLP exporter configuration
	OTLPEndpoint string `json:"otlpEndpoint"`
	OTLPHeaders  map[string]string `json:"otlpHeaders"`
	
	// Jaeger exporter configuration
	JaegerEndpoint string `json:"jaegerEndpoint"`
	
	// Zipkin exporter configuration
	ZipkinEndpoint string `json:"zipkinEndpoint"`
}

// ExporterType represents the type of exporter
type ExporterType int

const (
	// NoneExporter represents no exporter
	NoneExporter ExporterType = iota
	// OTLPExporter represents OTLP exporter
	OTLPExporter
	// JaegerExporter represents Jaeger exporter
	JaegerExporter
	// ZipkinExporter represents Zipkin exporter
	ZipkinExporter
)

// Trace type strings
const (
	// Disabled represents disabled exporter type
	Disabled = "none"
	// GRPC enables gRPC tracing
	GRPC = "grpc"
	// HTTP enables HTTP tracing
	HTTP = "http"
)

// New creates a new tracer based on the configuration
func New(cfg Config) (Tracer, error) {
	// For now, always return a noop tracer
	// TODO: Implement actual tracer initialization based on cfg
	return Noop, nil
}

// ExporterTypeFromString converts a string to ExporterType
func ExporterTypeFromString(s string) (ExporterType, error) {
	switch s {
	case "", "none":
		return NoneExporter, nil
	case "otlp":
		return OTLPExporter, nil
	case "jaeger":
		return JaegerExporter, nil
	case "zipkin":
		return ZipkinExporter, nil
	default:
		return NoneExporter, errors.New("unknown exporter type: " + s)
	}
}