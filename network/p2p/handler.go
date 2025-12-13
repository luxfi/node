// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
	"github.com/luxfi/p2p"
)

// Standardized identifiers for application protocol handlers
const (
	TxGossipHandlerID = iota
	AtomicTxGossipHandlerID
	// SignatureRequestHandlerID is specified in LP-118: https://github.com/luxfi/LPs/tree/main/LPs/118-warp-signature-request
	SignatureRequestHandlerID
)

var (
	_ Handler = (*NoOpHandler)(nil)
	_ Handler = (*TestHandler)(nil)
	_ Handler = (*ValidatorHandler)(nil)
)

// Handler is the server-side logic for virtual machine application protocols.
// This is an alias for p2p.Handler.
type Handler = p2p.Handler

// Sender is an alias for p2p.Sender
type Sender = p2p.Sender

// SendConfig is an alias for p2p.SendConfig
type SendConfig = p2p.SendConfig

// NoOpHandler drops all messages
type NoOpHandler struct{}

func (NoOpHandler) Gossip(context.Context, ids.NodeID, []byte) {}

func (NoOpHandler) Request(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *p2p.Error) {
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

func (v ValidatorHandler) Gossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if !v.validatorSet.Has(ctx, nodeID) {
		v.log.Debug("dropping message",
			log.Stringer("nodeID", nodeID),
			log.UserString("reason", "not a validator"),
		)
		return
	}

	v.handler.Gossip(ctx, nodeID, gossipBytes)
}

func (v ValidatorHandler) Request(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *p2p.Error) {
	if !v.validatorSet.Has(ctx, nodeID) {
		return nil, ErrNotValidator
	}

	return v.handler.Request(ctx, nodeID, deadline, requestBytes)
}

// responder automatically sends the response for a given request
type responder struct {
	Handler
	handlerID uint64
	log       log.Logger
	sender    Sender
}

// HandleRequest calls the underlying handler and sends back the response to nodeID
func (r *responder) HandleRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	response, err := r.Handler.Request(ctx, nodeID, deadline, request)
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
		return r.sender.SendError(ctx, nodeID, requestID, err.Code, err.Message)
	}

	return r.sender.SendResponse(ctx, nodeID, requestID, response)
}

// HandleGossip delegates to the underlying handler's Gossip method
func (r *responder) HandleGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	r.Handler.Gossip(ctx, nodeID, gossipBytes)
}

type TestHandler struct {
	GossipF  func(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte)
	RequestF func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *p2p.Error)
}

func (t TestHandler) Gossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	if t.GossipF == nil {
		return
	}

	t.GossipF(ctx, nodeID, gossipBytes)
}

func (t TestHandler) Request(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, *p2p.Error) {
	if t.RequestF == nil {
		return nil, nil
	}

	return t.RequestF(ctx, nodeID, deadline, requestBytes)
}
