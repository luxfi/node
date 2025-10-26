# LUX Node - Minimal C-Chain Setup

## Overview

Minimal, production-ready LUX node configuration for C-Chain development.
Single-node setup with immediate chain availability - no bootstrapping required.

## Configuration

- **Network ID**: 96369 (LUX Mainnet)
- **RPC Port**: 9630
- **Staking Port**: 9631
- **C-Chain Endpoint**: `http://127.0.0.1:9630/ext/bc/C/rpc`
- **Chain ID**: 96369 (0x17871)

## Quick Start

```bash
# Build the node (if not already built)
./scripts/build.sh

# Start with minimal configuration
./run-minimal.sh

# Test C-Chain connectivity
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId"}' \
     -H 'content-type:application/json' \
     http://127.0.0.1:9630/ext/bc/C/rpc
```

## Scripts

### `run-minimal.sh`
Simplest way to run the node. Uses `--dev` flag for single-node consensus.

```bash
./run-minimal.sh        # Start node
./run-minimal.sh clean  # Clean data and start fresh
```

### `run-production.sh`
Production wrapper with service management:

```bash
./run-production.sh start   # Start node
./run-production.sh stop    # Stop node
./run-production.sh status  # Check status
./run-production.sh logs    # View logs
./run-production.sh clean   # Remove all data
```

### `test-cchain.sh`
Verify C-Chain functionality:

```bash
./test-cchain.sh
```

## Systemd Service

For production deployment:

```bash
sudo cp luxd.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable luxd
sudo systemctl start luxd
```

## Test Account

Pre-funded account for testing:
- Address: `0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC`
- Balance: 10000 LUX

## Key Features

1. **Single-node consensus** - No validator requirements
2. **Immediate availability** - No bootstrap phase
3. **Dev mode** - Simplified configuration for development
4. **Minimal dependencies** - Uses standard library only
5. **Clean architecture** - No external abstractions

## Design Principles

Following Go/Plan 9 minimalism:
- Single obvious implementation
- Explicit error handling
- Standard library preferred
- Text-based configuration
- Fail fast with clear messages

## Troubleshooting

If node fails to start:
1. Check port availability: `lsof -i:9630`
2. Clean data directory: `rm -rf /home/z/work/lux/.luxd-single`
3. Verify build: `./build/luxd --version`
4. Check logs: `tail -f /tmp/luxd-dev.log`

## API Examples

```bash
# Get block number
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}' \
     -H 'content-type:application/json' \
     http://127.0.0.1:9630/ext/bc/C/rpc

# Get balance
curl -X POST --data '{
  "jsonrpc":"2.0",
  "id":1,
  "method":"eth_getBalance",
  "params":["0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC","latest"]
}' -H 'content-type:application/json' \
   http://127.0.0.1:9630/ext/bc/C/rpc
```

## Files

- `config-mainnet-minimal.json` - Minimal configuration (deprecated)
- `genesis-mainnet.json` - Genesis configuration (deprecated)
- `run-minimal.sh` - Primary runner script
- `run-production.sh` - Production management script
- `test-cchain.sh` - Testing utility
- `luxd.service` - Systemd service file