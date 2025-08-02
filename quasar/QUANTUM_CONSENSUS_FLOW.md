# Quantum Consensus Flow for Lux Network

## Overview
The Lux Network uses a dual-finality consensus mechanism where blocks are finalized only after receiving both P-Chain BLS signatures and Q-Chain Lattice signatures. Q-Chain wraps all other chains to provide quantum-secure finality.

## Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                  Q-Chain (Post-Quantum Chain)                  │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐   │
│  │ A-Chain │ │ B-Chain │ │ C-Chain │ │ M-Chain │ │ X-Chain │   │
│  │   AI    │ │ Bridge  │ │   EVM   │ │   MPC   │ │Exchange │   │
│  └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘   │
│       │           │           │           │           │        │
│  ┌────▼───────────▼───────────▼───────────▼───────────▼────┐   │
│  │              Consensus Block Builder                    │   │
│  │         (Collects operations from all chains)           │   │
│  └────────────────────────┬────────────────────────────────┘   │
│                           │                                    │
│  ┌────────────────────────▼────────────────────────────────┐   │
│  │                  Consensus Block                        │   │
│  │  Height: N                                              │   │
│  │  Operations: {A: [...], B: [...], C: [...], ...}        │   │
│  └────────────────────────┬────────────────────────────────┘   │
└───────────────────────────┼────────────────────────────────────┘
                            │
        ┌───────────────────┴───────────────────┐
        │                                       │
        ▼                                       ▼
┌───────────────┐                       ┌───────────────┐
│   P-Chain     │                       │   Q-Chain     │
│ BLS Signature │                       │   Lattice     │
│   Aggregate   │                       │   Signature   │
└───────┬───────┘                       └───────┬───────┘
        │                                       │
        └───────────────────┬───────────────────┘
                            │
                            ▼
                    ┌──────────────┐
                    │ Post-Quantum │
                    │   Finality   │
                    └──────────────┘
```

## Consensus Flow Steps

### 1. Operation Collection (Parallel)
Each of the 8 chains operates independently, processing operations:
- **A-Chain**: AI inference, model updates
- **B-Chain**: Cross-chain bridges, wrapped assets
- **C-Chain**: Smart contracts, DeFi operations
- **M-Chain**: Multi-party computation rounds
- **P-Chain**: Validator management, staking
- **Q-Chain**: Quantum operations, finality coordination
- **X-Chain**: Asset exchanges, trading
- **Z-Chain**: Zero-knowledge proofs, privacy operations

### 2. Consensus Block Building
Every consensus interval (e.g., 6 seconds):
1. Q-Chain initiates consensus round
2. Collects operations from all chains
3. Builds ConsensusBlock with all operations
4. Assigns block height and parent reference

### 3. P-Chain BLS Signing
1. P-Chain validators receive ConsensusBlock
2. Each validator signs with their BLS key
3. Signatures are aggregated into single BLS signature
4. Aggregate signature submitted to Q-Chain

### 4. Q-Chain Ringtail Signing
1. Q-Chain validators receive ConsensusBlock
2. Generate post-quantum Ringtail signature
3. Create quantum-secure witness
4. Submit Ringtail signature

### 5. Dual Finality
1. Q-Chain verifies both signatures
2. Creates DualFinalityProof
3. Broadcasts finalized block to all chains
4. Each chain applies relevant operations

## Timing Parameters

```yaml
consensus_timing:
  operation_collection: 1s      # Collect ops from chains
  block_building: 500ms         # Build consensus block
  p_chain_signing: 2s           # P-Chain BLS aggregation
  q_chain_signing: 2s           # Q-Chain Ringtail
  finality_broadcast: 500ms     # Broadcast to chains
  total_round_time: 6s          # Complete consensus round

parallel_processing:
  chunk_size: 8                 # Process in groups of 8
  worker_pools:
    fast_chains: 16             # A, X chains
    secure_chains: 4            # M, Z chains
    balanced_chains: 8          # B, C, P, Q chains
```

## Security Properties

### Dual Finality Requirements
- **Classical Security**: P-Chain BLS threshold (e.g., 2/3 of stake)
- **Quantum Security**: Q-Chain Ringtail signature
- **Both Required**: Block only finalized with both signatures

### Fork Prevention
- No chain can finalize without Q-Chain wrapper
- Q-Chain maintains global ordering
- Dual signatures prevent both classical and quantum attacks

### Performance Optimization for 8 Cores
1. **CPU Affinity**: Each chain pinned to dedicated core
2. **Cache Optimization**: L3 cache partitioned per chain
3. **NUMA Awareness**: Memory allocated locally per chain
4. **Parallel Signing**: P-Chain and Q-Chain sign concurrently

## Example Consensus Round

```
Time 0s: Start Round Height 1000
├─ 0-1s:   Chains A,B,C,M,P,Q,X,Z collect operations
├─ 1-1.5s: Q-Chain builds ConsensusBlock
├─ 1.5-3.5s: P-Chain validators sign (parallel)
├─ 1.5-3.5s: Q-Chain validators sign (parallel)
├─ 3.5-4s: Verify dual signatures
├─ 4-4.5s: Create finality proof
├─ 4.5-5s: Broadcast to all chains
└─ 5-6s:   Chains apply operations, prepare next round
```

## Benefits

1. **Quantum Security**: Ringtail signatures protect against quantum attacks
2. **High Throughput**: 8 chains process in parallel
3. **Composability**: Operations can reference across chains
4. **Deterministic Finality**: Clear finality with dual signatures
5. **Optimal Hardware Usage**: Designed for 8-core systems
