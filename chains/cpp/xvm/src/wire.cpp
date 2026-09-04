// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/wire.hpp"

#include <cstring>

namespace lux::xvm::wire {
namespace {

// read_envelope_prefix splits the 2-byte discriminator off a wire buffer and
// returns the ZAP slice behind it.
struct Prefix {
    TypeKind tk;
    ShapeKind sk;
    ByteView zap_bytes;
};

Result<Prefix> read_envelope_prefix(ByteView b) {
    if (b.size() < std::size_t(kEnvelopePrefix)) return std::unexpected(kErrShortEnvelope);
    return Prefix{TypeKind(b[0]), ShapeKind(b[1]), b.subspan(kEnvelopePrefix)};
}

// parse_shape is the one shape-check-then-parse sequence every Wrap* runs:
// the ShapeKind must match, a TypeKind of Reserved is refused where the shape is
// fx-owned, and only then is the ZAP body parsed.
Result<std::pair<TypeKind, zap::Message>> parse_shape(ByteView b, ShapeKind want,
                                                      bool reject_reserved_type) {
    auto pre = read_envelope_prefix(b);
    if (!pre) return std::unexpected(pre.error());
    if (pre->sk != want) return std::unexpected(kErrWrongShapeKind);
    if (reject_reserved_type && pre->tk == TypeKind::Reserved)
        return std::unexpected(kErrWrongTypeKind);
    zap::Message msg;
    std::string err;
    if (!zap::Message::parse(pre->zap_bytes, &msg, &err)) return std::unexpected(err);
    return std::make_pair(pre->tk, msg);
}

// write_address_list writes a stride-20 address list and returns
// (offset, ENTRY count) — add_bytes counts bytes, so the entry count is the
// caller's, exactly as in Go.
std::pair<int, int> write_address_list(zap::Builder& b, const std::vector<ShortId>& addrs) {
    if (addrs.empty()) return {0, 0};
    auto lb = b.start_list(int(kAddressStride));
    for (const auto& a : addrs) lb.add_bytes(view(a));
    auto [off, _] = lb.finish();
    return {off, int(addrs.size())};
}

std::pair<int, int> write_sig_indices(zap::Builder& b, const std::vector<std::uint32_t>& sigs) {
    if (sigs.empty()) return {0, 0};
    auto lb = b.start_list(int(kSigIndexStride));
    for (auto s : sigs) lb.add_u32(s);
    auto [off, _] = lb.finish();
    return {off, int(sigs.size())};
}

std::pair<int, int> write_byte_list(zap::Builder& b, const Bytes& data) {
    if (data.empty()) return {0, 0};
    auto lb = b.start_list(1);
    for (auto v : data) lb.add_u8(v);
    auto [off, _] = lb.finish();
    return {off, int(data.size())};
}

std::vector<std::uint32_t> read_sig_indices(const zap::Object& obj, int off) {
    auto l = obj.list_stride(off, kSigIndexStride);
    std::vector<std::uint32_t> out(static_cast<std::size_t>(l.len()));
    for (int i = 0; i < l.len(); ++i) out[std::size_t(i)] = l.u32(i);
    return out;
}

Id id_at(const zap::Object& obj, int off) {
    Id out{};
    auto s = obj.bytes_fixed_slice(off, 32);
    if (s.size() == 32) std::memcpy(out.data(), s.data(), 32);
    return out;
}

Result<void> owners_syntactic_verify(const AddressList& addrs, std::uint32_t threshold) {
    int n = addrs.len();
    if (n == 0) return std::unexpected(kErrOwnerAddrsEmpty);
    if (threshold == 0) return std::unexpected(kErrOwnerThresholdZero);
    if (std::uint64_t(threshold) > std::uint64_t(n))
        return std::unexpected(kErrOwnerThresholdExceedsAddrs);
    for (int i = 0; i < n; ++i) {
        if (addrs.at(i) == kEmptyShortId) return std::unexpected(kErrOwnerAddrZero);
    }
    return {};
}

}  // namespace

Result<Discriminator> peek_discriminator(ByteView b) {
    auto pre = read_envelope_prefix(b);
    if (!pre) return std::unexpected(pre.error());
    return Discriminator{pre->tk, pre->sk};
}

Result<Split> next_envelope(ByteView blob) {
    if (std::size_t(kEnvelopePrefix) > blob.size()) return std::unexpected(kErrShortEnvelope);
    std::size_t zap_start = kEnvelopePrefix;
    if (zap_start + std::size_t(zap::kHeaderSize) > blob.size())
        return std::unexpected(kErrShortEnvelope);
    int zap_size = int(zap::get_u32(blob.data() + zap_start + 12));
    std::size_t env_end = zap_start + std::size_t(zap_size);
    if (zap_size < zap::kHeaderSize || env_end > blob.size())
        return std::unexpected(kErrShortEnvelope);
    return Split{blob.subspan(0, env_end), blob.subspan(env_end)};
}

Bytes write_envelope_prefix(TypeKind tk, ShapeKind sk, const Bytes& zap_bytes) {
    Bytes out;
    out.reserve(std::size_t(kEnvelopePrefix) + zap_bytes.size());
    out.push_back(std::uint8_t(tk));
    out.push_back(std::uint8_t(sk));
    out.insert(out.end(), zap_bytes.begin(), zap_bytes.end());
    return out;
}

// ---- AddressList ----

ShortId AddressList::at(int i) const {
    ShortId out{};
    if (i < 0 || i >= list_.len()) return out;
    auto obj = list_.object(i, int(kAddressStride));
    for (int j = 0; j < int(kAddressStride); ++j) out[std::size_t(j)] = obj.u8(j);
    return out;
}

std::vector<ShortId> AddressList::all() const {
    std::vector<ShortId> out(static_cast<std::size_t>(list_.len()));
    for (int i = 0; i < list_.len(); ++i) out[std::size_t(i)] = at(i);
    return out;
}

// ---- OutputOwners ----

Result<void> OutputOwners::syntactic_verify() const {
    return owners_syntactic_verify(address_list(), threshold());
}

Bytes new_output_owners(const OutputOwnersInput& in) {
    zap::Builder b(512);
    auto [addr_off, addr_count] = write_address_list(b, in.addresses);
    auto ob = b.start_object(kSizeOutputOwners);
    ob.set_u64(kOffOwnersLocktime, in.locktime);
    ob.set_u32(kOffOwnersThreshold, in.threshold);
    ob.set_list(kOffOwnersAddressList, addr_off, addr_count);
    ob.finish_as_root();
    return write_envelope_prefix(TypeKind::Reserved, ShapeKind::OutputOwners, b.finish());
}

Result<OutputOwners> wrap_output_owners(ByteView b) {
    auto r = parse_shape(b, ShapeKind::OutputOwners, false);
    if (!r) return std::unexpected(r.error());
    return OutputOwners(r->second, r->second.root());
}

// ---- TransferOutput ----

Result<void> TransferOutput::syntactic_verify() const {
    return owners_syntactic_verify(address_list(), threshold());
}

Bytes new_transfer_output(const TransferOutputInput& in) {
    zap::Builder b(512);
    auto [addr_off, addr_count] = write_address_list(b, in.addresses);
    auto ob = b.start_object(kSizeTransferOutput);
    ob.set_u64(kOffTransferOutputAmount, in.amount);
    ob.set_u64(kOffTransferOutputLocktime, in.locktime);
    ob.set_u32(kOffTransferOutputThreshold, in.threshold);
    ob.set_list(kOffTransferOutputAddressList, addr_off, addr_count);
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::TransferOutput, b.finish());
}

