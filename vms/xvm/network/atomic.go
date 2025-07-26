// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/utils"
)

var _ Atomic = (*atomic)(nil)

type Atomic interface {
	core.AppHandler

	Set(core.AppHandler)
}

type atomic struct {
	handler utils.Atomic[core.AppHandler]
}

func NewAtomic(h core.AppHandler) Atomic {
	a := &atomic{}
	a.handler.Set(h)
	return a
}

func (a *atomic) AppRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	deadline time.Time,
	msg []byte,
) error {
	h := a.handler.Get()
	return h.AppRequest(
		ctx,
		nodeID,
		requestID,
		deadline,
		msg,
	)
}

func (a *atomic) AppRequestFailed(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	appErr *core.AppError,
) error {
	h := a.handler.Get()
	return h.AppRequestFailed(
		ctx,
		nodeID,
		requestID,
		appErr,
	)
}

func (a *atomic) AppResponse(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	msg []byte,
) error {
	h := a.handler.Get()
	return h.AppResponse(
		ctx,
		nodeID,
		requestID,
		msg,
	)
}

func (a *atomic) AppGossip(
	ctx context.Context,
	nodeID ids.NodeID,
	msg []byte,
) error {
	h := a.handler.Get()
	return h.AppGossip(
		ctx,
		nodeID,
		msg,
	)
}

func (a *atomic) Set(h core.AppHandler) {
	a.handler.Set(h)
}
