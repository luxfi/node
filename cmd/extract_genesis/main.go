package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/luxfi/database"
	"github.com/luxfi/database/pebbledb"
	"github.com/luxfi/node/genesis"
	"github.com/luxfi/node/utils/constants"
	"github.com/prometheus/client_golang/prometheus"
)

func main() {
	// Open the existing database
	dbPath := "~/.luxd/chainData/mainnet/db"

	fmt.Printf("Opening database at: %s\n", dbPath)

	// Open pebbledb
	db, err := pebbledb.New(dbPath, nil, database.NewDefaultLogger(), "", prometheus.NewRegistry())
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
	var gen genesis.Genesis
	if err := json.Unmarshal(genesisBytes, &gen); err != nil {
		// Try unparsed format
		var unparsed genesis.UnparsedGenesis
		if err := json.Unmarshal(genesisBytes, &unparsed); err != nil {
			log.Fatalf("Failed to parse genesis: %v", err)
		}

		// Convert unparsed to parsed
		gen, err = unparsed.Parse(constants.MainnetID)
		if err != nil {
			log.Fatalf("Failed to parse unparsed genesis: %v", err)
		}
	}

	// Calculate genesis ID
	genesisID, err := gen.GetID()
	if err != nil {
		log.Fatalf("Failed to calculate genesis ID: %v", err)
	}

	fmt.Printf("Genesis Network ID: %d\n", gen.NetworkID)
	fmt.Printf("Genesis ID: %s\n", genesisID)

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
