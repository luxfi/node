// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// genesis.cpp — the genesis blob's wire, and what makes one valid.
//
// Rendered from Go vms/platformvm/genesis/genesiswire.go and genesis.go.

#include "lux/platformvm/genesis.hpp"

#include <zap/zap.hpp>

namespace lux::platformvm::genesis {
namespace {

// Genesis object layout (size 72).
constexpr std::int64_t kTimestamp = 0;
constexpr std::int64_t kInitialSupply = 8;
constexpr std::int64_t kMessage = 16;
constexpr std::int64_t kUTXOLens = 24;
constexpr std::int64_t kUTXOBlob = 32;
constexpr std::int64_t kVdrLens = 40;
constexpr std::int64_t kVdrBlob = 48;
constexpr std::int64_t kChainLens = 56;
constexpr std::int64_t kChainBlob = 64;
constexpr std::int64_t kSize = 72;
constexpr std::int64_t kLenStride = 4;

// The genesis UTXO sub-object (size 16): the UTXO's own envelope, and the
// message the allocation carried.
constexpr std::int64_t kGutxoWire = 0;
constexpr std::int64_t kGutxoMsg = 8;
constexpr std::int64_t kGutxoSize = 16;

// The shared framing: a u32 length per element, plus their bytes concatenated.
// The same framing a block uses for its transactions, for the same reason —
// every element is already self-describing, so the container only has to say
// where each one ends.
struct BlobList {
    std::int64_t len_off = 0;
    std::int64_t len_count = 0;
    std::vector<std::uint8_t> blob;
};

BlobList write_blob_list(zap::Builder& b, const std::vector<std::vector<std::uint8_t>>& blobs) {
    BlobList out;
    if (blobs.empty()) return out;
    auto lb = b.start_list(kLenStride);
    for (const auto& raw : blobs) {
        lb.add_u32(static_cast<std::uint32_t>(raw.size()));
        out.blob.insert(out.blob.end(), raw.begin(), raw.end());
    }
    const auto [lb_off, lb_count] = lb.finish();
    out.len_off = lb_off;
    out.len_count = lb_count;
    return out;
}

Result<std::vector<std::span<const std::uint8_t>>> read_blob_list(const zap::Object& obj,
                                                                  std::int64_t len_off,
                                                                  std::int64_t blob_off) {
    const auto lengths = obj.list_stride(len_off, kLenStride);
    const int n = lengths.size();
    std::vector<std::span<const std::uint8_t>> out;
    if (n == 0) return out;
    const auto blob = obj.bytes(blob_off);
    std::size_t cursor = 0;
    for (int i = 0; i < n; ++i) {
        const std::size_t size = lengths.u32(i);
        if (cursor + size > blob.size())
            return fail(Err::BadGenesis, "element " + std::to_string(i) + " length " +
                                             std::to_string(size) + " overruns the blob");
        out.push_back(blob.subspan(cursor, size));
        cursor += size;
    }
    return out;
}

std::vector<std::uint8_t> encode_allocation(const Allocation& a) {
    const auto wire = a.utxo.wire_bytes();
    zap::Builder b(zap::kHeaderSize + kGutxoSize + static_cast<std::int64_t>(wire.size() + a.message.size()) +
                   32);
    auto ob = b.start_object(kGutxoSize);
    ob.set_bytes(kGutxoWire, wire);
    ob.set_bytes(kGutxoMsg, a.message);
    ob.finish_as_root();
    return b.finish();
}

Result<Allocation> parse_allocation(std::span<const std::uint8_t> b) {
    const auto m = zap::Message::parse(b);
    if (!m) return fail(Err::BadGenesis, "a genesis allocation is not a zap message");
    const auto root = m->root();
    auto utxo = UTXO::from_wire_bytes(root.bytes(kGutxoWire));
    if (!utxo) return std::unexpected(utxo.error());
    Allocation a;
    a.utxo = utxo.value();
    const auto msg = root.bytes(kGutxoMsg);
    a.message.assign(msg.begin(), msg.end());
    return a;
}

}  // namespace

std::vector<std::uint8_t> Genesis::encode() const {
    std::vector<std::vector<std::uint8_t>> utxo_blobs;
    utxo_blobs.reserve(utxos.size());
    for (const auto& a : utxos) utxo_blobs.push_back(encode_allocation(a));

    std::vector<std::vector<std::uint8_t>> vdr_blobs;
    vdr_blobs.reserve(validators.size());
    for (const auto& tx : validators) vdr_blobs.push_back(tx.bytes);

    std::vector<std::vector<std::uint8_t>> chain_blobs;
    chain_blobs.reserve(chains.size());
    for (const auto& tx : chains) chain_blobs.push_back(tx.bytes);

    zap::Builder b(zap::kHeaderSize + kSize + 1024);
    const auto utxo_list = write_blob_list(b, utxo_blobs);
    const auto vdr_list = write_blob_list(b, vdr_blobs);
    const auto chain_list = write_blob_list(b, chain_blobs);

    auto ob = b.start_object(kSize);
    ob.set_u64(kTimestamp, timestamp);
    ob.set_u64(kInitialSupply, initial_supply);
    ob.set_text(kMessage, message);
    ob.set_list(kUTXOLens, utxo_list.len_off, utxo_list.len_count);
    ob.set_bytes(kUTXOBlob, utxo_list.blob);
    ob.set_list(kVdrLens, vdr_list.len_off, vdr_list.len_count);
    ob.set_bytes(kVdrBlob, vdr_list.blob);
    ob.set_list(kChainLens, chain_list.len_off, chain_list.len_count);
    ob.set_bytes(kChainBlob, chain_list.blob);
    ob.finish_as_root();
    return b.finish();
}

Result<Genesis> Genesis::parse(std::span<const std::uint8_t> b) {
    const auto m = zap::Message::parse(b);
    if (!m) return fail(Err::BadGenesis, "the genesis blob is not a zap message");
    const auto root = m->root();

    Genesis g;
    g.timestamp = root.u64(kTimestamp);
    g.initial_supply = root.u64(kInitialSupply);
    g.message = std::string(root.text(kMessage));

    auto utxo_blobs = read_blob_list(root, kUTXOLens, kUTXOBlob);
    if (!utxo_blobs) return std::unexpected(utxo_blobs.error());
    for (std::size_t i = 0; i < utxo_blobs.value().size(); ++i) {
        auto a = parse_allocation(utxo_blobs.value()[i]);
        if (!a) return fail(Err::BadGenesis, "utxo " + std::to_string(i) + ": " + a.error().message());
        g.utxos.push_back(std::move(a.value()));
    }

    auto vdr_blobs = read_blob_list(root, kVdrLens, kVdrBlob);
    if (!vdr_blobs) return std::unexpected(vdr_blobs.error());
    for (std::size_t i = 0; i < vdr_blobs.value().size(); ++i) {
        // Re-parsed from their own SIGNED bytes, so every id is what it was
        // when the genesis was written.
        auto tx = txs::parse(vdr_blobs.value()[i]);
        if (!tx) return fail(Err::BadGenesis, "validator " + std::to_string(i) + ": " + tx.error().message());
        g.validators.push_back(std::move(tx.value()));
    }

    auto chain_blobs = read_blob_list(root, kChainLens, kChainBlob);
    if (!chain_blobs) return std::unexpected(chain_blobs.error());
    for (std::size_t i = 0; i < chain_blobs.value().size(); ++i) {
        auto tx = txs::parse(chain_blobs.value()[i]);
        if (!tx) return fail(Err::BadGenesis, "chain " + std::to_string(i) + ": " + tx.error().message());
        g.chains.push_back(std::move(tx.value()));
    }
    return g;
}

Status Genesis::verify() const {
    // Money that is not money.
    for (std::size_t i = 0; i < utxos.size(); ++i)
        if (utxos[i].utxo.out.amt == 0)
            return fail(Err::BadGenesis, "utxo " + std::to_string(i) + " has no value");

    for (std::size_t i = 0; i < validators.size(); ++i) {
        const auto& tx = validators[i];
        auto view = txs::staker_of(*tx.unsigned_tx);
        if (!view) return std::unexpected(view.error());
        if (!view.value())
            return fail(Err::BadGenesis, "validator " + std::to_string(i) + " is not a staker transaction");
        // A validator with no weight is a validator nobody can vote toward.
        if (view.value()->weight == 0)
            return fail(Err::BadGenesis, "validator " + std::to_string(i) + " has zero weight");
        // A validator whose term is already over would have to be removed by
        // the first block, which is a state no transaction could have produced.
        if (view.value()->end <= timestamp)
            return fail(Err::BadGenesis,
                        "validator " + std::to_string(i) + " would already have unstaked");
    }

    for (std::size_t i = 0; i < chains.size(); ++i)
        if (chains[i].unsigned_tx->kind() != txs::Kind::CreateChain)
            return fail(Err::BadGenesis, "chain " + std::to_string(i) + " is not a chain transaction");
    return ok();
}

}  // namespace lux::platformvm::genesis
