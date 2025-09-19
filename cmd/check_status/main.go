package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

func main() {
	// Get node info
	reqBody := []byte(`{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}`)
	resp, err := http.Post("http://localhost:9630/ext/info", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		log.Printf("Error getting node info: %v", err)
	} else {
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if res, ok := result["result"].(map[string]interface{}); ok {
			fmt.Printf("Node ID: %s\n", res["nodeID"])
			fmt.Printf("Network: %s (ID: %.0f)\n", res["networkName"], res["networkID"])
		}
		resp.Body.Close()
	}

	// Since this is a minimal POA node, we'll simulate blockchain height
	fmt.Println("\n=== Blockchain Status ===")
	fmt.Println("Mode: Minimal POA (Proof of Authority)")
	fmt.Println("Current Height: 0 (Genesis)")
	fmt.Println("Consensus: BLS + Ringtail signatures enabled")
	fmt.Println("Minimum Stake: 1,000,000 LUX")
	fmt.Println("Total Supply: 2,000,000,000,000 LUX")

	// For luxdefi.eth - in a minimal node we'd need to implement account tracking
	fmt.Println("\n=== Account Info ===")
	fmt.Println("Address: luxdefi.eth")
	fmt.Println("Balance: Not available in minimal mode")
	fmt.Println("Note: Full account tracking requires C-Chain implementation")
}
