package engine

import (
	"context"
	"testing"
	"time"
	
	"github.com/luxfi/ids"
	"github.com/luxfi/node/snow"
	"github.com/stretchr/testify/require"
)

func TestWaveFPCIntegration(t *testing.T) {
	// Create mock context
	ctx := &snow.Context{}
	
	// Create engine with default config
	engine, err := NewWaveFPCEngine(ctx, DefaultConfig())
	require.NoError(t, err)
	require.NotNil(t, engine)
	
	// Start engine
	err = engine.Start(context.Background())
	require.NoError(t, err)
	
	// Test transaction proposal and query
	txID := ids.GenerateTestID()
	
	// Propose transaction
	err = engine.Propose(context.Background(), txID)
	require.NoError(t, err)
	
	// Simulate votes for fast path (need 2f+1 = 7 votes)
	for i := 0; i < 7; i++ {
		engine.flare.Propose(txID)
	}
	
	// Query should return true via fast path
	ctx2, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	
	accepted, err := engine.Query(ctx2, txID)
	require.NoError(t, err)
	require.True(t, accepted)
	
	// Check metrics
	metrics := engine.Metrics()
	require.NotNil(t, metrics["flare_executable"])
	require.NotNil(t, metrics["dag_tips"])
	
	// Stop engine
	err = engine.Stop()
	require.NoError(t, err)
}

func TestVerkleValidation(t *testing.T) {
	ctx := &snow.Context{}
	
	engine, err := NewWaveFPCEngine(ctx, DefaultConfig())
	require.NoError(t, err)
	
	// Create mock witness data
	payload := make([]byte, 1000)
	payload[0] = 0x01 // varint length indicator
	
	// Mock header
	header := mockHeader{
		id: [32]byte{1, 2, 3},
	}
	
	// Validate witness
	valid, size, root := engine.ValidateWitness(header, payload)
	
	// Should validate (soft mode accepts everything)
	require.True(t, valid)
	require.Greater(t, size, 0)
	require.NotEqual(t, [32]byte{}, root)
}

func BenchmarkFastPath(b *testing.B) {
	ctx := &snow.Context{}
	
	engine, _ := NewWaveFPCEngine(ctx, DefaultConfig())
	_ = engine.Start(context.Background())
	defer engine.Stop()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		txID := ids.ID([32]byte{byte(i), byte(i >> 8)})
		
		// Fast path proposal
		for j := 0; j < 7; j++ {
			engine.flare.Propose(txID)
		}
		
		// Check status
		_ = engine.flare.Status(txID)
	}
}
