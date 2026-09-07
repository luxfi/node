// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txheap

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// newAddValidatorTx builds a native-wire AddValidatorTx staker whose
// distinguishing field is its end time, then wraps + initializes it as a
// signed tx (struct-is-wire: constructor + Initialize, no codec).
func newAddValidatorTx(t *testing.T, nodeID ids.NodeID, start, end uint64) *txs.Tx {
	t.Helper()
	utx, err := txs.NewAddValidatorTx(
		&lux.BaseTx{},
		txs.Validator{NodeID: nodeID, Start: start, End: end},
		nil, // stake outs
		&secp256k1fx.OutputOwners{},
		0, // delegation shares
	)
	require.NoError(t, err)
	tx := &txs.Tx{Unsigned: utx}
	require.NoError(t, tx.Initialize())
	return tx
}

func TestByEndTime(t *testing.T) {
	require := require.New(t)

	txHeap := NewByEndTime()

	baseTime := time.Now()
	base := uint64(baseTime.Unix())

	tx0 := newAddValidatorTx(t, ids.BuildTestNodeID([]byte{0}), base, base+1)
	utx0 := tx0.Unsigned.(*txs.AddValidatorTx)

	tx1 := newAddValidatorTx(t, ids.BuildTestNodeID([]byte{1}), base, base+2)
	utx1 := tx1.Unsigned.(*txs.AddValidatorTx)

	tx2 := newAddValidatorTx(t, ids.BuildTestNodeID([]byte{1}), base, base+3)
	utx2 := tx2.Unsigned.(*txs.AddValidatorTx)

	txHeap.Add(tx2)
	require.Equal(utx2.EndTime(), txHeap.Timestamp())

	txHeap.Add(tx1)
	require.Equal(utx1.EndTime(), txHeap.Timestamp())

	txHeap.Add(tx0)
	require.Equal(utx0.EndTime(), txHeap.Timestamp())
	require.Equal(tx0, txHeap.Peek())
}
