// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"context"
	"sync"
	gatomic "sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/networking/handler"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/service/health"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/vm"
)

// TestNew tests creating a new manager
func TestNew(t *testing.T) {
	require := require.New(t)

	config := &ManagerConfig{
		SkipBootstrap:    true,
		EnableAutomining: true,
		Log:              log.NewNoOpLogger(),
		Metrics:          metric.NewMultiGatherer(),
		VMManager:        vms.NewManager(),
		ChainDataDir:     t.TempDir(),
	}

	m, err := New(config)
	require.NoError(err)
	require.NotNil(m)

	// Cast to implementation to check internal state
	mImpl := m.(*manager)
	require.True(mImpl.SkipBootstrap)
	require.True(mImpl.EnableAutomining)
	require.NotNil(mImpl.chains)
	require.NotNil(mImpl.chainsQueue)
}

// TestSkipBootstrapTracker tests that skip bootstrap mode uses correct tracker
func TestSkipBootstrapTracker(t *testing.T) {
	require := require.New(t)

	// Create a mock tracker for testing
	config := &ManagerConfig{
		SkipBootstrap:    true,
		EnableAutomining: true,
		Log:              log.NewNoOpLogger(),
		Metrics:          metric.NewMultiGatherer(),
		VMManager:        vms.NewManager(),
		ChainDataDir:     t.TempDir(),
		// Tracker configuration not required for basic manager testing
	}

	m, err := New(config)
	require.NoError(err)
	require.NotNil(m)

	// Verify skip bootstrap mode is enabled
	mImpl := m.(*manager)
	require.True(mImpl.SkipBootstrap)

	// Test that manager can handle bootstrap status queries
	// even when skip bootstrap is enabled
	testChainID := ids.GenerateTestID()
	isBootstrapped := m.IsBootstrapped(testChainID)

	// When skip bootstrap is enabled, chains should be considered
	// bootstrapped by default, but this specific chain doesn't exist
	// so it returns false
	require.False(isBootstrapped)
}

// TestQueueChainCreation tests queuing chain creation
func TestQueueChainCreation(t *testing.T) {
	require := require.New(t)

	// Create chains with primary network config
	chainConfigs := map[ids.ID]nets.Config{
		constants.PrimaryNetworkID: {},
	}
	chains, err := NewNets(ids.GenerateTestNodeID(), chainConfigs)
	require.NoError(err)

	config := &ManagerConfig{
		Log:          log.NewNoOpLogger(),
		Metrics:      metric.NewMultiGatherer(),
		VMManager:    vms.NewManager(),
		ChainDataDir: t.TempDir(),
		Nets:         chains,
	}

	m, err := New(config)
	require.NoError(err)

	mImpl := m.(*manager)

	// Create test chain parameters
	chainID := ids.GenerateTestID()
	netID := ids.GenerateTestID()
	chainParams := ChainParameters{
		ID:      chainID,
		ChainID: netID,
		VMID:    ids.GenerateTestID(),
	}

	// Queue the chain
	m.QueueChainCreation(chainParams)

	// Check that the chain was queued
	queuedParams, ok := mImpl.chainsQueue.PopLeft()
	require.True(ok)
	require.Equal(chainParams.ID, queuedParams.ID)
	require.Equal(chainParams.ChainID, queuedParams.ChainID)
	require.Equal(chainParams.VMID, queuedParams.VMID)
}

// TestLookup tests chain alias lookup
func TestLookup(t *testing.T) {
	require := require.New(t)

	config := &ManagerConfig{
		Log:          log.NewNoOpLogger(),
		Metrics:      metric.NewMultiGatherer(),
		VMManager:    vms.NewManager(),
		ChainDataDir: t.TempDir(),
	}

	m, err := New(config)
	require.NoError(err)

	// Create a test chain ID and alias
	chainID := ids.GenerateTestID()
	alias := "test-chain"

	// Add the alias
	require.NoError(m.Alias(chainID, alias))

	// Lookup by alias
	lookedUpID, err := m.Lookup(alias)
	require.NoError(err)
	require.Equal(chainID, lookedUpID)

	// According to the comment in manager.go, the string representation of a chain's ID
	// is also considered to be an alias of the chain. So we need to add it explicitly.
	require.NoError(m.Alias(chainID, chainID.String()))

	// Now lookup by ID string should work
	lookedUpID, err = m.Lookup(chainID.String())
	require.NoError(err)
	require.Equal(chainID, lookedUpID)
}

