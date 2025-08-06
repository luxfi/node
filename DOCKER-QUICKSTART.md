# Lux Node Docker Quick Start

## Available Images

```bash
ghcr.io/luxfi/node:latest     # Latest stable version
ghcr.io/luxfi/node:v1.16.15   # Specific version
```

## Quick Start with Historic C-Chain Data

The Docker image includes support for loading 1,082,781 historic blocks from the C-chain.

### 1. Run with Docker

```bash
docker run -d \
  --name luxd \
  -p 9630:9630 \
  -p 9631:9631 \
  -v /home/z/work/lux/state/cchain:/blockchain-data:ro \
  -e NETWORK_ID=96369 \
  ghcr.io/luxfi/node:latest
```

### 2. Run with Docker Compose

Use the provided `docker-compose-cchain.yml`:

```bash
docker-compose -f docker-compose-cchain.yml up -d
```

### 3. Check Status

```bash
# View logs
docker logs -f luxd

# Check if bootstrapped
curl -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"info.isBootstrapped","params":{"chain":"C"}}' \
  http://localhost:9630/ext/info

# Get current block number
curl -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  http://localhost:9630/ext/bc/C/rpc

# Get specific historic block (e.g., block 100000)
curl -X POST -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["0x186a0",false]}' \
  http://localhost:9630/ext/bc/C/rpc
```

## Features

- ✅ **1,082,781 Historic Blocks** - Full C-chain history loaded
- ✅ **Network ID 96369** - Lux mainnet configuration
- ✅ **Security** - Runs as non-root user (uid 1000)
- ✅ **Health Checks** - Built-in Docker health monitoring
- ✅ **Lightweight** - 220MB image size
- ✅ **Production Ready** - Supports KMS, K8s, Docker Swarm

## API Endpoints

- **JSON-RPC**: `http://localhost:9630/ext/bc/C/rpc`
- **WebSocket**: `ws://localhost:9630/ext/bc/C/ws`
- **Health**: `http://localhost:9630/ext/health`
- **Info**: `http://localhost:9630/ext/info`

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NETWORK_ID` | Network ID | `96369` |
| `HTTP_HOST` | HTTP API host | `0.0.0.0` |
| `HTTP_PORT` | HTTP API port | `9630` |
| `STAKING_PORT` | Staking port | `9631` |
| `LOG_LEVEL` | Log level | `info` |
| `STAKING_ENABLED` | Enable staking | `true` |
| `SYBIL_PROTECTION_ENABLED` | Enable sybil protection | `true` |

## Building from Source

```bash
# Build locally
make build

# Build Docker image
docker build -t ghcr.io/luxfi/node:local .

# Push to registry (requires GitHub token)
docker push ghcr.io/luxfi/node:latest
```

## Troubleshooting

### Chain Not Bootstrapping

If the chain stays in bootstrapping state:
1. Check logs: `docker logs luxd`
2. Ensure blockchain data is mounted correctly
3. For single-node testing, disable consensus requirements:
   ```bash
   -e STAKING_ENABLED=false \
   -e SYBIL_PROTECTION_ENABLED=false \
   -e SNOW_SAMPLE_SIZE=1 \
   -e SNOW_QUORUM_SIZE=1
   ```

### Port Already in Use

If port 9630 is already in use:
```bash
# Check what's using the port
lsof -i :9630

# Use a different port
docker run -p 9730:9630 ...
```

### Permission Denied

If you get permission errors with blockchain data:
```bash
# Ensure read permissions
chmod -R 755 /home/z/work/lux/state/cchain
```

## Support

- GitHub Issues: https://github.com/luxfi/node/issues
- Documentation: https://docs.lux.network