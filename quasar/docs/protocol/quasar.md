# Quasar: Dual-Certificate Quantum Consensus

## Overview

**Quasar** is Lux Network's dual-certificate consensus engine that requires both BLS and Ringtail certificates for every block to achieve finality. This revolutionary approach provides immediate post-quantum security while maintaining sub-second performance through parallel verification and precomputation.

### Key Innovations

1. **Dual-Certificate Finality**: Every block requires both BLS and Ringtail certificates
   ```go
   isFinal := verifyBLS(blsAgg, quorum) && 
              verifyRT(rtCert, quorum) && 
              rtCert.round == blsAgg.round
   ```

2. **Unified RT Key**: One Ringtail key per validator secures all chains (Q, C, X, M)
   - Stored in `$HOME/.lux/rt.key`
   - Registered once on Q-Chain
   - Reused by all chain engines
   - No additional validator onboarding required

3. **Parallel Processing**: BLS on hot path, RT verification in parallel
   - Maintains sub-second finality
   - RT precompute hides ~90% of lattice cost
   - Verifications complete within one Snowman++ slot

### Performance

| Network | Nova Rounds | BLS Agg | RT Cert | Total Latency | Security |
|---------|-------------|---------|---------|---------------|----------|
| Mainnet | k rounds (300ms) | 50ms | 50ms | **<400ms** | Classical + Quantum |
| Testnet | k rounds (175ms) | 25ms | 25ms | **<225ms** | Classical + Quantum |
| Devnet | k rounds (40ms) | 5ms | 5ms | **<50ms** | Classical + Quantum |

Both certificates complete in parallel after Nova consensus, well within one Snowman++ slot (1-2s).

## How Quasar Works

### 1. Nova Foundation (Unchanged)
Quasar keeps Nova exactly as is:
- Run Avalanche DAG with vertex sampling
- Collect chits via k-peer queries
- Build confidence d(T) for every vertex
- Achieve probabilistic finality when d(T) > β

### 2. Dual-Certificate Generation
When Nova achieves confidence, generate both certificates in parallel:

#### BLS Certificate (Classical)
```go
// Standard BLS aggregation for classical finality
blsShare := bls.Sign(privateKey, blockHash)
blsShares := p2p.CollectBLSShares(blsShare, k)
blsAgg := bls.Aggregate(blsShares)
```

#### Ringtail Certificate (Quantum)
```go
// Phase I: Propose frontier with RT signature
frontier := nova.GetHighestConfidenceFrontier()
rtShare := ringtail.SignFromPrecompute(rtKey, frontier)
proposals := p2p.ExchangeRTShares(rtShare, k)

// Phase II: Commit if threshold reached
if countAgreement(proposals) > alphaCommit {
    rtCert := ringtail.Aggregate(proposals)
}
```

#### Dual-Certificate Block
```go
// Block requires BOTH certificates
block := Block{
    Header: BlockHeader{
        Height:    currentHeight,
        Timestamp: time.Now(),
        Certs: CertBundle{
            BLSAgg: blsAgg,      // Classical finality
            RTCert: rtCert,      // Quantum finality
        },
    },
    Transactions: txs,
}
// Block invalid if either certificate missing
```

### 3. Q-Chain Embedding
Q-blocks with dual certificates are embedded as internal transactions on Q-Chain:

```go
type QChainBlock struct {
    // Regular Q-Chain fields...
    Transactions []Tx
    
    // Dual-certificate finality
    QBlocks []QBlock // Quantum finality records
}

type QBlock struct {
    Height       uint64
    Frontier     []DAGVertex    // Nova vertices finalized
    CertBundle   CertBundle {   // Dual certificates
        BLSAgg   []byte         // Classical cert (96B)
        RTCert   []byte         // Quantum cert (~3KB)
    }
    ValidatorSet []ValidatorID  // Participating validators
}
```

## Why This Architecture Wins

### 1. Eliminates Snowman++ Complexity
- **Before**: Nova → Snowball → Snowman → P-Chain callback (4+ steps)
- **After**: Nova → Ringtail PQ → Done (2 steps)

### 2. True 2-Round Finality
```
T+0:   Transaction enters Nova DAG
T+1:   Nova builds confidence (existing k-sampling)
T+2:   Ringtail Phase I (propose frontier)
T+3:   Ringtail Phase II (commit frontier)
T+3ms: Q-block embedded in next P-Chain block
```

