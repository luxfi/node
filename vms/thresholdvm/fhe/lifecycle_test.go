// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fhe

import (
	"bytes"
	"testing"
	"time"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/stretchr/testify/require"
)

func newTestLifecycleManager(t *testing.T) (*LifecycleManager, *Registry) {
	db := memdb.New()
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	config := &LifecycleConfig{
		EpochDuration:     100,
		GracePeriod:       10,
		MinCommitteeSize:  2,
		MaxCommitteeSize:  10,
		DefaultThreshold:  2,
		DKGTimeout:        time.Second,
		KeyRotationBlocks: 0,
	}

	logger := log.NewLogger("test")
	lm := NewLifecycleManager(registry, config, logger)
	require.NotNil(t, lm)

	return lm, registry
}

func TestLifecycleManagerInit(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NotNil(t, lm)

	err := lm.Start()
	require.NoError(t, err)

	lm.Stop()
}

func TestInitiateEpoch(t *testing.T) {
	lm, registry := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	// Create committee
	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk3"), Weight: 100, Index: 2},
	}

	err := lm.InitiateEpoch(committee, 2, []byte("aggregated_public_key"))
	require.NoError(t, err)

	// Verify epoch was created
	epoch := registry.GetCurrentEpoch()
	require.Equal(t, uint64(1), epoch)

	epochInfo, err := registry.GetEpoch(1)
	require.NoError(t, err)
	require.Equal(t, 3, len(epochInfo.Committee))
	require.Equal(t, 2, epochInfo.Threshold)
	require.Equal(t, EpochActive, epochInfo.Status)
}

func TestInitiateEpochValidation(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	// Too few committee members
	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
	}
	err := lm.InitiateEpoch(committee, 1, []byte("pk"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "below minimum")

	// Invalid threshold
	committee = []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	err = lm.InitiateEpoch(committee, 0, []byte("pk"))
	require.ErrorIs(t, err, ErrInvalidThreshold)

	err = lm.InitiateEpoch(committee, 5, []byte("pk"))
	require.ErrorIs(t, err, ErrInvalidThreshold)
}

func TestRegisterMember(t *testing.T) {
	lm, registry := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	// Initialize epoch first
	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("pk")))

	// Register new member
	newNode := ids.GenerateTestNodeID()
	err := lm.RegisterMember(newNode, []byte("new_pk"), 150)
	require.NoError(t, err)

	// Verify member was added
	members, err := registry.GetCommittee()
	require.NoError(t, err)
	require.Equal(t, 3, len(members))

	// Try to register same member again
	err = lm.RegisterMember(newNode, []byte("new_pk"), 150)
	require.ErrorIs(t, err, ErrMemberAlreadyExists)
}

func TestRemoveMember(t *testing.T) {
	lm, registry := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	node1 := ids.GenerateTestNodeID()
	node2 := ids.GenerateTestNodeID()
	node3 := ids.GenerateTestNodeID()

	committee := []CommitteeMember{
		{NodeID: node1, PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: node2, PublicKey: []byte("pk2"), Weight: 100, Index: 1},
		{NodeID: node3, PublicKey: []byte("pk3"), Weight: 100, Index: 2},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("pk")))

	// Remove member
	err := lm.RemoveMember(node2)
	require.NoError(t, err)

	members, err := registry.GetCommittee()
	require.NoError(t, err)
	require.Equal(t, 2, len(members))

	// Verify node2 is gone
	for _, m := range members {
		require.NotEqual(t, node2, m.NodeID)
	}
}

func TestStartDKG(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	participants := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}

	err := lm.StartDKG(2, participants, 2)
	require.NoError(t, err)

	state := lm.GetDKGState()
	require.NotNil(t, state)
	require.Equal(t, uint64(2), state.Epoch)
	require.Equal(t, 3, len(state.Participants))
	require.Equal(t, 2, state.Threshold)
	require.Equal(t, DKGCommitPhase, state.Status)
}

