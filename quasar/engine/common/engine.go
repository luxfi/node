// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/version"
)

// Engine describes the common functionality of all consensus engines
type Engine interface {
	Handler

	// GetVM returns this engine's VM
	GetVM() interface{}
}

// Handler defines the functions that a consensus engine must implement
type Handler interface {
	AppHandler

	// Notify this engine of peer changes.
	Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error
	Disconnected(ctx context.Context, nodeID ids.NodeID) error

	// HealthCheck returns nil if this engine is healthy.
	// Otherwise, it should return an error that will prevent the node from
	// reporting healthy.
	HealthCheck(ctx context.Context) (interface{}, error)
}

// AppHandler defines application-level functionality that must be implemented
// by a consensus engine
type AppHandler interface {
	// AppRequest handles an application-level request to this node.
	AppRequest(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		deadline time.Time,
		msg []byte,
	) error

	// AppResponse handles an application-level response to a request this node
	// sent.
	AppResponse(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		msg []byte,
	) error

	// AppRequestFailed notifies the consensus engine that an AppRequest it
	// sent failed or timed out.
	AppRequestFailed(
		ctx context.Context,
		nodeID ids.NodeID,
		requestID uint32,
		appErr *AppError,
	) error

	// CrossChainAppRequest handles a cross-chain request.
	CrossChainAppRequest(
		ctx context.Context,
		chainID ids.ID,
		requestID uint32,
		deadline time.Time,
		msg []byte,
	) error

	// CrossChainAppResponse handles a cross-chain response to a request this
	// node sent.
	CrossChainAppResponse(
		ctx context.Context,
		chainID ids.ID,
		requestID uint32,
		msg []byte,
	) error

	// CrossChainAppRequestFailed notifies the consensus engine that a
	// CrossChainAppRequest it sent failed or timed out.
	CrossChainAppRequestFailed(
		ctx context.Context,
		chainID ids.ID,
		requestID uint32,
		appErr *AppError,
	) error
}

// SendAppError is used to send an application-level error message.
type SendAppError func(nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error

// AppError represents an application-level error
type AppError struct {
	Code    int32
	Message string
}