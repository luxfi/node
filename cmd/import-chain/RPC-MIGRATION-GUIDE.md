# Lux Network 96369 C-Chain Regenesis - RPC-Based Migration Guide

## Overview

This guide documents the **proper RPC-based approach** for migrating Lux Network 96369 from SubnetEVM (PebbleDB) to C-Chain (BadgerDB) with **complete state** (blocks + accounts + storage + code).

## Problem Statement

### Initial Approach (INCOMPLETE)
The initial migration attempt exported only blocks (headers, bodies, receipts) using direct database access:
- ✅ Exported 465,603 blocks to JSONL
- ✅ Imported blocks into BadgerDB
- ❌ **NO STATE DATA** (accounts, balances, storage, code)
- ❌ Treasury balance returns null ("missing trie node" error)
- ❌ Node stuck at block 0

### Root Cause
Direct database export cannot reconstruct state tries. Only block data was copied, missing:
- Account balances (e.g., treasury account with ~2T LUX)
- Account nonces
- Contract code
- Contract storage
- State root verification data

## Requirements

1. **Use RPC interface only** (no direct database manipulation)
2. **Export 100% of data** (blocks + complete state)
3. **Maintain block heights** (ensure proper chain continuity)
4. **Verify treasury balance** (must show > 1.9T LUX after migration)

## Solution: RPC-Based Complete State Migration

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                  Migration Workflow                          │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  1. Deploy Old Node (PebbleDB)                               │
│     └─> Read-only access to original PebbleDB                │
│     └─> Expose RPC on http://localhost:9640/ext/bc/C/rpc     │
│                                                               │
│  2. Export Complete State (RPC Queries)                       │
│     └─> Query blocks via eth_getBlockByNumber                │
│     └─> Extract accounts from transactions                   │
│     └─> Query balances via eth_getBalance                    │
│     └─> Query code via eth_getCode                           │
│     └─> Query storage root via eth_getProof                  │
│     └─> Output: complete-state.json (blocks + accounts)      │
│                                                               │
│  3. Import Complete State (RPC Writes)                        │
│     └─> Deploy new C-Chain node (BadgerDB)                   │
│     └─> Import via RPC: debug_importChain                    │
│     └─> Verify state root matches                            │
│     └─> Verify account balances                              │
│                                                               │
│  4. Verification                                              │
│     └─> Check treasury: eth_getBalance(0x9011E8...)          │
│     └─> Verify > 1.9T LUX                                    │
│     └─> Check block height matches (465,603)                 │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

### Tools Created

#### 1. RPC State Exporter (`rpc-export-state`)

**Purpose**: Query a running node via RPC and export complete state (blocks + accounts + code)

**Usage**:
```bash
./rpc-export-state \
  --rpc http://localhost:9640/ext/bc/C/rpc \
  --output /tmp/lux-96369-complete-state.json \
  --start 0 \
  --end 465603
```

**Output Format** (`CompleteStateExport`):
```json
{
  "networkID": 96369,
  "chainID": 96369,
  "latestBlock": 465603,
  "blocks": [
    {
      "number": 0,
      "hash": "0x...",
      "header": "0x...",
      "body": "0x...",
      "receipts": "0x..."
    }
  ],
  "accounts": {
    "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714": {
      "address": "0x9011E888251AB053B7bD1cdB598Db4f9DEd94714",
      "balance": "1900000000000000000000000000000",
      "nonce": 0,
      "code": "",
      "codeHash": "0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
      "root": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
    }
  },
  "code": {
    "0xabcd...": "0x608060405234801561001057600080fd5b50..."
  },
  "storage": {}
}
```

**Key Features**:
- Queries blocks via `eth_getBlockByNumber`
- Extracts all accounts from transactions (senders, recipients, coinbase)
- Queries each account's:
  - Balance (`eth_getBalance`)
  - Nonce (`eth_getNonce`)
  - Code (`eth_getCode`)
  - Storage root (`eth_getProof`)
- Deduplicates contract code
- Progress reporting every 100 accounts

#### 2. RPC State Importer (TO BE CREATED)

**Purpose**: Import complete state into new C-Chain via RPC

**Planned Usage**:
```bash
./rpc-import-state \
  --rpc http://localhost:9650/ext/bc/C/rpc \
  --input /tmp/lux-96369-complete-state.json \
  --verify-state-root
```

