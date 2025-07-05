# B-Chain Dual MPC Architecture: CGGMP21 + Ringtail

## Overview

B-Chain implements global network-level consensus with dual MPC protocol support: CGGMP21 for ECDSA compatibility and Ringtail for post-quantum security, maintaining full compatibility with the existing bridge API.

## 1. Dual MPC Protocol Architecture

### 1.1 Protocol Selection
```go
// vms/bvm/mpc/protocol.go
package mpc

type MPCProtocol int

const (
    PROTOCOL_CGGMP21  MPCProtocol = iota  // ECDSA threshold signatures
    PROTOCOL_RINGTAIL                      // Post-quantum lattice signatures
    PROTOCOL_HYBRID                        // Both for transition period
)

type GlobalMPCManager struct {
    // Active protocols
    cggmp21     *CGGMP21Protocol
    ringtail    *RingtailProtocol
    
    // Global consensus state
    validators  map[ids.NodeID]*MPCValidator
    ceremonies  map[ids.ID]Ceremony
    
    // Bridge API compatibility
    bridgeAPI   *BridgeAPIAdapter
}

type MPCValidator struct {
    NodeID       ids.NodeID
    NFTId        uint64
    
    // Dual key shares
    ECDSAShare   *cggmp21.KeyShare
    RingtailShare *ringtail.KeyShare
    
    // Protocol preferences
    PreferredProtocol MPCProtocol
}
```

### 1.2 CGGMP21 Implementation (Upgrade from GG18)
```go
// vms/bvm/mpc/cggmp21/protocol.go
package cggmp21

import (
    "github.com/taurusgroup/frost-ed25519/pkg/frost"
    "github.com/luxfi/cggmp21-go"  // Our Go port of Rust implementation
)

type CGGMP21Protocol struct {
    // Threshold parameters
    threshold    int  // t
    participants int  // n
    
    // Key generation state
    keyGenState  *KeyGenState
    
    // Auxiliary protocols
    auxInfo      *AuxiliaryInfo  // New in CGGMP21
    
    // Presigning storage
    presigns     *PresignStorage
}

// Key generation following CGGMP21 paper
type KeyGenState struct {
    // Round 1: Commitment
    commitments  map[int]*PedersenCommitment
    
    // Round 2: VSS shares
    vssShares    map[int]*FeldmanVSS
    
    // Round 3: Auxiliary info generation (new in CGGMP21)
    paillierKeys map[int]*PaillierPublic
    ringPedersen map[int]*RingPedersenParams
    
    // Final shares
    shares       map[int]*ECDSAShare
}

// Improved key generation with auxiliary info
func (p *CGGMP21Protocol) KeyGen() (*KeyGenResult, error) {
    // Phase 1: Distributed key generation
    shares, pubKey := p.distributedKeyGen()
    
    // Phase 2: Auxiliary info generation (CGGMP21 improvement)
    auxInfo := p.generateAuxiliaryInfo()
    
    // Phase 3: Verification and storage
    result := &KeyGenResult{
        PublicKey: pubKey,
        Shares:    shares,
        AuxInfo:   auxInfo,
    }
    
    return result, nil
}

// Signing with presigning (major CGGMP21 improvement)
type SigningSession struct {
    sessionID    ids.ID
    message      []byte
    
    // Presigning phase (can be done offline)
    presignData  *PresignData
    
    // Online signing phase (very fast)
    partialSigs  map[int]*PartialSignature
}

func (p *CGGMP21Protocol) Sign(message []byte) (*ECDSASignature, error) {
    // Use pre-generated presign data (major speedup)
    presign := p.presigns.Get()
    
    // Online phase - just one round!
    session := &SigningSession{
        sessionID:   ids.GenerateID(),
        message:     message,
        presignData: presign,
    }
    
    // Single round of communication
    partialSigs := p.computePartialSignatures(session)
    
    // Combine to final signature
    signature := p.combineSignatures(partialSigs)
    
    return signature, nil
}
```

