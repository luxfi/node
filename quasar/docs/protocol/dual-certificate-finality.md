# Dual-Certificate Finality: BLS + Ringtail Integration

## Overview

Lux Network implements **dual-certificate finality**, requiring both BLS and Ringtail certificates for every block to be considered final. This architecture provides immediate post-quantum security while maintaining sub-second finality.

## Key Innovation

**Every block is final only when both certificates are present:**
```go
isFinal := verifyBLS(blsAgg, quorum) && 
           verifyRT(rtCert, quorum) && 
           rtCert.round == blsAgg.round
```

## Architecture

### Key Hierarchy

| Layer | Key-pair | Purpose | Storage |
|-------|----------|---------|---------|
| Node-ID | ed25519 (unchanged) | P2P transport auth | `$HOME/.lux/node.key` |
| Validator-BLS | bls12-381 | Fast finality votes | `$HOME/.lux/bls.key` |
| **Validator-RT** | **ringtail-lattice** | **PQ finality shares (P, X, M chains)** | **`$HOME/.lux/rt.key`** |
| Wallet (EVM) | secp256k1 or Lamport XMSS | User tx signatures | In wallet |
| Wallet (X-Chain) | secp256k1 or Ringtail pubkey | UTXO locking | In wallet |

**Key insight**: The same `rt.key` registered on P-Chain is reused by all chains (C, X, M), no extra validator onboarding.

### Certificate Bundle

Every block header now contains:
```protobuf
message CertBundle {
  bytes blsAgg = 1;  // 96 B
  bytes rtCert = 2;  // ~3 KB
}
```

### Parallel Verification

- `verifyBLS` executes on the hot path
- `verifyRT` runs in parallel on a separate goroutine
- Block is invalid if either certificate is missing or fails

## Quasar Service Architecture

```
┌─RT Precompute (offline)────────────┐
│  20-50 shares ready in RAM         │
└────────────────────────────────────┘
                │
fast propose →  │  ← collected shares
                ▼
        ┌─────────────────┐
        │  RT Aggregator  │
        └─────────────────┘
                │ cert
                ▼
     broadcast → Block header
```

### Performance Characteristics
- RT verify/aggregate < 10% CPU on 8-core box
- Precompute thread hides ~90% of lattice cost
- Latency increase ≈ Δ_min (50ms mainnet)

## Chain-Specific Integration

### C-Chain (Beam/EVM)
```go
type Header struct {
    // Standard fields...
    ParentHash  common.Hash
    Number      *big.Int
    
    // Dual-certificate bundle
    CertBundle  CertBundle  // BLS + RT required
}

// Block invalid if BLS or RT missing
func (h *Header) Validate() error {
    if !verifyBLS(h.CertBundle.BlsAgg, validators) {
        return ErrInvalidBLS
    }
    if !verifyRT(h.CertBundle.RtCert, validators) {
        return ErrInvalidRT
    }
    return nil
}
```

### X-Chain (Nova)
- Vertex metadata includes RT cert every N vertices
- Vertex invalid if epoch root not sealed by RT cert on C-Chain

### M-Chain (MPC Custody)
- Each MPC round references last C-Chain height h
- Custody action invalid until RT cert covering h is on-chain

### Bridge Integration
- High-value transfers require PQ certificate
- Low-value transfers may use timeout fallback

## Account-Level Post-Quantum

### EVM (C-Chain)

#### Lamport-XMSS Multisig
```solidity
// Deployed at standard address
contract LamportMultisig {
    uint256 constant VERIFY_LAMPORT_GAS = 300_000;
    
    function verifyLamport(
        bytes32 msgHash,
        bytes memory lamportSig,
        bytes32 merkleRoot
    ) public pure returns (bool);
}
```

#### Wallet Integration
- Wallet can emit either `(v,r,s)` or `lamportSig`
- Meta-tx layer wraps XMSS inside EIP-4337 for compatibility

### X-Chain

#### New Output Type
```go
type PQTOutput struct {
    Algo      string  // "RINGTAIL"
    PublicKey []byte  // Ringtail public key
    Amount    uint64
}

// Spend requires Ringtail signature (~1.8 KB)
func (o *PQTOutput) Verify(sig []byte) error {
    return ringtail.Verify(o.PublicKey, sig)
}
```

#### Wallet CLI
```bash
lux-wallet generate --pq        # Generate PQ keypair
lux-wallet send --pq-sign      # Sign with Ringtail
```

## Migration Strategy

| Phase | EVM Rule | X-Chain Rule |
|-------|----------|--------------|
| 0 (Shadow) | PQ tx optional | PQ UTXO optional |
| 1 | Dual-sig (ECDSA + Lamport) required for new contracts | PQ required for > 10 LUX outputs |
| 2 | PQ mandatory for all tx | PQ mandatory |
| 3 | Remove secp verifier | Remove secp |

