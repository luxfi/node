// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/engine/common"
	"github.com/luxdefi/luxd/trace"
)

var _ Engine = (*tracedEngine)(nil)

type tracedEngine struct {
	common.Engine
	engine Engine
}

func TraceEngine(engine Engine, tracer trace.Tracer) Engine {
	return &tracedEngine{
		Engine: common.TraceEngine(engine, tracer),
		engine: engine,
	}
}

func (e *tracedEngine) GetVtx(vtxID ids.ID) (lux.Vertex, error) {
	return e.engine.GetVtx(vtxID)
}