Result<TransferOutput> wrap_transfer_output(ByteView b) {
    auto r = parse_shape(b, ShapeKind::TransferOutput, true);
    if (!r) return std::unexpected(r.error());
    return TransferOutput(r->first, r->second, r->second.root());
}

// ---- TransferInput ----

std::vector<std::uint32_t> TransferInput::sig_indices() const {
    return read_sig_indices(obj_, kOffTransferInputSigIndices);
}

Bytes new_transfer_input(const TransferInputInput& in) {
    zap::Builder b(512);
    auto [sig_off, sig_count] = write_sig_indices(b, in.sig_indices);
    auto ob = b.start_object(kSizeTransferInput);
    ob.set_u64(kOffTransferInputAmount, in.amount);
    ob.set_list(kOffTransferInputSigIndices, sig_off, sig_count);
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::TransferInput, b.finish());
}

Result<TransferInput> wrap_transfer_input(ByteView b) {
    auto r = parse_shape(b, ShapeKind::TransferInput, true);
    if (!r) return std::unexpected(r.error());
    return TransferInput(r->first, r->second, r->second.root());
}

// ---- MintOutput ----

Result<void> MintOutput::syntactic_verify() const {
    return owners_syntactic_verify(address_list(), threshold());
}

Bytes new_mint_output(const MintOutputInput& in) {
    zap::Builder b(512);
    auto [addr_off, addr_count] = write_address_list(b, in.addresses);
    auto ob = b.start_object(kSizeMintOutput);
    ob.set_u64(kOffOwnersLocktime, in.locktime);
    ob.set_u32(kOffOwnersThreshold, in.threshold);
    ob.set_list(kOffOwnersAddressList, addr_off, addr_count);
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::MintOutput, b.finish());
}

