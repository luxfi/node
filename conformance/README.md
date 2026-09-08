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
| Q quantumvm | 81 | yes | yes | yes |
| Z zkvm | 137 | yes | yes | yes |
| D dexvm | 35 | yes | yes | yes |
| F fhevm | 264 | yes | yes | yes |

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

**All three columns answer all six chains, and every compared field of all 725
vectors agrees.** That is recent. The Rust column used to answer P and X and
say nothing about the other four, and the runner listed 421 vectors under NOT
ANSWERED and exited non-zero — silence is not agreement, and a target that went
green while a whole column said nothing about four chains would be reporting the
agreement of whoever was left. Each of the four slotted in as one more
`-eval "rust=…"` line, which is the whole of what the runner had to learn.

The last two, Z and F, each found something on the way in. The Rust Z-chain had
no VM and no evaluator at all, and once it had both it agreed with Go on all 137
vectors first time. The Rust F-chain answered every vector and disagreed with Go
on seven, all of them in the `F_JSON_*` group and all of them the same kind of
mistake: a JSON decoder that was stricter than `encoding/json` in ways nobody
had written down. Those are described under **What is compared** below.

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

### `syntactic` where verify is one pass

`syntactic` and `exec` name two passes, and most of the corpus has two to read.
P and X run `SyntacticVerify` and then execute. F runs `SyntacticVerify` — well
formed and priceable, without state — and then `SubmitTx`. D's vectors are not
blocks at all but pure functions, and its two fields are whether the inputs were
admissible and what the function returned. On those there is nothing here to
decide.

Q and Z have ONE. `Verify` runs the block's own rules, then the transactions,
then the parent, and returns the first refusal — there is no second entry point
to call, so an evaluator that wants two answers has to find the seam itself.
Three of them did, and each found it somewhere else. That is not a fork in any
chain; it is a question the corpus was asking without ever having defined it.

**The definition, for any chain whose verify is one pass:**

> `syntactic` is the verdict that pass reached **before it read the chain**.
> The boundary is that implementation's first read of the store. A refusal
> ahead of it answers BOTH fields — the block never got far enough for the two
> to differ. A refusal past it answers `exec`, and `syntactic` is `OK`.

The boundary is a place in the code, not a category of rule. Q reads its parent
and only then checks the clock; Z checks the clock first. So the same rule is
`exec` on one chain and `syntactic` on the other, and that is the answer, not a
wrinkle to iron out — it is the difference between two verifies, and this field
is what says so. An implementation that moved a rule across its own first read
answers differently and fails the run, which is the whole point of asking.

**Each implementation answers for itself, out of its own code.** A port that
owns its source names the boundary there and the evaluator calls it:
`Block::syntactic_verify` in the C++ Z-chain, `on_chain` then `well_formed` in
the Rust Q-chain — the same two the Rust VM's own `verify` runs before it looks
for a parent. Where the reference is a published module nobody here can add a
method to, the evaluator names the refusals instead: `zBlockAlone` and
`qBlockAlone` quote `luxfi/chains`' own sentinels, because that package keeps
every one of them unexported and there is no symbol to name. Nothing is copied
between languages. Each list is a walk of one `Verify`, in the order that
`Verify` runs.

**Not by counting reads.** A store that counted its own reads would answer "did
this run touch the chain" exactly, in every language, with no list of words
anywhere. It answers a different question. A block whose transactions carry
nullifiers reads the spent set long before it reaches the state root, and a
block carrying none never reads it at all — so one rule refusing in one way
would land in a different class depending on what the block happened to hold. A
class that moves with the payload is not a class.

### `SKIPPED` is never a pass

A field an implementation declines to answer prints `SKIPPED`. The runner
excludes it from comparison, counts it under **DECLINED**, and if it leaves the
field with fewer than two running implementations, lists the field under **NOT
COMPARED** and fails the run. A field nothing compared is not agreement, and it
does not get to exit zero.

The reference's own recorded answers cannot make up the shortfall. `expected.tsv`
is what `gen eval` printed, so `go` and `corpus` are one function's output twice
over — byte for byte, `sha256sum` of both is the same string. Counting them as
two answers is what let every port decline every field of every vector and still
print `AGREED`. They are marked as a recording now: they may disagree with
anyone, and they may not stand in for a second implementation.

Today that is `exec` on the X-chain in C++ (13 fields) and `exec` on the
Q-chain in Rust (12). The two sets are disjoint, so every one of those fields
still has two implementations behind it. Go and Rust both run the X-chain's
semantic pass and then its executor over an empty chain — the same arrangement
the P-chain's vectors are judged under, for the reason above — so the field IS
compared, and a Rust chain that skipped the semantic pass and answered `OK`
where Go answers `LEDGER` fails the run on all five vectors. The C++ evaluator
still declines; giving it the same two passes on its own empty chain is what
closes the last voice. Until it does, the C++ Q-chain declining `exec` would
take those twelve fields down to one implementation and fail the run — which is
the point: the target should notice a voice leaving, not average over it.

