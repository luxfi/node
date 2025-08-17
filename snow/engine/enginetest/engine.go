// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"testing"

	"github.com/luxfi/node/snow/engine/common"
)

// Engine is a test engine that embeds common.Engine
type Engine struct {
	common.Engine
	T *testing.T
}

// NewEngine creates a new test engine
func NewEngine(t *testing.T) *Engine {
	return &Engine{
		T: t,
	}
}