### 3. No More Chain-Specific Engines
Every chain uses the same Quasar protocol:
- **C-Chain**: No more Snowman++ callbacks
- **X-Chain**: Same Quasar engine
- **M-Chain**: Same Quasar engine
- **Z-Chain**: Same Quasar engine

### 4. Monotonic Security
Because Nova's confidence d(T) is monotonic (only increases), Ringtail's lattice structure guarantees:
- Can't go backward once committed
- No need for β rounds of re-confirmation
- 2 phases sufficient for irreversibility

## Unified Key Architecture

### Key Hierarchy

| Layer | Key Type | Purpose | Storage |
|-------|----------|---------|---------|
| Node ID | ed25519 | P2P transport auth | `$HOME/.lux/node.key` |
| Validator BLS | bls12-381 | Classical finality | `$HOME/.lux/bls.key` |
| **Validator RT** | **ringtail** | **Quantum finality** | **`$HOME/.lux/rt.key`** |
| User Wallet | Various | Transaction signing | User controlled |

### One RT Key, All Chains

The same Ringtail key registered on Q-Chain secures all chains:

```go
// Validator registers RT key once on Q-Chain
type RegisterRTKey struct {
    ValidatorID  ids.NodeID
    RTPublicKey  []byte      // Ringtail public key
    BLSPublicKey []byte      // BLS public key
    Signature    []byte      // Signed with node key
}

// All chains use the same RT key
func (c *CChain) GetValidatorRTKey(nodeID ids.NodeID) []byte {
    return c.qchain.GetRTKey(nodeID) // Query Q-Chain
}

func (x *XChain) GetValidatorRTKey(nodeID ids.NodeID) []byte {
    return x.qchain.GetRTKey(nodeID) // Same key
}
```

### Benefits

1. **No Extra Onboarding**: Validators register once, secure all chains
2. **Simplified Key Management**: One RT key file to backup
3. **Unified Security**: Compromise requires breaking single key
4. **Cross-Chain Consistency**: Same validator set across chains

## Implementation Details

### Validator Participation
```go
type QuasarValidator struct {
    nova      *NovaEngine
    bls       *BLSEngine
    ringtail  *RingtailPQ
    qchain    *QChainClient
    precompute *PrecomputePool
}

func (v *QuasarValidator) Run() {
    // Precompute RT shares continuously
    go v.precompute.Run()
    
    // When Nova signals confidence
    v.nova.OnHighConfidence(func(frontier []Vertex) {
        blockHash := computeHash(frontier)
        
        // Generate both certificates in parallel
        var wg sync.WaitGroup
        var blsAgg, rtCert []byte
        
        // BLS certificate (hot path)
        wg.Add(1)
        go func() {
            defer wg.Done()
            blsShare := v.bls.Sign(blockHash)
            blsAgg = v.collectBLSShares(blsShare)
        }()
        
        // RT certificate (parallel)
        wg.Add(1)
        go func() {
            defer wg.Done()
            rtShare := v.precompute.GetShare(blockHash)
            rtCert = v.collectRTShares(rtShare)
        }()
        
        wg.Wait()
        
        // Create dual-certificate Q-block
        qblock := QBlock{
            Frontier: frontier,
            CertBundle: CertBundle{
                BLSAgg: blsAgg,
                RTCert: rtCert,
            },
        }
        
        // Submit to Q-Chain
        v.qchain.SubmitQBlock(qblock)
    })
}
```

### C-Chain Integration

#### Extended Header (ExtraData)
C-Chain blocks include Q-block references in their extended header:

```go
// C-Chain block header with Q-block reference
type Header struct {
    // Standard Ethereum fields
    ParentHash  common.Hash
    Number      *big.Int
    GasLimit    uint64
    // ... other fields ...
    
    // Extended header for Quasar
    Extra       []byte  // Contains encoded QBlockReference
}

type QBlockReference struct {
    QBlockID     ids.ID    // Q-block on P-Chain
    QBlockHeight uint64    // P-Chain height
    FrontierRoot [32]byte  // Nova frontier root
    Certificate  []byte    // Ringtail certificate (optional)
}

func (h *Header) SetQBlockRef(ref QBlockReference) {
    h.Extra = ref.Encode()
}

func (h *Header) GetQBlockRef() (*QBlockReference, error) {
    return DecodeQBlockRef(h.Extra)
}
```

