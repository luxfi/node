# chains/rust/zkvm — the Z-Chain in Rust

A port of `~/work/lux/chains/zkvm`, behind the VM seam of `~/work/lux-rs/node`.

```sh
cd chains/rust/zkvm
PATH=~/.cargo/bin:$PATH LUX_LIB_DIR=~/work/lux/crypto/dist cargo test
```

## What is here

| module | what it is |
| --- | --- |
| `zap` | the arena message format, the same one `luxfi/zap` writes |
| `hex` | bytes as text, in the one place that does it |
| `wire` | struct-is-wire over ZAP, byte for byte with Go |
| `tx` | a shielded transaction, its derived identity, its shape rules |
| `block`, `vertex` | the two shapes a decision takes |
| `nullifier` | the spent set — the whole of what stops a double spend |
| `utxo` | the unspent commitments |
| `root` | the committed state root and the fold that advances it |
| `db`, `store` | a durable log, and the chain over it |
| `verifier` | the strict-PQ gate, the proof cache, the public-input binding |
| `groth16` | bn254 pairing verification, in gnark's encoding |
| `starkfri` | the strict-PQ verifier seam, unbound by default |
| `precompiles` | the ZK verifier addresses, gated by the same profile bit |
| `mempool`, `fee` | what waits for a block, and what it costs to get there |
| `vm` | the chain behind `lux_node::vm::Vm` |

## The decisions worth knowing

**The seam is the node's.** `pub use lux_node::vm as host` — the node's own
trait definitions, imported. `Id` is `lux_consensus::finality::Id`, reached
through that seam. A chain that restated either would compile against a shape
the node never sees.

**State is on a device.** `db::Db` is an append-only log of batches (length,
checksum, payload; one write, one fsync) replayed into an index of key →
offset. A read is a `pread`, so a read can FAIL — which is what makes every
"a failed read is not an absence" check in the two sets a real check rather
than a vacuous one. A torn tail is arithmetic: the file is cut back to the last
whole record. A node that stops and starts again remembers what it spent;
`tests/chain.rs` proves it by restarting one.

**One predicate.** `ZkVm::admit` is what assembly asks and what consensus asks.
A proposer that assembled a transaction its own peers refuse produces a block
nobody accepts, forever, because nothing evicts it.

**One profile bit.** `Config::strict_pq` gates the shielded verifier AND the
classical precompile registration. It DEFAULTS TO TRUE, including for a chain
opened with no config at all: the Z-Chain is the shielded settlement chain, and
a permissive deployment has to say so.

**Fail closed, and say which.** With no STARK/FRI binding, a structurally
perfect proof is refused with `VerifierNotRegistered` — never accepted, and
never confused with "that proof is bad". Shielded transfer is disabled on such
a node rather than forgeable.

**The cache is keyed on content.** `Transaction::compute_id` covers the
nullifiers, the outputs and the proof. Keyed on the id as read off the wire —
which is what it used to be — a peer could copy an accepted transaction's id,
proof and public inputs onto a transaction spending different notes and be
answered "verified" from in front of the only check that binds the two.

**No process-global switch.** A chain is a plugin, so it is its own process:
there is no accelerator toggle and no shared verifier registry for a sibling to
write. `root::Root::after` is one SHA-256 fold, never a hardware-conditional
digest — a root that depended on what hardware a node had would make validators
with and without it reject each other's blocks — and `groth16::verify` computes
its public-input combination on the CPU for the same reason.

## What the tests prove

153 of them, and the ones that carry weight are:

`tests/golden.rs` — 12 tests over vectors emitted BY the Go reference
(`testdata/`, with the emitter beside its output). The wire, every transaction
identity, the state root fold, block and vertex ids, gnark's point encoding, and
a Groth16 instance the reference's own gnark pairing accepted. All of it
matches.

Sixteen of those vectors are ADVERSARIAL, and they are the ones that decide
whether the verifier is a verifier. Fifteen are the good instance with one
thing wrong — a proof element moved, a point at infinity (a pairing drops that
term, which is how an all-zero key reports every proof valid), a point on the
twist but off the prime-order subgroup, a non-canonical coordinate, a short
proof, a key whose point count overruns its bytes, a key for another circuit —
and one is the control. Go's verdict on each was computed by Go over those
exact bytes and recorded beside them; this crate reaches the same sixteen. Go
accepts exactly one, and so does this.

`tests/differential.rs` — the Z-chain's answers to the shared corpus in
`conformance/corpus/`, printed as the `RESULT` lines the cross-language harness
reads. Each vector is parsed, re-encoded and compared byte for byte with the
wire it came from, and its identity is recomputed and compared against the CB58
the Go generator recorded. A disagreement prints `REJECTED` with the reason and
fails the run.

`tests/chain.rs` — the chain, run on a file: a transaction becomes a block
becomes state; a node restarts and still refuses the spend; a block off the tip,
a wrong state root and a duplicated nullifier are each refused; the genesis
allocation happens once; two chains with the same genesis are two chains.

## What is deliberately not here

Note construction — deriving a nullifier, committing to a note, encrypting one
to a recipient. That is a wallet's work; a validator holds no spending keys.

The STARK/FRI verifier itself. It is a separately audited artefact reached
through `starkfri::Backend`, and it is not in this crate to be reimplemented.

PLONK's verification equation. It is not implemented, so a PLONK proof is
refused — in the verifier and at the precompile. The structural parse in front
of the refusal is there so a caller hears "malformed" before "not implemented",
and nothing beyond it pretends to be a check.

## Where this and Go read differently

**The genesis document.** Go unmarshals `initialTransactions` as JSON
transaction OBJECTS — which drags in Go's own encodings for the fields inside
them: `[]byte` as base64, and `ids.ID` as base58-with-checksum. This reads them
as the hex of the transaction's WIRE bytes instead, so genesis and the network
carry a transaction the same way and there is one encoding of a transaction
rather than three.

That is a real divergence and it is stated here rather than buried: a genesis
file written for the Go node does not parse here. Closing it means base58check
and Go's base64 `[]byte` convention, and the honest order is to decide which
genesis form is canonical first.

**`setupParams`.** Go's genesis declares it — powers of tau, a PLONK SRS, FHE
public parameters — and NOTHING in the reference ever reads it. A field that
changes nothing reads as a control and is not one, so it is absent here.

## Not yet done

**`make chains` has no Z lane, and it is blocked on the reference, not here.**
That target hands `conformance/corpus/vectors.tsv` to evaluator BINARIES and
compares their rows; the Go evaluator is `conformance/gen`, a separate module
that reaches the reference as a library. It can build a Z transaction and ask
the reference for its identity — but it cannot PARSE one, because
`github.com/luxfi/chains/zkvm` exports no parse entry point: `parseTransaction`
and `parseBlockBytes` are unexported, where the P and X chains reach theirs
through `luxfi/node`. An evaluator that re-derived a transaction from a
construction table instead of parsing the bytes would be a second
implementation of the wire, which is the one thing a differential must not
contain.

So the Z lane needs, in this order: exported `ParseTransaction` and
`ParseBlock` on the reference; then a Z generator and evaluator in
`conformance/gen`; then a `conformance` bin here on the same TSV protocol the
other four speak. The first step is a change to another repository and is not
on this branch.

What IS answered today: `tests/differential.rs` prints the `RESULT` rows for
the Z vectors in `conformance/corpus/chain_differential.json`, recomputing each
identity rather than restating it — and the adversarial table in
`tests/golden.rs` is a proof-verification differential against the reference
that does not need a parse entry point, because it runs inside the reference.
