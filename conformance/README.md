# conformance

Two harnesses live here, and they check different things.

- **`make conformance`** — the **consensus-layer** corpus: what a validator
  signs, what a certificate looks like on the wire, what the finality predicate
  decides. It already existed, and all three implementations already pass it.
- **`make chains`** — the **chain-layer** differential, which is new: one
  corpus of P-chain and X-chain bytes, handed to the Go, Rust and C++
  implementations of both chains, with every answer compared against every
  other answer.

Until `make chains` existed, every port checked itself against Go in isolation,
each one deciding for itself which cases to check. That is how a P-chain fork
survived: `chains/cpp/platformvm` executes the sovereign-L1 plane and
`chains/rust/platformvm` refuses it by name, and no test anywhere put those two
answers next to each other.

## The shape

```
conformance/gen        the Go generator and the Go evaluator (its own module)
conformance/corpus     vectors.tsv — the bytes; expected.tsv — Go's answers
conformance/runner     compares answers; understands no chain

chains/rust/platformvm/src/bin/conformance.rs   the Rust P-chain's answers
chains/rust/xvm/src/bin/conformance.rs          the Rust X-chain's answers
chains/cpp/platformvm/test/conformance.cpp      the C++ P-chain's answers
chains/cpp/xvm/test/conformance.cpp             the C++ X-chain's answers
```

**One corpus.** Every vector's bytes come out of a Go constructor —
`txs.New*Tx`, `block.New*Block` — and are signed the way the Go node signs.
Nothing writes a byte by hand except the deliberately damaged vectors, which
are damaged copies of well-formed ones and say so. A corpus written by a third
party would be a fourth opinion about the wire, and there would be nothing to
say which of the four was the chain.

**One line format, three readers.** Both files are tab-separated, for one
reason: the C++ evaluator must read the same file the Go and Rust ones read,
without a JSON library entering a chain's dependency graph.

```
V  <id>  <chain>  <op>  <wire-hex>
R  <id>  <parse>  <kind>  <hash>  <syntactic>  <exec>  <note>
```

`op` is `tx`, `block` or `seam`. The first five fields after the id are
compared; the note is not — it carries each implementation's own words, so a
disagreement can be read without opening three debuggers.

**The runner understands nothing.** It runs programs and compares strings. A
runner that understood the rules would be a fourth implementation, and the day
it was wrong it would hide a disagreement instead of reporting one.

## What is compared

| field | what it is |
| --- | --- |
| `parse` | did these bytes read back as a transaction or a block |
| `kind` | which of the nineteen P-chain kinds, or five X-chain kinds, it is |
| `hash` | the transaction id or block id — `sha256` of the very bytes given |
| `syntactic` | the well-formedness verdict, as a class |
| `exec` | the execution verdict, as a class |

### The verdict vocabulary

`OK · MALFORMED · SYNTACTIC · OVERFLOW · LEDGER · AUTH · WARP · UNSUPPORTED`

Each implementation maps its own error type into exactly one of these before
printing, because "failed to fetch UTXO", `MissingUtxo` and `kUtxoNotFound` are
three spellings of one answer, and comparing the spellings would report a
disagreement that is not one. The mapping is the same word table in the same
order in all three evaluators, and the raw words survive in the note beside the
class — a mapping that flattened a real difference is visible to anyone reading
the row.

`LEDGER` is deliberately coarse. Execution is judged against a chain that holds
nothing: no UTXO, no validator, no network. That is a state all three
implementations stand up identically, and using a funded one would mean
building three state builders and then comparing those instead of comparing
three chains. On an empty chain a missing UTXO and a missing validator are both
just "the chain does not hold this", and splitting them would report which
lookup each implementation happened to reach first as if it were a difference
of opinion.

What an empty chain still separates — and the whole reason it is enough — is
`UNSUPPORTED`: a refusal that never looked at the chain at all, because the
implementation does not run that kind of transaction. A chain that refuses a
kind by name answers `UNSUPPORTED` where a chain that tries to execute it
answers `LEDGER`. That difference is the shape of a fork.

### `SKIPPED` is never a pass

A field an implementation declines to answer prints `SKIPPED`, and the runner
excludes it from comparison and lists it under **NOT COMPARED**. A field no two
implementations answered was not checked by anything here, and the run says so
above the result rather than counting it as agreement.

Today that is the X-chain's `exec` field in C++ alone. Go and Rust both run
the X-chain's semantic pass and then its executor over an empty chain — the
same arrangement the P-chain's vectors are judged under, for the reason above —
so the field IS compared, and a Rust chain that skipped the semantic pass and
answered `OK` where Go answers `LEDGER` fails the run on all five vectors.
Until this was wired, that same broken chain passed: one voice on a field is
not a comparison, and the harness said so under NOT COMPARED rather than
pretending. The C++ evaluator still declines; giving it the same two passes on
its own empty chain is what closes the last voice.

### Seam vectors

Two vectors carry no bytes. They ask each implementation whether its
block-decision seam has a `reject`, and each answers **from its own compiler** —
a method expression in Go and Rust, a `requires` in C++. Nothing about that
answer is typed by hand, and it fixes itself the day the method appears.

A chain that cannot reject a block cannot hand back what that block was
carrying, so the two sides disagree about what is still pending. That is not
visible in any transaction's bytes, so no wire vector could ever catch it.

### Derived wire damage

