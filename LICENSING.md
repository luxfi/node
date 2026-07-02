# Licensing

This repository is licensed under the **BSD 3-Clause License** (see
[LICENSE](LICENSE)). It belongs to the **public** tier of the Lux
three-tier IP strategy: anyone may use, fork, or redistribute it,
including for commercial purposes, subject to the BSD-3 terms.

For the canonical Lux IP and licensing strategy, see:
<https://github.com/luxfi/.github/blob/main/profile/README.md>

For commercial inquiries that go beyond BSD-3 (e.g. private moat
acceleration kernels), contact `licensing@lux.network`.

## Upstream attribution

See [NOTICE](NOTICE) for the full attribution. In summary:

- **avalanchego** (Ava Labs, Inc.) — this repository is derived from
  [ava-labs/avalanchego](https://github.com/ava-labs/avalanchego), licensed
  under the **BSD 3-Clause License** (© 2019 Ava Labs, Inc.). BSD-3 is
  permissive; the Lux additions here are likewise BSD-3-Clause.
- **go-ethereum** (The go-ethereum Authors) — EVM support derives from
  [go-ethereum](https://github.com/ethereum/go-ethereum). It is **not**
  vendored in-tree; it is consumed as the external Go module
  `github.com/luxfi/geth`, which retains go-ethereum's original licenses:
  the library is **LGPL-3.0-or-later** and the command-line tools are
  **GPL-3.0**.

**Copyleft flag:** the LGPL-3.0/GPL-3.0 terms of the go-ethereum-derived code
(via `github.com/luxfi/geth`) are **not** superseded by this repository's
BSD-3-Clause license. Distributing compiled node binaries must honor LGPL-3.0
for the linked geth library (published source of that code and its
modifications at <https://github.com/luxfi/geth>, and user ability to relink).
