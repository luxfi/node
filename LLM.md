# node

`github.com/luxfi/node` — the Lux node. The Go implementation IS this module:
`cmd/luxd` and everything under it are these sources, so the repository is the
thing its import path names. The Rust and C++ implementations sit beside it,
and the differentials here hold all three to the same answers.

## Layout

```
cmd/luxd                the daemon
node/vms.go             the one chain registry — what runs, what is a plugin, what needs consent
mesh/                   the peer layer: dialer, post-quantum handshake, peer, throttling, tracker
network/                a set of chains that share a validator set and bootstrap together
chains/                 the chain manager, and chains/{rust,cpp}/* the ports
chains/schema/*.zap     the wire, from which the accessors are generated for all three languages
runtime/{go,rust,cpp}   what each implementation builds, from where, and the exact invocation
gpu/                    the primitive seam: a complete CPU backend, an optional kernel plugin
conformance/            the corpora and the harnesses that run them per language
bin/{go,rust,cpp}/luxd  build output (gitignored)
Makefile                the one build entry point
```

## The three implementations

Each is a running node: it executes blocks itself, decides them by BLS quorum
certificate over a real mesh, and serves them. None imports another. `make
luxd RUNTIME=<go|rust|cpp>` builds one; `make all` builds three and exits
nonzero unless all three come out.

**Go** builds from `./cmd/luxd` in this module and fetches nothing for it. It
runs the primary network's chains — P X C Q Z A B G K M F natively, D through
its plugin — and the closure is asserted clean at build time.

**Rust** builds `~/work/lux-rs/node`, **C++** builds `~/work/lux-cpp/node`.
The shims under `runtime/` name the checkout and the invocation; nothing is
copied or forked, and only the binary is carried back into `bin/`. Delete this
repository and both are exactly as they were.

**One brand.** This repository builds `luxd` and names no other. Downstream
networks layer their own identity in their own repositories and take these as
a dependency — `hanzoai/node` builds `hanzod`, `zooai/node` builds `zood`.

## The one chain registry

`node/vms.go` holds every chain in one map, and each row says the three things
that differ between them: whether the factory is built in `node.go` with
runtime dependencies, whether the chain brings its own plugin binary, and
whether an operator has to name it before it runs.

A chain asks for consent when running it costs an operator something that
validating the primary network did not — co-location with a matcher, an HSM, a
GPU, a custody role. `luxd --chains=D,B,M` names them, by letter or alias, and
an unknown name is refused at boot rather than ignored. A chain becomes
permissionless by dropping `Consent` from its row; nothing else changes.

## What is not here

The D-Chain matcher, the F-Chain's FHE implementation and the GPU kernels are
in `luxfi/compute`, which is private. Everything else here forks public work,
and anyone building on this network has to be able to read and run it, so the
two do not share a visibility. `chains/{rust,cpp}/{dexvm,fhevm}` and `gpu/` are
shims naming that checkout, and the differentials build them from `$(COMPUTE)`
so those rows are still measured. The pure-Go DEX in `luxfi/dex` is public.

## Building against live checkouts

The Rust and C++ trees are shared checkouts that other sessions edit while
this Makefile reads them. Every sub-build's exit code is checked and a failure
is reported rather than absorbed — `cargo` and `ctest` have both reported
stale success through a wrapper that did not look. Nothing here writes to
those trees to make a build go. If one fails, retry; if it persists, `git
status` in the checkout before suspecting the shim.

## The primitive seam (`gpu/`)

`gpu/` is not a shim. It is the one place a chain asks for a hash or a
signature check, in all three languages, and the one place the choice between
computing the answer and handing it to an installed kernel library is made.

Kernels are private and none of their source is here; what crosses into this
public tree is four symbol names, `dlopen`'d at run time. The CPU backend is
COMPLETE and is the definition of every primitive — a node with nothing
installed is a whole node, and `make luxd RUNTIME=go` builds exactly that
(`CGO_ENABLED=0` has no way to open a shared library, so it never has a
plugin). The plugin is a strict positive overlay: absent, declining, or
erroring, the CPU answers.

