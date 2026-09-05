# xvm — the Lux X-Chain in C++

The X-Chain is a UTXO DAG: a transaction consumes previous outputs and produces
new ones, and it is valid when its inputs are authorized to consume what they
name and consume at least as much as they produce. This is that chain, in C++,
behind `lux-cpp/node`'s VM seam (`include/lux/node/vm.hpp`).

Ported from the Go reference at `~/work/lux/node/vms/xvm` (13,306 LoC across 43
test files) — the behavioural source of truth. Where the two differ, the Go one
is right and this is a bug.

```
include/lux/xvm/   zap wire  ·  fx envelopes  ·  fx families  ·  txs  ·  address
                   store  ·  state  ·  root  ·  executor  ·  block  ·  genesis
                   mempool  ·  vm
src/               their implementations
test/              fourteen suites, 959 assertions, all green (also under
                   -fsanitize=address,undefined)
```

## The one thing that makes this a port and not a rewrite

**The bytes.** A transaction's id is `sha256` of its wire bytes, so a C++ node
that encodes a field one byte differently gives every transaction a different
name and is simply a different chain. `test/golden.hpp` is therefore not
hand-written: `test/golden/golden_gen.go` builds the same values with the **Go
implementation** and prints their canonical bytes, and `wire_test` /
`genesis_test` assert against them verbatim. Regenerate and diff at any time:

```sh
cp test/golden/golden_gen.go /tmp/gg/main.go
cd ~/work/lux/node && GOWORK=off go run /tmp/gg/main.go > /tmp/golden.hpp
diff /tmp/golden.hpp <this>/test/golden.hpp     # must be empty
```

That generator is a **tool**, not a dependency: it runs against the Go checkout
to produce a header. Nothing in the C++ build imports Go, and the library's
whole dependency closure is `luxcpp/crypto` (sha256, ripemd160, keccak256,
secp256k1) plus the two seam headers — nothing from the Go node, and nothing
from the lineage the Lux consensus spec replaced.

The second cross-language proof is in `fx_test`: Go's fixture carries a fixed
unsigned body `{0,1,2,3,4,5}`, a fixed 65-byte signature, and the address that
signature belongs to. Recovering that exact address here proves this port's
hash, its ECDSA recovery and its address derivation are the same three
functions Go's are — every later "the signature is accepted" rests on it.

That fixture is read back at every RECOVERY ID too, because the last byte of a
signature is consensus and reading it wrong is a fork. The table below is that
one signature handed to luxfi/crypto and to k256, run rather than reasoned
about, and `recovery_id_matches_the_reference` asserts this port lands on all
five rows:

| `v` | Go | Rust | meaning |
|---|---|---|---|
| 0 | `015cce…e7f0` | `015cce…e7f0` | the signer |
| 1 | `aaabc7…8271` | `aaabc7…8271` | the other candidate `y` |
| 2 | recovery failed | recovery failed | `x` wrapped the group order |
| 3 | recovery failed | recovery failed | `x` wrapped, odd `y` |
| ≥4 | invalid recovery id | invalid recovery id | not a recovery id |

2 and 3 name the recovery whose `R` has `x = r + n`. Producing one needs
`r < p - n`, about 2^128 of work, so no signature that exists takes those values
and both references fail them. `secp256k1_ecrecover` NORMALIZES its `v` argument
instead of refusing it, so the rule lives in `recover_address`, which is the only
place that still has the byte. Masking it there — `v & 1`, which this port did —
read 2 as 0 and handed back the signer: one flipped byte in any valid credential
made a transaction this chain accepted and the other two refused.

## Layering

Each layer knows only the ones below it.

| layer | what it answers |
|---|---|
| `zap` | the zero-copy structural codec — the ONLY serialization |
| `wire` | the `(TypeKind, ShapeKind)` envelope that names an fx primitive |
| `fx` | secp256k1fx · nftfx · propertyfx: what authorizes a spend |
| `txs` | the five transactions and the UTXO vocabulary |
| `address` | bech32, so a genesis written by a person can name an owner |
| `store` | the durable byte map, and the one point at which a write lasts |
| `state` | the occupied set, and a diff over it |
| `root` | the execution root a validator signs |
| `executor` | syntax → semantics → apply |
| `block` | position, commitment, transactions |
| `genesis` | the assets the chain starts with |
| `mempool` | what is pending, and the one gate that decides what may enter |
| `vm` | the seam |

Three decisions are worth stating because they are where this port is
deliberately **simpler** than the Go it renders, without answering differently:

- **An fx value says which family it belongs to** (`family()`), rather than
  having its family recovered by inspecting its type. Go dispatches on its
  interface tag and keeps a parallel `FxID` field that a separate pass
  (`tx_init.go`) fills in for its JSON API. This port has no such field: a
  second copy of an identity the value already carries is a field that is empty
  until someone remembers to populate it.
