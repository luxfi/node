// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// genesis.cpp — what a genesis blob MEANS, over the wire the schema states.
//
// The offsets are in schema/genesis.zap, through zapgen. Three lists of things
// that are already self-describing, so the container stores a u32 LENGTH per
// element beside one concatenated blob and re-encodes nothing.

#include "lux/platformvm/genesis.hpp"

#include "lux/platformvm/gen/genesis_zap.hpp"

namespace lux::platformvm::genesis {
namespace {

// The shared framing, written: a u32 length per element, plus their bytes
// concatenated. The same framing a block uses for its transactions.
struct BlobList {
    std::vector<std::uint32_t> lengths;
    std::vector<std::uint8_t> blob;
};

BlobList blob_list(const std::vector<std::vector<std::uint8_t>>& blobs) {
    BlobList out;
    out.lengths.reserve(blobs.size());
    for (const auto& raw : blobs) {
        out.lengths.push_back(static_cast<std::uint32_t>(raw.size()));
        out.blob.insert(out.blob.end(), raw.begin(), raw.end());
    }
    return out;
}

// The same framing, read: re-split the blob by the lengths beside it.
Result<std::vector<std::span<const std::uint8_t>>> read_blob_list(const zap::List& lengths,
                                                                  std::span<const std::uint8_t> blob) {
    const int n = lengths.size();
    std::vector<std::span<const std::uint8_t>> out;
    if (n == 0) return out;
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
    const auto utxo = a.utxo.wire_bytes();
    return wire::NewAllocation(wire::AllocationInput{.Utxo = {utxo.data(), utxo.size()},
                                                     .Message = {a.message.data(), a.message.size()}});
}

Result<Allocation> parse_allocation(std::span<const std::uint8_t> b) {
    const auto m = wire::WrapAllocation(b);
    if (!m) return fail(Err::BadGenesis, "a genesis allocation is not a zap message");
    auto utxo = UTXO::from_wire_bytes(m->Utxo());
    if (!utxo) return std::unexpected(utxo.error());
    Allocation a;
    a.utxo = utxo.value();
    const auto msg = m->Message();
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

    const auto utxo_list = blob_list(utxo_blobs);
    const auto vdr_list = blob_list(vdr_blobs);
    const auto chain_list = blob_list(chain_blobs);

    return wire::NewGenesis(wire::GenesisInput{
        .Timestamp = timestamp,
        .InitialSupply = initial_supply,
        .Message = message,
        .UtxoLengths = utxo_list.lengths,
        .UtxoBlob = {utxo_list.blob.data(), utxo_list.blob.size()},
        .ValidatorLengths = vdr_list.lengths,
        .ValidatorBlob = {vdr_list.blob.data(), vdr_list.blob.size()},
        .ChainLengths = chain_list.lengths,
        .ChainBlob = {chain_list.blob.data(), chain_list.blob.size()}});
}

Result<Genesis> Genesis::parse(std::span<const std::uint8_t> b) {
    const auto m = wire::WrapGenesis(b);
    if (!m) return fail(Err::BadGenesis, "the genesis blob is not a zap message");

    Genesis g;
    g.timestamp = m->Timestamp();
    g.initial_supply = m->InitialSupply();
    g.message = std::string(m->Message());

    auto utxo_blobs = read_blob_list(m->UtxoLengths(), m->UtxoBlob());
    if (!utxo_blobs) return std::unexpected(utxo_blobs.error());
    for (std::size_t i = 0; i < utxo_blobs.value().size(); ++i) {
        auto a = parse_allocation(utxo_blobs.value()[i]);
        if (!a) return fail(Err::BadGenesis, "utxo " + std::to_string(i) + ": " + a.error().message());
        g.utxos.push_back(std::move(a.value()));
    }

    auto vdr_blobs = read_blob_list(m->ValidatorLengths(), m->ValidatorBlob());
    if (!vdr_blobs) return std::unexpected(vdr_blobs.error());
    for (std::size_t i = 0; i < vdr_blobs.value().size(); ++i) {
        // Re-parsed from their own SIGNED bytes, so every id is what it was
        // when the genesis was written.
        auto tx = txs::parse(vdr_blobs.value()[i]);
        if (!tx) return fail(Err::BadGenesis, "validator " + std::to_string(i) + ": " + tx.error().message());
        g.validators.push_back(std::move(tx.value()));
    }

    auto chain_blobs = read_blob_list(m->ChainLengths(), m->ChainBlob());
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
