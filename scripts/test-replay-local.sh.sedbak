#!/bin/bash

echo "Testing replay with local network ID..."

# Clean up previous test
rm -rf /tmp/luxd-replay-test

# Create test directories
mkdir -p /tmp/luxd-replay-test/genesis-db
mkdir -p /tmp/luxd-replay-test/configs/chains

# Use the test staking keys from genesis
cp -r /home/z/work/lux/genesis/test-mainnet-keys /tmp/luxd-replay-test/

# Create chain config to use badgerdb for C-Chain
mkdir -p /tmp/luxd-replay-test/configs/chains/C
cat > /tmp/luxd-replay-test/configs/chains/C/config.json << 'EOF'
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

# Now test luxd with database replay using local network ID
echo "Starting luxd with genesis database replay..."
cd /home/z/work/lux/node

# Using local network ID (12345) which has proper BLS validation setup
./build/luxd \
    --network-id=local \
    --data-dir=/tmp/luxd-replay-test \
    --chain-config-dir=/tmp/luxd-replay-test/configs/chains \
    --db-type=pebbledb \
    --staking-tls-cert-file=/tmp/luxd-replay-test/test-mainnet-keys/staker.crt \
    --staking-tls-key-file=/tmp/luxd-replay-test/test-mainnet-keys/staker.key \
    --staking-signer-key-file=/tmp/luxd-replay-test/test-mainnet-keys/signer.key \
    --http-host=0.0.0.0 \
    --http-port=9630 \
    --staking-port=9631 \
    --log-level=info \
    --sybil-protection-enabled=false \
    --api-admin-enabled=true