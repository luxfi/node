// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/metric"

	"github.com/luxfi/database/leveldb"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"

	"github.com/luxfi/node/utils/constants"

	"github.com/luxfi/crypto/bls"

	"github.com/luxfi/node/utils/formatting"

	"github.com/luxfi/node/utils/formatting/address"

	"github.com/luxfi/node/utils/json"

	"github.com/luxfi/node/utils/timer/mockable"

	"github.com/luxfi/node/utils/units"

	"github.com/luxfi/node/vms/platformvm/api"

	"github.com/luxfi/node/vms/platformvm/block"

	"github.com/luxfi/node/vms/platformvm/config"

	"github.com/luxfi/node/vms/platformvm/reward"

	"github.com/luxfi/node/vms/platformvm/state"

	"github.com/luxfi/node/vms/platformvm/txs"
)

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
		0,    // blockCacheSize - use default
		0,    // writeCacheSize - use default
		1024, // handleCap - reasonable default
	)
	require.NoError(err)
	defer func() {
		require.NoError(db.Close())
	}()

	luxAssetID := ids.GenerateTestID()
	genesisTime := time.Now().Truncate(time.Second)
	genesisEndTime := genesisTime.Add(28 * 24 * time.Hour)

	addr, err := address.FormatBech32(constants.UnitTestHRP, ids.GenerateTestShortID().Bytes())
	require.NoError(err)

	genesisValidators := []api.GenesisPermissionlessValidator{{
		GenesisValidator: api.GenesisValidator{
			StartTime: json.Uint64(genesisTime.Unix()),
			EndTime:   json.Uint64(genesisEndTime.Unix()),
			NodeID:    ids.GenerateTestNodeID(),
		},
		RewardOwner: &api.Owner{
			Threshold: 1,
			Addresses: []string{addr},
		},
		Staked: []api.UTXO{{
			Amount:  json.Uint64(2 * units.KiloLux),
			Address: addr,
		}},
		DelegationFee: reward.PercentDenominator,
	}}

	buildGenesisArgs := api.BuildGenesisArgs{
		NetworkID:     json.Uint32(constants.UnitTestID),
		LuxAssetID:    luxAssetID,
		UTXOs:         nil,
		Validators:    genesisValidators,
		Chains:        nil,
		Time:          json.Uint64(genesisTime.Unix()),
		InitialSupply: json.Uint64(360 * units.MegaLux),
		Encoding:      formatting.Hex,
	}

	buildGenesisResponse := api.BuildGenesisReply{}
	platformvmSS := api.StaticService{}
	require.NoError(platformvmSS.BuildGenesis(nil, &buildGenesisArgs, &buildGenesisResponse))

	genesisBytes, err := formatting.Decode(buildGenesisResponse.Encoding, buildGenesisResponse.Bytes)
	require.NoError(err)

	vdrs := validators.NewManager()

	execConfig, err := config.GetExecutionConfig(nil)
	require.NoError(err)

	metrics, err := metric.New(prometheus.NewRegistry())
	require.NoError(err)

	s, err := state.New(
		db,
		genesisBytes,
		prometheus.NewRegistry(),
		&config.Config{
			Validators: vdrs,
		},
		execConfig,
		&context.Context{
			NetworkID: constants.UnitTestID,
			NodeID:    ids.GenerateTestNodeID(),
			Log:       log.NewNoOpLogger(),
		},
		metrics,
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: .12 * reward.PercentDenominator,
			MinConsumptionRate: .10 * reward.PercentDenominator,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * units.MegaLux,
		}),
	)
	require.NoError(err)

	m := NewManager(
		nil,
		config.Config{
			Validators: vdrs,
		},
		s,
		metrics,
		new(mockable.Clock),
	)

	var (
		nodeIDs       []ids.NodeID
		currentHeight uint64
	)
	for i := 0; i < 50; i++ {
		currentHeight++
		nodeID, err := addPrimaryValidator(s, genesisTime, genesisEndTime, currentHeight)
		require.NoError(err)
		nodeIDs = append(nodeIDs, nodeID)
	}
	subnetID := ids.GenerateTestID()
	for _, nodeID := range nodeIDs {
		currentHeight++
		require.NoError(addSubnetValidator(s, subnetID, genesisTime, genesisEndTime, nodeID, currentHeight))
	}
	for i := 0; i < 9900; i++ {
		currentHeight++
		require.NoError(addSubnetDelegator(s, subnetID, genesisTime, genesisEndTime, nodeIDs, currentHeight))
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
	sk, err := bls.NewSecretKey()
	if err != nil {
		return ids.EmptyNodeID, err
	}

	nodeID := ids.GenerateTestNodeID()
	s.PutCurrentValidator(&state.Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          nodeID,
		PublicKey:       bls.PublicFromSecretKey(sk),
		SubnetID:        constants.PrimaryNetworkID,
		Weight:          2 * units.MegaLux,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 0,
		NextTime:        endTime,
		Priority:        txs.PrimaryNetworkValidatorCurrentPriority,
	})

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
	s.PutCurrentValidator(&state.Staker{
		TxID:            ids.GenerateTestID(),
		NodeID:          nodeID,
		SubnetID:        subnetID,
		Weight:          1 * units.Lux,
		StartTime:       startTime,
		EndTime:         endTime,
		PotentialReward: 0,
		NextTime:        endTime,
		Priority:        txs.SubnetPermissionlessValidatorCurrentPriority,
	})

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
