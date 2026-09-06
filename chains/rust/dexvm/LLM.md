# lux-dexvm — the Lux D-Chain in Rust

The DEX's real-assets-only admission layer: what an asset IS, what a market IS,
which kinds may be registered, and whether native value may activate at all.
Ported from the Go reference at `~/work/lux/chains/dexvm/registry`.

2,772 lines across 11 files. 56 tests, all green. No stubs.

## Build

```
PATH=~/.cargo/bin:$PATH cargo test
PATH=~/.cargo/bin:$PATH cargo build --release --bin conformance
```

One dependency, `sha2`. No C library, no path dependency, no network. The crate
builds from a cold cache in about twenty seconds.

## D is not a block chain, and this crate is not one either

Go's `chains/dexvm` is a **registry**: a library the C-Chain settlement
precompile calls in process. It has no block, no transaction and no VM, and so
neither does this port. That is why there is no `lux-node` dependency here and
no `vm.rs` beside the other chains'.

The decisions are still consensus decisions. Two implementations deriving
different bytes for one asset have forked the value plane without ever
disagreeing about a transaction — which is exactly why the differential asks for
an AssetID rather than for a wire, and why a D vector's wire column carries the
ARGUMENTS to a derivation.

A block layer written here would be a wire no chain writes and nothing could
check it: the corpus has no D block vector, because there is no D block.

## What proves it is the same chain

**`tests/golden.rs`** — identities the Go reference printed, pasted in
unchanged. The first three are stronger than that: they are asserted in TWO Go
homes, the registry's own KAT in `asset_golden_test.go` and `luxfi/dex`'s
`AssetIDGoldenVectors`, which pin the same strings so a registered AssetID and a
swap-derived one name the same asset by the same id. One test states the
PREIMAGE in full rather than only its hash, so a failure says which bytes moved.

**`tests/corpus.rs`** — this crate's evaluator over
`conformance/corpus/vectors.tsv`, held to Go's recorded answers on all 35 D
vectors and all five compared fields. It also checks the notes, which the
differential does not compare: the port raises the reference's own sentinels, so
its sentences come out byte-identical to Go's, down to where the 160-byte trim
cuts the em dash. A port that reproduced every verdict and none of the reasons
would be agreeing by coincidence.

**`src/bin/conformance.rs`** is the D evaluator for `make chains`.

## Layout

```
ids.rs        the 32-byte name, and the one hash that derives it
error.rs      what the chain refuses for, in two halves — see below
asset.rs      derive_asset_id, market_id, the length-prefixed fold
registry.rs   the admitted set, and the one door an asset enters through
market.rs     an admitted pair, pinned to two real assets by construction
mode.rs       under which posture value may activate, and what it must then say
network.rs    which networks bear value, and which are a developer's
```

## Decisions worth knowing

**A refusal has an identity and a sentence, and they are kept apart.** Go builds
the second from the first — `fmt.Errorf("%w: ERC20 token address must be 20
bytes, got 19", ErrBadRef)` — and `classify` unwraps to the first before it
reads any words. `Error::sentinel()` is that unwrapped half.

It is not tidiness. The wrapped text of one refusal ends "UTXO assetID must be
32 bytes"; a classifier reading the sentence sees the word `utxo` and calls a
malformed reference a missing ledger entry. That is one arm of one refusal going
wrong alone, which is the shape a differential is built to catch and a unit test
usually is not. `error.rs` has a test that states it directly.

**The closed set is the type, so the default arm has nothing to catch.** Go
carries `ConsensusMode` as a `uint8` and needs a `default:` that refuses, because
a `uint8` can hold 7. Here the three postures are the enum, so there is no
fourth value to fall through to and no arm to forget: an unmodelled posture is
not something the guard has to catch, it is something a caller cannot construct.
The refusal Go raises there (`ErrValueModeIllegal`) has no reachable
construction site and is therefore absent rather than dead.

**The borrow checker is the mutex.** The reference guards its maps because a
`*Registry` is shared by every caller. Here `register` takes `&mut self` and
`resolve` takes `&self`, which is the same exclusion checked at compile time; a
caller sharing one across threads wraps it once at the edge and reads pay
nothing. There is no lock to poison and no `unwrap` on one.

**`Register` cannot be called without a verifier.** Go checks for a nil
`ChainVerifier` and refuses, because it can be nil. `&dyn ChainVerifier` cannot,
so that refusal is absent for the same reason as the one above.

**The order of the shape checks is Go's, not a tidier one.** `validate_shape`
reads kind, tier, reference, chain, symbol; `derive_asset_id` refuses the empty
source chain BEFORE it looks at the reference. Those two orders differ, and both
are the reference's. A caller who makes two mistakes at once gets told about the
same one in both languages, and `asset.rs` has a test that pins it.

## What is NOT ported

- **`gate.go`'s boot gate** (`RefuseUnderSyntheticConfig`) and `forbidden.go`'s
  deny-scan. `NetworkClass` and `network_class_for` ARE here — the corpus asks
  for the class — but the scan they feed is not. Nothing in the corpus reaches
  it, and the labels it matches on do not belong in a Lux tree.
- **`manifest.go`, `embedded.go`, `json.go`, `runtime_verifier.go`** — loading a
  declaration and pinning its content hash. That is how a registry gets
  populated, not what it decides. The C++ port carries them; this one does not,
  and the differential asks nothing of them.
