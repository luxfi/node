// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fx.hpp — the feature-extension primitives an X-Chain transaction is built out
// of, and the three fx families that give them meaning: secp256k1fx (value),
// nftfx (non-fungible), propertyfx (ownership).
//
// A tx is polymorphic in its outputs, inputs, operations and credentials, and
// the polymorphism is a CLOSED sum: secp256k1 | nft | property. Go dispatches on
// its interface tag; here each value simply SAYS which family it belongs to
// (`family()`), so the "which fx is this?" question is answered by the value
// rather than recovered by inspecting its type. That is the one place this port
// is deliberately simpler than the Go it renders — same answer, no lookup.
//
// Every primitive can emit its canonical wire envelope (`bytes()`), and that
// envelope is BOTH the serialization and the sort key: outputs are ordered by
// the bytes that reach the wire, never by a second, parallel encoding.

#pragma once

#include "lux/xvm/id.hpp"
#include "lux/xvm/wire.hpp"

#include <memory>
#include <string>
#include <vector>

namespace lux::xvm::fx {

template <class T>
using Result = wire::Result<T>;

inline constexpr std::uint64_t kCostPerSignature = 1000;
inline constexpr int kSignatureLen = 65;      // secp256k1 recoverable r||s||v
inline constexpr std::size_t kMaxPayloadSize = 1024;  // nftfx payload cap (1 KiB)

// ---- error sentinels, one per Go error, so a test can assert on the reason ----
inline constexpr const char* kErrNilInput = "nil input";
inline constexpr const char* kErrInputIndicesNotSortedUnique =
    "address indices not sorted and unique";
inline constexpr const char* kErrNilOutput = "nil output";
inline constexpr const char* kErrOutputUnspendable = "output is unspendable";
inline constexpr const char* kErrOutputUnoptimized = "output representation should be optimized";
inline constexpr const char* kErrAddrsNotSortedUnique = "addresses not sorted and unique";
inline constexpr const char* kErrNoValueOutput = "output has no value";
inline constexpr const char* kErrNoValueInput = "input has no value";
inline constexpr const char* kErrNilCredential = "nil credential";
inline constexpr const char* kErrNilMintOperation = "nil mint operation";
inline constexpr const char* kErrNilTransferOutput = "nil transfer output";
inline constexpr const char* kErrPayloadTooLarge = "payload too large";
inline constexpr const char* kErrNilTransferOperation = "nil transfer operation";
inline constexpr const char* kErrNilBurnOperation = "nil burn operation";

inline constexpr const char* kErrWrongVMType = "wrong vm type";
inline constexpr const char* kErrWrongTxType = "wrong tx type";
inline constexpr const char* kErrWrongOpType = "wrong operation type";
inline constexpr const char* kErrWrongUTXOType = "wrong utxo type";
inline constexpr const char* kErrWrongInputType = "wrong input type";
inline constexpr const char* kErrWrongCredentialType = "wrong credential type";
inline constexpr const char* kErrWrongOwnerType = "wrong owner type";
inline constexpr const char* kErrMismatchedAmounts = "utxo amount and input amount are not equal";
inline constexpr const char* kErrWrongNumberOfUTXOs = "wrong number of utxos for the operation";
inline constexpr const char* kErrWrongMintCreated = "wrong mint output created from the operation";
inline constexpr const char* kErrWrongUniqueID = "wrong unique ID provided";
inline constexpr const char* kErrTimelocked = "output is time locked";
inline constexpr const char* kErrTooManySigners = "input has more signers than expected";
inline constexpr const char* kErrTooFewSigners = "input has less signers than expected";
inline constexpr const char* kErrInputOutputIndexOutOfBounds =
    "input referenced a nonexistent address in the output";
inline constexpr const char* kErrInputCredentialSignersMismatch =
    "input expected a different number of signers than provided in the credential";
inline constexpr const char* kErrWrongSig = "wrong signature";

// ================= the value shapes =================

// Signature is a recoverable secp256k1 signature: r || s || v.
using Signature = std::array<std::uint8_t, kSignatureLen>;

// Input is the spend authorization half of every fx: which of the owner's
// addresses are signing, by index.
struct Input {
    std::vector<std::uint32_t> sig_indices;

    Result<std::uint64_t> cost() const;
    Result<void> verify() const;
};

// OutputOwners is the ownership half: a locktime, a signature threshold, and the
// address set the threshold is drawn from.
struct OutputOwners {
    std::uint64_t locktime = 0;
    std::uint32_t threshold = 0;
    std::vector<ShortId> addrs;

