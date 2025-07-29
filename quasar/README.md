# Quasar - Quantum-Secure Consensus Protocol Family

## Overview

The Quasar consensus protocol family is Lux Network's quantum-secure consensus implementation. Like Avalanche's Snow family, Quasar provides a modular consensus framework that can be adapted for different blockchain architectures.

## Protocol Family

Quasar builds upon our existing consensus stages:

```
Photon → Wave → Focus → Beam/Flare → Nova → Quasar
```

Each stage provides specific functionality:
- **Photon**: Binary consensus on single bit
- **Wave**: Multi-bit consensus
- **Focus**: Confidence aggregation
- **Beam**: Linear chain consensus (Snowman++ equivalent)
- **Flare**: State synchronization
- **Nova**: DAG-based consensus
- **Quasar**: Quantum-secure overlay with dual-certificate finality

## Core Innovation: Dual-Certificate Finality

Quasar achieves quantum security through dual-certificate finality:

```go
// Block is final IFF both certificates are valid
isFinal = verifyBLS(blsAgg, Q) && verifyRT(rtCert, Q)
```

- **BLS Certificate**: Classical security using BLS12-381 aggregated signatures
- **Ringtail Certificate**: Post-quantum security using lattice-based threshold signatures

## Architecture

```
/quasar/
├── choices/          # Consensus decision states
├── consensus/        # Core consensus algorithms
│   ├── beam/        # Linear chain consensus
│   └── nova/        # DAG consensus
├── crypto/          # Cryptographic primitives
│   ├── bls/         # BLS signatures
│   └── ringtail/    # Post-quantum signatures
├── engine/          # Consensus engines
│   ├── common/      # Shared engine code
│   ├── beam/        # Beam consensus engine
│   └── nova/        # Nova consensus engine
├── networking/      # Network layer
│   ├── handler/     # Message handlers
│   ├── router/      # Message routing
│   └── sender/      # Message sending
├── validators/      # Validator management
└── uptime/         # Validator uptime tracking
```

## Flow of a Single Blockchain

### 1. Transaction Submission
Users submit transactions to any node in the network. These transactions are gossiped to all nodes using the P2P layer.

### 2. Block Proposal
When a validator's turn comes (based on VRF or round-robin), they:
1. Collect transactions from mempool
2. Create a new block
3. Sign with BLS private key
4. Generate Ringtail share using precomputed data
5. Broadcast block proposal

### 3. Share Collection
Other validators:
1. Verify the proposed block
2. Generate their own Ringtail shares
3. Send shares to the proposer

### 4. Certificate Aggregation
The proposer:
1. Collects BLS signatures (happens automatically)
2. Collects Ringtail shares until threshold reached
3. Aggregates into dual certificates
4. Attaches certificates to block

### 5. Consensus
Validators poll each other:
1. Verify both BLS and Ringtail certificates
2. Vote accept/reject based on dual-cert validity
3. Achieve consensus through repeated sampling

### 6. Finalization
- Block is finalized when supermajority agrees
- Finality requires both certificates valid
- Missing Ringtail = proposer slashed

## Performance Characteristics

### Mainnet (21 validators)
- Block time: 500ms
- Dual-cert finality: <350ms
- BLS aggregation: ~295ms
- Ringtail aggregation: ~7ms
- Network overhead: ~50ms

### Quantum Security
- Attack window: <50ms (Quasar timeout)
- Lattice security: 128-bit post-quantum
- Automatic slashing for quantum attacks

## Components

### P2P Layer
Handles all inter-node communication:
- **Handshake**: Version negotiation
- **State Sync**: Fast sync to current state
- **Bootstrapping**: Full chain synchronization
- **Consensus**: Voting and certificate exchange
- **App Messages**: VM-specific communication

### Router
Routes messages to appropriate chains using ChainID. Handles timeouts adaptively based on network conditions.

### Handler
Processes incoming messages:
- Sync queue: State sync, bootstrapping, consensus
- Async queue: App messages
- Manages message ordering and delivery

### Sender
Builds and sends outbound messages:
- Registers timeouts for responses
- Benches unresponsive nodes
- Tracks message reliability

### Consensus Engine
Implements the Quasar protocol:
- Proposes blocks with dual certificates
- Polls network for decisions
- Manages state transitions
- Enforces slashing rules

## Blockchain Creation

The Manager bootstraps blockchains:
1. P-Chain starts first
2. P-Chain bootstraps C-Chain and X-Chain
3. Dynamic chain creation via subnet transactions
4. Each chain gets its own Quasar consensus instance

## Configuration

### Mainnet Parameters
```go
MainnetConfig = Config{
    K:               21,     // Sample size
    AlphaPreference: 13,     // Preference threshold
    AlphaConfidence: 18,     // Confidence threshold
    Beta:            8,      // Decision threshold
    QThreshold:      15,     // Ringtail threshold
    QuasarTimeout:   50ms,   // Certificate timeout
}
```

### Testnet Parameters
```go
TestnetConfig = Config{
    K:               11,
    AlphaPreference: 7,
    AlphaConfidence: 9,
    Beta:            6,
    QThreshold:      8,
    QuasarTimeout:   100ms,
}
```

## Usage

### Enable Quasar
```bash
luxd --quasar-enabled
```

### Monitor Performance
```bash
tail -f ~/.luxd/logs/quasar.log
```

### Expected Logs
```
[QUASAR] RT shares collected (15/21) @height=42 latency=48ms
[QUASAR] aggregated cert 2.9KB
[CONSENSUS] Block 42 dual-cert finalised ltcy=302ms
[QUASAR] Quantum-secure finality achieved ✓
```

## Security Guarantees

1. **Classical Security**: BLS12-381 provides 128-bit classical security
2. **Quantum Security**: Ringtail provides 128-bit post-quantum security
3. **Dual Requirement**: Both must be valid for finality
4. **Slashing**: Automatic punishment for protocol violations
5. **Physical Limits**: 50ms window makes quantum attacks impossible

## Future Improvements

- [ ] Dynamic validator sets
- [ ] Cross-subnet atomic swaps
- [ ] Light client proofs
- [ ] Mobile validator support
- [ ] Hardware security module integration

The Quasar protocol family ensures Lux Network remains secure against both classical and quantum adversaries, providing the foundation for decades of secure blockchain operation.