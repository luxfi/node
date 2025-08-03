// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
)

// Router routes messages to consensus engines
type Router interface {
	// RegisterRequest marks that we should expect to receive a reply to a
	// request from the given chain. The router should call the timeout handler
	// if we don't get a reply in time.
	RegisterRequest(
		nodeID ids.NodeID,
		chainID ids.ID,
		requestID uint32,
		op message.Op,
		timeoutHandler func(),
		engineType EngineType,
	)
}

// EngineType ...
type EngineType uint32

const (
	// EngineTypeUnspecified ...
	EngineTypeUnspecified EngineType = iota
	// EngineTypeAvalanche ...
	EngineTypeAvalanche
	// EngineTypeSnowman ...
	EngineTypeSnowman
)