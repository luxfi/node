// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"

	"github.com/luxfi/consensus"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/chain/p/builder"
	"github.com/luxfi/node/wallet/chain/p/signer"
	"github.com/luxfi/node/wallet/keychain"
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

type WalletFactory struct {
	ctx          context.Context
	sharedMemory atomic.SharedMemory
	cfg          *config.Config
	state        state.State
}

func (w *WalletFactory) NewWallet(keys ...*secp256k1.PrivateKey) (builder.Builder, signer.Signer) {
	var (
		kc       = secp256k1fx.NewKeychain(keys...)
		addrSet  = kc.AddressSet()
		backend  = newBackend(addrSet, w.state, w.sharedMemory)
		// Extract networkID and LUXAssetID from context
		networkID  = consensus.GetNetworkID(w.ctx)
		luxAssetID = consensus.GetLUXAssetID(w.ctx)
		context    = newContext(w.ctx, networkID, luxAssetID, w.cfg, w.state.GetTimestamp())
	)

	kcAdapter := keychain.NewSecp256k1fxKeychain(kc)
	return builder.New(addrSet, context, backend), signer.New(kcAdapter, backend)
}
