// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"io"

	"go.opentelemetry.io/otel/trace"
)

// Tracer is the interface for tracing
type Tracer interface {
	trace.Tracer
	io.Closer
}

// noopTracer provides a noop implementation of Tracer
type noopTracer struct {
	trace.Tracer
}

// newNoopTracer creates a new noop tracer
func newNoopTracer() *noopTracer {
	ntp := trace.NewNoopTracerProvider()
	return &noopTracer{
		Tracer: ntp.Tracer("noop"),
	}
}

// Close implements io.Closer
func (t *noopTracer) Close() error {
	return nil
}

// Noop is a no-op tracer instance
var Noop = newNoopTracer()