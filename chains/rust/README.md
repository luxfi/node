# chains/rust

The chain suite in Rust, behind `~/work/lux-rs/node`'s VM seam (`src/vm.rs`).
`runtime/rust` already runs a C-Chain today via `revm` (see its README); this
is the rest of the suite.

- **`platformvm/`** — the P-Chain. Ported from `~/work/lux/node/vms/platformvm`.
  Validators, staking, permissionless entry, networks. Its wire is checked
  byte-for-byte against the Go P-Chain's own constructors. See
  `platformvm/LLM.md` for what is ported, what is deliberately absent, and how
  the byte-identity is regenerated.

  ```
  cd platformvm && PATH=~/.cargo/bin:$PATH cargo test
  ```

The X, Q and Z chains are not here yet.
