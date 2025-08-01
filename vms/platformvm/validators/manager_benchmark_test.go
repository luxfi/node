// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/database/leveldb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/validators"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/state/statetest"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// testStateWrapper wraps state.State to implement State interface
type testStateWrapper struct {
	state.State
}

// GetSubnetID implements State
func (s *testStateWrapper) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

// GetValidatorSet implements State
func (s *testStateWrapper) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

// BenchmarkGetValidatorSet generates 10k diffs and calculates the time to
// generate the genesis validator set by applying them.
//
// This generates a single diff for each height. In practice there could be
// multiple or zero diffs at a given height.
//
// Note: BenchmarkGetValidatorSet gets the validator set of a subnet rather than
// the primary network because the primary network performs caching that would
// interfere with the benchmark.
func BenchmarkGetValidatorSet(b *testing.B) {
	require := require.New(b)

	db, err := leveldb.New(
		b.TempDir(),
		0,  // blockCacheSize
		0,  // writeCacheSize
		0,  // handleCap
	)
	require.NoError(err)
	defer func() {
		require.NoError(db.Close())
	}()

	vdrs := validators.NewManager()
	s := statetest.New(b, statetest.Config{
		DB:         db,
		Validators: vdrs,
	})

	m := NewManager(
		config.Internal{
			Validators: vdrs,
		},
		&testStateWrapper{State: s},
		metrics.Noop,
		new(mockable.Clock),
	)

	var (
		nodeIDs       []ids.NodeID
		currentHeight uint64
	)
	for i := 0; i < 50; i++ {
		currentHeight++
		nodeID, err := addPrimaryValidator(s, genesistest.DefaultValidatorStartTime, genesistest.DefaultValidatorEndTime, currentHeight)
		require.NoError(err)
		nodeIDs = append(nodeIDs, nodeID)
	}
	subnetID := ids.GenerateTestID()
	for _, nodeID := range nodeIDs {
		currentHeight++
		require.NoError(addSubnetValidator(s, subnetID, genesistest.DefaultValidatorStartTime, genesistest.DefaultValidatorEndTime, nodeID, currentHeight))
	}
	for i := 0; i < 9900; i++ {
		currentHeight++
		require.NoError(addSubnetDelegator(s, subnetID, genesistest.DefaultValidatorStartTime, genesistest.DefaultValidatorEndTime, nodeIDs, currentHeight))
	}

	ctx := context.Background()
	height, err := m.GetCurrentHeight(ctx)
	require.NoError(err)
	require.Equal(currentHeight, height)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := m.GetValidatorSet(ctx, 0, subnetID)
		require.NoError(err)
	}

	b.StopTimer()
}

func addPrimaryValidator(
	s state.State,
	startTime time.Time,
	endTime time.Time,
	height uint64,
) (ids.NodeID, error) {
	sk, err := localsigner.New()
	if err != nil {
		return ids.EmptyNodeID, err
	}

	nodeID := ids.GenerateTestNodeID()
	if err := s.PutCurrentValidator(&state.Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          nodeID,
		PublicKey:       sk.PublicKey(),
		SubnetID:        constants.PrimaryNetworkID,
		Weight:          2 * units.MegaLux,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 0,
		NextTime:        endTime,
		Priority:        txs.PrimaryNetworkValidatorCurrentPriority,
	}); err != nil {
		return ids.EmptyNodeID, err
	}

	blk, err := block.NewBanffStandardBlock(startTime, ids.GenerateTestID(), height, nil)
	if err != nil {
		return ids.EmptyNodeID, err
	}

	s.AddStatelessBlock(blk)
	s.SetHeight(height)
	return nodeID, s.Commit()
}

func addSubnetValidator(
	s state.State,
	subnetID ids.ID,
	startTime time.Time,
	endTime time.Time,
	nodeID ids.NodeID,
	height uint64,
) error {
	if err := s.PutCurrentValidator(&state.Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          nodeID,
		SubnetID:        subnetID,
		Weight:          1 * units.Lux,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 0,
		NextTime:        endTime,
		Priority:        txs.SubnetPermissionlessValidatorCurrentPriority,
	}); err != nil {
		return err
	}

	blk, err := block.NewBanffStandardBlock(startTime, ids.GenerateTestID(), height, nil)
	if err != nil {
		return err
	}

	s.AddStatelessBlock(blk)
	s.SetHeight(height)
	return s.Commit()
}

func addSubnetDelegator(
	s state.State,
	subnetID ids.ID,
	startTime time.Time,
	endTime time.Time,
	nodeIDs []ids.NodeID,
	height uint64,
) error {
	i := rand.Intn(len(nodeIDs)) //#nosec G404
	nodeID := nodeIDs[i]
	s.PutCurrentDelegator(&state.Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          nodeID,
		SubnetID:        subnetID,
		Weight:          1 * units.Lux,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 0,
		NextTime:        endTime,
		Priority:        txs.SubnetPermissionlessDelegatorCurrentPriority,
	})

	blk, err := block.NewBanffStandardBlock(startTime, ids.GenerateTestID(), height, nil)
	if err != nil {
		return err
	}

	s.AddStatelessBlock(blk)
	s.SetLastAccepted(blk.ID())
	s.SetHeight(height)
	return s.Commit()
}
