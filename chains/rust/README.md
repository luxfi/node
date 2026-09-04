# chains/rust

The chain suite in Rust, behind the VM seam of `~/work/lux-rs/node`
(`src/vm.rs`). `runtime/rust` already runs a C-Chain there via `revm`; this is
the rest of the suite.

| chain | crate | state |
| --- | --- | --- |
| X — the UTXO ledger | `xvm/` | ported, 239 tests green, byte-identical to Go |
| P, Q, Z | — | not started |

## xvm

```
cd xvm && PATH=~/.cargo/bin:$PATH cargo test
```

Five dependencies, no C library, no path dependency on the node. See
`xvm/LLM.md` for what was ported, what was deliberately left, and the Go golden
vectors that prove the wire and the state root are the same bytes in both
languages.
