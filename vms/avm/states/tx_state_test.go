// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package states

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/luxd/database"
	"github.com/luxdefi/luxd/database/memdb"
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/utils/crypto"
	"github.com/luxdefi/luxd/utils/units"
	"github.com/luxdefi/luxd/vms/avm/fxs"
	"github.com/luxdefi/luxd/vms/avm/txs"
	"github.com/luxdefi/luxd/vms/components/lux"
	"github.com/luxdefi/luxd/vms/nftfx"
	"github.com/luxdefi/luxd/vms/propertyfx"
	"github.com/luxdefi/luxd/vms/secp256k1fx"
)

var (
	networkID uint32 = 10
	chainID          = ids.ID{5, 4, 3, 2, 1}
	assetID          = ids.ID{1, 2, 3}
	keys             = crypto.BuildTestKeys()
)

func TestTxState(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	parser, err := txs.NewParser([]fxs.Fx{
		&secp256k1fx.Fx{},
		&nftfx.Fx{},
		&propertyfx.Fx{},
	})
	require.NoError(err)

	stateIntf, err := NewTxState(db, parser, prometheus.NewRegistry())
	require.NoError(err)

	s := stateIntf.(*txState)

	_, err = s.GetTx(ids.Empty)
	require.Equal(database.ErrNotFound, err)

	tx := &txs.Tx{
		Unsigned: &txs.BaseTx{
			BaseTx: lux.BaseTx{
				NetworkID:    networkID,
				BlockchainID: chainID,
				Ins: []*lux.TransferableInput{{
					UTXOID: lux.UTXOID{
						TxID:        ids.Empty,
						OutputIndex: 0,
					},
					Asset: lux.Asset{ID: assetID},
					In: &secp256k1fx.TransferInput{
						Amt: 20 * units.KiloLux,
						Input: secp256k1fx.Input{
							SigIndices: []uint32{
								0,
							},
						},
					},
				}},
			},
		},
	}

	err = tx.SignSECP256K1Fx(parser.Codec(), [][]*crypto.PrivateKeySECP256K1R{{keys[0]}})
	require.NoError(err)

	err = s.PutTx(ids.Empty, tx)
	require.NoError(err)

	loadedTx, err := s.GetTx(ids.Empty)
	require.NoError(err)
	require.Equal(tx.ID(), loadedTx.ID())

	s.txCache.Flush()

	loadedTx, err = s.GetTx(ids.Empty)
	require.NoError(err)
	require.Equal(tx.ID(), loadedTx.ID())

	err = s.DeleteTx(ids.Empty)
	require.NoError(err)

	_, err = s.GetTx(ids.Empty)
	require.Equal(database.ErrNotFound, err)

	s.txCache.Flush()

	_, err = s.GetTx(ids.Empty)
	require.Equal(database.ErrNotFound, err)
}
