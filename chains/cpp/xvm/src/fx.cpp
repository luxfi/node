// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/fx.hpp"

#include "lux/crypto/secp256k1.h"

#include <algorithm>
#include <cstring>
#include <limits>

namespace lux::xvm::fx {
namespace {

bool sorted_and_unique(const std::vector<std::uint32_t>& v) {
    for (std::size_t i = 0; i + 1 < v.size(); ++i) {
        if (v[i] >= v[i + 1]) return false;
    }
    return true;
}

bool sorted_and_unique(const std::vector<ShortId>& v) {
    for (std::size_t i = 0; i + 1 < v.size(); ++i) {
        if (!(v[i] < v[i + 1])) return false;
    }
    return true;
}


// recover_address recovers the signer of `hash` from a 65-byte recoverable
// signature and returns its Lux address — ripemd160(sha256(compressed pubkey)).
// The COMPRESSED form is what the address commits to, so recovery's
// uncompressed X||Y is compressed here rather than hashed as-is.
//
// THE LAST BYTE IS A RECOVERY ID, NOT A PARITY BIT. It names which of the
// candidate points the signature came from: 0 and 1 choose the y with even or
// odd parity over x = r; 2 and 3 are the same two points over x = r + n, the
// case where the signer's R had an x-coordinate that wrapped the group order.
// The reference admits the byte as a whole and then tries the recovery it
// names — luxfi/crypto rejects >= 4 outright and hands 2 and 3 to the wrapped
// path, k256 the same — and the wrapped path fails for every signature anyone
// can produce, because it needs r < p - n and p - n is about 2^128. So on the
// same signature all three languages must answer: 0 and 1 recover, 2 and 3 do
// not, 4 and above are not recovery ids at all.
//
// Masking the byte — v & 1 — would read 2 as 0 and recover the SAME address the
// untouched signature recovers. Editing one byte of any valid credential would
// then produce a transaction this node accepts and Go and Rust reject, which is
// a chain split, and one that costs an attacker a single XOR. The rule is
// enforced here because the C recovery routine below normalizes its v argument
// rather than refusing it, so the caller is the only place that can hold it.
Result<ShortId> recover_address(const Id& hash, const Signature& sig) {
    std::uint8_t uncompressed[64];
    const std::uint8_t v = sig[64];
    if (v >= 4) return std::unexpected("invalid signature recovery id");
    // 2 and 3 name the wrapped-x recovery. This curve arithmetic cannot express
    // it — secp256k1_ecrecover refuses an r that is not below n, which is
    // exactly what the wrapped case has — and the reference fails it for every
    // reachable signature, so failing it here is the same answer, reached
    // honestly rather than by dropping the bit that says so.
    if (v > 1) return std::unexpected("recovery failed");
    secp256k1_status st =
        secp256k1_ecrecover(hash.data(), sig.data(), sig.data() + 32, v, uncompressed);
    if (st != SECP256K1_OK) return std::unexpected("recovery failed");
    std::array<std::uint8_t, 33> compressed{};
    compressed[0] = std::uint8_t(0x02 | (uncompressed[63] & 1));
    std::memcpy(compressed.data() + 1, uncompressed, 32);
    return pubkey_to_address(view(compressed));
}

}  // namespace

// ---- Input ----

Result<std::uint64_t> Input::cost() const {
    std::uint64_t n = sig_indices.size();
    if (n != 0 && kCostPerSignature > std::numeric_limits<std::uint64_t>::max() / n)
        return std::unexpected("overflow");
    return n * kCostPerSignature;
}

Result<void> Input::verify() const {
    if (!sorted_and_unique(sig_indices)) return std::unexpected(kErrInputIndicesNotSortedUnique);
    return {};
}

// ---- OutputOwners ----

Result<void> OutputOwners::verify() const {
    if (threshold > std::uint32_t(addrs.size())) return std::unexpected(kErrOutputUnspendable);
    if (threshold == 0 && !addrs.empty()) return std::unexpected(kErrOutputUnoptimized);
    if (!sorted_and_unique(addrs)) return std::unexpected(kErrAddrsNotSortedUnique);
    return {};
}

bool OutputOwners::equals(const OutputOwners& other) const {
    return locktime == other.locktime && threshold == other.threshold && addrs == other.addrs;
}

void OutputOwners::sort() { std::sort(addrs.begin(), addrs.end()); }

Bytes OutputOwners::bytes() const {
    return wire::write_envelope_prefix(
        wire::TypeKind::Reserved, wire::ShapeKind::OutputOwners,
        wire::NewOutputOwners(wire::OutputOwnersInput{locktime, threshold, addrs}));
}

std::vector<Bytes> OutputOwners::addresses() const {
    std::vector<Bytes> out;
    out.reserve(addrs.size());
    for (const auto& a : addrs) out.emplace_back(a.begin(), a.end());
    return out;
}

Result<OutputOwners> wrap_output_owners(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::OutputOwners, wire::TypeKind::Reserved);
    if (!o) return std::unexpected(o.error());
    const wire::OutputOwners v(*o);
    return OutputOwners{v.Locktime(), v.Threshold(), wire::addresses(v)};
}

