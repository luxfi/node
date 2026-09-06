// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/wire.hpp"

#include <cstring>
#include <string>

namespace lux::quantumvm::wire {
namespace {

void append(Bytes& dst, ByteView src) { dst.insert(dst.end(), src.begin(), src.end()); }

}  // namespace

Bytes tx_body_bytes(Seconds timestamp, std::uint64_t nonce, ByteView data) {
    return NewTxBody(TxBodyInput{timestamp, nonce, data});
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

    return NewTxEnvelope(TxEnvelopeInput{body, sig->algorithm, sig->timestamp,
                                        view(sig->public_key), view(sig->signature),
                                        view(sig->quantum_stamp)});
}

Result<std::shared_ptr<BaseTransaction>> parse_tx_body(ByteView body) {
    const auto msg = zap::Message::parse(body);
    if (!msg) return fail(Err::NotAMessage, "transaction body");
    if (msg->size() != body.size()) return fail(Err::TrailingBytes, "transaction body");
    const zap::Object o = msg->root();
    if (o.offset() + kTxBodySize > static_cast<std::int64_t>(msg->size()))
        return fail(Err::ShortHeader,
                    "transaction body ends " +
                        std::to_string(static_cast<std::int64_t>(msg->size()) - o.offset()) +
                        " bytes into a " + std::to_string(kTxBodySize) + "-byte header");

    const TxBody v(o);
    const auto data = v.Data();
    return std::make_shared<BaseTransaction>(v.Timestamp(), v.Nonce(),
                                             Bytes(data.begin(), data.end()));
}

Result<std::shared_ptr<BaseTransaction>> unmarshal_tx(ByteView data) {
    const auto msg = zap::Message::parse(data);
    if (!msg) return fail(Err::NotAMessage, "transaction");
    if (msg->size() != data.size()) return fail(Err::TrailingBytes, "transaction");
    const zap::Object o = msg->root();
    if (o.offset() + kTxEnvelopeSize > static_cast<std::int64_t>(msg->size()))
        return fail(Err::ShortHeader,
                    "transaction wire ends " +
                        std::to_string(static_cast<std::int64_t>(msg->size()) - o.offset()) +
                        " bytes into a " + std::to_string(kTxEnvelopeSize) + "-byte header");

    const TxEnvelope v(o);
    auto tx = parse_tx_body(v.Body());
    if (!tx) return std::unexpected(tx.error());

    const auto key = v.PublicKey();
    const auto sig = v.Signature();
    const auto stamp = v.Stamp();

    quantum::QuantumSignature qs;
    qs.algorithm = v.Algorithm();
    qs.timestamp = static_cast<Nanos>(v.Stamped());
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

    return NewBlock(BlockInput{b.timestamp, b.height, b.parent_id, b.chain_id, b.network_id,
                               tx_lens, view(tx_blob)});
}

Result<std::vector<TxPtr>> parse_tx_set(const zap::List& lens, ByteView blob) {
    // Two bounds on one attacker-chosen count, and each says its own thing.
    // The schema knows the stride, so the wire layer already refused a count
    // the MESSAGE cannot hold — it answers the absent list, which is a refusal
    // and not an empty block whenever there is a blob to account for.
    if (lens.is_null() && !blob.empty())
        return fail(Err::TxCountAbsurd, "a count the message cannot hold");

    const std::int64_t n = lens.size();
    if (n <= 0) return std::vector<TxPtr>{};

    // The second bound is this chain's: a count the BLOB cannot back, refused
    // before anything is allocated for it.
    if (static_cast<std::size_t>(n) > blob.size() / kMinTxWire)
        return fail(Err::TxCountAbsurd,
                    std::to_string(n) + " in " + std::to_string(blob.size()) + " bytes");

    std::vector<TxPtr> txs;
    txs.reserve(static_cast<std::size_t>(n));
    std::size_t off = 0;
    for (std::int64_t i = 0; i < n; ++i) {
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
    if (o.offset() + kBlockSize > static_cast<std::int64_t>(msg->size()))
        return fail(Err::ShortHeader,
                    "block wire ends " +
                        std::to_string(static_cast<std::int64_t>(msg->size()) - o.offset()) +
                        " bytes into a " + std::to_string(kBlockSize) + "-byte header");

    const Block v(o);
    BlockFields b;
    b.timestamp = v.Timestamp();
    b.height = v.Height();
    b.parent_id = id_from(v.ParentID());
    b.chain_id = id_from(v.ChainID());
    b.network_id = v.NetworkID();

    auto txs = parse_tx_set(v.TxLens(), v.TxBlob());
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