// TestIsBootstrapped tests checking if a chain is bootstrapped
func TestIsBootstrapped(t *testing.T) {
	require := require.New(t)

	config := &ManagerConfig{
		Log:          log.NewNoOpLogger(),
		Metrics:      metric.NewMultiGatherer(),
		VMManager:    vms.NewManager(),
		ChainDataDir: t.TempDir(),
	}

	m, err := New(config)
	require.NoError(err)

	// Test non-existent chain
	chainID := ids.GenerateTestID()
	require.False(m.IsBootstrapped(chainID))
}

// TestIsBootstrappedTracksRealConvergence is the regression guard for the
// premature-true masking bug: manager.IsBootstrapped must report true ONLY once the
// chain has ACTUALLY finished initial sync (its net marked it Bootstrapped), NOT the
// instant it is merely tracked (added to m.chains with its sync goroutine launched).
// Before the fix, a C-Chain stalled at genesis (head 0x0) reported
// info.isBootstrapped(C)=true, masking the stall from any readiness gate.
func TestIsBootstrappedTracksRealConvergence(t *testing.T) {
	require := require.New(t)

	chainConfigs := map[ids.ID]nets.Config{
		constants.PrimaryNetworkID: {},
	}
	netsTracker, err := NewNets(ids.GenerateTestNodeID(), chainConfigs)
	require.NoError(err)

	config := &ManagerConfig{
		Log:          log.NewNoOpLogger(),
		Metrics:      metric.NewMultiGatherer(),
		VMManager:    vms.NewManager(),
		ChainDataDir: t.TempDir(),
		Nets:         netsTracker,
	}
	m, err := New(config)
	require.NoError(err)
	mImpl := m.(*manager)

	// A native chain validated by the primary network — the C-Chain shape.
	chainID := ids.GenerateTestID()

	// Simulate createChain's tracking: the chain EXISTS in m.chains and is registered
	// as bootstrapping in its validation net — but has NOT converged (initial sync is
	// still driving, e.g. stalled at genesis fetching ancestry).
	mImpl.chainsLock.Lock()
	mImpl.chains[chainID] = &chainInfo{Name: "C-Chain"}
	mImpl.chainsLock.Unlock()
	sb, _ := netsTracker.GetOrCreate(constants.PrimaryNetworkID)
	require.True(sb.AddChain(chainID))

	// THE FIX: exists-but-not-converged must be FALSE (was true — the masking bug).
	require.False(m.IsBootstrapped(chainID),
		"a tracked-but-still-syncing chain must not report bootstrapped")
	for _, ci := range m.(*manager).GetChains() {
		if ci.ID == chainID {
			require.False(ci.Bootstrapped, "GetChains must not report a syncing chain bootstrapped")
		}
	}

	// Initial sync reaches the frontier → monitorBootstrap calls sb.Bootstrapped.
	sb.Bootstrapped(chainID)

	// Now — and only now — it reports bootstrapped (head advanced to frontier, VM live).
	require.True(m.IsBootstrapped(chainID),
		"a converged chain must report bootstrapped")
	found := false
	for _, ci := range m.(*manager).GetChains() {
		if ci.ID == chainID {
			found = true
			require.True(ci.Bootstrapped, "GetChains must report a converged chain bootstrapped")
		}
	}
	require.True(found)
}