### What `encoding/json` accepts

An F operation payload is JSON, so what the chain admits depends on what its
decoder admits, and Go's decoder is not the strict reading anyone writes by
default. The `F_JSON_*` group asks the question directly, and it caught four
rules the Rust F-chain did not have. Each is one member of one payload, and each
would have refused a transaction the reference admits — which on a live chain is
a node that refuses a block its peers accepted.

`null` where a struct belongs is the ZERO struct, not a refusal. What then
refuses the transaction is whatever rule the zero payload breaks, and that is a
different rule per operation: `F_JSON_NULL_REVOKE` reaches the ledger and is
refused for a permit nobody holds, while the register and advance payloads are
refused for their own shapes. A decoder that refused `null` outright names the
wrong rule on all three and the wrong verdict on one.

Trailing content is two rules and not one. Go reads a payload through a
`Decoder` and asks `dec.More()`, and More answers "is there another ELEMENT" —
it reports false for `]` and `}`. So a stray closing bracket after a complete
value leaves the payload standing, while a comma or a second document does not.

Member names fold by `unicode.SimpleFold`, which is not ASCII case folding.
U+017F folds onto `S` and U+212A onto `K`, so `ſize` names Size and `publicKey`
spelled with a Kelvin sign names PublicKey. And when two members name one field
both are resolved and the LATER one is written, whichever way each of them
matched — so a decoder that preferred the exact match reads a different value
out of the same bytes, and one that sorted its members cannot express the rule
at all.