func TestStartDKGValidation(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	// Not enough participants
	participants := []ids.NodeID{ids.GenerateTestNodeID()}
	err := lm.StartDKG(1, participants, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not enough participants")

	// Invalid threshold
	participants = []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}
	err = lm.StartDKG(1, participants, 0)
	require.ErrorIs(t, err, ErrInvalidThreshold)

	err = lm.StartDKG(1, participants, 5)
	require.ErrorIs(t, err, ErrInvalidThreshold)
}

func TestDKGCommitmentPhase(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	node1 := ids.GenerateTestNodeID()
	node2 := ids.GenerateTestNodeID()
	participants := []ids.NodeID{node1, node2}

	require.NoError(t, lm.StartDKG(1, participants, 2))

	// Submit commitments
	err := lm.SubmitDKGCommitment(node1, bytes.Repeat([]byte{0xAA}, 32))
	require.NoError(t, err)

	state := lm.GetDKGState()
	require.Equal(t, DKGCommitPhase, state.Status)
	require.Equal(t, 1, len(state.Commitments))

	// Submit second commitment - should move to share phase
	err = lm.SubmitDKGCommitment(node2, bytes.Repeat([]byte{0x55}, 32))
	require.NoError(t, err)

	state = lm.GetDKGState()
	require.Equal(t, DKGSharePhase, state.Status)
	require.Equal(t, 2, len(state.Commitments))
}

func TestDKGSharePhase(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	node1 := ids.GenerateTestNodeID()
	node2 := ids.GenerateTestNodeID()
	participants := []ids.NodeID{node1, node2}

	require.NoError(t, lm.StartDKG(1, participants, 2))

	// Complete commit phase
	require.NoError(t, lm.SubmitDKGCommitment(node1, bytes.Repeat([]byte{0xAA}, 32)))
	require.NoError(t, lm.SubmitDKGCommitment(node2, bytes.Repeat([]byte{0x55}, 32)))

	// Submit shares
	err := lm.SubmitDKGShare(node1, []byte("share1"))
	require.NoError(t, err)

	state := lm.GetDKGState()
	require.Equal(t, DKGSharePhase, state.Status)

	// Submit second share - should complete DKG
	err = lm.SubmitDKGShare(node2, []byte("share2"))
	require.NoError(t, err)

	state = lm.GetDKGState()
	require.Equal(t, DKGCompleted, state.Status)
	require.NotEmpty(t, state.PublicKey)
}

func TestDKGInProgressError(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	participants := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}

	require.NoError(t, lm.StartDKG(1, participants, 2))

	// Try to start another DKG
	err := lm.StartDKG(2, participants, 2)
	require.ErrorIs(t, err, ErrDKGInProgress)
}

func TestAbortDKG(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	participants := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}

	require.NoError(t, lm.StartDKG(1, participants, 2))

	err := lm.AbortDKG("test abort reason")
	require.NoError(t, err)

	state := lm.GetDKGState()
	require.Equal(t, DKGAborted, state.Status)
	require.Equal(t, "test abort reason", state.Error)
}

func TestDKGCallback(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	var callbackEpoch uint64
	var callbackPK []byte
	lm.SetCallbacks(nil, nil, func(epoch uint64, pk []byte) {
		callbackEpoch = epoch
		callbackPK = pk
	})

	node1 := ids.GenerateTestNodeID()
	node2 := ids.GenerateTestNodeID()
	participants := []ids.NodeID{node1, node2}

	require.NoError(t, lm.StartDKG(5, participants, 2))
	require.NoError(t, lm.SubmitDKGCommitment(node1, bytes.Repeat([]byte{0xAA}, 32)))
	require.NoError(t, lm.SubmitDKGCommitment(node2, bytes.Repeat([]byte{0x55}, 32)))
	require.NoError(t, lm.SubmitDKGShare(node1, []byte("share1")))
	require.NoError(t, lm.SubmitDKGShare(node2, []byte("share2")))

	require.Equal(t, uint64(5), callbackEpoch)
	require.NotEmpty(t, callbackPK)
}

func TestGetStatus(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("pk")))

	status, err := lm.GetStatus()
	require.NoError(t, err)
	require.Equal(t, uint64(1), status.CurrentEpoch)
	require.Equal(t, 2, status.CommitteeSize)
	require.Equal(t, 2, status.Threshold)
	require.False(t, status.IsTransitioning)
}