// TestToEngineChannelFlow verifies the toEngine channel notification flow
// This tests the goroutine that reads from toEngine and triggers block building
func TestToEngineChannelFlow(t *testing.T) {
	require := require.New(t)

	// Create toEngine channel (same as what manager creates)
	toEngine := make(chan vm.Message, 1)
	defer close(toEngine)

	// Track block builds
	var buildCalls int
	var mu sync.Mutex

	// Simulate the goroutine that reads from toEngine
	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range toEngine {
			if msg.Type == 0 { // PendingTxs
				mu.Lock()
				buildCalls++
				mu.Unlock()
			}
		}
	}()

	// Send PendingTxs notification
	toEngine <- vm.Message{Type: 0} // PendingTxs = 0

	// Give goroutine time to process
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	count := buildCalls
	mu.Unlock()

	require.Equal(1, count, "Expected 1 build call after PendingTxs notification")

	// Send multiple notifications
	for i := 0; i < 5; i++ {
		toEngine <- vm.Message{Type: 0}
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	count = buildCalls
	mu.Unlock()

	require.Equal(6, count, "Expected 6 total build calls")
}

// TestToEngineMessageTypes verifies different message types are handled correctly
func TestToEngineMessageTypes(t *testing.T) {
	require := require.New(t)

	toEngine := make(chan vm.Message, 10)
	defer close(toEngine)

	var pendingTxsCalls int
	var otherCalls int
	var mu sync.Mutex

	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range toEngine {
			mu.Lock()
			if msg.Type == 0 { // PendingTxs
				pendingTxsCalls++
			} else {
				otherCalls++
			}
			mu.Unlock()
		}
	}()

	// Send different message types
	toEngine <- vm.Message{Type: 0} // PendingTxs - should trigger build
	toEngine <- vm.Message{Type: 1} // StateSyncDone - should NOT trigger build
	toEngine <- vm.Message{Type: 0} // PendingTxs - should trigger build
	toEngine <- vm.Message{Type: 2} // Unknown - should NOT trigger build

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	pendingCount := pendingTxsCalls
	otherCount := otherCalls
	mu.Unlock()

	require.Equal(2, pendingCount, "Expected 2 PendingTxs messages")
	require.Equal(2, otherCount, "Expected 2 other messages")
}

// reasonReader captures what a chain publishes about itself, so a test can read the
// same thing an operator reads.
type reasonReader struct {
	mu     sync.Mutex
	checks map[string]health.Checker
}

func (r *reasonReader) RegisterHealthCheck(name string, c health.Checker, _ ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.checks == nil {
		r.checks = map[string]health.Checker{}
	}
	r.checks[name] = c
	return nil
}
func (r *reasonReader) RegisterReadinessCheck(string, health.Checker, ...string) error { return nil }
func (r *reasonReader) RegisterLivenessCheck(string, health.Checker, ...string) error  { return nil }

// waitingHandler is a chain that is not bootstrapped and says why — the smallest thing
// monitorBootstrap needs to reach its report. Handler's four transport methods are never called
// on this path; only the bootstrap accessors are.
type waitingHandler struct {
	handler.Handler
	why string
}

func (waitingHandler) BootstrapComplete() bool { return false }
func (waitingHandler) BootstrapHeight() uint64 { return 7 }
func (w waitingHandler) BootstrapWait() string { return w.why }

