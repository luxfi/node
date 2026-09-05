# gpu — the one place a Lux chain asks for a primitive

A chain needs a hash and a signature check. This directory is where it asks for
one, in all three languages, and where the choice between computing the answer
and handing the question to an installed kernel library is made — once, in one
place, with one rule.

## The boundary

**Kernels are private and no kernel source is here.** They live in their own
repository (`lux-gpu/gpu`, checked out on this machine at `~/work/luxcpp/gpu`).
What crosses the line into this public tree is a C ABI: four symbol names.

```
lux_gpu_create()             -> LuxGPU*
lux_gpu_destroy(LuxGPU*)
lux_gpu_backend_name(LuxGPU*) -> const char*
lux_gpu_keccak256_batch(LuxGPU*, const uint8_t* inputs, uint8_t* out,
                        const size_t* input_lens, size_t n) -> LuxError
```

Nothing is linked at build time. The library is `dlopen`'d, so a build machine
that has never heard of it produces the same binary as one that has, and a
binary that never finds it is a whole node.

## The rule

**The CPU backend is complete.** Every primitive has a CPU body, always
compiled, and it is the DEFINITION: the plugin does not get to have its own
opinion. Two backends that disagree about a hash disagree about a block, so a
plugin answer is either byte-identical to the CPU answer or a bug.

The plugin is a **strict positive overlay**. It may only ever be faster. When it
is absent, when it declines a batch, or when it errors, the CPU answers — a
device's bad day never propagates into consensus.

## The knob

One environment variable, read once, at first use.

| `LUX_GPU` | |
|---|---|
| `off` | CPU only. Nothing is opened. |
| `on` | Plugin for batch work when a library is installed. **Default**, and it degrades to `off` on its own when nothing is installed. |
| `verify` | Compute **both** and stop on the first byte that differs. |

`LUX_GPU_LIB` names the library path when it is not on the loader's path.

`LUX_GPU=verify` is how "we believe they agree" becomes "we checked". It is what
`make gpu-differential` sets.

## What actually has two paths

| operation | CPU | plugin |
|---|---|---|
| `keccak256_batch` | yes, the definition | yes — `lux_gpu_keccak256_batch` |
| `merkle_root` (RFC 6962 fold) | yes | yes, through the batch: a level of the tree is a batch of independent hashes, which is the only shape a device can help with |
| `keccak256` (one input) | yes | **no, deliberately.** One hash is not a batch; a dispatch costs more than the answer |
| `sha256` | yes | **none exists.** The plugin ABI's hash surface is keccak256, SHA3-256, SHAKE256, BLAKE3 and Poseidon2. SHA3-256 is not SHA-256 |
| `ripemd160` | yes | **none exists** |
| `recover` (secp256k1) | yes | **present but not applicable.** `lux_gpu_ecrecover_batch` answers a different question: it returns an Ethereum address, `keccak(Q.x‖Q.y)[12:]`. A Lux address is `ripemd160(sha256(compressed Q))`, and the recovered key never leaves that call, so its answer cannot be turned into this one's |
| BLS proof of possession | yes (blst / `blst` crate) | **none exists.** The ABI has a raw `op_bls12_381_pairing`, not a verify that knows the `BLS_POP_…` domain tag; composing one here would mean writing hash-to-curve twice |

No operation diverges between the CPU and the plugin — "What was measured" below
says exactly what that claim rests on, and what it does not. A cross-LANGUAGE
divergence is a different thing, and consolidating the primitives turned one up;
it is the last section.

## The three homes

```
gpu/gpu.go, plugin_cgo.go, plugin_nocgo.go   Go     package github.com/luxfi/node2/gpu
gpu/rust/                                    Rust   crate lux-gpu
gpu/cpp/                                     C++    CMake target lux_gpu
```

Each is the same shape: a `cpu` half that is always there, a plugin half behind
`dlopen`, one `LUX_GPU` policy, and one `Backend()` that names which is
answering. In Go the plugin half is `//go:build cgo` — a `CGO_ENABLED=0` build
has no way to open a shared library, so it never has one. That is not a missing
feature; it is the build `make luxd RUNTIME=go` produces, and it is complete.

