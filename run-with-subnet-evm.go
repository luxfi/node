package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	
	"github.com/cockroachdb/pebble"
)

const (
	SUBNET_DB = "/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
	LUXD_PATH = "./build/luxd"
)

var namespace = []byte{
	0x33, 0x7f, 0xb7, 0x3f, 0x9b, 0xcd, 0xac, 0x8c,
	0x31, 0xa2, 0xd5, 0xf7, 0xb8, 0x77, 0xab, 0x1e,
	0x8a, 0x2b, 0x7f, 0x2a, 0x1e, 0x9b, 0xf0, 0x2a,
	0x0a, 0x0e, 0x6c, 0x6f, 0xd1, 0x64, 0xf1, 0xd1,
}

func main() {
	fmt.Println("=============================================================")
	fmt.Println("   Starting luxd with Subnet-EVM Database Support")
	fmt.Println("=============================================================")
	fmt.Println()
	
	// First, scan the database to find the highest block
	fmt.Println("Scanning subnet-EVM database for blocks...")
	db, err := pebble.Open(SUBNET_DB, &pebble.Options{ReadOnly: true})
	if err != nil {
		log.Error("Failed to open database:", err)
	}
	
	highestBlock := uint64(0)
	blockCount := 0
	iter, _ := db.NewIter(&pebble.IterOptions{})
	
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()
		
		if len(key) == 64 && bytes.Equal(key[:32], namespace) {
			hash := key[32:]
			if len(value) > 100 && (value[0] == 0xf8 || value[0] == 0xf9) {
				blockNum := uint64(hash[0])<<16 | uint64(hash[1])<<8 | uint64(hash[2])
				if blockNum > highestBlock {
					highestBlock = blockNum
				}
				blockCount++
			}
		}
	}
	iter.Close()
	db.Close()
	
	fmt.Printf("Found %d blocks, highest block: %d\n\n", blockCount, highestBlock)
	
	// Create a wrapper script that sets up the environment
	wrapperScript := fmt.Sprintf(`#!/bin/bash
export SUBNET_EVM_DB=true
export SUBNET_NAMESPACE="337fb73f9bcdac8c31a2d5f7b877ab1e8a2b7f2a1e9bf02a0a0e6c6fd164f1d1"
export SUBNET_HIGHEST_BLOCK=%d

# Start luxd with POA mode for development
%s \
  --network-id=96369 \
  --staking-enabled=false \
  --sybil-protection-enabled=false \
  --snow-sample-size=1 \
  --snow-quorum-size=1 \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --api-admin-enabled=true \
  --api-debug-enabled=true \
  --api-eth-enabled=true \
  --chain-config-dir=./chains \
  --db-dir=/tmp/luxd-subnet-evm \
  --log-level=debug
`, highestBlock, LUXD_PATH)
	
	// Write the wrapper script
	scriptPath := "/tmp/run-luxd-subnet.sh"
	err = os.WriteFile(scriptPath, []byte(wrapperScript), 0755)
	if err != nil {
		log.Error("Failed to write wrapper script:", err)
	}
	
	fmt.Println("Starting luxd with subnet-EVM support...")
	fmt.Println("Network ID: 96369")
	fmt.Println("POA mode: enabled")
	fmt.Println("RPC endpoint: http://localhost:9630/ext/bc/C/rpc")
	fmt.Println()
	
	// Execute the wrapper script
	cmd := exec.Command("/bin/bash", scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	
	err = cmd.Run()
	if err != nil {
		log.Error("Failed to start luxd:", err)
	}
}