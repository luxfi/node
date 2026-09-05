// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/wire.hpp"

#include <cstring>
#include <string>

namespace lux::quantumvm::wire {
namespace {

std::int64_t write_u32_list(zap::Builder& b, const std::vector<std::uint32_t>& xs) {
    auto lb = b.start_list(4);
    for (std::uint32_t x : xs) lb.add_u32(x);
    return lb.offset();
}

void append(Bytes& dst, ByteView src) { dst.insert(dst.end(), src.begin(), src.end()); }

}  // namespace

Bytes tx_body_bytes(Seconds timestamp, std::uint64_t nonce, ByteView data) {
    zap::Builder b(zap::kHeaderSize + kTxSize + data.size() + 32);
    auto ob = b.start_object(kTxSize);
    ob.set_u64(kTxTime, static_cast<std::uint64_t>(timestamp));
    ob.set_u64(kTxNonce, nonce);
    ob.set_bytes(kTxData, data);
    ob.finish_as_root();
    return b.finish();
}

Bytes marshal_tx(const Transaction& tx) {
    const ByteView body = tx.bytes();
    const quantum::QuantumSignature* sig = tx.signature();
    // A transaction with no signature still fills the envelope's signature
    // fields, and every one of them has to hold what Go's zero-valued struct
    // holds. The bytes are the same for the empty key, signature and stamp; the
    // TIME is not, because Go writes its zero time.Time rather than the epoch.
    // Writing 0 here gave the same transaction two encodings and therefore the
    // block carrying it two ids.
    static const quantum::QuantumSignature kNone = [] {
        quantum::QuantumSignature none;
        none.timestamp = kZeroTimeNanos;
        return none;
    }();
    if (sig == nullptr) sig = &kNone;

    zap::Builder b(zap::kHeaderSize + kEnvSize + body.size() + sig->public_key.size() +
                   sig->signature.size() + sig->quantum_stamp.size() + 64);
    auto ob = b.start_object(kEnvSize);
    ob.set_bytes(kEnvBody, body);
    ob.set_u32(kEnvAlg, sig->algorithm);
    ob.set_u64(kEnvTime, static_cast<std::uint64_t>(sig->timestamp));
    ob.set_bytes(kEnvKey, view(sig->public_key));
    ob.set_bytes(kEnvSig, view(sig->signature));
    ob.set_bytes(kEnvStamp, view(sig->quantum_stamp));
    ob.finish_as_root();
    return b.finish();
}

Result<std::shared_ptr<BaseTransaction>> parse_tx_body(ByteView body) {
    const auto msg = zap::Message::parse(body);
    if (!msg) return fail(Err::NotAMessage, "transaction body");
    if (msg->size() != body.size()) return fail(Err::TrailingBytes, "transaction body");
    const zap::Object o = msg->root();
    if (o.offset() + kTxSize > static_cast<std::int64_t>(msg->size()))
        return fail(Err::ShortHeader,
                    "transaction body ends " +
                        std::to_string(static_cast<std::int64_t>(msg->size()) - o.offset()) +
                        " bytes into a " + std::to_string(kTxSize) + "-byte header");

    const auto data = o.bytes(kTxData);
    return std::make_shared<BaseTransaction>(static_cast<Seconds>(o.u64(kTxTime)), o.u64(kTxNonce),
                                             Bytes(data.begin(), data.end()));
}

Result<std::shared_ptr<BaseTransaction>> unmarshal_tx(ByteView data) {
    const auto msg = zap::Message::parse(data);
    if (!msg) return fail(Err::NotAMessage, "transaction");
    if (msg->size() != data.size()) return fail(Err::TrailingBytes, "transaction");
    const zap::Object o = msg->root();
    if (o.offset() + kEnvSize > static_cast<std::int64_t>(msg->size()))
        return fail(Err::ShortHeader,
                    "transaction wire ends " +
                        std::to_string(static_cast<std::int64_t>(msg->size()) - o.offset()) +
                        " bytes into a " + std::to_string(kEnvSize) + "-byte header");

    auto tx = parse_tx_body(o.bytes(kEnvBody));
    if (!tx) return std::unexpected(tx.error());

    const auto key = o.bytes(kEnvKey);
    const auto sig = o.bytes(kEnvSig);
    const auto stamp = o.bytes(kEnvStamp);

    quantum::QuantumSignature qs;
    qs.algorithm = o.u32(kEnvAlg);
    qs.timestamp = static_cast<Nanos>(o.u64(kEnvTime));
    qs.public_key.assign(key.begin(), key.end());
    qs.signature.assign(sig.begin(), sig.end());
    qs.corona_key = qs.public_key;
    qs.quantum_stamp.assign(stamp.begin(), stamp.end());
    (*tx)->set_signature(std::move(qs));
    return *tx;
}

Bytes block_bytes(const BlockFields& b) {
    std::vector<std::uint32_t> tx_lens;
    tx_lens.reserve(b.transactions.size());
    Bytes tx_blob;
    for (const auto& tx : b.transactions) {
        const Bytes txb = marshal_tx(*tx);
        tx_lens.push_back(static_cast<std::uint32_t>(txb.size()));
        append(tx_blob, view(txb));
    }

    zap::Builder bld(zap::kHeaderSize + kBlkSize + tx_blob.size() + 4 * tx_lens.size() + 128);
    const std::int64_t tx_lens_off = write_u32_list(bld, tx_lens);

    auto ob = bld.start_object(kBlkSize);
    ob.set_u64(kBlkTime, static_cast<std::uint64_t>(b.timestamp));
    ob.set_u64(kBlkHeight, b.height);
    ob.set_bytes_fixed(kBlkParent, view(b.parent_id));
    ob.set_bytes_fixed(kBlkChain, view(b.chain_id));
    ob.set_u32(kBlkNetwork, b.network_id);
    ob.set_list(kBlkTxLens, tx_lens_off, static_cast<std::int64_t>(tx_lens.size()));
    ob.set_bytes(kBlkTxBlob, view(tx_blob));
    ob.finish_as_root();

    return bld.finish();
}

Result<std::vector<TxPtr>> parse_tx_set(const zap::List& lens, ByteView blob) {
    const int n = lens.size();
    if (n <= 0) return std::vector<TxPtr>{};

    // A list length is attacker-chosen and only clamped to the message size, so
    // bound it by what the blob can actually hold before allocating for it.
    if (static_cast<std::size_t>(n) > blob.size() / kMinTxWire)
        return fail(Err::TxCountAbsurd,
                    std::to_string(n) + " in " + std::to_string(blob.size()) + " bytes");

    std::vector<TxPtr> txs;
    txs.reserve(static_cast<std::size_t>(n));
    std::size_t off = 0;
    for (int i = 0; i < n; ++i) {
        const std::size_t size = lens.u32(i);
        if (size < kMinTxWire || off + size > blob.size())
            return fail(Err::TxBlobMismatch,
                        "entry " + std::to_string(i) + " claims " + std::to_string(size) + " of " +
                            std::to_string(blob.size() - off) + " remaining");
        auto tx = unmarshal_tx(blob.subspan(off, size));
        if (!tx)
            return fail(tx.error().code, "transaction " + std::to_string(i) + ": " +
                                             tx.error().message());
        txs.push_back(*tx);
        off += size;
    }
    if (off != blob.size())
        return fail(Err::TxBlobMismatch,
                    std::to_string(blob.size() - off) + " bytes past the last transaction");
    return txs;
}

Result<BlockFields> parse_block_bytes(ByteView data) {
    if (data.size() > kMaxBlockSize)
        return fail(Err::BlockTooLarge, std::to_string(data.size()) + " bytes over " +
                                            std::to_string(kMaxBlockSize));

    const auto msg = zap::Message::parse(data);
    if (!msg) return fail(Err::NotAMessage, "block");
    if (msg->size() != data.size()) return fail(Err::TrailingBytes, "block");

    const zap::Object o = msg->root();
    // A field read past the end of the buffer answers zero rather than failing,
    // so a wire too short to hold the header does not decode to nothing — it
    // decodes to height 0, time 0 and the empty parent. Every truncation would
    // name that one value, under as many different ids as there are ways to
    // truncate.
    if (o.offset() + kBlkSize > static_cast<std::int64_t>(msg->size()))
        return fail(Err::ShortHeader,
                    "block wire ends " +
                        std::to_string(static_cast<std::int64_t>(msg->size()) - o.offset()) +
                        " bytes into a " + std::to_string(kBlkSize) + "-byte header");

    BlockFields b;
    b.timestamp = static_cast<Seconds>(o.u64(kBlkTime));
    b.height = o.u64(kBlkHeight);
    b.parent_id = id_from(o.bytes_fixed(kBlkParent, 32));
    b.chain_id = id_from(o.bytes_fixed(kBlkChain, 32));
    b.network_id = o.u32(kBlkNetwork);

    auto txs = parse_tx_set(o.list(kBlkTxLens), o.bytes(kBlkTxBlob));
    if (!txs) return std::unexpected(txs.error());
    b.transactions = std::move(*txs);

    // Canonical or nothing. Re-serializing what was decoded and comparing is the
    // only check that covers every degree of freedom the container has — the
    // declared size, the root offset, and anything appended past the content —
    // rather than the handful anyone thought to enumerate.
    const Bytes again = block_bytes(b);
    if (again.size() != data.size() || std::memcmp(again.data(), data.data(), data.size()) != 0)
        return fail(Err::NonCanonical);

    return b;
}

}  // namespace lux::quantumvm::wire