## Performance Parameters

| Symbol | Description | Mainnet | Testnet | Dev-net |
|--------|-------------|---------|---------|---------|
| n | Validators | 21 | 11 | 5 |
| t | RT Threshold | 15 | 8 | 4 |
| Δ_min | Round delay | 50 ms | 25 ms | 5 ms |
| β | BLS focus rounds | 6 | 5 | 4 |
| RT rounds | Ringtail rounds | 2 | 2 | 2 |
| Q_blocks | Quantum size | 128 | 64 | 16 |
| **Expected latency** | **(β+RT)×Δ_min** | **400 ms** | **225 ms** | **45 ms** |

Both certificates finish well within one Snowman++ slot (1-2s).

## Security Model

### Timeline
1. **Pre-quantum world**: Attacker must corrupt ≥ ⅓ stake to fork
2. **Q-day (BLS broken)**: Attacker can forge BLS but cannot forge RT
   - Block fails finality check because `verifyRT` rejects forged cert
3. **PQ world**: Security rests on lattice SVP
   - Forging RT requires 2^160 operations even with quantum computer

### Attack Window
- Window ≤ RT round-time (≤ 50ms mainnet)
- Consensus halts rather than accepting unsafe fork

## Implementation

### Block Structure
```go
type Block struct {
    Header BlockHeader
    Txs    []Transaction
}

type BlockHeader struct {
    // Existing fields
    Height    uint64
    ParentID  ids.ID
    Timestamp int64
    
    // Dual certificates
    Certs CertBundle
}

type CertBundle struct {
    BLSAgg []byte  // 96 bytes
    RTCert []byte  // ~3 KB
}
```

### Consensus Rules
```go
func (b *Block) Verify() error {
    // 1. Verify both certificates exist
    if len(b.Header.Certs.BLSAgg) == 0 {
        return ErrMissingBLS
    }
    if len(b.Header.Certs.RTCert) == 0 {
        return ErrMissingRT
    }
    
    // 2. Verify BLS (hot path)
    if !bls.FastAggregateVerify(
        b.Header.Certs.BLSAgg,
        validators.GetBLSKeys(),
        b.Header.Hash(),
    ) {
        return ErrInvalidBLS
    }
    
    // 3. Verify RT (parallel)
    done := make(chan error, 1)
    go func() {
        if !ringtail.Verify(
            b.Header.Certs.RTCert,
            validators.GetRTKeys(),
            b.Header.Hash(),
        ) {
            done <- ErrInvalidRT
        } else {
            done <- nil
        }
    }()
    
    // 4. Wait for RT verification
    return <-done
}
```

### Quasar Service
```go
type QuasarService struct {
    precompute *PrecomputePool
    aggregator *RTAggregator
    validators ValidatorSet
}

func (q *QuasarService) Run() {
    // Continuously precompute RT shares
    go q.precompute.Run()
    
    // On block proposal
    q.OnPropose(func(block *Block) {
        // Get precomputed share
        share := q.precompute.GetShare()
        
        // Broadcast share immediately
        q.p2p.Broadcast(share)
        
        // Collect shares from peers
        shares := q.aggregator.Collect(q.validators.Threshold())
        
        // Create certificate
        rtCert := ringtail.Aggregate(shares)
        
        // Add to block
        block.Header.Certs.RTCert = rtCert
    })
}
```

## Precompiled Contracts

### Quasar Verification (0x0...001F)
```solidity
interface IQuasar {
    // Verify dual certificates for a block
    function verifyDualCert(
        uint256 blockNumber
    ) external view returns (
        bool hasBLS,
        bool hasRT,
        bool isFullyFinal
    );
    
    // Get certificate data
    function getCertificates(
        uint256 blockNumber
    ) external view returns (
        bytes memory blsAgg,
        bytes memory rtCert
    );
}
```

### Lamport Verification (0x0...0020)
```solidity
interface ILamport {
    // Verify Lamport signature (300k gas)
    function verifyLamport(
        bytes32 msgHash,
        bytes calldata signature,
        bytes32 publicKeyHash
    ) external pure returns (bool);
}
```

## Summary

Lux's dual-certificate architecture provides:
- **Immediate PQ security**: Every block requires both BLS and RT
- **Maintained performance**: <400ms finality on mainnet
- **Unified security**: One RT key secures all chains
- **Gradual user migration**: Optional → recommended → mandatory PQ
- **Future-proof**: Quantum adversary must break both curves in 50ms

The system remains lightning-fast while becoming quantum-immortal.