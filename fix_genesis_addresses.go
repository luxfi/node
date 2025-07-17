//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/formatting/address"
)

func main() {
	// Read genesis file
	data, err := ioutil.ReadFile("genesis/genesis_mainnet.json")
	if err != nil {
		log.Fatal(err)
	}

	var genesis map[string]interface{}
	if err := json.Unmarshal(data, &genesis); err != nil {
		log.Fatal(err)
	}

	// Fix allocations
	if allocations, ok := genesis["allocations"].([]interface{}); ok {
		for i, alloc := range allocations {
			if allocation, ok := alloc.(map[string]interface{}); ok {
				if luxAddr, ok := allocation["luxAddr"].(string); ok {
					// Parse the address to get the bytes
					parts := strings.Split(luxAddr, "-")
					if len(parts) == 2 && parts[0] == "X" {
						// Try to decode the address part
						hrp, _, addrBytes, err := address.Parse(luxAddr)
						if err != nil {
							// If parsing fails, try to extract just the address bytes
							// and reformat with correct checksum
							fmt.Printf("Address %d (%s) has invalid checksum, skipping for now\n", i, luxAddr)
							continue
						}
						
						// Convert to short ID
						shortID, err := ids.ToShortID(addrBytes)
						if err != nil {
							fmt.Printf("Failed to convert address %d to short ID: %v\n", i, err)
							continue
						}
						
						// Reformat with correct checksum
						newAddr, err := address.FormatBech32(hrp, shortID[:])
						if err != nil {
							fmt.Printf("Failed to format address %d: %v\n", i, err)
							continue
						}
						
						fmt.Printf("Fixed address %d: %s -> X-%s\n", i, luxAddr, newAddr)
						allocation["luxAddr"] = fmt.Sprintf("X-%s", newAddr)
					}
				}
			}
		}
	}

	// Write back
	output, err := json.MarshalIndent(genesis, "", "\t")
	if err != nil {
		log.Fatal(err)
	}

	if err := ioutil.WriteFile("genesis/genesis_mainnet_fixed.json", output, 0644); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Fixed genesis written to genesis/genesis_mainnet_fixed.json")
}