Result<MintOutput> wrap_mint_output(ByteView b) {
    auto r = parse_shape(b, ShapeKind::MintOutput, true);
    if (!r) return std::unexpected(r.error());
    return MintOutput(r->first, r->second, r->second.root());
}

// ---- MintOperation ----

std::vector<std::uint32_t> MintOperation::sig_indices() const {
    return read_sig_indices(obj_, kOffMintOpSigIndices);
}

Bytes new_mint_operation(const MintOperationInput& in) {
    zap::Builder b(512);
    auto [sig_off, sig_count] = write_sig_indices(b, in.sig_indices);
    auto ob = b.start_object(kSizeMintOperation);
    ob.set_list(kOffMintOpSigIndices, sig_off, sig_count);
    ob.set_bytes(kOffMintOpMintOutput, view(in.mint_output));
    ob.set_bytes(kOffMintOpTransferOutput, view(in.transfer_output));
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::MintOperation, b.finish());
}

Result<MintOperation> wrap_mint_operation(ByteView b) {
    auto r = parse_shape(b, ShapeKind::MintOperation, true);
    if (!r) return std::unexpected(r.error());
    return MintOperation(r->first, r->second, r->second.root());
}

// ---- Credential ----

Bytes Credential::signature_bytes() const {
    auto l = obj_.list_stride(kOffCredSignatureList, 1);
    int n = l.len();
    Bytes out(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) out[std::size_t(i)] = l.u8(i);
    return out;
}

Bytes Credential::pubkey_bytes() const {
    auto l = obj_.list_stride(kOffCredPubKeyList, 1);
    int n = l.len();
    Bytes out(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) out[std::size_t(i)] = l.u8(i);
    return out;
}

int Credential::signature_count(int sig_size) const {
    if (sig_size <= 0) return 0;
    int total = obj_.list_stride(kOffCredSignatureList, 1).len();
    if (total % sig_size != 0) return 0;
    return total / sig_size;
}

Bytes Credential::signature_at(int i, int sig_size) const {
    if (sig_size <= 0 || i < 0) return {};
    Bytes all = signature_bytes();
    std::size_t start = std::size_t(i) * std::size_t(sig_size);
    std::size_t end = start + std::size_t(sig_size);
    if (end > all.size()) return {};
    return Bytes(all.begin() + std::ptrdiff_t(start), all.begin() + std::ptrdiff_t(end));
}

Bytes new_credential(const CredentialInput& in) {
    zap::Builder b(512);
    auto [sigs_off, sigs_count] = write_byte_list(b, in.signatures);
    auto [pk_off, pk_count] = write_byte_list(b, in.pubkeys);
    auto ob = b.start_object(kSizeCredential);
    ob.set_u8(kOffCredSecurityLevel, in.security_level);
    ob.set_list(kOffCredSignatureList, sigs_off, sigs_count);
    ob.set_list(kOffCredPubKeyList, pk_off, pk_count);
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::Credential, b.finish());
}

Result<Credential> wrap_credential(ByteView b) {
    auto r = parse_shape(b, ShapeKind::Credential, true);
    if (!r) return std::unexpected(r.error());
    return Credential(r->first, r->second, r->second.root());
}

// ---- UTXO ----

Id UTXOWire::tx_id() const { return id_at(obj_, kOffUTXOTxID); }
Id UTXOWire::asset_id() const { return id_at(obj_, kOffUTXOAssetID); }

