# conformance

Two harnesses live here, and they check different things.

- **`make conformance`** — the **consensus-layer** corpus: what a validator
  signs, what a certificate looks like on the wire, what the finality predicate
  decides. It already existed, and all three implementations already pass it.
- **`make chains`** — the **chain-layer** differential: one corpus, covering
  six chains, handed to every implementation of each of them, with every answer
  compared against every other answer.

Until `make chains` existed, every port checked itself against Go in isolation,
each one deciding for itself which cases to check. That is how a P-chain fork
survived: `chains/cpp/platformvm` executes the sovereign-L1 plane and
`chains/rust/platformvm` refuses it by name, and no test anywhere put those two
answers next to each other.

## The six chains

| | vectors | Go | Rust | C++ |
| --- | --- | --- | --- | --- |
| P platformvm | 159 | yes | yes | yes |
| X xvm | 49 | yes | yes | yes |
| Q quantumvm | 81 | yes | **no** | yes |
| Z zkvm | 137 | yes | **no** | yes |
| D dexvm | 35 | yes | **no** | yes |
| F fhevm | 264 | yes | **no** | yes |

Most of those counts are damage. Every vector this reference reads back as a
block or a transaction is also cut to a quarter, cut to a half, cut by one
byte, extended by one and extended by four — which is where a parser reads past
a buffer. Which vectors get that treatment is not a list anyone maintains: a
vector is damaged if the reference parses it, because half of a truncation is a
truncation and a vector already damaged on purpose is one.

F's count includes an `F_JSON_*` group added after the C++ port and the
reference were found to disagree about what their JSON decoders accept. Each is
a correct transaction with one thing done to its payload — a stray closing
brace, a member name folded by `unicode.SimpleFold` rather than by ASCII case,
two keys naming one field, a discarded array element of the wrong type, a base64
word broken across a line, a literal `null` where a struct belongs — and every
one of them is a transaction the Go chain ADMITS, so an implementation that
refuses it is named rather than quietly stricter. See `chains/cpp/fhevm/LLM.md`.

P and X come from `luxfi/node`; Q, Z, D and F from `luxfi/chains`. Both are
PUBLISHED versions and there is no replace directive, so the corpus regenerates
on any machine rather than on one.

The four chains were added because they had **no vector at all**, which is the
same shape the P-chain fork hid in for weeks: a chain nothing is pointed at
agrees with itself. Every fork this program has found, the differential found.

**The Rust column does not answer them, and the run FAILS because of it.** The
runner lists what each implementation never answered under NOT ANSWERED and
exits non-zero: silence is not agreement, and a target that went green while a
whole column said nothing about four chains would be reporting the agreement of
whoever was left. Those four evaluators are `chains/rust`'s to write. Nothing
else is waiting on them — the corpus, the runner and the result format are the
ones already in use, and each slots in as one more `-eval "rust=…"` line.

D is not a block chain here. Go's `dexvm` is a REGISTRY: it decides what an
asset IS, what a market IS, which kinds may be registered, and whether native
value may activate. Those are consensus decisions with no wire of their own —
two implementations deriving different bytes for one asset have forked the
value plane without ever disagreeing about a transaction — so a D vector's wire
column carries the ARGUMENTS to a derivation rather than a serialization.

## The shape