    Result<void> verify() const;
    bool equals(const OutputOwners& other) const;
    void sort();
    Bytes bytes() const;  // the shared (Reserved, OutputOwners) envelope
    std::vector<Bytes> addresses() const;
};

Result<OutputOwners> wrap_output_owners(ByteView b);

// ---- the four polymorphic roles ----

// FxValue: anything that names its family and emits a canonical wire envelope.
struct FxValue {
    virtual ~FxValue() = default;
    virtual wire::TypeKind family() const = 0;
    virtual Bytes bytes() const = 0;
    virtual Result<void> verify() const = 0;
};

// FxOutput is Go's verify.State: a UTXO's payload.
struct FxOutput : FxValue {
    // owners() is how a spend is authorized; a state output that has no owner
    // group returns nullptr and simply cannot be spent by a credential.
    virtual const OutputOwners* owners() const = 0;
    // addresses() is what the atomic-memory index keys an exported UTXO on.
    virtual std::vector<Bytes> addresses() const { return {}; }
};

// FxTransferOut carries value: it is an FxOutput that has an Amount.
struct FxTransferOut : FxOutput {
    virtual std::uint64_t amount() const = 0;
};

// FxTransferIn spends value.
struct FxTransferIn : FxValue {
    virtual std::uint64_t amount() const = 0;
    virtual Result<std::uint64_t> cost() const = 0;
};

// FxOperation runs over existing UTXOs and produces new state outputs.
struct FxOperation : FxValue {
    virtual Result<std::uint64_t> cost() const = 0;
    virtual std::vector<std::shared_ptr<FxOutput>> outs() const = 0;
};

// FxCredential proves the spend.
struct FxCredential : FxValue {
    virtual const std::vector<Signature>& sigs() const = 0;
};

// ================= secp256k1fx =================
namespace secp256k1fx {

inline constexpr wire::TypeKind kTypeKind = wire::TypeKind::Secp256k1;

struct TransferOutput final : FxTransferOut {
    std::uint64_t amt = 0;
    OutputOwners out_owners;

    wire::TypeKind family() const override { return kTypeKind; }
    std::uint64_t amount() const override { return amt; }
    const OutputOwners* owners() const override { return &out_owners; }
    std::vector<Bytes> addresses() const override { return out_owners.addresses(); }
    Result<void> verify() const override;
    Bytes bytes() const override;
};
Result<std::shared_ptr<TransferOutput>> wrap_transfer_output(ByteView b);

struct TransferInput final : FxTransferIn {
    std::uint64_t amt = 0;
    Input input;

    wire::TypeKind family() const override { return kTypeKind; }
    std::uint64_t amount() const override { return amt; }
    Result<std::uint64_t> cost() const override { return input.cost(); }
    Result<void> verify() const override;
    Bytes bytes() const override;
};
Result<std::shared_ptr<TransferInput>> wrap_transfer_input(ByteView b);

struct MintOutput final : FxOutput {
    OutputOwners out_owners;

    wire::TypeKind family() const override { return kTypeKind; }
    const OutputOwners* owners() const override { return &out_owners; }
    std::vector<Bytes> addresses() const override { return out_owners.addresses(); }
    Result<void> verify() const override { return out_owners.verify(); }
    Bytes bytes() const override;
};
Result<std::shared_ptr<MintOutput>> wrap_mint_output(ByteView b);

struct MintOperation final : FxOperation {
    Input mint_input;
    MintOutput mint_output;
    TransferOutput transfer_output;

    wire::TypeKind family() const override { return kTypeKind; }
    Result<std::uint64_t> cost() const override { return mint_input.cost(); }
    std::vector<std::shared_ptr<FxOutput>> outs() const override;
    Result<void> verify() const override;
    Bytes bytes() const override;
};
Result<std::shared_ptr<MintOperation>> wrap_mint_operation(ByteView b);

struct Credential final : FxCredential {
    std::vector<Signature> signatures;

    wire::TypeKind family() const override { return kTypeKind; }
    const std::vector<Signature>& sigs() const override { return signatures; }
    Result<void> verify() const override { return {}; }
    Bytes bytes() const override;
};
Result<std::shared_ptr<Credential>> wrap_credential(ByteView b);

}  // namespace secp256k1fx

// ================= nftfx =================
namespace nftfx {

inline constexpr wire::TypeKind kTypeKind = wire::TypeKind::NFT;

struct MintOutput final : FxOutput {
    std::uint32_t group_id = 0;
    OutputOwners out_owners;