One knob, `LUX_GPU` ∈ `off | on | verify`, plus `LUX_GPU_LIB` for the path.
`verify` computes both answers and stops on the first differing byte;
`make gpu-differential` runs every seam twice, once with no library visible and
once under `verify`.

Only `keccak256_batch` — and the RFC 6962 fold built on it — has two paths
today. `sha256` and `ripemd160` have no op in the plugin ABI at all, and its
`ecrecover` answers a different question (an Ethereum address, where a Lux
address is `ripemd160(sha256(compressed key))`). `gpu/README.md` has the table,
what the differential actually measured on this machine, and the one
cross-language disagreement about secp256k1 recovery ids that consolidating
the primitives turned up.

## Conformance

`~/work/lux/consensus/conformance` (tag `v1.36.91`) is the finality standard
written down as data — what a validator signs, what a certificate looks like
on the wire, what the finality predicate decides — read live from the Go
definitions, never restated, so a rule change moves the corpus and the
failure is at the source. Rust and C++ each hold their own suite that checks
against it byte for byte, and both already pass. `make conformance` runs all
three, for real, every time: `go test` in the consensus repo,
five `cargo test` binaries in `pkg/rust` (`pop_conformance` and
`verdict_conformance` are the two the corpus is named for), and the two
prebuilt C++ binaries `conformance_test` and `pop_conformance_test`. A
runtime whose harness is not configured prints `skipped` and says why — a
skip is never reported as a pass.

`make chains` and `make precompiles` are the other two differentials, and they
are the same machine with different subjects: one corpus, three
implementations, a runner that understands nothing and compares strings. The
runner is shared — it is told which columns to compare, because a runner that
knew the chain's five field names would have to learn the next differential's
as well — and it fails a run in which any implementation printed no row at
all, since with four voices three answering is still a comparison, it agrees,
and the fourth's silence would read as a pass.

**`make chains` passes.** It covers seven chains — P and X from `luxfi/node`,
Q, Z, D and F from `luxfi/chains`, and O from `luxfi/oracle`, which had no
vector at all until they were added, which is the same shape the P-chain fork
hid in. All three columns now answer all seven, and every compared field of all
942 vectors agrees.

O was added last and taught three things before a line of either port was
written. `chains/oraclevm` is 213 lines and is NOT the chain: it re-exports
`luxfi/oracle/vm`, which is 1628 — and `chains/relayvm` is 161 lines over
`luxfi/relay/vm`'s 2077, so the two chains that looked smallest by an order of
magnitude were the two that were not there. The O-chain's wire is not a codec
frame but `encoding/json`, and its block id is the SHA-256 of the block
marshalled AGAIN, so a block does not round-trip its own id. And its genesis
block is built with `time.Unix`, which is LOCAL time, so the same genesis file
gives block id `6cf00752…` on a `-08:00` box and `f4970a27…` under
`TZ=Asia/Kolkata` — two validators in two timezones are on different chains
from block zero. That one is a bug in the chain and it is unfixed; the corpus
routes around it by pinning the genesis through a re-marshal instead, and
`conformance/README.md` says so rather than letting the workaround hide it.

The Rust column was the last to close, and the last two chains in it each said
something. `chains/rust/zkvm` had sixteen modules, no VM to hold them and no
evaluator; given both it agreed with Go on all 137 Z vectors on its first run,
so the fifteen thousand lines under it had been right and unexercised.
`chains/rust/fhevm` answered every F vector and disagreed with Go on seven —
all in the `F_JSON_*` group, all the same kind of mistake. Its JSON decoder was
stricter than `encoding/json` in four ways nobody had written down: it refused a
`null` payload where Go reads the zero struct, refused a stray `}` or `]` after
a complete value where Go's `Decoder.More` reports no further element, folded
member names by ASCII case where Go folds by `unicode.SimpleFold`, and refused a
base64 word broken across a line that `encoding/base64` reads straight through.
Each would have refused a transaction the reference admits, which on a live
chain is a node that refuses a block its peers accepted.
`conformance/README.md` states all four rules.