// ================= secp256k1fx =================
namespace secp256k1fx {

Result<void> TransferOutput::verify() const {
    if (amt == 0) return std::unexpected(kErrNoValueOutput);
    return out_owners.verify();
}

Bytes TransferOutput::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::TransferOutput,
        wire::NewTransferOutput(wire::TransferOutputInput{
            amt, out_owners.locktime, out_owners.threshold, out_owners.addrs}));
}

Result<std::shared_ptr<TransferOutput>> wrap_transfer_output(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::TransferOutput, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::TransferOutput v(*o);
    auto out = std::make_shared<TransferOutput>();
    out->amt = v.Amount();
    out->out_owners = OutputOwners{v.Locktime(), v.Threshold(), wire::addresses(v)};
    return out;
}

Result<void> TransferInput::verify() const {
    if (amt == 0) return std::unexpected(kErrNoValueInput);
    return input.verify();
}

Bytes TransferInput::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::TransferInput,
        wire::NewTransferInput(wire::TransferInputInput{amt, input.sig_indices}));
}

Result<std::shared_ptr<TransferInput>> wrap_transfer_input(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::TransferInput, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::TransferInput v(*o);
    auto in = std::make_shared<TransferInput>();
    in->amt = v.Amount();
    in->input.sig_indices = wire::sig_indices(v.SigIndices());
    return in;
}

Bytes MintOutput::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::MintOutput,
        wire::NewOutputOwners(wire::OutputOwnersInput{out_owners.locktime, out_owners.threshold,
                                                      out_owners.addrs}));
}

Result<std::shared_ptr<MintOutput>> wrap_mint_output(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::MintOutput, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::OutputOwners v(*o);
    auto out = std::make_shared<MintOutput>();
    out->out_owners = OutputOwners{v.Locktime(), v.Threshold(), wire::addresses(v)};
    return out;
}

std::vector<std::shared_ptr<FxOutput>> MintOperation::outs() const {
    auto mo = std::make_shared<MintOutput>(mint_output);
    auto to = std::make_shared<TransferOutput>(transfer_output);
    return {mo, to};
}

Result<void> MintOperation::verify() const {
    if (auto r = mint_input.verify(); !r) return r;
    if (auto r = mint_output.verify(); !r) return r;
    return transfer_output.verify();
}

Bytes MintOperation::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::MintOperation,
        wire::NewMintOperation(wire::MintOperationInput{
            mint_input.sig_indices, mint_output.bytes(), transfer_output.bytes()}));
}

