# Beam Consensus Engine

Beam is the linear chain consensus engine in the Quasar family, equivalent to Snowman++ in Avalanche.

## Overview

Beam provides:
- Linear chain consensus for blockchain ordering
- Dual-certificate finality (BLS + Ringtail)
- Sub-second finality with quantum security
- Automatic slashing for protocol violations

## Architecture

```
beam/
├── engine.go          # Main consensus engine
├── block.go           # Block structure with dual certificates
├── voter.go           # Voting and polling logic
├── issuer.go          # Block proposal logic
├── transitive.go      # Transitive voting
└── metrics.go         # Performance metrics
```

## Block Structure

```go
type Block struct {
    Header Header
    Txs    [][]byte
    Certs  CertBundle
}

type CertBundle struct {
    BLSAgg [96]byte  // BLS aggregate signature
    RTCert []byte    // Ringtail certificate
}
```

## Consensus Flow

1. **Block Proposal**
   - Proposer creates block
   - Signs with BLS private key
   - Initiates Ringtail share collection

2. **Share Collection**
   - Collect Ringtail shares from validators
   - Aggregate when threshold reached
   - Attach dual certificates to block

3. **Voting**
   - Validators verify dual certificates
   - Vote accept/reject
   - Achieve consensus through sampling

4. **Finalization**
   - Block accepted when supermajority agrees
   - Both certificates must be valid
   - Quantum-secure finality achieved

## Performance

- Block time: 500ms
- Dual-cert finality: <350ms
- Network overhead: ~50ms
- CPU overhead: +8%