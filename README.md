# node

`github.com/luxfi/node` — the Lux node, in three languages, with the
differentials that hold them to the same answers.

## What it is

The Go node lives here. It is not fetched and not vendored: `cmd/luxd` and
everything under it are this module, so the repository is the thing its import
path names rather than a host that reaches outside itself for it. It runs the
primary network's chains — P X C Q Z A B G K M F, and D through its plugin —
and `make luxd RUNTIME=go` builds it.

Rust and C++ sit beside it. `runtime/{rust,cpp}` name the checkouts they build
and the exact invocation; neither is a copy or a fork. `chains/{rust,cpp}/*`
hold the chain ports, and `chains/rust/xvm` is the X-Chain in Rust — a real
port against the Rust node's VM seam, not a shim.

That arrangement is the point: the reference implementation and the two ports
are one checkout, so parity is something you run rather than something you
assert.

## Opting into a chain

Some chains cost an operator something that validating the primary network did
not ask for — co-location with a matcher, an HSM, a GPU, a custody role — so
the node runs them only when named:

```sh
luxd --chains=D,B,M      # by letter or alias; an unknown name is refused at boot
```

Without it, D, B and M decline even under `--track-all-chains`. A chain becomes
permissionless by dropping `Consent` from its row in `node/vms.go`.

## The differentials

The point of the repository. One corpus, three implementations, a runner that
understands nothing and compares strings.

```sh
make chains        # 629 P/X/Q/Z/D/F wire vectors, all three languages
make precompiles   # 237 EVM precompile calls, all three languages
make bench         # the same chain corpus, timed
```

Each runtime already tests itself, and each passes. That is the problem a
differential exists for: a suite written against an implementation agrees with
the implementation it was written against, so a fork between two of them is
invisible from inside either one. `make chains` found thirteen when it was
first run; `make precompiles` found the BLS12-381 layout on its first
three-way run.

A run fails if any implementation printed no row at all. Silence is not
agreement — with four voices, three answering is still a comparison, it
agrees, and the fourth's absence would read as a pass.

## The chains, and where each language stands

Six chains, three languages. The differential compares all three against a
corpus the Go reference itself generated, so a row here is a measurement, not
a claim.

| chain | vectors | Go | Rust | C++ |
|---|---:|---:|---:|---:|
| P — platformvm | 159 | 42,398 | 33,859 | 31,217 |
| X — xvm | 49 | 13,306 | 18,894 | 17,148 |
| Q — quantumvm | 81 | 2,919 | 7,071 | 9,239 |
| Z — zkvm | 137 | 8,047 | 10,814 | 9,284 |
| D — dexvm | 35 | 1,966 | 6,303 | 7,200 |
| F — fhevm | 264 | 3,497 | 15,834 | 13,208 |

Source lines, non-test. All six exist in all three languages, and `make chains`
agrees on every compared field of all 725 vectors with no implementation
silent.

Read the line counts as shape, not as progress: a port is larger than its
reference where the reference leans on a runtime the port has to state for
itself, and smaller where the reference carries history the port does not.
Agreement on the corpus is the measurement that means something.

### What stops the three sharing one network

Measured, not assumed:

`luxd-rust` and `luxd-cpp` are both hosts, but they form a committee
differently. The Rust host publishes a validator line — a post-quantum identity
and its BLS key — and takes a committee file plus `--peers`. The C++ host takes
`--index I --n N --base-port P` and derives its peers positionally. Neither can
read the other's committee, so they do not yet meet on one mesh.

`luxd-go` is not a host at all. It is the C-Chain VM plugin, so it cannot be a
third validator until a Go host exists to run it.

Two things have to land before three implementations can co-finalise a block:
one committee format both hosts read, and a Go host. Everything else the
differential needs already agrees.

### On the clean cut

`node`'s reason for existing says `luxfi/node` carries ava-labs lineage. That
was true of its history and is no longer true of its dependency closure: a
`go list -deps ./main` over `luxfi/node` returns 1,051 packages and **zero**
matching ava-labs or avalanche. The lineage claim is about where the code came
from; the closure is clean today, and the two are different questions worth
keeping apart when deciding what this repository may import.

### What the three are, and are not

`luxd-rust` and `luxd-cpp` are full node hosts — they take a committee and
serve. `luxd-go` is the C-Chain VM plugin, not a daemon: the Go node host that
`luxfi/node` ships is not part of this repository's clean cut, and no
replacement for it has been built here yet. Until one is, the three cannot
stand up as peers on one network, and the differential compares them as
libraries rather than as validators.

The wire is no longer written by hand in any of them. Thirteen `.zap` schemas
generate the readers and builders for all three languages; the five
hand-written implementations that preceded them are deleted.

## Build

```sh
make luxd RUNTIME=go      # bin/luxd-go    — chains/evm, the C-Chain VM plugin
make luxd RUNTIME=rust    # bin/luxd-rust  — lux-rs/node, a full host
make luxd RUNTIME=cpp     # bin/luxd-cpp   — lux-cpp/node, a full host
make all                  # all three plus gpu; nonzero exit unless 3/3
make conformance          # the pop/verdict corpus, all three languages
```

Every sub-build's exit code is checked. `cargo` and `ctest` have both reported
stale success through a wrapper that did not look, so nothing here trusts a
green Makefile over a red sub-build.

## Where the detail is

`LLM.md` — what each runtime actually is, what the differentials found, and
the gaps stated plainly. `conformance/README.md` and
`conformance/PRECOMPILE.md` — how each harness works and what its verdicts
mean. `docs/` where present.

`AGENTS.md`, `CLAUDE.md` and `GEMINI.md` are symlinks to `LLM.md`. One
document, whoever is reading.

## Licence

BSD 3-Clause.
