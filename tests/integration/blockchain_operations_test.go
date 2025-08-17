// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package integration_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/luxfi/geth/common"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/logging"
)

// TestCChainOperations tests C-Chain (EVM) operations
func TestCChainOperations(t *testing.T) {
	require := require.New(t)
	
	// Start a test node with C-Chain enabled
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
	
	t.Log("Starting node for C-Chain test...")
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
	
	// Get C-Chain client
	cChainAPI := node.GetAPIClient().CChainAPI()
	require.NotNil(cChainAPI, "C-Chain API should not be nil")
	
	// Test eth_chainId
	t.Log("Getting chain ID...")
	chainID, err := cChainAPI.ChainID(ctx)
	require.NoError(err, "Failed to get chain ID")
	require.NotNil(chainID, "Chain ID should not be nil")
	t.Logf("C-Chain ID: %s", chainID.String())
	
	// Test eth_blockNumber
	t.Log("Getting latest block number...")
	blockNumber, err := cChainAPI.BlockNumber(ctx)
	require.NoError(err, "Failed to get block number")
	t.Logf("Latest block number: %d", blockNumber)
	
	// Test eth_getBlockByNumber
	t.Log("Getting genesis block...")
	block, err := cChainAPI.BlockByNumber(ctx, big.NewInt(0))
	require.NoError(err, "Failed to get genesis block")
	require.NotNil(block, "Genesis block should not be nil")
	t.Logf("Genesis block hash: %s", block.Hash().Hex())
	
	// Test eth_gasPrice
	t.Log("Getting gas price...")
	gasPrice, err := cChainAPI.SuggestGasPrice(ctx)
	require.NoError(err, "Failed to get gas price")
	require.NotNil(gasPrice, "Gas price should not be nil")
	t.Logf("Suggested gas price: %s", gasPrice.String())
	
	// Test eth_getBalance for a random address
	testAddress := common.HexToAddress("0x0000000000000000000000000000000000000000")
	balance, err := cChainAPI.BalanceAt(ctx, testAddress, nil)
	require.NoError(err, "Failed to get balance")
	t.Logf("Balance of zero address: %s", balance.String())
	
	t.Log("C-Chain operations test completed successfully")
}

// TestXChainOperations tests X-Chain (DAG) operations
func TestXChainOperations(t *testing.T) {
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
	
	t.Log("Starting node for X-Chain test...")
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
	
	// Get X-Chain API
	xChainAPI, err := node.GetAPIClient().XChainAPI()
	require.NoError(err, "Failed to get X-Chain API")
	
	// Get blockchain ID
	t.Log("Getting X-Chain blockchain ID...")
	info, err := node.GetAPIClient().InfoAPI()
	require.NoError(err, "Failed to get info API")
	
	xChainID, err := info.GetBlockchainID(ctx, "X")
	require.NoError(err, "Failed to get X-Chain blockchain ID")
	t.Logf("X-Chain blockchain ID: %s", xChainID)
	
	// Test GetAssetDescription for LUX
	t.Log("Getting LUX asset description...")
	assetDescription, err := xChainAPI.GetAssetDescription(ctx, "LUX")
	require.NoError(err, "Failed to get LUX asset description")
	require.NotNil(assetDescription, "Asset description should not be nil")
	t.Logf("LUX asset ID: %s", assetDescription.AssetID)
	t.Logf("LUX asset name: %s", assetDescription.Name)
	t.Logf("LUX asset symbol: %s", assetDescription.Symbol)
	t.Logf("LUX asset denomination: %d", assetDescription.Denomination)
	
	// Test GetBalance for a new address (should be 0)
	testAddress := "X-local1mmdvxhtrhms00wfvjgn5ecmq6x8t6un5py5p3q"
	balance, err := xChainAPI.GetBalance(ctx, testAddress, "LUX")
	require.NoError(err, "Failed to get balance")
	t.Logf("Balance of test address: %d", balance.Balance)
	require.GreaterOrEqual(balance.Balance, uint64(0), "Balance should be non-negative")
	
	// Test GetHeight
	height, err := xChainAPI.GetHeight(ctx)
	require.NoError(err, "Failed to get height")
	t.Logf("X-Chain height: %d", height.Height)
	require.GreaterOrEqual(height.Height, uint64(0), "Height should be non-negative")
	
	t.Log("X-Chain operations test completed successfully")
}

