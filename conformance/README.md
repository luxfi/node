# conformance

`make conformance` runs the existing pop/verdict conformance corpus against
all three consensus implementations — Go, Rust, C++ — and reports each
honestly: PASS with the real check count, or `skipped` with the reason, never
a false green.

## What this is, and what it is not

This is **consensus-layer** conformance: the corpus at
`~/work/lux/consensus/conformance` states what a validator signs, what a
certificate looks like on the wire, and what the finality predicate decides —
generated from the Go definitions and checked byte-for-byte against Rust and
C++. It is real, it already exists, and all three sides already pass it (tag
`v1.36.91`).

It is not **node-level** conformance — three live `bin/luxd-*` daemons handed
the same blocks and votes over real sockets, checked for agreement. That
harness does not exist yet; wiring it is Phase 2, once `runtime/go` has an
actual node host to point at (see `runtime/go/README.md`). Building it now,
against a Go side that is a VM plugin rather than a node, would not test what
its name claims.

## What each target runs

- `make conformance-go` — `go test ./conformance/...` in
  `~/work/lux/consensus`. This is the corpus's own source of truth: every
  case is read live from `CanonicalVoteMessage`, `QuorumCert.MarshalBinary`,
  `config.TwoThirdsStakeFloor`, the finality ladder, etc. — never restated —
  so an edit to a rule moves the corpus and this fails at the source.
- `make conformance-rust` — five integration test binaries in
  `~/work/lux/consensus/pkg/rust`: `conformance`, `cert_conformance`,
  `fpc_conformance`, `pop_conformance`, `verdict_conformance`. The last two
  are the pop-proof and verdict vectors the brief names by name
  (`tests/vectors/pop.json`).
- `make conformance-cpp` — the two binaries `~/work/lux-cpp/consensus/build`
  already builds, `conformance_test` and `pop_conformance_test`, rebuilt
  in place and run directly. `skipped` (not a false pass) if that build
  directory is not configured.
