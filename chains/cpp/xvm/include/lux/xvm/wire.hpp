// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// wire.hpp — the cross-fx wire envelopes the X-Chain composes its transactions
// out of, ported from luxfi/utxo/wire.
//
// Every fx primitive names itself on the wire with a 2-byte discriminator:
//
//   [TypeKind:1][ShapeKind:1][ZAP message: N]
//
// TypeKind names the fx FAMILY (secp256k1, nft, property, ...); ShapeKind names
// the SHAPE within it (TransferOutput, TransferInput, MintOutput, Credential,
// ...). Those are two independent questions, so they are two bytes rather than
// one dense slot id — which is the whole reason the legacy codec registry is
// gone. Adding an (fx, shape) is a new branch, never a growing shared table.

#pragma once

#include "lux/xvm/id.hpp"
#include "lux/core/zap.hpp"

#include <expected>
#include <string>
#include <vector>

namespace lux::xvm {
// ZAP belongs to the node, not to this chain: `zap::` below is lux/core/zap.hpp
// and there is no other one to reach.
namespace zap = lux::core::zap;
}  // namespace lux::xvm

namespace lux::xvm::wire {

enum class TypeKind : std::uint8_t {
    Reserved = 0x00,
    Secp256k1 = 0x01,
    MLDSA = 0x02,
    SLHDSA = 0x03,
    Ed25519 = 0x04,
    Secp256r1 = 0x05,
    Schnorr = 0x06,
    BLS12381 = 0x07,
    // Application fx families (built on secp256k1 credentials).
    NFT = 0x08,
    Property = 0x09,
};

enum class ShapeKind : std::uint8_t {
    Reserved = 0x00,
    TransferOutput = 0x01,
    TransferInput = 0x02,
    MintOutput = 0x03,
    MintInput = 0x04,
    MintOperation = 0x05,
    Credential = 0x06,
    AttestationOut = 0x07,
    AttestationIn = 0x08,
    OutputOwners = 0x09,
    UTXO = 0x0A,
    TransferableOut = 0x0B,
    TransferableIn = 0x0C,
    PChainOwner = 0x0D,
    SignedTx = 0x0E,
    LockedOutput = 0x0F,
    NFTMintOutput = 0x10,
    NFTTransferOutput = 0x11,
    XVMBaseTx = 0x12,
    NFTMintOperation = 0x13,
    NFTTransferOp = 0x14,
    OwnedOutput = 0x15,
    BurnOperation = 0x16,
};

// The error set every Wrap* returns. These are the exact Go sentinels — a
// discriminator that does not match the shape being read is a cross-type
// confusion attempt, not a parse detail.
inline constexpr const char* kErrWrongTypeKind =
    "wire: TypeKind discriminator does not match expected fx family";
inline constexpr const char* kErrWrongShapeKind =
    "wire: ShapeKind discriminator does not match expected primitive shape";
inline constexpr const char* kErrShortEnvelope =
    "wire: envelope shorter than 2-byte discriminator prefix";
inline constexpr const char* kErrTrailingBytes =
    "wire: trailing bytes after zap message (non-canonical envelope)";

// Semantic gates on an owner group, read off an untrusted buffer.
inline constexpr const char* kErrOwnerThresholdZero =
    "wire: OutputOwners.Threshold must be > 0; threshold=0 disables authorization";
inline constexpr const char* kErrOwnerThresholdExceedsAddrs =
    "wire: OutputOwners.Threshold exceeds Addresses.Len() — unsatisfiable signer quorum";
inline constexpr const char* kErrOwnerAddrsEmpty =
    "wire: OutputOwners.Addresses is empty — signer set undefined";
inline constexpr const char* kErrOwnerAddrZero =
    "wire: OutputOwners.Addresses contains the zero ShortID — phantom signer";

template <class T>
using Result = std::expected<T, std::string>;

inline constexpr int kEnvelopePrefix = 2;
inline constexpr std::uint32_t kAddressStride = 20;
inline constexpr std::uint32_t kSigIndexStride = 4;
inline constexpr std::uint32_t kObjPtrStride = 4;

struct Discriminator {
    TypeKind type_kind;
    ShapeKind shape_kind;
};

// peek_discriminator reads the (TypeKind, ShapeKind) without committing to a
// shape. Composite dispatchers use it to recurse.
Result<Discriminator> peek_discriminator(ByteView b);

// next_envelope splits the FIRST self-describing envelope off a packed run and
// returns (envelope, rest). The length comes from the inner ZAP header's own
// size field, which is why a packed list needs no separate length array. This is
// the ONE walker for every packed envelope run.
struct Split {
    ByteView envelope;
    ByteView rest;
};
Result<Split> next_envelope(ByteView blob);

Bytes write_envelope_prefix(TypeKind tk, ShapeKind sk, const Bytes& zap_bytes);

// ---- AddressList: the stride-20 owner address list ----
class AddressList {
public:
    AddressList() = default;
    explicit AddressList(zap::List l) : list_(l) {}

