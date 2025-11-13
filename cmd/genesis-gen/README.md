# Genesis Generator

A tool to generate valid `genesis.json` files for Lux local networks.

## Problem

The existing genesis files at `~/.lux/genesis/genesis.json` have invalid address checksums causing nodes to fail with "invalid checksum" errors. This tool generates properly formatted genesis files with valid Bech32 addresses.

## Features

- ✅ Generates valid Lux addresses with proper Bech32 checksums
- ✅ Creates configurable number of initial validators
- ✅ Supports loading existing staking certificates (optional)
- ✅ Generates fresh validator keys automatically
- ✅ Configurable network ID
- ✅ Creates complete P-Chain, X-Chain, and C-Chain genesis
- ✅ Validates all addresses and configurations

## Installation

```bash
cd /Users/z/work/lux/node/cmd/genesis-gen
go install
```

## Usage

### Generate new genesis with auto-generated validators

```bash
# Default: network ID 12345, 5 validators
genesis-gen

# Custom network ID and validator count
genesis-gen --network-id 54321 --num-validators 3 --output my_genesis.json
```

### Generate genesis from existing staking keys

If you have existing validator staking keys:

```bash
genesis-gen \
  --network-id 12345 \
  --num-validators 5 \
  --staking-keys-dir ~/.lux/staking \
  --output genesis.json
```

Expected directory structure for `--staking-keys-dir`:
```
~/.lux/staking/
├── node1/
│   └── staking/
│       ├── staker.crt
│       └── staker.key
├── node2/
│   └── staking/
│       ├── staker.crt
│       └── staker.key
...
```

## Command-Line Flags

- `--network-id`: Network ID for genesis (default: 12345)
- `--num-validators`: Number of initial validators (default: 5)
- `--output`: Output file path (default: genesis.json)
- `--staking-keys-dir`: Optional directory containing existing staking certificates

## Output

The tool prints:
1. Validator NodeIDs and reward addresses
2. Initial allocation address and amounts
3. Genesis file location

Example output:
```
Generating genesis for network ID 12345 with 5 validators

Generating new validator keys...
Initial supply: 360000000 LUX

✓ Genesis file written to: genesis.json

Validators:
  1. NodeID:      NodeID-Hrf88p5jsUPaPrLPJwftEQF66YKUc59Y9
     RewardAddr:  X-custom1hrnr0kgw2k4hc94wp0v2jz28ln6wkl64zqet3u

  2. NodeID:      NodeID-5bex7fsNMqxAfL2QkFB3hzifKQpEZDuKE
     RewardAddr:  X-custom1xfh9xwkkrp29fajpnnzugrxuul0xwy26l775y5

  ...

Initial Allocation:
  Address: X-custom1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5e6ndc5
  Amount:  252000000 LUX (unlocked)
  Locked:  108000000 LUX (for staking)
```

## Genesis Configuration

The generated genesis includes:

### Initial Supply
- **Total**: 360M LUX (same as mainnet)
- **Unlocked**: 252M LUX (70%) - available for transactions
- **Locked**: 108M LUX (30%) - locked for initial staking

### Validators
- **Stake Duration**: 1 year (365 days)
- **Delegation Fee**: 2% (20000 basis points)
- **Staking Start**: Current time
- **Staking End**: 1 year from start

### Chains
- **P-Chain**: Platform chain with validators and allocations
- **X-Chain**: Exchange chain (UTXO-based) with LUX asset
- **C-Chain**: EVM-compatible contract chain with 50M ETH pre-allocated

## Verification

To verify the generated genesis is valid:

```bash
# Install verification tool
cd /Users/z/work/lux/node/cmd/genesis-gen
go run main.go --output /tmp/test.json

# Verify with luxd
luxd --genesis-file=/tmp/test.json --network-id=12345 --version
```

Or use the test suite:

```bash
cd /Users/z/work/lux/node/cmd/genesis-gen
go test -v
```

## Use with Netrunner

To use the generated genesis with netrunner:

```bash
# Generate genesis
genesis-gen --network-id 12345 --output ~/.lux/genesis/genesis_12345.json

# Start network with netrunner
lux network start local \
  --genesis-file=~/.lux/genesis/genesis_12345.json \
  --network-id=12345
```

## Technical Details

### Address Format
All addresses use proper Bech32 encoding with checksums:
- **Format**: `<chain>-<hrp><address>`
- **HRP** (Human Readable Part): Determined by network ID
  - Network 12345: "custom"
  - Mainnet: "lux"
  - Testnet: "test"
- **Checksum**: Automatically calculated and validated

### NodeID Generation
NodeIDs are derived from validator certificates:
```
NodeID = RIPEMD160(SHA256(certificate))
```

### Validator Reward Addresses
Reward addresses are generated from validator NodeIDs to ensure uniqueness.

## Troubleshooting

### "invalid checksum" error
This means the genesis file has corrupt addresses. Regenerate with this tool.

### "no allocation to stake" error
The initial staked funds must match an allocation address. The tool ensures this automatically.

### Nodes fail to start
Check that:
1. Network ID matches between genesis and node config
2. Genesis file is valid JSON
3. All addresses have valid checksums

## Related Tools

- **derive-validators**: Generate deterministic validator keys from mnemonic
- **luxd**: Lux node implementation
- **netrunner**: Local network testing tool

## License

Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
See the file LICENSE for licensing terms.
