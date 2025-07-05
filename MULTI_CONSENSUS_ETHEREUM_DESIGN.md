# Multi-Consensus Design: Ethereum Integration

## Overview

This document extends the multi-consensus architecture to support Ethereum mainnet validation alongside Avalanche and Lux networks, enabling true cross-chain transaction synchronization.

## 1. Ethereum Consensus Integration Architecture

### 1.1 Extended ConsensusPlugin Interface
```go
type ConsensusPlugin interface {
    // Previous interface methods...
    
    // Ethereum-specific extensions
    GetConsensusType() ConsensusType // AVALANCHE, ETHEREUM, LUX
    GetFinalityThreshold() time.Duration
    
    // Cross-chain communication
    RegisterCrossChainHandler(handler CrossChainHandler)
    PropagateTransaction(tx CrossChainTx) error
}

type ConsensusType int
const (
    CONSENSUS_AVALANCHE ConsensusType = iota
    CONSENSUS_ETHEREUM
    CONSENSUS_LUX
)
```

### 1.2 Ethereum Consensus Plugin
```go
type EthereumConsensusPlugin struct {
    // Consensus components
    beaconClient    beacon.Client      // Prysm beacon chain client
    executionClient execution.Client   // Geth execution client
    
    // Validator components
    validatorClient *validator.Client
    keystores       map[string]*keystore.Keystore
    
    // Cross-chain bridge
    bridgeAdapter   *BridgeAdapter
}
```

### 1.3 Architecture Diagram
```
┌─────────────────────────────────────────────────────────────┐
│                        Lux Multi-Consensus Node              │
├─────────────────────────────────────────────────────────────┤
│                      Consensus Manager                       │
├──────────────┬──────────────┬──────────────┬────────────────┤
│  Avalanche   │     Lux      │   Ethereum   │   L2 Chains   │
│  Consensus   │  Consensus   │  Consensus   │  (Future)     │
├──────────────┼──────────────┼──────────────┼────────────────┤
│  Snowman     │  Snowman+    │   Gasper     │   Various     │
│  P/C/X-Chain │  P/C/X/B     │  Beacon+Exec │  Optimistic/  │
│              │              │              │  ZK-Rollup    │
├──────────────┴──────────────┴──────────────┴────────────────┤
│                    Cross-Chain Sync Layer                    │
│                  (Transaction Propagation)                   │
└─────────────────────────────────────────────────────────────┘
```

## 2. Ethereum Integration Components

### 2.1 Beacon Chain Client Integration
```go
// pkg/consensus/ethereum/beacon.go
type BeaconChainClient struct {
    prysm       *prysm.BeaconClient
    chainID     uint64
    networkID   uint64
    
    // Validator management
    validators  map[string]*Validator
    duties      *ValidatorDuties
}

func (b *BeaconChainClient) Initialize(config *EthConfig) error {
    // Initialize Prysm client with custom configuration
    cfg := &prysm.Config{
        DataDir:        filepath.Join(config.DataDir, "ethereum", "beacon"),
        RPCHost:        config.BeaconRPCHost,
        ExecutionNode:  config.ExecutionEndpoint,
        NetworkConfig:  networks.MainnetConfig(), // or Holesky for testnet
    }
    
    b.prysm = prysm.NewBeaconClient(cfg)
    return b.prysm.Start()
}
```

### 2.2 Execution Layer Adapter
```go
// pkg/consensus/ethereum/execution.go
type ExecutionAdapter struct {
    engineAPI   *engine.Client
    ethClient   *ethclient.Client
    
    // State tracking
    latestBlock *types.Block
    stateDB     state.Database
}

// Implement Engine API for consensus-execution communication
func (e *ExecutionAdapter) ForkchoiceUpdated(state engine.ForkchoiceState) error {
    // Handle fork choice updates from consensus layer
}

func (e *ExecutionAdapter) GetPayload(payloadID engine.PayloadID) (*engine.ExecutableData, error) {
    // Build execution payload for block proposal
}
```

### 2.3 Validator Management
```go
type EthereumValidator struct {
    pubkey      []byte
    index       uint64
    balance     *big.Int
    
    // Key management
    keystore    *keystore.Keystore
    signer      types.Signer
    
    // Duties
    attestationSlot uint64
    proposerSlot    uint64
}
```

## 3. Cross-Chain Transaction Synchronization

### 3.1 Unified Transaction Pool
```go
type CrossChainTxPool struct {
    // Per-chain pools
    avalancheTxs map[ids.ID]*TxPool
    ethereumTxs  *EthTxPool
    luxTxs       map[ids.ID]*TxPool
    
    // Cross-chain pending
    crossChainTxs *PendingCrossChainTxs
    
    // Sync engine
    syncEngine *CrossChainSyncEngine
}

type CrossChainTx struct {
    SourceChain      ChainIdentifier
    DestinationChain ChainIdentifier
    SourceTx         []byte
    DestTx           []byte
    Proof            []byte
    Status           TxStatus
}
```

