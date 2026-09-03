# gpu

A thin shim. No source lives here — `make gpu` builds `lux-gpu/gpu`'s existing
`build/` directory in place; `make all` builds it too (GPU is on by default,
not opt-in).

## Path correction

The brief names `~/work/lux-gpu/gpu`; that path does not exist on this
machine. The actual checkout of that repository (verified by `git remote -v`
→ `git@github.com:lux-gpu/gpu.git`) lives at `~/work/luxcpp/gpu`. This shim
points there. (A second, narrower repo, `lux-gpu/lux-gpu` at
`~/work/luxcpp/lux-gpu`, builds only a static core library linked into
`lux-accel` — not this one, and not what the brief describes as "kernels".)

## What it is

The core GPU acceleration library: a stable plugin ABI
(`include/lux/gpu/backend_plugin.h`), a dynamic loader, and a CPU fallback
backend built in. Backend plugins (Metal, CUDA, WebGPU) are separate repos
loaded at runtime — this repo alone gives a CPU-correct build with the loader
compiled in, which is what `make gpu` builds. It already carries real kernel
libraries beyond the loader — `libluxgpu_bls12_381_host.a`,
`libluxgpu_dilithium_host.a` — the post-quantum host-side primitives the
`lux-pq` crate (`runtime/rust`) and `libluxcrypto` (`runtime/go`, `runtime/cpp`)
both verify under.

## Build

```sh
cmake -B ~/work/luxcpp/gpu/build ~/work/luxcpp/gpu
cmake --build ~/work/luxcpp/gpu/build -j"$(nproc)"
```

Already configured on this machine — the above is an incremental rebuild, not
a from-scratch one.
