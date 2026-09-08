# runtime/cpp

A shim. No C++ source lives here — `make luxd RUNTIME=cpp` configures
`~/work/lux-cpp/node` into its own `build/` and copies the `luxd` target to
`bin/cpp/luxd`.

## What it builds

A running Lux node: `cevm` executing C-Chain blocks, BLS quorum-certificate
consensus over a TCP mesh framed by ZAP, and JSON-RPC at `/v1/chain/C/rpc`.
Every validator executes each block itself and signs the root its own
execution produced, so a certificate is agreement about a result rather than
about a name.

That project also builds other networks' executables from the same source.
This repository builds `luxd` and names nothing else.

## What it resolves

The node's own `CMakeLists.txt` finds these as siblings and adds them; this
shim does not resolve them a second time.

- `~/work/lux-cpp/consensus` — the consensus engine
- `~/work/luxcpp/cevm` — the EVM that executes C-Chain blocks
- `~/work/luxcpp` — `blst`, `bls_signature`, and the ZAP codec

## The build

    make luxd RUNTIME=cpp

It needs a Conan toolchain for `cevm`'s dependencies. `CONAN_TOOLCHAIN` names
it and the Makefile prints the `conan install` line to write one if it is
missing. Nothing is written back into the checkout except its own gitignored
`build/`.

## Reading a chain back

An RLP export enters two ways — at startup and while the node runs — and both
are the same function, `import_chain_data` in that repo's `src/import.cpp`.

    bin/cpp/luxd --chain-id 96368 --import-chain-data <file>.rlp

    curl -s -X POST -H 'content-type: application/json' \
      --data '{"jsonrpc":"2.0","id":1,"method":"admin_importChain","params":["<file>.rlp"]}' \
      http://127.0.0.1:<rpc>/v1/chain/C/rpc

After an import the node does not act as a caught-up validator: the outer
index is missing, so it refuses to build blocks until that is rebuilt from
certified peer state. An import writes inner blocks directly and creates no
outer envelope, so every consensus frontier below the imported tip is absent,
and a node that proposed on top of it would be signing history it never
verified.
