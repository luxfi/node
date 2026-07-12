// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

func TestImportTxRoundTrip(t *testing.T) {
	require := require.New(t)

	sourceChain := ids.ID{
		0x1f, 0x8f, 0x9f, 0x0f, 0x1e, 0x8e, 0x9e, 0x0e,
		0x2d, 0x7d, 0xad, 0xfd, 0x2c, 0x7c, 0xac, 0xfc,
		0x3b, 0x6b, 0xbb, 0xeb, 0x3a, 0x6a, 0xba, 0xea,
		0x49, 0x59, 0xc9, 0xd9, 0x48, 0x58, 0xc8, 0xd8,
	}
	importedIn := &lux.TransferableInput{
		UTXOID: lux.UTXOID{TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		}, OutputIndex: 5},
		Asset: lux.Asset{ID: assetID},
		In: &secp256k1fx.TransferInput{
			Amt:   1000,
			Input: secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}
	utx := &ImportTx{
		BaseTx: BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Memo:         []byte{0x00, 0x01, 0x02, 0x03},
		}},
		SourceChain: sourceChain,
		ImportedIns: []*lux.TransferableInput{importedIn},
	}

	tx := &Tx{Unsigned: utx}
	require.NoError(tx.Initialize())

	parsed, err := Parse(tx.Bytes())
	require.NoError(err)
	require.Equal(tx.Bytes(), parsed.Bytes())
	require.Equal(tx.ID(), parsed.ID())

	got := parsed.Unsigned.(*ImportTx)
	require.Equal(sourceChain, got.SourceChain)
	require.Len(got.ImportedIns, 1)
	require.Equal(importedIn.UTXOID, got.ImportedIns[0].UTXOID)
	require.Equal(importedIn.AssetID(), got.ImportedIns[0].AssetID())
	require.Equal(fxBytes(t, importedIn.In), fxBytes(t, got.ImportedIns[0].In))

	require.NoError(tx.SignSECP256K1Fx([][]*secp256k1.PrivateKey{{keys[0], keys[0]}}))
	signed, err := Parse(tx.Bytes())
	require.NoError(err)
	require.Equal(tx.ID(), signed.ID())
	require.Len(signed.Creds, 1)
}

func TestImportTxNotState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&ImportTx{})
	_, ok := intf.(verify.State)
	require.False(ok, "should not be marked as state")
}
