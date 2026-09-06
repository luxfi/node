# zkvm — the Lux Z-Chain in C++

The shielded-settlement chain, ported from the Go reference
(`~/work/lux/chains/zkvm`) and standing behind `lux-cpp/node`'s VM seam
(`include/lux/node/vm.hpp`). The seam is IMPORTED, never restated: `Block` and
`VM` here are that header's types, and `Id` is `lux::consensus::Id`.

## What the chain is

A Zcash-shaped shielded pool over a UTXO set:

- a transaction spends notes by naming their **nullifiers** and creates notes as
  **commitments**, with an optional transparent side for shield/unshield;
- a **zero-knowledge proof** attests that the spend is valid, bound to this
  chain and to this transaction's nullifiers and commitments;
- the **spent set** is the whole of what stops a note being spent twice, and is
  permanent — there is no removal path, not for reorg, not for compaction;
- the **state root** is one SHA-256 fold, and only acceptance advances it.

## The profile bit

`ZConfig::strict_pq` is one bit and it drives two switches at once:

- the shielded-tx verifier refuses every classical (bn254 pairing) system and
  accepts only STARK/FRI;
- a real bn254 verifying key is refused **at construction**, not tolerated with
  a warning.

**The default is strict.** A permissive deployment must say so explicitly. A
CRQC that breaks bn254 must not be able to forge a shield or unshield proof and
mint shielded value.

Under strict-PQ the STARK/FRI verifier is a *binding* (`starkfri.hpp`), exactly
as it is in Go: an out-of-band Plonky3-derived verifier registers through it. With
nothing registered, `verify` returns "verifier not registered" — never "ok". That
is the shipped posture of the reference in every build without the prover
binding, and it is the correct one: no classical fallback, no forgeable path.

## Layering

Each layer below is REUSED in place, not vendored:

| layer | what it gives |
|---|---|
| `luxcpp/crypto` | sha256; the first-party bn254 tower + optimal-ate pairing |
| `lux-cpp/consensus` | the `Id` the seam speaks |
| `lux-cpp/node` | `include/lux/node/vm.hpp` — the seam itself |

And, new here:

```
zap        the zero-copy structural codec (header-only)
wire       the canonical / agreement rules every frame is held to
txs        the shielded transaction and the identity it is decided by
store      the durable map (Memory, File) and the View a decision writes through
utxo       the shielded outputs
nullifier  the spent set
root       the state root a validator signs
mempool    what is pending, and the order a proposer takes it in
starkfri   the strict-PQ STARK/FRI verification seam
groth16    the classical system's encoding and point discipline
verifier   the profile gate and the proof cache
chainstore how a decision becomes fact
block      the linear block
vertex     the DAG shape
vm         the seam implementation
```

## The invariants worth knowing

**An identity is a function of content.** A transaction's id, a block's id and a
vertex's id are all derived, never read off the wire. The proof cache is keyed on
the transaction's id, so a peer that could choose it could reach an accepted
answer with a transaction spending different notes.

**Every id opens with the chain binding**, `sha256(ChainID ‖ NetworkID)`, which
is not on the wire. Two chains with an identical genesis config still have
different genesis ids, so one chain's blocks cannot chain onto the other's.

**One value has one byte string.** Every parser refuses a frame the buffer does
not exactly account for, and every length vector must cover its blob exactly.
Without both, one logical value has unboundedly many encodings under one id.

**A declared length never sizes an allocation.** Reservations come from the
length vector, which the frame already bounds.

**A read that failed is not an absent row.** The store returns
`Result<optional<Bytes>>` throughout: an error is "the disk is gone", an empty
optional is "no such row". On this chain the difference is a double spend.

**"Has this note been spent?" has ONE answer.** Both the read in front of a
transaction and the write that spends the note ask `spent_at`, so the records
decide both. `mark_spent` used to consult the in-memory set alone, which is
strictly weaker than the read beside it: a set that is not a superset of the
records answers "unspent" for a note the records already hold, and the write
then overwrote that record with a new height and returned success — the note
spent twice, with nothing anywhere saying so.

**write is separate from publish.** `write` stages every durable change through
the view and may fail, in which case it is discarded whole; `publish` runs after
the commit and cannot fail. That is what makes a half-applied block unwritable.

**admit is ONE predicate.** Assembly and verify both ask it, so a proposer never
assembles what its own peers refuse.

**The root has one definition.** A hardware-conditional digest would make the
consensus-committed root depend on whether a node has an accelerator, so
validators with and without one would reject each other's blocks. The CPU fold
is the root, always; nothing here needs or consults a GPU kernel.

## Persistence

Not memory-only. `store::File` appends one ZAP record per commit and replays the
log on open; a process killed mid-append leaves a partial trailing record, which
replay detects and truncates. `store::View` is the versioned overlay a decision
writes through. The chain store commits the decision, its height entry and the
tip pointer in that one batch, so a restarted node knows where it is and a
signature from a past height can still be checked.

## What is verified, and what is refused by name

Groth16 is verified here, on the reused first-party bn254 tower: the encoding is
gnark-crypto's, the point discipline is the reference's, and the equation is one
Miller loop over `e(-A,B)·e(α,β)·e(LC,γ)·e(C,δ) = 1`. The `tamper` suite is the
evidence — an acceptance, and then proofs that DECODE and are still refused.

**PLONK is refused, by name, and that matches the reference.** The Go Z-chain's
`plonk` case is `errPLONKIncomplete`, and its PLONK precompile parses seven G1
commitments and an SRS G2 point and then returns `errPLONKVerifierIncomplete` —
there is no PLONK verification equation anywhere in the reference. Writing one
here would make this chain accept proofs the reference refuses, which is a fork,
not a port. So the refusal is the whole of what the path does and the whole of
what it says. Landing PLONK is a change to the REFERENCE first.

