// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/vm/chains/atomic"
	"github.com/luxfi/node/upgrade/upgradetest"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/utxo/secp256k1fx"
)

func TestAtomicTxImports(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.rt.Lock.Lock()
	defer env.rt.Lock.Unlock()

	addr := genesistest.DefaultFundedKeys[0].Address()
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}

	m := atomic.NewMemory(prefixdb.New([]byte{5}, env.baseDB))

	env.msm.SharedMemory = m.NewSharedMemory(env.rt.ChainID)
	peerSharedMemory := m.NewSharedMemory(env.rt.XChainID)
	utxo := &lux.UTXO{
		UTXOID: lux.UTXOID{
			TxID:        ids.GenerateTestID(),
			OutputIndex: 1,
		},
		Asset: lux.Asset{ID: env.rt.XAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          70 * constants.MilliLux,
			OutputOwners: *owner,
		},
	}
	utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		env.rt.ChainID: {PutRequests: []*atomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				addr.Bytes(),
			},
		}}},
	}))

	// Create wallet - defaults to all genesis funded keys
	wallet := newWallet(t, env, walletConfig{})

	tx, err := wallet.IssueImportTx(
		env.rt.XChainID,
		owner,
	)
	require.NoError(err)

	require.NoError(env.Builder.Add(tx))
	b, err := env.Builder.BuildBlock(context.Background())
	require.NoError(err)
	// Test multiple verify calls work
	require.NoError(b.Verify(context.Background()))
	require.NoError(b.Accept(context.Background()))
	_, txStatus, err := env.state.GetTx(tx.ID())
	require.NoError(err)
	// Ensure transaction is in the committed state
	require.Equal(status.Committed, txStatus)
}
