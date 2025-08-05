package main

import (
	"encoding/json"
	"fmt"
	"os"
	
	"github.com/luxfi/node/genesis"
)

func main() {
	// Try to get the configs
	fmt.Println("Checking genesis configurations...")
	
	// Check mainnet
	fmt.Println("\nMainnet config:")
	mainnetJSON, _ := json.MarshalIndent(genesis.MainnetConfig, "", "  ")
	os.WriteFile("debug_mainnet.json", mainnetJSON, 0644)
	fmt.Println("Written to debug_mainnet.json")
	
	// Check testnet
	fmt.Println("\nTestnet config:")
	testnetJSON, _ := json.MarshalIndent(genesis.TestnetConfig, "", "  ")
	os.WriteFile("debug_testnet.json", testnetJSON, 0644)
	fmt.Println("Written to debug_testnet.json")
	
	// Check local
	fmt.Println("\nLocal config:")
	localJSON, _ := json.MarshalIndent(genesis.LocalConfig, "", "  ")
	os.WriteFile("debug_local.json", localJSON, 0644)
	fmt.Println("Written to debug_local.json")
}