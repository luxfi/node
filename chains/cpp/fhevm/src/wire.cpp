// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// wire.cpp — native-ZAP struct-is-wire for the F-Chain. No hand-rolled
// big-endian, no cursor codec: the transaction and the block each own their
// marshal and parse over ZAP objects at FIXED field offsets. The on-wire format
// IS these offsets, and BOTH parsers are canonical — each re-serializes what it
// decoded and refuses input that is not already byte-identical to it.

#include "lux/fhevm/block.hpp"
#include "lux/fhevm/transaction.hpp"
#include <zap/zap.hpp>

#include <cstring>

namespace lux::fhevm {
namespace {

// ---- transaction content object (the semantic fields; excludes auth/sig) ----
//
//   Type     u8    @ 0
//   Payer    20B   @ 1
//   Subject  32B   @ 21
//   GasLimit u64   @ 53
//   Nonce    u64   @ 61
//   Scheme   bytes @ 69
//   Payload  bytes @ 77
constexpr int kTxType = 0;
constexpr int kTxPayer = 1;
constexpr int kTxSubject = 21;
constexpr int kTxGas = 53;
constexpr int kTxNonce = 61;
constexpr int kTxScheme = 69;
constexpr int kTxPayld = 77;
constexpr int kTxSize = 85;

// ---- transaction sig object (appended after the content object) ----
//
//   Auth bytes @ 0
//   Sig  bytes @ 8
constexpr int kSgAuth = 0;
constexpr int kSgSig = 8;
constexpr int kSgSize = 16;

// txDomain separates a transaction preimage from every other thing this chain
// hashes, so no other digest can ever be mistaken for a payer's signature over
// a transaction.
constexpr std::string_view kTxDomain = "fhevm/tx/";

Bytes to_bytes(ByteView v) { return Bytes(v.begin(), v.end()); }

// zap_len returns the total length of the leading self-delimiting ZAP message
// in b — the split point between the signing prefix and the appended auth/sig
// object.
Result<std::size_t> zap_len(ByteView b) {
    if (b.size() < std::size_t(zap::kHeaderSize)) {
        return fail(Err::InvalidPayload, "short buffer");
    }
    std::size_t n = zap::load_u32(b.data() + 12);
    if (n < std::size_t(zap::kHeaderSize) || n > b.size()) {
        return fail(Err::InvalidPayload, "bad zap length");
    }
    return n;
}

}  // namespace

Bytes Transaction::content() const {
    zap::Builder b(zap::kHeaderSize + kTxSize + int(scheme.size()) + int(payload.size()) + 64);
    auto ob = b.start_object(kTxSize);
    ob.set_u8(kTxType, type);
    ob.set_bytes_fixed(kTxPayer, view(payer));
    ob.set_bytes_fixed(kTxSubject, view(subject));
    ob.set_u64(kTxGas, gas_limit);
    ob.set_u64(kTxNonce, nonce);
    ob.set_bytes(kTxScheme, view(scheme));
    ob.set_bytes(kTxPayld, view(payload));
    ob.finish_as_root();
    return b.finish();
}

Bytes Transaction::signing_bytes(const Id& chain) const {
    Bytes c = content();
    Bytes out;
    out.reserve(kTxDomain.size() + chain.size() + c.size());
    out.insert(out.end(), kTxDomain.begin(), kTxDomain.end());
    out.insert(out.end(), chain.begin(), chain.end());
    out.insert(out.end(), c.begin(), c.end());
    return out;
}

Bytes Transaction::bytes() const {
    Bytes signing = content();

    zap::Builder sb(zap::kHeaderSize + kSgSize + int(auth.size()) + int(sig.size()) + 32);
    auto so = sb.start_object(kSgSize);
    so.set_bytes(kSgAuth, view(auth));
    so.set_bytes(kSgSig, view(sig));
    so.finish_as_root();
    Bytes sigobj = sb.finish();

    Bytes out;
    out.reserve(signing.size() + sigobj.size());
    out.insert(out.end(), signing.begin(), signing.end());
    out.insert(out.end(), sigobj.begin(), sigobj.end());
    return out;
}

Id Transaction::id() const {
    if (!id_cached_) {
        id_ = sha256(view(bytes()));
        id_cached_ = true;
    }
    return id_;
}

Result<Transaction> parse_transaction(ByteView data) {
    auto n = zap_len(data);
    if (!n) return std::unexpected(n.error());

    auto sm = zap::Message::parse(data.subspan(0, *n));
    if (!sm) {
        return fail(Err::InvalidPayload, std::string(zap::describe(sm.error())));
    }
    auto gm = zap::Message::parse(data.subspan(*n));
    if (!gm) {
        return fail(Err::InvalidPayload, std::string(zap::describe(gm.error())));
    }
    if (*n + gm->size() != data.size()) {
        return fail(Err::InvalidPayload, "trailing bytes");
    }

    zap::Object so = sm->root();
    zap::Object sg = gm->root();
    Transaction tx;
    tx.type = so.u8(kTxType);
    auto scheme_bytes = so.bytes(kTxScheme);
    tx.scheme.assign(reinterpret_cast<const char*>(scheme_bytes.data()), scheme_bytes.size());
    tx.gas_limit = so.u64(kTxGas);
    tx.nonce = so.u64(kTxNonce);
    tx.payload = to_bytes(so.bytes(kTxPayld));
    tx.auth = to_bytes(sg.bytes(kSgAuth));
    tx.sig = to_bytes(sg.bytes(kSgSig));
    auto payer = so.bytes_fixed(kTxPayer, int(tx.payer.size()));
    std::copy(payer.begin(), payer.end(), tx.payer.begin());
    auto subject = so.bytes_fixed(kTxSubject, int(tx.subject.size()));
    std::copy(subject.begin(), subject.end(), tx.subject.begin());

    // Canonical wire: ZAP follows the root offset and ignores unreferenced
    // padding inside a message's declared size, so distinct byte-strings can
    // decode to identical fields. Bind the id to the RE-SERIALIZED form and
    // reject any input that is not already canonical — exactly one byte-string
    // authenticates per logical transaction (no id malleability, fail closed).
    Bytes canonical = tx.bytes();
    if (canonical.size() != data.size() ||
        !std::equal(canonical.begin(), canonical.end(), data.begin())) {
        return fail(Err::InvalidPayload, "non-canonical tx encoding");
    }
    return tx;
}

// ---- block --------------------------------------------------------------------
//
//   ParentID  32B   @ 0
//   Height    u64   @ 32
//   Timestamp i64   @ 40   (Unix seconds — the F-Chain's block-time resolution)
//   TxLens    list  @ 48   (u32 per transaction wire length)
//   TxBlob    bytes @ 56   (concatenated transaction wire objects)
namespace {
constexpr int kBlkParent = 0;
constexpr int kBlkHeight = 32;
constexpr int kBlkTime = 40;
constexpr int kBlkTxLens = 48;
constexpr int kBlkTxBlob = 56;
constexpr int kBlkSize = 64;
}  // namespace

Bytes block_bytes(const Id& parent, std::uint64_t height, std::int64_t timestamp,
                  const std::vector<Transaction>& txs) {
    std::vector<std::uint32_t> tx_lens;
    tx_lens.reserve(txs.size());
    Bytes blob;
    for (const auto& tx : txs) {
        Bytes b = tx.bytes();
        tx_lens.push_back(std::uint32_t(b.size()));
        blob.insert(blob.end(), b.begin(), b.end());
    }

    zap::Builder bld(zap::kHeaderSize + kBlkSize + int(blob.size()) + 4 * int(tx_lens.size()) + 128);
    auto lb = bld.start_list(4);
    for (std::uint32_t x : tx_lens) lb.add_u32(x);
    auto [lens_off, lens_len] = lb.finish();

    auto ob = bld.start_object(kBlkSize);
    ob.set_bytes_fixed(kBlkParent, view(parent));
    ob.set_u64(kBlkHeight, height);
    ob.set_u64(kBlkTime, static_cast<std::uint64_t>(timestamp));
    ob.set_list(kBlkTxLens, lens_off, lens_len);
    ob.set_bytes(kBlkTxBlob, view(blob));
    ob.finish_as_root();
    return bld.finish();
}

Result<BlockHeader> parse_block_bytes(ByteView data) {
    if (data.size() > kMaxBlockSize) {
        return fail(Err::InvalidPayload, "block is over the size bound");
    }
    auto msg = zap::Message::parse(data);
    if (!msg) return fail(Err::InvalidPayload, std::string(zap::describe(msg.error())));
    if (msg->size() != data.size()) return fail(Err::InvalidPayload, "block trailing bytes");

    zap::Object o = msg->root();
    BlockHeader h;
    auto parent = o.bytes_fixed(kBlkParent, int(h.parent.size()));
    std::copy(parent.begin(), parent.end(), h.parent.begin());
    h.height = o.u64(kBlkHeight);
    h.timestamp = static_cast<std::int64_t>(o.u64(kBlkTime));

    zap::List lens = o.list_stride(kBlkTxLens, 4);
    if (lens.size() < 0 || std::size_t(lens.size()) > kMaxBlockTxs) {
        return fail(Err::InvalidPayload, "block declares more transactions than may be verified");
    }
    auto blob = o.bytes(kBlkTxBlob);
    h.transactions.reserve(std::size_t(lens.size()));
    std::size_t pos = 0;
    for (int i = 0; i < lens.size(); ++i) {
        std::size_t l = lens.u32(i);
        if (pos + l > blob.size()) return fail(Err::InvalidPayload, "tx blob out of bounds");
        auto tx = parse_transaction(blob.subspan(pos, l));
        if (!tx) return std::unexpected(tx.error());
        h.transactions.push_back(std::move(*tx));
        pos += l;
    }
    // The whole encoding must be the one this block serializes to, the same rule
    // parse_transaction applies. Bytes that no length covers are refused by it
    // too, and not separately: a blob with unread bytes re-serializes SHORTER
    // than it arrived, so this comparison fails.
    Bytes canonical = block_bytes(h.parent, h.height, h.timestamp, h.transactions);
    if (canonical.size() != data.size() ||
        !std::equal(canonical.begin(), canonical.end(), data.begin())) {
        return fail(Err::InvalidPayload, "non-canonical block encoding");
    }
    return h;
}

std::size_t empty_block_size() {
    static const std::size_t n = block_bytes(kEmptyId, 0, 0, {}).size();
    return n;
}

}  // namespace lux::fhevm