- **The store is the host's to choose, and the chain must not be able to choose
  "none".** `store::Store` is a byte-keyed map with ONE durability point,
  `commit`; `Memory` never outlives the process and `File` does. It is a
  constructor argument rather than something the VM makes for itself, because
  whether this node survives a restart is the host's decision — but genesis and
  every acceptance commit through it, so a chain given a durable store IS
  durable. The `Chain` / `Diff` split, and the ascending enumeration order the
  execution root folds over, are ported exactly.
- **The clock is injected.** A test states the time rather than waiting for it.

## Execution is not optional

`Block::root()` is on the block because a block that cannot say what state it
produced cannot be built: it would ask validators to certify a name rather than
a result. So `verify` **recomputes** the root and refuses a block whose declared
root disagrees — `vm_test`'s *"the execution root"* section forges exactly that
block, valid in every other respect, and requires the refusal. That is what
turns a quorum certificate into agreement about a result.

The root itself (`root.hpp`) is a pure function of canonical leaf field values:
an RFC-6962 tagged Merkle fold over the occupied UTXO set in ascending UTXOID
order, the assets, and the block's transactions, composed with the parent root
and the height under keccak256. `root_test` checks it against the
cross-language KAT in Go's `xvmroot_test.go`, digit for digit.

## Build and test

```sh
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j
ctest --test-dir build            # 14/14, 959 assertions
cmake -S . -B build-san -DXVM_SANITIZE=address,undefined && ...   # also green
```

The reused checkouts are found where they actually sit; override with
`-DLUXCPP_CRYPTO_DIR=`, `-DNODE_DIR=`, `-DCONSENSUS_DIR=`. The seam's absence
is a hard error rather than a quiet feature-off: a chain built against no seam
is a library nothing runs.

## What is here, and what is not

Ported, with the Go tests ported alongside:

- the ZAP codec, byte-identical (`wire_test`, 59)
- all three fx families: outputs, inputs, operations, credentials, and every
  spend and operation gate (`fx_test`, 89 — ported from `secp256k1fx/fx_test.go`,
  `nftfx/fx_test.go`, `propertyfx/fx_test.go`)
- the five transactions, their wire, their round trips (`txs_test`, 84)
- syntactic verification — the complete Go case table, every section
  (`syntactic_test`, 101)
- semantic verification, including import/export across networks
  (`semantic_test`, 48)
- execution: what applying a transaction does to the UTXO set, and the atomic
  requests an import/export records (`executor_test`, 42)
- the store and the diff, and enumeration order (`state_test`, 68)
- durability: what a commit survives, and what an interrupted one does not
  (`store_test`, 109)
- bech32, against Go's own address strings (`address_test`, 42)
- blocks and their identity (`block_test`, 25)
- the execution root against the cross-language KAT (`root_test`, 12)
- genesis, byte-identical to Go's, a VM booted from Go's own buffer, and
  `NewGenesis` from a definition map (`genesis_test`, 33)
- the pool, the admission gate and the bloom filter (`mempool_test`, 62)
- build → verify → accept → **reject**, and the chain coming back after the
  process exits (`vm_test`, 185)

Two of those are worth naming, because each is a place where a chain that
merely answered questions correctly would still be wrong.

**Reject.** A rejected block's transactions were never refused — they lost a
race — so each one is asked again against the state that actually won, and the
ones that still hold go back into the pool. A port that dropped them would hold
a different pending set from every node that returned them and would propose a
different block, which no transaction's bytes could ever reveal. Ported from
`block/executor.Block.Reject`, including the thing it does NOT do: nothing is
marked dropped. A drop reason is a cached refusal, and a transaction invalidated
by a reorganisation is exactly the kind that can become valid again.

**A restart.** Genesis and every acceptance commit to the store, and a boot
reads the store before it installs anything — so the second boot of a chain does
not re-install genesis over its own history, and an output spent before the
restart cannot be spent after it. Which of the two happens is the store's
answer, never a flag the caller passes; Go asks the same question the same way
(`state.IsInitialized`).

**Absent, deliberately** — each is a surface the VM seam does not ask for, and
none is stubbed: the code simply is not there, so nothing can call it and get a
false answer.

- the JSON/`ops` RPC surface (`service.go`, `ops.go`, `static_service.go`,
  `api_types.go`) and the pubsub filterer
- the p2p transport. The gossip SET is here — the pool, the admission gate and
  the bloom filter a peer samples against, with the policy stated in
  `mempool.hpp` — but nothing dials a peer. What the transport adds is who may
  ASK: Go serves pull-gossip requests only to validators and throttles them,
  which is a rule about requests, not about transactions, and belongs with the
  transport.
- the address→transaction indexer, health, metrics
- `batch_verify.go`, a GPU batch-signature optimization whose Go version cannot
  change a verdict (it returns nil on every path)

## Bootstrapping is the exception, not the default

