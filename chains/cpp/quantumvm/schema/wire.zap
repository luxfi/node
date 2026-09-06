# SPDX-License-Identifier: BSD-3-Clause-Eco
# The Q-chain's wire, as a schema.
#
# These offsets are the ones the Go reference states in
# chains/quantumvm/wire.go. Written down once here, the readers and the
# builders come out of zapgen, and a block written in either language hashes
# to the same id because both are the same arithmetic over the same layout.
#
# The transaction set is a length vector beside a blob, not a list of
# transactions: a transaction is variable-width, and a length the blob cannot
# back is a refusal rather than a truncation. Both halves come from the peer
# and both are checked, in wire.cpp, where meaning lives.

package lux.quantumvm.wire

type id32 = bytes_fixed[32]

# What a signature covers, and what a transaction id is taken over. It
# excludes the signature, which is the whole point of it being its own byte
# string.
struct TxBody {
    Timestamp i64   @0
    Nonce     u64   @8
    Data      bytes @16
}

# The preimage plus the signature over it. CoronaKey is not here: signing sets
# it from the public key, so a second copy on the wire is a second thing to
# disagree.
struct TxEnvelope {
    Body      bytes @0
    Algorithm u32   @8
    Stamped   i64   @16
    PublicKey bytes @24
    Signature bytes @32
    Stamp     bytes @40
}

struct Block {
    Timestamp i64       @0
    Height    u64       @8
    ParentID  id32      @16
    ChainID   id32      @48
    NetworkID u32       @80
    TxLens    list<u32> @88
    TxBlob    bytes     @96
}
