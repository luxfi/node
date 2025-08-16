// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/state/statetest"

	. "github.com/luxfi/node/vms/platformvm/validators"
)

func TestGetValidatorSet_AfterEtna(t *testing.T) {
	require := require.New(t)

	vdrs := validators.NewManager()
	upgrades := upgradetest.GetConfig(upgradetest.Durango)
	upgradeTime := genesistest.DefaultValidatorStartTime.Add(2 * time.Second)
	upgrades.EtnaTime = upgradeTime
	s := statetest.New(t, statetest.Config{
		Validators: vdrs,
		Upgrades:   upgrades,
	})

	sk, err := localsigner.New()
	require.NoError(err)
	var (
		subnetID      = ids.GenerateTestID()
		startTime     = genesistest.DefaultValidatorStartTime
		endTime       = startTime.Add(24 * time.Hour)
		pk            = sk.PublicKey()
		primaryStaker = &state.Staker{
			TxID:            ids.GenerateTestID(),
			NodeID:          ids.GenerateTestNodeID(),
			PublicKey:       pk,
			SubnetID:        constants.PrimaryNetworkID,
			Weight:          1,
			StartTime:       startTime,
			EndTime:         endTime,
			PotentialReward: 1,
		}
		subnetStaker = &state.Staker{
			TxID:      ids.GenerateTestID(),
			NodeID:    primaryStaker.NodeID,
			PublicKey: nil, // inherited from primaryStaker
			SubnetID:  subnetID,
			Weight:    1,
			StartTime: upgradeTime,
			EndTime:   endTime,
		}
	)

	// Add a subnet staker during the Etna upgrade
	{
		blk, err := block.NewBanffStandardBlock(upgradeTime, s.GetLastAccepted(), 1, nil)
		require.NoError(err)

		s.SetHeight(blk.Height())
		s.SetTimestamp(blk.Timestamp())
		s.AddStatelessBlock(blk)
		s.SetLastAccepted(blk.ID())

		require.NoError(s.PutCurrentValidator(primaryStaker))
		require.NoError(s.PutCurrentValidator(subnetStaker))

		require.NoError(s.Commit())
	}

	// Remove a subnet staker
	{
		blk, err := block.NewBanffStandardBlock(s.GetTimestamp(), s.GetLastAccepted(), 2, nil)
		require.NoError(err)

		s.SetHeight(blk.Height())
		s.SetTimestamp(blk.Timestamp())
		s.AddStatelessBlock(blk)
		s.SetLastAccepted(blk.ID())

		s.DeleteCurrentValidator(subnetStaker)

		require.NoError(s.Commit())
	}

	m := NewManager(
		config.Internal{
			Validators: vdrs,
		},
		s,
		metric.Noop,
		new(mockable.Clock),
	)

	expectedValidators := []map[ids.NodeID]*validators.GetValidatorOutput{
		{}, // Subnet staker didn't exist at genesis
		{
			subnetStaker.NodeID: {
				NodeID:    subnetStaker.NodeID,
				PublicKey: pk,
				Weight:    subnetStaker.Weight,
			},
		}, // Subnet staker was added at height 1
		{}, // Subnet staker was removed at height 2
	}
	for height, expected := range expectedValidators {
		actual, err := m.GetValidatorSet(context.Background(), uint64(height), subnetID)
		require.NoError(err)
		require.Equal(expected, actual)
	}
}