`make precompiles` is described in `conformance/PRECOMPILE.md`. **It passes:
235 calls, three runtimes, agreement on every compared field.** What it found
on the way there is the reason it exists.

Every disagreement it caught came back to one question the Go reference was not
asking — what does the C-Chain actually serve? — and the answer has two halves
the reference had both of wrong.

**Registration is not activation.** Importing `luxfi/precompile/registry` links
fifty-six modules into a binary. `LuxPrecompileOverrider` answers from
`GetExtrasRules(...).Precompiles`, which holds only the keys a chain's
`precompileUpgrades` name, and it never asks the registry whether the code is
linked. The reference asked the registry, so it answered for nineteen modules
no network has turned on. It now asks mainnet's enabled set.

**The stock table is the chain's, and the chain is at Cancun.** `luxfi/evm` maps
its Quasar fork to `CancunTime` and defines nothing after it — its config
carries no `OsakaTime` for a network to set, so no network sets one.

At `0x…0100` both halves showed at once, which is why three runtimes gave three
answers. Two implementations are written for that address and neither runs: the
Lux `secp256r1` module charges 3450 and `secp256r1Config` is named in no
`upgrade.json`, and geth's `p256verify` charges 6900 and arrives at Osaka. So Go
read 3450 off the registry, the two ports read 6900 off a table held at a
revision no Lux chain reaches, and a call there meets an empty account. All
three answer ABSENT.

`0x0300…0003` is the same finding with a longer history. `aiInferenceConfig` is
enabled nowhere either — the AI key the chain does enable, `aiMiningConfig`, is
at another address — and cevm had been taught to serve inference there in order
to agree with Go. A fix that makes a second node serve an address the first
leaves empty is a state-root split adopted to settle a differential, and it is
unregistered again.

Holding the ports at Osaka had a second cost that no address made obvious:
modexp was priced by EIP-7883 and EIP-7823, which no validator charges. Moving
to Cancun moved 25 corpus rows, which is the whole of the corpus diff besides
the five at `0x…0100` and the two at `0x0300…0003`.

The BLS12-381 modules at `0x0b`–`0x11` are untouched by any of this, and they
are the reason the layout matters: they are on the **pre-final** EIP-2537, which
still had separate multiply precompiles, so from `0x0c` up the same address
names a different operation to Ethereum than to the chain — `0x0d` is a G1
multi-exponentiation to the module and a G2 addition to geth, revm and cevm,
different curve groups rather than different gas. All seven keys are enabled on
mainnet at a timestamp in December 2025, on testnet and devnet, and at zero on
local and localnet, and Cancun's stock table holds nothing there, so the modules
are the sole occupants and all three runtimes serve that layout.

Address zero is not unclaimed either: the dead-address module (LP-0150) is
registered there, is enabled, and reads chain state, so it cannot be answered
for without a chain — and the corpus does not ask.

What is left is a reporting convention rather than a behaviour, and less of it
than there was. revm computes a price inside the work, so its error carries no
charge and a refused call used to leave the gas column blank on six fields. A
price is the smallest offer a precompile accepts, so the Rust evaluator now
bisects the offer and recovers it — 0 where revm rejects a blake2 length before
reading the round count, 50000 for a KZG blob it prices before validating —
which is what Go and cevm already reported.

This is **consensus-layer** conformance, not **node-level** — three live
`bin/luxd-*` daemons handed the same blocks over real sockets and checked
for agreement is a different, larger harness that does not exist yet, and
wiring it before `runtime/go` has an actual node host to point at would not
test what its name claims. It is Phase 2, alongside the Go host itself.

## Build

