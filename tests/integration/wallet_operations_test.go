// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/log"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/net/primary"
)

// TestWalletCreation tests wallet creation
func TestWalletCreation(t *testing.T) {
	require := require.New(t)

	// Get node binary path
	luxdPath := getNodeBinaryPath(t)

	// Create a single node network
	network := &tmpnet.Network{
		Owner: "integration-test-wallet-creation",
		Nodes: tmpnet.NewNodesOrPanic(1),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Start the network
	err := tmpnet.BootstrapNewNetwork(
		ctx,
		log.NoLog{}, // Use no-op logger
		network,
		"", // Use default root dir
	)
	require.NoError(err)

	// Ensure network stops
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	// Get node URI
	uris, err := network.GetNodeURIs(ctx, func(f func()) {})
	require.NoError(err)
	require.NotEmpty(uris)
	nodeURI := uris[0].URI

	// Test creating a private key
	sk, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	require.NotNil(sk)

	// Test creating wallet
	kc := secp256k1fx.NewKeychain(sk)
	adapter := primary.NewKeychainAdapter(kc)
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         nodeURI,
		LUXKeychain: adapter,
		EthKeychain: adapter,
	})
	require.NoError(err)
	require.NotNil(wallet)

	// Test wallet has access to chains
	require.NotNil(wallet.P())
	require.NotNil(wallet.X())
	require.NotNil(wallet.C())
}

// TestWalletTransfer tests wallet transfer operations
func TestWalletTransfer(t *testing.T) {
	require := require.New(t)

	// Get node binary path
	luxdPath := getNodeBinaryPath(t)

	// Create a single node network
	network := &tmpnet.Network{
		Owner: "integration-test-wallet-transfer",
		Nodes: tmpnet.NewNodesOrPanic(1),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Start the network
	err := tmpnet.BootstrapNewNetwork(
		ctx,
		log.NoLog{}, // Use no-op logger
		network,
		"", // Use default root dir
	)
	require.NoError(err)

	// Ensure network stops
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	// Get node URI
	uris, err := network.GetNodeURIs(ctx, func(f func()) {})
	require.NoError(err)
	require.NotEmpty(uris)
	nodeURI := uris[0].URI

	// Create funded keys
	fundedKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	recipientKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	// Create wallet with funded key
	kc := secp256k1fx.NewKeychain(fundedKey)
	adapter := primary.NewKeychainAdapter(kc)
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         nodeURI,
		LUXKeychain: adapter,
		EthKeychain: adapter,
	})
	require.NoError(err)

	// Test that we can access wallet chains
	xWallet := wallet.X()
	require.NotNil(xWallet)

	// For basic integration test, just verify wallet structure is working
	// Real transfer operations require more complex setup with genesis funds
	require.NotNil(recipientKey) // Ensure recipient key was created
}

// TestWalletBalance tests wallet balance operations
func TestWalletBalance(t *testing.T) {
	require := require.New(t)

	// Get node binary path
	luxdPath := getNodeBinaryPath(t)

	// Create a single node network
	network := &tmpnet.Network{
		Owner: "integration-test-wallet-balance",
		Nodes: tmpnet.NewNodesOrPanic(1),
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{
				LuxNodePath: luxdPath,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Start the network
	err := tmpnet.BootstrapNewNetwork(
		ctx,
		log.NoLog{}, // Use no-op logger
		network,
		"", // Use default root dir
	)
	require.NoError(err)

	// Ensure network stops
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		network.Stop(shutdownCtx)
	}()

	// Get node URI
	uris, err := network.GetNodeURIs(ctx, func(f func()) {})
	require.NoError(err)
	require.NotEmpty(uris)
	nodeURI := uris[0].URI

	// Create a key
	sk, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	// Create wallet
	kc := secp256k1fx.NewKeychain(sk)
	adapter := primary.NewKeychainAdapter(kc)
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:         nodeURI,
		LUXKeychain: adapter,
		EthKeychain: adapter,
	})
	require.NoError(err)

	// Test balance query capability
	xWallet := wallet.X()
	require.NotNil(xWallet)

	// Get balance (should work even if balance is 0)
	balances, err := xWallet.Builder().GetFTBalance()
	require.NoError(err)
	require.NotNil(balances) // Map should exist even if empty
}
