# SPDX-License-Identifier: BSD-3-Clause-Eco
# The warp message wire, and the aggregated proof over it.
#
# A signed message is the unsigned bytes and the signature bytes, each a whole
# message of its own, so what a signer signed is carried verbatim rather than
# reconstructed.

package lux.platformvm.warp.wire

type id32 = bytes_fixed[32]
type proof = bytes_fixed[96]

struct Unsigned {
    NetworkID u32   @0
    Source    id32  @4
    Payload   bytes @36
}

struct Signed {
    Unsigned  bytes @0
    Signature bytes @8
}

# One aggregate signature and the bit vector saying who is in it. The vector
# is a big-endian big integer, which is why it is a byte run and not a list.
struct BitSet {
    Kind      u8    @0
    Signature proof @1
    Signers   bytes @97
}
