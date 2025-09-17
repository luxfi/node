// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"context"

	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/net/primary"
)

// NewEWallet creates a new ethereum wallet for testing
func NewEWallet(keychain *secp256k1fx.Keychain, nodeURI tmpnet.NodeURI, baseWallet primary.Wallet) primary.Wallet {
	return baseWallet
}

// CreatePrimaryWallet creates a wallet from the provided keychain
func CreatePrimaryWallet(ctx context.Context, tc primary.WalletConfig) (primary.Wallet, error) {
	return primary.MakeWallet(ctx, &tc)
}