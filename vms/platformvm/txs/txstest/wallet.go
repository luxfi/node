// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/runtime"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/node/wallet/chain/p/builder"
	"github.com/luxfi/node/wallet/chain/p/signer"
	"github.com/luxfi/node/wallet/chain/p/wallet"
	"github.com/luxfi/node/wallet/network/primary/common"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/utxo/secp256k1fx"
)

// NewWallet creates a test wallet for P-chain transactions.
//
// Parameters:
//   - ownedNetworkIDs: Network IDs (L1 validator sets) that the keychain owns (for GetNetOwner lookups)
//   - validationIDs: L1 validator IDs for deactivation owner lookups
//   - importSourceChainIDs: Chain IDs to check for importable atomic UTXOs
func NewWallet(
	t testing.TB,
	rt *runtime.Runtime,
	cfg *config.Config,
	state state.State,
	kc *secp256k1fx.Keychain,
	ownedNetworkIDs []ids.ID,
	validationIDs []ids.ID,
	importSourceChainIDs []ids.ID,
) wallet.Wallet {
	return NewWalletWithOptions(
		t,
		rt,
		WalletConfig{
			Config:      cfg,
			InternalCfg: nil, // No dynamic fees by default
		},
		state,
		kc,
		ownedNetworkIDs,
		validationIDs,
		importSourceChainIDs,
	)
}

type WalletConfig struct {
	Config      *config.Config
	InternalCfg *config.Internal // Optional: for dynamic fees
}

func NewWalletWithOptions(
	t testing.TB,
	rt *runtime.Runtime,
	wCfg WalletConfig,
	state state.State,
	kc *secp256k1fx.Keychain,
	ownedNetworkIDs []ids.ID,
	validationIDs []ids.ID,
	importSourceChainIDs []ids.ID,
) wallet.Wallet {
	var (
		require = require.New(t)
		addrs   = kc.Addresses()
		utxos   = common.NewUTXOs()
	)

	pChainUTXOs, err := lux.GetAllUTXOs(state, addrs)
	require.NoError(err)

	for _, utxo := range pChainUTXOs {
		require.NoError(utxos.AddUTXO(
			context.Background(),
			constants.PlatformChainID,
			constants.PlatformChainID,
			utxo,
		))
	}

	// Add cross-chain UTXOs from shared memory for import transactions.
	// importSourceChainIDs are chains that have exported UTXOs to us (P-Chain).
	if sm, ok := rt.SharedMemory.(interface {
		Indexed(chainID ids.ID, addrs [][]byte, startAddr, startUTXO []byte, limit int) ([][]byte, []byte, []byte, error)
	}); ok && len(importSourceChainIDs) > 0 {
		// Convert addresses to [][]byte for SharedMemory API
		addrsList := addrs.List()
		addrsBytes := make([][]byte, len(addrsList))
		for i, addr := range addrsList {
			addrsBytes[i] = addr.Bytes()
		}

		for _, sourceChainID := range importSourceChainIDs {
			// Indexed returns UTXOs that sourceChainID has put in our (P-Chain's) shared memory
			// for us to import. These were exported from sourceChainID to P-Chain.
			atomicUTXOs, _, _, err := sm.Indexed(
				sourceChainID, // The source chain we're importing from
				addrsBytes,
				nil,
				nil,
				100, // reasonable limit for test wallets
			)
			if err != nil {
				// If error getting atomic UTXOs, skip this chain but don't fail
				// Some tests may not have atomic UTXOs set up
				continue
			}

			for _, utxoBytes := range atomicUTXOs {
				var utxo lux.UTXO
				_, err := txs.Codec.Unmarshal(utxoBytes, &utxo)
				if err != nil {
					continue // Skip malformed UTXOs
				}

				require.NoError(utxos.AddUTXO(
					context.Background(),
					sourceChainID,
					constants.PlatformChainID,
					&utxo,
				))
			}
		}
	}

	// Build owners map for networks we own and validators we control
	owners := make(map[ids.ID]fx.Owner, len(ownedNetworkIDs)+len(validationIDs))
	for _, networkID := range ownedNetworkIDs {
		owner, err := state.GetNetOwner(networkID)
		require.NoError(err)
		owners[networkID] = owner
	}
	for _, validationID := range validationIDs {
		l1Validator, err := state.GetL1Validator(validationID)
		require.NoError(err)

		var owner message.PChainOwner
		_, err = txs.Codec.Unmarshal(l1Validator.DeactivationOwner, &owner)
		require.NoError(err)
		owners[validationID] = &secp256k1fx.OutputOwners{
			Threshold: owner.Threshold,
			Addrs:     owner.Addresses,
		}
	}

	backend := wallet.NewBackend(
		common.NewChainUTXOs(constants.PlatformChainID, utxos),
		owners,
	)
	builderContext := newContext(rt, rt.NetworkID, rt.UTXOAssetID, wCfg.Config, wCfg.InternalCfg, state.GetTimestamp())
	kcAdapter := &keychainAdapter{kc: kc}
	return wallet.New(
		&client{
			backend: backend,
		},
		builder.New(
			addrs,
			builderContext,
			backend,
		),
		signer.New(
			kcAdapter,
			backend,
		),
	)
}

type client struct {
	backend wallet.Backend
}

func (c *client) IssueTx(
	tx *txs.Tx,
	options ...common.Option,
) error {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	return c.backend.AcceptTx(ctx, tx)
}
