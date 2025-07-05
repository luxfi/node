# B-Chain (Bridge Chain) Implementation Plan

## Overview
This document outlines the step-by-step implementation plan for integrating the existing bridge infrastructure as a native B-Chain in the Lux node.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                         Lux Node                             │
├─────────────────────────────────────────────────────────────┤
│  P-Chain  │  X-Chain  │  C-Chain  │     B-Chain (New)      │
│ Platform  │   UTXO    │    EVM    │  Bridge Operations      │
├───────────┴───────────┴───────────┴─────────────────────────┤
│                    Shared Components                         │
│              (Networking, Database, Consensus)               │
└─────────────────────────────────────────────────────────────┘

B-Chain Components:
┌─────────────────────────────────────────────────────────────┐
│                         B-Chain VM                           │
├─────────────────────────────────────────────────────────────┤
│  MPC Engine  │  Chain Monitor │  NFT Validators │  State DB │
├──────────────┴────────────────┴─────────────────┴───────────┤
│                    Bridge Operations                         │
│  • Cross-chain transfers    • Validator management          │
│  • MPC key ceremonies       • Fee distribution              │
└─────────────────────────────────────────────────────────────┘
```

## Phase 1: Foundation (Weeks 1-2)

### 1.1 Create B-Chain VM Structure
```bash
# Directory structure
/node/vms/bvm/
├── vm.go                 # Main VM implementation
├── factory.go            # VM factory
├── config.go             # Configuration
├── state/                # State management
│   ├── state.go
│   └── statedb.go
├── block/                # Block structure
│   ├── block.go
│   └── builder.go
├── txs/                  # Transaction types
│   ├── bridge_tx.go
│   ├── validator_rotation_tx.go
│   └── fee_config_tx.go
└── mpc/                  # MPC components (ported)
    ├── manager.go
    ├── signer.go
    └── ceremony.go
```

### 1.2 Port MPC Components from TypeScript to Go
```go
// vms/bvm/mpc/manager.go
package mpc

import (
    "github.com/luxfi/node/ids"
    "github.com/luxfi/node/snow/consensus/snowman"
)

type Manager struct {
    threshold    int
    participants map[ids.NodeID]*Participant
    ceremonies   map[ids.ID]*KeyGenCeremony
}

type Participant struct {
    nodeID    ids.NodeID
    publicKey []byte
    share     *KeyShare
}

type KeyGenCeremony struct {
    id           ids.ID
    participants []ids.NodeID
    round        int
    messages     map[int][]Message
}
```

### 1.3 Implement Basic VM Interface
```go
// vms/bvm/vm.go
package bvm

import (
    "github.com/luxfi/node/database"
    "github.com/luxfi/node/snow"
    "github.com/luxfi/node/snow/engine/snowman/block"
    "github.com/luxfi/node/vms/bvm/mpc"
)

type VM struct {
    ctx         *snow.Context
    db          database.Database
    
    // Bridge components
    mpcManager  *mpc.Manager
    validators  *ValidatorSet
    
    // State
    state       State
    mempool     *Mempool
    
    // Consensus
    preferred   ids.ID
    lastAccepted ids.ID
}