### 3.2 Cross-Chain Sync Protocol
```go
type CrossChainSyncEngine struct {
    // Chain monitors
    monitors map[ChainIdentifier]ChainMonitor
    
    // State verification
    stateVerifier *StateVerifier
    
    // Bridge components
    bridgeOracle  *BridgeOracle
    mpcSigner     *MPCSigner
}

func (s *CrossChainSyncEngine) SyncTransaction(tx CrossChainTx) error {
    // 1. Verify source transaction finality
    if err := s.verifySourceFinality(tx); err != nil {
        return err
    }
    
    // 2. Generate cross-chain proof
    proof, err := s.generateProof(tx)
    if err != nil {
        return err
    }
    
    // 3. Submit to destination chain
    return s.submitToDestination(tx, proof)
}
```

### 3.3 L2 Integration Framework
```go
type L2Plugin interface {
    ConsensusPlugin
    
    // L2-specific methods
    GetL1Contract() common.Address
    GetSequencer() common.Address
    VerifyStateRoot(root common.Hash) error
}

// Example: Arbitrum integration
type ArbitrumPlugin struct {
    l1Client     *ethclient.Client
    l2Client     *arbclient.Client
    rollupReader *arbbridge.RollupReader
}
```

## 4. Configuration Updates

### 4.1 Multi-Chain Configuration
```json
{
  "consensus": {
    "networks": [
      {
        "networkID": 1,
        "name": "Avalanche Mainnet",
        "type": "avalanche",
        "enabled": true,
        "config": {
          "minStake": 2000000000000
        }
      },
      {
        "networkID": 99,
        "name": "Lux Mainnet", 
        "type": "lux",
        "enabled": true,
        "config": {
          "minStake": 1000000000000,
          "totalSupply": 2000000000000000000000
        }
      },
      {
        "networkID": 1,
        "chainID": 1,
        "name": "Ethereum Mainnet",
        "type": "ethereum",
        "enabled": true,
        "config": {
          "beaconRPC": "http://localhost:5052",
          "executionRPC": "http://localhost:8545",
          "validatorKeys": "/path/to/validator/keys",
          "minBalance": 32000000000000000000
        }
      },
      {
        "networkID": 42161,
        "name": "Arbitrum One",
        "type": "l2-arbitrum",
        "enabled": false,
        "config": {
          "l1RPC": "http://localhost:8545",
          "l2RPC": "https://arb1.arbitrum.io/rpc"
        }
      }
    ]
  },
  "crossChain": {
    "enabled": true,
    "bridgeValidators": 100,
    "syncInterval": 2000,
    "finalityThresholds": {
      "avalanche": 1000,
      "ethereum": 1200000,
      "lux": 1000
    }
  }
}
```

## 5. Implementation Phases

### Phase 1: Ethereum Read-Only Integration (Weeks 1-3)
- Integrate Prysm beacon client in read-only mode
- Monitor Ethereum consensus without validation
- Build chain state tracking

### Phase 2: Ethereum Validation (Weeks 4-8)
- Add validator key management
- Implement attestation and block proposal
- Test on Holesky testnet

### Phase 3: Cross-Chain Sync Engine (Weeks 9-12)
- Build unified transaction pool
- Implement cross-chain proof generation
- Create sync protocol

### Phase 4: L2 Integration (Weeks 13-16)
- Add Arbitrum support
- Add Optimism support
- Create L2 plugin framework

### Phase 5: Advanced Features (Weeks 17-20)
- MEV protection across chains
- Cross-chain atomic swaps
- Unified liquidity management

## 6. Technical Challenges

### 6.1 Consensus Timing Differences
- Avalanche: ~2 second finality
- Ethereum: ~12-19 minute finality
- Solution: Asynchronous finality tracking with buffering

### 6.2 State Management
- Different state models (UTXO vs Account)
- Solution: Abstract state interface with adapters

### 6.3 Resource Management
- Running multiple consensus clients is resource-intensive
- Solution: Optimize with shared components, pruning strategies

### 6.4 Security Isolation
- Consensus mechanisms must be isolated
- Solution: Process isolation, separate key management

## 7. Cross-Chain Transaction Flow

```
1. User initiates cross-chain transaction on Avalanche
   ↓
2. Lux node detects transaction in Avalanche mempool
   ↓
3. Wait for Avalanche finality (~2 seconds)
   ↓
4. Generate SPV proof of transaction
   ↓
5. Submit proof to Ethereum via B-Chain bridge
   ↓
6. B-Chain validators sign attestation (MPC)
   ↓
7. Submit to Ethereum (wait ~12 minutes for finality)
   ↓
8. Confirm execution on Ethereum
   ↓
9. Update cross-chain state
```

## 8. Benefits

1. **Single Node Operation**: One node validates multiple chains
2. **Native Cross-Chain**: Direct transaction synchronization
3. **Reduced Infrastructure**: Lower operational costs
4. **Enhanced Security**: Shared security model
5. **MEV Opportunities**: Cross-chain MEV extraction
6. **Unified Liquidity**: Seamless asset movement

## 9. Next Steps

1. Set up Ethereum client integration branch
2. Create proof-of-concept with Prysm integration
3. Design cross-chain message format
4. Build testnet with all chains active
5. Develop monitoring and alerting systems