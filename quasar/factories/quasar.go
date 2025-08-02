// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factories

import (
	"github.com/luxfi/node/v2/quasar/engine/core"
	"github.com/luxfi/node/v2/quasar/engine/dag"
	"github.com/luxfi/node/v2/quasar/engine/chain"
	"github.com/luxfi/node/v2/quasar/engine/quasar"
)

// QuasarFactory creates Quasar consensus engines.
type QuasarFactory struct{}

// NewQuasarFactory creates a new Quasar factory.
func NewQuasarFactory() *QuasarFactory {
	return &QuasarFactory{}
}

// NewDAG creates a new DAG consensus engine using Quasar.
func (f *QuasarFactory) NewDAG(ctx *core.Context, vm interface{}) dag.Engine {
	return quasar.NewDAGEngine(ctx, vm)
}

// NewLinear creates a new linear consensus engine using Quasar.
func (f *QuasarFactory) NewLinear(ctx *core.Context, vm interface{}) chain.Engine {
	return quasar.NewLinearEngine(ctx, vm)
}

// Factory is the main consensus engine factory.
type Factory interface {
	// NewDAG creates a new DAG consensus engine.
	NewDAG(ctx *core.Context, vm interface{}) dag.Engine

	// NewLinear creates a new linear consensus engine.
	NewLinear(ctx *core.Context, vm interface{}) chain.Engine
}

// Config configures which consensus engine to use.
type Config struct {
	// UseQuasar enables the Quasar unified engine.
	UseQuasar bool

	// UseLegacy enables legacy Snowman/Avalanche engines.
	UseLegacy bool
}

// New creates a consensus factory based on config.
func New(config Config) Factory {
	if config.UseQuasar {
		return NewQuasarFactory()
	}
	// Default to Quasar
	return NewQuasarFactory()
}