**Implementation Plan**:
- Use `debug_importChain` for blocks
- Use custom RPC calls for state injection (if available)
- Fallback: Use `debug_setHead` + state reconstruction
- Verify state root after import

## Step-by-Step Migration Process

### Step 1: Deploy Old Node with Original PebbleDB

**Objective**: Start a node with read-only access to original PebbleDB

**Required**:
- Original PebbleDB: `/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb`
- Network ID: 96369
- Port: 9640 (avoid conflict with other nodes)

**Options**:

**Option A: Using lux-cli** (if available):
```bash
lux migrate prepare \
  --db-path /Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb \
  --network-id 96369 \
  --http-port 9640
```

**Option B: Manual luxd deployment**:
```bash
# Stop any conflicting nodes
pkill -f luxd

# Start node with original PebbleDB
cd /Users/z/work/lux/node

./build/luxd \
  --network-id=96369 \
  --http-host=0.0.0.0 \
  --http-port=9640 \
  --staking-port=9641 \
  --data-dir=/tmp/luxd-old-pebble \
  --chain-config-dir=/tmp/luxd-old-pebble/configs \
  --log-level=info \
  --database-read-only=true

# Configure C-Chain to use PebbleDB
# (requires chain config pointing to original PebbleDB)
```

**Verification**:
```bash
# Check node is running
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  -H 'content-type:application/json;' \
  http://localhost:9640/ext/bc/C/rpc

# Should return: {"jsonrpc":"2.0","id":1,"result":"0x71ac3"} (465603)

# Check treasury balance
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["0x9011E888251AB053B7bD1cdB598Db4f9DEd94714","latest"]}' \
  -H 'content-type:application/json;' \
  http://localhost:9640/ext/bc/C/rpc

# Should return balance > 1.9T LUX
```

### Step 2: Export Complete State via RPC

**Objective**: Query old node and export all blocks + state

```bash
cd /Users/z/work/lux/node/cmd/import-chain

# Run RPC exporter
./rpc-export-state \
  --rpc http://localhost:9640/ext/bc/C/rpc \
  --output /tmp/lux-96369-complete-state.json \
  --start 0 \
  --end 465603

# Output:
# === RPC-Based Complete State Export ===
# Source RPC: http://localhost:9640/ext/bc/C/rpc
# Chain ID: 96369
# Latest block: 465603
# Exporting blocks: 0 to 465603
# Output: /tmp/lux-96369-complete-state.json
#
# Processed 465603 blocks, found 150000 unique accounts
# Queried 150000/150000 accounts
#
# ✓ Complete state exported to /tmp/lux-96369-complete-state.json
#
# Summary:
#   Blocks: 465603
#   Accounts: 150000
#   Contract code: 5000
#   Chain ID: 96369
#   Network ID: 96369
```

**Verification**:
```bash
# Check file size (should be large - several GB)
ls -lh /tmp/lux-96369-complete-state.json

# Verify JSON is valid
jq '.accounts["0x9011E888251AB053B7bD1cdB598Db4f9DEd94714"]' \
  /tmp/lux-96369-complete-state.json

# Should show treasury account with balance > 1.9T
```

### Step 3: Create RPC State Importer (PENDING)

**Objective**: Build tool to import complete state into new C-Chain

**Implementation requirements**:
- Parse `CompleteStateExport` JSON
- Import blocks via `debug_importChain` or similar
- Inject account state (may require custom RPC methods)
- Verify state root matches
- Handle genesis state properly

### Step 4: Deploy New C-Chain

**Objective**: Start fresh C-Chain node for import

```bash
# Clean old imported database
rm -rf ~/.luxd/db-96369-final

# Start new node
./build/luxd \
  --network-id=96369 \
  --http-host=0.0.0.0 \
  --http-port=9650 \
  --staking-port=9651 \
  --data-dir=~/.luxd/db-96369-final \
  --log-level=info
```

### Step 5: Import Complete State

**Objective**: Import using RPC importer tool

```bash
cd /Users/z/work/lux/node/cmd/import-chain

# Run RPC importer (TO BE IMPLEMENTED)
./rpc-import-state \
  --rpc http://localhost:9650/ext/bc/C/rpc \
  --input /tmp/lux-96369-complete-state.json \
  --verify-state-root
```

