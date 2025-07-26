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
	_ core.Engine = (*Engine)(nil)

	errUnexpectedStart = errors.New("unexpectedly started engine")
)

type Engine struct {
	core.AllGetsServer

	// list of NoOpsHandler for messages dropped by engine
	core.StateSummaryFrontierHandler
	core.AcceptedStateSummaryHandler
	core.AcceptedFrontierHandler
	core.AcceptedHandler
	core.AncestorsHandler
	core.PutHandler
	core.QueryHandler
	core.ChitsHandler
	core.AppHandler
	core.InternalHandler

	ctx *consensus.Context
}

func New(
	ctx *consensus.Context,
	gets core.AllGetsServer,
) core.Engine {
	return &Engine{
		AllGetsServer:               gets,
		StateSummaryFrontierHandler: core.NewNoOpStateSummaryFrontierHandler(ctx.Log),
		AcceptedStateSummaryHandler: core.NewNoOpAcceptedStateSummaryHandler(ctx.Log),
		AcceptedFrontierHandler:     core.NewNoOpAcceptedFrontierHandler(ctx.Log),
		AcceptedHandler:             core.NewNoOpAcceptedHandler(ctx.Log),
		AncestorsHandler:            core.NewNoOpAncestorsHandler(ctx.Log),
		PutHandler:                  core.NewNoOpPutHandler(ctx.Log),
		QueryHandler:                core.NewNoOpQueryHandler(ctx.Log),
		ChitsHandler:                core.NewNoOpChitsHandler(ctx.Log),
		AppHandler:                  core.NewNoOpAppHandler(ctx.Log),
		InternalHandler:             core.NewNoOpInternalHandler(ctx.Log),
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
