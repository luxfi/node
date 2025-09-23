// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"

	"github.com/luxfi/consensus"
	consContext "github.com/luxfi/consensus/context"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/chain/p/builder"
	"github.com/luxfi/node/wallet/chain/p/signer"
)

func NewWalletFactory(
	ctx context.Context,
	sharedMemory atomic.SharedMemory,
	cfg *config.Config,
	state state.State,
) *WalletFactory {
	return &WalletFactory{
		ctx:          ctx,
		sharedMemory: sharedMemory,
		cfg:          cfg,
		state:        state,
	}
}

// NewWalletFactoryWithAssets creates a wallet factory with explicit asset IDs
func NewWalletFactoryWithAssets(
	ctx context.Context,
	sharedMemory atomic.SharedMemory,
	cfg *config.Config,
	state state.State,
	luxAssetID ids.ID,
) *WalletFactory {
	// Put the LUX asset ID into the context so it can be retrieved later
	networkID := consensus.GetNetworkID(ctx)
	ctxIDs := consContext.IDs{
		NetworkID:  networkID,
		QuantumID:  networkID,
		NetID:      ids.Empty,
		ChainID:    ids.Empty,
		NodeID:     ids.EmptyNodeID,
		PublicKey:  nil,
		LUXAssetID: luxAssetID,
	}
	ctx = consContext.WithIDs(ctx, ctxIDs)
	return &WalletFactory{
		ctx:          ctx,
		sharedMemory: sharedMemory,
		cfg:          cfg,
		state:        state,
	}
}

type WalletFactory struct {
	ctx          context.Context
	sharedMemory atomic.SharedMemory
	cfg          *config.Config
	state        state.State
}

func (w *WalletFactory) NewWallet(keys ...*secp256k1.PrivateKey) (builder.Builder, signer.Signer) {
	var (
		kc      = secp256k1fx.NewKeychain(keys...)
		addrSet = kc.AddressSet()
		backend = newBackend(addrSet, w.state, w.sharedMemory)
		// Extract networkID and LUXAssetID from context
		networkID  = consensus.GetNetworkID(w.ctx)
		// Get LUX asset ID from context - this should match the asset ID used in genesis
		luxAssetID = consContext.GetLUXAssetID(w.ctx)
		context    = newContext(w.ctx, networkID, luxAssetID, w.cfg, w.state.GetTimestamp())
	)

	// Debug: log the asset ID being used
	//fmt.Printf("WalletFactory: Using LUX AssetID: %s\n", luxAssetID)

	return builder.New(addrSet, context, backend), signer.New(kc, backend)
}