Result<std::shared_ptr<MintOperation>> wrap_mint_operation(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::MintOperation, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::MintOperation v(*o);
    auto mo = wrap_mint_output(v.MintOutput());
    if (!mo) return std::unexpected(mo.error());
    auto to = wrap_transfer_output(v.TransferOutput());
    if (!to) return std::unexpected(to.error());
    auto op = std::make_shared<MintOperation>();
    op->mint_input.sig_indices = wire::sig_indices(v.SigIndices());
    op->mint_output = **mo;
    op->transfer_output = **to;
    return op;
}

Bytes Credential::bytes() const {
    Bytes concat;
    concat.reserve(signatures.size() * kSignatureLen);
    for (const auto& s : signatures) concat.insert(concat.end(), s.begin(), s.end());
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::Credential,
        wire::NewCredential(wire::CredentialInput{0, concat, {}}));
}

Result<std::shared_ptr<Credential>> wrap_credential(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::Credential, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::Credential v(*o);
    auto c = std::make_shared<Credential>();
    int n = wire::signature_count(v, kSignatureLen);
    c->signatures.resize(std::size_t(n));
    for (int i = 0; i < n; ++i) {
        Bytes raw = wire::signature_at(v, i, kSignatureLen);
        std::copy(raw.begin(), raw.end(), c->signatures[std::size_t(i)].begin());
    }
    return c;
}

}  // namespace secp256k1fx

// ================= nftfx =================
namespace nftfx {

Bytes MintOutput::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::NFTMintOutput,
        wire::NewNFTMintOutput(wire::NFTMintOutputInput{
            group_id, out_owners.locktime, out_owners.threshold, out_owners.addrs}));
}

Result<std::shared_ptr<MintOutput>> wrap_mint_output(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::NFTMintOutput, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::NFTMintOutput v(*o);
    auto out = std::make_shared<MintOutput>();
    out->group_id = v.GroupID();
    out->out_owners = OutputOwners{v.Locktime(), v.Threshold(), wire::addresses(v)};
    return out;
}

Result<void> TransferOutput::verify() const {
    if (payload.size() > kMaxPayloadSize) return std::unexpected(kErrPayloadTooLarge);
    return out_owners.verify();
}

Bytes TransferOutput::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::NFTTransferOutput,
        wire::NewNFTTransferOutput(wire::NFTTransferOutputInput{
            group_id, out_owners.locktime, out_owners.threshold, out_owners.addrs, payload}));
}

Result<std::shared_ptr<TransferOutput>> wrap_transfer_output(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::NFTTransferOutput, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::NFTTransferOutput v(*o);
    auto out = std::make_shared<TransferOutput>();
    out->group_id = v.GroupID();
    const auto p = v.Payload();
    out->payload.assign(p.begin(), p.end());
    out->out_owners = OutputOwners{v.Locktime(), v.Threshold(), wire::addresses(v)};
    return out;
}

std::vector<std::shared_ptr<FxOutput>> MintOperation::outs() const {
    std::vector<std::shared_ptr<FxOutput>> out;
    out.reserve(outputs.size());
    for (const auto& o : outputs) {
        auto t = std::make_shared<TransferOutput>();
        t->group_id = group_id;
        t->payload = payload;
        t->out_owners = *o;
        out.push_back(t);
    }
    return out;
}

Result<void> MintOperation::verify() const {
    if (payload.size() > kMaxPayloadSize) return std::unexpected(kErrPayloadTooLarge);
    for (const auto& o : outputs) {
        if (auto r = o->verify(); !r) return r;
    }
    return mint_input.verify();
}

Bytes MintOperation::bytes() const {
    // The owners travel as a packed run of whole envelopes, each self-
    // delimiting by its own ZAP size word, so the count rides beside it.
    Bytes owners;
    for (const auto& o : outputs) {
        const Bytes one = o->bytes();
        owners.insert(owners.end(), one.begin(), one.end());
    }
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::NFTMintOperation,
        wire::NewNFTMintOperation(wire::NFTMintOperationInput{
            mint_input.sig_indices, group_id, payload,
            static_cast<std::uint32_t>(outputs.size()), owners}));
}

