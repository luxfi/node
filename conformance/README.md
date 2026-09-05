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

## Timing it

`make chains` asks whether the three implementations agree. `make bench` asks
how long each of them takes to answer, over exactly the corpus they agreed on.

```
make bench            build the evaluators, time each one, print the table
```

Every evaluator takes an optional repeat count after the corpus path:

```
conformance/gen/gen eval conformance/corpus/vectors.tsv 200
chains/rust/platformvm/target/release/conformance conformance/corpus/vectors.tsv 200
chains/cpp/xvm/build/xvm_conformance conformance/corpus/vectors.tsv 200
```

With a count it walks the whole corpus that many times and prints one extra
line to **stderr**, where the differential's runner does not read:

```
B  <impl>  <vectors>  <repeats>  <seconds>
B  rust/platformvm  159  20  0.286851
```

Without a count nothing changes: one pass, no timing line, the same verdicts on
stdout. And the verdicts are byte-identical at any count — every round computes
them, the last round's are printed — so `make chains` reads the stream it always
read.

### What is measured

The differential's own work, and only that: for each vector, decode the hex,
parse the wire, verify it, and — as far as the implementation goes — execute it
against the empty chain. Each evaluator runs its own monotonic clock around its
own loop, starting after the corpus has been read and split into fields, and
stopping before the first verdict is printed.

### What is not measured

Process start: the dynamic loader, the Go runtime coming up, the 27 MB of
linked reference the Go evaluator carries. Reading and splitting `vectors.tsv`.
Formatting and writing the result lines. Process exit. The build.

That exclusion is not a detail. Timing each evaluator's whole process from the
outside at one repeat, fastest of twenty, against what its own clock reported:

| evaluator | whole process | the work | everything else |
| --- | --- | --- | --- |
| go | 63.9 ms | 20.3 ms | 43.6 ms |
| rust/platformvm | 28.1 ms | 17.7 ms | 10.3 ms |
| rust/xvm | 7.8 ms | 0.3 ms | 7.5 ms |
| cpp/platformvm | 39.2 ms | 25.4 ms | 13.7 ms |
| cpp/xvm | 5.9 ms | 0.2 ms | 5.7 ms |

Go pays 43.6 ms for things that are not chain work — four times what the C++
X-chain's entire process costs, and more than the C++ P-chain's entire process.
A benchmark that timed the processes would have reported that as Go being slow
at the P-chain.

### Where the comparison is fair

One corpus, byte for byte, the same vectors in the same order. `make chains` is
the proof that it is the same work: all three produce the same five compared
fields for all 208 vectors, so nobody is being timed while quietly answering an
easier question. Each evaluator is run five times and the runs are interleaved —
every evaluator once, then every evaluator again — so a machine that slows down
halfway through slows all of them down rather than one.

### Where it is not

**The C++ X-chain does less work than the other two.** `chains/cpp/xvm` answers
`SKIPPED` for `exec`: it parses and checks syntax and stops there, where the Go
and Rust X-chains go on to verify semantically and then execute against the
empty chain. It is the fastest row in the table and it is the one doing less,
and the harness cannot say how much of that gap is the missing pass and how much
is C++ being quicker at what it does share. Its number is a parse-and-syntax
number, and it becomes comparable the day that evaluator runs the same two
passes — the same gap the `SKIPPED` field already reports to the differential.

**`go` is one row over two chains.** The Go evaluator answers all 208 vectors;
Rust and C++ each answer one chain per binary, 159 P and 49 X. A P-chain vector
costs at least twenty times an X-chain one in all three languages, so `go`'s
µs/vector is a blend of the two and cannot be read against `rust/platformvm`'s.
The rows to read against `go` are `rust (all)` and `cpp (all)`: the same 208
vectors, that implementation's binaries summed within each run.

To compare one chain at a time, hand the evaluators a corpus holding only that
chain's vectors. Nothing in any of them has to change — the corpus path is an
argument, and the Rust and C++ evaluators already ignore the other chain's
lines, so only the Go one sees a difference:

```
awk -F'\t' '/^#/ || ($1=="V" && $3=="P")' conformance/corpus/vectors.tsv > p.tsv
go run ./conformance/bench -vectors p.tsv -repeats 200 -runs 7 \
  -eval "go=conformance/gen/gen eval" \
  -eval "rust=chains/rust/platformvm/target/release/conformance" \
  -eval "cpp=chains/cpp/platformvm/build/pvm_conformance"
```

`make bench` deliberately does not do this. Its job is to time the workload the
differential actually runs, and that workload is the whole corpus.

**Go builds its chain once, inside the clock.** `execp.go` stands up a real
`platformvm` state — an in-memory database, a genesis, a metrics registry, a
validator manager — behind a `sync.Once`, on the first vector that executes, and
that lands inside the first round. Fitting a line through the fastest of twelve
runs at one repeat and at two hundred puts the build at 2.4 ms against a pass of
16.9 ms: 0.07% of a 200-repeat run, and most of a single-repeat one. Rust and
C++ build a trivial in-memory state per vector instead, which is inside every
round, and the same fit gives them a fixed cost of 2 ms and -1 ms — which is to
say none, measured to the noise floor. The setups are not the same shape, and
the repeat count is what stops that from being the thing measured.