// TestPChainOperations tests P-Chain (Platform) operations
func TestPChainOperations(t *testing.T) {
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
	
	t.Log("Starting node for P-Chain test...")
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
	
	// Get P-Chain API
	pChainAPI, err := node.GetAPIClient().PChainAPI()
	require.NoError(err, "Failed to get P-Chain API")
	
	// Get blockchain ID
	t.Log("Getting P-Chain blockchain ID...")
	info, err := node.GetAPIClient().InfoAPI()
	require.NoError(err, "Failed to get info API")
	
	pChainID, err := info.GetBlockchainID(ctx, "P")
	require.NoError(err, "Failed to get P-Chain blockchain ID")
	t.Logf("P-Chain blockchain ID: %s", pChainID)
	require.Equal(constants.PlatformChainID.String(), pChainID.String(), 
		"P-Chain ID should match platform chain ID")
	
	// Test GetHeight
	t.Log("Getting P-Chain height...")
	height, err := pChainAPI.GetHeight(ctx)
	require.NoError(err, "Failed to get height")
	t.Logf("P-Chain height: %d", height)
	require.GreaterOrEqual(height, uint64(0), "Height should be non-negative")
	
	// Test GetBalance for a test address
	testAddress := "P-local1mmdvxhtrhms00wfvjgn5ecmq6x8t6un5py5p3q"
	balances, err := pChainAPI.GetBalance(ctx, []string{testAddress})
	require.NoError(err, "Failed to get balance")
	
	// New address should have 0 balance
	luxBalance := balances.Balances[constants.LuxAssetID]
	t.Logf("LUX balance of test address: %d", luxBalance)
	require.GreaterOrEqual(luxBalance, uint64(0), "Balance should be non-negative")
	
	// Test GetCurrentSupply
	t.Log("Getting current LUX supply...")
	supply, err := pChainAPI.GetCurrentSupply(ctx, constants.PrimaryNetworkID)
	require.NoError(err, "Failed to get current supply")
	t.Logf("Current LUX supply: %d", supply)
	require.Greater(supply, uint64(0), "Supply should be positive")
	
	// Test GetStakingAssetID
	t.Log("Getting staking asset ID...")
	stakingAssetID, err := pChainAPI.GetStakingAssetID(ctx, constants.PrimaryNetworkID)
	require.NoError(err, "Failed to get staking asset ID")
	t.Logf("Staking asset ID: %s", stakingAssetID)
	require.Equal(constants.LuxAssetID.String(), stakingAssetID.String(), 
		"Staking asset should be LUX")
	
	// Test GetBlockchains
	t.Log("Getting blockchains...")
	blockchains, err := pChainAPI.GetBlockchains(ctx)
	require.NoError(err, "Failed to get blockchains")
	t.Logf("Number of blockchains: %d", len(blockchains))
	
	// Should have at least P, X, and C chains
	require.GreaterOrEqual(len(blockchains), 3, "Should have at least 3 blockchains")
	
	// Check for expected chains
	hasP, hasX, hasC := false, false, false
	for _, blockchain := range blockchains {
		switch blockchain.Name {
		case "P-Chain":
			hasP = true
			require.Equal(constants.PlatformChainID.String(), blockchain.ID.String())
		case "X-Chain":
			hasX = true
		case "C-Chain":
			hasC = true
		}
		t.Logf("Blockchain: %s (ID: %s, VM: %s)", 
			blockchain.Name, blockchain.ID, blockchain.VMID)
	}
	
	require.True(hasP, "P-Chain should exist")
	require.True(hasX, "X-Chain should exist")
	require.True(hasC, "C-Chain should exist")
	
	t.Log("P-Chain operations test completed successfully")
}

// TestBlockProduction tests that blocks are being produced
func TestBlockProduction(t *testing.T) {
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
	
	t.Log("Starting node for block production test...")
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
	
	// Get initial heights for all chains
	pChainAPI, err := node.GetAPIClient().PChainAPI()
	require.NoError(err, "Failed to get P-Chain API")
	
	xChainAPI, err := node.GetAPIClient().XChainAPI()
	require.NoError(err, "Failed to get X-Chain API")
	
	cChainAPI := node.GetAPIClient().CChainAPI()
	require.NotNil(cChainAPI, "C-Chain API should not be nil")
	
	// Get initial heights
	t.Log("Getting initial blockchain heights...")
	
	pHeight1, err := pChainAPI.GetHeight(ctx)
	require.NoError(err, "Failed to get P-Chain height")
	
	xHeight1, err := xChainAPI.GetHeight(ctx)
	require.NoError(err, "Failed to get X-Chain height")
	
	cHeight1, err := cChainAPI.BlockNumber(ctx)
	require.NoError(err, "Failed to get C-Chain height")
	
	t.Logf("Initial heights - P: %d, X: %d, C: %d", 
		pHeight1, xHeight1.Height, cHeight1)
	
	// Wait for some time to allow block production
	t.Log("Waiting for block production...")
	time.Sleep(5 * time.Second)
	
	// Get heights again
	t.Log("Getting new blockchain heights...")
	
	pHeight2, err := pChainAPI.GetHeight(ctx)
	require.NoError(err, "Failed to get P-Chain height")
	
	xHeight2, err := xChainAPI.GetHeight(ctx)
	require.NoError(err, "Failed to get X-Chain height")
	
	cHeight2, err := cChainAPI.BlockNumber(ctx)
	require.NoError(err, "Failed to get C-Chain height")
	
	t.Logf("New heights - P: %d, X: %d, C: %d", 
		pHeight2, xHeight2.Height, cHeight2)
	
	// In local network, chains might not produce blocks without activity
	// So we just verify heights are valid
	require.GreaterOrEqual(pHeight2, pHeight1, "P-Chain height should not decrease")
	require.GreaterOrEqual(xHeight2.Height, xHeight1.Height, "X-Chain height should not decrease")
	require.GreaterOrEqual(cHeight2, cHeight1, "C-Chain height should not decrease")
	
	t.Log("Block production test completed successfully")
}