### 1.3 Bridge API Compatibility Layer
```go
// vms/bvm/bridge/api_adapter.go
package bridge

// Maintains compatibility with existing bridge API
type BridgeAPIAdapter struct {
    mpcManager *mpc.GlobalMPCManager
    
    // Legacy endpoints mapping
    endpoints  map[string]HandlerFunc
}

// Existing bridge API structure preserved
type BridgeRequest struct {
    Action      string          `json:"action"`
    Chain       string          `json:"chain"`
    TxHash      string          `json:"txHash"`
    Amount      string          `json:"amount"`
    Recipient   string          `json:"recipient"`
    TokenAddr   string          `json:"tokenAddress"`
    
    // New: Protocol selection
    Protocol    string          `json:"protocol,omitempty"`  // "ecdsa", "ringtail", "auto"
}

// Compatible with current bridge backend
func (api *BridgeAPIAdapter) HandleBridgeRequest(req *BridgeRequest) (*BridgeResponse, error) {
    // Determine protocol
    protocol := api.selectProtocol(req)
    
    // Create bridge transaction
    bridgeTx := &BridgeTx{
        SourceChain:   parseChain(req.Chain),
        SourceTxHash:  common.HexToHash(req.TxHash),
        Amount:        parseAmount(req.Amount),
        Recipient:     req.Recipient,
        TokenAddress:  common.HexToAddress(req.TokenAddr),
        Protocol:      protocol,
    }
    
    // Route to appropriate MPC protocol
    var signature []byte
    var err error
    
    switch protocol {
    case PROTOCOL_CGGMP21:
        signature, err = api.mpcManager.SignWithCGGMP21(bridgeTx)
    case PROTOCOL_RINGTAIL:
        signature, err = api.mpcManager.SignWithRingtail(bridgeTx)
    case PROTOCOL_HYBRID:
        signature, err = api.mpcManager.SignWithBoth(bridgeTx)
    }
    
    if err != nil {
        return nil, err
    }
    
    return &BridgeResponse{
        TxID:      bridgeTx.ID().String(),
        Signature: hex.EncodeToString(signature),
        Status:    "completed",
    }, nil
}
```

## 2. Global Network Consensus Integration

### 2.1 B-Chain Consensus Engine
```go
// vms/bvm/consensus/global_consensus.go
package consensus

type GlobalConsensus struct {
    // Avalanche consensus for B-Chain blocks
    snowman     snowman.Consensus
    
    // MPC consensus for signatures
    mpcConsensus *MPCConsensusEngine
    
    // Cross-chain state verification
    stateVerifier *StateVerifier
}

type MPCConsensusEngine struct {
    // Current validator set
    validators   *ValidatorSet
    
    // Active MPC ceremonies
    ceremonies   map[ids.ID]*MPCCeremony
    
    // Protocol metrics
    metrics      *MPCMetrics
}

// Consensus for MPC operations
func (mpc *MPCConsensusEngine) ProposeMPCOperation(op MPCOperation) error {
    ceremony := &MPCCeremony{
        ID:          ids.GenerateID(),
        Operation:   op,
        Proposer:    mpc.validators.Self(),
        StartTime:   time.Now(),
        Protocol:    op.PreferredProtocol(),
    }
    
    // Get 2/3+ validators to participate
    required := (len(mpc.validators) * 2) / 3 + 1
    
    // Initiate ceremony
    switch ceremony.Protocol {
    case PROTOCOL_CGGMP21:
        return mpc.initiateCGGMP21Ceremony(ceremony, required)
    case PROTOCOL_RINGTAIL:
        return mpc.initiateRingtailCeremony(ceremony, required)
    }
}
```