    wire::TypeKind family() const override { return kTypeKind; }
    const OutputOwners* owners() const override { return &out_owners; }
    std::vector<Bytes> addresses() const override { return out_owners.addresses(); }
    Result<void> verify() const override { return out_owners.verify(); }
    Bytes bytes() const override;
};
Result<std::shared_ptr<MintOutput>> wrap_mint_output(ByteView b);

struct TransferOutput final : FxOutput {
    std::uint32_t group_id = 0;
    Bytes payload;
    OutputOwners out_owners;

    wire::TypeKind family() const override { return kTypeKind; }
    const OutputOwners* owners() const override { return &out_owners; }
    std::vector<Bytes> addresses() const override { return out_owners.addresses(); }
    Result<void> verify() const override;
    Bytes bytes() const override;
};
Result<std::shared_ptr<TransferOutput>> wrap_transfer_output(ByteView b);

struct MintOperation final : FxOperation {
    Input mint_input;
    std::uint32_t group_id = 0;
    Bytes payload;
    std::vector<std::shared_ptr<OutputOwners>> outputs;

    wire::TypeKind family() const override { return kTypeKind; }
    Result<std::uint64_t> cost() const override { return mint_input.cost(); }
    std::vector<std::shared_ptr<FxOutput>> outs() const override;
    Result<void> verify() const override;
    Bytes bytes() const override;
};
Result<std::shared_ptr<MintOperation>> wrap_mint_operation(ByteView b);

struct TransferOperation final : FxOperation {
    Input input;
    TransferOutput output;

    wire::TypeKind family() const override { return kTypeKind; }
    Result<std::uint64_t> cost() const override { return input.cost(); }
    std::vector<std::shared_ptr<FxOutput>> outs() const override;
    Result<void> verify() const override;
    Bytes bytes() const override;
};
Result<std::shared_ptr<TransferOperation>> wrap_transfer_operation(ByteView b);

struct Credential final : FxCredential {
    std::vector<Signature> signatures;

    wire::TypeKind family() const override { return kTypeKind; }
    const std::vector<Signature>& sigs() const override { return signatures; }
    Result<void> verify() const override { return {}; }
    Bytes bytes() const override;
};
Result<std::shared_ptr<Credential>> wrap_credential(ByteView b);

}  // namespace nftfx

// ================= propertyfx =================
namespace propertyfx {

inline constexpr wire::TypeKind kTypeKind = wire::TypeKind::Property;

struct MintOutput final : FxOutput {
    OutputOwners out_owners;

    wire::TypeKind family() const override { return kTypeKind; }
    const OutputOwners* owners() const override { return &out_owners; }
    std::vector<Bytes> addresses() const override { return out_owners.addresses(); }
    Result<void> verify() const override { return out_owners.verify(); }
    Bytes bytes() const override;
};
Result<std::shared_ptr<MintOutput>> wrap_mint_output(ByteView b);

struct OwnedOutput final : FxOutput {
    OutputOwners out_owners;

    wire::TypeKind family() const override { return kTypeKind; }
    const OutputOwners* owners() const override { return &out_owners; }
    std::vector<Bytes> addresses() const override { return out_owners.addresses(); }
    Result<void> verify() const override { return out_owners.verify(); }
    Bytes bytes() const override;
};
Result<std::shared_ptr<OwnedOutput>> wrap_owned_output(ByteView b);

struct MintOperation final : FxOperation {
    Input mint_input;
    MintOutput mint_output;
    OwnedOutput owned_output;

    wire::TypeKind family() const override { return kTypeKind; }
    Result<std::uint64_t> cost() const override { return mint_input.cost(); }
    std::vector<std::shared_ptr<FxOutput>> outs() const override;
    Result<void> verify() const override;
    Bytes bytes() const override;
};
Result<std::shared_ptr<MintOperation>> wrap_mint_operation(ByteView b);

struct BurnOperation final : FxOperation {
    Input input;

    wire::TypeKind family() const override { return kTypeKind; }
    Result<std::uint64_t> cost() const override { return input.cost(); }
    std::vector<std::shared_ptr<FxOutput>> outs() const override { return {}; }
    Result<void> verify() const override { return input.verify(); }
    Bytes bytes() const override;
};
Result<std::shared_ptr<BurnOperation>> wrap_burn_operation(ByteView b);

struct Credential final : FxCredential {
    std::vector<Signature> signatures;