**The optimisation settings are alike, not identical.** Rust is cargo's default
release profile: `opt-level = 3`, no LTO, sixteen codegen units, no debug
assertions, no overflow checks. C++ is CMake's `Release`: `-O3 -DNDEBUG`, no LTO
and no interprocedural optimisation. Go has no such dial to set. Neither the
Rust nor the C++ binary was built with LTO or profile-guided optimisation, and
turning either on would move two of the three numbers and not the third.

**The allocators are not the same.** Every verdict is seven heap-allocated
strings and this workload allocates heavily. Rust and C++ both reach the system
allocator; Go uses its own and collects behind it. That difference is inside the
measurement and cannot be taken out of it without changing what is computed.

**They are not three ports of equal maturity.** Go is the reference the fleet
runs, executing against the node's own `state.State` behind a diff, with metrics
and a validator manager attached. Rust and C++ execute against state types of
their own. That is not a like-for-like runtime even where the verdict is
identical.

**Nothing memoises a verdict, and that was checked rather than assumed.** Every
round decodes the hex again, parses again, verifies again and executes again;
what survives a round is Go's chain state and nothing else. Two things say so.
The printed rows are identical whether the count is 1 or 200, and every round
writes into a vector that is printed afterwards, so no round can be dropped by
an optimiser. And the cost of ONE pass does not fall as the count rises: taking
the fastest of eight runs at 25, 50 and 100 repeats, `cpp/xvm` spends 0.1118,
0.1138 and 0.1128 ms per pass — flat to 1.8% across a fourfold change — and no
evaluator's per-pass cost falls by more than 3%, which is inside the noise on a
busy machine. A verdict cached after the first round would not shave three
percent off the later passes; it would make them nearly free.

**One machine, one architecture.** These are Apple M1 Max numbers and say
nothing about x86-64. An M1 Max also has performance cores and efficiency cores,
so a run scheduled onto an efficiency core is several times slower than one that
is not — one of the things the spread is there to show.

### Reading the table

```
evaluator        vectors  repeats  runs  fastest s  median s  slowest s  spread  µs/vector
```

`µs/vector` is per vector per repeat, taken from the **fastest** run. Everything
else on the machine can only ever add time to a run, never subtract it, so the
fastest one is the least contaminated. The spread beside it — slowest over
fastest — says how contaminated the others were. A large spread means the
machine was busy, not that an implementation is erratic, and the honest response
is to say so and run it again somewhere quiet rather than to quote a mean over
the noise.

That is not a hypothetical. The first run of this was taken on an Apple M1 Max
carrying a load average around 35, and the spreads came back between 341% and
1001% — a slowest run seventeen times the fastest. Nothing in that table was a
measurement of anything. Repeated at a load average near 13 the same
measurement came back with spreads between 6% and 19%, and that is what is
recorded below.

Which is also the evidence that the fastest run is the right one to quote. Run
again at a load average of 25, the spreads roughly tripled — 22% to 73% against
6% to 19% — and every µs/vector moved by less than 4%: `cpp/xvm` 2.23 both
times, `go` 71.41 against 72.41, `cpp/platformvm` 118.30 against 123.05. The
machine's noise went into the spread, which is what the spread is for, and left
the minimum where it was.

### What it said

An Apple M1 Max, ten cores, macOS, load average around 13. The absolute numbers
belong to that machine; the ratios are what carries.

```
evaluator        vectors  repeats  runs  fastest s  median s  slowest s  spread  µs/vector
go               208      200      5     3.012      3.198     3.428      14%     72.41
rust/platformvm  159      200      5     2.978      3.186     3.296      11%     93.63
rust/xvm          49      200      5     0.042      0.044     0.050      19%      4.27
rust (all)       208      200      5     3.022      3.230     3.345      11%     72.64
cpp/platformvm   159      200      5     3.913      4.112     4.238       8%     123.05
cpp/xvm           49      200      5     0.022      0.022     0.023       6%      2.23
cpp (all)        208      200      5     3.936      4.134     4.260       8%     94.61
```

Over the whole corpus Go and Rust are the same speed: 3.012 s against 3.022 s,
a third of a percent apart with spreads of 14% and 11%, which is to say the
difference is not resolvable here and should not be quoted as one. C++ takes
about 30% longer — and it does so while its X-chain evaluator is the one
skipping a pass.

Per chain, each evaluator handed a corpus of one chain's vectors, µs per vector
from the fastest run:

| chain | go | rust | cpp |
| --- | --- | --- | --- |
| P, 159 vectors | 92 | 92–94 | 119–123 |
| X, 49 vectors | 3.3 | 4.2 | 2.2, and skipping a pass |

The C++ P-chain is the slowest of the three by about 30%, and Go and Rust are
again indistinguishable from each other. The P row is four measurements per
implementation across two sittings and holds to within a few percent. Every
number in the X row is corroborated twice: `rust/xvm` and `cpp/xvm` appear as
4.27 and 2.23 in the whole-corpus table above, within 2% of their X-only
numbers, and `go`'s 3.3 came back as 3.38 in a separate run on a machine three
times busier.

The C++ X-chain is the fastest of the three at the X-chain and it is the one
doing less. Those two facts cannot be separated with this data: how much of the
gap is the skipped execution pass and how much is C++ being faster at what it
does share is not a question the harness can answer until that evaluator runs
the same two passes. Between the two that do run the same passes, Rust's
X-chain is about a quarter slower than Go's.

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
