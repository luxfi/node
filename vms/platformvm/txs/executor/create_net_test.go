// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	consensuscontext "github.com/luxfi/consensus/context"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs/txstest"
	"github.com/luxfi/node/vms/platformvm/utxo"
	"github.com/luxfi/node/vms/secp256k1fx"

	walletsigner "github.com/luxfi/node/wallet/chain/p/signer"
)

func TestCreateNetTxAP3FeeChange(t *testing.T) {
	defaultGenesisTime := time.Unix(1649891275, 0) // Use a default genesis time
	ap3Time := defaultGenesisTime.Add(time.Hour)
	tests := []struct {
		name        string
		time        time.Time
		fee         uint64
		expectedErr error
	}{
		{
			name:        "pre-fork - correctly priced",
			time:        defaultGenesisTime,
			fee:         0,
			expectedErr: nil,
		},
		{
			name:        "post-fork - incorrectly priced",
			time:        ap3Time,
			fee:         100*defaultTxFee - 1*units.NanoLux,
			expectedErr: utxo.ErrInsufficientUnlockedFunds,
		},
		{
			name:        "post-fork - correctly priced",
			time:        ap3Time,
			fee:         100 * defaultTxFee,
			expectedErr: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			env := newEnvironment(t, upgradetest.Latest)
			env.config.UpgradeConfig.ApricotPhase3Time = ap3Time
			env.ctx.Lock.Lock()
			defer env.ctx.Lock.Unlock()

			env.state.SetTimestamp(test.time) // to duly set fee

			cfg := *env.config
			cfg.StaticFeeConfig.CreateNetTxFee = test.fee
			// Convert context for wallet factory
			consensusCtx := &consensuscontext.Context{
				NetworkID:   env.ctx.NetworkID,
				NetID:       env.ctx.NetID,
				ChainID:     env.ctx.ChainID,
				NodeID:      env.ctx.NodeID,
				PublicKey:   env.ctx.PublicKey,
				XChainID:    env.ctx.XChainID,
				CChainID:    env.ctx.CChainID,
				DChainID:    env.ctx.DChainID,
				LUXAssetID:  env.ctx.LUXAssetID,
				ValidatorState: env.ctx.ValidatorState,
				SharedMemory: env.ctx.SharedMemory,
				ChainDataDir: env.ctx.ChainDataDir,
				Log:         env.ctx.Log,
				Lock:        env.ctx.Lock,
				Keystore:    env.ctx.Keystore,
				Signer:      env.ctx.Signer,
				WarpSigner:  env.ctx.WarpSigner,
			}
			factory := txstest.NewWalletFactory(consensusCtx, &cfg, env.state)
			builder, signer := factory.NewWallet()
			utx, err := builder.NewCreateNetTx(
				&secp256k1fx.OutputOwners{},
			)
			require.NoError(err)
			tx, err := walletsigner.SignUnsigned(context.Background(), signer, utx)
			require.NoError(err)

			stateDiff, err := state.NewDiff(lastAcceptedID, env)
			require.NoError(err)

			stateDiff.SetTimestamp(test.time)

			feeCalculator := state.PickFeeCalculator(env.config, stateDiff)
			executor := StandardTxExecutor{
				Backend:       &env.backend,
				FeeCalculator: feeCalculator,
				State:         stateDiff,
				Tx:            tx,
			}
			err = tx.Unsigned.Visit(&executor)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}