Every vector carrying a complete encoding — the twenty P-chain transaction
kinds, the four P-chain blocks, the five X-chain transactions and the two
X-chain blocks — is carried again cut short at three points and run long at
two, so length handling is checked once per kind rather than once for the
P-chain's `BaseTx`. The three cuts are a quarter in, half way in, and one byte
short of complete; the two extensions leave one and four unread bytes after
the structure. They are derived from the vector they damage and named after
it: `P_BASE_TRUNC_HALF`, `P_BASE_TRAIL_1`.

Truncation asks whether a decoder notices it has run out of buffer or reads
past the end of one. Trailing bytes ask the opposite and sharper question:
whether a decoder that has finished reading a structure cares that the buffer
has not ended. The two behaviours are indistinguishable on well-formed bytes,
which is why every kind needs both.

What the three implementations agree on today, and what they are now held to:

- Every truncation is refused, on both chains and all thirty-one kinds.
- The P-chain's block decoder refuses trailing bytes and names them —
  `trailing bytes after zap message`.
- The X-chain's block decoder and every transaction decoder on both chains
  accept them. `RewardValidator` is the one transaction that does not, and it
  is not refusing the remainder: it carries no credentials, and the appended
  bytes are read as a credential list that then runs out of buffer.
- Where trailing bytes are accepted, all three compute the id over the buffer
  they were given. `sha256` of the padded bytes is the id, so one transaction
  has as many ids as there are ways to pad it. `P_BASE` and `P_BASE_TRAIL_4`
  are the same transaction under two ids; the second is the vector that was
  hand-written as `P_EDGE_TRAILING_BYTES`, and the derivation reproduces its
  bytes exactly.

## Running it

```
make chains           build all four evaluators, run the differential
make chains-corpus    regenerate the corpus from the Go reference
```

The corpus is committed, so a reference that changed its mind shows up as a
diff. `expected.tsv` — the Go chains' answers at generation time — also joins
the run as one more voice, under the name `corpus`, so drift in Go itself is a
disagreement rather than a silent new normal.

Every evaluator is built before the run and a build that fails stops the
target. A differential that quietly lost one of its voices would report
agreement among whoever was left.

## The one place `luxfi/node` is allowed

`conformance/gen` depends on it. That is the whole point of a reference. It is
a separate Go module for exactly that reason, so it cannot reach node2's own
dependency graph — which `make luxd` still greps and still fails on.

## What it found, and how each one closed

Thirteen vectors disagreed when the harness was written. All thirteen are
closed: the three chains and the corpus now agree on every compared field of
all 208 vectors, and the NOT COMPARED list is empty. What follows is the record
of what it caught, because a differential that reported nothing would be
indistinguishable from one nobody had run.

The two the harness was built to catch:

**The P-chain fork.** On `RegisterL1Validator`, `SetL1ValidatorWeight`,
`IncreaseL1ValidatorBalance`, `DisableL1Validator`, `ConvertNetwork` and the
sovereign form of `CreateNetwork`, Go and C++ executed — reaching the ledger and
failing only for want of state — and Rust answered `UNSUPPORTED`, refusing each
by name. That is the sovereign-L1 birth path every downstream L1 depends on.
`AddPermissionlessDelegator` diverged the same way, through
`NetworkTermsNotHeld`.

CLOSED by the Rust port growing the plane: an L1 validator register, a
network's own staking terms and validator set, and a chain identity to check a
transaction against. The three `…NotHeld` refusals no longer exist as error
variants, and all seven vectors now reach the ledger.

**The X-chain reject.** `X_SEAM_BLOCK_REJECT`: Go and Rust had a reject, C++ did
not — and `P_SEAM_BLOCK_REJECT` showed the same hole on the P-chain. The root of
both was the C++ host seam itself: `lux::node::Block` declared `verify` and
`accept` and no `reject`, so neither C++ chain could be told a block lost.

CLOSED at the seam, which is where it had to be: `lux::node::Block` now
declares `virtual void reject() = 0`, the engine calls it on a block it gives
up on, and both C++ chains override it — xvm re-verifying each transaction
before returning it to the pool, platformvm reissuing its decision transactions
unverified, each following its own chain's reference rather than one rule for
both. The evaluators ask the question of `lux::node::Block` rather than of the
concrete class, so the vector goes green only when consensus can genuinely
reach the reject.

Three more it found that were not on anyone's list:

- **`P_EDGE_WRONG_NETWORK`** — a transaction addressed to network 2 passed the
  Rust P-chain's syntactic check, which Go and C++ both refuse with "wrong
  network ID". A transaction that is well-formed on two networks is one
  signature that spends on both. CLOSED: `syntactic_verify` takes the chain it
  is being verified for, and refuses a transaction naming another network or
  another blockchain.
- **`P_EDGE_EMPTY_NODE_ID`** — C++ refused `AddValidator` by kind before it
  looked at the validator; Go and Rust report the empty node id. The transaction
  is refused either way, for two different reasons, which is a rule that will
  one day accept for the wrong reason. CLOSED: C++ checks the node id first,
  which is the order Go's `standard_tx_executor.go` uses.
- **`P_IMPORT`** — Rust refused `Import` outright where Go and C++ execute it.
  CLOSED: execution takes an `Atomic`, the shared half an import reads from.
  A node with none finds nothing rather than refusing — the answer Go gives
  from an empty shared memory.
