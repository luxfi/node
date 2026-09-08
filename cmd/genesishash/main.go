package main

import (
	"fmt"
	"os"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <genesis_file>")
		os.Exit(1)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("File size: %d bytes\n", len(data))

	// Compute hash the same way luxd does
	rawHash := hash.ComputeHash256(data)
	id, err := ids.ToID(rawHash)
	if err != nil {
		fmt.Printf("Error converting to ID: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Genesis ID: %s\n", id.String())
}
