# Launch Checklist

This document provides a concise launch checklist for performing the one‑time import genesis boot (C‑Chain database snapshot import) and subsequent normal pruned operation, along with operational guidance.

## Hardware requirements
- Minimum CPU, RAM, and disk space to support full node import and pruning.
- SSD recommended for chaindata; at least 500 GB free disk for initial import.

## Ports & firewall rules
- RPC HTTP (default 9630)
- Staking API (default 9631)
- (Optional) Prometheus scrape port if metrics enabled
- Open C‑Chain HTTP port and peer P2P ports as per network config.

## Quick‑start
```bash
# Install binaries for genesis version:
lux node install v1.0.0-genesis

# Unpack finalized C‑Chain DB snapshot:
tar -C ~/.luxd -xzf c-chain-db-final.tgz

# Launch in normal (pruned) mode:
lux node run \
    --network-id 96369 \
    --db-dir ~/.luxd/db \
    --chain-config-dir ~/.luxd/configs/chains \
    --http-port 9630 --staking-port 9631 \
    --index-enabled=true --log-level info
```

## Health & metrics endpoints
- Health: `GET http://localhost:9630/ext/health` (expect `healthy=true`)
- Metrics: `GET http://localhost:9630/ext/metrics`
- C‑Chain RPC: `POST http://localhost:9630/ext/bc/C/rpc` (e.g., `eth_blockNumber`)

## Backup & restore
- Backup DB directory after import:
  ```bash
  tar -C ~/.luxd -czf c-chain-db-final.tgz chainData
  sha256sum c-chain-db-final.tgz
  ```
- Restore on other hosts by unpacking into `~/.luxd/chainData`.

## Upgrade procedure
- After genesis import, future upgrades do **not** require import mode.
- Rolling upgrade: stop node, install new binary, restart normally.
- Ensure data directory is compatible (pruning settings consistent).
