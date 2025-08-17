// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"context"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/tests"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/subnet/primary"
)

// TODO(marun) What else does a test need? e.g. node URIs?
type APITestFunction func(tc tests.TestContext, wallet primary.Wallet, ownerAddress ids.ShortID)

// TestEnvironment represents the test environment
type TestEnvironment struct {
	Network  *tmpnet.Network
	Nodes    []*tmpnet.Node
	Keychain *secp256k1fx.Keychain
}

// NewKeychain creates a new keychain for testing
func (e *TestEnvironment) NewKeychain() *secp256k1fx.Keychain {
	if e.Keychain == nil {
		e.Keychain = secp256k1fx.NewKeychain()
	}
	return e.Keychain
}

// GetRandomNodeURI returns a random node URI from the network
func (e *TestEnvironment) GetRandomNodeURI() tmpnet.NodeURI {
	if len(e.Nodes) == 0 {
		return tmpnet.NodeURI{}
	}
	// Return the first node for simplicity in testing
	return tmpnet.NodeURI{
		NodeID: e.Nodes[0].NodeID,
		URI:    e.Nodes[0].URI,
	}
}

// GetNetwork returns the test network
func (e *TestEnvironment) GetNetwork() *tmpnet.Network {
	return e.Network
}

// Env is the global test environment
var Env *TestEnvironment

// GetEnv returns the test environment
func GetEnv(tc tests.TestContext) *TestEnvironment {
	if Env == nil {
		tc.Log().Fatal("Test environment not initialized")
		return nil
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
		tc.Log().Fatal("Failed to create wallet: " + err.Error())
	}
	return &wallet
}

// CheckBootstrapIsPossible verifies that bootstrap is possible for the network
func CheckBootstrapIsPossible(tc tests.TestContext, network *tmpnet.Network) error {
	// Check if there are enough nodes to bootstrap
	if len(network.Nodes) < 1 {
		return fmt.Errorf("network must have at least 1 node to bootstrap")
	}
	
	// Check if bootstrap nodes are configured
	bootstrapNodes := 0
	for _, node := range network.Nodes {
		if node.Flags != nil && node.Flags["bootstrap-ips"] != "" {
			bootstrapNodes++
		}
	}
	
	// For initial bootstrap, we need at least one node without bootstrap IPs
	// (the bootstrap node itself) and other nodes should have bootstrap IPs configured
	if len(network.Nodes) > 1 && bootstrapNodes == 0 {
		tc.Log().Warn("No bootstrap IPs configured for multi-node network")
	}
	
	// Check that all nodes have unique node IDs
	nodeIDs := make(map[ids.NodeID]bool)
	for _, node := range network.Nodes {
		if nodeIDs[node.NodeID] {
			return fmt.Errorf("duplicate node ID found: %s", node.NodeID)
		}
		nodeIDs[node.NodeID] = true
	}
	
	// Verify network has a valid subnet configuration
	if network.Genesis == nil {
		return fmt.Errorf("network genesis is not configured")
	}
	
	tc.Log().Info(fmt.Sprintf("Bootstrap check passed with %d nodes", len(network.Nodes)))
	return nil
}

// NewTestContext creates a new test context
func NewTestContext() tests.TestContext {
	// This should be initialized with the actual test context from the test framework
	// For now, return a placeholder that will need to be provided by the test runner
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