Result<std::shared_ptr<MintOperation>> wrap_mint_operation(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::NFTMintOperation, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::NFTMintOperation v(*o);
    auto op = std::make_shared<MintOperation>();
    op->mint_input.sig_indices = wire::sig_indices(v.SigIndices());
    op->group_id = v.GroupID();
    const auto p = v.Payload();
    op->payload.assign(p.begin(), p.end());
    ByteView blob = v.OwnersBytes();
    for (std::uint32_t i = 0; i < v.OwnersCount(); ++i) {
        auto split = wire::next_envelope(blob);
        if (!split) return std::unexpected(split.error());
        auto owner = fx::wrap_output_owners(split->envelope);
        if (!owner) return std::unexpected(owner.error());
        op->outputs.push_back(std::make_shared<OutputOwners>(*owner));
        blob = split->rest;
    }
    return op;
}

std::vector<std::shared_ptr<FxOutput>> TransferOperation::outs() const {
    return {std::make_shared<TransferOutput>(output)};
}

Result<void> TransferOperation::verify() const {
    if (auto r = input.verify(); !r) return r;
    return output.verify();
}

Bytes TransferOperation::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::NFTTransferOp,
        wire::NewNFTTransferOperation(
            wire::NFTTransferOperationInput{input.sig_indices, output.bytes()}));
}

Result<std::shared_ptr<TransferOperation>> wrap_transfer_operation(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::NFTTransferOp, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::NFTTransferOperation v(*o);
    auto out = wrap_transfer_output(v.OutputBytes());
    if (!out) return std::unexpected(out.error());
    auto op = std::make_shared<TransferOperation>();
    op->input.sig_indices = wire::sig_indices(v.SigIndices());
    op->output = **out;
    return op;
}

Bytes Credential::bytes() const {
    Bytes concat;
    concat.reserve(signatures.size() * kSignatureLen);
    for (const auto& s : signatures) concat.insert(concat.end(), s.begin(), s.end());
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::Credential,
        wire::NewCredential(wire::CredentialInput{0, concat, {}}));
}

Result<std::shared_ptr<Credential>> wrap_credential(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::Credential, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::Credential v(*o);
    auto c = std::make_shared<Credential>();
    int n = wire::signature_count(v, kSignatureLen);
    c->signatures.resize(std::size_t(n));
    for (int i = 0; i < n; ++i) {
        Bytes raw = wire::signature_at(v, i, kSignatureLen);
        std::copy(raw.begin(), raw.end(), c->signatures[std::size_t(i)].begin());
    }
    return c;
}

}  // namespace nftfx

// ================= propertyfx =================
namespace propertyfx {

Bytes MintOutput::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::MintOutput,
        wire::NewOutputOwners(wire::OutputOwnersInput{out_owners.locktime, out_owners.threshold,
                                                      out_owners.addrs}));
}

Result<std::shared_ptr<MintOutput>> wrap_mint_output(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::MintOutput, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::OutputOwners v(*o);
    auto out = std::make_shared<MintOutput>();
    out->out_owners = OutputOwners{v.Locktime(), v.Threshold(), wire::addresses(v)};
    return out;
}

Bytes OwnedOutput::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::OwnedOutput,
        wire::NewOutputOwners(wire::OutputOwnersInput{out_owners.locktime, out_owners.threshold,
                                                      out_owners.addrs}));
}

Result<std::shared_ptr<OwnedOutput>> wrap_owned_output(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::OwnedOutput, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::OutputOwners v(*o);
    auto out = std::make_shared<OwnedOutput>();
    out->out_owners = OutputOwners{v.Locktime(), v.Threshold(), wire::addresses(v)};
    return out;
}

std::vector<std::shared_ptr<FxOutput>> MintOperation::outs() const {
    return {std::make_shared<MintOutput>(mint_output), std::make_shared<OwnedOutput>(owned_output)};
}

Result<void> MintOperation::verify() const {
    if (auto r = mint_input.verify(); !r) return r;
    if (auto r = mint_output.verify(); !r) return r;
    return owned_output.verify();
}

