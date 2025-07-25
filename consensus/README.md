# Lux Consensus Framework

## Overview

The Lux consensus framework provides a family of consensus protocols for building distributed systems. The framework has been reorganized to use descriptive names that clearly indicate the purpose and functionality of each component.

## Directory Structure

```
consensus/
├── binaryvote/      # Binary voting consensus primitive
├── linear/          # Linear chain consensus for blockchains
│   ├── poll/        # Voting and poll management
│   └── bootstrap/   # Chain bootstrapping logic
├── graph/           # DAG consensus for UTXO transactions
└── common/          # Shared interfaces and utilities
    └── choices/     # Decision states (Unknown, Processing, Accepted, Rejected)
```

## Consensus Protocols

### Binary Vote Consensus (`binaryvote`)
The fundamental building block of Lux consensus. It implements binary decision-making through repeated sampling and voting. Key features:
- **Binary decisions**: Each node votes between two conflicting options
- **Metastability**: The protocol amplifies small preferences into strong consensus
- **Confidence counters**: Tracks consecutive rounds of agreement to build confidence

### Linear Consensus (`linear`)
Extends binary voting for linear blockchain consensus. Used by all chains in the Lux network (X-Chain, C-Chain, P-Chain). Key features:
- **Linear ordering**: Ensures blocks form a single chain
- **Block finality**: Irreversible acceptance once consensus is reached
- **Efficient bootstrapping**: Quickly syncs new nodes to the current state

### Graph Consensus (`graph`)
Implements consensus for directed acyclic graph structures, used for UTXO transactions on the X-Chain. Key features:
- **Parallel transactions**: Multiple transactions can be accepted simultaneously
- **Conflict resolution**: Handles double-spend attempts through voting
- **Vertex-based structure**: Transactions organized in a DAG of vertices

## Key Components

### Choices (`common/choices`)
Defines the possible states for any consensus decision:
- `Unknown`: Initial state, no decision made
- `Processing`: Currently being evaluated by consensus
- `Accepted`: Irreversibly accepted by the network
- `Rejected`: Irreversibly rejected by the network

### Poll Management (`linear/poll`)
Handles the voting process:
- Tracks votes from validators
- Implements early termination when outcome is certain
- Manages vote aggregation and result calculation

### Bootstrap (`linear/bootstrap`)
Manages node synchronization:
- Fetches historical blocks from other nodes
- Verifies block validity during sync
- Transitions to normal consensus once caught up

## Usage

The consensus protocols are used by the Lux Virtual Machines (VMs) to achieve agreement on state transitions. Each blockchain selects the appropriate consensus protocol:

- **Linear consensus**: Used by X-Chain, C-Chain, and P-Chain for block acceptance
- **DAG consensus**: Fast massively parallelizable consensus X-Chain transactions
- **Binary vote**: Core primitive used internally by other protocols

## Migration from Snow Terminology

For developers familiar with the previous "Snow" naming:
- `confidence` → `binaryvote`
- `snowman` → `linear`
- `graph` → `graph`
- `avalanche` → `graph` (vertex definitions merged)
- `snow/choices` → `consensus/common/choices`

 This restructuring provides clearer semantics while maintaining full compatibility with the existing consensus algorithms.

The old “Snowflake” primitive hasn’t disappeared—it’s just folded into the new sampling package under the name “flat sampling.”
• Snowflake was the single-round, first-to-α decision rule (i.e. “sample k, if ≥α agree then decide”).
• In the new layout, that lives in `consensus/sampling/flat.go` and is exposed via:

```go
// Flat is the one-shot (unary) sampling protocol—i.e. Snowflake.
sampling.NewFlat(factory, params, choice)
```

• The old “Snowball” (i.e. the tree-of-Snowflakes, multi-round extension) is now the tree sampling protocol in `consensus/sampling/tree.go` exposed via:

```go
sampling.NewNetwork(factory, params, numColors, rng)
```

So there is no longer a separate snowflake package—everything lives under `consensus/sampling`:
• Unary (Snowflake) → `sampling.NewFlat`
• N-nary (Snowflake + Snowball) → `sampling.NewNetwork`

Thresholds, confidence and choice logic that used to live in snowball/snowflake are now in the sibling packages:
• `consensus/threshold` for the termination checks
• `consensus/confidence` for the confidence counters
• `consensus/choices` for the “decidable” interface

Together they reconstitute exactly what Snowflake and Snowball used to do, just reorganized into a more composable structure.
