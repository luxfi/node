// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertextest

import (
	"testing"

	"github.com/luxfi/node/consensus/engine/graph/vertex"
)

var _ vertex.Manager = (*Manager)(nil)

type Manager struct {
	Builder
	Parser
	Storage
}

func NewManager(t *testing.T) *Manager {
	return &Manager{
		Builder: Builder{T: t},
		Parser:  Parser{T: t},
		Storage: Storage{T: t},
	}
}

func (m *Manager) Default(cant bool) {
	m.Builder.Default(cant)
	m.Parser.Default(cant)
	m.Storage.Default(cant)
}
