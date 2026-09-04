// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// components.cpp — the canonical bytes an output orders by, and the two
// orderings a transaction must satisfy.
//
// The fx wire envelope is (TypeKind, ShapeKind, ZAP message): one byte naming
// the feature-extension family that owns the primitive, one byte naming the
// shape within it, then the fields. Rendered from github.com/luxfi/utxo/wire
// (discriminator.go, transfer_output.go, locked_output.go) and
// utxo/secp256k1fx/wire.go.
//
// These bytes are the SORT KEY for outputs, so they are consensus: two
// implementations that build them differently disagree about which
// transactions are well-formed. That is why the envelope is built here rather
// than approximated by comparing fields — the fields do not order the same way
// the bytes do once an address list is involved.

#include "lux/platformvm/components.hpp"

#include "lux/platformvm/zap.hpp"

#include <algorithm>
#include <cstring>

namespace lux::platformvm {
namespace {

// The discriminator pair every fx primitive's envelope begins with.
constexpr std::uint8_t kTypeKindReserved = 0x00;
constexpr std::uint8_t kTypeKindSecp256k1 = 0x01;
constexpr std::uint8_t kShapeKindTransferOutput = 0x01;
constexpr std::uint8_t kShapeKindLockedOutput = 0x0F;

// wire.TransferOutput fixed section.
constexpr std::int64_t kTOAmount = 0;
constexpr std::int64_t kTOLocktime = 8;
constexpr std::int64_t kTOThreshold = 16;
constexpr std::int64_t kTOAddressList = 20;
constexpr std::int64_t kTOSize = 28;

// wire.LockedOutput fixed section.
constexpr std::int64_t kLOLocktime = 0;
constexpr std::int64_t kLOTransferOutBytes = 8;
constexpr std::int64_t kLOSize = 16;

constexpr std::int64_t kAddressStride = static_cast<std::int64_t>(kShortIdLen);

std::vector<std::uint8_t> with_prefix(std::uint8_t tk, std::uint8_t sk, const std::vector<std::uint8_t>& body) {
    std::vector<std::uint8_t> out;
    out.reserve(2 + body.size());
    out.push_back(tk);
    out.push_back(sk);
    out.insert(out.end(), body.begin(), body.end());
    return out;
}

std::vector<std::uint8_t> secp_transfer_output_envelope(const TransferOutput& o) {
    zap::Builder b(512);
    std::int64_t addr_off = 0;
    std::int64_t addr_count = 0;
    if (!o.owners.addrs.empty()) {
        auto lb = b.start_list(kAddressStride);
        for (const auto& a : o.owners.addrs) lb.add_bytes(a.span());
        addr_off = lb.offset();
        addr_count = static_cast<std::int64_t>(o.owners.addrs.size());
    }
    auto ob = b.start_object(kTOSize);
    ob.set_u64(kTOAmount, o.amt);
    ob.set_u64(kTOLocktime, o.owners.locktime);
    ob.set_u32(kTOThreshold, o.owners.threshold);
    ob.set_list(kTOAddressList, addr_off, addr_count);
    ob.finish_as_root();
    return with_prefix(kTypeKindSecp256k1, kShapeKindTransferOutput, b.finish());
}

std::vector<std::uint8_t> locked_output_envelope(std::uint64_t locktime,
                                                 const std::vector<std::uint8_t>& inner) {
    zap::Builder b(512);
    auto ob = b.start_object(kLOSize);
    ob.set_u64(kLOLocktime, locktime);
    ob.set_bytes(kLOTransferOutBytes, {inner.data(), inner.size()});
    ob.finish_as_root();
    return with_prefix(kTypeKindReserved, kShapeKindLockedOutput, b.finish());
}

// wire.UTXO fixed section.
constexpr std::uint8_t kShapeKindUTXO = 0x0A;
constexpr std::int64_t kUTxID = 0;
constexpr std::int64_t kUOutputIndex = 32;
constexpr std::int64_t kUAssetID = 36;
constexpr std::int64_t kUOutput = 68;
constexpr std::int64_t kUSize = 76;

int bytes_compare(const std::vector<std::uint8_t>& a, const std::vector<std::uint8_t>& b) {
    const std::size_t n = std::min(a.size(), b.size());
    if (n > 0) {
        const int r = std::memcmp(a.data(), b.data(), n);
        if (r != 0) return r < 0 ? -1 : 1;
    }
    if (a.size() == b.size()) return 0;
    return a.size() < b.size() ? -1 : 1;
}

}  // namespace

std::vector<std::uint8_t> TransferableOutput::wire_bytes() const {
    auto inner = secp_transfer_output_envelope(out);
    if (stake_lock == 0) return inner;
    return locked_output_envelope(stake_lock, inner);
}

// The inverse of wire_bytes: peel the discriminator, unwrap a lock if there is
// one, and read the secp256k1 transfer output inside. Any other pair is a
// refusal — a UTXO this chain cannot spend is not a UTXO it should pretend to
// hold.
Result<TransferableOutput> TransferableOutput::from_wire_bytes(std::span<const std::uint8_t> b,
                                                               const Id& asset) {
    if (b.size() < 2) return fail(Err::BufferTooSmall, "an fx envelope is at least two bytes");
    const std::uint8_t tk = b[0];
    const std::uint8_t sk = b[1];
    const auto body = b.subspan(2);

    if (tk == kTypeKindReserved && sk == kShapeKindLockedOutput) {
        const auto m = zap::Message::parse(body);
        if (!m) return fail(Err::BufferTooSmall, "the locked-output envelope is malformed");
        const auto root = m->root();
        const std::uint64_t locktime = root.u64(kLOLocktime);
        auto inner = TransferableOutput::from_wire_bytes(root.bytes(kLOTransferOutBytes), asset);
        if (!inner) return inner;
        // A lock is a value here, not a second type; a lock inside a lock has
        // no meaning and is refused rather than flattened.
        if (inner.value().stake_lock != 0) return fail(Err::NestedStakeableLocks);
        if (locktime == 0) return fail(Err::InvalidLocktime);
        inner.value().stake_lock = locktime;
        return inner;
    }

    if (tk != kTypeKindSecp256k1 || sk != kShapeKindTransferOutput)
        return fail(Err::UnsupportedFxOutput, "fx envelope (" + std::to_string(tk) + ", " +
                                                  std::to_string(sk) + ") is not an output this chain spends");

    const auto m = zap::Message::parse(body);
    if (!m) return fail(Err::BufferTooSmall, "the transfer-output envelope is malformed");
    const auto root = m->root();
    TransferableOutput out;
    out.asset = asset;
    out.out.amt = root.u64(kTOAmount);
    out.out.owners.locktime = root.u64(kTOLocktime);
    out.out.owners.threshold = root.u32(kTOThreshold);
    const auto addrs = root.list_stride(kTOAddressList, static_cast<std::uint32_t>(kAddressStride));
    for (int i = 0; i < addrs.size(); ++i)
        out.out.owners.addrs.push_back(
            ShortId::from(addrs.object(i, kAddressStride).bytes_fixed(0, kAddressStride)));
    return out;
}

std::vector<std::uint8_t> UTXO::wire_bytes() const {
    const TransferableOutput as_output{asset, stake_lock, out};
    const auto inner = as_output.wire_bytes();
    zap::Builder b(zap::kHeaderSize + kUSize + static_cast<std::int64_t>(inner.size()) + 32);
    auto ob = b.start_object(kUSize);
    ob.set_bytes_fixed(kUTxID, utxo.tx_id.span());
    ob.set_u32(kUOutputIndex, utxo.output_index);
    ob.set_bytes_fixed(kUAssetID, asset.span());
    ob.set_bytes(kUOutput, inner);
    ob.finish_as_root();
    return with_prefix(kTypeKindReserved, kShapeKindUTXO, b.finish());
}

Result<UTXO> UTXO::from_wire_bytes(std::span<const std::uint8_t> b) {
    if (b.size() < 2) return fail(Err::BufferTooSmall, "a utxo envelope is at least two bytes");
    if (b[1] != kShapeKindUTXO) return fail(Err::UnsupportedFxOutput, "not a utxo envelope");
    const auto m = zap::Message::parse(b.subspan(2));
    if (!m) return fail(Err::BufferTooSmall, "the utxo envelope is malformed");
    const auto root = m->root();

    UTXO u;
    u.utxo.tx_id = Id::from(root.bytes_fixed(kUTxID, kIdLen));
    u.utxo.output_index = root.u32(kUOutputIndex);
    u.asset = Id::from(root.bytes_fixed(kUAssetID, kIdLen));
    auto out = TransferableOutput::from_wire_bytes(root.bytes(kUOutput), u.asset);
    if (!out) return std::unexpected(out.error());
    u.stake_lock = out.value().stake_lock;
    u.out = out.value().out;
    return u;
}

bool outputs_sorted(const std::vector<TransferableOutput>& outs) {
    if (outs.size() < 2) return true;
    std::vector<std::vector<std::uint8_t>> keys;
    keys.reserve(outs.size());
    for (const auto& o : outs) keys.push_back(o.wire_bytes());
    for (std::size_t i = 0; i + 1 < outs.size(); ++i) {
        // strictly greater means out of order; equal is allowed
        if (const auto c = outs[i + 1].asset <=> outs[i].asset; c != 0) {
            if (c < 0) return false;
            continue;
        }
        if (bytes_compare(keys[i + 1], keys[i]) < 0) return false;
    }
    return true;
}

void sort_outputs(std::vector<TransferableOutput>& outs) {
    std::vector<std::pair<std::vector<std::uint8_t>, TransferableOutput>> keyed;
    keyed.reserve(outs.size());
    for (auto& o : outs) keyed.emplace_back(o.wire_bytes(), o);
    std::stable_sort(keyed.begin(), keyed.end(), [](const auto& a, const auto& b) {
        if (const auto c = a.second.asset <=> b.second.asset; c != 0) return c < 0;
        return bytes_compare(a.first, b.first) < 0;
    });
    for (std::size_t i = 0; i < keyed.size(); ++i) outs[i] = keyed[i].second;
}

bool inputs_sorted_unique(const std::vector<TransferableInput>& ins) {
    for (std::size_t i = 0; i + 1 < ins.size(); ++i)
        if (ins[i].compare(ins[i + 1]) >= 0) return false;
    return true;
}

void sort_inputs(std::vector<TransferableInput>& ins) {
    std::sort(ins.begin(), ins.end(),
              [](const TransferableInput& a, const TransferableInput& b) { return a.compare(b) < 0; });
}

}  // namespace lux::platformvm