```sh
make luxd RUNTIME=go      # bin/go/luxd    — ./cmd/luxd, this module
make luxd RUNTIME=rust    # bin/rust/luxd  — lux-rs/node
make luxd RUNTIME=cpp     # bin/cpp/luxd   — lux-cpp/node
make all                  # all three + gpu; reports each; nonzero exit unless 3/3
make gpu                  # lux-gpu/gpu kernel library
make gpu-differential     # CPU alone, then CPU against the plugin, all three
make conformance          # the pop/verdict corpus, all three languages
make chains               # the seven-chain differential, all three languages
make chains-corpus        # regenerate that corpus from the Go reference
make precompiles          # the EVM precompile differential, all three languages
make precompiles-corpus   # regenerate that corpus from the Go reference
```

Every sub-build's exit code is checked explicitly — `cargo` and `ctest` have
both reported stale success from a wrapper that didn't check, so nothing here
trusts a green Makefile over a red sub-build.

## ZAP, and where it lives

ZAP is a bidirectional binary protocol with pipelining: both peers on a
connection initiate, and requests pipeline — many in flight, answers back in
whatever order the peer finishes them. It is not a codec, and treating it as
one is how this repo got into trouble.

The C++ chains share one wire implementation. It replaced
`chains/cpp/xvm/include/lux/xvm/zap.hpp` (557 lines) and
`chains/cpp/platformvm/include/lux/platformvm/zap.hpp` (517 lines) — and the
two copies had drifted from each other and from the reference. Two chains in
one binary disagreeing about frames is a fork surface inside one process.

They are gone. Both chains now speak the published SDK, `zap::zap` from
github.com/zap-proto/cpp, pinned in `chains/cpp/zap.cmake` and reached as a
package — never a path into a checkout, never vendored. An installed package
wins over the fetch, so `-DCMAKE_PREFIX_PATH=<install>` keeps an offline build
offline.

What the drift actually was, since it is the reason the rule exists:

- `set_bytes` must hold its payload until `finish()`. Both hand-written copies
  wrote it on the spot, which puts the payload BEFORE a list started on the
  same builder afterwards; the reference puts it after. Same fields, different
  bytes. Neither copy matched the reference, and no fixture here covered it —
  the SDK's `tail_then_list` case does, against bytes `zap-proto/go` printed.
- `set_bytes` must extend the object to cover the field it writes. platformvm
  did; xvm did not, and a field past the declared payload size wrote off the
  reserved region.
- `finish_with_flags` and a choosable header version: platformvm had both, xvm
  neither — so xvm could not have emitted a routed RPC envelope at all.
- Out-of-line list elements: xvm could read them, platformvm could only write
  them. One half of a pair each.

The wire-format suite lives in the SDK beside the implementation, case for case. A wire rule belongs in one
place, and its test belongs beside it.

Four more copies followed: `chains/cpp/zap` (which fhevm read), and
quantumvm's, zkvm's and dexvm's. 2,198 lines. Every C++ chain now links
`zap::zap` and nothing else.

### A nuance about `set_bytes`, since the paragraph above is half the story

There are two Go runtimes, and they differ here. `zap-proto/go` holds a byte
tail until `Finish()`; `github.com/luxfi/zap`, the hardened runtime the chain
corpus was generated through, appends it on the spot. They produce the same
bytes for exactly one write order — every payload first, then the object, then
nothing but field setters — and every builder in this tree happens to write
that way, which is why nothing ever caught it.

That is now a property of the code rather than an accident: zapgen EMITS that
order, and a test in the generator pins it.

## The wire's accessors come out of a schema

The runtime is one implementation. What sits on top of it — the offsets, the
strides, the readers and the builders — was written by hand, per chain, per
language, and that is what a generator is for.

`zapgen` has a C++ backend, so a chain states its wire once in a `.zap` schema
beside it and the accessors are printed:

```
chains/cpp/xvm/schema/wire.zap             ->  include/lux/xvm/gen/wire_zap.hpp
chains/cpp/quantumvm/schema/wire.zap       ->  include/lux/quantumvm/gen/wire_zap.hpp
chains/cpp/platformvm/schema/wire.zap      ->  include/lux/platformvm/gen/wire_zap.hpp
chains/cpp/platformvm/schema/genesis.zap   ->  include/lux/platformvm/gen/genesis_zap.hpp
chains/cpp/platformvm/schema/warp.zap      ->  include/lux/platformvm/gen/warp_zap.hpp
chains/cpp/platformvm/schema/warpmsg.zap   ->  include/lux/platformvm/gen/warpmsg_zap.hpp
```

