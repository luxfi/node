# lux-quantumvm — the Lux Q-Chain in Rust

The post-quantum lane of the parallel-witness finality model (LP-020). Ported
from the Go reference at `~/work/lux/chains/quantumvm` (6,470 lines), against
the object-safe VM seam of `~/work/lux-rs/node` (`src/vm.rs`).

6,875 lines across 14 files. 126 tests, all green. No stubs.

## Build

```
PATH=~/.cargo/bin:$PATH cargo test
PATH=~/.cargo/bin:$PATH cargo build --release --bin conformance
```

The one thing this crate needs that the X-chain does not is `libluxcrypto`: the
ML-DSA-65 signature is FIPS 204 from `lux-pq`, the same library the Go node
signs under, reached by path. `build.rs` repeats the loader path `lux-pq`
resolved, so the test binaries load without `LD_LIBRARY_PATH`.

## Two signatures, and they are not the same thing

- A **transaction stamp** is one validator's ML-DSA-65 signature over one
  transaction, bound to the moment it was made. It rides on the wire beside the
  transaction it covers. `src/quantum.rs`.
- A **round witness** is a quorum of the committee signing one block, whose
  signatures aggregate into a single certificate. It is deliberately NOT a field
  of a block: the block id is the hash of the block's own bytes, so a signature
  inside it would give two honest nodes two ids for one block. `src/quasar.rs`.

Q-Chain sells no blockspace. `Qvm::issue_tx` refuses every user transaction
whatever it offers (LP-0130 §6); the chain advances only through
consensus-internal aggregation, which reaches the pool through `Qvm::admit`.

## What proves it is the same chain

`tests/vectors/q_reference.json` — five vectors whose every byte came out of the
GO chain: the wires are what `chains/quantumvm`'s encoder writes and the
verdicts are what its parser answers, ids included. `tests/differential.rs`
holds this crate to them and fails on any divergence.

`src/wire.rs` carries the Go encoder's own bytes for a transaction preimage and
for the envelope around an unsigned one, as constants, asserted byte-for-byte.

`src/bin/conformance.rs` is the Q evaluator for the cross-language harness
(`conformance/corpus/vectors.tsv` → `R` rows), and `tests/corpus.rs` runs it
from `cargo test` and holds it to Go's recorded answers on all 81 Q vectors.
Go, Rust and C++ agree on every compared field, block ids included.

`exec` is the one field this evaluator declines: acceptance is decided against a
parent and a clock, and it stands up no chain to hold either. It prints
`SKIPPED`, the runner excludes it, and Go and C++ still answer it — so the field
is compared, just not by this voice.

**The chain identity is corpus contract.** A Q block names its chain and its
network on the wire and the chain refuses one whose pair is not the one the node
serves, so the two numbers decide the verdict of every well-formed vector. They
are declared once in the evaluator and feed both the `Q_CHAIN_IDENTITY` row and
the binding check, so an evaluator pointed at the wrong chain says so in one row
rather than answering "belongs to another chain" eighty times.

## Layout

```
zap.rs        the serialization: a fixed section of known offsets plus a tail
wire.rs       the block and transaction encodings, and canonical-or-nothing parse
ids.rs        Id — lux_consensus::finality::Id, the host's own
quantum.rs    ML-DSA-65: keys, the stamp, what the signature covers, batches
tx.rs         a transaction, the pool, and the triage that fills a block
block.rs      a block, and the rules decidable from it and its parent
quasar.rs     the committee, the threshold, and the aggregate that finalizes
store.rs      what the chain keeps, on disk, one all-or-nothing commit at a time
vm.rs         the chain behind the host seam, and the JSON-RPC it answers
config.rs     the parameters, and what an unset one settles on
error.rs      every refusal this chain can make, and its shape at the seam
```

## Decisions worth knowing

**The id type is the host's.** `Id` is `lux_consensus::finality::Id`, named from
the crate the node names it from. A chain that minted its own 32-byte name would
need translating at every call, and a node holding two such chains could not put
them in one map.

**The seam is imported, not restated.** `pub use lux_node::vm as host`. A
restated seam compiles against a shape the node never sees, and keeps compiling
on the day the node changes a method.

**Verify/accept/reject are on the VM, keyed by id** — not on the block. That is
what the wire already does: those three travel as messages carrying an id,
because a block handle cannot cross a process boundary.

**One commit, not a sequence of writes.** `Store::commit` takes the whole batch:
block, height index and tip pointer move together or not at all. `store::Log`
extends that across a crash — each batch is length-prefixed and digested, and a
torn tail is dropped whole at replay.

**A missing key and a store that could not answer are different facts.**
Collapsing them is how one transient read failure commits genesis over a live
chain: the tip reader answers the empty id for exactly one reason, and an error
for every other.

**Genesis is a constant of the chain**, not of the moment: height 0, time 0,
empty parent, this chain, this network. Wall-clock time there would give every
node a different id for the same block.

## Where this runtime is narrower than Go, and why

- **ML-DSA-44 and ML-DSA-87 cannot be signed under here.** `libluxcrypto`
  exposes ML-DSA-65 only. Asking for either is an error that says so — never a
  silent downgrade to 65, which is the whole point of refusing an unknown
  parameter set. ML-DSA-65 is what Q-Chain validators hold.
- **There is no GPU batch path.** The accelerator's ML-DSA batch kernel is
  reachable from the Go runtime only, so batch verification here is CPU-parallel
  across threads. `gpu_batch_threshold` stays in the config because it is part
  of the chain's configuration surface; this runtime reports it without reading
  it, and says so.
- **The committee's registration path mints keys locally**, as the reference's
  does, and it is the only one. `Quasar::add_validator` takes a name and a
  weight and makes the key pair, mirroring `AddValidator` in
  `chains/quantumvm/quasar.go`. There is deliberately no door that takes a key
  off the wire: aggregate BLS is sound only over keys the verifier chose, so a
  registrant free to name its own public key names `pk_a − Σpkᵢ` and signs a
  whole quorum by itself, and the same key under two names turns one honest
  signature into two of the threshold's slots. Minting makes both
  unrepresentable. Admitting a published key would need a proof of possession
  bound to the name registering it — and would put members in this committee
  that the reference cannot hold, which is a disagreement about which
  certificates verify.
