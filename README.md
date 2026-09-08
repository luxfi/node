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

## What is not here

The D-Chain matcher, the F-Chain's FHE implementation and the GPU kernels are in
`luxfi/compute`, which is private. Those three are ours; the rest of this
repository forks public work and anyone building on this network has to be able
to read and run it, so the two do not share a visibility.

`chains/{rust,cpp}/{dexvm,fhevm}` and `gpu/` are shims naming that checkout —
the same arrangement `runtime/rust` and `runtime/cpp` already use. The
differential builds them from `$(COMPUTE)`, so the rows below still measure
them; they are simply not stored here. The pure-Go DEX in `luxfi/dex` is the
public one and is unaffected.

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
agrees on every field of all 725 vectors, each answered by at least two running
implementations. A field a port declines is reported, never counted as
agreement.

Read the line counts as shape, not as progress: a port is larger than its
reference where the reference leans on a runtime the port has to state for
itself, and smaller where the reference carries history the port does not.
Agreement on the corpus is the measurement that means something.

### How a network is named

A network is a committee: one line per validator carrying the post-quantum
identity it is named by, the BLS key it votes with, and the proof it holds
that key. A node id is derived from the identity, a weight is one, and file
order is the order `--peers` addresses line up with. Nothing in the file is
taken on trust — the proof is checked when the set is built.

The commitment to a set is a SHA-256 over its validators sorted by node id,
each written `nodeID || weight || len(key) || key`. That encoding is written
once per language and no more: `validators.SetRoot` in Go, `Committee::root`
in Rust. Two writings of it would agree until they didn't.

Thirteen `.zap` schemas generate the wire readers and builders for all three
languages.

## Build

```sh
make luxd RUNTIME=go      # bin/go/luxd    — ./cmd/luxd
make luxd RUNTIME=rust    # bin/rust/luxd  — lux-rs/node
make luxd RUNTIME=cpp     # bin/cpp/luxd   — lux-cpp/node
make all                  # all three plus gpu; nonzero exit unless 3/3
make conformance          # the pop/verdict corpus, all three languages
```

Every sub-build's exit code is checked. `cargo` and `ctest` have both reported
stale success through a wrapper that did not look, so nothing here trusts a
green Makefile over a red sub-build.

## Where the detail is

`LLM.md` — what each runtime actually is, what the differentials found, and
`conformance/README.md` and
`conformance/PRECOMPILE.md` — how each harness works and what its verdicts
mean. `docs/` where present.

`AGENTS.md`, `CLAUDE.md` and `GEMINI.md` are symlinks to `LLM.md`. One
document, whoever is reading.

## Licence

BSD 3-Clause.