    int len() const { return list_.len(); }
    bool is_null() const { return list_.is_null(); }
    ShortId at(int i) const;
    std::vector<ShortId> all() const;

private:
    zap::List list_;
};

// ---- OutputOwners (Locktime, Threshold, Addresses) ----
inline constexpr int kOffOwnersLocktime = 0;
inline constexpr int kOffOwnersThreshold = 8;
inline constexpr int kOffOwnersAddressList = 12;
inline constexpr int kSizeOutputOwners = 20;

class OutputOwners {
public:
    OutputOwners() = default;
    OutputOwners(zap::Message m, zap::Object o) : msg_(m), obj_(o) {}

    std::uint64_t locktime() const { return obj_.u64(kOffOwnersLocktime); }
    std::uint32_t threshold() const { return obj_.u32(kOffOwnersThreshold); }
    AddressList address_list() const {
        return AddressList(obj_.list_stride(kOffOwnersAddressList, kAddressStride));
    }
    bool is_zero() const { return !msg_.valid(); }
    // Every executor-side gate on an owner group, in one place.
    Result<void> syntactic_verify() const;

private:
    zap::Message msg_;
    zap::Object obj_;
};

struct OutputOwnersInput {
    std::uint64_t locktime = 0;
    std::uint32_t threshold = 0;
    std::vector<ShortId> addresses;
};
Bytes new_output_owners(const OutputOwnersInput& in);
Result<OutputOwners> wrap_output_owners(ByteView b);

// ---- TransferOutput (Amount + owner group) ----
inline constexpr int kOffTransferOutputAmount = 0;
inline constexpr int kOffTransferOutputLocktime = 8;
inline constexpr int kOffTransferOutputThreshold = 16;
inline constexpr int kOffTransferOutputAddressList = 20;
inline constexpr int kSizeTransferOutput = 28;

class TransferOutput {
public:
    TransferOutput() = default;
    TransferOutput(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::uint64_t amount() const { return obj_.u64(kOffTransferOutputAmount); }
    std::uint64_t locktime() const { return obj_.u64(kOffTransferOutputLocktime); }
    std::uint32_t threshold() const { return obj_.u32(kOffTransferOutputThreshold); }
    AddressList address_list() const {
        return AddressList(obj_.list_stride(kOffTransferOutputAddressList, kAddressStride));
    }
    bool is_zero() const { return !msg_.valid(); }
    Result<void> syntactic_verify() const;

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct TransferOutputInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::uint64_t amount = 0;
    std::uint64_t locktime = 0;
    std::uint32_t threshold = 0;
    std::vector<ShortId> addresses;
};
Bytes new_transfer_output(const TransferOutputInput& in);
Result<TransferOutput> wrap_transfer_output(ByteView b);

// ---- TransferInput (Amount + SigIndices) ----
inline constexpr int kOffTransferInputAmount = 0;
inline constexpr int kOffTransferInputSigIndices = 8;
inline constexpr int kSizeTransferInput = 16;

class TransferInput {
public:
    TransferInput() = default;
    TransferInput(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::uint64_t amount() const { return obj_.u64(kOffTransferInputAmount); }
    std::vector<std::uint32_t> sig_indices() const;
    bool is_zero() const { return !msg_.valid(); }

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct TransferInputInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::uint64_t amount = 0;
    std::vector<std::uint32_t> sig_indices;
};
Bytes new_transfer_input(const TransferInputInput& in);
Result<TransferInput> wrap_transfer_input(ByteView b);

// ---- MintOutput: the same payload as an owner group, a different ShapeKind.
// The discriminator is what separates "mint authority" from "spending output".
inline constexpr int kSizeMintOutput = kSizeOutputOwners;

class MintOutput {
public:
    MintOutput() = default;
    MintOutput(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::uint64_t locktime() const { return obj_.u64(kOffOwnersLocktime); }
    std::uint32_t threshold() const { return obj_.u32(kOffOwnersThreshold); }
    AddressList address_list() const {
        return AddressList(obj_.list_stride(kOffOwnersAddressList, kAddressStride));
    }
    bool is_zero() const { return !msg_.valid(); }
    Result<void> syntactic_verify() const;

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct MintOutputInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::uint64_t locktime = 0;
    std::uint32_t threshold = 0;
    std::vector<ShortId> addresses;
};
Bytes new_mint_output(const MintOutputInput& in);
Result<MintOutput> wrap_mint_output(ByteView b);

// ---- MintOperation (SigIndices + nested MintOutput + nested TransferOutput) ----
inline constexpr int kOffMintOpSigIndices = 0;
inline constexpr int kOffMintOpMintOutput = 8;
inline constexpr int kOffMintOpTransferOutput = 16;
inline constexpr int kSizeMintOperation = 24;

class MintOperation {
public:
    MintOperation() = default;
    MintOperation(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::vector<std::uint32_t> sig_indices() const;
    ByteView mint_output_bytes() const { return obj_.bytes(kOffMintOpMintOutput); }
    ByteView transfer_output_bytes() const { return obj_.bytes(kOffMintOpTransferOutput); }
    bool is_zero() const { return !msg_.valid(); }

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct MintOperationInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::vector<std::uint32_t> sig_indices;
    Bytes mint_output;
    Bytes transfer_output;
};
Bytes new_mint_operation(const MintOperationInput& in);
Result<MintOperation> wrap_mint_operation(ByteView b);

// ---- Credential (SecurityLevel + concatenated signatures + pubkeys) ----
inline constexpr int kOffCredSecurityLevel = 0;
inline constexpr int kOffCredSignatureList = 4;
inline constexpr int kOffCredPubKeyList = 12;
inline constexpr int kSizeCredential = 20;

class Credential {
public:
    Credential() = default;
    Credential(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::uint8_t security_level() const { return obj_.u8(kOffCredSecurityLevel); }
    Bytes signature_bytes() const;
    Bytes pubkey_bytes() const;
    // The signature COUNT is derived, because the wire stores concatenated
    // bytes: a run that does not divide by the fx's signature size is 0
    // signatures, never a partial one.
    int signature_count(int sig_size) const;
    Bytes signature_at(int i, int sig_size) const;
    bool is_zero() const { return !msg_.valid(); }

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct CredentialInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::uint8_t security_level = 0;
    Bytes signatures;
    Bytes pubkeys;
};
Bytes new_credential(const CredentialInput& in);
Result<Credential> wrap_credential(ByteView b);

// ---- UTXO (TxID + OutputIndex + AssetID + inner output envelope) ----
inline constexpr int kOffUTXOTxID = 0;
inline constexpr int kOffUTXOOutputIndex = 32;
inline constexpr int kOffUTXOAssetID = 36;
inline constexpr int kOffUTXOOutput = 68;
inline constexpr int kSizeUTXO = 76;

class UTXOWire {
public:
    UTXOWire() = default;
    UTXOWire(ByteView b, zap::Message m, zap::Object o) : b_(b), msg_(m), obj_(o) {}

