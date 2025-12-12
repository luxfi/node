// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"
	"fmt"
	"sync"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/message"
)

// Router is the main interface for message routing
// This replaces the deprecated consensus/networking/router package
type Router interface {
	// HandleInbound handles an inbound message
	HandleInbound(ctx context.Context, msg message.InboundMessage)

	// RegisterHandler registers a handler for a specific chain
	RegisterHandler(chainID ids.ID, handler Handler)

	// UnregisterHandler removes a handler for a specific chain
	UnregisterHandler(chainID ids.ID)

	// Deprecated is required for backward compatibility
	Deprecated()
}

// InboundHandler handles inbound messages
type InboundHandler interface {
	HandleInbound(ctx context.Context, msg message.InboundMessage)
}

// InboundHandlerFunc is a function that implements InboundHandler
type InboundHandlerFunc func(ctx context.Context, msg message.InboundMessage)

// HandleInbound implements InboundHandler
func (f InboundHandlerFunc) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	f(ctx, msg)
}

// Handler handles network messages for a specific chain
type Handler interface {
	// HandleInbound handles inbound messages
	HandleInbound(ctx context.Context, msg HandlerMessage) error

	// HandleOutbound handles outbound messages
	HandleOutbound(ctx context.Context, msg HandlerMessage) error

	// Connected handles node connection events
	Connected(ctx context.Context, nodeID ids.NodeID) error

	// Disconnected handles node disconnection events
	Disconnected(ctx context.Context, nodeID ids.NodeID) error
}

// HandlerMessage represents a message for chain handlers
type HandlerMessage struct {
	NodeID    ids.NodeID
	RequestID uint32
	Op        Op
	Message   []byte
}

// Op represents a network operation
type Op byte

const (
	// GetAcceptedFrontier requests accepted frontier
	GetAcceptedFrontier Op = iota
	// AcceptedFrontier is accepted frontier response
	AcceptedFrontier
	// GetAccepted requests accepted items
	GetAccepted
	// Accepted is accepted items response
	Accepted
	// Get requests an item
	Get
	// Put provides an item
	Put
	// PushQuery sends a push query
	PushQuery
	// PullQuery sends a pull query
	PullQuery
	// Chits is query response
	Chits
)

// routerImpl implements the Router interface
type routerImpl struct {
	log      log.Logger
	handlers map[ids.ID]Handler
	mu       sync.RWMutex
}

// NewRouter creates a new router instance
func NewRouter(logger log.Logger) Router {
	return &routerImpl{
		log:      logger,
		handlers: make(map[ids.ID]Handler),
	}
}

// HandleInbound handles inbound messages from the network
func (r *routerImpl) HandleInbound(ctx context.Context, msg message.InboundMessage) {
	chainID, err := message.GetChainID(msg.Message())
	if err != nil {
		r.log.Debug("no chain ID in message",
			"op", msg.Op(),
			"nodeID", msg.NodeID(),
			"error", err,
		)
		return
	}

	r.mu.RLock()
	handler, exists := r.handlers[chainID]
	r.mu.RUnlock()

	if !exists {
		r.log.Debug("no handler registered for chain",
			"chainID", chainID,
			"op", msg.Op(),
		)
		return
	}

	requestID, hasRequestID := message.GetRequestID(msg.Message())
	if !hasRequestID {
		r.log.Debug("no request ID in message",
			"op", msg.Op(),
			"nodeID", msg.NodeID(),
		)
		requestID = 0 // Use 0 as default
	}

	handlerMsg := HandlerMessage{
		NodeID:    msg.NodeID(),
		RequestID: requestID,
		Op:        convertOp(msg.Op()),
		Message:   []byte(fmt.Sprintf("%s", msg.Message())),
	}

	if err := handler.HandleInbound(ctx, handlerMsg); err != nil {
		r.log.Error("failed to handle inbound message",
			"chainID", chainID,
			"nodeID", msg.NodeID(),
			"error", err,
		)
	}
}

// RegisterHandler registers a handler for a specific chain
func (r *routerImpl) RegisterHandler(chainID ids.ID, handler Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[chainID] = handler
	r.log.Info("registered handler for chain", "chainID", chainID)
}

// UnregisterHandler removes a handler for a specific chain
func (r *routerImpl) UnregisterHandler(chainID ids.ID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.handlers, chainID)
	r.log.Info("unregistered handler for chain", "chainID", chainID)
}

// Deprecated satisfies the deprecated router interface
func (r *routerImpl) Deprecated() {}

// convertOp converts message.Op to router.Op
func convertOp(msgOp message.Op) Op {
	switch msgOp {
	case message.GetAcceptedFrontierOp:
		return GetAcceptedFrontier
	case message.AcceptedFrontierOp:
		return AcceptedFrontier
	case message.GetAcceptedOp:
		return GetAccepted
	case message.AcceptedOp:
		return Accepted
	case message.GetOp:
		return Get
	case message.PutOp:
		return Put
	case message.PushQueryOp:
		return PushQuery
	case message.PullQueryOp:
		return PullQuery
	case message.ChitsOp:
		return Chits
	default:
		return Get // Default fallback
	}
}

// Sender sends messages to network peers
type Sender interface {
	// Send sends a message to the specified nodes
	Send(ctx context.Context, msg Message) error

	// SendAppRequest sends an app request to the specified nodes
	SendAppRequest(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, payload []byte) error

	// SendAppResponse sends an app response to a specific node
	SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, payload []byte) error

	// SendAppGossip sends app gossip to the specified nodes
	SendAppGossip(ctx context.Context, nodeIDs []ids.NodeID, payload []byte) error
}

// Message represents a network message
type Message struct {
	NodeIDs   []ids.NodeID
	RequestID uint32
	Op        Op
	Bytes     []byte
}

// senderImpl implements the Sender interface
type senderImpl struct {
	log log.Logger
	// In a real implementation, this would have network components
	// For now, this is a stub implementation
}

// NewSender creates a new sender instance
func NewSender(logger log.Logger) Sender {
	return &senderImpl{
		log: logger,
	}
}

func (s *senderImpl) Send(ctx context.Context, msg Message) error {
	s.log.Debug("sending message",
		"nodeIDs", msg.NodeIDs,
		"requestID", msg.RequestID,
		"op", msg.Op,
		"bytes", len(msg.Bytes),
	)

	// TODO: Implement actual network sending
	return fmt.Errorf("network sending not yet implemented")
}

func (s *senderImpl) SendAppRequest(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, payload []byte) error {
	s.log.Debug("sending app request",
		"nodeIDs", nodeIDs,
		"requestID", requestID,
		"payloadSize", len(payload),
	)

	// TODO: Implement actual app request sending
	return fmt.Errorf("app request sending not yet implemented")
}

func (s *senderImpl) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, payload []byte) error {
	s.log.Debug("sending app response",
		"nodeID", nodeID,
		"requestID", requestID,
		"payloadSize", len(payload),
	)

	// TODO: Implement actual app response sending
	return fmt.Errorf("app response sending not yet implemented")
}

func (s *senderImpl) SendAppGossip(ctx context.Context, nodeIDs []ids.NodeID, payload []byte) error {
	s.log.Debug("sending app gossip",
		"nodeIDs", nodeIDs,
		"payloadSize", len(payload),
	)

	// TODO: Implement actual app gossip sending
	return fmt.Errorf("app gossip sending not yet implemented")
}
