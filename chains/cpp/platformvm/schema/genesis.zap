# SPDX-License-Identifier: BSD-3-Clause-Eco
# The genesis blob's wire.
#
# Three lists of things that are already self-describing — allocations,
# validator transactions, chain transactions — so the container stores a u32
# LENGTH per element beside one concatenated blob rather than re-encoding
# anything. That is the same framing a block uses for its transactions, for
# the same reason.

package lux.platformvm.genesis.wire

# One allocation: the UTXO's own envelope, and the message it carried.
struct Allocation {
    Utxo    bytes @0
    Message bytes @8
}

struct Genesis {
    Timestamp        u64       @0
    InitialSupply    u64       @8
    Message          text      @16
    UtxoLengths      list<u32> @24
    UtxoBlob         bytes     @32
    ValidatorLengths list<u32> @40
    ValidatorBlob    bytes     @48
    ChainLengths     list<u32> @56
    ChainBlob        bytes     @64
}
