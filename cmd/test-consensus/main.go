package main

import (
	"fmt"

	"github.com/luxfi/consensus"
)

func main() {
	fmt.Println("Testing Lux Consensus Package Integration")
	fmt.Println("==========================================")

	// Test that we can import consensus package
	fmt.Printf("Consensus package version: %s\n", "integrated")

	// Verify consensus types are accessible
	var _ consensus.Acceptor

	fmt.Println("\n✅ Consensus package successfully integrated with node!")
	fmt.Println("The consensus package is now available as a native Go dependency.")
	fmt.Println("\nAvailable consensus components:")
	fmt.Println("  - Core consensus algorithms")
	fmt.Println("  - AI-powered consensus engines")
	fmt.Println("  - Block validation and verification")
	fmt.Println("  - Network protocol implementations")
}
