# quantumvm — the Lux Q-Chain in C++

The Q-Chain is the finality plane: it carries post-quantum-signed instruction
payloads and finalizes them when a quorum of the committee has signed a block
and the aggregate of those signatures verifies. This is that chain, in C++,
behind `lux-cpp/node`'s VM seam (`include/lux/node/vm.hpp`).

Ported from the Go reference at `~/work/lux/chains/quantumvm` (2,983 LoC of
implementation, 2,745 of tests) — the behavioural source of truth. Where the two
differ, the Go one is right and this is a bug.

```
include/lux/quantumvm/  zap wire · sha256 · sha512 · id · error · config
                        signer (ML-DSA) · transaction · block · store
                        quasar (finality) · vm · service
src/                    their implementations
test/                   twelve suites plus the differential evaluator
```

## What makes this a port and not a rewrite

**The bytes, and the ids taken over them.** A block's id is `sha256` of its
canonical ZAP wire, so a C++ node that encodes a field one byte differently
gives every block a different name and is simply a different chain. Two things
pin that, and neither is self-referential:

1. `test/genesis_test.cpp` asserts the genesis id of `testChain` against Go's
   own constant, copied verbatim from `genesis_test.go`:

   ```
   zAaMUekpcRPgjqxqT8kSm9P5xRKJuz2k5v3zN2WfnmwdBdojU
   ```

   That one string exercises the whole stack at once — the ZAP builder's field
   offsets and alignment, sha256, and the CB58 rendering an id is written in.

2. `test/differential.cpp` reads the SHARED corpus
   (`conformance/corpus/chain_differential.json`) and prints one
   `RESULT id=… status=… detail=…` line per Q vector for
   `conformance/harness_runner.py`, which compares Go, Rust and C++ on the same
   bytes. It is a test as well as an evaluator: the corpus carries Go's expected
   id for every vector, so an id derived differently fails here rather than
   being noticed by a human later.

## The dependency closure

`luxcpp/blst` (BLS12-381), `luxcpp/pqclean` (the ML-DSA reference body),
`luxcpp/crypto` (its C++ surface, and SHA-512), and the two seam headers from
`lux-cpp/node` and `lux-cpp/consensus`. That is the whole of it: nothing here
reaches a node implementation or an upstream fork.

Every one of them is REQUIRED and the build says so: a Q-chain that cannot check
a post-quantum signature is not a Q-chain, and a chain that cannot check a
validator's BLS signature finalizes on arithmetic rather than cryptography.
There is no mode in which a signature check is skipped.

## One id type

The chain's id type IS the node's — `lux::node::Id`, which `lux/node/vm.hpp` is
already written in (`id.hpp` imports it and adds free functions). A chain that
declared its own 32-byte name would have to convert at every call into the node,
and the conversion is where two id types drift apart.

## Where this port makes a decision the reference did not

- **`root()` — the execution state root.** The node's seam requires one: a block
  that cannot say what state it produced cannot be built, because a validator
  signing it would be certifying a name rather than a result. Go's Q-Chain has
  no such method, so this defines it as the commitment to the rows accepting the
  block writes — the block under its id, its height-index entry, and the tip and
  height the chain moves to. It is arithmetic over the block's own content, so it
  is available before the block is accepted, which is what a validator needs.

- **The accelerator.** Go asks `luxfi/accel` first and falls back to the CPU on
  any error. That fallback is only safe because a missing accelerator is an
  ERROR — a GPU path that answered "fine" having verified nothing would report
  every signature in a batch as good. No accelerator is bound to this build, so
  `gpu_batch_verify` always refuses and the CPU path does the work. The contract
  is the same and honestly reported; `signer_test.cpp` pins it.

- **The finality bridge tracks a block only once it holds a verified
  signature.** Go creates the tracking entry BEFORE signing and deletes it again
  when the signature does not arrive (a leak of one map entry per failure, since
  only a finalized block is ever cleaned up). Here the entry is created from a
  signature that already verified, so "tracked and empty" is not a state that
  exists.

