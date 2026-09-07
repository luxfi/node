// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// packing_test.go — the two decisions the packing loop makes about a
// transaction it cannot include, and the choice of who gets paid at a stake's
// end. Both are places where the wrong answer is silent: an over-capacity
// block is rejected by every peer, a dropped-but-valid transaction is gone,
// and a reward paid to the wrong staker mints value nobody staked for.

package builder

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/iterator"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/txs/mempool"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo"
)

// priceableTx returns a transaction the fee model does price, and asserts it —
// the capacity tests below say nothing if the transaction is refused earlier
// for being unpriceable.
func priceableTx(t *testing.T) *txs.Tx {
	t.Helper()

	assetID := ids.GenerateTestID()
	utx, err := txs.NewBaseTx(&utxo.BaseTx{
		NetworkID:    10,
		BlockchainID: ids.GenerateTestID(),
		Outs: []*utxo.TransferableOutput{{
			Asset: utxo.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
			},
		}},
		Ins: []*utxo.TransferableInput{{
			UTXOID: utxo.UTXOID{TxID: ids.GenerateTestID()},
			Asset:  utxo.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt:   1000,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
	})
	require.NoError(t, err)

	tx := &txs.Tx{Unsigned: utx}
	require.NoError(t, tx.Initialize())
	return tx
}

// A transaction that does not fit must be left where it is. Dropping it would
// destroy a valid transaction the very next block had room for; including it
// would build a block over capacity that every peer rejects.
func TestOverCapacityTxIsDeferredNotDropped(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	pool := mempool.New[*txs.Tx](noMetrics{})
	tx := priceableTx(t)
	require.NoError(pool.Add(tx))

	// Capacity zero: the parent's fee state reports none and none is demanded,
	// so nothing priceable can fit.
	blockTxs, err := packEtnaBlockTxs(
		context.Background(),
		ids.GenerateTestID(),
		stalledChain(ctrl),
		pool,
		testBackend(),
		nil, // manager: unreachable, no transaction is ever executed
		time.Unix(0, 0),
		0,
		gas.Gas(0),
	)

	require.NoError(err, "a full block is a normal outcome, not a build failure")
	require.Empty(blockTxs, "a transaction larger than the whole block was packed anyway")

	peeked, stillQueued := pool.Peek()
	require.True(stillQueued, "a transaction that merely did not fit was dropped")
	require.Equal(tx.ID(), peeked.ID())
}

// An empty mempool is a block with no transactions, not an error.
func TestEmptyMempoolPacksEmptyBlock(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	blockTxs, err := packEtnaBlockTxs(
		context.Background(),
		ids.GenerateTestID(),
		stalledChain(ctrl),
		mempool.New[*txs.Tx](noMetrics{}),
		testBackend(),
		nil,
		time.Unix(0, 0),
		0,
		gas.Gas(1_000_000),
	)

	require.NoError(err)
	require.Empty(blockTxs)
}

// stakerChain answers the one iterator getNextStakerToReward reads.
func stakerChain(ctrl *gomock.Controller, stakers ...*state.Staker) state.Chain {
	c := state.NewMockChain(ctrl)
	c.EXPECT().GetCurrentStakerIterator().DoAndReturn(
		func() (iterator.Iterator[*state.Staker], error) {
			return iterator.FromSlice(stakers...), nil
		},
	).AnyTimes()
	return c
}

// A permissioned chain validator never leaves through a reward, so it must
// never be named as the staker to pay. Naming one mints a reward for a
// validator that put up no stake to earn it.
func TestPermissionedValidatorIsNeverRewarded(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	end := time.Unix(1000, 0)
	permissioned := &state.Staker{
		TxID:     ids.GenerateTestID(),
		EndTime:  end,
		Priority: txs.ChainPermissionedValidatorCurrentPriority,
	}
	permissionless := &state.Staker{
		TxID:     ids.GenerateTestID(),
		EndTime:  end,
		Priority: txs.PrimaryNetworkValidatorCurrentPriority,
	}

	// The permissioned staker leaves first and must be skipped over.
	txID, shouldReward, err := getNextStakerToReward(
		end, stakerChain(ctrl, permissioned, permissionless),
	)
	require.NoError(err)
	require.True(shouldReward)
	require.Equal(permissionless.TxID, txID,
		"the permissioned validator was named as the staker to reward")

	// A set holding nothing but permissioned validators pays nobody.
	txID, shouldReward, err = getNextStakerToReward(end, stakerChain(ctrl, permissioned))
	require.NoError(err)
	require.False(shouldReward)
	require.Equal(ids.Empty, txID)
}

// The reward is due at the stake's end, not before it. Paying early ends a
// stake the chain still counts as active.
func TestRewardIsDueOnlyAtTheStakeEnd(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	end := time.Unix(1000, 0)
	staker := &state.Staker{
		TxID:     ids.GenerateTestID(),
		EndTime:  end,
		Priority: txs.PrimaryNetworkValidatorCurrentPriority,
	}

	txID, shouldReward, err := getNextStakerToReward(end.Add(-time.Second), stakerChain(ctrl, staker))
	require.NoError(err)
	require.Equal(staker.TxID, txID)
	require.False(shouldReward, "the staker was paid before its stake ended")

	_, shouldReward, err = getNextStakerToReward(end, stakerChain(ctrl, staker))
	require.NoError(err)
	require.True(shouldReward)
}

// The reward transaction must verify on its own terms. It carries no spending
// envelope, so a builder that fails to produce a verifiable one stalls every
// stake exit on the chain.
func TestRewardValidatorTxVerifies(t *testing.T) {
	require := require.New(t)

	txID := ids.GenerateTestID()
	tx, err := newRewardValidatorTx(txID)
	require.NoError(err)
	require.NoError(tx.SyntacticVerify(nil))

	utx, ok := tx.Unsigned.(*txs.RewardValidatorTx)
	require.True(ok)
	require.Equal(txID, utx.TxID())
}
