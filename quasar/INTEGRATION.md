# Quasar Integration Guide

## Overview

This guide covers the integration of the Quasar quantum-secure consensus protocol into the Lux node. Quasar is implemented at the root of the node following Avalanche's `/snow/` pattern, providing a modular consensus framework.

## Repository Structure

```
/node/
├── quasar/                    # Quantum-secure consensus family
│   ├── README.md             # Protocol overview and architecture
│   ├── INTEGRATION.md        # This file
│   ├── choices/              # Consensus decision states
│   ├── consensus/            # Consensus engines
│   │   ├── beam/            # Linear chain consensus
│   │   └── nova/            # DAG consensus (future)
│   ├── crypto/              # Cryptographic primitives
│   │   ├── bls/             # BLS signatures
│   │   └── ringtail/        # Post-quantum signatures
│   ├── validators/          # Validator management
│   └── uptime/             # Validator uptime tracking
├── vms/                      # Virtual machines
├── chains/                   # Chain management
└── ...
```

## Integration Steps

### 1. Import Quasar Consensus

In your VM or chain implementation:

```go
import (
    "github.com/luxfi/node/quasar"
    "github.com/luxfi/node/quasar/consensus/beam"
    "github.com/luxfi/node/quasar/crypto/bls"
    "github.com/luxfi/node/quasar/crypto/ringtail"
)
```

### 2. Create Consensus Context

```go
// Create context with quantum security enabled
ctx := &quasar.ConsensusContext{
    Context: &quasar.Context{
        NodeID:         nodeID,
        PublicKey:      blsPK,
        Log:            logger,
        QuasarEnabled:  true,
        RingtailSK:     rtSK,  // Ringtail secret key
        RingtailPK:     rtPK,  // Ringtail public key
        ValidatorState: vdrState,
    },
    Registerer: metrics.NewRegistry(),
}
```

### 3. Configure Beam Engine

```go
// Mainnet configuration
config := beam.Config{
    Params: beam.Parameters{
        K:                     21,
        AlphaPreference:       13,
        AlphaConfidence:       18,
        Beta:                  8,
        MaxItemProcessingTime: 9630 * time.Millisecond,
    },
    QuasarEnabled:     true,
    QuasarTimeout:     50 * time.Millisecond,
    RingtailThreshold: 15,
    StartupTracker:    startupTracker,
    Sender:            sender,
    Timer:             timer,
}

// Create engine
engine, err := beam.NewEngine(config, ctx, vm)
if err != nil {
    return err
}
```

### 4. Implement VM Interface

Your VM must implement the `beam.VM` interface:

```go
type VM interface {
    // GetBlock returns the block with the given ID
    GetBlock(context.Context, ids.ID) (quasar.Block, error)
    
    // BuildBlock builds a new block
    BuildBlock(context.Context) (quasar.Block, error)
    
    // SetPreference sets the preferred block
    SetPreference(context.Context, ids.ID) error
    
    // LastAccepted returns the last accepted block
    LastAccepted(context.Context) (ids.ID, error)
    
    // VerifyWithContext verifies a block with context
    VerifyWithContext(context.Context, quasar.Block) error
}
```

### 5. Implement QuasarBlock Interface

Your blocks must support dual certificates:

```go
type MyBlock struct {
    // Your existing fields
    height     uint64
    parentID   ids.ID
    timestamp  int64
    
    // Dual certificates
    certs      beam.CertBundle
}

// Implement QuasarBlock interface
func (b *MyBlock) HasDualCert() bool {
    return b.certs.BLSAgg != [96]byte{} && len(b.certs.RTCert) > 0
}

func (b *MyBlock) BLSSignature() []byte {
    return b.certs.BLSAgg[:]
}

func (b *MyBlock) RTCertificate() []byte {
    return b.certs.RTCert
}

func (b *MyBlock) SetQuantum() error {
    if !b.HasDualCert() {
        return errors.New("missing dual certificates")
    }
    b.status = choices.Quantum
    return nil
}
```

### 6. Handle Slashing Events

Monitor for slashing events:

```go
// Get slash channel
slashCh := engine.GetSlashChannel()

// Monitor in goroutine
go func() {
    for slash := range slashCh {
        log.Warn("Slashing event",
            "proposer", slash.ProposerID,
            "height", slash.Height,
            "reason", slash.Reason,
        )
        
        // Handle slashing (e.g., reduce stake)
        handleSlashing(slash)
    }
}()
```

### 7. Start Consensus

