# testdata — what the Go reference actually produced

`golden.json` was written by `~/work/lux/chains/zkvm`. Not by a person reading
that code and retyping constants: a retyped constant proves two people agree
about what a program says, which is not the question. The question is whether
these two implementations put the same bytes on a wire, and only the reference
can answer it.

`gen/` is the emitter, kept beside its output so the answer can be asked again.

## Regenerating

The emitter runs INSIDE the reference package — it needs `computeID`,
`serialize` and the `Root` fold, none of which are exported — but nothing is
written into the reference checkout. Go's `-overlay` maps the two files here
onto paths in that package for the duration of one command:

```sh
REF=$HOME/work/lux/chains
OUT=$PWD/testdata/golden.json
cat > /tmp/zkvm-overlay.json <<EOF
{"Replace": {"$REF/zkvm/golden_test.go": "$PWD/testdata/gen/emit_test.go"}}
EOF

cd $REF && GOWORK=off ZGOLD_OUT=$OUT \
  go test -overlay=/tmp/zkvm-overlay.json -run 'TestEmitGolden|TestCheckGoldenInstance' -v ./zkvm/
```

The output is byte-identical to what is committed here, which is the point: a
corpus that moved shows up as a diff rather than as a silent new normal.

`git status` in the reference checkout stays clean; an overlay is a build-time
map, not a write.

## The two tests

They are ONE file, in this order. Go runs a file's tests in the order they are
written; across files it runs them in filename order, which put the check ahead
of the emitter and had it read a file that did not exist yet.

`TestEmitGolden` writes the vectors: the chain binding, four transactions and
their identities, a UTXO, three state-root folds, three blocks, a vertex, gnark
point encodings, a Groth16 instance, sixteen ADVERSARIAL instances,
twenty-five GENESIS documents, and eighteen calls to the verifier
PRECOMPILES.

The adversarial table is the half that matters most, because a verifier that
accepts everything passes any test made only of good proofs. Each case is the
good instance with one thing wrong — a proof element moved, a point at infinity
where the pairing would then drop the term, a point on the twist but outside
the prime-order subgroup, a coordinate that is not a field element, a proof one
byte short, a key whose point count overruns its own bytes, a key for a
different circuit — and one case is the good instance itself, as the control.

The verdict beside each case is COMPUTED, not typed: `judge` runs the
reference's own four steps (the length bound, the key, the proof, the pairing)
over those exact bytes and records what Go answered, as
`adv.<case>.verdict` = `01` accepted / `00` refused. Go accepts exactly one of
the sixteen. `tests/golden.rs` runs the same four steps here and asserts the
same sixteen answers, so "a proof Go refuses is refused here" is a checked
claim rather than a hope.

The genesis documents are the same idea one level up. A genesis file is an
OPERATOR's, and the same file goes to every implementation of this chain, so
what it produces — the id of block zero, the initial state root, the first
shielded outputs — has to come out the same, and what it is REFUSED for has to
be the same too. Each `genesis.<case>.doc` is a whole document; the accepted
ones were written by Go's own encoder, so the shape recorded is the shape the
reference reads. Beside each is `.verdict`, and for an accepted one `.stamp`,
`.ntx`, then per transaction `.wire` (every decoded field, through the
reference's serializer), `.id_carried`, `.id_computed`, and the `.utxo<i>`
records genesis seeding writes — which are keyed on the id the FILE names, not
on a recomputed one. `.root` and `.block_id` close it. Eleven of the
twenty-five documents are refused, including a transaction given as its own
wire bytes in a hex string: the wire form belongs to a peer, and a genesis
names fields.

The precompile calls record something a byte alone does not say. `Run` answers
a byte AND an error, and they are different facts: `0x00` with no error is
"that proof does not verify", while an error is "this call could not be judged"
— in an EVM, the difference between a `false` and a revert. So each
`pc.<case>` carries `.input`, `.out`, `.err` and `.gas`, and the port has to
match all four. This is what pins the line the reference draws in its PLONK
verifier, where a bad FRAME is an error and everything the verification itself
refuses is a verdict.

`TestCheckGoldenInstance` reads them back and runs the REFERENCE's own verifier
over the Groth16 instance — gnark's pairing, on these exact bytes. It asserts
that the good instance verifies and that the mutated one does not. Without it,
`groth16.proof1` would only be a byte string that Rust happens to accept; with
it, the two implementations are known to agree about the same statement.

## Why the Groth16 instance is real

With β = γ = δ = g₂ the verification equation collapses to an exponent
identity, so choosing A = g₁^(a + Σ + c) satisfies it exactly. That is a
genuine satisfying assignment rather than a shape that resembles one — no
trusted setup is needed to produce it, and the thing under test is the
verifier, not the prover.
