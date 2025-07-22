// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dag

import (
	"context"
	"errors"

	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/engine/core"
)

var (
	_ engine.Engine = (*Engine)(nil)

	errUnexpectedStart = errors.New("unexpectedly started engine")
)

type Engine struct {
	engine.AllGetsServer

	// list of NoOpsHandler for messages dropped by engine
	engine.StateSummaryFrontierHandler
	engine.AcceptedStateSummaryHandler
	engine.AcceptedFrontierHandler
	engine.AcceptedHandler
	engine.AncestorsHandler
	engine.PutHandler
	engine.QueryHandler
	engine.ChitsHandler
	engine.AppHandler
	engine.InternalHandler

	ctx *consensus.Context
}

func New(
	ctx *consensus.Context,
	gets engine.AllGetsServer,
) engine.Engine {
	return &Engine{
		AllGetsServer:               gets,
		StateSummaryFrontierHandler: engine.NewNoOpStateSummaryFrontierHandler(ctx.Log),
		AcceptedStateSummaryHandler: engine.NewNoOpAcceptedStateSummaryHandler(ctx.Log),
		AcceptedFrontierHandler:     engine.NewNoOpAcceptedFrontierHandler(ctx.Log),
		AcceptedHandler:             engine.NewNoOpAcceptedHandler(ctx.Log),
		AncestorsHandler:            engine.NewNoOpAncestorsHandler(ctx.Log),
		PutHandler:                  engine.NewNoOpPutHandler(ctx.Log),
		QueryHandler:                engine.NewNoOpQueryHandler(ctx.Log),
		ChitsHandler:                engine.NewNoOpChitsHandler(ctx.Log),
		AppHandler:                  engine.NewNoOpAppHandler(ctx.Log),
		InternalHandler:             engine.NewNoOpInternalHandler(ctx.Log),
		ctx:                         ctx,
	}
}

func (*Engine) Start(context.Context, uint32) error {
	return errUnexpectedStart
}

func (e *Engine) Context() *consensus.Context {
	return e.ctx
}

func (*Engine) HealthCheck(context.Context) (interface{}, error) {
	return nil, nil
}
