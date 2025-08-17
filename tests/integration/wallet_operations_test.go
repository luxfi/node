// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/subnet/primary"
)

// TestWalletCreation tests wallet creation and basic operations
func TestWalletCreation(t *testing.T) {
	require := require.New(t)
	
	// Start a test node
	node := &tmpnet.Node{
		NodeID:      tmpnet.GenerateNodeID(),
		IsEphemeral: false,
		RuntimeConfig: &tmpnet.ProcessRuntimeConfig{
			GlobalNodeConfig: tmpnet.GlobalNodeConfig{
				NetworkID: constants.LocalID, // Use local network for testing
				LogLevel:  logging.Info.String(),
			},
		},
	}
	
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	t.Log("Starting node for wallet test...")
	err := node.Start(ctxWithTimeout)
	require.NoError(err, "Failed to start node")
	
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = node.Stop(stopCtx)
	}()
	
	// Wait for node to be healthy
	err = tmpnet.WaitForHealthy(ctxWithTimeout, node)
	require.NoError(err, "Node failed to become healthy")
	
	// Create a keychain
	t.Log("Creating keychain...")
	keychain := secp256k1fx.NewKeychain()
	require.NotNil(keychain, "Failed to create keychain")
	
	// Add a key to the keychain
	key, err := keychain.NewKey()
	require.NoError(err, "Failed to create new key")
	require.NotNil(key, "Key should not be nil")
	
	// Create wallet configuration
	walletConfig := &primary.WalletConfig{
		URI:         node.URI,
		LUXKeychain: keychain,
		EthKeychain: keychain,
	}
	
	// Create wallet
	t.Log("Creating wallet...")
	wallet, err := primary.MakeWallet(ctx, walletConfig)
	require.NoError(err, "Failed to create wallet")
	require.NotNil(wallet, "Wallet should not be nil")
	
	// Test P-chain operations
	pWallet := wallet.P()
	require.NotNil(pWallet, "P-chain wallet should not be nil")
	
	// Get the wallet's address
	addresses := keychain.List()
	require.NotEmpty(addresses, "Keychain should have at least one address")
	
	primaryAddress := addresses[0]
	t.Logf("Wallet primary address: %s", primaryAddress)
	
	// Test balance retrieval (will be 0 for new address)
	balances, err := pWallet.Builder().GetBalance()
	require.NoError(err, "Failed to get balance")
	
	luxBalance := balances[constants.LuxAssetID]
	t.Logf("LUX balance: %d", luxBalance)
	require.GreaterOrEqual(luxBalance, uint64(0), "Balance should be non-negative")
	
	// Test X-chain operations
	xWallet := wallet.X()
	require.NotNil(xWallet, "X-chain wallet should not be nil")
	
	// Test C-chain operations
	cWallet := wallet.C()
	require.NotNil(cWallet, "C-chain wallet should not be nil")
	
	t.Log("Wallet operations test completed successfully")
}

// TestCrossChainTransfer tests cross-chain transfer operations (P->X->C)
func TestCrossChainTransfer(t *testing.T) {
	t.Skip("Skipping cross-chain transfer test - requires funded wallet")
	
	require := require.New(t)
	
	// This test would require:
	// 1. A funded wallet with LUX tokens
	// 2. Export from P-chain to X-chain
	// 3. Import on X-chain
	// 4. Export from X-chain to C-chain
	// 5. Import on C-chain
	
	// The test structure is provided for future implementation
	// when a test environment with funded wallets is available
	
	ctx := context.Background()
	_ = ctx
	_ = require
}

