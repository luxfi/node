// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/quasar/choices"
	"github.com/luxfi/node/v2/quasar/validators"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// TestEngineStartStop tests engine lifecycle
func TestEngineStartStop(t *testing.T) {
	require := require.New(t)

	// Create engine
	engine, vm := createTestEngine(t)

	// Start engine
	ctx := context.Background()
	err := engine.Start(ctx)
	require.NoError(err)

	// Verify engine is running
	require.Equal(vm.lastAcceptedID, engine.lastAccepted)
	require.Equal(vm.lastAcceptedID, engine.preference)

	// Stop engine
	err = engine.Stop()
	require.NoError(err)
}

// TestBuildBlock tests block building
func TestBuildBlock(t *testing.T) {
	require := require.New(t)

	// Create engine with Quasar enabled
	engine, vm := createTestEngine(t)
	engine.config.QuasarEnabled = true

	// Mock Quasar
	mockQuasar := &mockQuasar{
		certReady: make(chan []byte, 1),
	}
	mockQuasar.certReady <- make([]byte, 3072) // Mock RT cert
	engine.quasar = mockQuasar

	// Start engine
	ctx := context.Background()
	err := engine.Start(ctx)
	require.NoError(err)

	// Build block
	vm.shouldBuild = true
	block, err := engine.BuildBlock(ctx)
	require.NoError(err)
	require.NotNil(block)

	// Verify block has dual certificates
	quasarBlock, ok := block.(quasar.QuasarBlock)
	require.True(ok)
	require.True(quasarBlock.HasDualCert())
}

// TestBuildBlockTimeout tests Quasar timeout
func TestBuildBlockTimeout(t *testing.T) {
	require := require.New(t)

	// Create engine with short timeout
	engine, vm := createTestEngine(t)
	engine.config.QuasarEnabled = true
	engine.config.QuasarTimeout = 10 * time.Millisecond

	// Mock Quasar that doesn't provide cert
	mockQuasar := &mockQuasar{
		certReady: make(chan []byte, 1),
	}
	engine.quasar = mockQuasar

	// Start engine
	ctx := context.Background()
	err := engine.Start(ctx)
	require.NoError(err)

	// Build block should timeout
	vm.shouldBuild = true
	_, err = engine.BuildBlock(ctx)
	require.Error(err)
	require.Contains(err.Error(), "Quasar timeout")

	// Check for slash event
	select {
	case slash := <-engine.slashChannel:
		require.Equal(engine.ctx.NodeID, slash.ProposerID)
		require.Contains(slash.Reason, "Quasar timeout")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("No slash event received")
	}
}

// TestConcurrentBlockBuilding tests concurrent block building protection
func TestConcurrentBlockBuilding(t *testing.T) {
	require := require.New(t)

	// Create engine
	engine, vm := createTestEngine(t)

	// Start engine
	ctx := context.Background()
	err := engine.Start(ctx)
	require.NoError(err)

	// Set block building flag
	engine.blockBuilding = true
	vm.shouldBuild = true

	// Try to build block
	_, err = engine.BuildBlock(ctx)
	require.Error(err)
	require.Contains(err.Error(), "block building already in progress")
}

// TestMessageHandling tests message processing
func TestMessageHandling(t *testing.T) {
	require := require.New(t)

	// Create engine
	engine, _ := createTestEngine(t)

	// Start engine
	ctx := context.Background()
	err := engine.Start(ctx)
	require.NoError(err)

	// Send various message types
	messages := []message{
		{msgType: getAncestorsMsg, nodeID: ids.GenerateTestNodeID(), requestID: 1},
		{msgType: pullQueryMsg, nodeID: ids.GenerateTestNodeID(), requestID: 2},
		{msgType: pushQueryMsg, nodeID: ids.GenerateTestNodeID(), requestID: 3},
		{msgType: chitsMsg, nodeID: ids.GenerateTestNodeID(), requestID: 4},
	}

	// Send messages
	for _, msg := range messages {
		select {
		case engine.incomingMsgs <- msg:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Failed to send message")
		}
	}

	// Give time to process
	time.Sleep(50 * time.Millisecond)

	// Stop engine
	err = engine.Stop()
	require.NoError(err)
}

