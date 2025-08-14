package consensus

import (
	"context"
	"testing"
	
	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

func TestFPCEngine(t *testing.T) {
	// Create engine with f=3 (tolerates 3 Byzantine nodes)
	engine := NewFPCEngine(3)
	require.NotNil(t, engine)
	
	// Start engine
	err := engine.Start(context.Background())
	require.NoError(t, err)
	
	// Test transaction proposal
	txID := ids.GenerateTestID()
	err = engine.Propose(txID)
	require.NoError(t, err)
	
	// Simulate votes for fast path (need 2f+1 = 7 votes)
	for i := 0; i < 7; i++ {
		engine.flare.Propose(txID)
	}
	
	// Query should return true
	accepted, err := engine.Query(txID)
	require.NoError(t, err)
	require.True(t, accepted)
	
	// Check executable
	executable := engine.Executable()
	require.NotNil(t, executable)
	
	// Stop engine
	err = engine.Stop()
	require.NoError(t, err)
}

func TestVerkleIntegration(t *testing.T) {
	engine := NewFPCEngine(3)
	require.NotNil(t, engine)
	
	// Test witness validation
	payload := make([]byte, 1000)
	payload[0] = 0x01 // varint length indicator
	
	header := mockHeader{
		id: [32]byte{1, 2, 3},
	}
	
	valid, size, root := engine.ValidateWitness(header, payload)
	
	// Should validate (soft mode accepts everything)
	require.True(t, valid)
	require.Greater(t, size, 0)
	require.NotEqual(t, [32]byte{}, root)
}

func BenchmarkFPCEngine(b *testing.B) {
	engine := NewFPCEngine(3)
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
		engine.flare.Status(txID)
	}
}
