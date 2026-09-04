# xvm — the Lux X-Chain in C++

The X-Chain is a UTXO DAG: a transaction consumes previous outputs and produces
new ones, and it is valid when its inputs are authorized to consume what they
name and consume at least as much as they produce. This is that chain, in C++,
behind `lux-cpp/node`'s VM seam (`include/lux/node/vm.hpp`).

Ported from the Go reference at `~/work/lux/node/vms/xvm` (13,306 LoC across 43
test files) — the behavioural source of truth. Where the two differ, the Go one
is right and this is a bug.

```
include/lux/xvm/   zap wire  ·  fx envelopes  ·  fx families  ·  txs  ·  state
                   root  ·  executor  ·  block  ·  genesis  ·  vm
src/               their implementations
test/              eleven suites, 677 assertions, all green
```

## The one thing that makes this a port and not a rewrite

**The bytes.** A transaction's id is `sha256` of its wire bytes, so a C++ node
that encodes a field one byte differently gives every transaction a different
name and is simply a different chain. `test/golden.hpp` is therefore not
hand-written: `test/golden_gen.go` builds the same values with the **Go
implementation** and prints their canonical bytes, and `wire_test` /
`genesis_test` assert against them verbatim. Regenerate and diff at any time:

```sh
cp test/golden_gen.go /tmp/gg/main.go
cd ~/work/lux/node && GOWORK=off go run /tmp/gg/main.go > /tmp/golden.hpp
diff /tmp/golden.hpp <this>/test/golden.hpp     # must be empty
```

That generator is a **tool**, not a dependency: it runs against the Go checkout
to produce a header. Nothing in the C++ build imports Go, and the library's
whole dependency closure is `luxcpp/crypto` (sha256, ripemd160, keccak256,
secp256k1) plus the two seam headers. Zero `luxfi/node`, zero lux-private.

The second cross-language proof is in `fx_test`: Go's fixture carries a fixed
unsigned body `{0,1,2,3,4,5}`, a fixed 65-byte signature, and the address that
signature belongs to. Recovering that exact address here proves this port's
hash, its ECDSA recovery and its address derivation are the same three
functions Go's are — every later "the signature is accepted" rests on it.

## Layering

Each layer knows only the ones below it.

| layer | what it answers |
|---|---|
| `zap` | the zero-copy structural codec — the ONLY serialization |
| `wire` | the `(TypeKind, ShapeKind)` envelope that names an fx primitive |
| `fx` | secp256k1fx · nftfx · propertyfx: what authorizes a spend |
| `txs` | the five transactions and the UTXO vocabulary |
| `state` | the occupied set, and a diff over it |
| `root` | the execution root a validator signs |
| `executor` | syntax → semantics → apply |
| `block` | position, commitment, transactions |
| `genesis` | the assets the chain starts with |
| `vm` | the seam |

Three decisions are worth stating because they are where this port is
deliberately **simpler** than the Go it renders, without answering differently:

- **An fx value says which family it belongs to** (`family()`), rather than
  having its family recovered by inspecting its type. Go dispatches on its
  interface tag and keeps a parallel `FxID` field that a separate pass
  (`tx_init.go`) fills in for its JSON API. This port has no such field: a
  second copy of an identity the value already carries is a field that is empty
  until someone remembers to populate it.
- **State is in memory.** Durability belongs to the host that embeds this VM;
  inventing a second database inside the VM would be a second answer to a
  question the host already answers. The `Chain` / `Diff` split — and the
  ascending enumeration order the execution root folds over — is ported exactly.
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
ctest --test-dir build            # 11/11
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
  spend and operation gate (`fx_test`, 79 — ported from `secp256k1fx/fx_test.go`,
  `nftfx/fx_test.go`, `propertyfx/fx_test.go`)
- the five transactions, their wire, their round trips (`txs_test`, 84)
- syntactic verification — the complete Go case table, every section
  (`syntactic_test`, 101)
- semantic verification, including import/export across networks
  (`semantic_test`, 48)
- execution: what applying a transaction does to the UTXO set, and the atomic
  requests an import/export records (`executor_test`, 42)
- the store and the diff, and enumeration order (`state_test`, 68)
- blocks and their identity (`block_test`, 25)
- the execution root against the cross-language KAT (`root_test`, 12)
- genesis, byte-identical to Go's, and a VM booted from Go's own buffer
  (`genesis_test`, 33)
- build → verify → accept through the seam (`vm_test`, 126)

**Absent, deliberately** — each is a surface the VM seam does not ask for, and
none is stubbed: the code simply is not there, so nothing can call it and get a
false answer.

- the JSON/`ops` RPC surface (`service.go`, `ops.go`, `static_service.go`,
  `api_types.go`) and the pubsub filterer
- the p2p gossip network and its mempool (`network/`, `txs/mempool`). The VM
  admits transactions through the same two verifiers a block does, against the
  preferred state; what is missing is gossip, eviction and the auth policy.
- the address→transaction indexer, health, metrics
- `NewGenesis` from a definition map — that path parses bech32 address strings,
  which is the address module's job, not the chain's. The genesis **wire** (the
  half a node is handed) is ported and byte-checked.
- `batch_verify.go`, a GPU batch-signature optimization whose Go version cannot
  change a verdict (it returns nil on every path)
- `Block::Reject` — the seam has no reject method. Go returns a rejected
  block's transactions to the mempool; with no gossip mempool here there is
  nowhere to return them to.

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