func TestGetCommitteeWeight(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 200, Index: 1},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk3"), Weight: 300, Index: 2},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("pk")))

	weight, err := lm.GetCommitteeWeight()
	require.NoError(t, err)
	require.Equal(t, uint64(600), weight)
}

func TestGetEpochKeyInfo(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("epoch_public_key")))

	info, err := lm.GetEpochKeyInfo(1)
	require.NoError(t, err)
	require.Equal(t, uint64(1), info.Epoch)
	require.Equal(t, []byte("epoch_public_key"), info.PublicKey)
	require.Equal(t, 2, info.Threshold)
	require.Equal(t, 2, info.Committee)
	require.True(t, info.IsActive)
}

func TestIsTransitioning(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	require.False(t, lm.IsTransitioning())

	// Force a transition
	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("pk")))

	err := lm.ForceEpochTransition()
	require.NoError(t, err)

	require.True(t, lm.IsTransitioning())

	state := lm.GetTransitionState()
	require.NotNil(t, state)
	require.Equal(t, uint64(1), state.FromEpoch)
	require.Equal(t, uint64(2), state.ToEpoch)
}

func TestDKGStatusString(t *testing.T) {
	tests := []struct {
		status   DKGStatus
		expected string
	}{
		{DKGPending, "pending"},
		{DKGCommitPhase, "commit_phase"},
		{DKGSharePhase, "share_phase"},
		{DKGCompleted, "completed"},
		{DKGFailed, "failed"},
		{DKGAborted, "aborted"},
		{DKGStatus(99), "unknown"},
	}

	for _, tc := range tests {
		require.Equal(t, tc.expected, tc.status.String())
	}
}

func TestTransitionStatusString(t *testing.T) {
	tests := []struct {
		status   TransitionStatus
		expected string
	}{
		{TransitionPending, "pending"},
		{TransitionDKGPhase, "dkg_phase"},
		{TransitionMigrationPhase, "migration_phase"},
		{TransitionFinalizingPhase, "finalizing"},
		{TransitionCompleted, "completed"},
		{TransitionFailed, "failed"},
		{TransitionStatus(99), "unknown"},
	}

	for _, tc := range tests {
		require.Equal(t, tc.expected, tc.status.String())
	}
}

func TestMemberStatusString(t *testing.T) {
	tests := []struct {
		status   MemberStatus
		expected string
	}{
		{MemberPending, "pending"},
		{MemberActive, "active"},
		{MemberInactive, "inactive"},
		{MemberSlashed, "slashed"},
		{MemberExiting, "exiting"},
		{MemberStatus(99), "unknown"},
	}

	for _, tc := range tests {
		require.Equal(t, tc.expected, tc.status.String())
	}
}

func TestDefaultLifecycleConfig(t *testing.T) {
	config := DefaultLifecycleConfig()
	require.NotNil(t, config)
	require.Equal(t, uint64(100000), config.EpochDuration)
	require.Equal(t, uint64(1000), config.GracePeriod)
	require.Equal(t, 4, config.MinCommitteeSize)
	require.Equal(t, 100, config.MaxCommitteeSize)
	require.Equal(t, 67, config.DefaultThreshold)
	require.Equal(t, 5*time.Minute, config.DKGTimeout)
}

func TestSlashMember(t *testing.T) {
	lm, registry := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	node1 := ids.GenerateTestNodeID()
	node2 := ids.GenerateTestNodeID()

	committee := []CommitteeMember{
		{NodeID: node1, PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: node2, PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("pk")))

	// Slash node1
	err := lm.SlashMember(node1, "misbehavior detected")
	require.NoError(t, err)

	// Verify node1 is removed
	members, err := registry.GetCommittee()
	require.NoError(t, err)
	require.Equal(t, 1, len(members))
	require.Equal(t, node2, members[0].NodeID)
}

