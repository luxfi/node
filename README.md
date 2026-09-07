# node

Three node runtimes — Go, Rust and C++ — one build entry point, and the
differentials that hold them to the same answers.

## What it is

`node` imports three existing implementations rather than adding a fourth.
Nothing under `runtime/` or `gpu/` is a copy or a fork; each is a README naming
the checkout it builds and the exact invocation. Delete this repository and
every one of them is exactly as it was.

The exception is `chains/`, where a port had no clean home. `chains/rust/xvm`
is the X-Chain in Rust — a real port against the Rust node's VM seam, not a
shim — and `chains/{rust,cpp}/*` hold the chain ports the differentials
compare.

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