Bytes MintOperation::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::MintOperation,
        wire::NewMintOperation(wire::MintOperationInput{
            mint_input.sig_indices, mint_output.bytes(), owned_output.bytes()}));
}

Result<std::shared_ptr<MintOperation>> wrap_mint_operation(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::MintOperation, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::MintOperation v(*o);
    auto mo = wrap_mint_output(v.MintOutput());
    if (!mo) return std::unexpected(mo.error());
    auto oo = wrap_owned_output(v.TransferOutput());
    if (!oo) return std::unexpected(oo.error());
    auto op = std::make_shared<MintOperation>();
    op->mint_input.sig_indices = wire::sig_indices(v.SigIndices());
    op->mint_output = **mo;
    op->owned_output = **oo;
    return op;
}

Bytes BurnOperation::bytes() const {
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::BurnOperation,
        wire::NewBurnOperation(wire::BurnOperationInput{input.sig_indices}));
}

Result<std::shared_ptr<BurnOperation>> wrap_burn_operation(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::BurnOperation, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::BurnOperation v(*o);
    auto op = std::make_shared<BurnOperation>();
    op->input.sig_indices = wire::sig_indices(v.SigIndices());
    return op;
}

Bytes Credential::bytes() const {
    Bytes concat;
    concat.reserve(signatures.size() * kSignatureLen);
    for (const auto& s : signatures) concat.insert(concat.end(), s.begin(), s.end());
    return wire::write_envelope_prefix(
        kTypeKind, wire::ShapeKind::Credential,
        wire::NewCredential(wire::CredentialInput{0, concat, {}}));
}

Result<std::shared_ptr<Credential>> wrap_credential(ByteView b) {
    auto o = wire::payload(b, wire::ShapeKind::Credential, kTypeKind);
    if (!o) return std::unexpected(o.error());
    const wire::Credential v(*o);
    auto c = std::make_shared<Credential>();
    int n = wire::signature_count(v, kSignatureLen);
    c->signatures.resize(std::size_t(n));
    for (int i = 0; i < n; ++i) {
        Bytes raw = wire::signature_at(v, i, kSignatureLen);
        std::copy(raw.begin(), raw.end(), c->signatures[std::size_t(i)].begin());
    }
    return c;
}

}  // namespace propertyfx

// ================= the shared spend gate =================

Result<void> verify_credentials(const Fx& fx, ByteView unsigned_bytes, const Input& in,
                                const std::vector<Signature>& sigs, const OutputOwners& out) {
    const std::size_t num_sigs = in.sig_indices.size();
    const std::uint64_t now = fx.clock ? fx.clock->unix() : 0;
    if (out.locktime > now) return std::unexpected(kErrTimelocked);
    if (out.threshold < std::uint32_t(num_sigs)) return std::unexpected(kErrTooManySigners);
    if (out.threshold > std::uint32_t(num_sigs)) return std::unexpected(kErrTooFewSigners);
    if (num_sigs != sigs.size()) return std::unexpected(kErrInputCredentialSignersMismatch);
    // Signature verification is disabled during bootstrapping: history is being
    // replayed, and it was verified when it was first accepted.
    if (!fx.is_bootstrapped()) return {};

    const Id tx_hash = lux::xvm::sha256(unsigned_bytes);
    for (std::size_t i = 0; i < num_sigs; ++i) {
        std::uint32_t index = in.sig_indices[i];
        if (index >= std::uint32_t(out.addrs.size()))
            return std::unexpected(kErrInputOutputIndexOutOfBounds);
        auto addr = recover_address(tx_hash, sigs[i]);
        if (!addr) return std::unexpected(addr.error());
        if (out.addrs[index] != *addr) return std::unexpected(kErrWrongSig);
    }
    return {};
}

