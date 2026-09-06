# node2 — one shim, one Makefile, three node runtimes

`node2` is a structural shell, not a fourth implementation. It imports the
three existing Lux node runtimes — Go, Rust, C++ — behind one `Makefile` and
runs the one conformance corpus that already holds across all three. Nothing
under `runtime/` or `gpu/` is a copy or a fork; each is a README naming an
absolute path on this machine and the exact invocation that builds it. Delete
`node2` and every one of those repos is exactly as it was.

One thing under `chains/` is not a README. `chains/rust/xvm` is the X-Chain
itself, in Rust, ported from `~/work/lux/node/vms/xvm` against the VM seam of
`~/work/lux-rs/node` — 13,388 lines, 239 tests, and twelve golden vectors
printed by the Go chain that prove the two produce the same bytes and give a
transaction the same name. It lives here rather than in a shim because there
was no clean Rust X-Chain to import; see `chains/rust/xvm/LLM.md`.

The reason this exists: `luxfi/node` (`~/work/lux/node`) carries ava-labs
lineage, and three separate teams have since built clean replacements for
its role in three languages, none of which had a shared home or a shared
proof that they agree with each other. `node2` is that home. `luxfi/node`
is untouched by this repo and stays running until conformance passes here —
retiring it is the outcome this repo works toward, not a precondition of it.

## Layout

```
runtime/{go,rust,cpp}   thin shim READMEs — what gets built, from where, how
chains/{go,rust,cpp}    the chain suite — cpp and rust each run six chains, go a README
gpu/                    thin shim to the GPU kernel library
conformance/            wires the existing pop/verdict corpus per language
bin/                    build output — luxd-go, luxd-rust, luxd-cpp (gitignored)
Makefile                the one build entry point
```

## The three runtimes, honestly

**Rust and C++ are real, running node hosts, already clean.**
`~/work/lux-rs/node` and `~/work/lux-cpp/node` each execute EVM blocks
themselves, decide them by BLS quorum certificate over a real ZAP/TCP mesh,
and serve JSON-RPC. Rust co-certifies with the Go `luxd` today, in a live
mesh. Neither imports `luxfi/node` or ava-labs. `make luxd RUNTIME=rust` and
`make luxd RUNTIME=cpp` build exactly these, unmodified, into
`bin/luxd-rust` and `bin/luxd-cpp`.

**`node2` is Lux-branded only.** `lux-rs/node`'s `main.rs` and
`lux-cpp/node`'s `src/noded.cpp` each also build other downstream orgs'
brand-specific argv0 binaries from that same source. `node2` builds none of
them, names none of them, and its Makefile and shims reference no brand but
Lux — `make luxd RUNTIME=rust` builds only the `luxd` bin target, `make luxd
RUNTIME=cpp` only the `luxd` CMake target. Those other-brand targets belong
in their own skinny importer repos, one per downstream org, that pull in
`node2` and layer on brand and config — not inside `node2` itself, and not
built by anything here.

**Go is not.** `make luxd RUNTIME=go` builds `~/work/lux/chains/evm` — the
C-Chain VM plugin — into `bin/luxd-go`. It is real and it is clean (proof
below), but it is not a node: run it and it logs a ZAP-serve line and exits
asking for `VM_RUNTIME_ENGINE_ADDR`, because it is a plugin waiting for a
host to dial it over `--plugin-dir`. `luxfi/chains` has no `cmd/luxd` — its
`cmd/` holds one validation tool, `dex-assets-validate`, and every other
buildable artifact in it is a plugin the same shape as `evm`. The host those
plugins are built against today is `luxfi/node` itself.

### The clean cut, precisely

Checked with `go list -deps` against each of the thirteen VMs `luxfi/chains`
builds, in isolation:

```
evm=0  keyvm=2  quantumvm=2  schain=1  oraclevm=4  fhevm=75
aivm=3 bridgevm=3 graphvm=3 identityvm=3 mpcvm=3 relayvm=3 zkvm=3
```
(counts are `luxfi/node` packages appearing in that VM's own dependency
closure — `go list -deps ./<vm>/...`, or `./<vm>/cmd/plugin/...` where the VM
has no root `main.go`)

`evm` is the only zero. The other twelve import exactly five `luxfi/node`
subpackages everywhere they touch it at all — `config`, `version`, `vms`,
`vms/artifacts`, `vms/types/fee` — the plugin-SDK surface a `ChainVM` needs to
declare itself to a host, not networking or consensus code. `ava-labs` itself
has zero real imports anywhere in the module (`go mod why -m
github.com/luxfi/node` shows the one real path, `chains/mpcvm/fhe →
node/config`; the sole `ava-labs` grep hit, `bridgevm/evmclient.go:12`, is a
comment disclaiming it).

