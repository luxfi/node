# SPDX-License-Identifier: BSD-3-Clause-Eco
# The X-chain wire.
#
# The offsets are the format. They are stated here once and every language's
# accessors come out of this file, so an X transaction built in Rust is the
# bytes a Go peer builds.
#
# Two things the chain does that are the chain's, not ZAP's, and so are not
# here. An fx primitive travels behind two bytes — the family that owns it and
# the shape within that family — which the caller strips before it reaches a
# reader. And a transaction's kind is the first byte of its own object, which
# picks which of the five structs below to read it as.

package xchain

type id32 = bytes_fixed[32]
type addr = bytes_fixed[20]

# ---- what an output is owned by ----------------------------------------
#
# One shape, three kinds: a mint output and an owned output are an owner set
# and nothing else, and are told apart by the family byte in front of them,
# not by their contents.
struct Owners {
    Locktime  u64        @0
    Threshold u32        @8
    Addresses list<addr> @12
}

struct TransferOutput {
    Amount    u64        @0
    Locktime  u64        @8
    Threshold u32        @16
    Addresses list<addr> @20
}

struct TransferInput {
    Amount     u64       @0
    SigIndices list<u32> @8
}

# The signatures are a byte run: how many there are is the run divided by the
# family's signature width, so a partial signature cannot pass for one.
struct Credential {
    SecurityLevel u8       @0
    Signatures    list<u8> @4
    PubKeys       list<u8> @12
}

struct MintOperation {
    SigIndices     list<u32> @0
    MintOutput     bytes     @8
    TransferOutput bytes     @16
}

struct BurnOperation {
    SigIndices list<u32> @0
}

struct NftMintOutput {
    Group     u32        @0
    Locktime  u64        @4
    Threshold u32        @12
    Addresses list<addr> @16
}

struct NftTransferOutput {
    Group     u32        @0
    Locktime  u64        @4
    Threshold u32        @12
    Addresses list<addr> @16
    Payload   bytes      @24
}

# The four bytes after the owner run are the pad that carries the object to an
# eight-aligned size; naming them is what makes the emitted size the one the
# wire uses.
struct NftMintOperation {
    SigIndices  list<u32>      @0
    Group       u32            @8
    Payload     bytes          @12
    OwnersCount u32            @20
    OwnersBytes bytes          @24
    Pad         bytes_fixed[4] @32
}

struct NftTransferOperation {
    SigIndices list<u32> @0
    Output     bytes     @8
}

# ---- what a transaction spends and makes --------------------------------

struct TransferableOut {
    Asset  id32  @0
    Output bytes @32
}

struct TransferableIn {
    TxID   id32  @0
    Index  u32   @32
    Asset  id32  @36
    Input  bytes @68
}

struct Utxo {
    TxID   id32  @0
    Index  u32   @32
    Asset  id32  @36
    Output bytes @68
}

# One reference to a spent output, inline in a 36-byte-stride list.
struct UtxoId {
    TxID  id32 @0
    Index u32  @32
}

# The multi-asset spending envelope every X transaction carries. Its outputs
# and inputs are pointer runs into this same buffer, not a concatenation of
# separately framed blobs: one buffer, one pass, and each leaf read where it
# lies.
struct Envelope {
    Network u32                     @0
    Chain   id32                    @8
    Outs    list<ptr<TransferableOut>> @40
    Ins     list<ptr<TransferableIn>>  @48
    Memo    bytes                   @56
}

struct Signed {
    Unsigned        bytes @0
    CredentialCount u32   @8
    CredentialBytes bytes @12
}

# ---- the five transactions ----------------------------------------------
#
# Each opens with the kind byte and the spending envelope's bytes, at the same
# two offsets, and then says its own thing.

struct Base {
    Kind u8    @0
    Base bytes @8
}

struct CreateAsset {
    Kind        u8        @0
    Base        bytes     @8
    Name        bytes     @16
    Symbol      bytes     @24
    Denominator u8        @32
    StateLens   list<u32> @36
    StateBlob   bytes     @44
}

struct Operate {
    Kind    u8        @0
    Base    bytes     @8
    OpLens  list<u32> @16
    OpBlob  bytes     @24
}

struct Import {
    Kind   u8                        @0
    Base   bytes                     @8
    Source id32                      @16
    Ins    list<ptr<TransferableIn>> @48
}

struct Export {
    Kind        u8                         @0
    Base        bytes                      @8
    Destination id32                       @16
    Outs        list<ptr<TransferableOut>> @48
}

# What an asset starts out with, per feature extension, and one operation on
# an asset already made.
struct InitialState {
    FxIndex  u32       @0
    OutLens  list<u32> @4
    OutBlob  bytes     @12
}

struct Operation {
    Asset    id32          @0
    UtxoIds  list<UtxoId>  @32
    FxOp     bytes         @40
}

# ---- a block -------------------------------------------------------------

struct Block {
    Parent    id32      @0
    Height    u64       @32
    Time      u64       @40
    Root      id32      @48
    TxLengths list<u32> @80
    TxBlob    bytes     @88
}
