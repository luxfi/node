// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	"github.com/luxfi/runtime"
	validators "github.com/luxfi/validators"

	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	platformvmmetrics "github.com/luxfi/node/vms/platformvm/metrics"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/state"
)

// newRaceTestState builds a REAL platform state (state.New, memdb + genesis) —
// the same constructor the VM uses. The genesis carries a validator set, so
// state.write (invoked by BOTH state.Commit and state.CommitBatch) iterates
// non-empty validator/metadata maps: exactly the shared maps that mutate
// concurrently in the fatal this test guards against.
func newRaceTestState(t *testing.T) state.State {
	t.Helper()
	require := require.New(t)

	reg := metric.NewRegistry()
	m, err := platformvmmetrics.New(reg)
	require.NoError(err)

	execCfg, err := config.GetConfig(nil)
	require.NoError(err)

	st, err := state.New(
		memdb.New(),
		genesistest.NewBytes(t, genesistest.Config{}),
		reg,
		validators.NewManager(),
		upgrade.GetConfig(constants.UnitTestID),
		execCfg,
		&runtime.Runtime{
			NetworkID: constants.UnitTestID,
			ChainID:   constants.PlatformChainID,
			Log:       log.Noop(),
		},
		m,
		reward.NewCalculator(reward.Config{
			MaxConsumptionRate: 120_000,
			MinConsumptionRate: 100_000,
			MintingPeriod:      365 * 24 * time.Hour,
			SupplyCap:          720 * constants.MegaLux,
		}),
	)
	require.NoError(err)
	return st
}

// TestStateCommitSerializedWithAcceptNoRace pins the single-lock rule for P-chain
// state.
//
// Event delivery drives VM.Disconnected on the node's peer-lifecycle goroutine,
// where it calls state.Commit() (→ state.write); the block acceptor calls
// state.CommitBatch() (→ state.write) on the consensus accept goroutine. Both
// mutate the shared currentValidator / staker / metadata maps, so without a
// common lock ordinary peer churn is a Go "concurrent map writes" fatal.
//
// ONE lock — the platform VM's stateLock — is held by BOTH sides:
//   - block DECISION: block/executor Block.Accept/Reject hold &vm.stateLock
//     (supplied to executor.NewManager), around the whole acceptor visit, and
//   - peer/lifecycle commits: VM.Disconnected and the Start/StopTracking uptime
//     flushes hold vm.stateLock directly.
//
// This test drives the two racing state operations — state.Commit()
// (VM.Disconnected's exact call) and state.SetUptime()+state.CommitBatch()
// (the tracker-flush + acceptor's exact calls) — from two goroutines, each
// holding the SAME lock. Under `-race` it must complete with no data race and no
// fatal. Remove the shared lock and `-race` immediately reports the concurrent
// write inside state.write().
func TestStateCommitSerializedWithAcceptNoRace(t *testing.T) {
	st := newRaceTestState(t)

	// The single serializer. In the VM this is &vm.stateLock, held by Block.Accept
	// (via the executor manager) and by VM.Disconnected/Start/StopTracking.
	var stateLock sync.Mutex

	// A genesis validator so SetUptime writes a real record (mirrors
	// tracker.Disconnect → state.SetUptime before state.Commit).
	nodeID := genesistest.DefaultNodeIDs[0]
	netID := constants.PrimaryNetworkID

	const iterations = 300
	var (
		wg            sync.WaitGroup
		acceptErr     error
		disconnectErr error
	)
	wg.Add(2)

	// Accept side: the acceptor's state.CommitBatch() + state.Abort() — the exact
	// calls block/executor acceptor.standardBlock makes, both routed through
	// state.write(). Block.Accept holds stateLock around this.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			stateLock.Lock()
			_, err := st.CommitBatch()
			st.Abort()
			stateLock.Unlock()
			if err != nil {
				acceptErr = err
				return
			}
		}
	}()

	// Disconnect side: tracker.Disconnect (state.SetUptime, error ignored exactly
	// as the tracker's updateUptimeLocked ignores ErrNotFound) followed by
	// state.Commit() — VM.Disconnected's exact sequence. Holds the SAME stateLock.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			stateLock.Lock()
			_ = st.SetUptime(nodeID, netID, time.Duration(i)*time.Second, time.Unix(int64(i), 0))
			err := st.Commit()
			stateLock.Unlock()
			if err != nil {
				disconnectErr = err
				return
			}
		}
	}()

	wg.Wait()

	require := require.New(t)
	require.NoError(acceptErr)
	require.NoError(disconnectErr)
}