`make luxd RUNTIME=go` proves this at build time, every time — it fails the
build if either name reappears in `chains/evm`'s dependency graph, rather
than trusting that it stayed true.

### The actual gap

A clean Go node **host** does not exist on disk yet, anywhere. That is new
implementation work — a plugin loader, P2P over ZAP, bootstrap, and
`luxfi/consensus` wired together — which this structural-shell pass does not
do, on the same principle that keeps the other `chains/` slots empty: import
where something clean exists, write it once where nothing does. Its template already exists, just not in Go:
`lux-rs/node` is exactly that architecture, already clean, already proven
against the Go side in a live mesh. A Go host (`luxd2`) mirrors it. The other
half of the gap is smaller than it looks — extracting the five-package
plugin-SDK surface out of `luxfi/node` into its own package makes all
thirteen `chains` VMs clean, not just `evm`, without touching any VM's logic.

## A note on building against live checkouts

`~/work/lux-cpp/node` (and the repos it reuses) are shared checkouts other
sessions actively edit, uncommitted, while this Makefile is reading them —
that is the cost of a thin shim, and it is real, not theoretical: mid-scaffold,
`make luxd RUNTIME=cpp` failed once with `AWS-LC not found` because another
session's in-flight, uncommitted edit to that repo's `CMakeLists.txt` (adding
TLS/peer/staking source and a new AWS-LC dependency) landed between two
otherwise-successful builds and was picked up by CMake's own
`cmake_check_build_system` reconfigure check. The next build, seconds later,
was clean again. Nothing here papers over that — a failure like it is exactly
what the exit-code check in `luxd-cpp` exists to catch and report, not hide,
and `node2` does not touch or fix the other session's checkout to make it go
away. If `make luxd RUNTIME=cpp` fails, retry before assuming the shim itself
is broken; if it persists, `git status` in `~/work/lux-cpp/node` first.

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

**`make chains` passes.** It covers six chains — P and X from `luxfi/node`, and
Q, Z, D and F from `luxfi/chains`, which had no vector at all until they were
added, which is the same shape the P-chain fork hid in. All three columns now
answer all six, and every compared field of all 725 vectors agrees.

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

`make precompiles` is described in `conformance/PRECOMPILE.md`. **It fails, and
what it found is the reason it exists.**

Across 235 calls the three runtimes split two against one, and the one is Go:
Rust and C++ disagree with each other on 15 fields and each disagrees with Go
on roughly 200. Go is alone because it is the only one that applies the Lux
precompile modules, and those modules are laid out on the **pre-final**
EIP-2537, which still had separate multiply precompiles. The final EIP dropped
them, so every BLS12-381 address from `0x0c` up is shifted by one slot:
`0x0d` is a G1 multi-exponentiation to the Lux module and a G2 addition to
geth, revm and cevm — different curve groups, not different gas. 243 of the
252 Go-versus-C++ disagreements name a `bls12381` module.

All seven keys are enabled on **mainnet**, in
`lux/genesis/configs/mainnet/upgrade.json` at a timestamp in December 2025, and
on testnet and devnet, and at zero on local and localnet. And nothing else can
serve those addresses there: mainnet's `cchain.json` stops at Cancun, geth's
stock BLS table starts at Prague, so the Lux modules are the sole occupants.
That makes it a fork at either revision a port could be run at, for a different
reason each time. Held at Cancun, matching the chain, cevm gates those
addresses on Prague and serves nothing where Go serves seven. Held at Osaka, as
this corpus asks, it serves the final layout where Go serves the draft. There
is no revision at which a port can express the layout the C-Chain runs, which
is the gap itself and not an artifact of how the question was put.

One of the three has since closed. cevm dispatched precompiles through a dense
array keyed on the last two bytes of an address, bounded by a full-address
comparison, so no Lux address could be reached at all: inference at
`0x0300…0003` shares its last two bytes with ripemd160, and the whole
`0x…0122xx` post-quantum block was equally unreachable. It matches the whole
address now for anything outside the stock range, and inference is registered
where Go serves it. Go and C++ return the same fourteen tokens and the same
600000 gas for the generate call both were written against, and the corpus
carries that vector. Rust answers ABSENT, because `lux-evm` is revm's
`EthPrecompiles` plus a profile gate and registers no Lux module anywhere.

Two smaller ones. At `0x…0100` the Lux `secp256r1` module would charge 3450
where the stock table charges 6900, and the module would win because
`LuxPrecompileOverrider` is consulted first — but its key is enabled on no
network, and stock p256verify is Osaka-only on a chain at Cancun, so nothing
serves that address anywhere today. It becomes a fork the moment either side is
switched on, and they cannot both be. And address zero is not unclaimed: the
dead-address module (LP-0150) is registered there and reads chain state, so it
cannot be answered for without a chain.

