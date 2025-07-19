// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dag

import (
	"context"
	"errors"

	"github.com/luxfi/node/consensus"
	common "github.com/luxfi/node/consensus/engine"
)

var (
	_ common.Engine = (*Engine)(nil)

	errUnexpectedStart = errors.New("unexpectedly started engine")
)

type Engine struct {
	common.AllGetsServer

	// list of NoOpsHandler for messages dropped by engine
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.AncestorsHandler
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler
	common.AppHandler
	common.InternalHandler

	ctx *consensus.Context
}

func New(
	ctx *consensus.Context,
	gets common.AllGetsServer,
) common.Engine {
	return &Engine{
		AllGetsServer:               gets,
		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(ctx.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(ctx.Log),
		AcceptedFrontierHandler:     common.NewNoOpAcceptedFrontierHandler(ctx.Log),
		AcceptedHandler:             common.NewNoOpAcceptedHandler(ctx.Log),
		AncestorsHandler:            common.NewNoOpAncestorsHandler(ctx.Log),
		PutHandler:                  common.NewNoOpPutHandler(ctx.Log),
		QueryHandler:                common.NewNoOpQueryHandler(ctx.Log),
		ChitsHandler:                common.NewNoOpChitsHandler(ctx.Log),
		AppHandler:                  common.NewNoOpAppHandler(ctx.Log),
		InternalHandler:             common.NewNoOpInternalHandler(ctx.Log),
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