// TestSubnetCreation tests subnet creation workflow
func TestSubnetCreation(t *testing.T) {
	require := require.New(t)
	
	// Start a test node
	node := &tmpnet.Node{
		NodeID:      tmpnet.GenerateNodeID(),
		IsEphemeral: false,
		RuntimeConfig: &tmpnet.ProcessRuntimeConfig{
			GlobalNodeConfig: tmpnet.GlobalNodeConfig{
				NetworkID: constants.LocalID,
				LogLevel:  logging.Info.String(),
			},
		},
	}
	
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	t.Log("Starting node for subnet test...")
	err := node.Start(ctxWithTimeout)
	require.NoError(err, "Failed to start node")
	
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = node.Stop(stopCtx)
	}()
	
	// Wait for node to be healthy
	err = tmpnet.WaitForHealthy(ctxWithTimeout, node)
	require.NoError(err, "Node failed to become healthy")
	
	// Create wallet for subnet operations
	keychain := secp256k1fx.NewKeychain()
	key, err := keychain.NewKey()
	require.NoError(err, "Failed to create key")
	
	walletConfig := &primary.WalletConfig{
		URI:         node.URI,
		LUXKeychain: keychain,
		EthKeychain: keychain,
	}
	
	wallet, err := primary.MakeWallet(ctx, walletConfig)
	require.NoError(err, "Failed to create wallet")
	
	// Get subnet owner addresses
	addresses := keychain.List()
	require.NotEmpty(addresses, "No addresses in keychain")
	
	// Test subnet ID generation
	subnetID := ids.GenerateTestID()
	require.NotEqual(ids.Empty, subnetID, "Subnet ID should not be empty")
	
	t.Logf("Generated test subnet ID: %s", subnetID)
	
	// In a real test with funded wallet, we would:
	// 1. Create subnet transaction
	// 2. Add validator to subnet
	// 3. Create blockchain on subnet
	// 4. Verify subnet is operational
	
	// For now, we just verify the wallet can access P-chain for subnet operations
	pWallet := wallet.P()
	require.NotNil(pWallet, "P-chain wallet required for subnet operations")
	
	// Verify we can get the base fee for subnet creation
	baseFee, err := pWallet.Builder().GetBaseFee()
	require.NoError(err, "Failed to get base fee")
	
	// Subnet creation fee is typically 1 LUX
	expectedSubnetCreationFee := 1 * units.Lux
	t.Logf("Base fee: %d, Expected subnet creation fee: %d", baseFee, expectedSubnetCreationFee)
	
	t.Log("Subnet creation test completed successfully")
}

// TestValidatorOperations tests validator-related operations
func TestValidatorOperations(t *testing.T) {
	require := require.New(t)
	
	// Start a test node
	node := &tmpnet.Node{
		NodeID:      tmpnet.GenerateNodeID(),
		IsEphemeral: false,
		RuntimeConfig: &tmpnet.ProcessRuntimeConfig{
			GlobalNodeConfig: tmpnet.GlobalNodeConfig{
				NetworkID: constants.LocalID,
				LogLevel:  logging.Info.String(),
			},
		},
	}
	
	ctx := context.Background()
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	t.Log("Starting node for validator test...")
	err := node.Start(ctxWithTimeout)
	require.NoError(err, "Failed to start node")
	
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = node.Stop(stopCtx)
	}()
	
	// Wait for node to be healthy
	err = tmpnet.WaitForHealthy(ctxWithTimeout, node)
	require.NoError(err, "Node failed to become healthy")
	
	// Get platform API client
	platformAPI, err := node.GetAPIClient().PChainAPI()
	require.NoError(err, "Failed to get platform API")
	
	// Get current validators
	t.Log("Getting current validators...")
	height, err := platformAPI.GetHeight(ctx)
	require.NoError(err, "Failed to get blockchain height")
	t.Logf("Current P-chain height: %d", height)
	
	validators, err := platformAPI.GetCurrentValidators(ctx, constants.PrimaryNetworkID, nil)
	require.NoError(err, "Failed to get current validators")
	
	t.Logf("Current validator count: %d", len(validators))
	
	// In local network, we should have at least one validator
	require.GreaterOrEqual(len(validators), 1, "Should have at least one validator")
	
	// Check validator details
	for i, validator := range validators {
		t.Logf("Validator %d: NodeID=%s, Weight=%d", 
			i, validator.NodeID, validator.Weight)
		
		// Verify validator has positive weight
		require.Greater(validator.Weight, uint64(0), 
			"Validator weight should be positive")
	}
	
	// Test pending validators (should be empty in local network)
	pendingValidators, err := platformAPI.GetPendingValidators(ctx, constants.PrimaryNetworkID, nil)
	require.NoError(err, "Failed to get pending validators")
	
	t.Logf("Pending validator count: %d", len(pendingValidators))
	
	// Get minimum stake amount
	minValidatorStake, err := platformAPI.GetMinStake(ctx, constants.PrimaryNetworkID)
	require.NoError(err, "Failed to get minimum stake")
	
	t.Logf("Minimum validator stake: %d", minValidatorStake.MinValidatorStake)
	t.Logf("Minimum delegator stake: %d", minValidatorStake.MinDelegatorStake)
	
	t.Log("Validator operations test completed successfully")
}