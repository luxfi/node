// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/chains/atomic"
	"github.com/luxfi/node/v2/upgrade/upgradetest"
	"github.com/luxfi/node/v2/utils/units"
	"github.com/luxfi/node/v2/vms/components/lux"
	"github.com/luxfi/node/v2/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/v2/vms/platformvm/status"
	"github.com/luxfi/node/v2/vms/platformvm/txs"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
)

func TestAtomicTxImports(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t, upgradetest.Latest)
	env.ctx.Lock.Lock()
	defer env.ctx.Lock.Unlock()

	addr := genesistest.DefaultFundedKeys[0].Address()
	owner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	}

	m := atomic.NewMemory(prefixdb.New([]byte{5}, env.baseDB))

	env.msm.SharedMemory = m.NewSharedMemory(env.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(env.ctx.XChainID)
	utxo := &lux.UTXO{
		UTXOID: lux.UTXOID{
			TxID:        ids.GenerateTestID(),
			OutputIndex: 1,
		},
		Asset: lux.Asset{ID: env.ctx.LUXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          70 * units.MicroLux,
			OutputOwners: *owner,
		},
	}
	utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		env.ctx.ChainID: {PutRequests: []*atomic.Element{{
			Key:   inputID[:],
			Value: utxoBytes,
			Traits: [][]byte{
				addr.Bytes(),
			},
		}}},
	}))

	wallet := newWallet(t, env, walletConfig{})

	tx, err := wallet.IssueImportTx(
		env.ctx.XChainID,
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