Skipping signature checks while replaying settled history is an OPTIMIZATION, so
the default is the checking one and the switch only ever relaxes it
(`executor::Backend::bootstrapped`, `fx::Fx::bootstrapped_`, both `true`). Go's
field starts `false` and is safe because its engine drives every chain through
`SetState` before it meets a peer. Nothing in the C++ node did, so a chain that
started `false` stayed `false`: it checked no signature for its whole life and
looked exactly like a chain that did.

`Vm::set_bootstrapped` is the one switch, and its signature is the seam's —
`lux/node/vm.hpp` grew `virtual void set_bootstrapped(bool)`, defaulted to
silence, so a host holding nothing but a `lux::node::VM*` can drive it and a
host holding the concrete `Vm` reaches the same method rather than a second one
that could disagree. `vm_test` asserts that signature at compile time: a method
differing by one qualifier would SHADOW the seam's rather than override it, and
a host driving the seam would then reach the do-nothing body while the chain sat
unchanged — the same fail-open by a quieter route.

## The one thing this port cannot do by itself

`VmBlock::reject` exists, is faithful and is tested — but the node's seam
(`lux/node/vm.hpp`) declares `verify` and `accept` and no `reject`, so consensus
in `lux-cpp/node` has no way to call it. The port is ready for it and the body
does not change when the seam grows `virtual void reject() = 0`; until then the
method is reachable only by naming `VmBlock`.

That is a HOST decision, not this chain's: the seam is shared with the C-Chain,
whose reject is the EVM's to write. It is written down here rather than papered
over with a default no-op, because a no-op reject would be a fake that returns
success — the chain would silently go on dropping a rejected block's
transactions and the differential's `X_SEAM_BLOCK_REJECT` vector would report
PRESENT while the divergence stayed.

## Where this chain is checked against the other two

`conformance/corpus/vectors.tsv` is one corpus, generated by the Go X-chain and
handed to implementations that never see each other's answers. The evaluator
that speaks for this chain — `test/conformance.cpp`, built as `xvm_conformance`
— and the runner that compares the answers both belong to the differential
(`conformance/`, the `chains` target at the repository root), not to this
directory: a chain that scored itself would be marking its own paper.

Run against this port, it agrees with Go on thirteen of the fourteen X vectors,
on every compared field — `parse`, `kind`, the id hash, `syntactic` and `exec`.
The fourteenth is not a transaction at all. `X_SEAM_BLOCK_REJECT` asks the
compiler whether a block here can be rejected, and it answers ABSENT while Go
answers PRESENT: the divergence written up above, held open by the only kind of
check that could catch it, and the runner exits non-zero for it.

It asks that of `lux::node::Block`, and the distinction is the whole vector.
Asked of `VmBlock` — which is what it asked before — it answered PRESENT, because
`VmBlock::reject` does exist; but consensus holds a `lux::node::Block&` and can
call only what that interface declares, so it reported agreement about a
capability the C++ node does not have. A probe of the concrete class can only
ever say that this file's author wrote a method. Three runs pin the difference:
the old probe on today's seam AGREES with Go on all 53 vectors, the new one
reports one disagreement, and the new one on a seam carrying
`virtual void reject() = 0` agrees again — green only once consensus can
genuinely reach the reject.

One gap is worth stating because agreement can hide it: on five of those vectors
the `exec` field is compared by nobody. Go, Rust and C++ all answer `SKIPPED`
("needs a funded UTXO set"), and the runner counts a skip as no answer at all —
correctly, and it prints them under NOT COMPARED. X-chain execution is therefore
covered by this port's own ported case tables (`executor_test`, `semantic_test`)
and not yet by the cross-language differential. Closing it means the corpus
carrying a funded ledger for the X vectors, which is a change to the corpus
shared by all three evaluators rather than something this chain can do alone.

## Traps

- **`add_bytes` counts bytes, not elements.** Go's `ListBuilder.AddBytes` adds
  `len(data)` to the count, so every fixed-stride list writer passes the element
  count to `set_list` itself. Getting this wrong produces a list that parses
  with the wrong length and no error.
- **`SetBytes` appends immediately.** `start_object` reserves the whole fixed
  section up front so a variable field can write its tail on the spot. Building
  a child object *between* `start_object` and a `set_bytes` would interleave
  bytes into the parent's tail. Build children first — that is the
  `write_*_list`-then-`start_object` order every writer here uses.
- **Object pointers are signed, byte pointers are not.** A nested object may
  sit earlier in the buffer than the pointer to it; a byte run may not. Both
  reject any target inside the 16-byte header.
- **A mint operation must re-create the mint authority it consumed.** Otherwise
  minting would let a holder rewrite who may mint next. The check fires *before*
  the signature is looked at, which is why a test that wants a signature failure
  has to keep the authorities equal.
- **The fee asset is the first asset in genesis, by position.** A field naming
  it would be a second answer that could disagree with the first.
