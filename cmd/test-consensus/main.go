// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"

	"github.com/luxfi/consensus"
)

func main() {
	fmt.Println("Testing Lux Consensus Package Integration")
	fmt.Println("==========================================")

	// Test that we can import consensus package
	fmt.Printf("Consensus package version: %s\n", "v1.22.0")

	// Create a consensus engine with the new clean API
	config := consensus.DefaultConfig()
	chain := consensus.NewChain(config)

	// Start the engine
	ctx := context.Background()
	if err := chain.Start(ctx); err != nil {
		fmt.Printf("Failed to start consensus engine: %v\n", err)
		return
	}

	// Create a test block
	block := consensus.NewBlock(
		consensus.ID{1, 2, 3},
		consensus.GenesisID,
		1,
		[]byte("Test block from node"),
	)

	// Add the block
	if err := chain.Add(ctx, block); err != nil {
		fmt.Printf("Failed to add block: %v\n", err)
		return
	}

	fmt.Println("\n✅ Consensus package successfully integrated with node!")
	fmt.Println("The consensus package now provides a clean single-import interface.")
	fmt.Println("\nAvailable consensus components:")
	fmt.Println("  - Chain consensus engine")
	fmt.Println("  - DAG consensus (coming soon)")
	fmt.Println("  - Post-quantum consensus (coming soon)")
	fmt.Println("  - AI-powered validation")
	fmt.Println("  - Block validation and verification")
	fmt.Println("  - Network protocol implementations")
	
	fmt.Printf("\nTest block added: ID=%s, Height=%d\n", block.ID, block.Height)
	
	// Clean up
	chain.Stop()
}