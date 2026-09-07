# SPDX-License-Identifier: BSD-3-Clause-Eco
# The F-chain wire.
#
# The offsets are the format. They are stated here once and every language's
# accessors come out of this file, so a transaction built in Rust is the bytes
# a Go peer builds.
#
# A transaction is TWO objects, one after the other, because it is two things.
# The first is the signing preimage — what the payer's signature covers, and
# therefore what can never carry it. The second is the authorization over
# those bytes. Both are self-delimiting, so the split is found by reading the
# first one's length rather than by re-encoding anything.

package fchain

type id32 = bytes_fixed[32]
type addr = bytes_fixed[20]

# The semantic fields, and nothing about who authorized them.
struct Tx {
    Type    u8    @0
    Payer   addr  @1
    Subject id32  @21
    Gas     u64   @53
    Nonce   u64   @61
    Scheme  bytes @69
    Payload bytes @77
}

# What rides behind the preimage: the authorization and the signature over it.
struct Auth {
    Auth      bytes @0
    Signature bytes @8
}

struct Block {
    Parent id32      @0
    Height u64       @32
    Time   i64       @40
    TxLens list<u32> @48
    TxBlob bytes     @56
}
