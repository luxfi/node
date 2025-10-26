package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/luxfi/database/pebbledb"
)

func main() {
	// Open the existing database
	dbPath := "~/.luxd/chainData/mainnet/db"

	fmt.Printf("Opening database at: %s\n", dbPath)

	// Open pebbledb
	db, err := pebbledb.New(dbPath, 1024, 1024, "", false)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Read the genesis bytes
	genesisKey := []byte("genesis")
	genesisBytes, err := db.Get(genesisKey)
	if err != nil {
		log.Fatalf("Failed to read genesis from database: %v", err)
	}

	fmt.Printf("Found genesis bytes: %d bytes\n", len(genesisBytes))

	// Parse genesis
	var gen map[string]interface{}
	if err := json.Unmarshal(genesisBytes, &gen); err != nil {
		log.Fatalf("Failed to parse genesis: %v", err)
	}

	fmt.Printf("Genesis parsed successfully\n")

	// Marshal to JSON for saving
	jsonBytes, err := json.MarshalIndent(gen, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal genesis: %v", err)
	}

	// Save to file
	outputFile := "extracted_genesis.json"
	if err := os.WriteFile(outputFile, jsonBytes, 0644); err != nil {
		log.Fatalf("Failed to write genesis file: %v", err)
	}

	fmt.Printf("Genesis saved to: %s\n", outputFile)
}
