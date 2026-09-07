# chains/rust/fhevm — the Lux F-Chain in Rust

A port of `github.com/luxfi/chains/fhevm` (3,497 non-test lines, 151 tests). Go is
the reference. Where the two disagree, Go is right: a cross-language divergence on
a chain is a fork, not a preference.

```
cd chains/rust/fhevm && PATH=~/.cargo/bin:$PATH cargo test
```

208 tests, no warnings, clippy clean.

## What F is, and what it therefore does not do

F is the COORDINATION plane for confidential compute. It records the PUBLIC
coordinates of an encrypted value — a handle, the digest of the off-chain
ciphertext body, its owner, the capabilities granted over it, and the threshold
decryptions asked for and answered. It holds no ciphertext body, no FHE secret key
and no decryption share.

**There is no homomorphic evaluation here because there is none in the reference.**
The bodies live in off-chain storage and the key shares live on the threshold
committee; the committee combines them off-chain and each member ATTESTS the
public handle of what came out. So there is no CPU evaluation path to port, no GPU
dispatch to route, and no kernel — public or private — that F could call. What
would be a compute path on another chain is, here, a threshold vote counted over
distinct members (`state::tally`). `tests/invariant.rs` holds that line
structurally: the persisted schema is pinned field for field by an exhaustive
destructure, and a source scan fails the build if the crate names any identifier by
which it could generate a key, produce or combine shares, or decrypt.

## The differential, and what it proves

`vectors/` is the gate. `run.sh` copies the REFERENCE Go package and the generator
into a scratch directory and runs it there — the reference checkout is never
written to — and the generator is a member of that package, because most of what
has to be pinned (the handle derivation, the effect key, the committee digest, the
record JSON, the database layout) is package-internal, and a generator that reached
for it from outside would have to restate it.

`tests/golden.rs` puts those answers to this port:

| what | how |
| --- | --- |
| ML-DSA-65 | a signature Go's `luxfi/crypto/mldsa` (circl) made, verified here under `fips204` — a different implementation of FIPS 204 |
| derivations | handle, permit id, request id, committee digest, address, cb58, the vmID word |
| the wire | content, signed preimage, full bytes, id and effect for one transaction of every kind, plus a block |
| records | the exact JSON bytes of all four record types, `omitempty` and nil-vs-empty included |
| the schedule | gas and fee for every (operation, scheme) pair, and every pair Go refuses |
| **the whole chain** | Go runs the full lifecycle and hands over its blocks; this replays exactly those bytes and must leave a **byte-identical database** — every record, nonce, balance, the burned counter, the height index and the block store |

The last row subsumes most of the others. F commits no state root, so nothing in
consensus would catch a divergence; that check stands in place of one, and it
stands across the language boundary as well as between two nodes of one language
(`tests/replay.rs`).

Regenerate after any change to the Go chain:

```
./vectors/run.sh [path-to-chains-checkout]     # default ~/work/lux/chains
```

A Rust test failing afterwards is the point.

## Deliberate differences of shape, none of meaning

- **Verify/accept/reject are on the VM, keyed by block id**, because that is the
  seam `lux-rs/node` declares (`lux_node::vm`, re-exported here as `host`) and
  because it is what the wire already does: those three calls travel as messages
  carrying an id. Go puts them on the block.
- **The read surface is the seam's `call`**, not a `gorilla/rpc` HTTP server. Same
  methods, same views, same refusals; the host owns the transport.
- **Errors instead of log lines.** Go logs a reverted transaction and a failed
  cache reload; here both are returned or discarded at the one place that can act
  on them. Nothing that Go treats as fatal is treated as anything else.
- **A genesis allocation is credited in sorted order.** Go walks a map, which
  reaches the same balances because sums commute — but nothing should have to
  depend on that.
- **`Transaction::scheme` is bytes, not a Rust `String`.** Go's `string` is a byte
  string: a scheme that is not valid UTF-8 is bounded, priced and re-serialized
  there unchanged, and a port that insisted on UTF-8 would refuse a block Go
  accepts, which is a fork rather than a stricter parser.
- **`mldsa::parse_public_key` checks the length and nothing else**, because that is
  all circl's `UnmarshalBinary` checks. A committee member Go seats and this port
  refuses is a fork; the rule that decides membership is `validate_committee`.
- **Time keeps nanoseconds.** Go's `time.Time` does, and the difference is
  observable: a proposer whose clock reads half a second past its own tip has a
  time that is AFTER the tip while its whole-second form is the same second.

## Where this does NOT appear

`conformance/corpus/chain_differential.json` has no F-Chain vectors, and its Go
evaluator (`conformance/gen/eval_go.go`) echoes the corpus's own stated
expectations rather than running the Go chain. Adding F rows there would report a
number without checking anything, so none were added: `vectors/` is the differential
that actually runs both implementations, and it is the one this crate is gated on.

## The layout

| module | what |
| --- | --- |
| `zap` | the arena format every byte on the wire is written in |
| `wire` | the two encodings, and the canonical rule that gives each exactly one byte-string |
| `transaction` | the six operations, their payloads, and the three gates each is held to |
| `state` | the four records, the derivations that name them, and the threshold decision |
| `gas` / `fee` | what an operation costs, and the ledger it is burned from |
| `batch` | the sequence rule a block's transactions satisfy together |
| `block` | verify, accept, reject |
| `vm` | the chain, its queue, and its store |
| `service` | the read surface |
| `gojson` | JSON written and read the way Go writes and reads it |
| `ids` / `mldsa` / `clock` / `db` | identifiers, the public signature path, time, the store |
