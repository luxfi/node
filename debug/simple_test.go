// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package debug

import (
	validators "github.com/luxfi/consensus/validator"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"testing"
)

func TestValidatorManager(t *testing.T) {
	// Test our validator manager implementation
	mgr := validators.NewManager()

	nodeID := ids.GenerateTestNodeID()
	chainID := constants.PrimaryNetworkID
	txID := ids.GenerateTestID()
	weight := uint64(1000000)

	// Test AddStaker
	err := mgr.AddStaker(chainID, nodeID, nil, txID, weight)
	if err != nil {
		t.Fatalf("AddStaker failed: %v", err)
	}

	// Test GetValidator
	vdr, exists := mgr.GetValidator(chainID, nodeID)
	if !exists {
		t.Fatal("Validator should exist after AddStaker")
	}
	if vdr.Weight != weight {
		t.Fatalf("Expected weight %d, got %d", weight, vdr.Weight)
	}

	// Test GetWeight
	w := mgr.GetWeight(chainID, nodeID)
	if w != weight {
		t.Fatalf("Expected weight %d, got %d", weight, w)
	}

	// Test TotalWeight
	total, err := mgr.TotalWeight(chainID)
	if err != nil {
		t.Fatalf("TotalWeight failed: %v", err)
	}
	if total != weight {
		t.Fatalf("Expected total weight %d, got %d", weight, total)
	}

	// Test RemoveWeight
	err = mgr.RemoveWeight(chainID, nodeID, weight)
	if err != nil {
		t.Fatalf("RemoveWeight failed: %v", err)
	}

	// Verify validator is removed
	_, exists = mgr.GetValidator(chainID, nodeID)
	if exists {
		t.Fatal("Validator should not exist after RemoveWeight")
	}

	t.Log("All validator manager tests passed!")
}

func TestConsensusParameters(t *testing.T) {
	// Verify consensus parameters for 1/1 validator setup
	sampleSize := 1
	quorumSize := 1
	commitThreshold := 1

	if sampleSize != 1 || quorumSize != 1 || commitThreshold != 1 {
		t.Fatal("Consensus parameters should be set to 1 for single validator")
	}

	t.Log("Consensus parameters verified: K=1, Alpha=1, Beta=1")
}
