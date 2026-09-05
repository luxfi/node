# chains/cpp — the chain suite, in C++

Ported from `~/work/lux/node/vms/*` into `~/work/lux-cpp/node`'s VM seam
(`include/lux/node/vm.hpp`). `runtime/cpp` already runs a C-Chain today via
`cevm` (see its README); this is the rest of the suite, behind the same seam.

| chain | here | state |
| --- | --- | --- |
| P — the platform chain | `platformvm/` | ported; see its `LLM.md` |
| X — the UTXO DAG | `xvm/` | ported; see its `LLM.md` |
| Q, Z | — | not started |

## core — what the chains share, and share ONE of

```
core/include/lux/core/
  id.hpp       the fixed-width names, and the hashes that derive them
  zap.hpp      the zero-copy structural codec
  store.hpp    the durable byte-keyed map a chain's state rests on
  mempool.hpp  the pool of what is not yet in a block
  check.hpp    the test harness
```

Nothing in `core` knows what a chain is, and that is the test of whether
something belongs there: a P-chain concept that leaked in would make the
X-Chain link the P-chain's opinions.

Two rules follow from what it holds.

**`Id` IS `lux::consensus::Id`** — `std::array<uint8_t,32>`, the type the VM
seam speaks. A block id crosses into consensus with no conversion, and a
conversion that does not exist cannot be got wrong. `NodeId` is the one name
that is strengthened into a type of its own, because it shares its width with an
address and the compiler should refuse that swap.

**The store is the ONE durability point.** A commit is durable when `commit`
returns — it fsyncs; a process killed mid-append leaves a partial record that
replay detects and truncates, so a half-written commit never happened. Both
chains rest on it, and both prove it the only way that counts: a second process
writes, commits and is destroyed by SIGKILL, and a third must find the same
state root (P) and the same spent output still spent (X).

```
cd core && cmake -S . -B build && cmake --build build -j && (cd build && ctest)
```

## platformvm

The validator set, the staking rules that admit and pay it, and the blocks that
change it. A node joins by staking: no allowlist, no admin key, no argument
anywhere in the tree to pass one.

```
cd platformvm && cmake -S . -B build && cmake --build build -j && (cd build && ctest)
```

It finds `luxcpp/blst`, `luxcpp/crypto`, `lux-cpp/node` and `lux-cpp/consensus`
where they actually sit, matching on a file rather than a directory name. All
four are required: a build that cannot check a proof of possession or recover a
signer does not produce a P-chain at all.

`platformvm/LLM.md` says what is ported, what is absent, and why — including the
one subsystem (L1 validators, which rest on warp) whose transaction wire is
byte-identical but whose execution is not here.

## xvm

The UTXO DAG: the five transactions, the three fx families, the execution root a
validator signs, and the mempool gate every transaction enters through.

```
cd xvm && cmake -S . -B build && cmake --build build -j && (cd build && ctest)
```

## What is still written twice

`platformvm/include/lux/platformvm/zap.hpp` is a second implementation of the
ZAP format, serving that chain's transaction wire. It writes the same bytes as
`core/zap.hpp` — the differential proves it, field for field, over 208 vectors —
but it is a second implementation of the one thing a node can least afford two
of, and collapsing it means porting ~155 call sites in seven files onto the
other API. It is the next cut, and it is the sharpest one left.

`genesis` is NOT that: the X-Chain's genesis is a list of (alias,
CreateAssetTx) and the P-chain's is a timestamp, a supply, allocations,
validators and chains. They share a word, not a concept, and folding them
together would invent an abstraction neither chain has.