### 2.2 Global State Synchronization
```go
// vms/bvm/state/global_state.go
type GlobalBridgeState struct {
    // Network states
    networks    map[ids.ID]*NetworkState
    
    // Bridge operations
    operations  *OperationQueue
    
    // Liquidity tracking
    liquidity   map[common.Address]*big.Int
    
    // MPC key states
    keyStates   map[MPCProtocol]*KeyState
}

type NetworkState struct {
    ChainID         ids.ID
    LatestBlock     uint64
    BridgeContract  common.Address
    
    // Pending operations
    PendingOps      []*BridgeOperation
    
    // Network-specific configs
    Confirmations   uint64
    Protocol        MPCProtocol
}

// Synchronize across all connected networks
func (s *GlobalBridgeState) SyncNetworkStates() error {
    for chainID, network := range s.networks {
        // Update latest state
        latestBlock, err := network.GetLatestBlock()
        if err != nil {
            continue
        }
        
        // Process new events
        events, err := network.GetBridgeEvents(network.LatestBlock, latestBlock)
        if err != nil {
            continue
        }
        
        // Queue operations for MPC signing
        for _, event := range events {
            op := s.createBridgeOperation(event)
            s.operations.Add(op)
        }
        
        network.LatestBlock = latestBlock
    }
    
    return nil
}
```

## 3. Native Protocol Implementations

### 3.1 CGGMP21 Rust Integration
```go
// vms/bvm/mpc/cggmp21/rust_bridge.go
// #cgo LDFLAGS: -L${SRCDIR}/rust/target/release -lcggmp21
// #include "cggmp21.h"
import "C"

type RustCGGMP21 struct {
    handle unsafe.Pointer
}

// Call into Rust implementation for performance
func (r *RustCGGMP21) KeyGen(threshold, parties int) (*KeyGenResult, error) {
    result := C.cggmp21_keygen(
        C.uint32_t(threshold),
        C.uint32_t(parties),
    )
    
    if result.error != nil {
        return nil, errors.New(C.GoString(result.error))
    }
    
    return &KeyGenResult{
        PublicKey: C.GoBytes(result.public_key, C.int(result.pk_len)),
        Shares:    unmarshalShares(result.shares),
    }, nil
}

// Presigning in Rust for performance
func (r *RustCGGMP21) GeneratePresigns(count int) ([]*PresignData, error) {
    result := C.cggmp21_generate_presigns(
        r.handle,
        C.uint32_t(count),
    )
    
    return unmarshalPresigns(result), nil
}
```

### 3.2 Ringtail Native Implementation
```go
// vms/bvm/mpc/ringtail/native.go
package ringtail

type NativeRingtailMPC struct {
    params      *RingtailParams
    validators  map[ids.NodeID]*RingtailValidator
}

// Threshold key generation for Ringtail
func (r *NativeRingtailMPC) DistributedKeyGen() (*RingtailDKGResult, error) {
    // Phase 1: Each party generates polynomial
    polynomials := make(map[ids.NodeID]*Polynomial)
    for _, validator := range r.validators {
        poly := generatePolynomial(r.params.Threshold)
        polynomials[validator.NodeID] = poly
    }
    
    // Phase 2: Share distribution
    shares := distributeShares(polynomials, r.validators)
    
    // Phase 3: Verification
    if !verifyShares(shares, r.params) {
        return nil, errors.New("share verification failed")
    }
    
    // Phase 4: Combine for public key
    publicKey := combinePublicKeys(shares)
    
    return &RingtailDKGResult{
        PublicKey: publicKey,
        Shares:    shares,
    }, nil
}
```

## 4. Bridge Transaction Flow

### 4.1 Unified Bridge Transaction
```go
// vms/bvm/txs/bridge_transaction.go
type BridgeTransaction struct {
    // Transaction metadata
    ID           ids.ID
    SourceChain  ids.ID
    DestChain    ids.ID
    
    // Asset information
    Token        common.Address
    Amount       *big.Int
    Recipient    []byte
    
    // Dual signature support
    ECDSASig     *cggmp21.Signature   `json:"ecdsaSignature,omitempty"`
    RingtailSig  *ringtail.Signature  `json:"ringtailSignature,omitempty"`
    
    // Protocol selection
    Protocol     MPCProtocol
    
    // Consensus tracking
    Attestations map[ids.NodeID]*Attestation
}

// Process bridge transaction with selected protocol
func (tx *BridgeTransaction) Process(vm *VM) error {
    // Verify source transaction exists
    if !vm.verifySourceTx(tx.SourceChain, tx.ID) {
        return errInvalidSourceTx
    }
    
    // Check protocol requirements
    destRequirements := vm.getDestinationRequirements(tx.DestChain)
    if !tx.meetsRequirements(destRequirements) {
        return errInsufficientSecurity
    }
    
    // Initiate MPC signing
    ceremony := vm.mpcManager.CreateSigningCeremony(tx)
    
    // Wait for consensus
    signature, err := ceremony.Execute()
    if err != nil {
        return err
    }
    
    // Submit to destination chain
    return vm.submitToDestination(tx, signature)
}
```