// TestParametersValidation tests parameter validation
func TestParametersValidation(t *testing.T) {
	tests := []struct {
		name    string
		params  Parameters
		wantErr bool
	}{
		{
			name: "valid parameters",
			params: Parameters{
				K:               21,
				AlphaPreference: 15,
				AlphaConfidence: 18,
				Beta:            8,
			},
			wantErr: false,
		},
		{
			name: "invalid K",
			params: Parameters{
				K:               0,
				AlphaPreference: 15,
				AlphaConfidence: 18,
				Beta:            8,
			},
			wantErr: true,
		},
		{
			name: "invalid AlphaPreference",
			params: Parameters{
				K:               21,
				AlphaPreference: 22,
				AlphaConfidence: 18,
				Beta:            8,
			},
			wantErr: true,
		},
		{
			name: "invalid AlphaConfidence",
			params: Parameters{
				K:               21,
				AlphaPreference: 15,
				AlphaConfidence: 0,
				Beta:            8,
			},
			wantErr: true,
		},
		{
			name: "invalid Beta",
			params: Parameters{
				K:               21,
				AlphaPreference: 15,
				AlphaConfidence: 18,
				Beta:            0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Valid()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Helper functions

func createTestEngine(t *testing.T) (*Engine, *mockVM) {
	// Create mock VM
	vm := &mockVM{
		lastAcceptedID: ids.GenerateTestID(),
		blocks:         make(map[ids.ID]*mockBlock),
	}

	// Create context
	ctx := &quasar.ConsensusContext{
		Context: &quasar.Context{
			NodeID:       ids.GenerateTestNodeID(),
			Log:          log.NewNoOpLogger(),
			RingtailSK:   make([]byte, 64),
			RingtailPK:   make([]byte, 32),
			ValidatorState: &mockValidatorState{},
		},
		Registerer: prometheus.NewRegistry(),
	}

	// Create config
	config := Config{
		Params: Parameters{
			K:                     21,
			AlphaPreference:       15,
			AlphaConfidence:       18,
			Beta:                  8,
			MaxItemProcessingTime: 1 * time.Second,
		},
		QuasarEnabled:     false,
		QuasarTimeout:     100 * time.Millisecond,
		RingtailThreshold: 15,
	}

	// Create engine
	engine, err := NewEngine(config, ctx, vm)
	require.NoError(t, err)

	return engine, vm
}

// Mock implementations

type mockVM struct {
	lastAcceptedID ids.ID
	blocks         map[ids.ID]*mockBlock
	shouldBuild    bool
}

func (m *mockVM) GetBlock(ctx context.Context, blkID ids.ID) (quasar.Block, error) {
	blk, ok := m.blocks[blkID]
	if !ok {
		return nil, errors.New("block not found")
	}
	return blk, nil
}

func (m *mockVM) BuildBlock(ctx context.Context) (quasar.Block, error) {
	if !m.shouldBuild {
		return nil, errors.New("not building")
	}

	blk := &mockBlock{
		id:       ids.GenerateTestID(),
		parentID: m.lastAcceptedID,
		height:   1,
		bytes:    []byte("test block"),
		status:   choices.Processing,
	}

	m.blocks[blk.id] = blk
	return blk, nil
}

func (m *mockVM) SetPreference(ctx context.Context, blkID ids.ID) error {
	return nil
}

func (m *mockVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	return m.lastAcceptedID, nil
}

func (m *mockVM) VerifyWithContext(ctx context.Context, blk quasar.Block) error {
	return nil
}

type mockBlock struct {
	id        ids.ID
	parentID  ids.ID
	height    uint64
	timestamp int64
	bytes     []byte
	status    choices.Status
	blsSig    []byte
	rtCert    []byte
}

func (b *mockBlock) ID() ids.ID                { return b.id }
func (b *mockBlock) Parent() ids.ID            { return b.parentID }
func (b *mockBlock) Height() uint64            { return b.height }
func (b *mockBlock) Timestamp() int64          { return b.timestamp }
func (b *mockBlock) Bytes() []byte             { return b.bytes }
func (b *mockBlock) Verify() error             { return nil }
func (b *mockBlock) Accept() error             { b.status = choices.Accepted; return nil }
func (b *mockBlock) Reject() error             { b.status = choices.Rejected; return nil }
func (b *mockBlock) Status() choices.Status    { return b.status }
func (b *mockBlock) HasDualCert() bool         { return len(b.blsSig) > 0 && len(b.rtCert) > 0 }
func (b *mockBlock) BLSSignature() []byte      { return b.blsSig }
func (b *mockBlock) RTCertificate() []byte     { return b.rtCert }
func (b *mockBlock) SetQuantum() error         { b.status = choices.Quantum; return nil }

// mockQuasar implements a mock for testing
type mockQuasar struct {
	certReady chan []byte
}

func (m *mockQuasar) RegisterForCertificate(height uint64, ch chan []byte) {
	go func() {
		select {
		case cert := <-m.certReady:
			ch <- cert
		case <-time.After(1 * time.Second):
			// Timeout
		}
	}()
}

func (m *mockQuasar) OnRTShare(height uint64, nodeID ids.NodeID, share []byte) {}
func (m *mockQuasar) QuickSign(msg []byte) ([]byte, error) { return make([]byte, 430), nil }

type mockValidatorState struct{}

func (m *mockValidatorState) GetValidatorSet(
	ctx interface{},
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return make(map[ids.NodeID]*validators.GetValidatorOutput), nil
}

func (m *mockValidatorState) GetValidator(
	ctx interface{},
	subnetID ids.ID,
	nodeID ids.NodeID,
) (*validators.GetValidatorOutput, error) {
	return &validators.GetValidatorOutput{
		NodeID: nodeID,
		Weight: 1,
	}, nil
}