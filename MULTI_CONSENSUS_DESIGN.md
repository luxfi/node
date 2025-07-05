# Lux Node Multi-Consensus & Enhanced Features Design

## Overview

This document outlines the architectural changes needed to transform the Lux node into a multi-consensus capable validator that can simultaneously participate in multiple networks while adding quantum-safe signatures and native bridge functionality.

## 1. Multi-Consensus Architecture

### 1.1 Design Goals
- Single node can validate for both Avalanche mainnet and Lux mainnet simultaneously
- Maintain compatibility as Lux diverges with custom chainID and consensus parameters
- Pluggable consensus mechanism allowing easy addition of new networks
- Isolated state and configuration per network

### 1.2 Core Components

#### ConsensusPlugin Interface
```go
type ConsensusPlugin interface {
    // Network identification
    NetworkID() uint32
    ChainID() ids.ID
    
    // Consensus operations
    Initialize(ctx context.Context, config ConsensusConfig) error
    Start() error
    Stop() error
    
    // Validator operations
    IsValidator(nodeID ids.NodeID) bool
    GetValidatorSet(height uint64) (validators.Set, error)
    
    // Block processing
    BuildBlock(ctx context.Context) (snowman.Block, error)
    ParseBlock(ctx context.Context, bytes []byte) (snowman.Block, error)
    
    // State management
    GetState() database.Database
    GetVM() block.ChainVM
}
```

#### ConsensusManager
```go
type ConsensusManager struct {
    plugins map[uint32]ConsensusPlugin // networkID -> plugin
    configs map[uint32]ConsensusConfig
    
    // Shared resources
    nodeID     ids.NodeID
    networking network.Network
    database   database.Database
}
```

### 1.3 Configuration Structure
```json
{
  "consensus": {
    "networks": [
      {
        "networkID": 1,  // Avalanche Mainnet
        "enabled": true,
        "plugin": "avalanche",
        "config": {
          "minStake": 2000000000000,  // 2000 AVAX
          "chainConfigs": {}
        }
      },
      {
        "networkID": 99,  // Lux Mainnet
        "enabled": true,
        "plugin": "lux",
        "config": {
          "minStake": 1000000000000,  // 1000 LUX
          "totalSupply": 2000000000000000000000,  // 2T LUX
          "chainConfigs": {}
        }
      }
    ]
  }
}
```

## 2. Lux-Specific Consensus Changes

### 2.1 Staking Requirements
- Minimum stake: 1,000 LUX per validator
- Total supply: 2,000,000,000,000 (2T) LUX
- Update genesis configuration
- Modify staking calculations in P-Chain

### 2.2 Implementation Files to Modify
- `genesis/config.go` - Update supply and staking parameters
- `vms/platformvm/config/config.go` - Update minimum stake validation
- `vms/platformvm/txs/executor/standard_tx_executor.go` - Enforce new staking rules
- `vms/platformvm/txs/validator_tx.go` - Update validator transaction validation

## 3. Quantum-Safe Signatures (CKKS)

### 3.1 Lattice Library Integration
- Import lattice library for CKKS homomorphic encryption
- Add as optional signature type alongside secp256k1
- Enable native MPC operations on encrypted data

### 3.2 X-Chain Integration
```go
// New signature type
type CKKSSignature struct {
    PublicKey  []byte
    Signature  []byte
    Parameters CKKSParams
}

// Extended keychain interface
type QuantumSafeKeychain interface {
    Keychain
    NewCKKSKey() (*crypto.PrivateKeyCKKS, error)
    GetCKKSKey(address ids.ShortID) (*crypto.PrivateKeyCKKS, error)
    SignCKKS(message []byte, key *crypto.PrivateKeyCKKS) ([]byte, error)
}
```

### 3.3 Implementation Path
1. Add CKKS crypto package: `crypto/ckks/`
2. Extend `vms/avm/txs/` to support new signature type
3. Update wallet functionality to handle quantum-safe keys
4. Add opt-in mechanism for users to enable CKKS signatures

## 4. B-Chain (Bridge Chain) Architecture

### 4.1 Design Overview
- New native chain alongside C-Chain, P-Chain, and X-Chain
- Dedicated to cross-chain bridge operations
- Integrates existing bridge codebase
- Top 100 genesis validator NFTs control bridge validation

### 4.2 B-Chain VM Structure
```go
type BridgeVM struct {
    blockChain.VM
    
    // Bridge components
    mpcNode     *mpc.Node
    relayer     *relayer.Service
    attestation *attestation.Manager
    
    // NFT validator management
    validatorNFTs map[uint64]common.Address  // NFT ID -> owner
    activeNodes   map[ids.NodeID]uint64      // NodeID -> NFT ID
}
```