Bytes new_utxo(const UTXOInput& in) {
    zap::Builder b(512);
    auto ob = b.start_object(kSizeUTXO);
    ob.set_bytes_fixed(kOffUTXOTxID, view(in.tx_id));
    ob.set_u32(kOffUTXOOutputIndex, in.output_index);
    ob.set_bytes_fixed(kOffUTXOAssetID, view(in.asset_id));
    ob.set_bytes(kOffUTXOOutput, view(in.output));
    ob.finish_as_root();
    return write_envelope_prefix(TypeKind::Reserved, ShapeKind::UTXO, b.finish());
}

Result<UTXOWire> wrap_utxo(ByteView b) {
    auto r = parse_shape(b, ShapeKind::UTXO, false);
    if (!r) return std::unexpected(r.error());
    return UTXOWire(b, r->second, r->second.root());
}

// ---- TransferableOut / TransferableIn ----

Id TransferableOut::asset_id() const { return id_at(obj_, kOffTransferableOutAssetID); }
Id TransferableIn::tx_id() const { return id_at(obj_, kOffTransferableInTxID); }
Id TransferableIn::asset_id() const { return id_at(obj_, kOffTransferableInAssetID); }

int append_transferable_out(zap::Builder& b, const Id& asset_id, ByteView inner_envelope) {
    auto ob = b.start_object(kSizeTransferableOut);
    ob.set_bytes_fixed(kOffTransferableOutAssetID, view(asset_id));
    ob.set_bytes(kOffTransferableOutOutput, inner_envelope);
    return ob.finish();
}

int append_transferable_in(zap::Builder& b, const Id& tx_id, std::uint32_t output_index,
                           const Id& asset_id, ByteView inner_envelope) {
    auto ob = b.start_object(kSizeTransferableIn);
    ob.set_bytes_fixed(kOffTransferableInTxID, view(tx_id));
    ob.set_u32(kOffTransferableInOutputIndex, output_index);
    ob.set_bytes_fixed(kOffTransferableInAssetID, view(asset_id));
    ob.set_bytes(kOffTransferableInInput, inner_envelope);
    return ob.finish();
}

// ---- XVMBaseTx ----

Id XVMBaseTx::blockchain_id() const { return id_at(obj_, kOffXVMBaseTxBlockchainID); }

Result<TransferableOut> XVMBaseTx::out_at(std::uint32_t i) const {
    auto l = obj_.list_stride(kOffXVMBaseTxOuts, kObjPtrStride);
    if (int(i) >= l.len()) return std::unexpected(kErrShortEnvelope);
    auto o = l.object_ptr(int(i));
    if (o.is_null()) return std::unexpected(kErrShortEnvelope);
    return TransferableOut(o);
}

Result<TransferableIn> XVMBaseTx::in_at(std::uint32_t i) const {
    auto l = obj_.list_stride(kOffXVMBaseTxIns, kObjPtrStride);
    if (int(i) >= l.len()) return std::unexpected(kErrShortEnvelope);
    auto o = l.object_ptr(int(i));
    if (o.is_null()) return std::unexpected(kErrShortEnvelope);
    return TransferableIn(o);
}

Bytes new_xvm_base_tx(const XVMBaseTxInput& in) {
    zap::Builder b(512);

    // 1. tail each Transferable object; collect absolute offsets.
    std::vector<int> out_offs(in.outs.size());
    for (std::size_t i = 0; i < in.outs.size(); ++i)
        out_offs[i] = append_transferable_out(b, in.outs[i].asset_id, view(in.outs[i].output));
    std::vector<int> in_offs(in.ins.size());
    for (std::size_t i = 0; i < in.ins.size(); ++i)
        in_offs[i] = append_transferable_in(b, in.ins[i].tx_id, in.ins[i].output_index,
                                            in.ins[i].asset_id, view(in.ins[i].input));

    // 2. object-ptr lists over those offsets.
    auto ol = b.start_list(int(kObjPtrStride));
    for (int off : out_offs) ol.add_object_ptr(off);
    auto [outs_off, outs_len] = ol.finish();
    auto il = b.start_list(int(kObjPtrStride));
    for (int off : in_offs) il.add_object_ptr(off);
    auto [ins_off, ins_len] = il.finish();

    // 3. root object.
    auto ob = b.start_object(kSizeXVMBaseTx);
    ob.set_u32(kOffXVMBaseTxNetworkID, in.network_id);
    ob.set_bytes_fixed(kOffXVMBaseTxBlockchainID, view(in.blockchain_id));
    ob.set_list(kOffXVMBaseTxOuts, outs_off, outs_len);
    ob.set_list(kOffXVMBaseTxIns, ins_off, ins_len);
    ob.set_bytes(kOffXVMBaseTxMemo, view(in.memo));
    ob.finish_as_root();
    return write_envelope_prefix(TypeKind::Reserved, ShapeKind::XVMBaseTx, b.finish());
}

