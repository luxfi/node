// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"testing"
	"time"

	"github.com/luxfi/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var errCustom = errors.New("custom")

func TestStakerLess(t *testing.T) {
	tests := []struct {
		name  string
		left  *Staker
		right *Staker
		less  bool
	}{
		{
			name: "left time < right time",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(1, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			less: true,
		},
		{
			name: "left time > right time",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(1, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			less: false,
		},
		{
			name: "left priority < right priority",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkDelegatorLegacyPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			less: true,
		},
		{
			name: "left priority > right priority",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkDelegatorLegacyPendingPriority,
			},
			less: false,
		},
		{
			name: "left txID < right txID",
			left: &Staker{
				TxID:     ids.ID([32]byte{0}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{1}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			less: true,
		},
		{
			name: "left txID > right txID",
			left: &Staker{
				TxID:     ids.ID([32]byte{1}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{0}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorPendingPriority,
			},
			less: false,
		},
		{
			name: "equal",
			left: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			right: &Staker{
				TxID:     ids.ID([32]byte{}),
				NextTime: time.Unix(0, 0),
				Priority: txs.PrimaryNetworkValidatorCurrentPriority,
			},
			less: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.less, test.left.Less(test.right))
		})
	}
}

func TestNewCurrentStaker(t *testing.T) {
	require := require.New(t)
	stakerTx := generateStakerTx(require)

	txID := ids.GenerateTestID()
	startTime := stakerTx.StartTime().Add(2 * time.Hour)
	potentialReward := uint64(12345)

	staker, err := NewCurrentStaker(txID, stakerTx, startTime, potentialReward)
	require.NoError(err)
	publicKey, isNil, err := stakerTx.PublicKey()
	require.NoError(err)
	require.True(isNil)
	require.Equal(&Staker{
		TxID:            txID,
		NodeID:          stakerTx.NodeID(),
		PublicKey:       publicKey,
		ChainID:         stakerTx.ChainID(),
		Weight:          stakerTx.Weight(),
		StartTime:       startTime,
		EndTime:         stakerTx.EndTime(),
		PotentialReward: potentialReward,
		NextTime:        stakerTx.EndTime(),
		Priority:        stakerTx.CurrentPriority(),
	}, staker)

	// A staker transaction whose PublicKey() fails verification must
	// propagate the error. The tx is now struct-is-wire (immutable zap
	// buffer), so the failing signer can no longer be injected onto the
	// concrete tx; instead a ScheduledStaker whose PublicKey() returns the
	// error is passed through the interface NewCurrentStaker accepts.
	ctrl := gomock.NewController(t)
	mockStaker := txs.NewMockScheduledStaker(ctrl)
	mockStaker.EXPECT().PublicKey().Return(nil, false, errCustom)

	_, err = NewCurrentStaker(txID, mockStaker, startTime, potentialReward)
	require.ErrorIs(err, errCustom)
}

func TestNewPendingStaker(t *testing.T) {
	require := require.New(t)

	stakerTx := generateStakerTx(require)

	txID := ids.GenerateTestID()
	staker, err := NewPendingStaker(txID, stakerTx)
	require.NoError(err)
	publicKey, isNil, err := stakerTx.PublicKey()
	require.NoError(err)
	require.True(isNil)
	require.Equal(&Staker{
		TxID:      txID,
		NodeID:    stakerTx.NodeID(),
		PublicKey: publicKey,
		ChainID:   stakerTx.ChainID(),
		Weight:    stakerTx.Weight(),
		StartTime: stakerTx.StartTime(),
		EndTime:   stakerTx.EndTime(),
		NextTime:  stakerTx.StartTime(),
		Priority:  stakerTx.PendingPriority(),
	}, staker)

	// See TestNewCurrentStaker: exercise the PublicKey() error path through
	// the ScheduledStaker interface.
	ctrl := gomock.NewController(t)
	mockStaker := txs.NewMockScheduledStaker(ctrl)
	mockStaker.EXPECT().PublicKey().Return(nil, false, errCustom)

	_, err = NewPendingStaker(txID, mockStaker)
	require.ErrorIs(err, errCustom)
}

func generateStakerTx(require *require.Assertions) *txs.AddPermissionlessValidatorTx {
	nodeID := ids.GenerateTestNodeID()
	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)
	chainID := ids.GenerateTestID()
	weight := uint64(12345)
	startTime := time.Now().Truncate(time.Second)
	endTime := startTime.Add(time.Hour)

	validator := txs.Validator{
		NodeID: nodeID,
		Start:  uint64(startTime.Unix()),
		End:    uint64(endTime.Unix()),
		Wght:   weight,
	}
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	stakeOuts := []*lux.TransferableOutput{
		{
			Asset: lux.Asset{ID: ids.GenerateTestID()},
			Out: &secp256k1fx.TransferOutput{
				Amt:          weight,
				OutputOwners: *owner,
			},
		},
	}

	tx, err := txs.NewAddPermissionlessValidatorTx(
		&lux.BaseTx{},
		validator,
		chainID,
		pop,
		stakeOuts,
		owner,
		owner,
		0,
	)
	require.NoError(err)
	return tx
}
