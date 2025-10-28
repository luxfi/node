# Regenesis 5-Node Mainnet Setup

## Overview
This document describes the process for launching a 5-node Lux mainnet network locally using deterministic keys derived from a BIP39 mnemonic.

## Mnemonic Configuration

**IMPORTANT: NEVER COMMIT THESE VALUES TO GIT**

Mnemonic stored in `/Users/z/work/lux/genesis/.env`:
```
minimum horror put keen cycle claw position flag recycle name zoo candy limb buyer nasty
```

## Key Derivation

**Mainnet Validators (Accounts 0-4):**
- BIP44 Path: m/44'/9000'/0'/0/{0-4}
- Node 1: Account 0
- Node 2: Account 1
- Node 3: Account 2
- Node 4: Account 3
- Node 5: Account 4

**Testnet Validators (Accounts 5-9):**
- BIP44 Path: m/44'/9000'/0'/0/{5-9}
- Node 1: Account 5
- Node 2: Account 6
- Node 3: Account 7
- Node 4: Account 8
- Node 5: Account 9

## Network Configuration

**Mainnet:**
- Network ID: 96369
- Chain ID: 96369
- 5 initial validators
- Each with equal stake

## Setup Steps

### 1. Generate Validator Keys

The node has a `derive-validators` tool (in development) that generates keys from the mnemonic:

```bash
cd /Users/z/work/lux/node
./build/derive-validators \
  --mnemonic "minimum horror put keen cycle claw position flag recycle name zoo candy limb buyer nasty" \
  --start 0 \
  --count 5 \
  --output ~/.luxd/mainnet-validators \
  --network mainnet
```

This will create 5 validator directories:
- `~/.luxd/mainnet-validators/node1/`
- `~/.luxd/mainnet-validators/node2/`
- `~/.luxd/mainnet-validators/node3/`
- `~/.luxd/mainnet-validators/node4/`
- `~/.luxd/mainnet-validators/node5/`

Each containing:
- `staking/staker.crt` - TLS certificate
- `staking/staker.key` - Private key
- `staking/signer.key` - BLS key

### 2. Generate Genesis File

Use the genesis tool to create the genesis file with these 5 validators:

```bash
cd /Users/z/work/lux/genesis
source .env
./bin/genesis generate mainnet \
  --mnemonic "$MNEMONIC" \
  --output configs/mainnet
```

This generates:
- `configs/mainnet/genesis.json` - Network genesis
- `configs/mainnet/bootstrap.json` - Bootstrap node info

### 3. Start 5-Node Network

Each node should be started with its own data directory and validator keys:

**Node 1 (Port 9650):**
```bash
./build/luxd \
  --data-dir=~/.luxd/node1 \
  --network-id=96369 \
  --http-port=9650 \
  --staking-port=9651 \
  --genesis-file=/Users/z/work/lux/genesis/configs/mainnet/genesis.json
```

**Node 2 (Port 9652):**
```bash
./build/luxd \
  --data-dir=~/.luxd/node2 \
  --network-id=96369 \
  --http-port=9652 \
  --staking-port=9653 \
  --bootstrap-ids=<node1-id> \
  --bootstrap-ips=127.0.0.1:9651 \
  --genesis-file=/Users/z/work/lux/genesis/configs/mainnet/genesis.json
```

**Node 3 (Port 9654):**
```bash
./build/luxd \
  --data-dir=~/.luxd/node3 \
  --network-id=96369 \
  --http-port=9654 \
  --staking-port=9655 \
  --bootstrap-ids=<node1-id>,<node2-id> \
  --bootstrap-ips=127.0.0.1:9651,127.0.0.1:9653 \
  --genesis-file=/Users/z/work/lux/genesis/configs/mainnet/genesis.json
```

**Node 4 (Port 9656):**
```bash
./build/luxd \
  --data-dir=~/.luxd/node4 \
  --network-id=96369 \
  --http-port=9656 \
  --staking-port=9657 \
  --bootstrap-ids=<node1-id>,<node2-id>,<node3-id> \
  --bootstrap-ips=127.0.0.1:9651,127.0.0.1:9653,127.0.0.1:9655 \
  --genesis-file=/Users/z/work/lux/genesis/configs/mainnet/genesis.json
```

**Node 5 (Port 9658):**
```bash
./build/luxd \
  --data-dir=~/.luxd/node5 \
  --network-id=96369 \
  --http-port=9658 \
  --staking-port=9659 \
  --bootstrap-ids=<node1-id>,<node2-id>,<node3-id>,<node4-id> \
  --bootstrap-ips=127.0.0.1:9651,127.0.0.1:9653,127.0.0.1:9655,127.0.0.1:9657 \
  --genesis-file=/Users/z/work/lux/genesis/configs/mainnet/genesis.json
```

## Next Steps

1. Complete the `derive-validators` tool in the node repository
2. Integrate key generation into lux-cli
3. Create Makefile targets for easy mainnet/testnet launch
4. Document deployment to production servers

## Security Notes

- The mnemonic is stored in .env files that are in .gitignore
- NEVER commit .env files to git
- Validator private keys should be backed up securely
- For production, consider hardware security modules (HSMs)

## Version

- Node Version: v1.20.0
- Branch: regenesis
- Granite ACPs: Integrated (ACP-181, LP-226, LP-176, ACP-204)