```go
// Start the engine
if err := engine.Start(ctx); err != nil {
    return err
}

// Build blocks when needed
block, err := engine.BuildBlock(ctx)
if err != nil {
    // Handle error (may include slashing)
    return err
}

// Block will have dual certificates attached
```

## Testing Integration

### Unit Tests

```go
func TestQuasarIntegration(t *testing.T) {
    // Create test engine
    engine, vm := createTestEngine(t)
    
    // Enable Quasar
    engine.config.QuasarEnabled = true
    
    // Start engine
    ctx := context.Background()
    err := engine.Start(ctx)
    require.NoError(t, err)
    
    // Build block
    block, err := engine.BuildBlock(ctx)
    require.NoError(t, err)
    
    // Verify dual certificates
    quasarBlock := block.(quasar.QuasarBlock)
    require.True(t, quasarBlock.HasDualCert())
}
```

### Integration Tests

Run the full test suite:

```bash
# Unit tests
go test ./quasar/...

# Fuzz tests
go test -fuzz=Fuzz ./quasar/consensus/beam/

# Benchmarks
go test -bench=. ./quasar/...

# Race detection
go test -race ./quasar/...
```

## Performance Tuning

### Precomputation Pool

Adjust pool size based on block production rate:

```go
// For high-frequency block production
precompPool := beam.NewPrecompPool(128)

// For standard rates
precompPool := beam.NewPrecompPool(64)
```

### Timeout Configuration

Adjust timeouts based on network conditions:

```go
// Fast network
config.QuasarTimeout = 30 * time.Millisecond

// Standard network
config.QuasarTimeout = 50 * time.Millisecond

// High-latency network
config.QuasarTimeout = 100 * time.Millisecond
```

## Monitoring

### Metrics

Key metrics to monitor:

```prometheus
# Block metrics
quasar_beam_blocks_proposed
quasar_beam_blocks_accepted
quasar_beam_blocks_rejected

# Certificate metrics
quasar_beam_bls_cert_time
quasar_beam_rt_cert_time
quasar_beam_dual_cert_time

# Quantum metrics
quasar_beam_quantum_finality
quasar_beam_slashing_events
quasar_beam_quasar_timeouts

# Performance metrics
quasar_beam_consensus_time
```

### Logs

Important log patterns:

```
[QUASAR] RT shares collected (15/21) @height=42 latency=48ms
[QUASAR] aggregated cert 2.9KB
[CONSENSUS] Block 42 dual-cert finalised ltcy=302ms
[QUASAR] Quantum-secure finality achieved ✓
[QUASAR] Slashing event: proposer=NodeID-xxx reason="Quasar timeout"
```

## Troubleshooting

### Common Issues

1. **Quasar Timeout**
   - Increase `QuasarTimeout` configuration
   - Check network latency
   - Verify validator connectivity

2. **Missing Ringtail Certificate**
   - Ensure Ringtail keys are properly configured
   - Check precomputation pool is running
   - Verify threshold is achievable

3. **Slashing Events**
   - Monitor slash channel
   - Check proposer's network connection
   - Verify Ringtail key validity

### Debug Mode

Enable debug logging:

```go
ctx.Log = logging.NewLogger("quasar", logging.Debug)
```

## Migration from Snowman++

### Minimal Changes

1. Replace imports:
```go
// Old
import "github.com/luxfi/node/snow/consensus/snowman"

// New
import "github.com/luxfi/node/quasar/consensus/beam"
```

2. Update configuration:
```go
// Add to existing config
config.QuasarEnabled = true
config.RingtailThreshold = 15
```

3. Implement dual certificate support in blocks

### Gradual Rollout

1. **Phase 1**: Deploy with `QuasarEnabled = false`
2. **Phase 2**: Enable on testnet
3. **Phase 3**: Enable on subset of mainnet validators
4. **Phase 4**: Full mainnet deployment

## Security Considerations

1. **Key Management**
   - Store Ringtail keys securely
   - Use hardware security modules for production
   - Rotate keys periodically

2. **Slashing Protection**
   - Monitor slash events
   - Implement automatic response to attacks
   - Set appropriate timeout values

3. **Network Security**
   - Use TLS for all communications
   - Implement rate limiting
   - Monitor for quantum attack patterns

## Support

- GitHub Issues: Report bugs and feature requests
- Discord: Community support
- Documentation: https://docs.lux.network/quasar

---

For the latest updates and best practices, refer to the main README.md and monitor release notes.