### 4.2 Multi-Protocol Bridge Flow
```
┌─────────────────────────────────────────────────────────────┐
│                   Bridge Transaction Flow                    │
├─────────────────────────────────────────────────────────────┤
│  1. User initiates transfer on source chain                 │
│     └── Smart contract emits BridgeRequested event          │
│                                                              │
│  2. B-Chain validators detect event                         │
│     ├── Verify transaction on source chain                  │
│     └── Determine required protocol (ECDSA/Ringtail)        │
│                                                              │
│  3. MPC Ceremony Initiated                                  │
│     ├── CGGMP21: Use presigned data for speed              │
│     └── Ringtail: Full threshold signing                    │
│                                                              │
│  4. Collect Attestations (2/3+ validators)                  │
│     └── Each validator signs with their share               │
│                                                              │
│  5. Combine signatures                                       │
│     ├── CGGMP21: Combine partial ECDSA signatures          │
│     └── Ringtail: Combine lattice-based shares             │
│                                                              │
│  6. Submit to destination chain                              │
│     └── Bridge contract verifies and mints/releases tokens  │
└─────────────────────────────────────────────────────────────┘
```

## 5. Monitoring and Metrics

### 5.1 MPC Performance Metrics
```go
// vms/bvm/metrics/mpc_metrics.go
type MPCMetrics struct {
    // Protocol-specific metrics
    CGGMP21Stats  *ProtocolStats
    RingtailStats *ProtocolStats
    
    // Global metrics
    TotalCeremonies    prometheus.Counter
    SuccessfulSigns    prometheus.Counter
    FailedCeremonies   prometheus.Counter
    AverageSigningTime prometheus.Histogram
}

type ProtocolStats struct {
    KeyGenTime         prometheus.Histogram
    SigningTime        prometheus.Histogram
    PresignGenerations prometheus.Counter
    ActiveCeremonies   prometheus.Gauge
}
```

### 5.2 Real-time Monitoring Dashboard
```go
// API endpoints for monitoring
func (api *BridgeAPI) GetMPCStatus() (*MPCStatus, error) {
    return &MPCStatus{
        ActiveValidators: len(api.vm.validators),
        CGGMP21: &ProtocolStatus{
            KeysGenerated:    api.metrics.CGGMP21Stats.KeysGenerated,
            PresignsAvailable: api.vm.cggmp21.PresignCount(),
            AverageSignTime:  api.metrics.CGGMP21Stats.AvgSignTime(),
        },
        Ringtail: &ProtocolStatus{
            KeysGenerated:   api.metrics.RingtailStats.KeysGenerated,
            AverageSignTime: api.metrics.RingtailStats.AvgSignTime(),
        },
        PendingOperations: api.vm.operations.Pending(),
    }, nil
}
```

## 6. Security Considerations

### 6.1 Protocol Security Levels
- **CGGMP21**: 256-bit ECDSA security, improved over GG18 with:
  - Non-malleable commitments
  - Improved range proofs
  - Auxiliary info for faster signing
  
- **Ringtail**: 256-bit post-quantum security
  - Lattice-based assumptions
  - Resistant to quantum attacks

### 6.2 Key Rotation
```go
func (vm *VM) RotateKeys(protocol MPCProtocol) error {
    // Generate new keys
    newKeys, err := vm.mpcManager.GenerateKeys(protocol)
    if err != nil {
        return err
    }
    
    // Transition period: sign with both old and new
    vm.enterTransitionPeriod(newKeys)
    
    // After grace period, revoke old keys
    time.AfterFunc(24*time.Hour, func() {
        vm.revokeOldKeys()
    })
    
    return nil
}
```

This architecture ensures B-Chain maintains full compatibility with the existing bridge API while upgrading to CGGMP21 and adding Ringtail support for quantum safety.