Bulletproofs are the same shape, and the STARK/FRI path is a binding: with no
P3Q verifier registered it answers "verifier not registered", never "ok".

## Tests

`ctest` in `build/`. Fourteen suites, 705 assertions; `golden` is the contract
with Go. Clean under `-DZKVM_SANITIZE=address,undefined` with
`ASAN_OPTIONS=detect_leaks=1`, tests and the corpus evaluator both.

`tamper` is the adversarial suite. Every other suite's refusals are decided in
FRONT of the arithmetic — too short, off the curve, at infinity, no key — so a
`verify` ending in `return {}` passes all of them, which is what the first port
of this chain did. `tamper` only asks questions the pairing has to answer: a
point swapped for its own negation, a point swapped for another valid one, A and
C exchanged, one bit flipped in each of the proof's eight coordinates, each
public input moved by one, α negated in the key, K[0] replaced. It opens with an
ACCEPTANCE, because a verifier that refuses everything is as broken as one that
accepts everything and only a suite holding both verdicts can tell them apart.

Proven able to fail: with `verify_pairing` short-circuited to `return {}`,
`tamper` reports 14 failures, `groth16` 8 and `verifier` 1. With `mark_spent`
back on the in-memory set alone, `state` reports 3 — a note spent twice, its
record overwritten at the second height, and a spend proceeding against a store
whose reads fail.

`test/golden.hpp` is GENERATED by `test/golden/golden_gen.go`, which builds the
values with the Go Z-Chain itself — including a chain that verifies and accepts a
real block — and prints them as a C++ header:

```
cp test/golden/golden_gen.go ~/work/lux/chains/zkvm_golden_gen.go
cd ~/work/lux/chains && GOWORK=off go run ./zkvm_golden_gen.go > .../test/golden.hpp
rm ~/work/lux/chains/zkvm_golden_gen.go
```

The corpus pins transaction bytes and ids, block bytes and ids, the UTXO record,
the chain binding, the genesis id and root, gnark-crypto's compressed and
uncompressed point encodings, the scalar field's byte reduction, and a Groth16
proof that SATISFIES the pairing equation — so the arithmetic here is checked
against an acceptance, not only against refusals.

## Deliberate differences from the reference, and why

- **Genesis configuration is ZAP, not JSON.** JSON is the Go host's configuration
  encoding; what the genesis block's identity binds is the timestamp and the
  initial transactions, never the buffer they arrived in. A Go node and this one
  configured with the same values reach the same genesis id — the `golden` suite
  asserts exactly that.
- **The pool breaks fee ties on the id.** Go's heap leaves equal fees in an
  unspecified order, which is a proposer that cannot reproduce its own choice.
- **No FHE co-processor.** The Go package carries one (`zkvm/fhe`, a third of
  its lines). It is a different machine — threshold-decryption committees and a
  ciphertext ALU — that happens to live in the same directory, and nothing in
  the shielded settlement path calls it. It is not stubbed here; it is absent,
  and a caller looking for it finds nothing rather than something that returns
  success.
- **No HTTP surface.** The Go VM exposes JSON endpoints; in this stack the node
  owns RPC (`lux/node/rpc.hpp`) and a chain answers the seam. Nothing here is a
  stub standing in for one.

## The differential

`make chains` from the repo root hands the shared corpus
(`conformance/corpus/vectors.tsv`) to this chain's evaluator,
`build/zkvm_conformance`, and to the Go Z-chain's, and compares every field.
137 Z vectors: the three transaction types, blocks that carry one, two and a
hundred and one of them, both ways one shielded note can be spent twice inside a
block, every refusal `ValidateBasic` has, the block's own shape (state root,
parent at height zero, a timestamp past the skew bound), the frame truncated and
extended at five places each, the canonicality of the frame in both directions,
and the decision seam.

They are BLOCKS rather than transactions because the Go reference's transaction
parser is not exported, and a corpus that restated the frame to get at it would
be a second opinion about the wire. A block carries its transactions, so
parsing one parses them.

Each vector meets a chain stood up fresh over `store::Memory` at height 0 with
an empty parent, on the CONSTRUCTED DEFAULT profile — strict-PQ. The evaluator
states the chain id and the network id, because a block id opens with
sha256(ChainID ‖ NetworkID) and that is not on the wire; it does not state the
profile, so a change to the chain's default moves the evaluator with it.

**`syntactic` and `exec` are cut out of ONE verdict.** The corpus asks whether a
refusal was decided from the block's own bytes — its height, its clock, its
transactions — with nothing asked of the chain. So the evaluator runs `check()`,
the whole verdict, once, and splits its answer on `block_alone`, built from the
chain's OWN error constants rather than a list of the same words written out
again beside them.

It used to run a per-transaction `validate_basic` pass in front of `check` and
call that the syntactic answer. That is a DIFFERENT question, and it answered
"the block is fine" for four vectors the reference refuses on the block's own
bytes: `Z_BLOCK_GENESIS_WITH_PARENT`, `Z_BLOCK_TIME_AHEAD`,
`Z_BLOCK_DUPLICATE_NULLIFIER` and `Z_TX_EXPIRED`. None of the four is about a
transaction in isolation, so no per-transaction pass could ever have seen them.
Those were the differential's only four disagreements; every compared field of
all 629 vectors agrees now.

Proven able to fail: built permissive (`z.strict_pq = false`) the evaluator
disagrees with Go and with the corpus on `Z_BLOCK_TRANSFER_GROTH16 . exec` and
the run exits non-zero.

`make chains` itself still exits non-zero, for a reason outside this chain: the
Rust column prints no row at all for Q, Z, D or F, and the runner fails rather
than call a silent column an agreeing one. Those evaluators are `chains/rust`'s
to write.
