// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/zkvm/txs.hpp"

namespace lux::zkvm {
namespace {

using wire::cp;
using wire::read_id;
using wire::read_u32_list;
using wire::write_u32_list;

// TransparentInput: TxID 32B@0, OutputIdx u32@32, Amount u64@36, Address bytes@44
constexpr int kTiTxId = 0, kTiIdx = 32, kTiAmount = 36, kTiAddress = 44;

// TransparentOutput: Amount u64@0, AssetID 32B@8, Address bytes@40
constexpr int kToAmount = 0, kToAsset = 8, kToAddress = 40;

// ShieldedOutput: four byte fields @0/8/16/24
constexpr int kSoCommit = 0, kSoNote = 8, kSoEpk = 16, kSoProof = 24;

// ZKProof: ProofType bytes@0, ProofData bytes@8, PubInputLens list@16,
//          PubInputBlob bytes@24
constexpr int kZkpType = 0, kZkpData = 8, kZkpPubLens = 16, kZkpPubBlob = 24;

// Transaction: Type u8@0, Version u8@1, Fee u64@2, Expiry u64@10,
//   TInLens list@18, TInBlob bytes@26, TOutLens list@34, TOutBlob bytes@42,
//   NullLens list@50, NullBlob bytes@58, SOutLens list@66, SOutBlob bytes@74,
//   Proof bytes@82, Memo bytes@90
constexpr int kTxType_ = 0, kTxVersion = 1, kTxFee = 2, kTxExpiry = 10;
constexpr int kTxTinLens = 18, kTxTinBlob = 26, kTxToutLens = 34, kTxToutBlob = 42;
constexpr int kTxNullLens = 50, kTxNullBlob = 58, kTxSoutLens = 66, kTxSoutBlob = 74;
constexpr int kTxProof = 82, kTxMemo = 90;

ByteView str_view(const std::string& s) {
    return ByteView(reinterpret_cast<const std::uint8_t*>(s.data()), s.size());
}

}  // namespace

// ================= identity =================

Id Transaction::compute_id() const {
    Hasher h;
    h.write_byte(static_cast<std::uint8_t>(type));
    h.write_byte(version);

    h.count(transparent_inputs.size());
    for (const auto& in : transparent_inputs) {
        h.write(view(in.tx_id));
        h.num(std::uint64_t(in.output_idx));
        h.num(in.amount);
        h.blob(view(in.address));
    }

    h.count(transparent_outputs.size());
    for (const auto& out : transparent_outputs) {
        h.num(out.amount);
        h.write(view(out.asset_id));
        h.blob(view(out.address));
    }

    h.count(nullifiers.size());
    for (const auto& n : nullifiers) h.blob(view(n));

    h.count(outputs.size());
    for (const auto& o : outputs) {
        h.blob(view(o.commitment));
        h.blob(view(o.encrypted_note));
        h.blob(view(o.ephemeral_pubkey));
        h.blob(view(o.output_proof));
    }

    h.present(proof.has_value());
    if (proof) {
        h.blob(str_view(proof->proof_type));
        h.blob(view(proof->proof_data));
        h.count(proof->public_inputs.size());
        for (const auto& pi : proof->public_inputs) h.blob(view(pi));
    }

    h.num(fee);
    h.num(expiry);
    h.blob(view(memo));

    return h.sum();
}

std::vector<Bytes> Transaction::output_commitments() const {
    std::vector<Bytes> out;
    out.reserve(outputs.size());
    for (const auto& o : outputs) out.push_back(o.commitment);
    return out;
}

wire::Result<void> Transaction::validate_basic() const {
    if (static_cast<std::uint8_t>(type) > static_cast<std::uint8_t>(TxType::Unshield))
        return std::unexpected(kErrInvalidTxType);

    if (nullifiers.empty() && transparent_inputs.empty())
        return std::unexpected(kErrNoInputs);
    if (outputs.empty() && transparent_outputs.empty())
        return std::unexpected(kErrNoOutputs);
    if (!proof) return std::unexpected(kErrMissingProof);

    // A transaction names the height it stops being valid at. Without one it
    // sits in a bounded pool forever: it can never enter a block, nothing
    // evicts it, and once the pool is full of them every honest arrival paying
    // the same floor is refused.
    if (expiry == 0) return std::unexpected(kErrNoExpiry);

    switch (type) {
        case TxType::Transfer:
            if (nullifiers.empty() || outputs.empty())
                return std::unexpected(kErrInvalidTransfer);
            break;
        case TxType::Shield:
            if (transparent_inputs.empty() || outputs.empty())
                return std::unexpected(kErrInvalidShield);
            break;
        case TxType::Unshield:
            if (nullifiers.empty() || transparent_outputs.empty())
                return std::unexpected(kErrInvalidUnshield);
            break;
        case TxType::Mint:
        case TxType::Burn:
            break;
    }
    return {};
}

// ================= sub-frames =================

Bytes marshal_transparent_input(const TransparentInput& t) {
    zap::Builder b(zap::kHeaderSize + kTiSize + int(t.address.size()) + 32);
    auto ob = b.start_object(kTiSize);
    ob.set_bytes_fixed(kTiTxId, view(t.tx_id));
    ob.set_u32(kTiIdx, t.output_idx);
    ob.set_u64(kTiAmount, t.amount);
    ob.set_bytes(kTiAddress, view(t.address));
    ob.finish_as_root();
    return b.finish();
}

wire::Result<TransparentInput> parse_transparent_input(ByteView data) {
    zap::Message msg;
    auto o = wire::parse_frame(data, &msg);
    if (!o) return std::unexpected(o.error());
    TransparentInput t;
    t.tx_id = read_id(*o, kTiTxId);
    t.output_idx = o->u32(kTiIdx);
    t.amount = o->u64(kTiAmount);
    t.address = cp(o->bytes(kTiAddress));
    return t;
}

Bytes marshal_transparent_output(const TransparentOutput& t) {
    zap::Builder b(zap::kHeaderSize + kToSize + int(t.address.size()) + 32);
    auto ob = b.start_object(kToSize);
    ob.set_u64(kToAmount, t.amount);
    ob.set_bytes_fixed(kToAsset, view(t.asset_id));
    ob.set_bytes(kToAddress, view(t.address));
    ob.finish_as_root();
    return b.finish();
}

wire::Result<TransparentOutput> parse_transparent_output(ByteView data) {
    zap::Message msg;
    auto o = wire::parse_frame(data, &msg);
    if (!o) return std::unexpected(o.error());
    TransparentOutput t;
    t.amount = o->u64(kToAmount);
    t.asset_id = read_id(*o, kToAsset);
    t.address = cp(o->bytes(kToAddress));
    return t;
}

Bytes marshal_shielded_output(const ShieldedOutput& s) {
    zap::Builder b(zap::kHeaderSize + kSoSize + int(s.commitment.size()) +
                   int(s.encrypted_note.size()) + int(s.ephemeral_pubkey.size()) +
                   int(s.output_proof.size()) + 64);
    auto ob = b.start_object(kSoSize);
    ob.set_bytes(kSoCommit, view(s.commitment));
    ob.set_bytes(kSoNote, view(s.encrypted_note));
    ob.set_bytes(kSoEpk, view(s.ephemeral_pubkey));
    ob.set_bytes(kSoProof, view(s.output_proof));
    ob.finish_as_root();
    return b.finish();
}

wire::Result<ShieldedOutput> parse_shielded_output(ByteView data) {
    zap::Message msg;
    auto o = wire::parse_frame(data, &msg);
    if (!o) return std::unexpected(o.error());
    ShieldedOutput s;
    s.commitment = cp(o->bytes(kSoCommit));
    s.encrypted_note = cp(o->bytes(kSoNote));
    s.ephemeral_pubkey = cp(o->bytes(kSoEpk));
    s.output_proof = cp(o->bytes(kSoProof));
    return s;
}

Bytes marshal_zkproof(const std::optional<ZkProof>& z) {
    if (!z) return {};
    std::vector<std::uint32_t> lens;
    Bytes blob;
    wire::pack_bytes_list(z->public_inputs, lens, blob);

    zap::Builder b(zap::kHeaderSize + kZkpSize + int(z->proof_type.size()) +
                   int(z->proof_data.size()) + int(blob.size()) + 4 * int(lens.size()) + 64);
    const int lens_off = write_u32_list(b, lens);
    auto ob = b.start_object(kZkpSize);
    ob.set_bytes(kZkpType, str_view(z->proof_type));
    ob.set_bytes(kZkpData, view(z->proof_data));
    ob.set_list(kZkpPubLens, lens_off, int(lens.size()));
    ob.set_bytes(kZkpPubBlob, view(blob));
    ob.finish_as_root();
    return b.finish();
}

wire::Result<std::optional<ZkProof>> parse_zkproof(ByteView data) {
    if (data.empty()) return std::optional<ZkProof>{};
    zap::Message msg;
    auto o = wire::parse_frame(data, &msg);
    if (!o) return std::unexpected(o.error());
    auto pub = wire::unpack_bytes_list(read_u32_list(*o, kZkpPubLens), o->bytes(kZkpPubBlob));
    if (!pub) return std::unexpected(pub.error());
    ZkProof z;
    const auto t = o->bytes(kZkpType);
    z.proof_type.assign(reinterpret_cast<const char*>(t.data()), t.size());
    z.proof_data = cp(o->bytes(kZkpData));
    z.public_inputs = std::move(*pub);
    return std::optional<ZkProof>(std::move(z));
}

// ================= the transaction =================

Bytes Transaction::marshal() const {
    std::vector<std::uint32_t> tin_lens, tout_lens, null_lens, sout_lens;
    Bytes tin_blob, tout_blob, null_blob, sout_blob;
    wire::pack_objs(transparent_inputs, marshal_transparent_input, tin_lens, tin_blob);
    wire::pack_objs(transparent_outputs, marshal_transparent_output, tout_lens, tout_blob);
    wire::pack_bytes_list(nullifiers, null_lens, null_blob);
    wire::pack_objs(outputs, marshal_shielded_output, sout_lens, sout_blob);
    const Bytes proof_bytes = marshal_zkproof(proof);

    zap::Builder b(zap::kHeaderSize + kTxSize + int(tin_blob.size()) + int(tout_blob.size()) +
                   int(null_blob.size()) + int(sout_blob.size()) + int(proof_bytes.size()) +
                   int(memo.size()) +
                   4 * int(tin_lens.size() + tout_lens.size() + null_lens.size() +
                           sout_lens.size()) +
                   512);
    const int tin_off = write_u32_list(b, tin_lens);
    const int tout_off = write_u32_list(b, tout_lens);
    const int null_off = write_u32_list(b, null_lens);
    const int sout_off = write_u32_list(b, sout_lens);

    auto ob = b.start_object(kTxSize);
    ob.set_u8(kTxType_, static_cast<std::uint8_t>(type));
    ob.set_u8(kTxVersion, version);
    ob.set_u64(kTxFee, fee);
    ob.set_u64(kTxExpiry, expiry);
    ob.set_list(kTxTinLens, tin_off, int(tin_lens.size()));
    ob.set_bytes(kTxTinBlob, view(tin_blob));
    ob.set_list(kTxToutLens, tout_off, int(tout_lens.size()));
    ob.set_bytes(kTxToutBlob, view(tout_blob));
    ob.set_list(kTxNullLens, null_off, int(null_lens.size()));
    ob.set_bytes(kTxNullBlob, view(null_blob));
    ob.set_list(kTxSoutLens, sout_off, int(sout_lens.size()));
    ob.set_bytes(kTxSoutBlob, view(sout_blob));
    ob.set_bytes(kTxProof, view(proof_bytes));
    ob.set_bytes(kTxMemo, view(memo));
    ob.finish_as_root();
    return b.finish();
}

wire::Result<Transaction> parse_transaction(ByteView data) {
    zap::Message msg;
    auto o = wire::parse_frame(data, &msg);
    if (!o) return std::unexpected(o.error());

    Transaction tx;
    tx.type = static_cast<TxType>(o->u8(kTxType_));
    tx.version = o->u8(kTxVersion);
    tx.fee = o->u64(kTxFee);
    tx.expiry = o->u64(kTxExpiry);
    tx.memo = cp(o->bytes(kTxMemo));

    auto tin = wire::unpack_objs<TransparentInput>(read_u32_list(*o, kTxTinLens),
                                                   o->bytes(kTxTinBlob), parse_transparent_input);
    if (!tin) return std::unexpected(tin.error());
    tx.transparent_inputs = std::move(*tin);

    auto tout = wire::unpack_objs<TransparentOutput>(
        read_u32_list(*o, kTxToutLens), o->bytes(kTxToutBlob), parse_transparent_output);
    if (!tout) return std::unexpected(tout.error());
    tx.transparent_outputs = std::move(*tout);

    auto nulls = wire::unpack_bytes_list(read_u32_list(*o, kTxNullLens), o->bytes(kTxNullBlob));
    if (!nulls) return std::unexpected(nulls.error());
    tx.nullifiers = std::move(*nulls);

    auto souts = wire::unpack_objs<ShieldedOutput>(read_u32_list(*o, kTxSoutLens),
                                                   o->bytes(kTxSoutBlob), parse_shielded_output);
    if (!souts) return std::unexpected(souts.error());
    tx.outputs = std::move(*souts);

    auto proof = parse_zkproof(o->bytes(kTxProof));
    if (!proof) return std::unexpected(proof.error());
    tx.proof = std::move(*proof);

    // The identity is derived, never read. See compute_id.
    tx.id = tx.compute_id();
    return tx;
}

}  // namespace lux::zkvm
