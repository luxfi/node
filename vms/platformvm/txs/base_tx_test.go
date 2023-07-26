// Copyright (C) 2019-2023, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/vms/components/lux"
)

func TestBaseTxMarshalJSON(t *testing.T) {
	require := require.New(t)

	blockchainID := ids.ID{1}
	utxoTxID := ids.ID{2}
	assetID := ids.ID{3}
	fxID := ids.ID{4}
	tx := &BaseTx{BaseTx: lux.BaseTx{
		BlockchainID: blockchainID,
		NetworkID:    4,
		Ins: []*lux.TransferableInput{
			{
				FxID:   fxID,
				UTXOID: lux.UTXOID{TxID: utxoTxID, OutputIndex: 5},
				Asset:  lux.Asset{ID: assetID},
				In:     &lux.TestTransferable{Val: 100},
			},
		},
		Outs: []*lux.TransferableOutput{
			{
				FxID:  fxID,
				Asset: lux.Asset{ID: assetID},
				Out:   &lux.TestTransferable{Val: 100},
			},
		},
		Memo: []byte{1, 2, 3},
	}}

	txBytes, err := json.Marshal(tx)
	require.NoError(err)

	asString := string(txBytes)

	require.Contains(asString, `"networkID":4`)
	require.Contains(asString, `"blockchainID":"SYXsAycDPUu4z2ZksJD5fh5nTDcH3vCFHnpcVye5XuJ2jArg"`)
	require.Contains(asString, `"inputs":[{"txID":"t64jLxDRmxo8y48WjbRALPAZuSDZ6qPVaaeDzxHA4oSojhLt","outputIndex":5,"assetID":"2KdbbWvpeAShCx5hGbtdF15FMMepq9kajsNTqVvvEbhiCRSxU","fxID":"2mB8TguRrYvbGw7G2UBqKfmL8osS7CfmzAAHSzuZK8bwpRKdY","input":{"Err":null,"Val":100}}]`)
	require.Contains(asString, `"outputs":[{"assetID":"2KdbbWvpeAShCx5hGbtdF15FMMepq9kajsNTqVvvEbhiCRSxU","fxID":"2mB8TguRrYvbGw7G2UBqKfmL8osS7CfmzAAHSzuZK8bwpRKdY","output":{"Err":null,"Val":100}}]`)
}
