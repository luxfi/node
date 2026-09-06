# SPDX-License-Identifier: BSD-3-Clause-Eco
# The Z-chain wire.
#
# The offsets are the format. They are stated here once and every language's
# accessors come out of this file, so a shielded transaction built in Rust is
# the bytes a Go peer builds.
#
# A shielded chain carries most of its content as opaque runs — a commitment,
# a note, a proof — so most fields here are `bytes` and what they MEAN is the
# cryptography's business, not the wire's. What the wire owes is that a run
# ends where it says it ends.
#
# Repeated items travel as a length list beside one blob rather than as a list
# of pointers: the items are variable-width messages, and a run of lengths
# says where each begins without a pointer per item.

package zchain

type id32 = bytes_fixed[32]

# Value coming in from the transparent side, naming the output it spends.
struct TransparentIn {
    TxID    id32  @0
    Output  u32   @32
    Amount  u64   @36
    Address bytes @44
}

# Value going out to the transparent side.
struct TransparentOut {
    Amount  u64   @0
    Asset   id32  @8
    Address bytes @40
}

# Value going out to the shielded side: a commitment nobody can open without
# the note, and the note encrypted to whoever may.
struct Shielded {
    Commitment bytes @0
    Note       bytes @8
    Ephemeral  bytes @16
    RangeProof bytes @24
}

# A proof and the public inputs it was made over. Absence is an empty bytes
# field where a proof would be, so a proof and no proof are one field rather
# than a flag and a value that can disagree.
struct Proof {
    System     bytes     @0
    Data       bytes     @8
    PublicLens list<u32> @16
    PublicBlob bytes     @24
}

# One unspent shielded output, as it is stored.
struct Utxo {
    TxID       id32  @0
    Output     u32   @32
    Height     u64   @36
    Commitment bytes @44
    Ciphertext bytes @52
    Ephemeral  bytes @60
}

struct Tx {
    Kind         u8        @0
    Version      u8        @1
    Fee          u64       @2
    Expiry       u64       @10
    InLens       list<u32> @18
    InBlob       bytes     @26
    OutLens      list<u32> @34
    OutBlob      bytes     @42
    NullLens     list<u32> @50
    NullBlob     bytes     @58
    ShieldedLens list<u32> @66
    ShieldedBlob bytes     @74
    Proof        bytes     @82
    Memo         bytes     @90
}

struct Block {
    Parent    id32      @0
    Height    u64       @32
    Time      i64       @40
    TxLens    list<u32> @48
    TxBlob    bytes     @56
    StateRoot bytes     @64
    Proof     bytes     @72
}
