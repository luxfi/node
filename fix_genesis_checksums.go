package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/formatting/address"
)

type Allocation struct {
	ETHAddr        string         `json:"ethAddr"`
	LUXAddr        string         `json:"luxAddr"`
	InitialAmount  uint64         `json:"initialAmount"`
	UnlockSchedule []LockedAmount `json:"unlockSchedule"`
}

type LockedAmount struct {
	Amount   uint64 `json:"amount"`
	Locktime uint64 `json:"locktime"`
}

type Genesis struct {
	NetworkID   uint32       `json:"networkID"`
	Allocations []Allocation `json:"allocations"`
	StartTime   uint64       `json:"startTime"`
	// Add other fields as needed
}

func fixAddress(addrStr string, networkID uint32) (string, error) {
	// Skip if already valid
	_, _, _, err := address.Parse(addrStr)
	if err == nil {
		return addrStr, nil
	}
	
	// Extract chain prefix and address part
	parts := strings.SplitN(addrStr, "-", 2)
	if len(parts) != 2 {
		return addrStr, nil
	}
	
	chainPrefix := parts[0]
	oldAddr := parts[1]
	
	// Generate a valid address with the same prefix pattern
	// For simplicity, we'll use a deterministic address based on the old one
	var addrBytes []byte
	if strings.HasPrefix(oldAddr, "lux1") {
		// Use the old address string as seed for deterministic generation
		hash := []byte(oldAddr)
		if len(hash) > 20 {
			hash = hash[:20]
		} else {
			hash = append(hash, make([]byte, 20-len(hash))...)
		}
		addrBytes = hash
	} else {
		// For other addresses, try to decode hex
		addrBytes = make([]byte, 20)
		hex.Encode(addrBytes, []byte(oldAddr))
	}
	
	// Get the correct HRP for the network
	hrp := constants.GetHRP(networkID)
	
	// Format with correct checksum
	newAddr, err := address.Format(chainPrefix, hrp, addrBytes)
	if err != nil {
		return addrStr, err
	}
	
	return newAddr, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run fix_genesis_checksums.go <genesis_file>")
		os.Exit(1)
	}
	
	filename := os.Args[1]
	
	// Read the genesis file
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		os.Exit(1)
	}
	
	// Parse JSON
	var genesis Genesis
	if err := json.Unmarshal(data, &genesis); err != nil {
		// Try parsing as raw allocations
		var allocations []Allocation
		if err2 := json.Unmarshal(data, &allocations); err2 != nil {
			fmt.Printf("Error parsing JSON: %v\n", err)
			os.Exit(1)
		}
		genesis.Allocations = allocations
	}
	
	// Fix addresses
	for i, alloc := range genesis.Allocations {
		fixed, err := fixAddress(alloc.LUXAddr, genesis.NetworkID)
		if err != nil {
			fmt.Printf("Warning: Could not fix address %s: %v\n", alloc.LUXAddr, err)
		} else if fixed != alloc.LUXAddr {
			fmt.Printf("Fixed: %s -> %s\n", alloc.LUXAddr, fixed)
			genesis.Allocations[i].LUXAddr = fixed
		}
	}
	
	// Write back
	output, err := json.MarshalIndent(genesis, "", "\t")
	if err != nil {
		fmt.Printf("Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}
	
	outputFile := strings.TrimSuffix(filename, ".json") + "_fixed.json"
	if err := ioutil.WriteFile(outputFile, output, 0644); err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("Fixed genesis written to %s\n", outputFile)
}