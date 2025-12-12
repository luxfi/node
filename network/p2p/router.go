// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/message"
	"github.com/luxfi/p2p"
)

var (
	ErrExistingAppProtocol = errors.New("existing app protocol")
	ErrUnrequestedResponse = errors.New("unrequested response")
)

type pendingRequest struct {
	handlerID string
	callback  ResponseCallback
}

type metrics struct {
	msgTime  metric.GaugeVec
	msgCount metric.CounterVec
}

func (m *metrics) observe(labels map[string]string, start time.Time) {
	metricTime := m.msgTime.With(labels)
	metricCount := m.msgCount.With(labels)

	metricTime.Add(float64(time.Since(start)))
	metricCount.Inc()
}

// router routes incoming messages to the corresponding registered
// handler. Messages must be made using the registered handler's
// corresponding Client.
type router struct {
	log     log.Logger
	sender  p2p.Sender
	metrics metrics

	lock            sync.RWMutex
	handlers        map[uint64]*responder
	pendingRequests map[uint32]pendingRequest
	requestID       uint32
}

// newRouter returns a new instance of Router
func newRouter(
	log log.Logger,
	sender p2p.Sender,
	metrics metrics,
) *router {
	return &router{
		log:             log,
		sender:          sender,
		metrics:         metrics,
		handlers:        make(map[uint64]*responder),
		pendingRequests: make(map[uint32]pendingRequest),
		// invariant: sdk uses odd-numbered requestIDs
		requestID: 1,
	}
}

func (r *router) addHandler(handlerID uint64, handler Handler) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	if _, ok := r.handlers[handlerID]; ok {
		return fmt.Errorf("failed to register handler id %d: %w", handlerID, ErrExistingAppProtocol)
	}

	r.handlers[handlerID] = &responder{
		Handler:   handler,
		handlerID: handlerID,
		log:       r.log,
		sender:    r.sender,
	}

	return nil
}

// Request handles incoming requests
func (r *router) Request(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) ([]byte, *p2p.Error) {
	start := time.Now()
	parsedMsg, handler, handlerID, ok := r.parse(request)
	if !ok {
		r.log.Debug("received message for unregistered handler",
			log.Stringer("messageOp", message.AppRequestOp),
			log.Stringer("nodeID", nodeID),
			log.Uint32("requestID", requestID),
			log.Time("deadline", deadline),
			log.Binary("message", request),
		)

		// Return an error. Invalid requests that we
		// cannot parse a handler id for are handled the same way as requests
		// for which we do not have a registered handler.
		return nil, ErrUnregisteredHandler
	}

	// call the corresponding handler and send back a response to nodeID
	if err := handler.HandleRequest(ctx, nodeID, requestID, deadline, parsedMsg); err != nil {
		return nil, &p2p.Error{Code: -1, Message: err.Error()}
	}

	r.metrics.observe(
		metric.Labels{
			opLabel:      message.AppRequestOp.String(),
			handlerLabel: handlerID,
		},
		start,
	)
	return nil, nil
}

// RequestFailed handles failed requests
func (r *router) RequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, err *p2p.Error) error {
	start := time.Now()
	pending, ok := r.clearRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.callback(ctx, nodeID, nil, err)

	r.metrics.observe(
		metric.Labels{
			opLabel:      message.AppErrorOp.String(),
			handlerLabel: pending.handlerID,
		},
		start,
	)
	return nil
}

// Response handles incoming responses
func (r *router) Response(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	start := time.Now()
	pending, ok := r.clearRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.callback(ctx, nodeID, response, nil)

	r.metrics.observe(
		metric.Labels{
			opLabel:      message.AppResponseOp.String(),
			handlerLabel: pending.handlerID,
		},
		start,
	)
	return nil
}

// Gossip handles incoming gossip messages
func (r *router) Gossip(ctx context.Context, nodeID ids.NodeID, gossip []byte) error {
	start := time.Now()
	parsedMsg, handler, handlerID, ok := r.parse(gossip)
	if !ok {
		r.log.Debug("received message for unregistered handler",
			log.Stringer("messageOp", message.AppGossipOp),
			log.Stringer("nodeID", nodeID),
			log.Binary("message", gossip),
		)
		return nil
	}

	handler.HandleGossip(ctx, nodeID, parsedMsg)

	r.metrics.observe(
		metric.Labels{
			opLabel:      message.AppGossipOp.String(),
			handlerLabel: handlerID,
		},
		start,
	)
	return nil
}

// Parse parses a gossip or request message and maps it to a corresponding
// handler if present.
//
// Returns:
// - The unprefixed protocol message.
// - The protocol responder.
// - The protocol metric name.
// - A boolean indicating that parsing succeeded.
//
// Invariant: Assumes [r.lock] isn't held.
func (r *router) parse(prefixedMsg []byte) ([]byte, *responder, string, bool) {
	handlerID, msg, ok := ParseMessage(prefixedMsg)
	if !ok {
		return nil, nil, "", false
	}

	handlerStr := strconv.FormatUint(handlerID, 10)

	r.lock.RLock()
	defer r.lock.RUnlock()

	handler, ok := r.handlers[handlerID]
	return msg, handler, handlerStr, ok
}

// Invariant: Assumes [r.lock] isn't held.
func (r *router) clearRequest(requestID uint32) (pendingRequest, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	callback, ok := r.pendingRequests[requestID]
	delete(r.pendingRequests, requestID)
	return callback, ok
}

// Parse a gossip or request message.
//
// Returns:
// - The protocol ID.
// - The unprefixed protocol message.
// - A boolean indicating that parsing succeeded.
func ParseMessage(msg []byte) (uint64, []byte, bool) {
	handlerID, bytesRead := binary.Uvarint(msg)
	if bytesRead <= 0 {
		return 0, nil, false
	}
	return handlerID, msg[bytesRead:], true
}
