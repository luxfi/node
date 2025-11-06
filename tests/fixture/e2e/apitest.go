// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/tests"
	"github.com/luxfi/node/tests/fixture/tmpnet"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/wallet/net/primary"
	"github.com/luxfi/node/wallet/net/primary/common"

	ginkgo "github.com/onsi/ginkgo/v2"
)

// Additional test requirements: node URIs, network configuration, test timeouts
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

// NewTestContext creates a new test context for ginkgo-based tests
func NewTestContext() tests.TestContext {
	return &ginkgoTestContext{}
}

// ginkgoTestContext implements TestContext for use with ginkgo
type ginkgoTestContext struct{}

func (tc *ginkgoTestContext) Errorf(format string, args ...any) {
	require.Fail(ginkgo.GinkgoT(), fmt.Sprintf(format, args...))
}

func (tc *ginkgoTestContext) FailNow() {
	require.FailNow(ginkgo.GinkgoT(), "test failed")
}

func (tc *ginkgoTestContext) By(text string, callback ...func()) {
	ginkgo.By(text, callback...)
}

func (tc *ginkgoTestContext) DeferCleanup(cleanup func()) {
	ginkgo.DeferCleanup(cleanup)
}

func (tc *ginkgoTestContext) Log() log.Logger {
	return log.NoLog{}
}

func (tc *ginkgoTestContext) ContextWithTimeout(duration time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	ginkgo.DeferCleanup(cancel)
	return ctx
}

func (tc *ginkgoTestContext) DefaultContext() context.Context {
	return tc.ContextWithTimeout(2 * time.Minute) // DefaultTimeout
}

func (tc *ginkgoTestContext) WithDefaultContext() common.Option {
	return common.WithContext(tc.DefaultContext())
}

func (tc *ginkgoTestContext) GetDefaultContextParent() context.Context {
	return context.Background()
}

func (tc *ginkgoTestContext) Eventually(condition func() bool, waitFor time.Duration, tick time.Duration, msg string) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()
	for !condition() {
		select {
		case <-ctx.Done():
			require.Fail(ginkgo.GinkgoT(), msg)
		case <-ticker.C:
		}
	}
}

// ExecuteAPITest executes a test whose primary dependency is being
// able to access the API of one or more Lux Nodes.
func ExecuteAPITest(apiTest APITestFunction) {
	// This function needs proper test context setup
	// For now, it's a placeholder that should be called from actual test runners
}
