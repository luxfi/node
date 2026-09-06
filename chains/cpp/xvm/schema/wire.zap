# SPDX-License-Identifier: BSD-3-Clause-Eco
# The X-chain's wire, as a schema.
#
# These offsets are not invented here. They are the consensus offsets the Go
# reference states in github.com/luxfi/utxo/wire, one file per shape; written
# down once here, the C++ readers and builders come out of zapgen instead of
# out of a person, and the P-chain, the Rust port and this one stop being
# three chances to mistype the same number.
#
# What is NOT here is the 2-byte (TypeKind, ShapeKind) discriminator each of
# these travels behind. That prefix sits OUTSIDE the ZAP message — it says
# which fx family and which shape the bytes are, which is a question about
# the chain, not about the payload. wire.hpp answers it; this file describes
# what follows it.
#
# Three shapes share OutputOwners' payload exactly — a mint authority, a
# property state output and a bare owner group are the same four fields. They
# are one struct here and three ShapeKinds there, which is where the
# difference actually lives.

package lux.xvm.wire

type id32 = bytes_fixed[32]
type addr20 = bytes_fixed[20]

# An owner group: when it unlocks, how many of the addresses have to sign, and
# who they are. Also the payload of ShapeKind MintOutput and OwnedOutput.
struct OutputOwners {
    Locktime  u64          @0
    Threshold u32          @8
    Addrs     list<addr20> @12
}

# An amount, owned.
struct TransferOutput {
    Amount    u64          @0
    Locktime  u64          @8
    Threshold u32          @16
    Addrs     list<addr20> @20
}

# An amount, spent, naming which of the owner's addresses signed.
struct TransferInput {
    Amount     u64       @0
    SigIndices list<u32> @8
}

# Minting: who authorised it, the authority that survives, and what it made.
# The two outputs are whole envelopes — they carry their own discriminator —
# so they are byte runs here rather than nested structs.
struct MintOperation {
    SigIndices     list<u32> @0
    MintOutput     bytes     @8
    TransferOutput bytes     @16
}

# The signatures over a transaction, and the keys that made them. Both are
# runs of bytes rather than lists of signatures: the fx knows its own
# signature width, and a run that does not divide by it is no signatures
# rather than a partial one.
struct Credential {
    SecurityLevel u8       @0
    Signatures    list<u8> @4
    PubKeys       list<u8> @12
}

# One unspent output, standing alone: where it came from and what it is.
struct UTXO {
    TxID        id32  @0
    OutputIndex u32   @32
    AssetID     id32  @36
    Output      bytes @68
}

# The asset-bearing containers inside a transaction. They carry a tail, so a
# list of them is a run of pointers.
struct TransferableOut {
    AssetID id32  @0
    Output  bytes @32
}

struct TransferableIn {
    TxID        id32  @0
    OutputIndex u32   @32
    AssetID     id32  @36
    Input       bytes @68
}

# The multi-asset spending envelope.
struct XVMBaseTx {
    NetworkID    u32                   @0
    BlockchainID id32                  @8
    Outs         list<TransferableOut> @40
    Ins          list<TransferableIn>  @48
    Memo         bytes                 @56
}

# Unsigned bytes, and the credentials packed behind them. The credentials are
# a run of whole envelopes, each self-delimiting by its own ZAP size word, so
# the count is carried beside the run rather than derived from it.
struct SignedTx {
    UnsignedBytes   bytes @0
    CredentialCount u32   @8
    CredentialBytes bytes @12
}

# nftfx.

struct NFTMintOutput {
    GroupID   u32          @0
    Locktime  u64          @4
    Threshold u32          @12
    Addrs     list<addr20> @16
}

struct NFTTransferOutput {
    GroupID   u32          @0
    Locktime  u64          @4
    Threshold u32          @12
    Addrs     list<addr20> @16
    Payload   bytes        @24
}

# The reserved width is 36; the fields end at 32. The wire says so, so the
# schema says so.
struct NFTMintOperation @36 {
    SigIndices  list<u32> @0
    GroupID     u32       @8
    Payload     bytes     @12
    OwnersCount u32       @20
    OwnersBytes bytes     @24
}

struct NFTTransferOperation {
    SigIndices  list<u32> @0
    OutputBytes bytes     @8
}

# propertyfx.

struct BurnOperation {
    SigIndices list<u32> @0
}
