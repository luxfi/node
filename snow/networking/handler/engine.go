// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/engine/common"
)

// Engine is a wrapper around a consensus engine's components.
type Engine struct {
	StateSyncer  common.StateSyncer
	Bootstrapper common.BootstrapableEngine
	Consensus    common.Engine
}

// Get returns the engine corresponding to the provided state,
// and whether its corresponding engine is initialized (not nil).
func (e *Engine) Get(state snow.State) (common.Engine, bool) {
	if e == nil {
		return nil, false
	}
	switch state {
	case snow.StateSyncing:
		return e.StateSyncer, e.StateSyncer != nil
	case snow.Bootstrapping:
		return e.Bootstrapper, e.Bootstrapper != nil
	case snow.NormalOp:
		return e.Consensus, e.Consensus != nil
	default:
		return nil, false
	}
}

// EngineManager resolves the engine that should be used given the current
// execution context of the chain.
type EngineManager struct {
	Lux *Engine
	Snowman   *Engine
}

// Get returns the engine corresponding to the provided type if possible.
// If an engine type is not specified, the initial engine type is returned.
func (e *EngineManager) Get(engineType p2p.EngineType) *Engine {
	switch engineType {
	case p2p.EngineType_ENGINE_TYPE_LUX:
		return e.Lux
	case p2p.EngineType_ENGINE_TYPE_SNOWMAN:
		return e.Snowman
	default:
		return nil
	}
}