func TestValidateThresholdMet(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk3"), Weight: 100, Index: 2},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("pk")))

	// Threshold met
	err := lm.ValidateThresholdMet(2)
	require.NoError(t, err)

	err = lm.ValidateThresholdMet(3)
	require.NoError(t, err)

	// Threshold not met
	err = lm.ValidateThresholdMet(1)
	require.ErrorIs(t, err, ErrInsufficientWeight)
}

func TestOnBlockNoTransition(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("pk")))

	// Process block before epoch ends - no transition
	err := lm.OnBlock(50)
	require.NoError(t, err)
	require.False(t, lm.IsTransitioning())
}

func TestOnBlockTriggersTransition(t *testing.T) {
	db := memdb.New()
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	// Short epoch for testing
	config := &LifecycleConfig{
		EpochDuration:     10, // Very short epoch
		GracePeriod:       2,
		MinCommitteeSize:  2,
		MaxCommitteeSize:  10,
		DefaultThreshold:  2,
		DKGTimeout:        time.Second,
		KeyRotationBlocks: 0,
	}

	logger := log.NewLogger("test")
	lm := NewLifecycleManager(registry, config, logger)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}

	// Use block height as StartTime for test consistency
	epochInfo := &EpochInfo{
		Epoch:     1,
		StartTime: 0, // Block 0
		Committee: committee,
		Threshold: 2,
		PublicKey: []byte("pk"),
		Status:    EpochActive,
	}
	require.NoError(t, registry.SetEpoch(1, epochInfo))

	// Process block that should trigger transition
	err = lm.OnBlock(15)
	require.NoError(t, err)
	require.True(t, lm.IsTransitioning())

	// Verify DKG started for new epoch
	state := lm.GetDKGState()
	require.NotNil(t, state)
	require.Equal(t, uint64(2), state.Epoch)
}

func TestKeyRotationOnBlock(t *testing.T) {
	db := memdb.New()
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	config := &LifecycleConfig{
		EpochDuration:     1000,
		GracePeriod:       10,
		MinCommitteeSize:  2,
		MaxCommitteeSize:  10,
		DefaultThreshold:  2,
		DKGTimeout:        time.Second,
		KeyRotationBlocks: 50, // Rotate every 50 blocks
	}

	logger := log.NewLogger("test")
	lm := NewLifecycleManager(registry, config, logger)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}

	epochInfo := &EpochInfo{
		Epoch:     1,
		StartTime: 0,
		Committee: committee,
		Threshold: 2,
		PublicKey: []byte("pk"),
		Status:    EpochActive,
	}
	require.NoError(t, registry.SetEpoch(1, epochInfo))

	// Process block at key rotation interval
	err = lm.OnBlock(50)
	require.NoError(t, err)

	// Should have started DKG for key rotation (same epoch)
	state := lm.GetDKGState()
	require.NotNil(t, state)
	require.Equal(t, uint64(1), state.Epoch) // Same epoch for rotation
}