    Id tx_id() const;
    std::uint32_t output_index() const { return obj_.u32(kOffUTXOOutputIndex); }
    Id asset_id() const;
    ByteView output_bytes() const { return obj_.bytes(kOffUTXOOutput); }
    ByteView bytes() const { return b_; }
    bool is_zero() const { return !msg_.valid(); }

private:
    ByteView b_;
    zap::Message msg_;
    zap::Object obj_;
};

struct UTXOInput {
    Id tx_id{};
    std::uint32_t output_index = 0;
    Id asset_id{};
    Bytes output;
};
Bytes new_utxo(const UTXOInput& in);
Result<UTXOWire> wrap_utxo(ByteView b);

// ---- TransferableOut / TransferableIn: the fx-agnostic containers that bind an
// AssetID (and, for an input, the spent UTXOID) to an inner fx primitive. They
// live INLINE inside their parent buffer — no container prefix, no blob concat.
inline constexpr int kOffTransferableOutAssetID = 0;
inline constexpr int kOffTransferableOutOutput = 32;
inline constexpr int kSizeTransferableOut = 40;

inline constexpr int kOffTransferableInTxID = 0;
inline constexpr int kOffTransferableInOutputIndex = 32;
inline constexpr int kOffTransferableInAssetID = 36;
inline constexpr int kOffTransferableInInput = 68;
inline constexpr int kSizeTransferableIn = 76;

class TransferableOut {
public:
    TransferableOut() = default;
    explicit TransferableOut(zap::Object o) : obj_(o), set_(true) {}

