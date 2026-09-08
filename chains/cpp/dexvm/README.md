# chains/cpp/dexvm

A shim. No source lives here — the D-Chain matcher in C++ is in `luxfi/compute`, which is private,
and is built from a checkout at `~/work/lux/compute`.

Same arrangement as `runtime/rust` and `runtime/cpp`: this repository is
public because it forks public work and because anyone building on this network
has to read and run it. The matcher, the FHE implementation and the kernels are
ours, so they are named here and kept there. Nothing in this directory is a
partial copy of what it points at.