### 4.3 Integration Steps

#### 4.3.1 Move Bridge Components
1. Copy `/Users/z/work/hanzo/bridge/mpc` → `/Users/z/work/lux/node/vms/bvm/mpc`
2. Copy `/Users/z/work/hanzo/bridge/relayer` → `/Users/z/work/lux/node/vms/bvm/relayer`
3. Copy `/Users/z/work/hanzo/bridge/contracts` → `/Users/z/work/lux/node/vms/bvm/contracts`

#### 4.3.2 Create B-Chain VM
- Base path: `/Users/z/work/lux/node/vms/bvm/`
- Implement `block.ChainVM` interface
- Add bridge-specific APIs
- Integrate MPC key management

### 4.4 Validator NFT System

#### 4.4.1 NFT Contract Design
```solidity
contract BridgeValidatorNFT {
    uint256 constant MAX_VALIDATORS = 100;
    mapping(uint256 => address) public nftOwners;
    mapping(uint256 => bytes) public validatorPubKeys;
    
    event ValidatorTransferred(uint256 indexed nftId, address from, address to);
    event KeyRotationRequested(uint256 indexed nftId, bytes newPubKey);
    
    function transferValidator(uint256 nftId, address to, bytes newPubKey) external;
    function requestKeyRotation(uint256 nftId) external;
}
```

#### 4.4.2 Key Rotation Protocol
1. NFT transfer triggers key rotation request
2. Current validator set acknowledges transfer
3. MPC ceremony generates new key shares
4. Old validator's access revoked after handoff
5. New validator joins consensus with fresh keys

## 5. Implementation Phases

### Phase 1: Multi-Consensus Foundation (Weeks 1-4)
1. Design and implement ConsensusPlugin interface
2. Create ConsensusManager 
3. Refactor existing consensus to use plugin architecture
4. Add configuration loading for multiple networks
5. Test dual-network validation

### Phase 2: Lux Consensus Parameters (Weeks 5-6)
1. Update genesis configuration for 2T supply
2. Modify staking requirements to 1000 LUX
3. Create Lux-specific consensus plugin
4. Test staking and validation logic

### Phase 3: Quantum-Safe Signatures (Weeks 7-10)
1. Integrate lattice library
2. Implement CKKS signature support
3. Add to X-Chain transaction types
4. Create wallet integration
5. Build opt-in UI/UX

### Phase 4: B-Chain Development (Weeks 11-16)
1. Port bridge codebase to node
2. Create BridgeVM implementation
3. Implement validator NFT smart contract
4. Build key rotation mechanism
5. Integrate with existing chains

### Phase 5: Bitcoin Integration (Weeks 17-20)
1. Add Bitcoin light client to B-Chain
2. Implement Bitcoin UTXO verification
3. Create Bitcoin bridge contracts
4. Test cross-chain transfers

## 6. Testing Strategy

### 6.1 Unit Tests
- Consensus plugin switching
- CKKS signature verification
- NFT transfer and key rotation
- Bridge operation validation

### 6.2 Integration Tests
- Multi-network validation
- Cross-chain communication
- MPC key generation ceremonies
- Bitcoin bridge operations

### 6.3 Network Tests
- Local network with all features
- Testnet deployment
- Stress testing bridge capacity
- Validator handoff scenarios

## 7. Migration Plan

### 7.1 Existing Validators
1. Provide migration tool for current validators
2. Grandfather existing stakes with conversion rate
3. Gradual rollout of new features

### 7.2 Bridge Migration
1. Deploy B-Chain in parallel with existing bridge
2. Migrate liquidity gradually
3. Sunset external bridge infrastructure

## 8. Security Considerations

### 8.1 Multi-Consensus Isolation
- Separate state databases per network
- Independent mempool management
- Isolated network connections

### 8.2 Quantum Safety
- CKKS parameters chosen for 256-bit security
- Hybrid signatures during transition period
- Key migration tools for users

### 8.3 Bridge Security
- Validator NFT smart contract audits
- MPC ceremony security protocols
- Rate limiting and monitoring

## 9. API Changes

### 9.1 New Endpoints
- `/ext/consensus/networks` - List active networks
- `/ext/x/ckks/address` - Generate CKKS address
- `/ext/b/validators` - List bridge validators
- `/ext/b/nft/{id}` - Get NFT validator info

### 9.2 Modified Endpoints
- Platform API to support multiple networks
- Wallet API for quantum-safe operations
- Info API to show multi-network status

## 10. Next Steps

1. Create detailed technical specifications for each component
2. Set up development branches for each phase
3. Assign team members to specific modules
4. Begin Phase 1 implementation