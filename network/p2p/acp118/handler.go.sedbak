// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package acp118 implements ACP-118 message handling
package acp118

import (
	"context"
	"time"
	

	"github.com/luxfi/ids"
)

// Handler handles ACP-118 messages
type Handler interface {
	// AppRequest handles an incoming request
	AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, request []byte) ([]byte, error)
}

// NoOpHandler is a no-op implementation of Handler
type NoOpHandler struct{}

// AppRequest returns an empty response
func (NoOpHandler) AppRequest(context.Context, ids.NodeID, time.Time, []byte) ([]byte, error) {
	return nil, nil
}