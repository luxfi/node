// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// unpriceable_tx_test.go — one transaction nobody can price must not stop a chain.
//
// The packing loop PEEKS the mempool and only executeTx takes a transaction off
// it, so anything that fails before executeTx is left at the front of the queue.
// The complexity check is one of those, and it used to abort the whole block.
// The next build then peeks the same transaction, fails the same way, and the
// chain stops producing — permanently, and across restarts, because peers
// re-gossip what was never dropped.

package builder

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/utils/iterator"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/node/vms/txs/mempool"
	"github.com/luxfi/runtime"
)

type noMetrics struct{}

func (noMetrics) Update(int, int) {}

// unpriceableTx returns a transaction the fee complexity visitor refuses, and
// asserts that it does — the test is worthless if the premise has quietly
// stopped holding.
func unpriceableTx(t *testing.T) *txs.Tx {
	t.Helper()
	tx := &txs.Tx{Unsigned: txs.NewRewardValidatorTx(ids.GenerateTestID())}
	require.NoError(t, tx.Initialize())

	_, err := fee.TxComplexity(tx.Unsigned)
	require.ErrorIs(t, err, fee.ErrUnsupportedTx,
		"premise gone: this transaction is priceable now, so it cannot exercise the abort")
	return tx
}

// TestUnpriceableTxIsDroppedNotFatal is the failure this file exists for: the
// block must still be built, and the transaction must be gone from the mempool
// so the next build does not meet it again.
func TestUnpriceableTxIsDroppedNotFatal(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	pool := mempool.New[*txs.Tx](noMetrics{})
	tx := unpriceableTx(t)
	require.NoError(pool.Add(tx))

	peeked, ok := pool.Peek()
	require.True(ok)
	require.Equal(tx.ID(), peeked.ID(), "the unpriceable tx must be the one the loop meets first")

	blockTxs, err := packEtnaBlockTxs(
		context.Background(),
		ids.GenerateTestID(),
		stalledChain(ctrl),
		pool,
		testBackend(),
		nil, // manager: unreachable, the tx never gets as far as execution
		time.Unix(0, 0),
		0,
		gas.Gas(1_000_000),
	)

	require.NoError(err, "a transaction nobody can price stopped the chain from building at all")
	require.Empty(blockTxs, "the unpriceable transaction must not be included either")

	_, stillQueued := pool.Peek()
	require.False(stillQueued,
		"the transaction was left in the mempool, so the next build meets it again and "+
			"the chain never produces another block")
}

// stalledChain is a parent state that answers only what the packing loop reads
// before it meets the transaction: a timestamp and a fee state. Everything past
// the complexity check is unreachable in this test, and leaving it unstubbed is
// deliberate — if the loop ever starts reading more, this fails loudly rather
// than quietly testing a different path.
func stalledChain(ctrl *gomock.Controller) state.Chain {
	c := state.NewMockChain(ctrl)
	c.EXPECT().GetTimestamp().Return(time.Unix(0, 0)).AnyTimes()
	c.EXPECT().GetFeeState().Return(gas.State{}).AnyTimes()
	c.EXPECT().GetCurrentSupply(gomock.Any()).Return(uint64(0), nil).AnyTimes()
	c.EXPECT().GetL1ValidatorExcess().Return(gas.Gas(0)).AnyTimes()
	c.EXPECT().GetAccruedFees().Return(uint64(0)).AnyTimes()
	c.EXPECT().NumActiveL1Validators().Return(0).AnyTimes()
	c.EXPECT().GetPendingStakerIterator().Return(state.EmptyIterator, nil).AnyTimes()
	c.EXPECT().GetCurrentStakerIterator().Return(state.EmptyIterator, nil).AnyTimes()
	c.EXPECT().GetExpiryIterator().Return(iterator.Empty[state.ExpiryEntry]{}, nil).AnyTimes()
	c.EXPECT().GetActiveL1ValidatorsIterator().Return(iterator.Empty[state.L1Validator]{}, nil).AnyTimes()
	return c
}

// testBackend supplies the two things the loop reads before the complexity
// check: the gas weights it prices a block against, and a logger.
func testBackend() *txexecutor.Backend {
	return &txexecutor.Backend{
		Config: &config.Internal{
			DynamicFeeConfig: gas.Config{
				Weights:     gas.Dimensions{1, 1, 1, 1},
				MaxCapacity: 1_000_000,
			},
		},
		Runtime: &runtime.Runtime{Log: log.NewNoOpLogger()},
		Log:     log.NewNoOpLogger(),
	}
}
