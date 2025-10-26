# Genesis Replay Guide

This guide explains how to set up and run a Lux node with genesis database replay, using different database backends for replay vs runtime.

## Prerequisites

1. Build luxd with database support:
```bash
cd /home/z/work/lux/node
make build
```

2. Build genesis tool:
```bash
cd /home/z/work/lux/genesis
make build
```

## Step-by-Step Process

### 1. Generate Staking Keys

```bash
cd /home/z/work/lux/genesis

# Generate keys for single node
./bin/genesis staking keygen --output ./mainnet-keys

# For multi-node setup (5 validators)
for i in {1..5}; do
  ./bin/genesis staking keygen --output ./mainnet-keys/node$i
done
```

### 2. Prepare Genesis Configuration

The genesis tool handles the genesis configuration with database replay support:

```bash
# Verify genesis database exists
./bin/genesis inspect tip --db-path /path/to/subnet/evm/database --db-type pebbledb
```

### 3. Launch Node with Genesis Replay

#### Single Node Configuration

```bash
cd /home/z/work/lux/node

./build/luxd \
  --network-id=96369 \
  --genesis-db=/home/z/work/lux/genesis/state/chaindata/C-import/pebbledb \
  --genesis-db-type=pebbledb \
  --db-type=pebbledb \
  --p-chain-db-type=badgerdb \
  --x-chain-db-type=badgerdb \
  --c-chain-db-type=badgerdb \
  --data-dir=~/.luxd-mainnet \
  --staking-tls-cert-file=/home/z/work/lux/genesis/mainnet-keys/staker.crt \
  --staking-tls-key-file=/home/z/work/lux/genesis/mainnet-keys/staker.key \
  --staking-signer-key-file=/home/z/work/lux/genesis/mainnet-keys/signer.key \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --staking-port=9631 \
  --log-level=info \
  --sybil-protection-enabled=false \
  --consensus-sample-size=1 \
  --consensus-quorum-size=1 \
  --api-admin-enabled=true
```

#### Multi-Node Configuration

For production with multiple validators:

```bash
# Node 1 (bootstrap node)
./build/luxd \
  --network-id=96369 \
  --genesis-db=/home/z/work/lux/genesis/state/chaindata/C-import/pebbledb \
  --genesis-db-type=pebbledb \
  --db-type=pebbledb \
  --p-chain-db-type=badgerdb \
  --x-chain-db-type=badgerdb \
  --c-chain-db-type=badgerdb \
  --data-dir=~/.luxd-node1 \
  --staking-tls-cert-file=/home/z/work/lux/genesis/mainnet-keys/node1/staker.crt \
  --staking-tls-key-file=/home/z/work/lux/genesis/mainnet-keys/node1/staker.key \
  --staking-signer-key-file=/home/z/work/lux/genesis/mainnet-keys/node1/signer.key \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --staking-port=9631 \
  --log-level=info \
  --api-admin-enabled=true

# Get Node 1's NodeID and IP
NODE1_ID=$(curl -s -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}' -H 'content-type:application/json;' http://localhost:9630/ext/info | jq -r .result.nodeID)

# Node 2-5 (with bootstrap)
./build/luxd \
  --network-id=96369 \
  --genesis-db=/home/z/work/lux/genesis/state/chaindata/C-import/pebbledb \
  --genesis-db-type=pebbledb \
  --db-type=pebbledb \
  --p-chain-db-type=badgerdb \
  --x-chain-db-type=badgerdb \
  --c-chain-db-type=badgerdb \
  --data-dir=~/.luxd-node2 \
  --bootstrap-ips=127.0.0.1:9631 \
  --bootstrap-ids=$NODE1_ID \
  --staking-tls-cert-file=/home/z/work/lux/genesis/mainnet-keys/node2/staker.crt \
  --staking-tls-key-file=/home/z/work/lux/genesis/mainnet-keys/node2/staker.key \
  --staking-signer-key-file=/home/z/work/lux/genesis/mainnet-keys/node2/signer.key \
  --http-host=0.0.0.0 \
  --http-port=9632 \
  --staking-port=9633 \
  --log-level=info
```

## Verification

### Check Node Status

```bash
# Get node info
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNodeID","params":{}}' \
  -H 'content-type:application/json;' http://localhost:9630/ext/info | jq

# Check network ID
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"info.getNetworkID","params":{}}' \
  -H 'content-type:application/json;' http://localhost:9630/ext/info | jq

# Check blockchain status
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"platform.getBlockchains","params":{}}' \
  -H 'content-type:application/json;' http://localhost:9630/ext/P | jq
```

### Check C-Chain Replay Status

```bash
# Get current block height
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  -H 'content-type:application/json;' http://localhost:9630/ext/bc/C/rpc | jq

# Get chain ID
curl -X POST --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' \
  -H 'content-type:application/json;' http://localhost:9630/ext/bc/C/rpc | jq
```

## Database Backend Strategy

1. **Genesis Replay**: Uses PebbleDB to read existing subnet EVM data
2. **Runtime Storage**: 
   - P-Chain: BadgerDB for validator state
   - X-Chain: BadgerDB for UTXO transactions
   - C-Chain: BadgerDB for EVM state after replay

This configuration optimizes for:
- Fast replay from existing PebbleDB data
- Efficient runtime performance with BadgerDB
- Flexibility to tune each chain independently

## Troubleshooting

### Genesis Hash Mismatch
- Ensure --genesis-db points to correct database
- Verify --genesis-db-type matches actual database format

### Database Initialization Errors
- Check disk space and permissions
- Verify database build tags: `make build`

### Consensus Issues
- For single node: Use --consensus-sample-size=1 --consensus-quorum-size=1
- For multi-node: Ensure all nodes have same genesis configuration

## Advanced Configuration

### Custom Chain Configuration

Create chain-specific configs in data directory:

```bash
# C-Chain config for EVM settings
mkdir -p ~/.luxd-mainnet/configs/chains/C
cat > ~/.luxd-mainnet/configs/chains/C/config.json << EOF
{
  "eth-apis": ["eth", "personal", "admin", "debug", "web3"],
  "pruning-enabled": false,
  "state-sync-enabled": false,
  "allow-unfinalized-queries": true
}
EOF
```

### Performance Tuning

For production deployments:
- Increase file descriptor limits: `ulimit -n 65535`
- Allocate sufficient memory for BadgerDB
- Use SSD storage for database directories
- Monitor disk I/O during replay