One schema per namespace, because a package name is what a backend renders as
a namespace and a file declares one. The P-chain's four cover every shape it
puts on a wire or in a store: nineteen transactions, three blocks, the
credentials, an owner set, the two fx envelopes an output is ordered by, the
genesis blob, and the six warp messages.

`cmake --build build --target wire-schema` regenerates (`genesis-schema`,
`warp-schema`, `warpmsg-schema` for the others); the output is committed, so a
build needs neither Go nor the generator. The generator has its own published
pin, `ZAPGEN_TAG` in `chains/cpp/zap.cmake`, separate from the runtime's
`ZAP_TAG` — they are different repositories, and one tag naming both is how
that target came to point at a version that was never published.

The four list shapes the Lux wire actually has are DERIVED from the element
type, never declared — a run of numbers at its own width, a run of fixed-width
byte records, a run of struct payloads back to back, and a run of relative
pointers when the element carries a tail and cannot sit inline. A schema
therefore cannot spell one list two ways.

What is left in each chain's `wire.hpp` is what the bytes MEAN: the X-chain's
(TypeKind, ShapeKind) discriminator and its quorum gate, the Q-chain's
canonicality rule and its bound on a transaction count. Neither spells an
offset.

The P-chain kept 283 lines beside its four schemas, and not one of them is an
offset. What is there is the ARRANGEMENT, which is a fact about the chain
rather than about the format: an output's owner addresses do not live in the
output, they live in one run shared by the whole transaction, and the output
names a slice of it. That decision is stated once instead of at the nineteen
places a transaction is built.

Still by hand, and named rather than left to be found: zkvm (1,452 lines),
fhevm (913), dexvm (585).

### The Rust chains: generated, from `chains/schema`

Rust had the same drift — `chains/rust/{xvm,quantumvm,zkvm}/src/zap.rs` at 896
lines each, platformvm's at 770 and fhevm's at 598, five copies of one reader.
They are gone, and so is every hand-written accessor over them.

**Every byte a Rust chain writes now comes out of a schema.** The schemas are
in `chains/schema`, one per wire, and `zapgen` writes the accessors:

| schema | what it states | emitted to |
|---|---|---|
| `pchain.zap` | the P-chain's transactions, blocks, credentials, and the fx primitives it spends | `platformvm/src/pchain_zap.rs` |
| `warp.zap` | the warp envelope, its payloads, and the conversion preimage | `platformvm/src/warp_zap.rs` |
| `state.zap` | what the P-chain writes to its own disk | `platformvm/src/state_zap.rs` |
| `xchain.zap` | the X-chain's transactions, blocks and fx primitives | `xvm/src/xchain_zap.rs` |
| `qchain.zap` | the Q-chain's blocks and transactions | `quantumvm/src/qchain_zap.rs` |
| `zchain.zap` | the Z-chain's shielded transactions and blocks | `zkvm/src/zchain_zap.rs` |
| `fchain.zap` | the F-chain's transactions and blocks | `fhevm/src/fchain_zap.rs` |

One runtime under all of them: `chains/rust/zap`, also emitted, which every
chain takes as a path dependency. `make wire` rewrites all of it and is a
fixed point — run it and `git diff` is empty, which is how you check that what
is committed is what the schemas say. The generator is pinned by exact commit,
because a moving reference would let the accessors change without the schema
changing.

Two things that are NOT in a schema and should not be: the two bytes an fx
primitive travels behind (the family and the shape), and the framing of a run
of variable-width items as a length list beside one blob. Both are the chain's
rules about a sequence of messages, not the layout of one.

**Three traps, all of them found by measurement.**

