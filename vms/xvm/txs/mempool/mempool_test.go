// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"testing"
	
	"github.com/luxfi/metric"

	"github.com/stretchr/testify/require"


	"github.com/luxfi/ids"

	"github.com/luxfi/node/consensus/engine/core"

	"github.com/luxfi/node/utils"

	"github.com/luxfi/node/vms/xvm/txs"

	"github.com/luxfi/node/vms/components/lux"
)

func newMempool(toEngine chan<- core.Message) (Mempool, error) {
	return New("mempool", metrics.NewNoOpMetrics("test").Registry(), toEngine)
}

func TestRequestBuildBlock(t *testing.T) {
	require := require.New(t)

	toEngine := make(chan core.Message, 1)
	mempool, err := newMempool(toEngine)
	require.NoError(err)

	mempool.RequestBuildBlock()
	select {
	case <-toEngine:
		require.FailNow("should not have sent message to engine")
	default:
	}

	tx := newTx(0, 32)
	require.NoError(mempool.Add(tx))

	mempool.RequestBuildBlock()
	mempool.RequestBuildBlock() // Must not deadlock
	select {
	case <-toEngine:
	default:
		require.FailNow("should have sent message to engine")
	}
	select {
	case <-toEngine:
		require.FailNow("should have only sent one message to engine")
	default:
	}
}

func newTx(index uint32, size int) *txs.Tx {
	tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: lux.BaseTx{
		Ins: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID:        ids.ID{'t', 'x', 'I', 'D'},
				OutputIndex: index,
			},
		}},
	}}}
	tx.SetBytes(utils.RandomBytes(size), utils.RandomBytes(size))
	return tx
}
