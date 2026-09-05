# lux-xvm — the Lux X-Chain in Rust

The UTXO ledger: value exists as unspent outputs, a transaction consumes some
and produces others, a block is an ordered run of transactions plus the root of
the state they leave behind. Ported from the Go reference at
`~/work/lux/node/vms/xvm` (13,306 lines, 43 test files), behind the object-safe
VM seam of `~/work/lux-rs/node` (`src/vm.rs`) — which this crate imports rather
than restates.

17,222 lines across 29 files. 313 tests, all green. No stubs.

## Build

```
PATH=~/.cargo/bin:$PATH LUX_LIB_DIR=~/work/lux/crypto/dist cargo test
```

`LUX_LIB_DIR` is needed because this crate names the node, the node holds an
ML-DSA identity and an ML-KEM session, and both come from `libluxcrypto`
through `lux-pq`. `build.rs` repeats the directory that crate resolved rather
than searching for a second one, so this crate's test binaries — which run the
node — can load.

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

`tests/node.rs` runs the chain inside `lux-rs/node` itself: the node's registry
finds it, the node's JSON-RPC server routes a call to it over a real socket, the
node's sealed plugin link drives build/verify/accept across a process boundary,
and the node's engine reads the two roots a certificate is made of out of a
block this chain built. Nothing there is a mock.

`src/bin/conformance.rs` answers the shared corpus under `conformance/` — the
differential that hands one corpus to the Go, Rust and C++ X-chains and fails on
any field two of them answer differently.

## Layout

```
zap.rs        the serialization: a fixed section of known offsets plus a tail
wire/         the (family, shape) envelopes and the container shapes
fx/           the three fx families and the rules by which a credential spends
utxo.rs       an output, an input, an unspent output, and the flow check
txs/          the five transactions, their encoding, signing, and executor/
state/        what the chain knows, in two shapes: committed, and what-would-be
db.rs         where the chain writes itself down, so a restart remembers
block/        a block, how one is built, and the state machine over them
mempool.rs    what waits to go in a block, and what is refused before it waits
security.rs   on what terms a transaction is admitted at all
gossip.rs     how a transaction reaches the network, and the filter it is in
vm.rs         the chain behind the node's seam
```

## Five decisions worth knowing

**The seam is imported, not stated.** `lux_node::vm` is `host` here, and `Id` is
`lux_consensus::finality::Id` — the node's own trait and the node's own id, not
a copy of either. A restated seam compiles against a shape the node never sees
and keeps compiling on the day the node changes a method; a second 32-byte id
would need translating at every call, and a node holding two chains that each
minted one could not put them in one map.

**There is one mempool, and one door into it.** A wallet's transaction and a
peer's arrive at the same `Xvm::issue`, and the refusals run in Go's order:
already held, already refused for a reason still remembered, does it verify
against the preferred state, does it fit, does it conflict, is it allowed. Every
check before the verification costs a lookup — that ordering is the whole
anti-flood argument. The pool lives inside `gossip::Gossip`, so what is held is
also what this node advertises and what it has to pass on; there is nothing to
keep in step.

**The security profile is where a transaction enters.** A chain that is
post-quantum in its signatures and classical in its mempool is classical.
`Xvm::hold_to` is Go's `SetAuthPolicy`, and under the strict profile with no
exemption list this chain admits nothing — every fx family it runs spends with a
secp256k1 signature. That refusal is the correct answer rather than a gap, and
Go's own X-chain test asserts it.

**Nothing reaches the disk until a block is accepted.** `Manager::accept` puts
the block, everything its transactions changed, and the position it moved the
chain to into one batch. A restart therefore never sees a block applied at a
height the chain has not reached, and a block that was verified and then lost
leaves nothing behind.

**The asset family of the state root is empty, deliberately.** The chain's state
is unspent outputs and nothing else — an asset exists only as the id stamped on
the outputs its creating transaction produced. Projecting an asset arena would
mean inventing state the executor does not keep. Go does the same thing for the
same reason (`block/executor/executionroot.go`), and the asset binding survives
through each UTXO leaf's asset id.

## Storage

`db::Db` is an ordered map from bytes to bytes, written in batches that either
all happen or none do. The keys are Go's, byte for byte — `utxo`, `tx`,
`blockID`, `block`, `singleton`, and the singleton keys `0x00`, `0x01`, `0x02`,
with a height packed big-endian. What is under a key is the object's canonical
ZAP encoding, the same bytes that go on a wire and that the id is taken over, so
a stored block hashes to the id it is filed under and `Store::on` checks exactly
that when it reads one back.

This is NOT Go's on-disk file format. That format is LevelDB's; no rule in this
chain is stated over it and two nodes never exchange it. `db::Log` is ours: one
framed record per batch, flushed to the device before the write returns,
replayed in order on open. A torn tail is a batch that never returned to a
caller, so it is dropped and the file is cut back to the last whole frame.

The timestamp is seconds since the epoch, big-endian, where Go writes a
`time.Time`'s own binary form. A language's internal representation of a clock
reading is not a ledger value, and the chain's timestamp is seconds everywhere
else it appears.

## What was ported, and what was left

Ported: the ZAP object format; the wire envelopes; secp256k1 / nft / property fx
with their spend rules; all five transactions with their encoding, signing and
parsing; the syntactic, semantic and execution passes; the store and the diff
stack; the block, its builder, and the verify/accept/reject machine; the
execution root and the owner root; the VM behind the seam; persistence; the
bounded conflict-aware mempool with its memory of refusals; the strict
post-quantum admission profile; the gossip set and its Bloom filter, function
for function with `p2p/gossip/bloom.go`.

Not ported, and none of it is ledger behaviour:

- the node's transaction indexer (`index_test.go`)
- the sockets themselves. This crate holds the SET and answers the three
  questions a p2p layer asks — do you have this, here is one, what do you
  already know — and which socket they arrive on is the node's business.
- the RPC op registry and its doc/tool generators (`ops.go`, `ops_test.go`)
- the genesis-building static service (`static_service*.go`)
- config parsing (`config/`)
- Go's parallel Merkle fold and the tests that assert it equals the serial one.
  There is one fold here, so the property has nothing to compare against; the
  KAT pins the bytes either way.

## Known, and not hidden

The X vectors' `exec` field is not compared in the differential. Every
evaluator — Go's, this one, and C++'s — reports semantic verification as not
evaluated, because none of them stands up a funded UTXO set for the corpus.
Closing it means the Go generator standing one up too, so the two answers have
something to be compared against; until then the field honestly reads as
skipped rather than as a pass.
