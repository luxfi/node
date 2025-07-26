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

