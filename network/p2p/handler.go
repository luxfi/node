// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"time"


	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	consensuscore "github.com/luxfi/consensus/core"
	"github.com/luxfi/log"
)

// Standardized identifiers for application protocol handlers
const (
	TxGossipHandlerID = iota
	AtomicTxGossipHandlerID
	// SignatureRequestHandlerID is specified in ACP-118: https://github.com/luxfi/ACPs/tree/main/ACPs/118-warp-signature-request
	SignatureRequestHandlerID
)

var (
	_ Handler = (*NoOpHandler)(nil)
	_ Handler = (*TestHandler)(nil)
	_ Handler = (*ValidatorHandler)(nil)
)

// Handler is the server-side logic for virtual machine application protocols.
type Handler interface {
	// AppGossip is called when handling an AppGossip message.
	AppGossip(
		ctx context.Context,
		nodeID ids.NodeID,
		gossipBytes []byte,
	)
	// AppRequest is called when handling an AppRequest message.
	// Sends a response with the response corresponding to [requestBytes] or
	// an application-defined error.
	AppRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		deadline time.Time,
		requestBytes []byte,
	) ([]byte, *consensuscore.AppError)
}

// NoOpHandler drops all messages
type NoOpHandler struct{}

func (NoOpHandler) AppGossip(context.Context, ids.NodeID, []byte) {}

func (NoOpHandler) AppRequest(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *consensuscore.AppError) {
	return nil, nil
}

func NewValidatorHandler(
	handler Handler,
	validatorSet ValidatorSet,
	log log.Logger,
) *ValidatorHandler {
	return &ValidatorHandler{
		handler:      handler,
		validatorSet: validatorSet,
		log:          log,
	}
}

// ValidatorHandler drops messages from non-validators
type ValidatorHandler struct {
	handler      Handler
	validatorSet ValidatorSet
	log          log.Logger
}

func (v ValidatorHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if !v.validatorSet.Has(ctx, nodeID) {
		v.log.Debug("dropping message",
			log.Stringer("nodeID", nodeID),
			log.UserString("reason", "not a validator"),
		)
		return
	}

	v.handler.AppGossip(ctx, nodeID, gossipBytes)
}

func (v ValidatorHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *consensuscore.AppError) {
	if !v.validatorSet.Has(ctx, nodeID) {
		return nil, ErrNotValidator
	}

	return v.handler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

// responder automatically sends the response for a given request
type responder struct {
	Handler
	handlerID uint64
	log       log.Logger
	sender    consensuscore.AppSender
}

// AppRequest calls the underlying handler and sends back the response to nodeID
func (r *responder) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	appResponse, err := r.Handler.AppRequest(ctx, nodeID, deadline, request)
	if err != nil {
		r.log.Debug("failed to handle message",
			log.Stringer("messageOp", message.AppRequestOp),
			log.Stringer("nodeID", nodeID),
			log.Uint32("requestID", requestID),
			log.Time("deadline", deadline),
			log.Uint64("handlerID", r.handlerID),
			log.Binary("message", request),
			log.Err(err),
		)
		return r.sender.SendAppError(ctx, nodeID, requestID, err.Code, err.Message)
	}

	return r.sender.SendAppResponse(ctx, nodeID, requestID, appResponse)
}

type TestHandler struct {
	AppGossipF  func(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte)
	AppRequestF func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *consensuscore.AppError)
}

func (t TestHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if t.AppGossipF == nil {
		return
	}

	t.AppGossipF(ctx, nodeID, gossipBytes)
}

func (t TestHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *consensuscore.AppError) {
	if t.AppRequestF == nil {
		return nil, nil
	}

	return t.AppRequestF(ctx, nodeID, deadline, requestBytes)
}