#### Block Production
```go
// New Quasar-based block production
func (c *CChain) ProduceBlock() *Block {
    // Get latest Q-block from P-Chain
    latestQBlock := c.pchain.GetLatestQBlock()
    
    // Create block header with Q-block reference
    header := &Header{
        Number: new(big.Int).Add(parent.Number, common.Big1),
        // ... other fields ...
    }
    
    // Embed Q-block reference in extra data
    header.SetQBlockRef(QBlockReference{
        QBlockID:     latestQBlock.ID,
        QBlockHeight: latestQBlock.Height,
        FrontierRoot: latestQBlock.FrontierRoot,
    })
    
    return NewBlock(header, txs)
}
```

#### Consensus Flow
```go
// C-Chain consensus with dual-certificate Quasar
func (c *CChain) ProcessConsensus(block *Block) error {
    // 1. Run Nova DAG consensus (unchanged)
    vertex := c.nova.AddBlock(block)
    confidence := c.nova.GetConfidence(vertex)
    
    // 2. When Nova reaches confidence threshold
    if confidence > c.params.Beta {
        // Generate dual certificates
        certBundle := c.generateDualCertificates(block)
        
        // 3. Embed certificates in block header
        block.Header().CertBundle = certBundle
        
        // 4. Wait for Q-block on Q-Chain
        qblock := c.WaitForQBlock(vertex, certBundle)
        
        // 5. Once Q-block appears with both certs, block is final
        if qblock.VerifyDualCerts() {
            block.SetFinalized(true)
        }
    }
    
    return nil
}

// Verify dual-certificate finality for a block
func (c *CChain) VerifyDualCertificateFinality(block *Block) bool {
    // 1. Check block has both certificates
    certs := block.Header().CertBundle
    if len(certs.BLSAgg) == 0 || len(certs.RTCert) == 0 {
        return false // Missing certificates
    }
    
    // 2. Verify BLS certificate
    if !c.verifyBLS(certs.BLSAgg, block.Hash()) {
        return false // Invalid BLS
    }
    
    // 3. Verify RT certificate (parallel)
    if !c.verifyRT(certs.RTCert, block.Hash()) {
        return false // Invalid RT
    }
    
    // 4. Verify Q-block exists on Q-Chain
    qref, _ := block.Header().GetQBlockRef()
    qchainQBlock := c.qchain.GetQBlock(qref.QBlockID)
    if qchainQBlock == nil {
        return false // Q-block not yet on Q-Chain
    }
    
    // 5. Verify Q-block has matching dual certificates
    return qchainQBlock.CertBundle.Equals(certs)
}
```

### Cross-Chain Finality
All chains verify finality by checking Q-Chain for dual-certificate Q-blocks:

```go
// Any chain can determine finality by watching Q-Chain
func (chain *AnyChain) IsFinalized(txID TxID) bool {
    // Find which Q-block contains this tx
    qblock := chain.qchain.GetQBlockContaining(txID)
    
    // If Q-block exists with dual certificates, tx is final
    return qblock != nil && qblock.HasDualCertificates()
}

// Verify both certificates are present and valid
func (q *QBlock) HasDualCertificates() bool {
    return len(q.CertBundle.BLSAgg) > 0 && 
           len(q.CertBundle.RTCert) > 0 &&
           q.VerifiedBLS && q.VerifiedRT
}
```

## Performance Analysis

### Latency Breakdown

| Component | Avalanche (Snowman++) | Lux (Quasar) | Improvement |
|-----------|----------------------|--------------|-------------|
| DAG confidence | k rounds | k rounds | Same |
| Linearization | +1 round | 0 | -1 round |
| P-Chain callback | +1 round | 0 | -1 round |
| Quantum security | N/A | +2 rounds | +2 rounds |
| **Total** | k+2 rounds | k+2 rounds | Quantum for free! |

### Key Insight
Quasar achieves quantum security in the same number of rounds as Snowman++ classical finality!

## Configuration

### Consensus Parameters
```go
type QuasarParams struct {
    // Nova parameters (unchanged)
    K               int   // Sample size
    Beta1           int   // Early commit threshold
    Beta2           int   // Finality threshold
    
    // Ringtail parameters (new)
    AlphaPropose    int   // Phase I threshold
    AlphaCommit     int   // Phase II threshold
    
    // Q-Chain parameters
    QBlockInterval  time.Duration // How often to create Q-blocks
    EmbedInPChain   bool         // Whether to embed in P-Chain
}

// Mainnet defaults
var MainnetQuasar = QuasarParams{
    K:              21,
    Beta1:          11,
    Beta2:          18,
    AlphaPropose:   13,
    AlphaCommit:    18,
    QBlockInterval: 100 * time.Millisecond,
    EmbedInPChain:  true,
}
```