func TestFinalizeTransition(t *testing.T) {
	db := memdb.New()
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	config := &LifecycleConfig{
		EpochDuration:     10,
		GracePeriod:       2,
		MinCommitteeSize:  2,
		MaxCommitteeSize:  10,
		DefaultThreshold:  2,
		DKGTimeout:        time.Second,
		KeyRotationBlocks: 0,
	}

	logger := log.NewLogger("test")
	lm := NewLifecycleManager(registry, config, logger)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	node1 := ids.GenerateTestNodeID()
	node2 := ids.GenerateTestNodeID()
	committee := []CommitteeMember{
		{NodeID: node1, PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: node2, PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}

	epochInfo := &EpochInfo{
		Epoch:     1,
		StartTime: 0,
		Committee: committee,
		Threshold: 2,
		PublicKey: []byte("pk"),
		Status:    EpochActive,
	}
	require.NoError(t, registry.SetEpoch(1, epochInfo))

	// Trigger transition
	err = lm.OnBlock(15)
	require.NoError(t, err)
	require.True(t, lm.IsTransitioning())

	// Complete DKG
	require.NoError(t, lm.SubmitDKGCommitment(node1, bytes.Repeat([]byte{0xAA}, 32)))
	require.NoError(t, lm.SubmitDKGCommitment(node2, bytes.Repeat([]byte{0x55}, 32)))
	require.NoError(t, lm.SubmitDKGShare(node1, []byte("share1")))
	require.NoError(t, lm.SubmitDKGShare(node2, []byte("share2")))

	// Update transition to finalizing phase manually for test
	// Also set StartedAt to block-compatible value for the test
	lm.mu.Lock()
	lm.currentTransition.Status = TransitionFinalizingPhase
	lm.currentTransition.StartedAt = 15 // Started at block 15
	lm.mu.Unlock()

	// Process block past grace period to finalize (15 + 2 grace = 17, so block 20 is past)
	err = lm.OnBlock(20)
	require.NoError(t, err)

	// Transition should be complete
	require.False(t, lm.IsTransitioning())
	require.Equal(t, uint64(2), registry.GetCurrentEpoch())
}

func TestEpochChangeCallback(t *testing.T) {
	lm, registry := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	var callbackOld, callbackNew uint64
	lm.SetCallbacks(func(oldEpoch, newEpoch uint64) {
		callbackOld = oldEpoch
		callbackNew = newEpoch
	}, nil, nil)

	node1 := ids.GenerateTestNodeID()
	node2 := ids.GenerateTestNodeID()
	committee := []CommitteeMember{
		{NodeID: node1, PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: node2, PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("pk")))

	// Force transition
	require.NoError(t, lm.ForceEpochTransition())

	// Complete DKG
	require.NoError(t, lm.SubmitDKGCommitment(node1, bytes.Repeat([]byte{0xAA}, 32)))
	require.NoError(t, lm.SubmitDKGCommitment(node2, bytes.Repeat([]byte{0x55}, 32)))
	require.NoError(t, lm.SubmitDKGShare(node1, []byte("share1")))
	require.NoError(t, lm.SubmitDKGShare(node2, []byte("share2")))

	// Update transition to finalizing phase
	// Set StartedAt to block-compatible value for the test
	lm.mu.Lock()
	lm.currentTransition.Status = TransitionFinalizingPhase
	lm.currentTransition.StartedAt = 50 // Started at block 50
	lm.mu.Unlock()

	// Finalize - block 100 is past grace period (50 + default grace)
	epochInfo := &EpochInfo{Epoch: 1, StartTime: 0, Status: EpochActive, Committee: committee, Threshold: 2}
	require.NoError(t, registry.SetEpoch(1, epochInfo))

	err := lm.OnBlock(100)
	require.NoError(t, err)

	require.Equal(t, uint64(1), callbackOld)
	require.Equal(t, uint64(2), callbackNew)
}

func TestCommitteeFullError(t *testing.T) {
	db := memdb.New()
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	config := &LifecycleConfig{
		EpochDuration:     100,
		GracePeriod:       10,
		MinCommitteeSize:  1,
		MaxCommitteeSize:  2, // Very small max
		DefaultThreshold:  1,
		DKGTimeout:        time.Second,
		KeyRotationBlocks: 0,
	}

	logger := log.NewLogger("test")
	lm := NewLifecycleManager(registry, config, logger)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 1, []byte("pk")))

	// Try to add a third member when max is 2
	err = lm.RegisterMember(ids.GenerateTestNodeID(), []byte("pk3"), 100)
	require.ErrorIs(t, err, ErrCommitteeFull)
}

func TestDKGClone(t *testing.T) {
	state := &DKGState{
		CeremonyID:   [32]byte{1, 2, 3},
		Epoch:        5,
		Participants: []ids.NodeID{ids.GenerateTestNodeID(), ids.GenerateTestNodeID()},
		Threshold:    2,
		Shares:       map[string][]byte{"node1": {1, 2, 3}},
		Commitments:  map[string][]byte{"node1": {4, 5, 6}},
		PublicKey:    []byte{7, 8, 9},
		Status:       DKGCompleted,
		StartedAt:    time.Now().Unix(),
		CompletedAt:  time.Now().Unix(),
	}

	clone := state.Clone()
	require.NotNil(t, clone)
	require.Equal(t, state.Epoch, clone.Epoch)
	require.Equal(t, state.Threshold, clone.Threshold)
	require.Equal(t, len(state.Participants), len(clone.Participants))
	require.Equal(t, state.PublicKey, clone.PublicKey)
	require.Equal(t, state.Status, clone.Status)

	// Modify clone to ensure deep copy
	clone.Shares["node2"] = []byte{10, 11, 12}
	require.NotEqual(t, len(state.Shares), len(clone.Shares))

	// Test nil clone
	var nilState *DKGState
	require.Nil(t, nilState.Clone())
}

