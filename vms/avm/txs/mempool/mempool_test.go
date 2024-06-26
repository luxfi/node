// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/secp256k1"
	"github.com/luxfi/node/vms/avm/txs"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var (
	keys    = secp256k1.TestKeys()
	chainID = ids.ID{5, 4, 3, 2, 1}
	assetID = ids.ID{1, 2, 3}
)

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilderMaxMempoolSizeHandling(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	mempoolIntf, err := New("mempool", registerer, nil)
	require.NoError(err)

	mempool := mempoolIntf.(*mempool)

	testTxs := createTestTxs(2)
	tx := testTxs[0]

	// shortcut to simulated almost filled mempool
	mempool.bytesAvailable = len(tx.Bytes()) - 1

	err = mempool.Add(tx)
	require.ErrorIs(err, errMempoolFull)

	// shortcut to simulated almost filled mempool
	mempool.bytesAvailable = len(tx.Bytes())

	require.NoError(mempool.Add(tx))
}

func TestTxsInMempool(t *testing.T) {
	require := require.New(t)

	registerer := prometheus.NewRegistry()
	toEngine := make(chan common.Message, 100)
	mempool, err := New("mempool", registerer, toEngine)
	require.NoError(err)

	testTxs := createTestTxs(2)

	mempool.RequestBuildBlock()
	select {
	case <-toEngine:
		require.FailNow("should not have sent message to engine")
	default:
	}

	for _, tx := range testTxs {
		txID := tx.ID()
		// tx not already there
		require.False(mempool.Has(txID))

		// we can insert
		require.NoError(mempool.Add(tx))

		// we can get it
		require.True(mempool.Has(txID))

		retrieved := mempool.Get(txID)
		require.NotNil(retrieved)
		require.Equal(tx, retrieved)

		// tx exists in mempool
		require.True(mempool.Has(txID))

		// once removed it cannot be there
		mempool.Remove([]*txs.Tx{tx})

		require.False(mempool.Has(txID))
		require.Nil(mempool.Get(txID))

		// we can reinsert it again to grow the mempool
		require.NoError(mempool.Add(tx))
	}

	mempool.RequestBuildBlock()
	select {
	case <-toEngine:
	default:
		require.FailNow("should have sent message to engine")
	}

	mempool.Remove(testTxs)

	mempool.RequestBuildBlock()
	select {
	case <-toEngine:
		require.FailNow("should not have sent message to engine")
	default:
	}
}

func createTestTxs(count int) []*txs.Tx {
	testTxs := make([]*txs.Tx, 0, count)
	addr := keys[0].PublicKey().Address()
	for i := uint32(0); i < uint32(count); i++ {
		tx := &txs.Tx{Unsigned: &txs.CreateAssetTx{
			BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
				NetworkID:    constants.UnitTestID,
				BlockchainID: chainID,
				Ins: []*lux.TransferableInput{{
					UTXOID: lux.UTXOID{
						TxID:        ids.ID{'t', 'x', 'I', 'D'},
						OutputIndex: i,
					},
					Asset: lux.Asset{ID: assetID},
					In: &secp256k1fx.TransferInput{
						Amt: 54321,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{i},
						},
					},
				}},
				Outs: []*lux.TransferableOutput{{
					Asset: lux.Asset{ID: assetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: 12345,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addr},
						},
					},
				}},
			}},
			Name:         "NormalName",
			Symbol:       "TICK",
			Denomination: byte(2),
			States: []*txs.InitialState{
				{
					FxIndex: 0,
					Outs: []verify.State{
						&secp256k1fx.TransferOutput{
							Amt: 12345,
							OutputOwners: secp256k1fx.OutputOwners{
								Threshold: 1,
								Addrs:     []ids.ShortID{addr},
							},
						},
					},
				},
			},
		}}
		tx.SetBytes(utils.RandomBytes(16), utils.RandomBytes(16))
		testTxs = append(testTxs, tx)
	}
	return testTxs
}
