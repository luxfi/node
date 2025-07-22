// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/node/vms/secp256k1fx"
	pwallet "github.com/luxfi/node/wallet"
	"github.com/luxfi/node/wallet/chain/p/builder"
	"github.com/luxfi/node/wallet/chain/p/signer"
	"github.com/luxfi/node/wallet/chain/p/wallet"
)

func NewWallet(
	t testing.TB,
	ctx *consensus.Context,
	config *config.Internal,
	state state.State,
	kc *secp256k1fx.Keychain,
	subnetIDs []ids.ID,
	validationIDs []ids.ID,
	chainIDs []ids.ID,
) wallet.Wallet {
	var (
		require = require.New(t)
		addrs   = kc.Addresses()
		utxos   = pwallet.NewUTXOs()
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

	for _, chainID := range chainIDs {
		remoteChainUTXOs, _, _, err := lux.GetAtomicUTXOs(
			ctx.SharedMemory,
			txs.Codec,
			chainID,
			addrs,
			ids.ShortEmpty,
			ids.Empty,
			math.MaxInt,
		)
		require.NoError(err)

		for _, utxo := range remoteChainUTXOs {
			require.NoError(utxos.AddUTXO(
				context.Background(),
				chainID,
				constants.PlatformChainID,
				utxo,
			))
		}
	}

	owners := make(map[ids.ID]fx.Owner, len(subnetIDs)+len(validationIDs))
	for _, subnetID := range subnetIDs {
		owner, err := state.GetSubnetOwner(subnetID)
		require.NoError(err)
		owners[subnetID] = owner
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
		pwallet.NewChainUTXOs(constants.PlatformChainID, utxos),
		owners,
	)
	builderContext := newContext(ctx, config, state)
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
			kc,
			backend,
		),
	)
}

type client struct {
	backend wallet.Backend
}

func (c *client) IssueTx(
	tx *txs.Tx,
	options ...pwallet.Option,
) error {
	ops := pwallet.NewOptions(options)
	if f := ops.IssuanceHandler(); f != nil {
		f(pwallet.IssuanceReceipt{
			ChainAlias: builder.Alias,
			TxID:       tx.ID(),
		})
	}
	if f := ops.ConfirmationHandler(); f != nil {
		f(pwallet.ConfirmationReceipt{
			ChainAlias: builder.Alias,
			TxID:       tx.ID(),
		})
	}
	ctx := ops.Context()
	return c.backend.AcceptTx(ctx, tx)
}
