# gpu

A shim. No source lives here — the kernels are in `luxfi/compute`, which is
private, and are built from a checkout at `~/work/lux/compute`:

    gpu/rust    the Rust chains take it as `lux-gpu`
    gpu/cpp     the C++ chains take it as `LUX_GPU_DIR`, which the Makefile sets

Same arrangement as `runtime/rust` and `runtime/cpp`: this repository is
public because it forks public work and because anyone building on this network
has to read and run it. The matcher, the FHE implementation and the kernels are
ours, so they are named here and kept there. Nothing in this directory is a
partial copy of what it points at.
