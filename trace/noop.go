// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/luxdefi/node/utils/constants"
)

// noOpTracer is an implementation of trace.Tracer that does nothing.
type noOpTracer struct {
	trace.Tracer
}

var Noop Tracer = noOpTracer{
	Tracer: noop.NewTracerProvider().Tracer(constants.AppName),
}

func (n noOpTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return n.Start(ctx, spanName, opts...)
}

func (noOpTracer) Close() error {
	return nil
}