    Id asset_id() const;
    ByteView output_bytes() const { return obj_.bytes(kOffTransferableOutOutput); }
    bool is_zero() const { return !set_; }

private:
    zap::Object obj_;
    bool set_ = false;
};

class TransferableIn {
public:
    TransferableIn() = default;
    explicit TransferableIn(zap::Object o) : obj_(o), set_(true) {}

    Id tx_id() const;
    std::uint32_t output_index() const { return obj_.u32(kOffTransferableInOutputIndex); }
    Id asset_id() const;
    ByteView input_bytes() const { return obj_.bytes(kOffTransferableInInput); }
    bool is_zero() const { return !set_; }

private:
    zap::Object obj_;
    bool set_ = false;
};

int append_transferable_out(zap::Builder& b, const Id& asset_id, ByteView inner_envelope);
int append_transferable_in(zap::Builder& b, const Id& tx_id, std::uint32_t output_index,
                           const Id& asset_id, ByteView inner_envelope);

// ---- XVMBaseTx: the multi-asset spending envelope ----
inline constexpr int kOffXVMBaseTxNetworkID = 0;
inline constexpr int kOffXVMBaseTxBlockchainID = 8;
inline constexpr int kOffXVMBaseTxOuts = 40;
inline constexpr int kOffXVMBaseTxIns = 48;
inline constexpr int kOffXVMBaseTxMemo = 56;
inline constexpr int kSizeXVMBaseTx = 64;

class XVMBaseTx {
public:
    XVMBaseTx() = default;
    XVMBaseTx(ByteView b, zap::Message m, zap::Object o) : b_(b), msg_(m), obj_(o) {}

    std::uint32_t network_id() const { return obj_.u32(kOffXVMBaseTxNetworkID); }
    Id blockchain_id() const;
    std::uint32_t outs_count() const {
        return std::uint32_t(obj_.list_stride(kOffXVMBaseTxOuts, kObjPtrStride).len());
    }
    Result<TransferableOut> out_at(std::uint32_t i) const;
    std::uint32_t ins_count() const {
        return std::uint32_t(obj_.list_stride(kOffXVMBaseTxIns, kObjPtrStride).len());
    }
    Result<TransferableIn> in_at(std::uint32_t i) const;
    ByteView memo() const { return obj_.bytes(kOffXVMBaseTxMemo); }
    ByteView bytes() const { return b_; }
    bool is_zero() const { return !msg_.valid(); }

private:
    ByteView b_;
    zap::Message msg_;
    zap::Object obj_;
};

struct XVMTransferOut {
    Id asset_id{};
    Bytes output;
};
struct XVMTransferIn {
    Id tx_id{};
    std::uint32_t output_index = 0;
    Id asset_id{};
    Bytes input;
};
struct XVMBaseTxInput {
    std::uint32_t network_id = 0;
    Id blockchain_id{};
    std::vector<XVMTransferOut> outs;
    std::vector<XVMTransferIn> ins;
    Bytes memo;
};
Bytes new_xvm_base_tx(const XVMBaseTxInput& in);
Result<XVMBaseTx> wrap_xvm_base_tx(ByteView b);

// ---- SignedTx: unsigned bytes + a packed run of credential envelopes ----
inline constexpr int kOffSignedTxUnsignedBytes = 0;
inline constexpr int kOffSignedTxCredentialCount = 8;
inline constexpr int kOffSignedTxCredentialBytes = 12;
inline constexpr int kSizeSignedTx = 20;

class SignedTx {
public:
    SignedTx() = default;
    SignedTx(zap::Message m, zap::Object o) : msg_(m), obj_(o) {}