A base64 word broken across a line is the same word: `encoding/base64` ignores
CR and LF wherever they fall. `F_JSON_BASE64_NEWLINE` carries a complete epoch
proposal with its network key wrapped, so the answer turns on whether the key
was read — a reader that refused the newline reaches a proposal with no key and
refuses the committee, which is a rule about the committee standing in for a
rule about base64.

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
make chains           build all twelve evaluators, run the differential
make chains-corpus    regenerate the corpus from the Go reference
```

**`make chains` passes today.** 725 vectors, three running implementations —
go, rust and cpp — plus the committed corpus as a recording of the first.
Agreement on every field, every field answered by at least two of the three,
nothing under NOT ANSWERED, and 25 of 3625 fields declined: `exec` on the
X-chain in C++, `exec` on the Q-chain in Rust. Those two sets do not overlap,
which is the only reason the run is green rather than short a voice.

The corpus is committed, so a reference that changed its mind shows up as a
diff. `expected.tsv` — the Go chains' answers at generation time — also joins
the run under the name `corpus`, so drift in Go itself is a disagreement rather
than a silent new normal. It is a recording, not a second opinion, so it does
not count toward the two answers a field needs before it counts as compared.

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

### One chain at a time

A port is one chain: `chains/rust/platformvm` answers the P rows and walks past
the rest, so what it reports is a P-chain time. The Go reference is all six
chains in one program, and a whole corpus is not a time to set beside a sixth
of one — so it is told which chain to answer:

```
conformance/gen/gen eval -chain Q conformance/corpus/vectors.tsv 200
```

It selects before the clock starts and calls itself `go/quantumvm` on the B
line, in the words the ports already use for themselves, so the three rows for
one chain read as three rows about one thing. `make bench` asks it once per
chain: eighteen rows, and a total per language over the same 725 vectors.

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

One corpus, byte for byte, the same vectors in the same order, and one row per
chain per language over exactly that chain's vectors. `make chains` is what
says whether it is the same work: a field all three answer is a field nobody
was timed on while quietly answering an easier question. Each evaluator is run
five times and the runs are interleaved — every evaluator once, then every
evaluator again — so a machine that slows down halfway through slows all of
them down rather than one.

### Where it is not

**A declined field is work that did not happen.** `make chains` prints what
each port skipped, and a row that skips a pass is fast for that reason and not
for a reason about the language. The two it named while this was written were
`cpp . X . exec` and `rust . Q . exec`; run it for the current ones, because
they move. A row so named is a parse-and-syntax number over part of its corpus,
and the harness
cannot say how much of a gap is the missing pass. A chain's three rows become
comparable the day nothing under DECLINED names it — which is the same gap the
`SKIPPED` field already reports to the differential.

**The evaluators do not stand their chains up the same way, and on Q that is
the whole number.** The C++ Q-chain builds a fresh `QuantumVM` for every
vector, on purpose: "so no vector can be answered differently because of one
that ran before it". The Go one builds it once per process behind a
`sync.Once`, on purpose too: "starting it opens a committee, which is work".
Handed a corpus of ONE Q block and timed at 1, 10, 100 and 1000 repeats, Go
fits a line of 0.30 ms fixed plus 4.6 µs a vector, and that line predicts all
four measurements to within 2%: one chain, then cheap blocks. C++ has no fixed
cost to fit — from ten repeats to a thousand it is 1.86 ms EVERY vector. (Its
first vector in a process costs 0.36 ms and every one after it the full 1.86;
that is not a cached chain, since each vector gets its own, and it is not
explained here.) A Q block that dies on its FIRST BYTE costs C++ 1.85 ms,
within 1% of one that verifies to the end, so 99% of that row is standing the
chain up and 1% is the block.

Two things are then true and only one of them is about C++. It builds 81 chains
a round where Go builds one a process, which is the whole ratio; and its build
is 1.86 ms against Go's 0.30, which says the C++ Q-chain is six times more
expensive to stand up. The second is a real number about the chain. The first
is a choice about the evaluator, and it is what the row is measuring.

Nothing here says which policy is right. Fresh-per-vector is the stronger
isolation and Go's `sync.Once` is the faster measurement; what is not tenable is
reading the two numbers as a fact about C++.

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
an optimiser. And the cost of ONE pass does not fall toward zero as the
count rises. Over the Z corpus, fastest of six at 25, 50 and 100 repeats,
`cpp/zkvm` spends 0.197, 0.201 and 0.199 ms a pass and `rust/zkvm` 0.227, 0.229
and 0.237 — flat to 2% and 4% across a fourfold change. `go/zkvm` does fall,
0.130 to 0.108, and it fits a line: 0.101 ms a pass on a fixed 0.74 ms, which
is a chain stood up once and not a verdict remembered. A cached verdict would
not shave a fifth off the later passes; it would make them nearly free.

**One machine, one architecture.** The table below is x86-64, one AMD part,
Linux, and says nothing about arm64. An earlier sitting on an Apple M1 Max, on a
smaller corpus, also put C++ about 30% behind on the P-chain and Go and Rust
level with each other; that ordering is the only thing carried across from it,
and the absolute numbers are not comparable at all. Machines with cores of two
kinds are a further trap — a run scheduled onto a slow core is several times
slower than one that is not, which is one of the things the spread is there to
show.

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

That is not a hypothetical. A run of this on an Apple M1 Max carrying a load
average around 35 came back with spreads between 341% and 1001% — a slowest run
seventeen times the fastest. Nothing in that table was a measurement of
anything. Repeated near a load average of 13, the same measurement came back
with spreads between 6% and 19%.

And the minimum is what survives a busy machine. The table below was taken at a
load average of 56 and repeated immediately at 46: one spread reached 12304%,
and every µs/vector in the two tables agrees to within 6%, most within 2%,
`cpp/quantumvm` to within 0.06%. The noise went into the spread, which is what
the spread is for, and left the minimum where it was.

### What it said

Thirty-two cores of an AMD Ryzen AI Max+ 395, Linux, and a machine shared with
other work: the one-minute load average was 56 when the run started and 54 when
it ended. Those numbers belong to that machine on that day; the ratios are what
carries, and the spreads say how much even they should be leaned on.

```
evaluator        vectors  repeats  runs  fastest s  median s  slowest s  spread  µs/vector
go/platformvm    159      200      5     2.059      2.459     3.026      47%     64.74
go/xvm           49       200      5     0.022      0.040     2.754      12304%  2.27
go/quantumvm     81       200      5     0.016      0.018     0.349      2017%   1.02
go/zkvm          137      200      5     0.022      0.027     0.035      58%     0.80
go/dexvm         35       200      5     0.004      0.004     0.005      18%     0.59
go/fhevm         264      200      5     1.055      1.083     1.441      37%     19.97
go (all)         725      200      5     3.184      4.085     6.911      117%    21.96
rust/platformvm  159      200      5     1.935      2.041     2.408      24%     60.85
rust/xvm         49       200      5     0.021      0.022     0.024      15%     2.13
rust/quantumvm   81       200      5     0.245      0.262     0.317      29%     15.14
rust/zkvm        137      200      5     0.047      0.050     0.062      30%     1.73
rust/dexvm       35       200      5     0.004      0.004     0.005      15%     0.59
rust/fhevm       264      200      5     0.740      0.756     1.234      67%     14.02
rust (all)       725      200      5     3.011      3.115     4.050      35%     20.76
cpp/platformvm   159      200      5     2.655      2.743     4.171      57%     83.49
cpp/xvm          49       200      5     0.012      0.016     0.024      102%    1.21
cpp/quantumvm    81       200      5     29.503     29.653    31.655     7%      1821.19
cpp/zkvm         137      200      5     0.042      0.044     0.047      11%     1.54
cpp/dexvm        35       200      5     0.003      0.003     0.007      128%    0.44
cpp/fhevm        264      200      5     1.568      1.700     2.198      40%     29.70
cpp (all)        725      200      5     33.886     34.509    37.591     11%     233.69
```

Run it twice and every row of the second lands within 6% of the first, most
within 2% and `cpp/quantumvm` within 0.06% — on a machine whose spreads reach
12000%. That is the case for quoting the fastest run and reading the spread as
weather.

**P and F are the measurement.** They are 423 of the 725 vectors and all but a
few percent of every total, they are the two chains that verify signatures, and
all three implementations answer every field of both. Rust and Go are within
6% on P (60.9 against 64.7 µs a vector) and C++ is 30% behind them (83.5).
On F, Rust is fastest at 14.0, Go is 20.0 and C++ is 29.7 — the same ordering,
a wider gap. Nothing here separates Go from Rust by more than the machine does.

**Z is the one chain Go wins outright**, 0.80 against C++'s 1.54 and Rust's
1.73, and the harness cannot say why. **D and X are 0.4 to 2.3 µs a vector** —
derivations and fail-fast parses on 84 vectors between them, near enough the
floor that they should not be argued over.

**Q is not a comparison**, for the reason under "Where it is not": the C++
evaluator stands a fresh chain up for every vector. Take Q out and the three
totals over the remaining 644 vectors are Go 3.16 s, Rust 2.75 s, C++ 4.28 s —
C++ about 35% behind Go, which is the same shape P and F give and the same
shape an earlier sitting on an M1 gave for a smaller corpus.

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

The Rust Q-chain became the third voice on those 81 later than the other two,
and it arrived with the SAME hole the Z-chain had just closed. Its evaluator
read `syntactic` off `well_formed()` alone, so `Q_BLOCK_FOREIGN_CHAIN` and
`Q_BLOCK_FOREIGN_NETWORK` came back `OK` where Go and C++ both answered
`SYNTACTIC` — a block naming another chain, waved through the field that is
supposed to catch exactly that. The Rust chain was never wrong: `on_chain` is
right there, and `Vm::verify` calls it first, ahead of `well_formed` and well
ahead of the parent lookup. Only the evaluator had stopped one call short. Two
readers can invent the seam in two places; three can invent it in three, which
is why the definition is written down now instead of inferred a fourth time.

It also declined the `identity` vector outright — `unknown op identity`, five
INTERNAL fields — so the one row that would report a Rust evaluator built for
the wrong chain was the row it did not print. It prints the pair it serves now,
and derives it from nothing the corpus hands it.

**D — 35 of 35 agree.** Every asset id, every market id, the kind and mode
parsers, the network class and the value-activation guard.

**F — 168 of 168 agree.** The six operations, every payload rule, the four
ways a signature can fail to be the payer's, and the id the genesis block
takes.

**Z — 137 of 137 agree.** This is where the field got its definition.

`Z_BLOCK_TIME_AHEAD`, `Z_BLOCK_GENESIS_WITH_PARENT`,
`Z_BLOCK_DUPLICATE_NULLIFIER` and `Z_TX_EXPIRED` answered `syntactic OK` in
C++ and `syntactic SYNTACTIC` in Go. Both refused the block, both gave the
same reason, and `exec` agreed on all four. They disagreed about which phase
caught it, and neither of them was wrong, because nothing said what the phases
were. Three of the four rules are block-level besides, so no per-transaction
pass could have held them however the two readers had split it.

The Z-chain reference has ONE verify. So Go matched the sentinels it knew were
decided before any lookup, C++ called the per-transaction `validate_basic` its
port happened to expose, and the two inferences did not coincide. The fix is
the definition above — `syntactic` is what verify decided before it read the
chain — plus one change per implementation to answer THAT.

C++ could name the boundary in its own source, and does:
`Block::syntactic_verify()`, the name the C++ P-chain already uses at twenty
sites, holding the five rules that are settled from the block in hand — the
genesis/parent pairing, the transaction cap, the clock, a nullifier repeated
inside the block, and per transaction `ValidateBasic` and `expiry < height`,
where the height is the block's own, on the wire, not the chain's. `check()`
calls it first and then asks `admit` per transaction as before, so the evaluator
asks the chain rather than holding an opinion about it.

Go cannot: its reference is a published module, and `luxfi/chains/zkvm` keeps
every sentinel unexported, so there is no symbol to name and no method to add.
`zBlockAlone` quotes the words instead, matched whole rather than by substring
— the proof verifier raises `transaction missing proof` too, from the far side
of the boundary, and a `Contains` would have called that one syntactic.

One thing the corpus cannot prove: that the pass really avoids the ledger.
Every vector names the genesis block as its parent, and genesis exists, so a
`syntactic_verify` that secretly did a lookup would pass all 137. That is
asserted in `block_test.cpp` instead, on a block whose parent no chain holds.

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
