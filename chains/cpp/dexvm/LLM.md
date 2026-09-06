# dexvm — the D-Chain in C++

The DEX's real-assets-only admission layer, ported from Go's
`~/work/lux/chains/dexvm/registry` (1,966 source lines, 27 tests) onto
`~/work/lux-cpp/node`'s VM seam.

One property is made structural rather than incidental: **every asset the DEX can
credit or debit corresponds to a real object on-chain** — an ERC-20 contract on
the C-Chain, the C-Chain native coin, or a UTXO asset on the X-Chain. There is no
synthetic class, no ASCII-ticker identity, no declared-but-unbacked credit. An
asset's identity is a hash of *where it lives*, so two parties given the same
chain derive the same 32-byte AssetID, a fabricated asset has no preimage, and a
market over one has no id to resolve.

```
cmake -S . -B build && cmake --build build -j && (cd build && ctest)
```

## What is here

| file | what it holds |
| --- | --- |
| `id` | the identifier, its two renderings (cb58 and hex), and the error identity |
| `asset` | `derive_asset_id`, `market_id`, the length-prefixed fold — the consensus bytes |
| `registry` | the admitted set, and the ONE door an asset enters through |
| `forbidden` | the deny half of admission: mock liquidity, off-network universes, ticker-ids |
| `consensus_mode` | under which posture value may activate, and what it must then say |
| `gate` | the single fail-closed boot gate |
| `json` | the manifest's encoding, byte-identical to Go's `MarshalIndent` |
| `manifest` | the per-network declaration, its content pin, and how it becomes a live registry |
| `embedded` | the three committed manifests, carried in the binary; localnet synthesised from live ids |
| `runtime_verifier` | the node-side verifier: manifest ids bound to the ids this node runs |
| `zap` / `store` | the codec, and the durable log the set rests on |
| `tx` / `state` | what a block carries, and what executing it produces |
| `vm` | the seam implementation |

## The identity bytes are cross-language, and they are checked

`test/golden.hpp` holds values **printed by the Go reference**, not by this port:

- the three AssetID vectors, which Go's registry and `luxfi/dex` *both* assert,
  so a registered AssetID and a swap-derived one name the same asset;
- the cb58 renderings Go's `ids.ID.String()` produces, which is how a manifest
  spells a chain id;
- for each committed manifest: its file hash, its C-Chain id, the AssetID its
  asset derives, and the hash of **Go's own re-encode** of it.

That last pair separates two different claims. The *file* hash proves this port
reads the same bytes; the *re-encode* hash proves it writes them. They differ on
purpose: the committed manifests were hand-written, so they carry a trailing
newline and unsorted `chainLabels`, and no marshaller reproduces either — Go's
re-encode does not equal Go's file. A port that made them equal would be the
divergent one.

## What is NEW here, with no Go counterpart

**The block and transaction layer.** Go's dexvm is a library the C-Chain
settlement precompile calls in-process; it has no block of its own. So
`tx.hpp`/`state.hpp`/`vm.hpp` — the two transactions, their ZAP encoding, the
execution root, the durable rows — are defined *here first*. The registry
semantics inside them are the ported ones, unchanged and identical to Go's; it is
the envelope around them that is new.

A Go or Rust D-Chain must therefore be rendered **from** this definition rather
than written beside it. Two chains that each invented a block format would
disagree the first time they were asked the same question.

The block layer's own rules, stated so they can be mirrored:

- Block bytes are `parent | height | txs`. They carry **no root**: a receiving
  node derives the root by executing, so a proposer cannot assert a result it did
  not produce.
- Transaction order within a block is consensus. A market may follow the two
  assets it names in the same block, and the same transactions in another order
  can admit a different set, so order is preserved exactly as encoded.
- The execution root is `fold_rows` — the same length-prefixed SHA-256 discipline
  the AssetID uses — over the rows in ascending key order.
- A block sits on the last-accepted block at the next height, or this node
  refuses to vote for it.
- **Reject hands the transactions back.** They were never refused; they lost a
  race. A node that dropped them would disagree with every other node about what
  is still pending, then propose a block built from that disagreement.
- A block is decided once, either way: accept is inert after reject and reject
  after accept.

## What is deliberately absent

**`rpcverify`** (228 of the reference's 1,966 lines). It is a live-network CI
tool: it dials a C-Chain EVM RPC via `luxfi/geth`'s `ethclient` and a P/X-Chain
JSON-RPC endpoint to prove each token exists before an artifact ships. The
reference keeps it in its own package precisely so the consensus path carries no
JSON-RPC dependency, and nothing on the consensus path calls it. Porting it would
mean carrying an HTTP and EVM-RPC client into a chain that must not have one.

Its absence is not a gap in the proof, because the proof was always in two
halves and only one of them is the node's:

- **CI** proves each token EXISTS on the live target net, and pins the artifact's
  content hash. That half stays in Go.
- **The node** proves chain-IDENTITY binding, structure and policy at boot, with
  no remote RPC in the path, and refuses any manifest whose bytes are not the
  ones CI approved. That half is `runtime_verifier` + `manifest` + `gate`, and it
  is here in full.

## One place the reference's comment and its code disagree

`looksLikeASCIITickerID`'s doc comment offers `"LUX-USDC@venue"` as an example of
what it catches. Its code does not catch it: one lowercase letter takes the
string out of the ticker alphabet. Verified against Go, which answers `false` for
exactly that string, and `true` for `"LUX/USDC"`, `"LUX-USDC@VENUE"` and `"A-B"`.

The **behaviour** is ported, not the comment. A port that "fixed" this would be
the divergence, and `test/gate_test.cpp` states both the rule and this case.

## Two deliberate strengthenings

Neither changes a decision; both remove a source of nondeterminism.

- **Iteration is ordered.** Go walks a map, so which of several bad records the
  boot gate reports first is random. Here it is ascending id order. The decisions
  do not depend on order; only the message does.
- **Errors carry an identity.** Go's callers ask `errors.Is(err, ErrUnknownAsset)`
  and branch on the answer, so `Err` is an enum that travels with the message and
  survives wrapping. A port that returned only prose would have dropped a
  distinction the reference depends on.

## Verification

352 assertions across ten suites, all green under a fresh build, and again under
`-DDEXVM_SANITIZE=address,undefined` with leak detection on. The mutation passes
in `vm_test` and `json_test` drive 4,000 mutated inputs through each untrusted
decoder — block, transaction, manifest — plus every truncation, and assert every
one reaches a decision rather than a crash or a half-believed value.
