# Consensus Package - Session Notes

## Terminology Update (2026-01-04)

Updated documentation to use "Vote" terminology consistently:

- **Vote**: Semantic name for validator responses to block proposals
- **Chits**: Wire protocol format (preserved for backwards compatibility)

Note: "Vote (wire format: Chits)" should be used where clarification is needed.

## Package Contents

### acceptor.go

Node-side `Acceptor` interface for block acceptance callbacks:
- `Accept()` called before container committed as accepted
- `AcceptorGroup` manages chain-ID-keyed acceptors (used by indexer + warp)
- Thread-safe with RWMutex

The canonical consensus `Acceptor` lives in `luxfi/consensus/core`. This
package's variant differs in that it takes a `*runtime.Runtime` (node-side
runtime context) rather than a `context.Context`, and supports multi-chain
registration via `AcceptorGroup`.

### quasar/

Node-side wiring around `github.com/luxfi/consensus/protocol/quasar`:
- `Quasar` - Wraps the canonical engine with P-Chain provider + finality channel
- `CoronaCoordinator` - Stub for threshold signing (real keys loaded later)
- `CoronaSignature`, `BLSSignature`, `QuasarSignature` - Node-side signature wrappers

Imports `github.com/luxfi/consensus/protocol/quasar` for the actual protocol.

## Architecture Notes

### Dual-Path Finality

```
Block arrives
    |
    +-- BLS PATH (fast) --------+-- CORONA PATH (quantum-safe) --+
    |   All validators sign     |   Round 1: commitments           |
    |   with BLS keys           |   Round 2: partial signatures    |
    |   Aggregate (96 bytes)    |   Combine threshold signature    |
    |                           |                                  |
    +---------------------------+----------------------------------+
                                |
                         HYBRID PROOF
                    (BLS + Corona combined)
                                |
                        QUANTUM FINALITY
```

### Vote Flow

1. Block proposed via gossip
2. Validators vote (wire: Chits message)
3. Votes collected and aggregated
4. Quorum check (2/3+ weight)
5. Finality achieved when both BLS and Corona complete

## Test Coverage

- `quasar/config_test.go` - Configuration tests
- `quasar/integration_test.go` - Integration tests

## Recent Changes

- 2026-01-04: Created documentation files with Vote terminology