// TestMonitorBootstrapPublishesTheWaitReason closes the last hop: the reason the driver computed
// has to survive all the way to the payload an operator reads. Everything upstream of here can be
// right and the chain still answers with a sentence that fits any outage, which is the state this
// change exists to end — so the assertion is on the published check, not on an intermediate.
func TestMonitorBootstrapPublishesTheWaitReason(t *testing.T) {
	require := require.New(t)

	published := &reasonReader{}
	m := &manager{}
	m.Log = log.NewNoOpLogger()
	m.Health = published
	m.Aliaser = ids.NewAliaser()
	m.chainCreatorShutdownCh = make(chan struct{})
	m.stallWindow = 10 * time.Millisecond // reach the report without sitting out a real window

	const why = "waiting for the beacon quorum: 3 of 6 beacons responded, need 4"
	chainID := ids.GenerateTestID()

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.monitorBootstrap(nil, waitingHandler{why: why}, nil, chainID)
	}()

	check := func() health.Checker {
		published.mu.Lock()
		defer published.mu.Unlock()
		return published.checks[chainID.String()+"-bootstrap"]
	}
	require.Eventually(func() bool { return check() != nil }, 5*time.Second, time.Millisecond,
		"a chain waiting on its quorum must publish a check an operator can read")

	details, err := check().HealthCheck(context.Background())
	require.Error(err, "a chain that has not bootstrapped must report unhealthy")
	require.Equal(why, details.(map[string]interface{})["error"],
		"the published reason must be the driver's own numbers, not a generic sentence that fits any outage")
	require.Equal(uint64(7), details.(map[string]interface{})["height"],
		"and the height it is waiting at")

	close(m.chainCreatorShutdownCh)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("monitorBootstrap must return on shutdown — a reported wait keeps watching, it does not detach")
	}
}

// TestMonitorBootstrapWaitsOutTheDefaultWindow guards the window a manager gets when nobody sets
// one. Zero has to mean the long default and not zero itself: a zero window makes every tick a
// no-progress window, so a healthy slow sync is written up as stalled within the first tick and
// the report says nothing about anything.
func TestMonitorBootstrapWaitsOutTheDefaultWindow(t *testing.T) {
	require := require.New(t)

	published := &reasonReader{}
	m := &manager{}
	m.Log = log.NewNoOpLogger()
	m.Health = published
	m.Aliaser = ids.NewAliaser()
	m.chainCreatorShutdownCh = make(chan struct{})
	// stallWindow deliberately left unset.

	chainID := ids.GenerateTestID()
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.monitorBootstrap(nil, waitingHandler{why: "waiting for the beacon quorum"}, nil, chainID)
	}()

	time.Sleep(time.Second) // ≫ the 100ms poll tick, ≪ the 5-minute default
	published.mu.Lock()
	_, reported := published.checks[chainID.String()+"-bootstrap"]
	published.mu.Unlock()
	require.False(reported,
		"an unset window must mean the long default — a chain must not be written up a tick after it starts")

	close(m.chainCreatorShutdownCh)
	<-done
}

// TestReportBootstrapNamesTheCurrentReason: a chain that has not finished initial sync has
// to say WHY. The node-level check names a chain id and nothing else, so without this the
// only difference between "waiting for a quorum that will come back" and "wedged" is a log
// line nobody is reading. The reason is re-read on every call, so it tracks the condition
// instead of freezing whichever one happened first.
func TestReportBootstrapNamesTheCurrentReason(t *testing.T) {
	require := require.New(t)

	published := &reasonReader{}
	m := &manager{}
	m.Log = log.NewNoOpLogger()
	m.Health = published
	m.Aliaser = ids.NewAliaser()

	chainID := ids.GenerateTestID()
	converged := false

	var reason gatomic.Pointer[string]
	wait := "waiting for the beacon quorum to become reachable"
	reason.Store(&wait)

	m.reportBootstrap(chainID, func() bool { return converged }, 42, &reason)

	check, ok := published.checks[chainID.String()+"-bootstrap"]
	require.True(ok, "a chain that has not bootstrapped must publish a check an operator can read")

	details, err := check.HealthCheck(context.Background())
	require.Error(err, "an unbootstrapped chain must report unhealthy")
	require.Equal(wait, details.(map[string]interface{})["error"],
		"the check must name WHY the chain is not bootstrapped, not just that it is not")

	// The condition changes. The SAME check must state what is true now.
	stalled := "bootstrap stalled (no progress)"
	reason.Store(&stalled)
	details, err = check.HealthCheck(context.Background())
	require.Error(err)
	require.Equal(stalled, details.(map[string]interface{})["error"],
		"the check must re-read the reason rather than freeze the first one")

	// And it clears itself the moment the chain converges.
	converged = true
	_, err = check.HealthCheck(context.Background())
	require.NoError(err, "a reason that has since been falsified must stop being reported as a failure")
}
