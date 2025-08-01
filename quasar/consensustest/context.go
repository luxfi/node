// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consensustest

import (
	"testing"
	
	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus"
)

// CChainID is a test chain ID
var CChainID = ids.GenerateTestID()

// Context creates a test consensus context
func Context(t *testing.T, chainID ids.ID) *consensus.Context {
	return &consensus.Context{
		ChainID: chainID,
		State:   &consensus.EngineState{},
	}
}

// ConsensusContext returns the context as-is (for compatibility)
func ConsensusContext(ctx *consensus.Context) *consensus.Context {
	return ctx
}