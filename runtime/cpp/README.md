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