func (vm *VM) Initialize(
    ctx context.Context,
    snowCtx *snow.Context,
    db database.Database,
    genesisBytes []byte,
    upgradeBytes []byte,
    configBytes []byte,
    toEngine chan<- common.Message,
    _ []*common.Fx,
    _ common.AppSender,
) error {
    vm.ctx = snowCtx
    vm.db = db
    
    // Initialize MPC manager
    vm.mpcManager = mpc.NewManager(threshold: 67) // 67 of 100
    
    // Initialize validators from genesis
    return vm.initializeValidators(genesisBytes)
}
```

## Phase 2: Validator NFT System (Weeks 3-4)

### 2.1 NFT Validator Contract (C-Chain)
```solidity
// contracts/BridgeValidatorNFT.sol
contract BridgeValidatorNFT is ERC721, AccessControl {
    uint256 public constant MAX_VALIDATORS = 100;
    bytes32 public constant ADMIN_ROLE = keccak256("ADMIN_ROLE");
    
    struct ValidatorInfo {
        address owner;
        bytes publicKey;
        uint256 stake;
        bool active;
        uint256 lastRotation;
    }
    
    mapping(uint256 => ValidatorInfo) public validators;
    mapping(address => uint256) public ownerToNFT;
    
    event ValidatorTransferred(
        uint256 indexed tokenId,
        address indexed from,
        address indexed to,
        bytes newPublicKey
    );
    
    event KeyRotationInitiated(
        uint256 indexed tokenId,
        bytes oldKey,
        bytes newKey
    );
    
    function transferWithRotation(
        uint256 tokenId,
        address to,
        bytes calldata newPublicKey
    ) external {
        require(ownerOf(tokenId) == msg.sender, "Not owner");
        require(validators[tokenId].active, "Validator not active");
        
        // Initiate key rotation on B-Chain
        _initiateKeyRotation(tokenId, validators[tokenId].publicKey, newPublicKey);
        
        // Transfer NFT
        _transfer(msg.sender, to, tokenId);
        
        // Update validator info
        validators[tokenId].owner = to;
        validators[tokenId].publicKey = newPublicKey;
        validators[tokenId].lastRotation = block.timestamp;
        
        emit ValidatorTransferred(tokenId, msg.sender, to, newPublicKey);
    }
}
```

### 2.2 B-Chain Validator Management
```go
// vms/bvm/validators.go
type ValidatorSet struct {
    nftContract common.Address
    validators  map[uint64]*BridgeValidator
    
    // Rotation tracking
    pendingRotations map[uint64]*KeyRotation
}

type BridgeValidator struct {
    NFTID       uint64
    Owner       common.Address
    NodeID      ids.NodeID
    PublicKey   []byte
    MPCShare    *mpc.KeyShare
    Active      bool
}

type KeyRotation struct {
    NFTID       uint64
    OldKey      []byte
    NewKey      []byte
    NewOwner    common.Address
    Signatures  map[ids.NodeID][]byte
    StartTime   time.Time
}

func (v *ValidatorSet) InitiateRotation(nftID uint64, newOwner common.Address, newKey []byte) error {
    rotation := &KeyRotation{
        NFTID:    nftID,
        OldKey:   v.validators[nftID].PublicKey,
        NewKey:   newKey,
        NewOwner: newOwner,
        StartTime: time.Now(),
    }
    
    v.pendingRotations[nftID] = rotation
    
    // Trigger MPC key generation ceremony
    return v.startKeyGenCeremony(rotation)
}
```

## Phase 3: Bridge Operations (Weeks 5-6)

### 3.1 Bridge Transaction Types
```go
// vms/bvm/txs/bridge_tx.go
type BridgeTx struct {
    // Source chain info
    SourceChain   ids.ID
    SourceTxID    ids.ID
    SourceAddress string
    
    // Destination info
    DestChain     ids.ID
    DestAddress   string
    
    // Asset info
    TokenAddress  common.Address
    Amount        *big.Int
    
    // Validation
    MPCSignature  []byte
    Nonce         uint64
}

func (tx *BridgeTx) Verify() error {
    // Verify MPC signature
    // Verify source transaction exists
    // Check nonce for replay protection
    return nil
}
```

### 3.2 Chain Monitoring Integration
```go
// vms/bvm/monitor/monitor.go
type ChainMonitor struct {
    vm          *VM
    chains      map[ids.ID]ChainClient
    eventQueues map[ids.ID]*EventQueue
}

type ChainClient interface {
    GetLatestBlock() (uint64, error)
    GetEvents(from, to uint64, filter EventFilter) ([]Event, error)
    VerifyTransaction(txID ids.ID) (bool, error)
}

// Monitor Ethereum
type EthereumClient struct {
    client *ethclient.Client
    bridge common.Address
}

// Monitor Avalanche C-Chain
type CChainClient struct {
    client *avalancheclient.Client
    bridge common.Address
}
```

## Phase 4: State Management (Weeks 7-8)

### 4.1 B-Chain State Structure
```go
// vms/bvm/state/state.go
type State interface {
    // Validator operations
    GetValidator(nftID uint64) (*BridgeValidator, error)
    SetValidator(validator *BridgeValidator) error
    
    // Bridge operations
    GetBridgeTransaction(id ids.ID) (*BridgeTx, error)
    AddBridgeTransaction(tx *BridgeTx) error
    
    // Nonce tracking
    GetNonce(chain ids.ID, address string) (uint64, error)
    IncrementNonce(chain ids.ID, address string) error
    
    // Fee tracking
    GetCollectedFees() (*big.Int, error)
    AddFees(amount *big.Int) error
}
```

### 4.2 Block Structure
```go
// vms/bvm/block/block.go
type Block struct {
    PrntID ids.ID        `serialize:"true"`
    Hght   uint64        `serialize:"true"`
    Tmstmp int64         `serialize:"true"`
    
    // Bridge transactions
    BridgeTxs []*BridgeTx `serialize:"true"`
    
    // Validator updates
    ValidatorUpdates []*ValidatorUpdate `serialize:"true"`
    
    // MPC ceremonies
    Ceremonies []*mpc.CeremonyResult `serialize:"true"`
    
    id     ids.ID
    bytes  []byte
    status choices.Status
    vm     *VM
}

