// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package consensustest

import (
	"testing"
	
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar"
)

// CChainID is a test chain ID
var CChainID = ids.GenerateTestID()

// Context creates a test consensus context
func Context(t *testing.T, chainID ids.ID) *quasar.Context {
	state := &quasar.EngineState{}
	return &quasar.Context{
		NetworkID: 10, // constants.UnitTestID
		ChainID:   chainID,
		State:     state,
		CChainID:  CChainID,
		XChainID:  XChainID,
	}
}

// ConsensusContext returns the context as-is (for compatibility)
func ConsensusContext(ctx *quasar.Context) *quasar.Context {
	return ctx
}