# runtime/rust

A thin shim. No Rust source lives here — `make luxd RUNTIME=rust` builds
`~/work/lux-rs/node` in place and copies its `luxd` binary to `bin/luxd-rust`.

## What it is

A complete, running Lux node: a validator mesh over real TCP sockets framed by
ZAP, BLS quorum-certificate finality via `lux-consensus` (the same crate the
conformance corpus checks Rust against), and a pure-Go-equivalent EVM
(`revm`) for the C-Chain. `co-certifies with luxd today` — the Rust host and
the Go `luxd` reach agreement in a live mesh, not just on paper. Its `main.rs`
also builds other orgs' brand-specific argv0 binaries — out of scope here.
`node2` is Lux-branded only; it builds `luxd` and nothing else from this
crate.

It carries no `luxfi/node` and no lux-private lineage — see its own `Cargo.toml`
header comment on the `lux-consensus` dependency for why that matters and how
it is kept true (there is a second, unpublished, stale copy of a
`lux-consensus`-shaped crate elsewhere on this machine; this shim always
resolves through Cargo's registry dependency, never a path override to that
one).

`runtime/go`'s README names this crate's architecture — plugin-equivalent VM
+ P2P-over-ZAP + bootstrap + `luxfi/consensus` — as the template a clean Go
node host would mirror.

## Build

```sh
cd ~/work/lux-rs/node
PATH="$HOME/.cargo/bin:$PATH" LUX_LIB_DIR=~/work/lux/crypto/dist cargo build --release --bin luxd
```

`LUX_LIB_DIR` locates `libluxcrypto` (ML-KEM-768 / ML-DSA-65) for `lux-pq`'s
own build script, which republishes the resolved path as `DEP_LUXPQ_LIB_DIR`
for this crate's `build.rs` to set as an rpath — the same library the Go node
links via cgo, so both languages verify under the same post-quantum
implementation.
