// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
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

	luxmetrics "github.com/luxfi/metric"
	"github.com/luxfi/log"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
)

var (
	ErrExistingAppProtocol = errors.New("existing app protocol")
	ErrUnrequestedResponse = errors.New("unrequested response")

	_ core.AppHandler = (*routerAppHandlerAdapter)(nil)
)

// routerAppHandlerAdapter adapts router to core.AppHandler
type routerAppHandlerAdapter struct {
	*router
}

// AppRequest implements core.AppHandler
func (r *routerAppHandlerAdapter) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	return r.router.AppRequest(ctx, nodeID, requestID, deadline, msg)
}

// AppResponse implements core.AppHandler
func (r *routerAppHandlerAdapter) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	return r.router.AppResponse(ctx, nodeID, requestID, msg)
}

// AppGossip implements core.AppHandler
func (r *routerAppHandlerAdapter) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return r.router.AppGossip(ctx, nodeID, msg)
}

type pendingAppRequest struct {
	handlerID string
	callback  AppResponseCallback
}

type pendingCrossChainAppRequest struct {
	handlerID string
	callback  CrossChainAppResponseCallback
}

// metrics defines the interface for collecting metrics
type metrics interface {
	observe(labels luxmetrics.Labels, start time.Time)
}

// meteredHandler emits metrics for a Handler
type meteredHandler struct {
	*responder
	metrics
}

type metricsImpl struct {
	msgTime  luxmetrics.GaugeVec
	msgCount luxmetrics.CounterVec
}

func (m *metricsImpl) observe(labels luxmetrics.Labels, start time.Time) {
	metricTime := m.msgTime.With(labels)
	metricCount := m.msgCount.With(labels)

	metricTime.Add(float64(time.Since(start)))
	metricCount.Inc()
}

// router routes incoming application messages to the corresponding registered
// app handler. App messages must be made using the registered handler's
// corresponding Client.
type router struct {
	log     log.Logger
	sender  core.AppSender
	metrics metrics

	lock                         sync.RWMutex
	handlers                     map[uint64]*meteredHandler
	pendingAppRequests           map[uint32]pendingAppRequest
	pendingCrossChainAppRequests map[uint32]pendingCrossChainAppRequest
	requestID                    uint32
}

// newRouter returns a new instance of Router
func newRouter(
	log log.Logger,
	sender core.AppSender,
	metrics metrics,
) *router {
	return &router{
		log:                          log,
		sender:                       sender,
		metrics:                      metrics,
		handlers:                     make(map[uint64]*meteredHandler),
		pendingAppRequests:           make(map[uint32]pendingAppRequest),
		pendingCrossChainAppRequests: make(map[uint32]pendingCrossChainAppRequest),
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

	r.handlers[handlerID] = &meteredHandler{
		responder: &responder{
			Handler:   handler,
			handlerID: handlerID,
			log:       r.log,
			sender:    r.sender,
		},
		metrics: r.metrics,
	}

	return nil
}

// AppRequest routes an AppRequest to a Handler based on the handler prefix. The
// message is dropped if no matching handler can be found.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
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

		// Send an error back to the requesting peer. Invalid requests that we
		// cannot parse a handler id for are handled the same way as requests
		// for which we do not have a registered handler.
		return r.sender.SendAppError(ctx, nodeID, requestID, ErrUnregisteredHandler.Code, ErrUnregisteredHandler.Message)
	}

	// call the corresponding handler and send back a response to nodeID
	if err := handler.AppRequest(ctx, nodeID, requestID, deadline, parsedMsg); err != nil {
		return err
	}

	r.metrics.observe(
		luxmetrics.Labels{
			opLabel:      message.AppRequestOp.String(),
			handlerLabel: handlerID,
		},
		start,
	)
	return nil
}