Every hand copy defaulted `Builder::new` to wire version 2. The emitted
runtime keeps `new` at version 1 and names `new_v2` for the other, so a swap
that looks purely mechanical moves byte 4 of every message. The P-chain's warp
differential caught it against bytes Go printed. Every P-chain writer says
`new_v2`.

`chains/rust/platformvm/src/state_zap.rs`'s `Staker` reserves sixteen bytes
past its last field. The record has always reserved them; the schema names
them `Reserved` for the same reason `Out` and `In` name their `Pad` — a schema
that stopped at the last real field would write a shorter record than every
record already on disk.

`TransferOutput` and `Utxo` are declared in BOTH `xchain.zap` and
`pchain.zap`, because the X-chain makes them and the P-chain spends them.
That is two statements of one format, and it is not what anyone wants. Lifting
them into a shared `fx.zap` needs an import across schemas that `zapgen` does
not have — `xchain.zap`'s own envelope holds `list<ptr<TransferableOut>>`, so
the reference cannot be moved away from it. Until the import exists,
`chains/rust/zap/tests/one_format.rs` is the join: it reads both schema files
and fails the day the two declarations drift.

**What proves the bytes did not move**: `tests/corpus_bytes.rs`, in each of the
five chains. Each reads every vector of its own chain out of the shared corpus
— which the Go chain generated — re-encodes it through the BUILDER, and
compares to Go's hex. `Tx::parse` keeps the bytes it was handed, so asking a
parsed value for its bytes would prove nothing; these go back through the
writer. 68 P transactions and 4 P blocks, 19 X transactions and 6 X blocks, 12
Q blocks, 22 Z blocks, 43 F transactions — every vector the corpus expects to
parse. The rest are the deliberately malformed ones and are still refused.

A ZAP message carries its own length, so some vectors hold bytes past the end
of the message and still parse (`…_TRAIL_1`, `…_TRAIL_4`, `X_EDGE_CORRUPT_TAIL`).
What a writer writes is the message: where a vector IS the message the two are
the same bytes, and where it is not, the message is its prefix. A rewrite
longer than its vector, or one disagreeing anywhere inside the message, fails.

## The chain differential (`make chains`)

`make conformance` is the **consensus** layer. `make chains` is the **chain**
layer, and it did not exist until now: one corpus of P-chain and X-chain bytes,
generated by the Go chains, handed to the Go, Rust and C++ implementations of
both, with every answer compared against every other. See
`conformance/README.md` for the format, the verdict vocabulary and why an empty
ledger is the right state to judge against.

Why it had to exist: every port checked itself against Go in isolation, each
one choosing its own cases. That is how a P-chain fork survived —
`chains/cpp/platformvm` executes the sovereign-L1 plane and
`chains/rust/platformvm` refuses it by name, and nothing anywhere put those two
answers side by side.

It failed when it was written, which was the point: thirteen vectors
disagreed, including both bugs it was built to catch (the L1-plane fork; the
missing block reject on the C++ side, whose root was that `lux::node::Block` —
the C++ host seam itself — declared `verify` and `accept` and no `reject`) and
three that were not on anyone's list, the sharpest being that a transaction
addressed to another network passed the Rust P-chain's syntactic check.

All thirteen are closed. The three chains and the corpus now agree on every
compared field of all 208 vectors, and no field goes uncompared. `make chains`
exits zero, and it is the only thing that says so — the ports' own suites pass
either way, which is how a fork lived here for as long as it did.

`conformance/gen` is the one place in this repo that depends on `luxfi/node`.
That is what a reference is. It is a separate Go module so it cannot reach
this module's own dependency graph, which `make luxd` greps and fails on.

Two golden-generator packages, `chains/rust/platformvm/tests/vectors` and
`chains/cpp/xvm/test/golden`, are references of the same kind but live in the
ROOT module, so `go list -deps ./...` does reach `luxfi/node` through them.
Nothing that ships imports either one and `make luxd`'s grep is scoped to the
runtime it builds, so no artifact carries the dependency — but the module is
not clean by inspection, and the honest fix is to give them their own module
the way `conformance/gen` has one.