The residue is two reporting conventions rather than two behaviours. revm
cannot say what a refusal cost, because it computes the price inside the
function that does the work, so it writes SKIPPED rather than a zero it does
not mean. And cevm prices an unpriceable input above the limit, so it calls a
malformed length "out of gas" where revm calls it a refusal — 15 vectors. At
transaction level both consume everything offered, so neither is a fork.

This is **consensus-layer** conformance, not **node-level** — three live
`bin/luxd-*` daemons handed the same blocks over real sockets and checked
for agreement is a different, larger harness that does not exist yet, and
wiring it before `runtime/go` has an actual node host to point at would not
test what its name claims. It is Phase 2, alongside the Go host itself.

## Build

```sh
make luxd RUNTIME=go      # bin/luxd-go    — chains/evm (C-Chain VM plugin)
make luxd RUNTIME=rust    # bin/luxd-rust  — lux-rs/node (full host)
make luxd RUNTIME=cpp     # bin/luxd-cpp   — lux-cpp/node (full host)
make all                  # all three + gpu; reports each; nonzero exit unless 3/3
make gpu                  # lux-gpu/gpu kernel library
make conformance          # the pop/verdict corpus, all three languages
make chains               # the six-chain differential, all three languages
make chains-corpus        # regenerate that corpus from the Go reference
make precompiles          # the EVM precompile differential, all three languages
make precompiles-corpus   # regenerate that corpus from the Go reference
```

Every sub-build's exit code is checked explicitly — `cargo` and `ctest` have
both reported stale success from a wrapper that didn't check, so nothing here
trusts a green Makefile over a red sub-build.

## Known corrections against the original brief

- **GPU path.** The brief names `~/work/lux-gpu/gpu`; it does not exist on
  this machine. The real checkout of that repository (`git remote -v` →
  `git@github.com:lux-gpu/gpu.git`) is at `~/work/luxcpp/gpu` — confirmed by
  remote, not assumed by path similarity. `gpu/README.md` records the
  correction; the Makefile points there.
- **This directory was not empty.** `~/work/lux/node2` already held a clean,
  fully-pushed, but stale clone of `lux-cpp/node` (same remote, older HEAD
  than the live checkout at `~/work/lux-cpp/node`) — apparently left from
  earlier work exploring the same "second node" idea in C++ alone (its own
  history has a commit titled "drop the 2: this is the C++ node, not a
  second one"). Nothing was lost — it had no uncommitted work and a newer
  copy of the same history lives on at `~/work/lux-cpp/node` and on GitHub.
  Preserved rather than deleted, at
  `~/work/lux/node2.stale-luxcpp-node-clone-see-LLM-md`, in case anything
  about it turns out to matter later.
- **License label.** This repo's `LICENSE` is the plain, permissive BSD
  3-Clause text — the same one `~/work/lux/chains` uses for what its own
  `LICENSING.md` calls the "public tier": usable, forkable, redistributable,
  including commercially, with no added restriction. Several sibling repos'
  file headers say `SPDX-License-Identifier: BSD-3-Clause-Eco` while shipping
  this same plain text (`lux-cpp/node`'s `LICENSE` file is in fact
  Apache-2.0 under that header, unrelated to either). A *different*,
  genuinely restrictive "BSD 3-Clause Ecosystem License" also exists in this
  ecosystem (`~/work/luxcpp/dex/LICENSE` — bars use outside Lux-authorized
  networks, requires a separate commercial license) and is reserved for
  monetizable primitives, not core node software. A node that other
  operators are meant to run needs the permissive one; that is what shipped
  here. Flagged in case "-Eco" was meant to invoke the restrictive variant
  specifically.

## ZAP, and where it lives

ZAP is a bidirectional binary protocol with pipelining: both peers on a
connection initiate, and requests pipeline — many in flight, answers back in
whatever order the peer finishes them. It is not a codec, and treating it as
one is how this repo got into trouble.

Both C++ chains used to carry their own copy of the wire —
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

The wire-format suite that used to sit in `chains/cpp/platformvm/test/` moved
to the SDK with the implementation, case for case. A wire rule belongs in one
place, and its test belongs beside it.

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
node2's own dependency graph, which `make luxd` still greps and still fails on.

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

The differential already hands all three the same 208 vectors and makes each one
parse and verify them, which is a fair identical workload. `make bench` times
it. Every evaluator takes an optional repeat count after the corpus path, walks
the whole corpus that many times, and prints its own elapsed time to stderr as
`B <impl> <vectors> <repeats> <seconds>`. `conformance/bench` runs them and
prints the table. Without a count nothing changes and `make chains` is untouched.

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
**Timing it** — the sharpest being that `chains/cpp/xvm` answers `SKIPPED` for
`exec` and is therefore the fastest thing in the table because it is the only
one that stops after the syntactic pass.