Result<XVMBaseTx> wrap_xvm_base_tx(ByteView b) {
    auto r = parse_shape(b, ShapeKind::XVMBaseTx, false);
    if (!r) return std::unexpected(r.error());
    return XVMBaseTx(b, r->second, r->second.root());
}

// ---- SignedTx ----

Result<Credential> SignedTx::credential_at(std::uint32_t i) const {
    std::uint32_t count = credential_count();
    if (i >= count) return std::unexpected(kErrWrongShapeKind);
    ByteView blob = credential_bytes();
    for (std::uint32_t k = 0; k <= i; ++k) {
        auto split = next_envelope(blob);
        if (!split) return std::unexpected(split.error());
        if (k == i) return wrap_credential(split->envelope);
        blob = split->rest;
    }
    return std::unexpected(kErrWrongShapeKind);
}

Result<std::vector<Credential>> SignedTx::all_credentials() const {
    std::uint32_t count = credential_count();
    std::vector<Credential> out;
    out.reserve(count);
    ByteView blob = credential_bytes();
    for (std::uint32_t k = 0; k < count; ++k) {
        auto split = next_envelope(blob);
        if (!split) return std::unexpected(split.error());
        auto c = wrap_credential(split->envelope);
        if (!c) return std::unexpected(c.error());
        out.push_back(*c);
        blob = split->rest;
    }
    return out;
}

Bytes new_signed_tx(const SignedTxInput& in) {
    Bytes cred_blob;
    std::size_t total = 0;
    for (const auto& c : in.credentials) total += c.size();
    cred_blob.reserve(total);
    for (const auto& c : in.credentials) cred_blob.insert(cred_blob.end(), c.begin(), c.end());

    zap::Builder b(512);
    auto ob = b.start_object(kSizeSignedTx);
    ob.set_bytes(kOffSignedTxUnsignedBytes, view(in.unsigned_bytes));
    ob.set_u32(kOffSignedTxCredentialCount, std::uint32_t(in.credentials.size()));
    ob.set_bytes(kOffSignedTxCredentialBytes, view(cred_blob));
    ob.finish_as_root();
    return write_envelope_prefix(TypeKind::Reserved, ShapeKind::SignedTx, b.finish());
}

Result<SignedTx> wrap_signed_tx(ByteView b) {
    auto r = parse_shape(b, ShapeKind::SignedTx, false);
    if (!r) return std::unexpected(r.error());
    return SignedTx(r->second, r->second.root());
}

// ---- nftfx / propertyfx composites ----

Bytes new_nft_mint_output(const NFTMintOutputInput& in) {
    zap::Builder b(512);
    auto [addr_off, addr_count] = write_address_list(b, in.addresses);
    auto ob = b.start_object(kSizeNFTMintOutput);
    ob.set_u32(kOffNFTMintOutputGroupID, in.group_id);
    ob.set_u64(kOffNFTMintOutputLocktime, in.locktime);
    ob.set_u32(kOffNFTMintOutputThreshold, in.threshold);
    ob.set_list(kOffNFTMintOutputAddressList, addr_off, addr_count);
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::NFTMintOutput, b.finish());
}

Result<NFTMintOutput> wrap_nft_mint_output(ByteView b) {
    auto r = parse_shape(b, ShapeKind::NFTMintOutput, true);
    if (!r) return std::unexpected(r.error());
    return NFTMintOutput(r->first, r->second, r->second.root());
}

Bytes new_nft_transfer_output(const NFTTransferOutputInput& in) {
    zap::Builder b(512);
    auto [addr_off, addr_count] = write_address_list(b, in.addresses);
    auto ob = b.start_object(kSizeNFTTransferOutput);
    ob.set_u32(kOffNFTTransferOutputGroupID, in.group_id);
    ob.set_u64(kOffNFTTransferOutputLocktime, in.locktime);
    ob.set_u32(kOffNFTTransferOutputThreshold, in.threshold);
    ob.set_list(kOffNFTTransferOutputAddressList, addr_off, addr_count);
    ob.set_bytes(kOffNFTTransferOutputPayload, view(in.payload));
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::NFTTransferOutput, b.finish());
}