## The chain benchmark (`make bench`)

Three implementations of the same node and nothing comparing their speed: Go
carries 48 benchmark functions in `luxfi/node`, the Rust chains have no
`benches/` at all, and the C++ chains have none of their own. Any claim that one
is faster than another was unsupported.

The differential already hands all three the same 725 vectors and makes each one
parse and verify them, which is a fair identical workload. `make bench` times
it. Every evaluator takes an optional repeat count after the corpus path, walks
its chain's vectors that many times, and prints its own elapsed time to stderr
as `B <impl> <vectors> <repeats> <seconds>`. `conformance/bench` runs them and
prints the table. Without a count nothing changes and `make chains` is untouched.

**It did not run.** Six of the twelve evaluators the differential runs had no
clock at all — rust dexvm and quantumvm, cpp quantumvm, zkvm, dexvm and fhevm —
and the target named two of the six, so `make bench` exited 1 on the first of
them and had never produced a number for any chain. All twelve take the count
now; in C++ the loop lives once in the header the four already share. The D and
F evaluators are in `luxfi/compute` with the rest of the licensed half, so the
change to those two is there and the header they build against is here.

**One row per chain per language.** A port is one chain, so its time is that
chain's. The Go reference is all six in one program and could only report a
whole-corpus time, which is not a number to set beside a sixth of one; it now
takes `-chain P|X|Q|Z|D|F` before the corpus path and names itself the way the
ports name themselves. Eighteen rows, plus a total per language over the same
725 vectors.

Three things about it are load-bearing:

**Each evaluator times itself.** Timing the child from outside would measure
five process starts, five corpus reads and five output writes as if they were
chain work. Measured: Go spends 43.6 ms on all of that, four times the C++
X-chain evaluator's entire process and more than the C++ P-chain's, so a
process-level benchmark would have reported it as Go being slow at the chain.
The clock starts after the corpus is read and stops before the first verdict is
printed.

**The verdicts are kept, not dropped.** Each round writes into a vector that is
printed afterwards, in all three languages, so no round can be optimised away —
and the printed rows are byte-identical whether the count is 1 or 200.

**A single timing is not a measurement.** The evaluators are run five times, the
runs interleaved, and the table carries the fastest, the median, the slowest and
the spread. `µs/vector` comes from the fastest run: everything else on the
machine can add time to a run and nothing can subtract it, so the fastest is the
least contaminated and the spread says how contaminated the rest were.

What it says, and the honest caveats, are in `conformance/README.md` under
**Timing it**. The sharpest is that the biggest number in the table is not
about a language: `cpp/quantumvm` costs 1821 µs a vector against
`go/quantumvm`'s 1.02, and a Q block that dies on its FIRST BYTE costs it 1.85
ms — within 1% of one that verifies to the end. The C++ evaluator stands a
fresh chain up per vector and the Go one stands one up per process, both
deliberately: 81 chains a round against one a process is the entire ratio. The
next sharpest is that a field a port DECLINES is work it did not do, so a row
`make chains` names under DECLINED is fast for that reason. Take Q out and the
three totals over the remaining 644 vectors are Go 3.16 s, Rust 2.75 s, C++
4.28 s — the same shape P and F give on their own.

## Running the three

`make luxd RUNTIME={go,rust,cpp}` builds; nothing here starts a cluster. Three
things have to hold before a height moves, and only the first is obvious.

**Up is not producing.** All three answer `eth_chainId` and `eth_blockNumber`
the moment their listener opens, from a chain that has decided nothing. A
read-only RPC benchmark cannot tell the difference, because every question it
asks is answerable at height 0. Ask for the height twice, a minute apart, and
compare — a single sample cannot tell a live chain from a frozen one.

