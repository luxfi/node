# lux-xvm — the Lux X-Chain in Rust

The UTXO ledger: value exists as unspent outputs, a transaction consumes some
and produces others, a block is an ordered run of transactions plus the root of
the state they leave behind. Ported from the Go reference at
`~/work/lux/node/vms/xvm` (13,306 lines, 43 test files), against the object-safe
VM seam of `~/work/lux-rs/node` (`src/vm.rs`).

13,388 lines across 24 files. 239 tests, all green. No stubs.

## Build

```
PATH=~/.cargo/bin:$PATH cargo test
```

Nothing else is needed — no `LUX_LIB_DIR`, no C library, no path dependency. The
crate's whole dependency list is five crates: `sha2`, `ripemd`, `sha3`, `k256`,
`serde_json`.

## What proves it is the same chain

`tests/golden.rs`. Twelve vectors printed by the Go X-chain
(`luxfi/node/vms/xvm` on `luxfi/zap v1.2.7`, `luxfi/utxo v0.5.10`) and pasted in
unchanged, covering all five transaction types, a block, and every fx primitive
envelope. Each asserts three things: the unsigned bytes a signature is computed
over, the signed bytes that go on the wire, and the id — which is the SHA-256 of
those bytes, so a difference of one byte is two names for one transaction.

`src/state/root.rs` carries the execution-root KAT verbatim from
`state/xvmroot/xvmroot_test.go` — the value the Go package and all seven GPU
backends produce. It passes.

Those two together mean the wire and the state commitment are byte-identical to
Go, not merely self-consistent.

## Layout

```
zap.rs        the serialization: a fixed section of known offsets plus a tail
wire/         the (family, shape) envelopes and the container shapes
fx/           the three fx families and the rules by which a credential spends
utxo.rs       an output, an input, an unspent output, and the flow check
txs/          the five transactions, their encoding, signing, and executor/
state/        what the chain knows, in two shapes: committed, and what-would-be
block/        a block, how one is built, and the state machine over them
host.rs       the node's seam, stated here so the chain builds without the node
vm.rs         the chain behind that seam
```

## Three decisions worth knowing

**The seam is stated here, not imported.** `host.rs` is the same shape, method
for method and type for type, as `lux-rs/node`'s `src/vm.rs` — `Id` is
`[u8; 32]` in both. A chain crate that depended on the whole node would invert
the direction the node's own EVM already runs in: `lux-evm` is a standalone
crate and the node has one adapter module over it. Wiring this chain into that
node is one `impl` block that forwards each method.

**The mempool is in `vm.rs`, not in the block manager.** Holding pending
transactions is a node's job; the manager decides what is true. A rejected
block's transactions come back through the mempool, re-checked; an accepted
block's are gone.

**The asset family of the state root is empty, deliberately.** The chain's
state is unspent outputs and nothing else — an asset exists only as the id
stamped on the outputs its creating transaction produced. Projecting an asset
arena would mean inventing state the executor does not keep. Go does the same
thing for the same reason (`block/executor/executionroot.go`), and the asset
binding survives through each UTXO leaf's asset id.

## What was ported, and what was left

Ported: the ZAP object format; the wire envelopes; secp256k1 / nft / property fx
with their spend rules; all five transactions with their encoding, signing and
parsing; the syntactic, semantic and execution passes; the store and the diff
stack; the block, its builder, and the verify/accept/reject machine; the
execution root and the owner root; the VM behind the seam.

Not ported, and none of it is ledger behaviour:

- the node's transaction indexer (`index_test.go`)
- p2p gossip and the RPC network path (`network/`, `TestMarshaller`,
  `TestFilter`)
- the RPC op registry and its doc/tool generators (`ops.go`, `ops_test.go`)
- the genesis-building static service (`static_service*.go`)
- config parsing (`config/`)
- the strict-post-quantum mempool admission profile
  (`vm_security_profile_test.go`) — that is `luxfi/chains/fee` policy over a
  chain, not a rule of this chain
- Go's parallel Merkle fold and the tests that assert it equals the serial one.
  There is one fold here, so the property has nothing to compare against; the
  KAT pins the bytes either way.

Storage is in memory. Go's `state.State` is a `versiondb` over a key-value
store with caches; `Store` here is ordered maps behind the same `Chain` trait,
so a persistent layer is a second implementation of that trait and nothing
above it changes.
