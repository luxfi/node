// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"
	"errors"

	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/chain/engine/common"
)

var (
	_ core.Engine = (*engine)(nil)

	errUnexpectedStart = errors.New("unexpectedly started engine")
)

type engine struct {
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

	ctx *snow.ConsensusContext
}

func New(
	ctx *snow.ConsensusContext,
	gets core.AllGetsServer,
) core.Engine {
	return &engine{
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

func (*engine) Start(context.Context, uint32) error {
	return errUnexpectedStart
}

func (e *engine) Context() *snow.ConsensusContext {
	return e.ctx
}

func (*engine) HealthCheck(context.Context) (interface{}, error) {
	return nil, nil
}