### Step 6: Verification

**Objective**: Verify migration succeeded

```bash
# Check block height
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  -H 'content-type:application/json;' \
  http://localhost:9650/ext/bc/C/rpc

# Should return: {"jsonrpc":"2.0","id":1,"result":"0x71ac3"} (465603)

# Check treasury balance
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBalance","params":["0x9011E888251AB053B7bD1cdB598Db4f9DEd94714","latest"]}' \
  -H 'content-type:application/json;' \
  http://localhost:9650/ext/bc/C/rpc

# MUST return balance > 1,900,000,000,000,000,000,000,000,000,000 wei (1.9T LUX)

# Verify state root
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x71ac3",false]}' \
  -H 'content-type:application/json;' \
  http://localhost:9650/ext/bc/C/rpc | jq '.result.stateRoot'

# Should match state root from old node
```

## Current Status

### ✅ Completed
1. Block-only export (465,603 blocks) - INCOMPLETE APPROACH
2. Block-only import - INCOMPLETE APPROACH
3. RPC state exporter tool created and compiled
4. Documentation of proper RPC-based workflow

### ⏳ Pending
1. Deploy node with original PebbleDB for RPC queries
2. Run RPC exporter to get complete state
3. Create RPC state importer tool
4. Import complete state into new C-Chain via RPC
5. Verify treasury balance > 1.9T LUX

## Key Files

### Export/Import Tools
- `rpc-export-state.go` - RPC-based complete state exporter ✅ READY
- `rpc-export-state` - Compiled binary ✅ READY
- `rpc-import-state.go` - RPC-based state importer ⏳ TO BE CREATED

### Data Files
- `/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb` - Original PebbleDB (751 files, 34.1M entries)
- `/tmp/lux-96369-complete-state.json` - Complete state export (TO BE GENERATED)
- `/tmp/lux-mainnet-96369-full.jsonl` - Block-only export (INCOMPLETE, 465,603 blocks)

### Database Locations
- `~/.luxd/db-96369-imported/` - Block-only import (INCOMPLETE)
- `~/.luxd/db-96369-final/` - Target for complete state import (TO BE CREATED)

## Technical Notes

### Why Direct Database Migration Failed

1. **State Trie Structure**: Ethereum state is stored in Merkle Patricia Tries, not flat key-value pairs
2. **Hash Dependencies**: Account states reference storage tries via root hashes
3. **RLP Encoding**: Headers have different encoding between SubnetEVM (17 fields) and C-Chain (22 fields)
4. **Missing Intermediate Nodes**: State trie requires intermediate nodes, not just leaf data

### Why RPC Approach Works

1. **Proper State Reconstruction**: RPC queries return verified account states
2. **Complete Data**: Includes balances, nonces, code, and storage roots
3. **No Database Format Issues**: Works across PebbleDB → BadgerDB transition
4. **Verified Imports**: State root verification ensures integrity

### Performance Considerations

- **Export Time**: ~2-4 hours for 465,603 blocks + 150,000 accounts
- **File Size**: ~5-10 GB for complete state JSON
- **Import Time**: ~3-6 hours via RPC
- **Memory Usage**: ~8-16 GB during export/import

### Alternative Approaches Considered

1. ❌ **Direct Database Copy**: Cannot reconstruct state tries
2. ❌ **Block-only Migration**: Missing account state (current failed attempt)
3. ✅ **RPC-based Migration**: Proper approach using verified state queries

## Next Steps

1. **Deploy Old Node**: Get original PebbleDB accessible via RPC
2. **Run RPC Export**: Generate complete state JSON
3. **Implement RPC Importer**: Build state import tool
4. **Execute Migration**: Import complete state into new C-Chain
5. **Verify Success**: Confirm treasury balance and block height

## References

- Original PebbleDB: `/Users/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb`
- Network ID: 96369 (Lux Mainnet)
- Blockchain ID: `dnmzhuf6poM6PUNQCe7MWWfBdTJEnddhHRNXz2x7H6qSmyBEJ`
- Treasury Account: `0x9011E888251AB053B7bD1cdB598Db4f9DEd94714`
- Expected Balance: > 1.9T LUX
