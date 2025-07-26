// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"context"

	"github.com/luxfi/trace"
)

var _ BootstrapableEngine = (*tracedBootstrapableEngine)(nil)

type tracedBootstrapableEngine struct {
	Engine
	bootstrapableEngine BootstrapableEngine
	tracer              trace.Tracer
}

func TraceBootstrapableEngine(bootstrapableEngine BootstrapableEngine, tracer trace.Tracer) BootstrapableEngine {
	return &tracedBootstrapableEngine{
		Engine:              TraceEngine(bootstrapableEngine, tracer),
		bootstrapableEngine: bootstrapableEngine,
		tracer:              tracer,
	}
}

func (e *tracedBootstrapableEngine) Clear(ctx context.Context) error {
	ctx, span := e.tracer.Start(ctx, "tracedBootstrapableEngine.Clear")
	defer span.End()

	return e.bootstrapableEngine.Clear(ctx)
}
