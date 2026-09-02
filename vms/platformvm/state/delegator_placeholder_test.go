// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
	validators "github.com/luxfi/validators"

	consensustest "github.com/luxfi/consensus/test/helpers"

	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// bootableState builds the minimum state initValidatorSets reads: a real
// validator manager, an empty L1 store, and an empty current-staker set for the
// caller to populate.
func bootableState(t *testing.T) *state {
	t.Helper()
	require := require.New(t)

	m, err := metrics.New(metric.NewRegistry())
	require.NoError(err)

	return &state{
		currentStakers:     newBaseStakers(),
		chainIDNodeIDDB:    memdb.New(),
		inactiveDB:         memdb.New(),
		activeL1Validators: newActiveL1Validators(),
		validators:         validators.NewManager(),
		rt:                 consensustest.Runtime(t, ids.GenerateTestID()),
		metrics:            m,
	}
}

func staker(nodeID ids.NodeID, chainID ids.ID, weight uint64, priority txs.Priority) *Staker {
	now := time.Unix(1000, 0)
	return &Staker{
		TxID:      ids.GenerateTestID(),
		NodeID:    nodeID,
		ChainID:   chainID,
		Weight:    weight,
		StartTime: now,
		EndTime:   now.Add(365 * 24 * time.Hour),
		NextTime:  now.Add(365 * 24 * time.Hour),
		Priority:  priority,
	}
}

// TestInitValidatorSetsSkipsDelegatorPlaceholder is the MED-2 gate for the
// chain-side deref.
//
// A map entry is not proof of a validator. DeleteValidator nils the validator
// and pruneValidatorLocked keeps the entry while delegators remain, so removing
// a chain validator that still has delegators attached leaves an entry whose
// .validator is nil. initValidatorSets dereferenced it for TxID and Weight
// without a check, and it runs at startup — so this state did not degrade a
// read, it panicked the boot.
func TestInitValidatorSetsSkipsDelegatorPlaceholder(t *testing.T) {
	require := require.New(t)

	s := bootableState(t)

	sk, err := localsigner.New()
	require.NoError(err)

	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()

	primary := staker(nodeID, constants.PrimaryNetworkID, 1000, txs.PrimaryNetworkValidatorCurrentPriority)
	primary.PublicKey = sk.PublicKey()
	s.currentStakers.PutValidator(primary)

	// A chain validator with a delegator attached...
	chainValidator := staker(nodeID, chainID, 1000, txs.ChainPermissionedValidatorCurrentPriority)
	s.currentStakers.PutValidator(chainValidator)
	s.currentStakers.PutDelegator(staker(nodeID, chainID, 25, txs.ChainPermissionlessDelegatorCurrentPriority))

	// ...removed while that delegator remains. The entry survives the prune
	// holding a nil validator.
	s.currentStakers.DeleteValidator(chainValidator)

	placeholder, ok := s.currentStakers.validators[chainID][nodeID]
	require.True(ok, "the prune must keep the entry while delegators remain")
	require.Nil(placeholder.validator, "the entry must be the nil-validator placeholder this test is about")

	require.NotPanics(func() {
		require.NoError(s.initValidatorSets())
	}, "a delegator placeholder must not panic the boot")

	// The placeholder names no validator, so nothing is registered for it — and
	// the delegator weight has no staker to attach to either.
	_, registered := s.validators.GetValidator(chainID, nodeID)
	require.False(registered, "a placeholder must not be registered as a validator")
	require.Zero(s.validators.GetWeight(chainID, nodeID))

	// The real primary-network validator is still loaded normally.
	_, registered = s.validators.GetValidator(constants.PrimaryNetworkID, nodeID)
	require.True(registered, "the primary-network validator must still load")
	require.Equal(uint64(1000), s.validators.GetWeight(constants.PrimaryNetworkID, nodeID))
}

// TestInitValidatorSetsRefusesPlaceholderPrimaryValidator is the MED-2 gate for
// the primary-side deref, which the chain-side check alone does not cover.
//
// A chain validator signs with the key it inherits from its primary-network
// validator. If that primary entry is itself a delegator placeholder there is
// no key to inherit, and reading PublicKey off it panicked. That is the same
// condition as the entry being absent — which is how GetValidator already reads
// a nil validator — so it takes the same answer. Registering the staker keyless
// instead would strand its weight in the quorum denominator.
func TestInitValidatorSetsRefusesPlaceholderPrimaryValidator(t *testing.T) {
	require := require.New(t)

	s := bootableState(t)

	nodeID := ids.GenerateTestNodeID()
	chainID := ids.GenerateTestID()

	// The primary-network validator is removed with a delegator still attached.
	primary := staker(nodeID, constants.PrimaryNetworkID, 1000, txs.PrimaryNetworkValidatorCurrentPriority)
	s.currentStakers.PutValidator(primary)
	s.currentStakers.PutDelegator(staker(nodeID, constants.PrimaryNetworkID, 25, txs.PrimaryNetworkDelegatorCurrentPriority))
	s.currentStakers.DeleteValidator(primary)

	placeholder, ok := s.currentStakers.validators[constants.PrimaryNetworkID][nodeID]
	require.True(ok)
	require.Nil(placeholder.validator)

	// A chain validator for that same node outlives it.
	s.currentStakers.PutValidator(staker(nodeID, chainID, 1000, txs.ChainPermissionedValidatorCurrentPriority))

	var err error
	require.NotPanics(func() {
		err = s.initValidatorSets()
	}, "a placeholder primary validator must not panic the boot")
	require.ErrorIs(err, errMissingPrimaryNetworkValidator,
		"a chain validator with no primary validator to inherit from must be refused, not registered keyless")
}