namespace {

// verify_all runs a fixed list of Verify()s in order, the C++ shape of Go's
// verify.All — first failure wins, and the ORDER is part of the contract because
// tests assert on which error surfaces.
Result<void> verify_all(std::initializer_list<const FxValue*> vals) {
    for (const auto* v : vals) {
        if (v == nullptr) return std::unexpected(kErrNilOutput);
        if (auto r = v->verify(); !r) return r;
    }
    return {};
}

}  // namespace

// ---- Secp256k1Fx ----

Result<void> Secp256k1Fx::verify_transfer(ByteView unsigned_bytes, const FxValue* in,
                                          const FxValue* cred, const FxValue* utxo) const {
    const auto* tin = dynamic_cast<const secp256k1fx::TransferInput*>(in);
    if (tin == nullptr) return std::unexpected(kErrWrongInputType);
    const auto* c = dynamic_cast<const secp256k1fx::Credential*>(cred);
    if (c == nullptr) return std::unexpected(kErrWrongCredentialType);
    const auto* out = dynamic_cast<const secp256k1fx::TransferOutput*>(utxo);
    if (out == nullptr) return std::unexpected(kErrWrongUTXOType);

    if (auto r = verify_all({out, tin, c}); !r) return r;
    if (out->amt != tin->amt) return std::unexpected(kErrMismatchedAmounts);
    return verify_credentials(*this, unsigned_bytes, tin->input, c->signatures, out->out_owners);
}

Result<void> Secp256k1Fx::verify_permission(ByteView unsigned_bytes, const Input& in,
                                            const FxValue* cred,
                                            const OutputOwners& owner) const {
    const auto* c = dynamic_cast<const secp256k1fx::Credential*>(cred);
    if (c == nullptr) return std::unexpected(kErrWrongCredentialType);
    if (auto r = in.verify(); !r) return r;
    if (auto r = c->verify(); !r) return r;
    if (auto r = owner.verify(); !r) return r;
    return verify_credentials(*this, unsigned_bytes, in, c->signatures, owner);
}

Result<void> Secp256k1Fx::verify_operation(ByteView unsigned_bytes, const FxValue* op,
                                           const FxValue* cred,
                                           const std::vector<const FxValue*>& utxos) const {
    const auto* mop = dynamic_cast<const secp256k1fx::MintOperation*>(op);
    if (mop == nullptr) return std::unexpected(kErrWrongOpType);
    const auto* c = dynamic_cast<const secp256k1fx::Credential*>(cred);
    if (c == nullptr) return std::unexpected(kErrWrongCredentialType);
    if (utxos.size() != 1) return std::unexpected(kErrWrongNumberOfUTXOs);
    const auto* out = dynamic_cast<const secp256k1fx::MintOutput*>(utxos[0]);
    if (out == nullptr) return std::unexpected(kErrWrongUTXOType);

    if (auto r = verify_all({mop, c, out}); !r) return r;
    if (!out->out_owners.equals(mop->mint_output.out_owners))
        return std::unexpected(kErrWrongMintCreated);
    return verify_credentials(*this, unsigned_bytes, mop->mint_input, c->signatures,
                              out->out_owners);
}

// ---- NFTFx: cannot transfer; value moves through OPERATIONS only ----

Result<void> NFTFx::verify_transfer(ByteView, const FxValue*, const FxValue*,
                                    const FxValue*) const {
    return std::unexpected("cant transfer with this fx");
}

