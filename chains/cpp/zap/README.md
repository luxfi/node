# zap — the codec, once

ZAP decides what the bytes MEAN. Two chains in one binary carrying two codecs
are two answers to "what is this frame", which is a fork surface inside a single
process — so there is one codec here and every C++ chain in this tree includes
it.

```
include/lux/zap.hpp   the runtime: reader, builder, and the arithmetic
test/zap_test.cpp     23 cases over that runtime, including the hostile ones
```

## It is generated, not written

The wire is DEFINED in one place — the ZAP runtime the Go chains link,
`github.com/luxfi/zap` — and rendered per language by `zip`. The header carries
its own regeneration line:

```
zipgen zap -lang cpp -ns lux::zap -o chains/cpp/zap/include/lux/zap.hpp
```

`zipgen` is `github.com/zap-proto/zip/cmd/zipgen`. Do not edit the header: an
edit here is a fourth definition of the wire, which is the thing this directory
exists to prevent. Change the emitter (`zip/cpp_zap.go`) and regenerate, and the
Rust and Go columns get the same change from the same source.

The emitter's promise — that C++ writes the bytes Go writes — is a test result,
not a claim: `zip`'s `TestCppZapKeepsTheWire` compiles this header and diffs its
buffers against the Go runtime's over seventeen shapes, and
`TestCppZapReadsWhatGoWrote` closes the loop the other way.

## Including it

A chain adds `../zap/include` to its include path and says
`#include "lux/zap.hpp"`. No alias is needed: from inside `lux::xvm` or
`lux::platformvm`, unqualified `zap::` finds `lux::zap` by ordinary enclosing-
namespace lookup. That is the whole coupling.

## The shape of the wire

```
header 16B: Magic "ZAP\0" | Version u16 | Flags u16 | RootOffset u32 | Size u32
body:       8-byte-aligned objects, lists and byte runs
```

Every pointer is RELATIVE to the position of the pointer field itself, and every
multi-byte integer is little-endian. A bytes pointer is UNSIGNED (forward only);
an object or list pointer is SIGNED, because an honest builder that finalized a
child first points backwards. Neither may target the header.

Reads are total: a read that leaves the buffer answers zero or an empty span,
never a fault. Bounds live in this header and not in every caller. Callers still
validate MEANING.