**The Rust and Go nodes build a block only when there is one to build.** Rust
says so in its own loop: `Error::Empty` is not an error there but *a chain with
no transactions is a chain at rest*. Go reaches the same place from the other
end — `--automine` is documented as anvil-like, auto-producing *on
transactions*. So both idle at height 0 forever with nothing wrong, and neither
logs anything while doing it. The C++ node does the opposite and produces empty
blocks, which is why it is the only one of the three that ever looks alive on
its own. Neither policy is a bug; the difference between them is worth knowing
before reading a height.

**The shipped genesis funds nobody who holds a key.** `configs/localnet`
allocates the C-chain to one treasury address, which is a Safe. There is no key
here for it, so out of the box neither Go nor Rust can execute a single
transaction, and by the paragraph above that means neither can leave height 0.
Feeding work to them takes a genesis that funds a key the caller holds — the
first Anvil account is the one the C++ node's built-in genesis already funds and
the one `conformance/dex` signs with, so funding it on the other two makes one
key drive all three. Rust takes such a document with `--genesis`, and its parser
reads a Go *network* genesis by descending into `cChainGenesis`, so the single
file Go is given with `--genesis-file` serves both.

**The C++ node cannot catch up, and that halts the cluster.** Leadership is
`height % n == index` and a follower waits for that leader's block, deliberately
never building a sibling. But certification needs live votes, and a proposer
certifies on a quorum without waiting for the last follower — so a node that is
slow once is left a height behind with no way back, because its peers have moved
on and will not vote at an old height again. It stays behind until the rotation
reaches its turn to lead, and then the whole cluster stops: observed at height
533, with node 1 stuck on 532 and nodes 0, 2 and 3 waiting on node 1 to lead.
The logs say `timeout — retrying` forever and the RPC keeps answering, so the
cluster reads as up.

Closing it needs a block a node can accept on a certificate it verifies rather
than on votes it collects. `Node2Host::verifyCert` already exists, so the
verification half is there; what is missing is a frame to carry a certificate
(the link defines only `kTxMsgType` and `kBlockMsgType`) and a path into the VM
that accepts a block without voting on it. That is a real addition, not a patch,
and it is not attempted here.

## What a Rust validator does after it restarts

**A validator that restarts rejoins the cluster it can see.**

The signing journal is durable on purpose: it is written before the key is
used, so a node that comes back cannot contradict the run before it. A refusal
to sign means this node does not vote *here* — it re-sends the statement it
already stands by, which the journal hands back for exactly that, then stays
silent and keeps collecting. A certificate is checked by a rule that does not
ask whether the checker voted.
Quorum is three of four, so the others certify without it and it accepts their
block. `anchor` relies on that, and so does the equivocation case.

**The mesh re-forms after boot.** `connect` is the opening rendezvous and
it returns; nothing accepted an inbound link afterwards. A validator that
restarted dialed into a backlog no one was reading — `peers 0 of 3` while the
three peers it could see carried on, each still holding a socket that had
already died. It is worth seeing how that reads from outside: the node serves
RPC the whole time. Up, and not in the cluster. Now the rendezvous runs once a
round for as long as the node runs, under the same rule about who dials, so a
pair still ends with one link. A whole mesh pays nothing, because every peer
is already held and no dial is attempted.

**It still cannot catch up, and that one is not fixed.** The journal outlives
the chain: votes are on disk, the C-chain is not — `Evm::new(genesis)` builds
the world in memory. So a restarted node is at height 0 holding a journal that
names heights 1 to N. Blocks do reach it — it is in the mesh — but a node can
only accept one whose parent it already holds, and nothing asks a peer for the
ones in between. Rejoined and caught up are different things, and the log says
which one this is:

    peers         3 of 3
    following a peer's block: the chain refused the block: no state for the parent

repeating while the cluster moves on without it. Closing it means either
keeping the chain across a restart or fetching the history from a peer, and
both are additions. Note the two fixes above are still what makes that work
reachable: a node cannot rejoin on a certificate that arrives over a link it
does not have.

A caution that outlives all three. Every one of these looked healthy from
outside — the RPC answers `eth_chainId` and `eth_blockNumber` from a chain
that has decided nothing. Ask twice, a minute apart, and compare.
