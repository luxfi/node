# Consensus Package - AI Assistant Guide

This package is node-side glue around the canonical consensus engine in
`github.com/luxfi/consensus`. It does NOT implement the protocol — the
protocol lives in that module.

## Package Structure

- `acceptor.go` - Node-side block-acceptance callbacks (chain-ID-keyed)
- `quasar/` - Node-side wiring around `consensus/protocol/quasar`
- `zap/` - ZAP agentic-consensus / DID bridge (self-contained, opt-in)

## Acceptor

`Acceptor` interface called when consensus accepts a block / vertex.
`AcceptorGroup` manages multiple acceptors per chain — used by the indexer
and warp IPC. The variant here differs from the canonical
`luxfi/consensus/core.Acceptor` (different signature: `*runtime.Runtime`
instead of `context.Context`, plus chain-ID-keyed registration).

## Quasar Wiring

The `quasar/` subpackage wraps `github.com/luxfi/consensus/protocol/quasar`
with node-specific adapters (P-Chain validator state, BLS signing keys,
Corona threshold coordinator stub).

### Signature Types

```go
SignatureTypeBLS      // Classical BLS
SignatureTypeCorona // Post-quantum threshold
SignatureTypeQuasar   // Hybrid BLS + Corona
SignatureTypeMLDSA    // Fallback ML-DSA
```

## Testing

```bash
GOWORK=off go test ./consensus/... -v
```

## Dependencies

- `github.com/luxfi/consensus` - Canonical consensus protocol (engine,
  protocol/quasar, types, etc.)
- `github.com/luxfi/ids` - ID types
- `github.com/luxfi/log` - Logging
- `github.com/luxfi/runtime` - Runtime context for acceptors
