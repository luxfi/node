// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/chain/p/builder"
	"github.com/luxfi/node/wallet/chain/p/signer"
	"github.com/luxfi/node/wallet/chain/p/wallet"
	"github.com/luxfi/node/wallet/subnet/primary/common"
)

func NewWallet(
	t testing.TB,
	ctx context.Context,
	sharedMemory atomic.SharedMemory,
	config *config.Config,
	state state.State,
	kc *secp256k1fx.Keychain,
	subnetIDs []ids.ID,
	validationIDs []ids.ID,
	chainIDs []ids.ID,
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

	for _, chainID := range chainIDs {
		remoteChainUTXOs, _, _, err := lux.GetAtomicUTXOs(
			sharedMemory,
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
		common.NewChainUTXOs(constants.PlatformChainID, utxos),
		owners,
	)
	// Use constants for networkID and LUXAssetID
	builderContext := newContext(constants.TestnetID, ids.Empty, config, state.GetTimestamp())
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
	options ...common.Option,
) error {
	// Options no longer have IssuanceHandler and ConfirmationHandler
	// Just accept the transaction directly
	ops := common.NewOptions(options)
	ctx := ops.Context()
	return c.backend.AcceptTx(ctx, tx)
}