func TestPersistState(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	// Set up DKG state
	participants := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}
	require.NoError(t, lm.StartDKG(1, participants, 2))

	// Persist should succeed
	err := lm.persistState()
	require.NoError(t, err)
}

func TestForceEpochTransitionAlreadyInProgress(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	committee := []CommitteeMember{
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk1"), Weight: 100, Index: 0},
		{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte("pk2"), Weight: 100, Index: 1},
	}
	require.NoError(t, lm.InitiateEpoch(committee, 2, []byte("pk")))

	// First force succeeds
	require.NoError(t, lm.ForceEpochTransition())
	require.True(t, lm.IsTransitioning())

	// Second force fails
	err := lm.ForceEpochTransition()
	require.ErrorIs(t, err, ErrTransitionInProgress)
}

func TestNewLifecycleManagerDefaultConfig(t *testing.T) {
	db := memdb.New()
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	logger := log.NewLogger("test")

	// Pass nil config to use defaults
	lm := NewLifecycleManager(registry, nil, logger)
	require.NotNil(t, lm)
	require.Equal(t, DefaultLifecycleConfig().EpochDuration, lm.config.EpochDuration)
}

func TestSubmitDKGCommitmentNotParticipant(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	node1 := ids.GenerateTestNodeID()
	node2 := ids.GenerateTestNodeID()
	notParticipant := ids.GenerateTestNodeID()

	participants := []ids.NodeID{node1, node2}
	require.NoError(t, lm.StartDKG(1, participants, 2))

	// Non-participant tries to submit
	err := lm.SubmitDKGCommitment(notParticipant, []byte("commitment"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a DKG participant")
}

func TestSubmitDKGCommitmentWrongPhase(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	node1 := ids.GenerateTestNodeID()
	node2 := ids.GenerateTestNodeID()
	participants := []ids.NodeID{node1, node2}
	require.NoError(t, lm.StartDKG(1, participants, 2))

	// Complete commit phase
	require.NoError(t, lm.SubmitDKGCommitment(node1, bytes.Repeat([]byte{0xAA}, 32)))
	require.NoError(t, lm.SubmitDKGCommitment(node2, bytes.Repeat([]byte{0x55}, 32)))

	// Now in share phase - commitment should fail
	err := lm.SubmitDKGCommitment(node1, []byte("another_commitment"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "DKG not in commit phase")
}

func TestSubmitDKGShareWrongPhase(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	node1 := ids.GenerateTestNodeID()
	node2 := ids.GenerateTestNodeID()
	participants := []ids.NodeID{node1, node2}
	require.NoError(t, lm.StartDKG(1, participants, 2))

	// Try to submit share before commit phase complete
	err := lm.SubmitDKGShare(node1, []byte("share"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "DKG not in share phase")
}

func TestSubmitDKGShareNotStarted(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	// No DKG started
	err := lm.SubmitDKGShare(ids.GenerateTestNodeID(), []byte("share"))
	require.ErrorIs(t, err, ErrDKGNotStarted)
}

func TestSubmitDKGCommitmentNotStarted(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	// No DKG started
	err := lm.SubmitDKGCommitment(ids.GenerateTestNodeID(), []byte("commitment"))
	require.ErrorIs(t, err, ErrDKGNotStarted)
}

func TestAbortDKGNotStarted(t *testing.T) {
	lm, _ := newTestLifecycleManager(t)
	require.NoError(t, lm.Start())
	defer lm.Stop()

	// No DKG started
	err := lm.AbortDKG("reason")
	require.ErrorIs(t, err, ErrDKGNotStarted)
}
