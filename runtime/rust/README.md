# runtime/rust

A shim. No Rust source lives here — `make luxd RUNTIME=rust` builds
`~/work/lux-rs/node` in place and copies its `luxd` binary to `bin/rust/luxd`.

## What it builds

A running Lux node: a validator mesh over TCP framed by ZAP, BLS
quorum-certificate finality through `lux-consensus`, and `revm` executing the
C-Chain. Every validator executes each block itself and signs the root its own
execution produced, so a certificate is agreement about a result rather than
about a name.

That crate also builds other networks' binaries from the same source. This
repository builds `luxd` and names nothing else.

## The build

    make luxd RUNTIME=rust

It needs `libluxcrypto.a`, which `LUX_LIB_DIR` points at; the Makefile passes
`~/work/lux/crypto/dist`. Build it with:

    cd ~/work/lux/crypto/bindings/cabi && \
      CGO_ENABLED=1 go build -buildmode=c-archive -o ~/work/lux/crypto/dist/libluxcrypto.a .

The exit code of every step is checked. Nothing is written back into the
checkout except its own `target/`.
