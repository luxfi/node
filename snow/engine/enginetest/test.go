// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package enginetest provides test utilities for consensus engines
package enginetest

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/version"
)

// Engine is a test engine
type Engine struct {
	T common.Engine

	StartF              func(context.Context, uint32) error
	IsBootstrappedF     func() bool
	ContextF            func() interface{}
	StopF               func(context.Context) error
	NotifyF             func(context.Context, common.Message) error
	GetVMF              func() interface{}
	SetStateF           func(context.Context, interface{}) error
	HealthCheckF        func(context.Context) (interface{}, error)
	ConnectedF          func(context.Context, ids.NodeID, *version.Application) error
	DisconnectedF       func(context.Context, ids.NodeID) error
	GetAcceptedFrontierF func(context.Context, ids.NodeID, uint32) error
}

// Default returns a default test engine
func Default() *Engine {
	return &Engine{
		StartF:              func(context.Context, uint32) error { return nil },
		IsBootstrappedF:     func() bool { return true },
		ContextF:            func() interface{} { return nil },
		StopF:               func(context.Context) error { return nil },
		NotifyF:             func(context.Context, common.Message) error { return nil },
		GetVMF:              func() interface{} { return nil },
		SetStateF:           func(context.Context, interface{}) error { return nil },
		HealthCheckF:        func(context.Context) (interface{}, error) { return nil, nil },
		ConnectedF:          func(context.Context, ids.NodeID, *version.Application) error { return nil },
		DisconnectedF:       func(context.Context, ids.NodeID) error { return nil },
		GetAcceptedFrontierF: func(context.Context, ids.NodeID, uint32) error { return nil },
	}
}

// Start implements common.Engine
func (e *Engine) Start(ctx context.Context, request uint32) error {
	if e.StartF != nil {
		return e.StartF(ctx, request)
	}
	return nil
}

// GetVM implements common.Engine
func (e *Engine) GetVM() interface{} {
	if e.GetVMF != nil {
		return e.GetVMF()
	}
	if e.T != nil {
		return e.T.GetVM()
	}
	return nil
}

// Connected implements common.Handler
func (e *Engine) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	if e.ConnectedF != nil {
		return e.ConnectedF(ctx, nodeID, nodeVersion)
	}
	if e.T != nil {
		return e.T.Connected(ctx, nodeID, nodeVersion)
	}
	return nil
}

// Disconnected implements common.Handler
func (e *Engine) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if e.DisconnectedF != nil {
		return e.DisconnectedF(ctx, nodeID)
	}
	if e.T != nil {
		return e.T.Disconnected(ctx, nodeID)
	}
	return nil
}

// HealthCheck implements common.Handler
func (e *Engine) HealthCheck(ctx context.Context) (interface{}, error) {
	if e.HealthCheckF != nil {
		return e.HealthCheckF(ctx)
	}
	if e.T != nil {
		return e.T.HealthCheck(ctx)
	}
	return nil, nil
}

// AppRequest implements common.AppHandler
func (e *Engine) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, msg []byte) error {
	if e.T != nil {
		return e.T.AppRequest(ctx, nodeID, requestID, deadline, msg)
	}
	return nil
}

// AppResponse implements common.AppHandler
func (e *Engine) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	if e.T != nil {
		return e.T.AppResponse(ctx, nodeID, requestID, msg)
	}
	return nil
}

// AppRequestFailed implements common.AppHandler
func (e *Engine) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	if e.T != nil {
		return e.T.AppRequestFailed(ctx, nodeID, requestID, appErr)
	}
	return nil
}

// CrossChainAppRequest implements common.AppHandler
func (e *Engine) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, msg []byte) error {
	if e.T != nil {
		return e.T.CrossChainAppRequest(ctx, chainID, requestID, deadline, msg)
	}
	return nil
}

// CrossChainAppResponse implements common.AppHandler
func (e *Engine) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	if e.T != nil {
		return e.T.CrossChainAppResponse(ctx, chainID, requestID, msg)
	}
	return nil
}

// CrossChainAppRequestFailed implements common.AppHandler
func (e *Engine) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32, appErr *common.AppError) error {
	if e.T != nil {
		return e.T.CrossChainAppRequestFailed(ctx, chainID, requestID, appErr)
	}
	return nil
}

// Message is a test message
type Message struct {
	InboundMessageF    func() common.Message
	OnFinalizeF        func()
	OnDropF            func()
}

// InboundMessage implements common.Message
func (m *Message) InboundMessage() common.Message {
	if m.InboundMessageF != nil {
		return m.InboundMessageF()
	}
	return nil
}

// OnFinalize implements common.Message
func (m *Message) OnFinalize() {
	if m.OnFinalizeF != nil {
		m.OnFinalizeF()
	}
}

// OnDrop implements common.Message
func (m *Message) OnDrop() {
	if m.OnDropF != nil {
		m.OnDropF()
	}
}