Result<NFTTransferOutput> wrap_nft_transfer_output(ByteView b) {
    auto r = parse_shape(b, ShapeKind::NFTTransferOutput, true);
    if (!r) return std::unexpected(r.error());
    return NFTTransferOutput(r->first, r->second, r->second.root());
}

std::vector<std::uint32_t> NFTMintOperation::sig_indices() const {
    return read_sig_indices(obj_, kOffNFTMintOpSigIndices);
}

Bytes new_nft_mint_operation(const NFTMintOperationInput& in) {
    zap::Builder b(512);
    auto [sig_off, sig_count] = write_sig_indices(b, in.sig_indices);

    Bytes owners_blob;
    for (const auto& o : in.owners) owners_blob.insert(owners_blob.end(), o.begin(), o.end());

    auto ob = b.start_object(kSizeNFTMintOperation);
    ob.set_list(kOffNFTMintOpSigIndices, sig_off, sig_count);
    ob.set_u32(kOffNFTMintOpGroupID, in.group_id);
    ob.set_bytes(kOffNFTMintOpPayload, view(in.payload));
    ob.set_u32(kOffNFTMintOpOwnersCount, std::uint32_t(in.owners.size()));
    ob.set_bytes(kOffNFTMintOpOwnersBytes, view(owners_blob));
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::NFTMintOperation, b.finish());
}

Result<NFTMintOperation> wrap_nft_mint_operation(ByteView b) {
    auto r = parse_shape(b, ShapeKind::NFTMintOperation, true);
    if (!r) return std::unexpected(r.error());
    return NFTMintOperation(r->first, r->second, r->second.root());
}

std::vector<std::uint32_t> NFTTransferOperation::sig_indices() const {
    return read_sig_indices(obj_, kOffNFTTransferOpSigIndices);
}

Bytes new_nft_transfer_operation(const NFTTransferOperationInput& in) {
    zap::Builder b(512);
    auto [sig_off, sig_count] = write_sig_indices(b, in.sig_indices);
    auto ob = b.start_object(kSizeNFTTransferOp);
    ob.set_list(kOffNFTTransferOpSigIndices, sig_off, sig_count);
    ob.set_bytes(kOffNFTTransferOpOutputBytes, view(in.output));
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::NFTTransferOp, b.finish());
}

Result<NFTTransferOperation> wrap_nft_transfer_operation(ByteView b) {
    auto r = parse_shape(b, ShapeKind::NFTTransferOp, true);
    if (!r) return std::unexpected(r.error());
    return NFTTransferOperation(r->first, r->second, r->second.root());
}

Bytes new_owned_output(const OwnedOutputInput& in) {
    zap::Builder b(512);
    auto [addr_off, addr_count] = write_address_list(b, in.addresses);
    auto ob = b.start_object(kSizeOwnedOutput);
    ob.set_u64(kOffOwnersLocktime, in.locktime);
    ob.set_u32(kOffOwnersThreshold, in.threshold);
    ob.set_list(kOffOwnersAddressList, addr_off, addr_count);
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::OwnedOutput, b.finish());
}

Result<OwnedOutput> wrap_owned_output(ByteView b) {
    auto r = parse_shape(b, ShapeKind::OwnedOutput, true);
    if (!r) return std::unexpected(r.error());
    return OwnedOutput(r->first, r->second, r->second.root());
}

std::vector<std::uint32_t> BurnOperation::sig_indices() const {
    return read_sig_indices(obj_, kOffBurnOpSigIndices);
}

Bytes new_burn_operation(const BurnOperationInput& in) {
    zap::Builder b(512);
    auto [sig_off, sig_count] = write_sig_indices(b, in.sig_indices);
    auto ob = b.start_object(kSizeBurnOperation);
    ob.set_list(kOffBurnOpSigIndices, sig_off, sig_count);
    ob.finish_as_root();
    return write_envelope_prefix(in.type_kind, ShapeKind::BurnOperation, b.finish());
}

Result<BurnOperation> wrap_burn_operation(ByteView b) {
    auto r = parse_shape(b, ShapeKind::BurnOperation, true);
    if (!r) return std::unexpected(r.error());
    return BurnOperation(r->first, r->second, r->second.root());
}

}  // namespace lux::xvm::wire