Result<void> NFTFx::verify_operation(ByteView unsigned_bytes, const FxValue* op,
                                     const FxValue* cred,
                                     const std::vector<const FxValue*>& utxos) const {
    if (utxos.size() != 1) return std::unexpected(kErrWrongNumberOfUTXOs);
    const auto* c = dynamic_cast<const nftfx::Credential*>(cred);
    if (c == nullptr) return std::unexpected(kErrWrongCredentialType);

    if (const auto* mop = dynamic_cast<const nftfx::MintOperation*>(op)) {
        const auto* out = dynamic_cast<const nftfx::MintOutput*>(utxos[0]);
        if (out == nullptr) return std::unexpected(kErrWrongUTXOType);
        if (auto r = verify_all({mop, c, out}); !r) return r;
        if (out->group_id != mop->group_id) return std::unexpected(kErrWrongUniqueID);
        return verify_credentials(*this, unsigned_bytes, mop->mint_input, c->signatures,
                                  out->out_owners);
    }
    if (const auto* top = dynamic_cast<const nftfx::TransferOperation*>(op)) {
        const auto* out = dynamic_cast<const nftfx::TransferOutput*>(utxos[0]);
        if (out == nullptr) return std::unexpected(kErrWrongUTXOType);
        if (auto r = verify_all({top, c, out}); !r) return r;
        if (out->group_id != top->output.group_id) return std::unexpected(kErrWrongUniqueID);
        if (out->payload != top->output.payload) return std::unexpected("wrong bytes provided");
        return verify_credentials(*this, unsigned_bytes, top->input, c->signatures,
                                  out->out_owners);
    }
    return std::unexpected(kErrWrongOpType);
}

// ---- PropertyFx ----

Result<void> PropertyFx::verify_transfer(ByteView, const FxValue*, const FxValue*,
                                         const FxValue*) const {
    return std::unexpected("cant transfer with this fx");
}

Result<void> PropertyFx::verify_operation(ByteView unsigned_bytes, const FxValue* op,
                                          const FxValue* cred,
                                          const std::vector<const FxValue*>& utxos) const {
    if (utxos.size() != 1) return std::unexpected(kErrWrongNumberOfUTXOs);
    const auto* c = dynamic_cast<const propertyfx::Credential*>(cred);
    if (c == nullptr) return std::unexpected(kErrWrongCredentialType);

    if (const auto* mop = dynamic_cast<const propertyfx::MintOperation*>(op)) {
        const auto* out = dynamic_cast<const propertyfx::MintOutput*>(utxos[0]);
        if (out == nullptr) return std::unexpected(kErrWrongUTXOType);
        if (auto r = verify_all({mop, c, out}); !r) return r;
        if (!out->out_owners.equals(mop->mint_output.out_owners))
            return std::unexpected("wrong mint output provided");
        return verify_credentials(*this, unsigned_bytes, mop->mint_input, c->signatures,
                                  out->out_owners);
    }
    if (const auto* bop = dynamic_cast<const propertyfx::BurnOperation*>(op)) {
        const auto* out = dynamic_cast<const propertyfx::OwnedOutput*>(utxos[0]);
        if (out == nullptr) return std::unexpected(kErrWrongUTXOType);
        if (auto r = verify_all({bop, c, out}); !r) return r;
        return verify_credentials(*this, unsigned_bytes, bop->input, c->signatures,
                                  out->out_owners);
    }
    return std::unexpected(kErrWrongOpType);
}

// ================= envelope dispatch =================

