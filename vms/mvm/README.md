# M-Chain VM (MVM)

The MPC-Chain VM (MVM) aka, "Money Chain", is a core component of the Lux Network that implements the bridge chain functionality, enabling seamless cross-chain asset transfers through the Lux Teleport Protocol.

## Overview

The M-Chain serves as the trust anchor and execution engine for cross-chain operations within the Lux ecosystem. It is secured by the top 100 highest-staked LUX validators who opt-in to participate in the M-Chain validator set.

## Key Features

### 1. **Lux Teleport Protocol**
- Seamless cross-chain asset transfers without wrapped tokens
- Native support for both fungible tokens and NFTs
- X-Chain serves as the universal settlement layer with mint/burn mechanics
- Zero-knowledge proofs for verification

### 2. **MPC Security (CGG21)**
- Implements Canetti-Gennaro-Goldfeder 2021 (CGG21) threshold signature scheme
- Distributed key management with no single point of failure
- 2/3+ threshold for security
- Automatic key rotation and resharing

### 3. **NFT Support**
- Full support for NFT transfers between UTXO (X-Chain) and account-based (C-Chain) models
- Special "Validator NFTs" that can be staked on P-Chain to operate validators
- Preserves NFT metadata across chains

### 4. **X-Chain Settlement**
- All assets entering the Lux ecosystem mint on X-Chain
- All assets leaving the Lux ecosystem burn on X-Chain
- Provides a unified settlement layer for cross-chain operations

## Architecture

```
┌─────────────────────┐
│    User Intent      │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│   M-Chain (MVM)     │
│  ┌───────────────┐  │
│  │ Intent Pool   │  │
│  └───────┬───────┘  │
│          │          │
│  ┌───────▼───────┐  │
│  │Teleport Engine│  │
│  └───────┬───────┘  │
│          │          │
│  ┌───────▼───────┐  │
│  │ MPC Manager   │  │
│  │   (CGG21)     │  │
│  └───────┬───────┘  │
└──────────┼──────────┘
           │
    ┌──────▼──────┐
    │  X-Chain    │
    │(Settlement) │
    └─────────────┘
```

## Building

```bash
cd node/bvm
go build -o bvm ./plugin
```

## Configuration

Example configuration for M-Chain VM:

```json
{
  "mpcEnabled": true,
  "mpcConfig": {
    "threshold": 67,
    "partyCount": 100,
    "keyGenTimeout": "5m",
    "signTimeout": "30s"
  },
  "teleportEnabled": true,
  "xChainSettlement": true,
  "xChainEndpoint": "http://localhost:9650/ext/bc/X",
  "zkEnabled": true,
  "zkConfig": {
    "proofSystem": "groth16"
  }
}
```

## Teleport Process Flow

### Fungible Token Transfer (L1 → C-Chain)

1. User signs intent: "Transfer 100 TOKEN from L1 to C-Chain"
2. M-Chain validators lock tokens on L1
3. X-Chain mints equivalent tokens
4. Executor swaps on C-Chain DEX
5. User receives native C-Chain tokens

### NFT Transfer (C-Chain → X-Chain)

1. User initiates NFT transfer
2. NFT burned on C-Chain
3. ZK proof generated
4. X-Chain mints native NFT UTXO
5. User owns NFT on X-Chain

### Validator NFT Staking (Any Chain → P-Chain)

1. User owns Validator NFT
2. NFT locked on source chain
3. M-Chain verifies validator eligibility
4. NFT registered on P-Chain
5. Validator activated

## Security Model

- **Economic Security**: Secured by top 100 LUX validators
- **Cryptographic Security**: CGG21 MPC with 2/3+ threshold
- **Verification**: Zero-knowledge proofs for all operations
- **No Single Point of Failure**: Distributed architecture

## API Endpoints

- `/bvm` - Core M-Chain APIs
- `/teleport` - Teleport Protocol operations
- `/mpc` - MPC status and operations
- `/validators` - M-Chain validator set info

## Development Status

The M-Chain VM is currently in active development. Key components implemented:
- ✅ Core VM structure
- ✅ Teleport Protocol engine
- ✅ X-Chain settlement integration
- ✅ NFT transfer support
- ✅ MPC manager framework
- 🚧 CGG21 protocol implementation
- 🚧 ZK proof generation/verification
- 🚧 Network message handlers
