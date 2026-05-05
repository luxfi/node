# Consensus Package (Node-Side Integration)

This package provides node-side glue around the canonical consensus engine
in [`github.com/luxfi/consensus`](https://github.com/luxfi/consensus). It does
NOT implement the consensus protocol — it only wires the engine into the
node's chain manager, indexer, and IPC layers.

## Overview

The consensus package contains:

- **Acceptor**: Callback mechanism invoked when consensus accepts a block /
  vertex. Adapts chain-ID-keyed runtime contexts to the node's indexer and
  warp IPC.
- **Quasar**: Node-side wiring around `consensus/protocol/quasar` (BLS +
  Ringtail hybrid finality). Adapts P-Chain validator state, BLS signing
  keys, and the Ringtail threshold coordinator.

## Package Structure

```
consensus/
  acceptor.go     # Acceptor interface + chain-ID-keyed AcceptorGroup
  quasar/         # Node-side wrapper around luxfi/consensus/protocol/quasar
  zap/            # ZAP agentic-consensus / DID bridge (self-contained)
```

## Acceptor

The `Acceptor` interface is called before containers are committed as
accepted:

```go
type Acceptor interface {
    Accept(rt *runtime.Runtime, containerID ids.ID, container []byte) error
}
```

Multiple acceptors can be registered per chain via `AcceptorGroup`.

## Quasar Integration

The `quasar/` subpackage wraps `github.com/luxfi/consensus/protocol/quasar`
and provides node-specific wiring:

1. **P-Chain provider** — feeds live validator state from PlatformVM's
   validator manager into the engine.
2. **Quantum fallback signer** — adapts the node's BLS signing key.
3. **Ringtail coordinator stub** — placeholder until real threshold key
   material is loaded.

The actual hybrid finality protocol (BLS + Ringtail + ML-DSA threshold
signing) lives in `luxfi/consensus`. See that repository for protocol
details, parameter tuning, and benchmarks.
