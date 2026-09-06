// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// txs_parse.cpp — the signed transaction: dispatch, credentials, identity.
//
// Rendered from Go vms/platformvm/txs/parse.go, kind.go and tx.go.
//
// A signed transaction is `unsigned ‖ credentials`. Both halves are
// self-delimiting ZAP messages, so the split point is the size field of the
// first — no length prefix, no framing of our own. That is what makes the
// unsigned bytes a genuine byte-PREFIX of the signed bytes: the bytes a signer
// signs are literally the leading bytes of what ships, so there is no second
// encoding step in which the two could differ.
//
// The identity of a transaction is sha256 of the signed bytes exactly as
// received. Nothing is re-encoded to be hashed, which removes the entire class
// of "two spellings, two ids, one effect" bugs.
//
// The credential buffer holds a list of {sigStart, sigCount} entries slicing
// into ONE shared array of 65-byte signature blobs. A claimed range that is not
// inside the array it names is a refusal, not a clamp: a credential that says
// it holds signatures it does not hold has already lied.

#include "lux/platformvm/txs.hpp"

#include "lux/platformvm/sha256.hpp"
#include "lux/platformvm/txs_wire.hpp"

#include <cstring>

namespace lux::platformvm::txs {
namespace {

// credential wire: object{ credsList ptr @0, sigArray ptr @8 } (size 16).
constexpr std::int64_t kOffCredsList = 0;
constexpr std::int64_t kOffSigArray = 8;
constexpr std::int64_t kCredsObjSize = 16;
constexpr std::int64_t kCredEntry = 8;

Result<std::shared_ptr<UnsignedTx>> parse_unsigned(std::span<const std::uint8_t> buf) {
    const auto m = zap::Message::parse(buf);
    if (!m) return fail(Err::BufferTooSmall);
    auto owned = std::make_shared<const std::vector<std::uint8_t>>(buf.begin(), buf.end());
    switch (static_cast<Kind>(m->root().u8(kOffKind))) {
        case Kind::RewardValidator: return RewardValidatorTx::wrap(owned);
        case Kind::Base: return BaseTxUnsigned::wrap(owned);
        case Kind::Import: return ImportTx::wrap(owned);
        case Kind::Export: return ExportTx::wrap(owned);
        case Kind::CreateNetwork: return CreateNetworkTx::wrap(owned);
        case Kind::CreateChain: return CreateChainTx::wrap(owned);
        case Kind::TransferChainOwnership: return TransferChainOwnershipTx::wrap(owned);
        case Kind::RemoveChainValidator: return RemoveChainValidatorTx::wrap(owned);
        case Kind::TransformChain: return TransformChainTx::wrap(owned);
        case Kind::AddValidator: return AddValidatorTx::wrap(owned);
        case Kind::AddChainValidator: return AddChainValidatorTx::wrap(owned);
        case Kind::AddDelegator: return AddDelegatorTx::wrap(owned);
        case Kind::AddPermissionlessValidator: return AddPermissionlessValidatorTx::wrap(owned);
        case Kind::AddPermissionlessDelegator: return AddPermissionlessDelegatorTx::wrap(owned);
        case Kind::RegisterL1Validator: return RegisterL1ValidatorTx::wrap(owned);
        case Kind::SetL1ValidatorWeight: return SetL1ValidatorWeightTx::wrap(owned);
        case Kind::IncreaseL1ValidatorBalance: return IncreaseL1ValidatorBalanceTx::wrap(owned);
        case Kind::DisableL1Validator: return DisableL1ValidatorTx::wrap(owned);
        case Kind::ConvertNetwork: return ConvertNetworkTx::wrap(owned);
    }
    return fail(Err::UnknownTxKind);
}

}  // namespace

Result<std::vector<std::uint8_t>> write_creds(const std::vector<Credential>& creds) {
    zap::Builder b(zap::kHeaderSize + 128 + creds.size() * kCredEntry);
    std::vector<std::array<std::uint8_t, kSigLen>> blobs;
    auto clb = b.start_list(kCredEntry);
    std::uint32_t cursor = 0;
    for (const auto& cred : creds) {
        std::uint8_t e[kCredEntry] = {};
        zap::store_u32(e, cursor);
        zap::store_u32(e + 4, static_cast<std::uint32_t>(cred.sigs.size()));
        clb.add_bytes({e, kCredEntry});
        blobs.insert(blobs.end(), cred.sigs.begin(), cred.sigs.end());
        cursor += static_cast<std::uint32_t>(cred.sigs.size());
    }
    // add_bytes counts BYTES; the wire length is the element count.
    const auto [clb_off, _] = clb.finish();
    const std::int64_t creds_off = clb_off;
    const std::int64_t creds_count = static_cast<std::int64_t>(creds.size());

    std::int64_t sig_off = 0, sig_count = 0;
    if (!blobs.empty()) {
        auto slb = b.start_list(static_cast<std::int64_t>(kSigLen));
        for (const auto& s : blobs) slb.add_bytes({s.data(), s.size()});
        const auto [slb_off, _] = slb.finish();
        sig_off = slb_off;
        sig_count = static_cast<std::int64_t>(blobs.size());
    }

    auto ob = b.start_object(kCredsObjSize);
    ob.set_list(kOffCredsList, creds_off, creds_count);
    ob.set_list(kOffSigArray, sig_off, sig_count);
    ob.finish_as_root();
    return b.finish();
}

Result<std::vector<Credential>> parse_creds(std::span<const std::uint8_t> b) {
    const auto m = zap::Message::parse(b);
    if (!m) return fail(Err::BufferTooSmall);
    const auto obj = m->root();
    const auto creds_list = obj.list_stride(kOffCredsList, kCredEntry);
    const auto sig_arr = obj.list_stride(kOffSigArray, static_cast<std::uint32_t>(kSigLen));
    const int n = creds_list.size();
    const std::uint32_t total = static_cast<std::uint32_t>(sig_arr.size());
    std::vector<Credential> creds(static_cast<std::size_t>(n));
    for (int i = 0; i < n; ++i) {
        const auto e = creds_list.object(i, kCredEntry);
        const std::uint32_t start = e.u32(0);
        const std::uint32_t count = e.u32(4);
        // Both bounds come off the wire. start <= total makes total-start safe;
        // an out-of-range claim is refused rather than silently truncated.
        if (start > total || count > total - start) return fail(Err::CredSigsOutOfRange);
        auto& c = creds[static_cast<std::size_t>(i)];
        c.sigs.resize(count);
        for (std::uint32_t j = 0; j < count; ++j) {
            const auto blob = sig_arr.object(static_cast<int>(start + j), static_cast<std::int64_t>(kSigLen));
            for (std::size_t k = 0; k < kSigLen; ++k) c.sigs[j][k] = blob.u8(static_cast<std::int64_t>(k));
        }
    }
    return creds;
}

void Tx::set_bytes(std::vector<std::uint8_t> signed_bytes) {
    bytes = std::move(signed_bytes);
    tx_id = id_from_hash(sha256(bytes));
}

Status Tx::initialize() {
    if (!unsigned_tx) return fail(Err::NilTx);
    const auto u = unsigned_tx->bytes();
    std::vector<std::uint8_t> signed_bytes(u.begin(), u.end());
    if (!creds.empty()) {
        const auto cb = write_creds(creds);
        if (!cb) return std::unexpected(cb.error());
        signed_bytes.insert(signed_bytes.end(), cb->begin(), cb->end());
    }
    set_bytes(std::move(signed_bytes));
    return ok();
}

std::vector<UTXO> Tx::utxos() const {
    std::vector<UTXO> out;
    if (!unsigned_tx) return out;
    const auto outs = unsigned_tx->outputs();
    out.reserve(outs.size());
    for (std::size_t i = 0; i < outs.size(); ++i) {
        out.push_back(UTXO{UtxoId{tx_id, static_cast<std::uint32_t>(i)}, outs[i].asset, outs[i].stake_lock,
                           outs[i].out});
    }
    return out;
}

std::vector<Id> Tx::input_ids() const {
    if (!unsigned_tx) return {};
    return unsigned_tx->input_ids();
}

Status Tx::syntactic_verify(const Runtime& rt) const {
    if (!unsigned_tx) return fail(Err::NilSignedTx);
    if (tx_id == kEmptyId) return fail(Err::SignedTxNotInitialized);
    return unsigned_tx->syntactic_verify(rt);
}

Result<Tx> parse(std::span<const std::uint8_t> signed_bytes) {
    const auto n = zap::message_length(signed_bytes);
    if (!n) return fail(Err::BufferTooSmall);
    auto u = parse_unsigned(signed_bytes.first(*n));
    if (!u) return std::unexpected(u.error());
    Tx tx;
    tx.unsigned_tx = *u;
    tx.bytes.assign(signed_bytes.begin(), signed_bytes.end());
    tx.tx_id = id_from_hash(sha256(signed_bytes));
    if (signed_bytes.size() > *n) {
        auto c = parse_creds(signed_bytes.subspan(*n));
        if (!c) return std::unexpected(c.error());
        tx.creds = std::move(*c);
    }
    return tx;
}

}  // namespace lux::platformvm::txs
