# SPDX-License-Identifier: BSD-3-Clause-Eco
# The Q-chain wire.
#
# The offsets are the format. They are stated here once and every language's
# accessors come out of this file, so a Q block built in Rust is the bytes a
# Go peer builds.
#
# A transaction is two shapes, because it is two things. Body is the
# SIGNATURE PREIMAGE — what the ML-DSA signature covers, and therefore what
# can never carry the signature. Tx is what rides in a block: the preimage's
# bytes and the stamp over them.

package qchain

type id32 = bytes_fixed[32]

struct Block {
    Time      i64        @0
    Height    u64        @8
    Parent    id32       @16
    Chain     id32       @48
    Network   u32        @80
    TxLengths list<u32>  @88
    TxBlob    bytes      @96
}

struct Body {
    Time  i64   @0
    Nonce u64   @8
    Data  bytes @16
}

struct Tx {
    Body      bytes @0
    Algorithm u32   @8
    Stamped   i64   @16
    PublicKey bytes @24
    Signature bytes @32
    Stamp     bytes @40
}
