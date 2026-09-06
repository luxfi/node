# chains/cpp — the chain suite, in C++

Ported from `~/work/lux/node/vms/*` into `~/work/lux-cpp/node`'s VM seam
(`include/lux/node/vm.hpp`). `runtime/cpp` already runs a C-Chain today via
`cevm` (see its README); this is the rest of the suite, behind the same seam.

| chain | here | state |
| --- | --- | --- |
| P — the platform chain | `platformvm/` | ported; see its `LLM.md` |
| X — the UTXO DAG | `xvm/` | ported; see its `LLM.md` |
| Q — the quantum chain | `quantumvm/` | ported; see its `LLM.md` |
| Z — the shielded chain | `zkvm/` | ported; see its `LLM.md` |
| D — the DEX admission layer | `dexvm/` | ported; see its `LLM.md` |
| F — confidential compute | `fhevm/` | ported; see its `LLM.md` |

Every one of them answers the shared corpus. `test/conformance.cpp` in each
chain reads `conformance/corpus/vectors.tsv` and prints that chain's verdicts;
`make chains` compares them against Go's and against Rust's. They are built
whether or not tests are, because a differential that quietly lost one of its
voices would report agreement among whoever was left.

`conformance/` holds what those six evaluators share — the corpus format, the
verdict words and the error-word table. It lives once because a table that
differed between two of them reported its own difference as a chain's.

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