```
conformance/gen        the Go generator and the Go evaluator (its own module)
conformance/corpus     vectors.tsv — the bytes; expected.tsv — Go's answers
conformance/runner     compares answers; understands no chain

chains/rust/platformvm/src/bin/conformance.rs   the Rust P-chain's answers
chains/rust/xvm/src/bin/conformance.rs          the Rust X-chain's answers
chains/cpp/platformvm/test/conformance.cpp      the C++ P-chain's answers
chains/cpp/xvm/test/conformance.cpp             the C++ X-chain's answers
chains/cpp/quantumvm/test/conformance.cpp       the C++ Q-chain's answers
chains/cpp/zkvm/test/conformance.cpp            the C++ Z-chain's answers
chains/cpp/dexvm/test/conformance.cpp           the C++ D-chain's answers
chains/cpp/fhevm/test/conformance.cpp           the C++ F-chain's answers
chains/cpp/conformance/include/…/corpus.hpp     the format, the verdict words
                                                and the error-word table, once
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

`op` is `tx`, `block`, `seam`, `identity`, `genesis`, or — on D — the name of
the derivation being asked for. The first five fields after the id are
compared; the note is not, and it carries each implementation's own words so a
disagreement can be read without opening three debuggers.

### What is NOT on the wire is corpus contract

Three of the six chains hash something that never travels into every id they
derive. The Z-chain's block id opens with `sha256(ChainID ‖ NetworkID)`; the
F-chain does the same and binds its signing preimage to the chain id besides;
the Q-chain carries the pair in the block and refuses a block whose pair is not
the one the node serves. An evaluator that picks its own numbers therefore
derives a different id for every well-formed vector on that chain.

That is not hypothetical. The first Z run disagreed on the id of all
twenty-five vectors and on none of the malformed ones — which is what a hash
fork looks like — because one evaluator had been built for chain 40 and the
other for chain 4.

So the identity is stated once, in `conformance/gen/identity.go`, and asked
back as a vector: `Q_CHAIN_IDENTITY`, `Z_CHAIN_IDENTITY`, `F_CHAIN_IDENTITY`.
Each evaluator prints the numbers IT was built with, never the ones the corpus
asked about, so a mismatch is one row naming both and the rows underneath can
be read for what they are.

`F_GENESIS` goes further and carries the genesis bytes themselves. An F
transaction is judged against a funded payer, a committee, a threshold and a
network key, none of which is in its bytes; both evaluators stand their chain
up on those exact bytes, and the vector's answer is the id the chain's own
genesis block takes — so "we applied the same configuration" is a compared
field rather than an assumption under all the others.

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
make chains           build all nine evaluators, run the differential
make chains-corpus    regenerate the corpus from the Go reference
```

**`make chains` fails today, and it should.** Four chains have no Rust
evaluator, the runner lists 421 vectors under NOT ANSWERED, and it exits
non-zero. Silence is not agreement.

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

## What it found on the four new chains

The first thing to say is that it bites. Flip one nibble of one vector's wire
on each of Q, Z, D and F, and the derived id moves on all four; Go and C++
independently compute the SAME new id, and both differ from the corpus. A
harness that could not do that would agree with everything.

**Q — 81 of 81 agree.** The wire, the canonical re-encode, the block id, the
chain binding and the ML-DSA verification all match. What the differential
caught was in the evaluator, and it is worth writing down: built on a
default-CONSTRUCTED `Config` rather than the chain's `default_config()`, the
C++ Q-chain accepted four blocks Go refused — an expired quantum stamp, a
duplicate transaction, and an unsupported ML-DSA parameter set. The zero value
of that struct has `quantum_stamp_enabled` false, and `Config::validate`
normalises every other unset field to its default and leaves that one alone.
A Q-chain configured by omission checks no post-quantum signature, and nothing
downstream says so.

**D — 35 of 35 agree.** Every asset id, every market id, the kind and mode
parsers, the network class and the value-activation guard.

**F — 168 of 168 agree.** The six operations, every payload rule, the four
ways a signature can fail to be the payer's, and the id the genesis block
takes.

**Z — 137 vectors, 133 fully agree, 4 disagree on one field.**

`Z_BLOCK_TIME_AHEAD`, `Z_BLOCK_GENESIS_WITH_PARENT`,
`Z_BLOCK_DUPLICATE_NULLIFIER` and `Z_TX_EXPIRED`: Go answers `syntactic
SYNTACTIC`, C++ answers `syntactic OK`. Both refuse the block, both give the
same reason, and `exec` agrees on all four.

This one is the corpus's, not either chain's, and it is left visible rather
than tuned away. The Z-chain reference has ONE pass: `Block.Verify` runs the
shape rules, then the proofs, then the parent lookup, and returns the first
refusal. There is no syntactic pass to read, so each evaluator invented the
split and they invented it differently — Go by matching the sentinels it knows
are decided before any lookup, C++ by calling the per-transaction
`validate_basic` the port happens to have. Three of the four rules are
BLOCK-level (a height-0 block with a parent, the clock, a nullifier repeated
across two transactions) and could not live in a per-transaction pass at all.

The fix belongs above this cell: either `syntactic` is defined once for Z as
"what Verify decided before it read the chain" and both evaluators answer that
question, or Z declares the field `SKIPPED` and it appears under NOT COMPARED —
which is honest, and is not a pass.

## What it found on P and X, and how each one closed

Thirteen vectors disagreed when the harness was written. All thirteen are
closed: the three chains and the corpus now agree on every compared field of
all 208 P and X vectors, and the NOT COMPARED list is empty. What follows is
the record of what it caught, because a differential that reported nothing
would be
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
