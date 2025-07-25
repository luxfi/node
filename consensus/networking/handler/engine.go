// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/consensus"
	
	"github.com/luxfi/node/consensus/engine/core"
)

// Engine is a wrapper around a consensus engine's components.
type Engine struct {
	StateSyncer  core.StateSyncer
	Bootstrapper core.BootstrapableEngine
	Consensus    core.Engine
}

// Get returns the engine corresponding to the provided state,
// and whether its corresponding engine is initialized (not nil).
func (e *Engine) Get(state consensus.State) (core.Engine, bool) {
	if e == nil {
		return nil, false
	}
	switch state {
	case consensus.StateSyncing:
		return e.StateSyncer, e.StateSyncer != nil
	case consensus.Bootstrapping:
		return e.Bootstrapper, e.Bootstrapper != nil
	case consensus.NormalOp:
		return e.Consensus, e.Consensus != nil
	default:
		return nil, false
	}
}

// EngineManager resolves the engine that should be used given the current
// execution context of the chain.
type EngineManager struct {
	Dag   *Engine
	Chain *Engine
}

// Get returns the engine corresponding to the provided type if possible.
// If an engine type is not specified, the initial engine type is returned.
func (e *EngineManager) Get(engineType p2p.EngineType) *Engine {
	switch engineType {
	case p2p.EngineType_ENGINE_TYPE_DAG:
		return e.Dag
	case p2p.EngineType_ENGINE_TYPE_CHAIN:
		return e.Chain
	default:
		return nil
	}
}