    ByteView unsigned_bytes() const { return obj_.bytes(kOffSignedTxUnsignedBytes); }
    std::uint32_t credential_count() const { return obj_.u32(kOffSignedTxCredentialCount); }
    ByteView credential_bytes() const { return obj_.bytes(kOffSignedTxCredentialBytes); }
    Result<Credential> credential_at(std::uint32_t i) const;
    Result<std::vector<Credential>> all_credentials() const;
    bool is_zero() const { return !msg_.valid(); }

private:
    zap::Message msg_;
    zap::Object obj_;
};

struct SignedTxInput {
    Bytes unsigned_bytes;
    std::vector<Bytes> credentials;
};
Bytes new_signed_tx(const SignedTxInput& in);
Result<SignedTx> wrap_signed_tx(ByteView b);

// ---- nftfx / propertyfx composites ----

inline constexpr int kOffNFTMintOutputGroupID = 0;
inline constexpr int kOffNFTMintOutputLocktime = 4;
inline constexpr int kOffNFTMintOutputThreshold = 12;
inline constexpr int kOffNFTMintOutputAddressList = 16;
inline constexpr int kSizeNFTMintOutput = 24;

class NFTMintOutput {
public:
    NFTMintOutput() = default;
    NFTMintOutput(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::uint32_t group_id() const { return obj_.u32(kOffNFTMintOutputGroupID); }
    std::uint64_t locktime() const { return obj_.u64(kOffNFTMintOutputLocktime); }
    std::uint32_t threshold() const { return obj_.u32(kOffNFTMintOutputThreshold); }
    AddressList address_list() const {
        return AddressList(obj_.list_stride(kOffNFTMintOutputAddressList, kAddressStride));
    }
    bool is_zero() const { return !msg_.valid(); }

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct NFTMintOutputInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::uint32_t group_id = 0;
    std::uint64_t locktime = 0;
    std::uint32_t threshold = 0;
    std::vector<ShortId> addresses;
};
Bytes new_nft_mint_output(const NFTMintOutputInput& in);
Result<NFTMintOutput> wrap_nft_mint_output(ByteView b);

inline constexpr int kOffNFTTransferOutputGroupID = 0;
inline constexpr int kOffNFTTransferOutputLocktime = 4;
inline constexpr int kOffNFTTransferOutputThreshold = 12;
inline constexpr int kOffNFTTransferOutputAddressList = 16;
inline constexpr int kOffNFTTransferOutputPayload = 24;
inline constexpr int kSizeNFTTransferOutput = 32;

class NFTTransferOutput {
public:
    NFTTransferOutput() = default;
    NFTTransferOutput(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::uint32_t group_id() const { return obj_.u32(kOffNFTTransferOutputGroupID); }
    std::uint64_t locktime() const { return obj_.u64(kOffNFTTransferOutputLocktime); }
    std::uint32_t threshold() const { return obj_.u32(kOffNFTTransferOutputThreshold); }
    AddressList address_list() const {
        return AddressList(obj_.list_stride(kOffNFTTransferOutputAddressList, kAddressStride));
    }
    ByteView payload() const { return obj_.bytes(kOffNFTTransferOutputPayload); }
    bool is_zero() const { return !msg_.valid(); }

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct NFTTransferOutputInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::uint32_t group_id = 0;
    Bytes payload;
    std::uint64_t locktime = 0;
    std::uint32_t threshold = 0;
    std::vector<ShortId> addresses;
};
Bytes new_nft_transfer_output(const NFTTransferOutputInput& in);
Result<NFTTransferOutput> wrap_nft_transfer_output(ByteView b);

inline constexpr int kOffNFTMintOpSigIndices = 0;
inline constexpr int kOffNFTMintOpGroupID = 8;
inline constexpr int kOffNFTMintOpPayload = 12;
inline constexpr int kOffNFTMintOpOwnersCount = 20;
inline constexpr int kOffNFTMintOpOwnersBytes = 24;
inline constexpr int kSizeNFTMintOperation = 36;

class NFTMintOperation {
public:
    NFTMintOperation() = default;
    NFTMintOperation(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::uint32_t group_id() const { return obj_.u32(kOffNFTMintOpGroupID); }
    std::vector<std::uint32_t> sig_indices() const;
    ByteView payload() const { return obj_.bytes(kOffNFTMintOpPayload); }
    std::uint32_t owners_count() const { return obj_.u32(kOffNFTMintOpOwnersCount); }
    ByteView owners_bytes() const { return obj_.bytes(kOffNFTMintOpOwnersBytes); }
    bool is_zero() const { return !msg_.valid(); }

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct NFTMintOperationInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::vector<std::uint32_t> sig_indices;
    std::uint32_t group_id = 0;
    Bytes payload;
    std::vector<Bytes> owners;
};
Bytes new_nft_mint_operation(const NFTMintOperationInput& in);
Result<NFTMintOperation> wrap_nft_mint_operation(ByteView b);

inline constexpr int kOffNFTTransferOpSigIndices = 0;
inline constexpr int kOffNFTTransferOpOutputBytes = 8;
inline constexpr int kSizeNFTTransferOp = 16;

class NFTTransferOperation {
public:
    NFTTransferOperation() = default;
    NFTTransferOperation(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::vector<std::uint32_t> sig_indices() const;
    ByteView output_bytes() const { return obj_.bytes(kOffNFTTransferOpOutputBytes); }
    bool is_zero() const { return !msg_.valid(); }

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct NFTTransferOperationInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::vector<std::uint32_t> sig_indices;
    Bytes output;
};
Bytes new_nft_transfer_operation(const NFTTransferOperationInput& in);
Result<NFTTransferOperation> wrap_nft_transfer_operation(ByteView b);

// OwnedOutput: a bare owner group as a property STATE output. Same payload as
// OutputOwners; the ShapeKind is what separates the two.
inline constexpr int kSizeOwnedOutput = kSizeOutputOwners;

class OwnedOutput {
public:
    OwnedOutput() = default;
    OwnedOutput(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::uint64_t locktime() const { return obj_.u64(kOffOwnersLocktime); }
    std::uint32_t threshold() const { return obj_.u32(kOffOwnersThreshold); }
    AddressList address_list() const {
        return AddressList(obj_.list_stride(kOffOwnersAddressList, kAddressStride));
    }
    bool is_zero() const { return !msg_.valid(); }

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct OwnedOutputInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::uint64_t locktime = 0;
    std::uint32_t threshold = 0;
    std::vector<ShortId> addresses;
};
Bytes new_owned_output(const OwnedOutputInput& in);
Result<OwnedOutput> wrap_owned_output(ByteView b);

inline constexpr int kOffBurnOpSigIndices = 0;
inline constexpr int kSizeBurnOperation = 8;

class BurnOperation {
public:
    BurnOperation() = default;
    BurnOperation(TypeKind tk, zap::Message m, zap::Object o) : tk_(tk), msg_(m), obj_(o) {}

    TypeKind type_kind() const { return tk_; }
    std::vector<std::uint32_t> sig_indices() const;
    bool is_zero() const { return !msg_.valid(); }

private:
    TypeKind tk_ = TypeKind::Reserved;
    zap::Message msg_;
    zap::Object obj_;
};

struct BurnOperationInput {
    TypeKind type_kind = TypeKind::Reserved;
    std::vector<std::uint32_t> sig_indices;
};
Bytes new_burn_operation(const BurnOperationInput& in);
Result<BurnOperation> wrap_burn_operation(ByteView b);

}  // namespace lux::xvm::wire
