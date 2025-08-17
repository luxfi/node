// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"context"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/tests"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/subnet/primary"
)

// TODO(marun) What else does a test need? e.g. node URIs?
type APITestFunction func(tc tests.TestContext, wallet primary.Wallet, ownerAddress ids.ShortID)

// GetEnv returns the test environment
func GetEnv(tc tests.TestContext) *TestEnvironment {
	if Env == nil {
		tc.FailNow("Test environment not initialized")
	}
	return Env
}

// NewWallet creates a new wallet for testing
func NewWallet(tc tests.TestContext, keychain *secp256k1fx.Keychain, uri tmpnet.NodeURI) *primary.Wallet {
	ctx := context.Background()
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         uri.URI,
		LUXKeychain: keychain,
		EthKeychain: keychain,
	})
	if err != nil {
		tc.Log().Fatal("Failed to create wallet", err)
	}
	return &wallet
}

// CheckBootstrapIsPossible verifies that bootstrap is possible for the network
func CheckBootstrapIsPossible(tc tests.TestContext, network *tmpnet.Network) error {
	// TODO: Implement bootstrap check
	return nil
}

// ExecuteAPITest executes a test whose primary dependency is being
// able to access the API of one or more luxd nodes.
func ExecuteAPITest(apiTest APITestFunction) {
	tc := NewTestContext()
	env := GetEnv(tc)
	keychain := env.NewKeychain()
	wallet := NewWallet(tc, keychain, env.GetRandomNodeURI())
	apiTest(tc, *wallet, keychain.Keys[0].Address())
	_ = CheckBootstrapIsPossible(tc, env.GetNetwork())
}
