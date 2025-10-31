# Experimental Virtual Machines

This directory contains experimental VMs that are **NOT** part of mainnet launch.

## Status

### AIVM (AI Virtual Machine)
- **Status**: Experimental - Not ready for mainnet
- **Reason**: Redundant with Hanzo.network AI compute layer
- **Future**: May be integrated post-mainnet if distinct use case emerges
- **Features**: AI task registry, GPU provider coordination, proof-of-compute

### YVM (Years/Yield-Curve Virtual Machine)  
- **Status**: Experimental - Not ready for mainnet
- **Reason**: Checkpointing functionality can be handled by P-Chain
- **Future**: May be used for archival/historical state management
- **Features**: Epoch checkpointing, SPHINCS+ aggregation, Bitcoin anchoring, IPFS archival

## Mainnet VM Architecture

**Production VMs** (in `/vms/` root):
- **P-Chain** (PlatformVM): Quantum-safe consensus with Ringtail+BLS hybrid
- **X-Chain** (XVM): UTXO-based asset management
- **B-Chain** (BridgeVM): Cross-chain interoperability with MPC security
- **Z/A-Chain** (AttestationVM/ZVM): ZK privacy coprocessor + AI attestation
- **Q-Chain** (QVM): Quantum consensus oracle (PQC integrated into P-Chain)

## Development Guidelines

1. **No Mainnet Dependencies**: Code in this directory MUST NOT be required by mainnet VMs
2. **Independent Testing**: Each experimental VM has its own test suite
3. **Documentation**: Each VM must have a comprehensive README explaining:
   - Purpose and use cases
   - Why it's experimental
   - Path to production (if any)
   - Technical architecture
4. **PR Process**: Experimental VMs require separate PRs and approval before mainnet consideration

## Contributing

To propose an experimental VM for mainnet:
1. Ensure 100% test coverage
2. Document security model and attack vectors
3. Provide economic analysis (tokenomics, incentives)
4. Demonstrate unique value proposition vs existing chains
5. Get approval from core team

---

**Last Updated**: 2025-10-31
**Mainnet Launch Target**: TBD (P/X/B/Z-Chains first)
