// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package snowtest provides test utilities for snow consensus
package snowtest

import (
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/log"
)

// ConsensusContext returns a test consensus context
func ConsensusContext(t testing.TB) *snow.Context {
	ctx := Context()
	ctx.Log = log.NewNoOpLogger()
	return ctx
}

// Context returns a test context
func Context() *snow.Context {
	return &snow.Context{
		NetworkID:    1,
		SubnetID:     ids.Empty,
		ChainID:      ids.GenerateTestID(),
		NodeID:       ids.GenerateTestNodeID(),
		XChainID:     ids.GenerateTestID(),
		CChainID:     ids.GenerateTestID(),
		AVAXAssetID:  ids.GenerateTestID(),
		ChainDataDir: "",
	}
}

// Validator creates a test validator
type Validator struct {
	NodeID ids.NodeID
	Weight uint64
}

// TestState is a test implementation of validators.State
type TestState struct {
	validators map[ids.ID]map[ids.NodeID]*Validator
}

// GetValidatorSet returns the validator set for the given subnet at the given height
func (ts *TestState) GetValidatorSet(pChainHeight uint64, subnetID ids.ID) (map[ids.NodeID]*Validator, error) {
	subnet, ok := ts.validators[subnetID]
	if !ok {
		return nil, nil
	}

	result := make(map[ids.NodeID]*Validator, len(subnet))
	for nodeID, val := range subnet {
		result[nodeID] = val
	}
	return result, nil
}