The C++ target also compiles the reused `luxcpp/crypto` bodies its CPU half
delegates to. Those used to be compiled twice, once into `xvm` and once into
`platformvm`, and the P-chain carried a third SHA-256 of its own in a header —
three builds of one hash in one tree, and three places for the chains to drift.

## Who goes through it

Every chain, for every primitive it is defined over.

```
chains/rust/xvm/src/hash.rs              names the seam's primitives, defines none
chains/rust/xvm/src/fx/secp256k1.rs      recover_public_key
chains/rust/platformvm/src/ids.rs        hash256
chains/rust/platformvm/src/sign.rs       address, recover
chains/cpp/xvm/src/id.cpp                sha256, pubkey_to_address
chains/cpp/xvm/src/root.cpp              keccak, the tags, the fold
chains/cpp/xvm/src/fx.cpp                recover_address
chains/cpp/platformvm/include/.../sha256.hpp   sha256
chains/cpp/platformvm/src/fx.cpp         address_of_compressed_key, recover_address
```

Signing is deliberately outside: a chain verifies signatures and never makes
them, so `k256`'s `SigningKey` stays in the chains' test and tooling halves. The
seam exists to make VERIFICATION agree across backends.

## What was measured

Run `make gpu-differential`. It builds all three seams and runs each one's tests
twice — once with no library visible, once with `LUX_GPU=verify` against an
installed one — and every input is asked of both backends and compared byte for
byte.

On the machine this was written on:

- **`plugin:cpu`, not `plugin:cuda`.** `lux_backend_available` reports 1 for the
  plugin's own SIMD CPU backend and 0 for Metal, CUDA and Dawn. The only device
  backend built here, `libluxgpu_backend_webgpu.so`, does not load: it needs
  `libwgpu_native.so`, which is not on this machine. So the differential proves
  the **dispatch and the ABI** agree with the CPU definition, on every batch
  shape tried; it does **not** exercise a device kernel, because there is no
  device backend installed to exercise. `Backend()` reports the plugin's own
  name for exactly this reason — a host that says `plugin:cpu` is telling you it
  found the library and no device.
- Every seam's test carries a **negative control** on the `verify` comparison
  itself: it is handed a deliberately flipped byte and must refuse. A comparison
  nothing has ever seen fail is a comparison nobody has checked can fail.

## A one-byte fork, closed by consolidating

Recovery ids 2 and 3. Go accepts all four (`luxfi/crypto/secp256k1`
`checkSignature` refuses only `v >= 4`) and recovers correctly for each; Rust's
`k256` does the same. Ids 2 and 3 mean `R.x = r + n`, and the first-party C++
curve refuses `r >= n` at parse time, so it has no path to them.

Before this seam the two C++ chains disagreed about that, with each other and
with Go:

- `chains/cpp/xvm` **accepted** them, by masking `v & 1`. That turns `v = 2` into
  `v = 0` and recovers the key of a *different* id — so **take any valid
  signature and move its recovery byte from 0 to 2**, and a C++ node accepted a
  transaction Go and Rust both refuse. A fork anyone could build in one byte.
- `chains/cpp/platformvm` **refused** them, which matched Go for every such
  input.

Both refuse now, and that closes the exploitable direction. Measured on the KAT
in `gpu/cpp/test/gpu_test.cpp` and `gpu/rust/tests/differential.rs` — the same
signature asked of both seams:

| `v` | Go / Rust | C++ before | C++ now |
|---|---|---|---|
| 0 | the signer's key | the signer's key | the signer's key |
| 1 | the other candidate | the other candidate | the other candidate |
| 2, 3 | no key (`r + n` is not on the curve) | **the id-0/1 key** | no key |
| ≥ 4 | no key | no key | no key |

The residue is one case: a deliberately built `r` where `r + n` *is* a valid x
coordinate. There Go recovers a key and C++ still refuses — C++ refusing what Go
accepts, never the reverse. It belongs to whoever owns the P/X differential, and
closing it properly means teaching the C++ curve the `r + n` case.

## Building the kernel library

`make gpu` builds `lux-gpu/gpu`'s `luxgpu_core_static` target in place. It is
not needed to build or run a node; it is needed to have something for
`LUX_GPU_LIB` to point at.
