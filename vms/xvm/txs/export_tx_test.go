// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

func TestExportTxRoundTrip(t *testing.T) {
	require := require.New(t)

	destChain := ids.ID{
		0x1f, 0x8f, 0x9f, 0x0f, 0x1e, 0x8e, 0x9e, 0x0e,
		0x2d, 0x7d, 0xad, 0xfd, 0x2c, 0x7c, 0xac, 0xfc,
		0x3b, 0x6b, 0xbb, 0xeb, 0x3a, 0x6a, 0xba, 0xea,
		0x49, 0x59, 0xc9, 0xd9, 0x48, 0x58, 0xc8, 0xd8,
	}
	utx := &ExportTx{
		BaseTx:           *sampleBaseTx(),
		DestinationChain: destChain,
		ExportedOuts: []*lux.TransferableOutput{{
			Asset: lux.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
	}

	tx := &Tx{Unsigned: utx}
	require.NoError(tx.Initialize())

	parsed, err := Parse(tx.Bytes())
	require.NoError(err)
	require.Equal(tx.Bytes(), parsed.Bytes())
	require.Equal(tx.ID(), parsed.ID())

	got := parsed.Unsigned.(*ExportTx)
	require.Equal(destChain, got.DestinationChain)
	require.Len(got.ExportedOuts, 1)
	require.Equal(utx.ExportedOuts[0].AssetID(), got.ExportedOuts[0].AssetID())
	require.Equal(fxBytes(t, utx.ExportedOuts[0].Out), fxBytes(t, got.ExportedOuts[0].Out))

	require.NoError(tx.SignSECP256K1Fx([][]*secp256k1.PrivateKey{{keys[0], keys[0]}}))
	signed, err := Parse(tx.Bytes())
	require.NoError(err)
	require.Equal(tx.ID(), signed.ID())
	require.Len(signed.Creds, 1)
}

func TestExportTxNotState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&ExportTx{})
	_, ok := intf.(verify.State)
	require.False(ok, "should not be marked as state")
}
