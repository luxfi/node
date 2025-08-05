#!/bin/bash

echo "Testing mainnet with custom genesis..."

# Clean up previous test
rm -rf /tmp/luxd-mainnet-test

# Create test directories
mkdir -p /tmp/luxd-mainnet-test
mkdir -p /tmp/luxd-mainnet-test/configs/chains

# Copy the existing mainnet genesis
cp /home/z/work/lux/genesis/configs/lux-mainnet-96369/P/genesis.json /tmp/luxd-mainnet-test/

# Create chain config to use badgerdb for C-Chain
mkdir -p /tmp/luxd-mainnet-test/configs/chains/C
cat > /tmp/luxd-mainnet-test/configs/chains/C/config.json << 'EOF'
{
  "state-sync-enabled": false,
  "local-txs-enabled": true,
  "pruning-enabled": false,
  "api-max-duration": "0s",
  "api-max-blocks-per-request": 0,
  "allow-unfinalized-queries": false,
  "allow-unprotected-txs": true,
  "keystore-directory": "",
  "keystore-external-signer": "",
  "keystore-insecure-unlock-allowed": false,
  "remote-tx-gossip-only-enabled": false,
  "tx-regossip-frequency": "1m0s",
  "tx-regossip-max-size": 15,
  "log-level": "info",
  "offline-pruning-enabled": false,
  "offline-pruning-blocks-to-keep": 0,
  "offline-pruning-data-directory": "",
  "max-outbound-active-requests": 16,
  "max-outbound-active-cross-chain-requests": 64,
  "db-type": "badgerdb"
}
EOF

# Create staking keys for the expected NodeID
# We need to generate keys that produce NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg
mkdir -p /tmp/luxd-mainnet-test/staking

# For now, let's use a custom network ID to bypass mainnet restrictions
echo "Starting luxd with custom genesis..."
cd /home/z/work/lux/node

# Using a custom network ID (96370) with the mainnet genesis format
./build/luxd \
    --network-id=96370 \
    --genesis-file=/tmp/luxd-mainnet-test/genesis.json \
    --data-dir=/tmp/luxd-mainnet-test \
    --chain-config-dir=/tmp/luxd-mainnet-test/configs/chains \
    --db-type=pebbledb \
    --http-host=0.0.0.0 \
    --http-port=9630 \
    --staking-port=9631 \
    --log-level=info \
    --sybil-protection-enabled=false \
    --api-admin-enabled=true \
    --snow-sample-size=1 \
    --snow-quorum-size=1