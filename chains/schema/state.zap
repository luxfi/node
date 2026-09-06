# SPDX-License-Identifier: BSD-3-Clause-Eco
# What the P-chain writes down.
#
# These are records in this node's own store, not messages between nodes: a
# peer never sees them, and Go's store writes its own layout. They are stated
# here for the same reason the wire is — the offsets ARE the format, and a
# record read at the wrong offset is a validator with the wrong weight, found
# at the next restart rather than at the write.
#
# Nothing here is consensus. A change to this file rewrites what a node reads
# back from its own disk, which is a migration; a change to a wire schema is a
# fork. They are separate files because they are separate consequences.

package state

type id32 = bytes_fixed[32]
type addr = bytes_fixed[20]

# An owner set, as it is stored: the addresses are one flat run rather than a
# list, because nothing indexes into them here.
struct Owners {
    Locktime  u64   @0
    Threshold u32   @8
    Addresses bytes @16
}

# One staker. The key is present or it is not — a validator registered before
# keys existed has none, and an all-zero key would be a key that verifies
# nothing, so a byte says which and the run says what.
struct Staker {
    Weight    u64   @0
    Start     u64   @8
    End       u64   @16
    Reward    u64   @24
    Next      u64   @32
    Priority  u8    @40
    HasKey    u8    @41
    PublicKey bytes @48
    # Sixteen bytes past the last field. The record has always reserved them
    # and no field has ever covered them, so naming them is what keeps the
    # emitted size the size already on disk — a schema that stopped at the
    # public key would write a shorter record than every record already
    # written.
    Reserved  bytes_fixed[16] @56
}

struct L1Validator {
    Chain          id32  @0
    NodeID         addr  @32
    Start          u64   @56
    Weight         u64   @64
    MinNonce       u64   @72
    EndFee         u64   @80
    PublicKey      bytes @88
    RemainingOwner bytes @96
    DisableOwner   bytes @104
}

struct Conversion {
    ConversionID id32  @0
    Chain        id32  @32
    Address      bytes @64
}

# One pending change to a validator set: how the weight moved, whether the key
# changed, and the two keys it moved between.
struct Change {
    Amount     u64   @0
    Decrease   u8    @8
    Renamed    u8    @9
    Validation id32  @16
    KeyBefore  bytes @48
    KeyAfter   bytes @56
}
