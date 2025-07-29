# Quasar Post-Quantum Finality Layer

Quasar is a post-quantum finality layer for the Lux Network that provides dual-certificate finality using both classical BLS signatures and quantum-resistant Ringtail signatures.

## Overview

Quasar acts as a cryptographic finality gadget that overlays the existing consensus ladder (Photon → Wave → Beam/Flare → Nova) and enforces finality on blocks in a manner resilient to quantum attacks.

### Key Features

- **Dual-Certificate Finality**: Blocks require both BLS and Ringtail signatures
- **Parallel Aggregation**: BLS and Ringtail signatures are aggregated in parallel for performance
- **Precomputation**: Ringtail signatures use precomputed values for <200ms signing
- **Key Rotation**: Automatic rotation of post-quantum keys for enhanced security
- **Slashing Detection**: Automatic detection of validator misbehavior
- **Sub-second Finality**: Target <1s finality with ~700ms for BLS + ~200ms for Ringtail

## Architecture

```
Nova Consensus
     ↓
OnNovaDecided()
     ↓
Quasar Engine
     ├── BLS Signing (parallel)
     └── Ringtail Signing (parallel)
           ↓
     Aggregator
     ├── BLS Aggregation
     └── Ringtail Aggregation
           ↓
    Dual Certificate
           ↓
    Block Finalized
```

## Components

### Engine (`engine.go`)
Main Quasar engine that coordinates finality process:
- Receives blocks from Nova consensus
- Manages dual signing process
- Tracks finalized blocks
- Emits finality and slashing events

### Aggregator (`aggregator.go`)
Manages parallel signature aggregation:
- Collects BLS and Ringtail shares
- Performs threshold aggregation
- Creates dual certificates

### Key Manager (`key_manager.go`)
Handles cryptographic key lifecycle:
- Manages BLS and Ringtail keypairs
- Automatic key rotation
- Epoch-based key validity
- DKG support (for threshold Ringtail)

### Precompute Pool (`precompute_pool.go`)
Optimizes Ringtail signing performance:
- Maintains pool of precomputed values
- Background generation
- Automatic refilling

### Slashing Detector (`slashing_detector.go`)
Detects validator misbehavior:
- Double signing detection
- Missing Ringtail signature detection
- Invalid signature detection

### Nova Integration (`nova_integration.go`)
Hooks Quasar into Nova consensus:
- Intercepts Nova decisions
- Triggers finality process
- Reports finality status

## Usage

### Basic Integration

```go
// Create Quasar configuration
quasarConfig := pq.Config{
    NodeID:          nodeID,
    Threshold:       67, // 67% of validators
    BLSSecretKey:    blsSK,
    RingtailSK:      rtSK,
    Validators:      validators,
    FinalityTimeout: 10 * time.Second,
    PrecompPoolSize: 100,
}

// Create Nova engine with Quasar
engine, err := nova.NewQuasarEngine(novaParams, quasarConfig)

// Initialize
ctx := context.Background()
err = engine.Initialize(ctx)

// Blocks will automatically achieve Quasar finality after Nova decides
```

### Checking Finality

```go
// Check if block is finalized
if engine.IsQuasarFinalized(blockID) {
    // Get certificate
    cert, _ := engine.GetQuasarCertificate(blockID)
    fmt.Printf("Block finalized with %d validators\n", len(cert.SignerIDs))
}
```

### Monitoring

```go
// Get performance metrics
metrics := engine.GetQuasarMetrics()
fmt.Printf("Blocks finalized: %d\n", metrics.BlocksFinalized)
fmt.Printf("Average latency: %v\n", metrics.FinalityLatency)
fmt.Printf("Slashing events: %d\n", metrics.SlashingEvents)
```

## Security Considerations

1. **Dual Signatures**: Both BLS and Ringtail must succeed for finality
2. **Key Rotation**: Ringtail keys rotate periodically (default: 24h)
3. **Slashing**: Automatic detection and evidence generation for misbehavior
4. **Threshold**: Requires >67% of validators by default

## Performance Targets

- **Finality Latency**: <1 second
- **BLS Aggregation**: ~700ms (network + computation)
- **Ringtail Addition**: ~200ms (using precomputation)
- **Verification**: <1ms per certificate
- **Throughput**: No significant impact on consensus throughput

## Testing

Run the test suite:
```bash
go test ./consensus/engine/quasar/...
```

Run benchmarks:
```bash
go test -bench=. ./consensus/engine/quasar/...
```

## Future Enhancements

1. **Distributed Key Generation (DKG)**: Full implementation for threshold Ringtail
2. **Network Optimization**: Dedicated channels for signature propagation
3. **Storage Optimization**: Efficient certificate storage and pruning
4. **Light Client Support**: Compact proofs for light clients