func (b *Block) Verify(context.Context) error {
    // Verify all bridge transactions
    // Verify validator updates
    // Verify MPC ceremony results
    return nil
}
```

## Phase 5: API and Integration (Weeks 9-10)

### 5.1 B-Chain API
```go
// vms/bvm/api.go
type API struct {
    vm *VM
}

// Bridge status
func (api *API) GetBridgeStatus() (*BridgeStatus, error) {
    return &BridgeStatus{
        ActiveValidators: len(api.vm.validators.GetActive()),
        PendingTxs:       api.vm.mempool.Len(),
        TotalBridged:     api.vm.state.GetTotalBridged(),
    }, nil
}

// Submit bridge transaction
func (api *API) Bridge(args *BridgeArgs) (*BridgeResponse, error) {
    tx := &BridgeTx{
        SourceChain:  args.SourceChain,
        SourceTxID:   args.SourceTxID,
        DestChain:    args.DestChain,
        DestAddress:  args.DestAddress,
        TokenAddress: args.TokenAddress,
        Amount:       args.Amount,
    }
    
    // Verify and add to mempool
    if err := api.vm.mempool.Add(tx); err != nil {
        return nil, err
    }
    
    return &BridgeResponse{
        TxID: tx.ID(),
        Status: "pending",
    }, nil
}
```

### 5.2 Cross-Chain Communication
```go
// Enable native communication with other chains
func (vm *VM) SendCrossChainMessage(
    destinationChain ids.ID,
    message []byte,
) error {
    // Use Avalanche Warp for native cross-chain messaging
    return vm.ctx.SendWarpMessage(destinationChain, message)
}
```

## Phase 6: Testing and Deployment (Weeks 11-12)

### 6.1 Test Suite
```go
// vms/bvm/vm_test.go
func TestBridgeOperations(t *testing.T) {
    // Test bridge transaction validation
    // Test validator rotation
    // Test MPC key generation
    // Test fee distribution
}

func TestValidatorNFTIntegration(t *testing.T) {
    // Test NFT transfer triggers rotation
    // Test key ceremony completion
    // Test validator handoff
}
```

### 6.2 Deployment Steps
1. Deploy validator NFT contract on C-Chain
2. Initialize genesis validators
3. Deploy B-Chain subnet
4. Migrate bridge liquidity
5. Enable cross-chain messaging

## Migration Strategy

### From Current Bridge to B-Chain
1. **Parallel Operation**: Run both systems initially
2. **Gradual Migration**: Move liquidity in phases
3. **Validator Transition**: Convert MPC nodes to B-Chain validators
4. **Deprecation**: Sunset old bridge after full migration

## Security Considerations

1. **Key Rotation Security**: 
   - Time-locked rotations
   - Multi-sig approval for critical operations

2. **Bridge Security**:
   - Rate limiting
   - Anomaly detection
   - Emergency pause mechanism

3. **Validator Security**:
   - Slashing for misbehavior
   - Minimum stake requirements
   - Regular key rotation

## Performance Targets

- Block time: 2 seconds (same as C-Chain)
- Transaction throughput: 1000+ bridge operations/second
- Finality: Sub-second with Avalanche consensus
- Key rotation: Complete within 10 minutes

## Monitoring and Maintenance

1. **Metrics to Track**:
   - Bridge volume per chain
   - Validator participation rates
   - Key rotation success rates
   - Cross-chain message latency

2. **Alerts**:
   - Failed bridge transactions
   - Validator offline
   - Key rotation failures
   - Unusual activity patterns

This implementation plan provides a clear path to integrating the bridge as a native B-Chain in the Lux node, with enhanced security, performance, and decentralization through the NFT validator system.