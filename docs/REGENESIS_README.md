# Lux Network Regenesis Branch

This is the official mainnet launch branch for the Lux Network ecosystem, implementing our complete 6-chain architecture with advanced cryptographic features.

## Overview

The regenesis branch represents a complete reimagining of the Lux Network, extending the standard Avalanche 3-chain model to a comprehensive 6-chain architecture:

| Chain | Name | Purpose | Key Features |
|-------|------|---------|--------------|
| **P-Chain** | Platform | Validator management | NFT-based staking, 1M LUX minimum |
| **X-Chain** | Exchange | Asset transfers | High-throughput trading |
| **C-Chain** | Contract | Smart contracts | EVM compatible |
| **A-Chain** | Attestation | Oracle & AI verification | CGGMP21 signatures, TEE support |
| **B-Chain** | Bridge | Cross-chain interoperability | 100M LUX stake, dual MPC |
| **Z-Chain** | Zero-knowledge | Private transactions | ZK proofs, FHE optional |

## Key Innovations

### 1. Advanced Cryptography
- **CGGMP21 Threshold Signatures**: State-of-the-art threshold ECDSA with identifiable abort
- **Aggregated Signatures**: Support for BLS (free), Ringtail (premium), and CGGMP21
- **Zero-Knowledge Proofs**: Groth16, PLONK, and Bulletproofs support
- **Ring Signatures**: Ringtail implementation for validator privacy

### 2. NFT-Based Validator Staking
- 100 Genesis Validator NFTs
- Staking multipliers reducing LUX requirements
- Cross-chain verification with Ethereum mainnet
- Tiered system with different benefits

### 3. Privacy Features
- **Z-Chain**: Fully private transactions with shielded addresses
- **Ringtail Signatures**: Anonymous validator participation
- **FHE Support**: Optional computation on encrypted data

### 4. Multi-Consensus Support
- Avalanche Snowman++ (default)
- Ethereum Clique PoA
- Tendermint BFT
- OP Stack compatibility

## Token Economics

- **Total Supply**: 2,000,000,000,000 LUX (2 trillion)
- **Validator Requirements**:
  - Standard: 1,000,000 LUX (reducible with NFTs)
  - Bridge Validators: 100,000,000 LUX
  - Delegator Minimum: 25,000 LUX

## Quick Start

### 1. Build the Node
```bash
./scripts/build.sh
```

### 2. Launch Local Test Network
```bash
# Single node
./scripts/quick_launch.sh

# 5-node network
./scripts/local_network_5nodes.sh
```

### 3. Verify Features
```bash
./scripts/verify_lux_features.sh
```

## Architecture Documents

- [Multi-Consensus Design](MULTI_CONSENSUS_DESIGN.md)
- [NFT Staking Architecture](NFT_STAKING_ARCHITECTURE.md)
- [B-Chain Dual MPC Architecture](B_CHAIN_DUAL_MPC_ARCHITECTURE.md)
- [Quantum-Safe Architecture](QUANTUM_SAFE_UNIFIED_ARCHITECTURE.md)
- [Z-Chain FHE Implementation](Z_CHAIN_FHE_IMPLEMENTATION.md)
- [Ringtail Implementation Guide](RINGTAIL_IMPLEMENTATION_GUIDE.md)

## Development Status

### Completed ✅
- A-Chain implementation with attestation verification
- Z-Chain implementation with ZK proofs and privacy
- B-Chain enhanced with dual MPC architecture
- CGGMP21 threshold signature protocol
- Aggregated signature framework (BLS/Ringtail)
- NFT-based validator staking
- Multi-consensus framework
- Genesis configuration for all 6 chains

### In Progress 🚧
- Comprehensive test suite
- Integration testing with modified node binary
- 5-node test network deployment

### Planned 📋
- Mainnet genesis block creation
- Validator onboarding documentation
- SDK and tooling updates
- Cross-chain bridge UI

## Security Considerations

1. **Threshold Security**: CGGMP21 provides UC-secure threshold signatures
2. **Privacy**: Z-Chain offers optional full privacy with ZK proofs
3. **Bridge Security**: 100M LUX requirement ensures bridge validator commitment
4. **Quantum Preparedness**: Modular crypto allows post-quantum upgrades

## Contributing

This branch is the foundation for Lux Network's mainnet launch. Key areas for contribution:

1. Testing new chain implementations
2. Security audits of cryptographic components
3. Performance optimization
4. Documentation improvements
5. SDK development for new chains

## Genesis Configuration

The enhanced genesis configuration (`genesis/genesis_local_enhanced_v2.json`) includes:
- All 6 chains enabled
- 2T LUX initial allocation
- NFT staking parameters
- Multi-consensus settings
- Fee configuration for signature types

## Contact

For questions about the regenesis:
- GitHub Issues: [Create an issue](https://github.com/luxfi/node/issues)
- Documentation: [docs.lux.network](https://docs.lux.network)

---

**Note**: This branch represents a significant evolution of the Lux Network. Please review all documentation and test thoroughly before deploying in production environments.