## Migration from Snowman++

### Phase 1: Parallel Operation
1. Run Quasar alongside existing Snowman++
2. Compare finality times and security
3. Validate Q-blocks match Snowman decisions

### Phase 2: Gradual Cutover
1. C-Chain starts trusting Quasar finality
2. Remove Snowball wrap layer
3. Remove Snowman linearizer

### Phase 3: Full Quasar
1. All chains use Quasar exclusively
2. Deprecate Snowman++ code
3. Celebrate 2-round quantum finality!

## Summary: The Photonic Path

Quasar completes Lux's photonic consensus journey:

1. **Photon**: Transactions enter as light particles
2. **Wave**: K-sampling creates interference patterns  
3. **Nova**: Confidence explodes like a supernova
4. **Quasar**: Ringtail PQ focuses the energy into a tight beam
5. **Q-Chain**: The beam is recorded permanently on P-Chain

Result: Every chain in the Lux Network achieves both classical and quantum finality in just 2 rounds beyond Nova—making it the fastest, most secure consensus protocol ever created.

## Technical Deep Dive

### Why Ringtail Works on Top of Nova

Nova provides three critical properties that make Ringtail's 2-phase protocol sufficient:

1. **Monotonicity**: Confidence d(T) only increases
2. **Metastability**: Once high confidence, extremely unlikely to revert
3. **Network-wide convergence**: All honest nodes see same frontier

These properties mean Ringtail doesn't need multiple rounds—the underlying Nova has already done the hard work of achieving agreement.

### Lattice Structure

The Ringtail lattice leverages Nova's DAG structure:
```
         Q[n+1]
        /  |  \
       /   |   \
    F[a]  F[b]  F[c]  <- Nova frontiers
      \    |    /
       \   |   /
         Q[n]
```

Where:
- Q[n] = Previous Q-block
- F[a,b,c] = Possible frontiers with high confidence
- Q[n+1] = New Q-block selecting winning frontier

The lattice property ensures that once Q[n+1] commits to F[b], all future Q-blocks must build on F[b] or its descendants.

### Security Analysis

