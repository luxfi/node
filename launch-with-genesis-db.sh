#!/bin/bash
# Launch luxd with genesis-db flag for replay (NO LUX_GENESIS env var needed)

cd /home/z/work/lux/node

./build/luxd \
  --network-id=96369 \
  --http-host=0.0.0.0 \
  --http-port=9630 \
  --staking-port=9631 \
  --log-level=info \
  --consensus-sample-size=1 \
  --consensus-quorum-size=1 \
  --consensus-commit-threshold=1 \
  --consensus-app-concurrency=512 \
  --skip-bootstrap \
  --data-dir=/home/z/.luxd \
  --chain-data-dir=/home/z/.luxd/chainData \
  --genesis-db=/home/z/work/lux/state/chaindata/lux-mainnet-96369/db/pebbledb \
  --genesis-db-type=pebbledb \
  2>&1 | tee /tmp/genesis-db-launch.log