# chains/rust — Phase 2 placeholder

Empty. Port target: the P/X/C/Q/Z chain suite, ported from `chains/go`'s
reference (`~/work/lux/chains`) into `~/work/lux-rs/node`'s VM seam
(`src/vm.rs`). `runtime/rust` already runs a C-Chain today via `revm`
(see its README) — this is the rest of the chain suite, in Rust, behind the
same seam.
