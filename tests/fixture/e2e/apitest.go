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
	"github.com/luxfi/node/wallet/net/primary"
)

// TODO(marun) What else does a test need? e.g. node URIs?
type APITestFunction func(tc tests.TestContext, wallet primary.Wallet, ownerAddress ids.ShortID)

// APITestEnvironment represents the test environment for API tests
type APITestEnvironment struct {
	Network  *tmpnet.Network
	Nodes    []*tmpnet.Node
	Keychain *secp256k1fx.Keychain
}

// NewKeychain creates a new keychain for testing
func (e *APITestEnvironment) NewKeychain() *secp256k1fx.Keychain {
	if e.Keychain == nil {
		e.Keychain = secp256k1fx.NewKeychain()
	}
	return e.Keychain
}

// GetRandomNodeURI returns a random node URI from the network
func (e *APITestEnvironment) GetRandomNodeURI() tmpnet.NodeURI {
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
func (e *APITestEnvironment) GetNetwork() *tmpnet.Network {
	return e.Network
}

// APIEnv is the global test environment for API tests
var APIEnv *APITestEnvironment

// GetAPIEnv returns the test environment for API tests
func GetAPIEnv(tc tests.TestContext) *APITestEnvironment {
	if APIEnv == nil {
		tc.Log().Fatal("API Test environment not initialized")
		return nil
	}
	return APIEnv
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
	// This function needs proper test context setup
	// For now, it's a placeholder that should be called from actual test runners
}
