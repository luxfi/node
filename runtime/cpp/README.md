# runtime/cpp

A thin shim. No C++ source lives here — `make luxd RUNTIME=cpp` builds
`~/work/lux-cpp/node` in its own existing `build/` directory and copies the
`luxd` target to `bin/luxd-cpp`.

## What it is

A complete, running Lux node: it executes real EVM blocks through `cevm`,
decides them with BLS quorum-certificate consensus over a mesh of real TCP
sockets framed by ZAP, and serves `/v1/chain/C/rpc`. Every validator executes
every block itself and signs its own result — a certificate is agreement
about an executed root, not a name (see that repo's `LLM.md` for the register-
before-publish and mesh-is-not-the-quorum details this shim does not restate).
That CMake project also defines other orgs' brand-specific executables off the
same `src/noded.cpp` — out of scope here. `node2` is Lux-branded only; it
builds `luxd` and nothing else from this source.

## What it reuses, unmodified

`~/work/lux-cpp/node`'s own `CMakeLists.txt` finds these as siblings and adds
them as subdirectories — this shim does not resolve them a second time:

- `~/work/lux-cpp/consensus` — the C++ consensus engine (the gate + `Node`).
- `~/work/luxcpp/cevm` — the C++/GPU EVM that executes C-Chain blocks.
- `~/work/luxcpp` — `blst`, `bls_signature`, `zap-cpp-core` (the wire codec).

## Build

Requires a Conan toolchain for `cevm`'s dependencies (`intx`, `nlohmann_json`,
…) — already generated on this machine at
`~/work/luxcpp/cevm/build-node/build/Release/generators/conan_toolchain.cmake`.
The node repo's own `build/` directory is already configured against it:

```sh
cmake --build ~/work/lux-cpp/node/build --target luxd -j"$(nproc)"
```

A from-scratch configure (`cmake -S ~/work/lux-cpp/node -B <dir>
-DCMAKE_TOOLCHAIN_FILE=…`) works too and resolves the same three siblings by
its own sibling-search default — no `-DCONSENSUS_DIR` override needed, and
none is passed here. This shim reuses the repo's existing `build/` rather than
configuring a parallel one, for speed; either way nothing is written back into
the source tree except that conventional, already-gitignored directory.

## Reading a chain back: two doors, one reader

The C++ node takes an RLP block export the two ways Go's does, and both are the
same function — `import_chain_data` in that repo's `src/import.cpp`:

```sh
# at startup
bin/luxd-cpp --index 0 --n 4 --base-port 41850 --rpc-port 41898 \
             --chain-id 96368 --import-chain-data …/lux-testnet-96368.rlp

# and while it runs, on the same node
curl -s -X POST -H 'content-type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"admin_importChain",
           "params":["…/lux-testnet-96368.rlp"]}' \
  http://127.0.0.1:41898/v1/chain/C/rpc
```

Go's two doors call `importBlocksFromFile` (`plugin/evm/admin_api.go:84` for
the RPC, `plugin/evm/vm.go:628` for the flag); the C++ column's call
`import_chain_data`. That is the whole reason to write it down: a flag that
grows a reader and an RPC that grows another leave one binary holding two
answers to what a block is, which is a fork surface with no network in it.

Both doors reach Go's own result on the canonical bytes —
`lux-testnet-96368.rlp` reads to tip `0x722e2b39ae…81a5`, state root
`0x4e19366fcc…7f35`, height 218 — and the run above shows the second door
answering `{"blocks":0,"skipped":218}` at that tip, because a re-read resumes
from whatever head the first door left.

**An imported node is not a caught-up validator, through either door.** Reading
an export moves the tip and certifies nothing under it, so the C++ node's
frontier stays where its own decisions stopped and the engine will not build or
follow on it — Go refuses for the same reason at `vms/proposervm/vm.go:930`.
See that repo's `LLM.md`; `import_test` asserts the refusal after an import
through each door, with an un-imported chain as the control.
