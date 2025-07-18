package main

import (
	"crypto/rand"
	"fmt"
	"log"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/cb58"
)

func main() {
	// Generate 11 random NodeIDs for Lux validators
	fmt.Println("Generating 11 NodeIDs for Lux validators:")
	fmt.Println()
	
	for i := 1; i <= 11; i++ {
		// Generate random 20 bytes for NodeID
		bytes := make([]byte, 20)
		if _, err := rand.Read(bytes); err != nil {
			log.Fatalf("Failed to generate random bytes: %v", err)
		}
		
		// Create NodeID from bytes
		nodeID := ids.NodeID(bytes)
		
		// Encode to CB58 with proper checksum
		encoded, err := cb58.Encode(nodeID[:])
		if err != nil {
			log.Fatalf("Failed to encode NodeID: %v", err)
		}
		
		fmt.Printf("Validator %d:\n", i)
		fmt.Printf("  NodeID bytes: %x\n", bytes)
		fmt.Printf("  NodeID: NodeID-%s\n", encoded)
		fmt.Println()
	}
}