#### Classical Security
- Nova provides 10^-9 safety with k=21, β=18
- Ringtail preserves this safety (doesn't weaken Nova)
- 2-phase commit adds deterministic finality on top

#### Quantum Security  
- Ringtail signatures are lattice-based (post-quantum)
- Certificate size: ~2KB per Q-block
- Verification time: <1ms on modern hardware

#### Combined Guarantee
A transaction is final when:
1. Its vertex has d(T) > β (Nova)
2. A Q-block includes it (Ringtail Phase II)
3. The Q-block appears on P-Chain

This provides both probabilistic (Nova) and deterministic (Quasar) finality with post-quantum security.

## Quasar Service Architecture

### Precompute Pool
```go
type PrecomputePool struct {
    rtKey    *ringtail.PrivateKey
    shares   chan *RTShare
    workers  int
}

func (p *PrecomputePool) Run() {
    // Continuously generate RT shares in background
    for i := 0; i < p.workers; i++ {
        go func() {
            for {
                share := ringtail.Precompute(p.rtKey)
                p.shares <- share // Buffer 20-50 shares
            }
        }()
    }
}

func (p *PrecomputePool) GetShare(blockHash []byte) *RTShare {
    share := <-p.shares
    share.Bind(blockHash) // Bind to specific block
    return share
}
```

### RT Aggregator
```go
type RTAggregator struct {
    threshold int
    timeout   time.Duration
}

func (a *RTAggregator) CollectAndAggregate(myShare *RTShare) ([]byte, error) {
    shares := make([]*RTShare, 0, a.threshold)
    shares = append(shares, myShare)
    
    // Collect shares from peers
    for len(shares) < a.threshold {
        select {
        case share := <-a.incomingShares:
            if share.Verify() {
                shares = append(shares, share)
            }
        case <-time.After(a.timeout):
            return nil, ErrTimeout
        }
    }
    
    // Aggregate into certificate
    return ringtail.Aggregate(shares), nil
}
```

### Performance Optimization
- **Precompute**: ~90% of lattice operations done offline
- **Parallel Verification**: BLS and RT verified concurrently
- **Share Caching**: 20-50 precomputed shares ready in RAM
- **Fast Aggregation**: < 10ms to combine threshold shares

## Smart Contract Integration

### Quasar Precompiled Contract

The Quasar precompile at `0x0100000000000000000000000000000000000001F` verifies Q-block references:

```solidity
interface IQuasar {
    // Get Q-block reference from current block's extended header
    function getCurrentQBlockRef() external view returns (
        bytes32 qBlockId,
        uint256 qBlockHeight,
        bytes32 frontierRoot,
        bool hasQuantumFinality
    );
    
    // Verify a C-Chain block has quantum finality
    function isBlockQuantumFinalized(uint256 blockNumber) external view returns (bool);
    
    // Check if a transaction is in a quantum-finalized block
    function isTxQuantumFinalized(bytes32 txHash) external view returns (bool);
    
    // Get the Q-block info from P-Chain
    function getQBlockFromPChain(bytes32 qBlockId) external view returns (
        uint256 height,
        bytes32 frontierRoot,
        uint256 timestamp,
        bytes ringtailCert
    );
    
    // Verify a Ringtail certificate
    function verifyQuantumCert(bytes cert, bytes32 root) external view returns (bool);
    
    // Get blocks since last quantum finality
    function getBlocksSinceQuantumFinality() external view returns (uint256);
}
```

### Usage Examples

#### High-Value Transfer
```solidity
contract SecureVault {
    IQuasar constant quasar = IQuasar(0x0100000000000000000000000000000000000001F);
    
    function withdraw(uint256 amount) external {
        require(amount > 10000 ether, "Use standard withdraw");
        
        // Check current block's quantum finality via extended header
        (,,, bool hasQuantum) = quasar.getCurrentQBlockRef();
        require(hasQuantum, "Block not quantum finalized");
        
        // Additional safety: ensure we're not too far from last quantum block
        require(quasar.getBlocksSinceQuantumFinality() < 5, "Too far from quantum finality");
        
        // Execute withdrawal
        payable(msg.sender).transfer(amount);
    }
}
```

#### Bridge Security with Extended Header
```solidity
contract QuantumBridge {
    IQuasar constant quasar = IQuasar(0x0100000000000000000000000000000000000001F);
    
    function releaseFunds(uint256 amount, uint256 sourceBlock) external {
        // Verify the source block has quantum finality
        require(quasar.isBlockQuantumFinalized(sourceBlock), "Source block not quantum final");
        
        // Get Q-block reference from that block's extended header
        (bytes32 qBlockId,,,) = quasar.getCurrentQBlockRef();
        
        // Verify the Q-block exists on P-Chain
        (uint256 pchainHeight,,, bytes memory cert) = quasar.getQBlockFromPChain(qBlockId);
        require(pchainHeight > 0, "Q-block not found on P-Chain");
        
        // Verify quantum certificate
        require(quasar.verifyQuantumCert(cert, qBlockId), "Invalid quantum certificate");
        
        // Release funds
        _executeBridgeTransfer(amount);
    }
}
```

#### DEX with Quantum Finality Check
```solidity
contract QuantumDEX {
    IQuasar constant quasar = IQuasar(0x0100000000000000000000000000000000000001F);
    
    modifier quantumRequired() {
        // Read Q-block reference from current block's extended header
        (bytes32 qBlockId, uint256 qHeight,, bool hasQuantum) = quasar.getCurrentQBlockRef();
        
        if (!hasQuantum) {
            // Fallback: wait for more confirmations
            require(block.number > lastTradeBlock + 50, "Need 50 block confirmations without quantum");
        }
        _;
    }
    
    function executeLargeTrade(uint256 amount) external quantumRequired {
        // Trade is protected by quantum finality from extended header
        _performTrade(amount);
    }
}
```

## Account-Level Post-Quantum Options

### EVM/C-Chain: Lamport-XMSS Multisig

Users can opt into quantum security for their accounts:

```solidity
// Lamport multisig contract at standard address
contract LamportMultisig {
    uint256 constant VERIFY_LAMPORT_GAS = 300_000;
    
    // Register Lamport public key for account
    function registerLamportKey(
        bytes32 lamportPubKeyHash,
        bytes32 merkleRoot
    ) external {
        lamportKeys[msg.sender] = LamportKey({
            pubKeyHash: lamportPubKeyHash,
            merkleRoot: merkleRoot,
            isActive: true
        });
    }
    
    // Execute transaction with Lamport signature
    function executeWithLamport(
        address to,
        uint256 value,
        bytes calldata data,
        bytes calldata lamportSig
    ) external {
        require(verifyLamport(
            keccak256(abi.encode(to, value, data, nonce[msg.sender])),
            lamportSig,
            lamportKeys[msg.sender]
        ), "Invalid Lamport signature");
        
        (bool success,) = to.call{value: value}(data);
        require(success, "Transaction failed");
        nonce[msg.sender]++;
    }
}
```

### X-Chain: Native Ringtail UTXOs

X-Chain supports quantum-secure outputs natively:

```go
// New UTXO type for quantum security
type RingtailOutput struct {
    OutputType  string  // "RINGTAIL"
    PublicKey   []byte  // Ringtail public key
    Amount      uint64
    Locktime    uint64
}

// Spending requires Ringtail signature
type RingtailInput struct {
    OutputID    ids.ID
    Signature   []byte  // ~1.8KB Ringtail signature
}

// Wallet commands
lux-wallet generate --pq           // Generate RT keypair
lux-wallet send --pq-sign         // Create RT-locked UTXO
lux-wallet sweep --upgrade-to-pq  // Convert all UTXOs to RT
```

### M-Chain: MPC with PQ Fallback

M-Chain custody operations can require PQ approval:

```go
type MPCOperation struct {
    Type        string
    Amount      uint64
    Signers     []Signer
    PQRequired  bool     // Require RT signature for high value
    RTSignature []byte   // Optional RT signature
}

func (m *MChain) ValidateMPCOp(op MPCOperation) error {
    // Standard MPC threshold check
    if !m.checkMPCThreshold(op.Signers) {
        return ErrInsufficientSigners
    }
    
    // Additional PQ check for high-value ops
    if op.PQRequired && len(op.RTSignature) == 0 {
        return ErrMissingQuantumSignature
    }
    
    if len(op.RTSignature) > 0 {
        if !ringtail.Verify(op.RTSignature, op.Hash()) {
            return ErrInvalidQuantumSignature
        }
    }
    
    return nil
}
```

### Migration Timeline

| Phase | EVM/C-Chain | X-Chain | M-Chain |
|-------|-------------|---------|---------|
| 0 (Now) | Lamport optional | RT UTXOs optional | PQ optional |
| 1 (6mo) | Dual-sig for contracts | RT required >10 LUX | PQ for >100K |
| 2 (1yr) | PQ mandatory >1K LUX | RT mandatory | PQ mandatory |
| 3 (2yr) | Remove ECDSA | Remove secp256k1 | PQ only |

## Future Enhancements

### 1. Adaptive Parameters
- Adjust α thresholds based on network conditions
- Optimize k-sampling for current validator set
- Dynamic Q-block intervals

### 2. Compression
- Aggregate multiple frontiers per Q-block
- Compress Ringtail certificates with BLS
- Deduplicate cross-chain data

### 3. Hardware Acceleration
- GPU acceleration for lattice operations
- FPGA Ringtail signature verification
- Dedicated Quasar ASICs for validators

### 4. Cross-Network Bridges
- Export Q-blocks to other chains
- Universal finality proofs
- Quantum-secure interoperability

## Conclusion

Quasar represents a fundamental breakthrough in consensus design. By requiring both BLS and Ringtail certificates for every block, we achieve the impossible: consensus that is simultaneously classical AND quantum secure, without sacrificing performance.

Key achievements:
- **Dual-Certificate Finality**: Every block requires both BLS and RT certificates
- **Unified Security**: One RT key per validator secures all chains
- **Maintained Performance**: Sub-second finality through parallel verification
- **Account-Level Options**: Users can opt into PQ security gradually
- **Future-Proof**: Quantum adversary must break both schemes in <50ms

The photonic journey is complete:
- **Photon**: Transactions enter as light
- **Wave**: K-sampling creates interference patterns
- **Nova**: Confidence explodes like a supernova
- **Quasar**: Dual certificates (BLS + RT) focus into quantum beam
- **Q-Chain**: The beam is permanently recorded

Welcome to the age of dual-certificate quantum consensus. Welcome to Quasar.