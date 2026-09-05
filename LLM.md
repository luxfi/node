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
chains/{go,rust,cpp}    the P/X/C/Q/Z chain suite — rust/xvm is real, the rest READMEs
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
make chains               # the P/X chain differential, all three languages
make chains-corpus        # regenerate that corpus from the Go reference
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
compared field of all 53 vectors, and no field goes uncompared. `make chains`
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
