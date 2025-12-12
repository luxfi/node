// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package qvm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/quantumvm/quantum"
	"github.com/stretchr/testify/require"
)

// mockPChainProvider implements PChainProvider for testing (thread-safe)
type mockPChainProvider struct {
	mu         sync.RWMutex
	height     uint64
	validators []ValidatorState
	finCh      chan FinalityEvent
}

func newMockPChain(validators int) *mockPChainProvider {
	vs := make([]ValidatorState, validators)
	for i := 0; i < validators; i++ {
		vs[i] = ValidatorState{
			NodeID:      ids.GenerateTestNodeID(),
			Weight:      100,
			BLSPubKey:   make([]byte, 48),
			RingtailKey: make([]byte, 1952),
			Active:      true,
		}
	}
	return &mockPChainProvider{
		height:     100,
		validators: vs,
		finCh:      make(chan FinalityEvent, 100), // Larger buffer for stress tests
	}
}

func (m *mockPChainProvider) GetFinalizedHeight() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.height
}

func (m *mockPChainProvider) GetValidators(height uint64) ([]ValidatorState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.validators, nil
}

func (m *mockPChainProvider) SubscribeFinality() <-chan FinalityEvent {
	return m.finCh
}

func (m *mockPChainProvider) emitFinality(blockID ids.ID) {
	m.mu.Lock()
	newHeight := atomic.AddUint64(&m.height, 1)
	validators := m.validators
	m.mu.Unlock()

	m.finCh <- FinalityEvent{
		Height:     newHeight,
		BlockID:    blockID,
		Validators: validators,
		Timestamp:  time.Now(),
	}
}

func TestNewQuasar(t *testing.T) {
	require := require.New(t)

	logger := log.NewLogger("test")
	q, err := NewQuasar(logger, 3, 2, 3) // 2/3 quorum
	require.NoError(err)
	require.NotNil(q)
	threshold, quorumNum, quorumDen := q.GetConfig()
	require.Equal(3, threshold)
	require.Equal(uint64(2), quorumNum)
	require.Equal(uint64(3), quorumDen)
}

func TestQuasarConnections(t *testing.T) {
	require := require.New(t)

	logger := log.NewLogger("test")
	q, err := NewQuasar(logger, 3, 2, 3)
	require.NoError(err)

	// Initially disconnected
	ctx := context.Background()
	err = q.Start(ctx)
	require.ErrorIs(err, ErrPChainNotConnected)

	// Connect P-Chain
	pChain := newMockPChain(5)
	q.ConnectPChain(pChain)

	// Still can't start without Q-Chain
	err = q.Start(ctx)
	require.ErrorIs(err, ErrQChainNotConnected)

	// Create mock VM with real quantum signer
	vm := &VM{
		quantumSigner: quantum.NewQuantumSigner(logger, 1, 64, time.Hour, 100),
	}
	ConnectQuantumSigner(q, vm.quantumSigner)

	// Now can start
	err = q.Start(ctx)
	require.NoError(err)
	require.True(q.IsRunning())

	q.Stop()
	require.False(q.IsRunning())
}

func TestQuasarStats(t *testing.T) {
	require := require.New(t)

	logger := log.NewLogger("test")
	q, err := NewQuasar(logger, 3, 2, 3)
	require.NoError(err)

	stats := q.Stats()
	require.Equal(uint64(0), stats.PChainHeight)
	require.Equal(uint64(0), stats.QChainHeight)
	require.Equal(0, stats.FinalizedBlocks)
	require.Equal(3, stats.Threshold)
	require.Equal(uint64(2), stats.QuorumNum)
	require.Equal(uint64(3), stats.QuorumDen)
	require.False(stats.Running)
}

func TestQuorumCheck(t *testing.T) {
	require := require.New(t)

	logger := log.NewLogger("test")
	q, err := NewQuasar(logger, 3, 2, 3) // 2/3 quorum
	require.NoError(err)

	// With integer division: 100 * 2 / 3 = 66, so need >= 66
	require.True(q.CheckQuorum(67, 100))
	require.True(q.CheckQuorum(100, 100))
	require.True(q.CheckQuorum(66, 100))  // 66 >= 66 (integer division)
	require.False(q.CheckQuorum(65, 100)) // 65 < 66
	require.False(q.CheckQuorum(0, 100))
}

func TestCreateMessage(t *testing.T) {
	require := require.New(t)

	logger := log.NewLogger("test")
	q, err := NewQuasar(logger, 3, 2, 3)
	require.NoError(err)

	blockID := ids.GenerateTestID()
	event := FinalityEvent{
		Height:    12345,
		BlockID:   blockID,
		Timestamp: time.Now(),
	}

	msg := q.CreateMessage(event)
	require.Len(msg, 48)
	require.Equal(blockID[:], msg[:32])
}

func TestTotalWeight(t *testing.T) {
	require := require.New(t)

	logger := log.NewLogger("test")
	q, err := NewQuasar(logger, 3, 2, 3)
	require.NoError(err)

	validators := []ValidatorState{
		{Weight: 100, Active: true},
		{Weight: 200, Active: true},
		{Weight: 50, Active: false}, // Inactive
		{Weight: 150, Active: true},
	}

	total := q.TotalWeight(validators)
	require.Equal(uint64(450), total) // Only active validators
}