Result<std::shared_ptr<FxOutput>> wrap_output(ByteView envelope) {
    auto d = wire::peek_discriminator(envelope);
    if (!d) return std::unexpected(d.error());
    switch (d->type_kind) {
        case wire::TypeKind::Secp256k1:
            if (d->shape_kind == wire::ShapeKind::TransferOutput) {
                auto v = secp256k1fx::wrap_transfer_output(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOutput>(*v);
            }
            if (d->shape_kind == wire::ShapeKind::MintOutput) {
                auto v = secp256k1fx::wrap_mint_output(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOutput>(*v);
            }
            break;
        case wire::TypeKind::NFT:
            if (d->shape_kind == wire::ShapeKind::NFTMintOutput) {
                auto v = nftfx::wrap_mint_output(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOutput>(*v);
            }
            if (d->shape_kind == wire::ShapeKind::NFTTransferOutput) {
                auto v = nftfx::wrap_transfer_output(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOutput>(*v);
            }
            break;
        case wire::TypeKind::Property:
            if (d->shape_kind == wire::ShapeKind::MintOutput) {
                auto v = propertyfx::wrap_mint_output(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOutput>(*v);
            }
            if (d->shape_kind == wire::ShapeKind::OwnedOutput) {
                auto v = propertyfx::wrap_owned_output(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOutput>(*v);
            }
            break;
        default:
            break;
    }
    return std::unexpected("xvm fx: unknown fx output envelope");
}

Result<std::shared_ptr<FxTransferOut>> wrap_transfer_out(ByteView envelope) {
    auto d = wire::peek_discriminator(envelope);
    if (!d) return std::unexpected(d.error());
    if (d->type_kind == wire::TypeKind::Secp256k1 &&
        d->shape_kind == wire::ShapeKind::TransferOutput) {
        auto v = secp256k1fx::wrap_transfer_output(envelope);
        if (!v) return std::unexpected(v.error());
        return std::static_pointer_cast<FxTransferOut>(*v);
    }
    return std::unexpected("xvm fx: envelope is not a value-bearing transferable output");
}

Result<std::shared_ptr<FxTransferIn>> wrap_transfer_in(ByteView envelope) {
    auto d = wire::peek_discriminator(envelope);
    if (!d) return std::unexpected(d.error());
    if (d->type_kind == wire::TypeKind::Secp256k1 &&
        d->shape_kind == wire::ShapeKind::TransferInput) {
        auto v = secp256k1fx::wrap_transfer_input(envelope);
        if (!v) return std::unexpected(v.error());
        return std::static_pointer_cast<FxTransferIn>(*v);
    }
    return std::unexpected("xvm fx: envelope is not a transferable input");
}

Result<std::shared_ptr<FxOperation>> wrap_operation(ByteView envelope) {
    auto d = wire::peek_discriminator(envelope);
    if (!d) return std::unexpected(d.error());
    switch (d->type_kind) {
        case wire::TypeKind::Secp256k1:
            if (d->shape_kind == wire::ShapeKind::MintOperation) {
                auto v = secp256k1fx::wrap_mint_operation(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOperation>(*v);
            }
            break;
        case wire::TypeKind::NFT:
            if (d->shape_kind == wire::ShapeKind::NFTMintOperation) {
                auto v = nftfx::wrap_mint_operation(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOperation>(*v);
            }
            if (d->shape_kind == wire::ShapeKind::NFTTransferOp) {
                auto v = nftfx::wrap_transfer_operation(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOperation>(*v);
            }
            break;
        case wire::TypeKind::Property:
            if (d->shape_kind == wire::ShapeKind::MintOperation) {
                auto v = propertyfx::wrap_mint_operation(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOperation>(*v);
            }
            if (d->shape_kind == wire::ShapeKind::BurnOperation) {
                auto v = propertyfx::wrap_burn_operation(envelope);
                if (!v) return std::unexpected(v.error());
                return std::static_pointer_cast<FxOperation>(*v);
            }
            break;
        default:
            break;
    }
    return std::unexpected("xvm fx: unknown fx operation envelope");
}

Result<std::shared_ptr<FxCredential>> wrap_credential(ByteView envelope) {
    auto d = wire::peek_discriminator(envelope);
    if (!d) return std::unexpected(d.error());
    if (d->shape_kind != wire::ShapeKind::Credential)
        return std::unexpected("xvm fx: credential envelope has non-credential shape");
    switch (d->type_kind) {
        case wire::TypeKind::Secp256k1: {
            auto v = secp256k1fx::wrap_credential(envelope);
            if (!v) return std::unexpected(v.error());
            return std::static_pointer_cast<FxCredential>(*v);
        }
        case wire::TypeKind::NFT: {
            auto v = nftfx::wrap_credential(envelope);
            if (!v) return std::unexpected(v.error());
            return std::static_pointer_cast<FxCredential>(*v);
        }
        case wire::TypeKind::Property: {
            auto v = propertyfx::wrap_credential(envelope);
            if (!v) return std::unexpected(v.error());
            return std::static_pointer_cast<FxCredential>(*v);
        }
        default:
            break;
    }
    return std::unexpected("xvm fx: unknown fx credential family");
}

}  // namespace lux::xvm::fx
