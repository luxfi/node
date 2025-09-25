#!/bin/bash

# Clean start for local development with chain ID 1337 and POA automining
# Based on working mainnet configuration

DATA_DIR="/tmp/luxd-local-1337-automining"
echo "Starting Lux node with chain ID 1337 and POA automining on port 9630..."

# Clean previous data
rm -rf "$DATA_DIR"

# Run with POA automining configuration similar to mainnet
./build/luxd \
  --network-id=1337 \
  --staking-enabled=false \
  --sybil-protection-enabled=false \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --staking-port=9631 \
  --data-dir="$DATA_DIR" \
  --snow-sample-size=1 \
  --snow-quorum-size=1 \
  --snow-virtuous-commit-threshold=1 \
  --snow-rogue-commit-threshold=2 \
  --snow-concurrent-repolls=1 \
  --snow-optimal-processing=10 \
  --index-enabled=false \
  --api-admin-enabled \
  --keystore-enabled \
  --api-keystore-enabled \
  --api-auth-required=false \
  --api-ipcs-enabled \
  --api-metrics-enabled \
  --chain-config-content='{"C": {"config": {"chainId": 1337, "homesteadBlock": 0, "daoForkBlock": 0, "daoForkSupport": false, "eip150Block": 0, "eip150Hash": "0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0", "eip155Block": 0, "eip158Block": 0, "byzantiumBlock": 0, "constantinopleBlock": 0, "petersburgBlock": 0, "istanbulBlock": 0, "muirGlacierBlock": 0, "berlinBlock": 0, "londonBlock": 0, "arrowGlacierBlock": 0, "grayGlacierBlock": 0, "mergeNetsplitBlock": 0, "shanghaiTime": 0, "cancunTime": 0, "etnaTime": 0, "blobSchedule": {"cancun": {"target": 3, "max": 6, "updateFraction": 3338477}}, "durangoTimestamp": 0, "feeConfig": {"gasLimit": 12000000, "targetBlockRate": 1, "minBaseFee": 1000000000, "targetGas": 60000000, "baseFeeChangeDenominator": 36, "minBlockGasCost": 0, "maxBlockGasCost": 1000000, "blockGasCostStep": 200000}, "warpConfig": {"blockTimestamp": 0, "quorumNumerator": 67, "requirePrimaryNetworkSigners": false}, "pruneConfig": {"pruneLimit": 0}, "continuousProfilerDir": "", "continuousProfilerFrequency": 0, "continuousProfilerMaxFiles": 0, "allowMissingTries": true}}}' \
  --log-level=info \
  --log-format=json