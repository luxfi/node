// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"

	"github.com/luxfi/consensus/runtime"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	wkeychain "github.com/luxfi/keychain"
	"github.com/luxfi/math/set"
	"github.com/luxfi/vm/chains/atomic"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/wallet/chain/p/builder"
	"github.com/luxfi/node/wallet/chain/p/signer"
	"github.com/luxfi/utxo/secp256k1fx"
)

func NewWalletFactory(
	ctx *runtime.Runtime,
	cfg *config.Config,
	state state.State,
) *WalletFactory {
	return &WalletFactory{
		ctx:   ctx,
		cfg:   cfg,
		state: state,
	}
}

// NewWalletFactoryWithAssets creates a wallet factory with explicit asset IDs
func NewWalletFactoryWithAssets(
	stdCtx context.Context,
	sharedMemory atomic.SharedMemory,
	cfg *config.Config,
	state state.State,
	luxAssetID ids.ID,
) *WalletFactory {
	// Put the asset ID into the context so it can be retrieved later
	networkID := runtime.GetNetworkID(stdCtx)
	ctxIDs := runtime.IDs{
		NetworkID: networkID,
		ChainID:   ids.Empty,
		NodeID:    ids.EmptyNodeID,
		PublicKey: nil,
		XAssetID:  luxAssetID,
	}
	stdCtx = runtime.WithIDs(stdCtx, ctxIDs)

	// Extract consensus context or create one
	consCtx := runtime.FromContext(stdCtx)
	if consCtx == nil {
		consCtx = &runtime.Runtime{
			NetworkID: networkID,
			XAssetID:  luxAssetID,
		}
	}

	return &WalletFactory{
		ctx:   consCtx,
		cfg:   cfg,
		state: state,
	}
}

type WalletFactory struct {
	ctx   *runtime.Runtime
	cfg   *config.Config
	state state.State
}

// keychainAdapter adapts secp256k1fx.Keychain (utils/crypto keychain) to wallet keychain
type keychainAdapter struct {
	kc *secp256k1fx.Keychain
}

func (k *keychainAdapter) Get(addr ids.ShortID) (wkeychain.Signer, bool) {
	utilsSigner, ok := k.kc.Get(addr)
	if !ok {
		return nil, false
	}
	return utilsSigner.(wkeychain.Signer), true
}

func (k *keychainAdapter) Addresses() set.Set[ids.ShortID] {
	return k.kc.Addresses()
}

func (w *WalletFactory) NewWallet(keys ...*secp256k1.PrivateKey) (builder.Builder, signer.Signer) {
	var (
		kc      = secp256k1fx.NewKeychain(keys...)
		addrSet = kc.AddressSet()
		backend = newBackend(addrSet, w.state)
		// Extract networkID and LUXAssetID from context
		networkID  = w.ctx.NetworkID
		luxAssetID = w.ctx.XAssetID
	)

	context := newContext(w.ctx, networkID, luxAssetID, w.cfg, nil, w.state.GetTimestamp())
	kcAdapter := &keychainAdapter{kc: kc}

	return builder.New(addrSet, context, backend), signer.New(kcAdapter, backend)
}
