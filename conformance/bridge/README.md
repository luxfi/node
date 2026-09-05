# The bridge, asked of all three runtimes

`bridgediff` puts the same cross-chain-transfer questions to every running Lux
node runtime, in the same order, and prints where their answers differ. Run it:

```
go run . -endpoints \
  cpp-zoo=http://127.0.0.1:19730/,\
  go-lux=http://127.0.0.1:19600/,\
  go-zoo=http://127.0.0.1:19830/,\
  rust-hanzo=http://127.0.0.1:19780/v1/chain/hanzo
```

Exit code 1 means at least one runtime judged a release differently from the
others, or all of them broke a rule the bridge's own code states.

## What was measured, and against what

Three runtimes were live when this was written, each serving its own testnet:

| runtime | binary | chain | JSON-RPC |
|---|---|---|---|
| Go | `luxd` | lux-testnet 96368 | `127.0.0.1:19600/` |
| Go | `luxd` | zoo-testnet 200201 | `127.0.0.1:19830/` |
| C++ | `zood` | zoo-testnet 200201 | `127.0.0.1:19730/` |
| Rust | `hanzod` | hanzo-testnet 36962 | `127.0.0.1:19780/v1/chain/hanzo` |

The Go zoo node is included on purpose: it and the C++ node serve the *same
chain id*, so a difference between those two columns cannot be explained away by
"different chains".

The message under test is not invented. It is a `BridgeTransfer` — 1000 units
from chain 96368 to chain 200201, at nonce 7 — and its digest was printed by
`luxfi/chains/internal/bridgeattest` itself, then pinned in `main.go` as
`goldenDigest`. The harness recomputes it every run and refuses to start if the
two disagree, so it can never end up comparing runtimes on a message no chain
would accept. `bridgeattest`'s own KAT suite passes (7/7).

## The result

**No runtime diverged on proof verification. One request in ten split three
ways, and it is the one that moves the value.**

### The bridge is not deployed anywhere (S-probes)

The registrar precompile at `0x0400…0046` and the gateway at `0x0400…0040` both
return empty code on all four endpoints, and no endpoint serves a bridge chain
at `/ext/bc/B/rpc`. So there is no live cross-chain transfer path on any of the
three testnets to move value across today. Each refuses the bridge chain in its
own words — Go 404s, C++ says `no such chain`, Rust says `no such endpoint` —
which is a difference in phrasing, not in behaviour.

### Proof verification agrees, exactly (P-probes)

All four endpoints returned byte-identical answers to every proof question: the
valid attestation recovers the attester; a proof over a tampered amount recovers
a different address; a bit-flipped `r` recovers nobody; `v = 29` and an all-zero
signature recover nobody. Zero divergences.

`P4` is worth reading. It presents `(r, n-s)` with the recovery bit flipped —
different bytes, same digest, same key. All three accept it and recover the
attester, and that is **correct**: geth's `ecrecover` calls
`ValidateSignatureValues(v, r, s, false)`, because the low-s rule binds
transaction signatures, not this precompile. The consequence is the finding.
Two distinct 65-byte proofs authorise one transfer, so a bridge keying its
once-only guard on proof bytes double-claims. `bridgevm` does not: `settledKey`
is keyed on `BridgeRequest.ID`, which *is* the transfer digest, so the guard is
immune to the second encoding by construction.

### Moving the value splits the three (R-probes)

`R1` asks each runtime to move the bridged amount to the bridged recipient,
signed for the chain being asked, from an account holding nothing:

```
cpp-zoo     admitted     (0xf27dd624…d4cf14)
go-lux      funds        (insufficient funds for gas * price + value: balance 0)
go-zoo      funds        (insufficient funds for gas * price + value: balance 0)
rust-hanzo  admitted     (0xc0f2533b…905da2)
```

Go refuses. **C++ and Rust admit an unbacked release and answer with a
transaction hash** — which is precisely what a bridge relayer reads as "the
release landed". The C++ node's own `txpool_status` then reports it pending.

Read against the source, the three diverge in a fourth way as well:

- **Go** (`luxd`, geth txpool): balance and nonce are checked at admission. The
  transaction never enters the pool.
- **Rust** (`lux-rs/node`, `Evm::submit`): decodes, checks chain id, recovers
  the signer, dedupes by hash, pushes. No balance check. At build time
  `block::apply` refuses it, records why, and drops it — so it never reaches a
  block.
- **C++** (`lux-cpp/node`, `Chain::accept_tx`): decodes and pushes. No balance
  check, no nonce check, no sender state read at all. And `Chain::build` swaps
  the whole mempool into the block: `execute()` returns only a state root, so
  there is no per-transaction verdict for the builder to act on and nothing
  drops a transaction `process_block` refused.

The build-step half of that is read from source, not measured: none of the three
chains is producing blocks right now, so the admitted transactions sat in the
pools and no receipt was ever written. Balances on both chains were unchanged
afterwards.

`R2` — one chain's signed release offered to every chain — is the cross-chain
replay, and all three refuse it. Go says `invalid sender`, Rust names both ids,
C++ says the transaction "did not decode, was not signed for chain 200201, or
its signature did not recover". C++'s message folds three distinct causes into
one string, which makes a real refusal indistinguishable from a decode failure
to anything reading it; the refusal itself is correct.

## The half that cannot be asked of two of them

Arrival and once-only are not EVM questions. They are decided by the settled
ledger in `luxfi/chains/bridgevm`, and that VM exists in Go only. Grepped across
the whole tree: `lux-cpp/node` contains the string "bridge" zero times, and
`lux-rs/node` contains it only in prose about finality tiers plus one registry
row naming the registrar's address for genesis-marker purposes. Neither has a
lock, a release, an attestation check or a settled ledger.

So the L-probes ask, and record that no runtime can answer. The Go reference
behaviour is established by `bridgevm`'s own suite, which is green:

```
go test ./bridgevm/...     ok  (211 tests)
```

including, by name, the three properties this exercise is about —
`TestAnAcceptedBlockIsDurableAndReleased`,
`TestATransferSettledByAnEarlierBlockCannotBeSettledAgain`,
`TestATransferSettledBeforeARestartCannotBeSettledAgain`,
`TestReleaseSkipsAnAlreadyProcessedTransfer`,
`TestReleaseRefusesAnAttestationSignedByItsOwnSuppliedKey`,
`TestReleaseRefusesAMissingAttestation`, `TestReleaseRefusesAShallowLock`,
`TestAReleaseIsNotBroadcastToTheWrongChain`.

There is no second implementation to compare those answers against. That is the
finding, not a gap in the harness.

## A note on what this run left behind

`R1` puts a transaction into whichever pools admit it, and neither the C++ nor
the Rust node offers a way to take one back out. Both are unbacked and can never
execute — Rust drops it at build, and under C++'s builder it would be sealed
into a block having changed nothing. Nothing else was written: no balance moved,
no block was produced, no chain state changed, and mainnet was not touched.
