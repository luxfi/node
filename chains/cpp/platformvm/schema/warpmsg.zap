# SPDX-License-Identifier: BSD-3-Clause-Eco
# What travels inside a warp message: the payload envelope, and the four
# things an L1 and the P-chain say to each other.
#
# Every one of them opens with a kind byte, which says which table to read.

package lux.platformvm.warpmsg.wire

type id32 = bytes_fixed[32]
type addr = bytes_fixed[20]
type key = bytes_fixed[48]

# ---- the payload envelope

struct Digest {
    Kind u8   @0
    Hash id32 @1
}

struct Call {
    Kind    u8    @0
    Source  bytes @1
    Payload bytes @9
}

# ---- what an L1 says

struct Register {
    Kind             u8         @0
    ChainID          id32       @1
    BLSKey           key        @33
    Expiry           u64        @81
    Weight           u64        @89
    NodeID           bytes      @97
    RemoveThreshold  u32        @105
    RemoveAddrs      list<addr> @109
    DisableThreshold u32        @117
    DisableAddrs     list<addr> @121
}

struct Registration {
    Kind         u8   @0
    ValidationID id32 @1
    Registered   bool @33
}

struct Reweight {
    Kind         u8   @0
    ValidationID id32 @1
    Nonce        u64  @33
    Weight       u64  @41
}

struct Conversion {
    Kind u8   @0
    ID   id32 @1
}

# ---- the conversion preimage
#
# No kind byte: it is never dispatched, only hashed. Each validator names a
# run in one blob of node ids, whose lengths differ.

struct Validator {
    NodeIDStart u32 @0
    NodeIDLen   u32 @4
    BLSKey      key @8
    Weight      u64 @56
}

struct Conversions {
    ChainID        id32            @0
    ManagerChainID id32            @32
    ManagerAddress bytes           @64
    Validators     list<Validator> @72
    NodeIDPool     bytes           @80
}
