// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package servicenodevm

import (
	"testing"

	"github.com/luxfi/consensus/core/choices"
	"github.com/luxfi/ids"
)

func TestServiceNodeVertexConflicts_SameNodeEpoch(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	v1 := &ServiceNodeVertex{
		id:     ids.GenerateTestID(),
		status: choices.Processing,
		keys:   []NodeEpochKey{{NodeID: nodeID, Epoch: 5}},
	}
	v2 := &ServiceNodeVertex{
		id:     ids.GenerateTestID(),
		status: choices.Processing,
		keys:   []NodeEpochKey{{NodeID: nodeID, Epoch: 5}},
	}

	if !v1.Conflicts(v2) {
		t.Fatal("expected conflict: same (nodeID, epoch)")
	}
	if !v2.Conflicts(v1) {
		t.Fatal("expected conflict: symmetric check failed")
	}
}

func TestServiceNodeVertexConflicts_DifferentNodes(t *testing.T) {
	v1 := &ServiceNodeVertex{
		id:     ids.GenerateTestID(),
		status: choices.Processing,
		keys:   []NodeEpochKey{{NodeID: ids.GenerateTestNodeID(), Epoch: 5}},
	}
	v2 := &ServiceNodeVertex{
		id:     ids.GenerateTestID(),
		status: choices.Processing,
		keys:   []NodeEpochKey{{NodeID: ids.GenerateTestNodeID(), Epoch: 5}},
	}

	if v1.Conflicts(v2) {
		t.Fatal("expected no conflict: different nodes in same epoch")
	}
}

func TestServiceNodeVertexConflicts_SameNodeDifferentEpoch(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()

	v1 := &ServiceNodeVertex{
		id:     ids.GenerateTestID(),
		status: choices.Processing,
		keys:   []NodeEpochKey{{NodeID: nodeID, Epoch: 1}},
	}
	v2 := &ServiceNodeVertex{
		id:     ids.GenerateTestID(),
		status: choices.Processing,
		keys:   []NodeEpochKey{{NodeID: nodeID, Epoch: 2}},
	}

	if v1.Conflicts(v2) {
		t.Fatal("expected no conflict: same node but different epochs")
	}
}
