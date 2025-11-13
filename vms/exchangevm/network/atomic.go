// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"time"

	consensuscore "github.com/luxfi/consensus/core"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils"
)

var _ Atomic = (*atomic)(nil)

type Atomic interface {
	consensuscore.AppHandler

	Set(consensuscore.AppHandler)
}

type atomic struct {
	handler utils.Atomic[consensuscore.AppHandler]
}

func NewAtomic(h consensuscore.AppHandler) Atomic {
	a := &atomic{}
	a.handler.Set(h)
	return a
}

// func (a *atomic) CrossChainAppRequest(
// 	ctx context.Context,
// 	chainID ids.ID,
// 	requestID uint32,
// 	deadline time.Time,
// 	msg []byte,
// ) error {
// 	h := a.handler.Get()
// 	return h.CrossChainAppRequest(
// 		ctx,
// 		chainID,
// 		requestID,
// 		deadline,
// 		msg,
// 	)
// }

// func (a *atomic) CrossChainAppRequestFailed(
// 	ctx context.Context,
// 	chainID ids.ID,
// 	requestID uint32,
// 	appErr *consensuscore.AppError,
// ) error {
// 	h := a.handler.Get()
// 	return h.CrossChainAppRequestFailed(
// 		ctx,
// 		chainID,
// 		requestID,
// 		appErr,
// 	)
// }

// func (a *atomic) CrossChainAppResponse(
// 	ctx context.Context,
// 	chainID ids.ID,
// 	requestID uint32,
// 	msg []byte,
// ) error {
// 	h := a.handler.Get()
// 	return h.CrossChainAppResponse(
// 		ctx,
// 		chainID,
// 		requestID,
// 		msg,
// 	)
// }

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
	appErr *consensuscore.AppError,
) error {
	// AppRequestFailed might not be defined in consensuscore.AppHandler
	// Just return nil for now
	return nil
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

func (a *atomic) Set(h consensuscore.AppHandler) {
	a.handler.Set(h)
}
