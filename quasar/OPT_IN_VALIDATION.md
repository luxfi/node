# Opt-In Chain Validation System

## Overview

The Lux Network implements an opt-in validation system that allows validators to choose which of the 8 chains they want to validate. B-Chain (Bridge) and M-Chain (MPC) validation is exclusive to Genesis NFT holders, making them key infrastructure for secure cross-chain operations.

## Chain Validation Structure

### Core Required Chains
All validators MUST validate:
- **P-Chain**: Platform chain for validator management
- **Q-Chain**: Quantum chain for post-quantum finality

### Public Opt-In Chains
Validators can choose to additionally validate:
- **C-Chain**: EVM-compatible smart contract chain
- **X-Chain**: Asset exchange chain

### Genesis NFT-Gated Chains
Only Genesis NFT holders can validate:
- **B-Chain**: Bridge chain for cross-chain operations
- **M-Chain**: MPC chain for threshold signatures

### Specialized Opt-In Chains
Advanced validators can opt into:
- **A-Chain**: AI operations chain
- **Z-Chain**: Zero-knowledge proof chain

## Validation Requirements

### Minimum Validator Sets
```yaml
Core Chains:
  P-Chain: 21 validators (full set)
  Q-Chain: 21 validators (full set)

Public Chains:
  C-Chain: 15 validators
  X-Chain: 15 validators

Genesis NFT-Gated:
  B-Chain: 7 validators (Genesis NFT holders only)
  M-Chain: 7 validators (Genesis NFT holders only)

Specialized:
  A-Chain: 5 validators
  Z-Chain: 5 validators
```

### Staking Requirements
```yaml
Minimum Stake per Chain:
  P-Chain: 100,000 LUX
  Q-Chain: 100,000 LUX
  C-Chain: 100,000 LUX
  X-Chain: 100,000 LUX
  A-Chain: 100,000 LUX
  B-Chain: 100,000 LUX (+ Genesis NFT)
  M-Chain: 100,000 LUX (+ Genesis NFT)
  Z-Chain: 100,000 LUX
```

## Genesis NFT System

### NFT Tiers
```yaml
Founders:    Token IDs 1-5      # Original founders
Early:       Token IDs 6-10     # Early contributors
Genesis:     Token IDs 11-100   # Genesis validators
Community:   Token IDs 101-1000 # Community validators
Bridge:      Token IDs 1001-2000 # Bridge specialist NFTs
MPC:         Token IDs 2001-3000 # MPC specialist NFTs
```

### Bridge Infrastructure
B-Chain and M-Chain form the critical bridge infrastructure:
- **B-Chain**: Manages cross-chain bridges and wrapped assets
- **M-Chain**: Provides threshold signatures for bridge security
- Genesis NFT holders form a trusted validator set for these critical operations

## Validator Configuration

### Basic Configuration (C,P,Q,X)
Most validators will run the basic set:
```go
config := &ChainValidationConfig{
    ValidateCChain: true,  // EVM operations
    ValidatePChain: true,  // Required
    ValidateQChain: true,  // Required
    ValidateXChain: true,  // Asset exchange
}
```

### Genesis Validator Configuration
Genesis NFT holders can validate bridge infrastructure:
```go
config := &ChainValidationConfig{
    // Basic chains
    ValidateCChain: true,
    ValidatePChain: true,
    ValidateQChain: true,
    ValidateXChain: true,

    // Genesis NFT-gated chains
    ValidateBChain: true,
    ValidateMChain: true,

    // Genesis NFT proof
    GenesisNFTContract: "0x...",
    GenesisNFTTokenIDs: []uint64{42}, // Their Genesis NFT ID
}
```

### Full Stack Configuration
Advanced validators can secure all 8 chains:
```go
config := &ChainValidationConfig{
    ValidateAChain: true,  // AI operations
    ValidateBChain: true,  // Bridge (requires Genesis NFT)
    ValidateCChain: true,  // EVM
    ValidateMChain: true,  // MPC (requires Genesis NFT)
    ValidatePChain: true,  // Platform (required)
    ValidateQChain: true,  // Quantum (required)
    ValidateXChain: true,  // Exchange
    ValidateZChain: true,  // Zero-knowledge

    // Genesis NFT for B/M chains
    GenesisNFTContract: "0x...",
    GenesisNFTTokenIDs: []uint64{42},
}
```

## Benefits of Opt-In System

### For Validators
1. **Flexibility**: Choose chains based on expertise and resources
2. **Specialization**: Focus on specific chain types
3. **Resource Optimization**: Run only what you need
4. **Exclusive Access**: Genesis NFT holders get bridge validator privileges

### For the Network
1. **Scalability**: Each chain has dedicated validators
2. **Security**: Genesis NFT-gating ensures trusted bridge validators
3. **Efficiency**: Validators aren't forced to run all chains
4. **Decentralization**: Different validator sets per chain

## Implementation Details

### Chain Registration
```go
// Validator registers their chain preferences
validatorManager.RegisterValidator(nodeID, config)

// System verifies:
// 1. P-Chain and Q-Chain participation (required)
// 2. Genesis NFT ownership for B/M chains
// 3. Staking requirements per chain
// 4. Minimum validator set requirements
```

### Consensus Participation
```go
// Only validators opted into a chain participate in its consensus
validators := validatorManager.GetValidatorsForChain(chainID)

// Genesis NFT-gated chains have additional verification
if chainID == BridgeChainID || chainID == MPCChainID {
    requireGenesisNFT(validatorID)
}
```

### Dynamic Updates
Validators can update their chain preferences:
```go
// Add new chain validation
config.ValidateAChain = true
validatorManager.UpdateValidatorConfig(nodeID, config)

// Remove chain validation (except P/Q)
config.ValidateZChain = false
validatorManager.UpdateValidatorConfig(nodeID, config)
```

## Security Considerations

### Bridge Security
- B/M chains are critical infrastructure
- Genesis NFT requirement ensures known, trusted validators
- Higher staking requirements for M-Chain (10,000 LUX)
- Smaller validator sets acceptable due to trust assumptions

### Chain Independence
- Each chain maintains its own validator set
- Chain failures don't affect other chains
- Validators can exit non-critical chains without affecting core operations

### Quantum Security
- All chains wrapped by Q-Chain for quantum finality
- Even opt-in chains get post-quantum security
- P-Chain + Q-Chain dual finality required for all operations