    wire::TypeKind family() const override { return kTypeKind; }
    const std::vector<Signature>& sigs() const override { return signatures; }
    Result<void> verify() const override { return {}; }
    Bytes bytes() const override;
};
Result<std::shared_ptr<Credential>> wrap_credential(ByteView b);

}  // namespace propertyfx

// ================= the fx families themselves =================

// Clock is the one thing an fx needs from its host: the wall time a locktime is
// judged against. Injected rather than read, so a test states the time.
struct Clock {
    std::uint64_t unix_time = 0;
    std::uint64_t unix() const { return unix_time; }
};

// Fx is what a feature extension must implement for the X-Chain. Two questions:
// can this input+credential spend that UTXO, and can this operation+credential
// consume those UTXOs to produce its outputs.
struct Fx {
    virtual ~Fx() = default;

    virtual void bootstrapping() { bootstrapped_ = false; }
    virtual void bootstrapped() { bootstrapped_ = true; }
    bool is_bootstrapped() const { return bootstrapped_; }

    // verify_transfer: unsigned_bytes is the SIGNING TARGET — the tx's unsigned
    // wire bytes, which every signature was computed over.
    virtual Result<void> verify_transfer(ByteView unsigned_bytes, const FxValue* in,
                                         const FxValue* cred, const FxValue* utxo) const = 0;
    virtual Result<void> verify_operation(ByteView unsigned_bytes, const FxValue* op,
                                          const FxValue* cred,
                                          const std::vector<const FxValue*>& utxos) const = 0;

    Clock* clock = nullptr;

protected:
    bool bootstrapped_ = false;
};

// verify_credentials is the shared spend gate: locktime, threshold, signer
// count, and then — only once bootstrapped — that every signature recovers to
// the address the input pointed at. It is shared because all three fx families
// authorize with secp256k1 credentials; only their SHAPES differ.
Result<void> verify_credentials(const Fx& fx, ByteView unsigned_bytes, const Input& in,
                                const std::vector<Signature>& sigs, const OutputOwners& out);

struct Secp256k1Fx final : Fx {
    Result<void> verify_transfer(ByteView unsigned_bytes, const FxValue* in, const FxValue* cred,
                                 const FxValue* utxo) const override;
    Result<void> verify_operation(ByteView unsigned_bytes, const FxValue* op, const FxValue* cred,
                                  const std::vector<const FxValue*>& utxos) const override;

    // verify_permission asks whether a signer set authorizes an OWNER GROUP
    // that carries no value of its own — a mint authority, an NFT owner list.
    // It is the spend gate without the amount, so it is the one an operation
    // whose UTXO is pure authority goes through.
    Result<void> verify_permission(ByteView unsigned_bytes, const Input& in, const FxValue* cred,
                                   const OutputOwners& owner) const;
};

struct NFTFx final : Fx {
    Result<void> verify_transfer(ByteView unsigned_bytes, const FxValue* in, const FxValue* cred,
                                 const FxValue* utxo) const override;
    Result<void> verify_operation(ByteView unsigned_bytes, const FxValue* op, const FxValue* cred,
                                  const std::vector<const FxValue*>& utxos) const override;
};

struct PropertyFx final : Fx {
    Result<void> verify_transfer(ByteView unsigned_bytes, const FxValue* in, const FxValue* cred,
                                 const FxValue* utxo) const override;
    Result<void> verify_operation(ByteView unsigned_bytes, const FxValue* op, const FxValue* cred,
                                  const std::vector<const FxValue*>& utxos) const override;
};

// ---- envelope dispatch: bytes on the wire -> a typed value ----
//
// One function per role, each a total match on (TypeKind, ShapeKind). Adding an
// (fx, shape) is a new branch; there is no registry to grow and nothing to
// register at startup.
Result<std::shared_ptr<FxOutput>> wrap_output(ByteView envelope);
Result<std::shared_ptr<FxTransferOut>> wrap_transfer_out(ByteView envelope);
Result<std::shared_ptr<FxTransferIn>> wrap_transfer_in(ByteView envelope);
Result<std::shared_ptr<FxOperation>> wrap_operation(ByteView envelope);
Result<std::shared_ptr<FxCredential>> wrap_credential(ByteView envelope);

}  // namespace lux::xvm::fx
