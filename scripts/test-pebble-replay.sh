#!/bin/bash

# Test PebbleDB Replay - Direct loading from old mainnet
set -e

echo "=== PebbleDB Replay Test ==="
echo "Loading from: ~/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"
echo

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Configuration
OLD_DB="/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb"

# Check database exists
if [ ! -d "$OLD_DB" ]; then
    echo -e "${RED}Error: Database not found at: $OLD_DB${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Found PebbleDB at: $OLD_DB${NC}"

# Check database contents
echo
echo "Database info:"
ls -lah "$OLD_DB" | head -10
echo

# Build test program to directly test the replay
cd /Users/z/work/lux/node/vms/cchainvm

# Create a simple test program
cat > test_pebble_replay.go <<'EOF'
package main

import (
    "fmt"
    "log"
    "os"
    "path/filepath"
    
    "github.com/cockroachdb/pebble"
    "github.com/luxfi/geth/common"
)

func main() {
    dbPath := os.Args[1]
    fmt.Printf("Opening PebbleDB at: %s\n", dbPath)
    
    opts := &pebble.Options{
        ReadOnly: true,
    }
    
    db, err := pebble.Open(dbPath, opts)
    if err != nil {
        log.Fatalf("Failed to open PebbleDB: %v", err)
    }
    defer db.Close()
    
    fmt.Println("✓ Successfully opened PebbleDB")
    
    // Try to read some keys
    iter := db.NewIter(nil)
    defer iter.Close()
    
    keyCount := 0
    blockCount := 0
    stateCount := 0
    
    for iter.First(); keyCount < 100 && iter.Valid(); iter.Next() {
        key := iter.Key()
        keyCount++
        
        // Check key types
        if len(key) > 0 {
            prefix := string(key[0:1])
            switch prefix {
            case "b", "B": // Block data
                blockCount++
                if blockCount <= 3 {
                    fmt.Printf("Found block key: %x (len=%d)\n", key[:10], len(key))
                }
            case "n", "t", "c": // State data
                stateCount++
            case "H", "h": // Headers
                if blockCount == 0 {
                    fmt.Printf("Found header key: %x\n", key[:10])
                }
            }
        }
    }
    
    fmt.Printf("\nScanned %d keys:\n", keyCount)
    fmt.Printf("  Blocks: %d\n", blockCount)
    fmt.Printf("  State: %d\n", stateCount)
    
    // Try to get specific keys
    testKeys := []string{
        "LastBlock",
        "LastHeader", 
        "LastFast",
    }
    
    fmt.Println("\nChecking metadata keys:")
    for _, k := range testKeys {
        if value, closer, err := db.Get([]byte(k)); err == nil {
            if k == "LastBlock" && len(value) == 8 {
                height := uint64(0)
                for i := 0; i < 8; i++ {
                    height = height<<8 | uint64(value[i])
                }
                fmt.Printf("  %s: %d\n", k, height)
            } else if k == "LastHeader" || k == "LastFast" {
                hash := common.BytesToHash(value)
                fmt.Printf("  %s: %s\n", k, hash.Hex())
            } else {
                fmt.Printf("  %s: %x\n", k, value)
            }
            closer.Close()
        }
    }
}
EOF

# Build and run the test
echo -e "${YELLOW}Building test program...${NC}"
go build -o test_pebble test_pebble_replay.go

echo -e "${YELLOW}Running test...${NC}"
./test_pebble "$OLD_DB"

# Clean up
rm -f test_pebble test_pebble_replay.go

echo
echo -e "${GREEN}Test complete!${NC}"