- **The RPC service is a projection, not a server.** Go registers a JSON-RPC
  service; the node's own `rpc.hpp` owns routing, codecs and the lock a method
  runs under, so `service.hpp` is every answer the service gives at real types
  and nothing else. Mounting it is a matter of naming those functions in a method
  table.

- **The store.** Go reaches `luxfi/database` + `versiondb`; this carries the two
  layers itself — a durable byte map whose batches are all-or-nothing, and the
  staging layer over it whose `abort()` is what stops a failed commit from being
  flushed, wholesale, by the next commit that succeeds.

## Building and running

```sh
cmake -S . -B build && cmake --build build -j"$(nproc)"
cd build && ctest --output-on-failure
```

The checkouts are found by a marker FILE rather than a directory name, so a
same-named sibling cannot be mistaken for one; override with `-DLUXCPP_ROOT=`,
`-DNODE_DIR=`, `-DCONSENSUS_DIR=`, `-DPQCLEAN_DIR=`.

## A corpus defect this port found

`conformance/gen/main.go` built the `Q_BLOCK` vector with a bare signature
PREIMAGE in the block's transaction blob rather than the ENVELOPE a block
carries, and declared it ACCEPTED without running it through anything. The Go
reference refuses those bytes — `parseBlockBytes` answers
`transaction 0: zap: buffer too small` — so the vector claimed an acceptance no
implementation could produce. The generator now wraps the preimage
(`writeQEnvelope`), and both the Go reference and this port accept the
regenerated vector under the same id.

## An absent signature still has a time

Go's `marshalTx` substitutes a zero-valued `QuantumSignature` when a transaction
carries none, and writes `sig.Timestamp.UnixNano()` from it. The zero
`time.Time` is year 1, not the Unix epoch, so that call yields
`-6795364578871345152` — not 0. This port wrote 0, which gave one transaction
two encodings, the block carrying it two ids, and the two implementations two
chains. `kZeroTimeNanos` (`clock.hpp`) is that value, and `marshal_tx` writes it.

The trap is that it looks like an omission rather than a value, so three
independent readers wrote 0: this port, and `conformance/gen`'s hand-built
`writeQEnvelope`, whose comment claimed the bytes were "exactly what marshalTx
emits". They are not. Agreeing with the corpus was therefore no evidence at all
— it was the same guess twice.

## The corpus checks decoding, not emitting

Every Q vector is a byte string the evaluator PARSES, and the id it reports is
`sha256` of the bytes it was handed. So a runtime that decodes correctly and
SERIALIZES differently agrees on every vector in the corpus. That is the exact
shape of the bug above, and it is why the emit-side parity checks in
`test/wire_test.cpp` — `AnUnsignedTransactionSerializesAsGoDoes` and
`ABlockCarryingAnUnsignedTransactionHasGosID` — hold their expected bytes as
constants read off the Go reference's own `marshalTx` and `Block.Bytes`, not off
the corpus. Anything that changes what this port EMITS has to be pinned that
way.

## Nothing the sender wrote decides how its aggregate is checked

`AggregatedSignature` arrives from a peer, so every field in it is a claim.
`signer_count` is the sender's own number and `is_threshold` is the sender's own
routing; neither is evidence. `Core::verify_aggregate` measures the threshold
against the DISTINCT registered validators whose keys actually entered the sum,
which is the only count in the function that had to be earned — each id was
looked up in the committee and its key is under the signature being checked.
Without that, one validator handed over its own real signature, named itself
once, wrote 100 beside it and finalized a block alone.

The repeated-name rule is separate and both are needed. BLS is linear, so t
copies of one validator sum to t·pk and its own signature scaled to t·σ
satisfies the pairing; and a full quorum of distinct signers naming one of its
members once more clears the distinct count while still summing that member's
key twice. `OneSignerCannotSpendItsOwnSignatureTwice` covers both, the second
case being the one only the repeat rule can catch.
