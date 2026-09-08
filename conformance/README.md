# conformance

Two harnesses live here, and they check different things.

- **`make conformance`** — the **consensus-layer** corpus: what a validator
  signs, what a certificate looks like on the wire, what the finality predicate
  decides. It already existed, and all three implementations already pass it.
- **`make chains`** — the **chain-layer** differential: one corpus, covering
  seven chains, handed to every implementation of each of them, with every
  answer compared against every other answer.

Until `make chains` existed, every port checked itself against Go in isolation,
each one deciding for itself which cases to check. That is how a P-chain fork
survived: `chains/cpp/platformvm` executes the sovereign-L1 plane and
`chains/rust/platformvm` refuses it by name, and no test anywhere put those two
answers next to each other.

## The seven chains

| | vectors | Go | Rust | C++ |
| --- | --- | --- | --- | --- |
| P platformvm | 159 | yes | yes | yes |
| X xvm | 49 | yes | yes | yes |
| Q quantumvm | 81 | yes | yes | yes |
| Z zkvm | 137 | yes | yes | yes |
| D dexvm | 35 | yes | yes | yes |
| F fhevm | 264 | yes | yes | yes |
| O oraclevm | 195 | yes | yes | yes |

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

P and X come from `luxfi/node`; Q, Z, D and F from `luxfi/chains`. All are
PUBLISHED versions and there is no replace directive, so the corpus regenerates
on any machine rather than on one.

## O, and where the O-chain actually is

`chains/oraclevm` is 213 lines and none of them are the chain: it re-exports
`github.com/luxfi/oracle/vm`, which is 1628. Reading the shim's size as the
chain's size understates it by an order of magnitude. The same is true of
`chains/relayvm` — 161 lines over `luxfi/relay/vm`'s 2077. Of the eight chains
the node runs that this differential did not cover, the two that looked
smallest by an order of magnitude were the two that were not there at all.

### O is JSON, and its id is a hash of the RE-MARSHAL

The O-chain has no codec frame. `ParseBlock` is `json.Unmarshal` and `Bytes` is
`json.Marshal`, so the wire is whatever `encoding/json` writes for the chain's
structs: CB58 for an id, base64 for a byte slice, a list of numbers for a fixed
array, RFC 3339 for a time, and field order taken from the struct declaration.
None of that is written down anywhere but in the standard library's behaviour,
and all of it is consensus here — because `computeID` marshals the parsed block
AGAIN and hashes the result rather than hashing the bytes it was handed.

Two things follow, and the corpus pins both:

  - **a block does not round-trip its own id.** `BuildBlock` hashes the block
    while its `id` member is still empty and then writes that id INTO the
    struct, so `Bytes()` is not the preimage of `ID()`. `O_BLOCK_ID_SET` is the
    block the chain wrote, and it takes a different id when read back.
  - **a member the reader accepts and `omitempty` then drops does not change
    the id.** `"observations":[]` parses to an empty slice and re-marshals to
    nothing at all, so `O_BLOCK_EMPTY_ARRAY` and `O_BLOCK_EMPTY` are two
    different buffers under ONE id.

### The O-chain's genesis id depends on the machine's timezone

`Initialize` builds the genesis block with `time.Unix(genesis.Timestamp, 0)` —
LOCAL time — and `MarshalJSON` writes the offset. Measured: one genesis file
gives block id `6cf00752…` with `TZ` unset on a `-08:00` box and `f4970a27…`
under `TZ=Asia/Kolkata`. Two validators in two timezones derive different
genesis ids from identical genesis bytes and are on different chains from
block zero.

No vector carries that id — a corpus answer that changes with the machine is
not an answer — so `O_GENESIS` pins the genesis by the one thing that IS a
function of its bytes: the SHA-256 of the canonical re-marshal of the parsed
struct. The bug is in the chain and not in the corpus, and it is written down
here because a differential that quietly worked around it would have hidden it.

### What O's five ops ask

`block` is the wire and the id above. `genesis` is the feed configuration the
observation vectors are judged under, so "we applied the same configuration" is
a compared field rather than an assumption. `requestid` is the derivation that
names a request — `sha256("LUX:OracleRequest:v1" ‖ service ‖ session ‖
be32(step) ‖ be32(retry) ‖ tx)` — where nothing separates the three ids, so
their ORDER is the whole of what keeps two requests apart. `commit` carries a
request and the records executed against it and answers with the Merkle root
the chain commits, which is the one number a light client checks an oracle
answer against. `observation` is offered to the seeded chain and separates the
feed lookup, the staleness rule and the operator check into three refusals.

`Block.Verify` is `return nil` with no condition in it, so on O's block vectors
`syntactic` and `exec` are OK for everything that parses. That is the chain and
not a gap in the corpus: what O decides lives in the other four ops, and a port
that invented a block rule would fail the differential for being right.

Three O vectors are PAIRS, and neither half of a pair means anything alone.
`O_COMMIT_THREE` and `O_COMMIT_THREE_PADDED` are different record sets that
commit to the SAME root, because the tree pairs a lone last leaf with itself —
reproduced here rather than corrected, since a port that fixed the malleability
would derive a different root for every odd record count and fork the chain in
the act of improving it. `O_BLOCK_DUPLICATE_HEIGHT_FIRST` and `_LAST` are one
intruding member placed either side of the original, and only the pair says
which of two members naming one field wins. `O_COMMIT_ONE` sits against six
`O_COMMIT_OTHER_*` copies, five of which must move the root and one — the
signature, which the leaf does not hash — must not.

The four chains were added because they had **no vector at all**, which is the
same shape the P-chain fork hid in for weeks: a chain nothing is pointed at
agrees with itself. Every fork this program has found, the differential found.

**All three columns answer all seven chains, and every compared field of all
920 vectors agrees.** That is recent. The Rust column used to answer P and X and
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
chains/rust/oraclevm/src/bin/conformance.rs     the Rust O-chain's answers
chains/cpp/oraclevm/test/conformance.cpp        the C++ O-chain's answers
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

Three of the seven chains hash something that never travels into every id they
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
make chains           build all fourteen evaluators, run the differential
make chains-corpus    regenerate the corpus from the Go reference
```

**`make chains` passes today.** 920 vectors, three running implementations —
go, rust and cpp — plus the committed corpus as a recording of the first.
Agreement on every field, every field answered by at least two of the three,
and nothing under NOT ANSWERED.

A handful of fields are still DECLINED, and the runner prints which: `exec` on
the X-chain in C++ and `exec` on the Q-chain in Rust. Those two sets do not
overlap, which is the only reason the run is green rather than short a voice —
and the count is deliberately not written down here, because it moves as those
two ports close and a number in prose would go stale silently. Read it off the
run.

The O column declines NOTHING. A port that agreed by declining is the failure
mode this differential already had once, so both O evaluators were held to
answering every field of every O vector before either was wired in.

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
