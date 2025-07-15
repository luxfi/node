// Copyright (C) 2019-2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/version"
)

func TestSkipBootstrap(t *testing.T) {
	require := require.New(t)

	// Create a skip bootstrap tracker
	peers := NewPeers()
	sb := NewSkipBootstrap(peers)

	// Should always return true, even with no peers
	require.True(sb.ShouldStart())

	// Add some peers
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	
	sb.Connected(nodeID1, version.CurrentApp)
	require.True(sb.ShouldStart())
	
	sb.Connected(nodeID2, version.CurrentApp)
	require.True(sb.ShouldStart())

	// Disconnect peers
	sb.Disconnected(nodeID1)
	require.True(sb.ShouldStart())
	
	sb.Disconnected(nodeID2)
	require.True(sb.ShouldStart())

	// Even with no peers, should still return true
	require.Equal(0, sb.ConnectedWeight())
	require.True(sb.ShouldStart())
}

func TestSkipBootstrapInterface(t *testing.T) {
	require := require.New(t)

	// Ensure skipBootstrap implements the Startup interface
	var _ Startup = (*skipBootstrap)(nil)

	// Test that it properly embeds Peers
	peers := NewPeers()
	sb := NewSkipBootstrap(peers)

	// Test Peers methods work
	nodeID := ids.GenerateTestNodeID()
	sb.Connected(nodeID, version.CurrentApp)
	
	weight := sb.ConnectedWeight()
	require.Equal(1, weight)

	percent := sb.ConnectedPercent()
	require.Equal(1.0, percent)

	sb.Disconnected(nodeID)
	weight = sb.ConnectedWeight()
	require.Equal(0, weight)
}

func TestSkipBootstrapConcurrent(t *testing.T) {
	require := require.New(t)

	// Test concurrent access
	peers := NewPeers()
	sb := NewSkipBootstrap(peers)

	// Run multiple goroutines checking ShouldStart
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				require.True(sb.ShouldStart())
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Still should return true
	require.True(sb.ShouldStart())
}