// AppRequestFailed routes an AppRequestFailed message to the callback
// corresponding to requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *core.AppError) error {
	start := time.Now()
	pending, ok := r.clearAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.callback(ctx, nodeID, nil, appErr)

	r.metrics.observe(
		luxmetrics.Labels{
			opLabel:      message.AppErrorOp.String(),
			handlerLabel: pending.handlerID,
		},
		start,
	)
	return nil
}

// AppResponse routes an AppResponse message to the callback corresponding to
// requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	start := time.Now()
	pending, ok := r.clearAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.callback(ctx, nodeID, response, nil)

	r.metrics.observe(
		luxmetrics.Labels{
			opLabel:      message.AppResponseOp.String(),
			handlerLabel: pending.handlerID,
		},
		start,
	)
	return nil
}

// AppGossip routes an AppGossip message to a Handler based on the handler
// prefix. The message is dropped if no matching handler can be found.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) AppGossip(ctx context.Context, nodeID ids.NodeID, gossip []byte) error {
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

	handler.AppGossip(ctx, nodeID, parsedMsg)

	r.metrics.observe(
		luxmetrics.Labels{
			opLabel:      message.AppGossipOp.String(),
			handlerLabel: handlerID,
		},
		start,
	)
	return nil
}

// CrossChainAppRequest routes a CrossChainAppRequest message to a Handler
// based on the handler prefix. The message is dropped if no matching handler
// can be found.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) CrossChainAppRequest(
	ctx context.Context,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	msg []byte,
) error {
	start := time.Now()
	parsedMsg, handler, handlerID, ok := r.parse(msg)
	if !ok {
		r.log.Debug("received message for unregistered handler",
			log.Stringer("messageOp", message.CrossChainAppRequestOp),
			log.Stringer("chainID", chainID),
			log.Uint32("requestID", requestID),
			log.Time("deadline", deadline),
			log.Binary("message", msg),
		)
		return nil
	}

	if err := handler.CrossChainAppRequest(ctx, chainID, requestID, deadline, parsedMsg); err != nil {
		return err
	}

	r.metrics.observe(
		luxmetrics.Labels{
			opLabel:      message.CrossChainAppRequestOp.String(),
			handlerLabel: handlerID,
		},
		start,
	)
	return nil
}

// CrossChainAppRequestFailed routes a CrossChainAppRequestFailed message to
// the callback corresponding to requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32, appErr *core.AppError) error {
	start := time.Now()
	pending, ok := r.clearCrossChainAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.callback(ctx, chainID, nil, appErr)

	r.metrics.observe(
		luxmetrics.Labels{
			opLabel:      message.CrossChainAppErrorOp.String(),
			handlerLabel: pending.handlerID,
		},
		start,
	)
	return nil
}

// CrossChainAppResponse routes a CrossChainAppResponse message to the callback
// corresponding to requestID.
//
// Any error condition propagated outside Handler application logic is
// considered fatal
func (r *router) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	start := time.Now()
	pending, ok := r.clearCrossChainAppRequest(requestID)
	if !ok {
		// we should never receive a timeout without a corresponding requestID
		return ErrUnrequestedResponse
	}

	pending.callback(ctx, chainID, response, nil)

	r.metrics.observe(
		luxmetrics.Labels{
			opLabel:      message.CrossChainAppResponseOp.String(),
			handlerLabel: pending.handlerID,
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
func (r *router) parse(prefixedMsg []byte) ([]byte, *meteredHandler, string, bool) {
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
func (r *router) clearAppRequest(requestID uint32) (pendingAppRequest, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	callback, ok := r.pendingAppRequests[requestID]
	delete(r.pendingAppRequests, requestID)
	return callback, ok
}

// Invariant: Assumes [r.lock] isn't held.
func (r *router) clearCrossChainAppRequest(requestID uint32) (pendingCrossChainAppRequest, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	callback, ok := r.pendingCrossChainAppRequests[requestID]
	delete(r.pendingCrossChainAppRequests, requestID)
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
