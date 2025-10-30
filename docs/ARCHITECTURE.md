# Lux Network Chain Architecture

## Chain Overview

Lux Network implements a 7-chain architecture optimized for specific use cases:

### Core Chains

1. **A-Chain (AttestationVM - avm)**
   - AI/ML model verification and inference
   - Oracle quorum consensus
   - TEE (Trusted Execution Environment) attestations
   - GPU computation verification

2. **B-Chain (BridgeVM - bvm)**
   - MPC-secured cross-chain bridges
   - Liquidity rebalancing
   - 100M LUX validator requirement for enhanced security
   - Threshold signatures using CGGMP21

3. **C-Chain (ContractVM - cvm)**
   - EVM-compatible smart contracts via Geth
   - DeFi applications
   - NFT contracts
   - General purpose computation

4. **D-Chain (DaoVM - dvm)** *(formerly P-Chain)*
   - Ecosystem governance
   - Validator staking and delegation
   - Subnet management
   - Treasury operations
   - NFT-based validator staking (1M LUX minimum or equivalent NFT)

5. **X-Chain (ExchangeVM - xvm)**
   - High-throughput UTXO transactions
   - Native asset creation and exchange
   - DAG-based consensus for speed
   - DEX functionality

6. **Z-Chain (ZeroKnowledgeVM - zvm)**
   - Fully homomorphic encryption (FHE)
   - Zero-knowledge proofs (Groth16, PLONK, Bulletproofs)
   - Confidential transactions
   - Ring signatures for privacy

7. **Y-Chain (Yield/Years-Proof VM - yvm)**
   - Quantum-resistant checkpoint ledger
   - SPHINCS+ hash-based signatures
   - Epoch-based state root commitments
   - Bitcoin anchoring for immutability
   - Minimal footprint (~5KB blocks)

### Additional Infrastructure

- **Hanzo Chain**: Confidential GPU/AI workloads (separate network)

## Key Features

### Security
- **Multi-signature schemes**: BLS (free), Ringtail (premium), CGGMP21 (threshold)
- **Quantum resistance**: Y-Chain uses SPHINCS+ for long-term safety
- **MPC bridges**: Secure cross-chain asset transfers
- **NFT staking**: Genesis, Pioneer, and Standard tiers with multipliers

### Performance
- **2T LUX total supply**
- **5-node minimum test network**
- **Aggregated signatures** for efficiency
- **DAG consensus** on X-Chain for high throughput

### Governance
- **D-Chain (DAO)** manages ecosystem decisions
- **100M LUX requirement** for B-Chain validators
- **1M LUX minimum** for standard validators (or NFT equivalent)
- **Slashing mechanisms** via Y-Chain checkpoints

### Privacy
- **Z-Chain** provides full privacy features
- **FHE** for encrypted computations
- **Nullifier-based** double-spend prevention
- **Ring signatures** for transaction privacy

## Architecture Benefits

1. **Separation of Concerns**: Each chain optimized for specific tasks
2. **Scalability**: Independent chains can scale based on their needs
3. **Security**: Multiple cryptographic primitives provide defense-in-depth
4. **Interoperability**: B-Chain enables seamless cross-chain operations
5. **Future-Proof**: Y-Chain ensures quantum resistance
6. **Developer-Friendly**: C-Chain maintains EVM compatibility