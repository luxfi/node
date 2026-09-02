// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"

	"github.com/luxfi/node/vms/platformvm/txs"
)

// legacyChainStakerState builds the shape a pre-LP-77 chain validator actually
// has in state: a primary-network staker that owns the BLS key, and a chain
// staker for the SAME node that owns none — AddChainValidatorTx carries no
// signer, so the chain staker's key is inherited, never stored.
func legacyChainStakerState(t *testing.T) (*state, ids.ID, ids.NodeID, *bls.PublicKey) {
	t.Helper()
	require := require.New(t)

	sk, err := localsigner.New()
	require.NoError(err)
	pk := sk.PublicKey()

	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()
	now := time.Unix(1000, 0)

	s := &state{
		currentStakers:  newBaseStakers(),
		chainIDNodeIDDB: memdb.New(),
	}

	s.currentStakers.PutValidator(&Staker{
		TxID:      ids.GenerateTestID(),
		NodeID:    nodeID,
		PublicKey: pk, // the primary-network validator owns the key
		ChainID:   constants.PrimaryNetworkID,
		Weight:    1000,
		StartTime: now,
		EndTime:   now.Add(365 * 24 * time.Hour),
		NextTime:  now.Add(365 * 24 * time.Hour),
		Priority:  txs.PrimaryNetworkValidatorCurrentPriority,
	})
	s.currentStakers.PutValidator(&Staker{
		TxID:      ids.GenerateTestID(),
		NodeID:    nodeID,
		PublicKey: nil, // AddChainValidatorTx has no signer
		ChainID:   chainID,
		Weight:    1000,
		StartTime: now,
		EndTime:   now.Add(30 * 24 * time.Hour),
		NextTime:  now.Add(30 * 24 * time.Hour),
		Priority:  txs.ChainPermissionedValidatorCurrentPriority,
	})

	return s, chainID, nodeID, pk
}

// TestLegacyChainValidatorSurfacedWithInheritedKey is the BUG-2 gate.
//
// The validator set handed to quorum and warp must name the key the validator
// actually signs with. A legacy chain validator signs with its inherited
// primary-network key — the same key initValidatorSets registers and the same
// one the height diffs record — so surfacing it keyless leaves its weight in
// the quorum denominator with no way to ever vote toward it.
func TestLegacyChainValidatorSurfacedWithInheritedKey(t *testing.T) {
	require := require.New(t)

	s, chainID, nodeID, pk := legacyChainStakerState(t)

	stakers, _, _, err := s.GetCurrentValidators(context.Background(), chainID)
	require.NoError(err)
	require.Len(stakers, 1)

	got := stakers[0]
	require.Equal(nodeID, got.NodeID)
	require.NotNil(got.PublicKey, "legacy chain validator surfaced KEYLESS to quorum/warp")
	require.Equal(bls.PublicKeyToCompressedBytes(pk), bls.PublicKeyToCompressedBytes(got.PublicKey),
		"surfaced a different key than the validator inherits")
}

// TestSurfacingInheritedKeyDoesNotMutateState guards the shared staker. The
// stakers held in currentStakers are live state, also reached by the write
// paths; surfacing an inherited key must hand back a copy, never write the key
// back into the chain staker.
func TestSurfacingInheritedKeyDoesNotMutateState(t *testing.T) {
	require := require.New(t)

	s, chainID, nodeID, _ := legacyChainStakerState(t)

	_, _, _, err := s.GetCurrentValidators(context.Background(), chainID)
	require.NoError(err)

	stored, err := s.GetCurrentValidator(chainID, nodeID)
	require.NoError(err)
	require.Nil(stored.PublicKey, "surfacing the inherited key mutated the stored chain staker")
}

// TestPrimaryNetworkValidatorKeepsItsOwnKey pins the other half: a primary
// network validator owns its key outright and must be surfaced unchanged.
func TestPrimaryNetworkValidatorKeepsItsOwnKey(t *testing.T) {
	require := require.New(t)

	s, _, nodeID, pk := legacyChainStakerState(t)

	stakers, _, _, err := s.GetCurrentValidators(context.Background(), constants.PrimaryNetworkID)
	require.NoError(err)
	require.Len(stakers, 1)
	require.Equal(nodeID, stakers[0].NodeID)
	require.NotNil(stakers[0].PublicKey)
	require.Equal(bls.PublicKeyToCompressedBytes(pk), bls.PublicKeyToCompressedBytes(stakers[0].PublicKey))
}

// TestSurfacedKeyMatchesRecordedDiffKey is the determinism invariant between the
// two planes.
//
// The fast path surfaces a legacy chain validator's key through
// GetCurrentValidators; the replay path reconstructs it from the height diffs
// that calculateValidatorDiffs records. Both must resolve to the same key. They
// do because both now read the one inheritance source, getInheritedPublicKey —
// this test fails the moment either plane grows its own.
func TestSurfacedKeyMatchesRecordedDiffKey(t *testing.T) {
	require := require.New(t)

	s, chainID, nodeID, _ := legacyChainStakerState(t)

	stakers, _, _, err := s.GetCurrentValidators(context.Background(), chainID)
	require.NoError(err)
	require.Len(stakers, 1)
	surfaced := bls.PublicKeyToUncompressedBytes(stakers[0].PublicKey)
	require.NotEmpty(surfaced)

	// The bytes the diff plane records for this validator.
	inherited, err := s.getInheritedPublicKey(nodeID)
	require.NoError(err)
	require.NotNil(inherited)
	recorded := bls.PublicKeyToUncompressedBytes(inherited)

	require.Equal(recorded, surfaced,
		"the surfaced key and the key recorded in the height diffs disagree")
}

// TestDeletedValidatorWithDelegatorsIsNotSurfacedAsNil covers a third way this
// set could hand out a broken entry.
//
// DeleteValidator sets baseStaker.validator to nil, and pruneValidatorLocked
// deliberately KEEPS that entry while the validator still has delegators. The
// entry therefore exists with a nil validator, and every staker in this list is
// dereferenced immediately afterwards — getCurrentValidatorSet reads
// staker.PublicKey — so surfacing the nil crashes the node rather than merely
// mis-keying it.
func TestDeletedValidatorWithDelegatorsIsNotSurfacedAsNil(t *testing.T) {
	require := require.New(t)

	s, chainID, nodeID, _ := legacyChainStakerState(t)
	now := time.Unix(1000, 0)

	chainValidator, err := s.GetCurrentValidator(chainID, nodeID)
	require.NoError(err)

	// A delegator on the chain validator keeps the entry alive past the delete.
	s.currentStakers.PutDelegator(&Staker{
		TxID:      ids.GenerateTestID(),
		NodeID:    nodeID,
		ChainID:   chainID,
		Weight:    10,
		StartTime: now,
		EndTime:   now.Add(24 * time.Hour),
		NextTime:  now.Add(24 * time.Hour),
		Priority:  txs.ChainPermissionedValidatorCurrentPriority,
	})
	s.currentStakers.DeleteValidator(chainValidator)

	// The entry survived with a nil validator.
	require.NotNil(s.currentStakers.validators[chainID][nodeID])
	require.Nil(s.currentStakers.validators[chainID][nodeID].validator)

	stakers, _, _, err := s.GetCurrentValidators(context.Background(), chainID)
	require.NoError(err)
	for i, staker := range stakers {
		require.NotNil(staker, "surfaced a nil staker at %d; every consumer dereferences it", i)
	}
}
