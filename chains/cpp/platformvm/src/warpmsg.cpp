// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// warpmsg.cpp — the envelope, and the four things an L1 says.
//
// Rendered from Go vms/platformvm/warp/payload and vms/platformvm/warp/message.

#include "lux/platformvm/warpmsg.hpp"

#include "lux/platformvm/sha256.hpp"
#include "lux/platformvm/zap.hpp"

#include <cstring>

namespace lux::platformvm::warpmsg {
namespace {

// ── envelope kinds. Appending a payload appends an id; never reorder.
enum class PKind : std::uint8_t { Hash = 0, AddressedCall = 1 };

constexpr std::int64_t kOffPKind = 0;
constexpr std::int64_t kHashOffHash = 1;
constexpr std::int64_t kHashSize = 33;
constexpr std::int64_t kAcOffSource = 1;
constexpr std::int64_t kAcOffPayload = 9;
constexpr std::int64_t kAcSize = 17;

// ── message kinds
enum class MKind : std::uint8_t {
    ChainToL1Conversion = 0,
    RegisterL1Validator = 1,
    L1ValidatorRegistration = 2,
    L1ValidatorWeight = 3,
};

constexpr std::int64_t kOffMKind = 0;

constexpr std::int64_t kRvOffChainId = 1;
constexpr std::int64_t kRvOffBlsKey = 33;
constexpr std::int64_t kRvOffExpiry = 81;
constexpr std::int64_t kRvOffWeight = 89;
constexpr std::int64_t kRvOffNodeId = 97;
constexpr std::int64_t kRvOffRemThreshold = 105;
constexpr std::int64_t kRvOffRemAddrs = 109;
constexpr std::int64_t kRvOffDisThreshold = 117;
constexpr std::int64_t kRvOffDisAddrs = 121;
constexpr std::int64_t kRvSize = 129;
constexpr std::int64_t kAddrStride = 20;

constexpr std::int64_t kConvOffId = 1;
constexpr std::int64_t kConvSize = 33;
constexpr std::int64_t kRegOffValidationId = 1;
constexpr std::int64_t kRegOffRegistered = 33;
constexpr std::int64_t kRegSize = 34;
constexpr std::int64_t kVwOffValidationId = 1;
constexpr std::int64_t kVwOffNonce = 33;
constexpr std::int64_t kVwOffWeight = 41;
constexpr std::int64_t kVwSize = 49;

// The conversion preimage: no kind byte, because it is never dispatched — it is
// only ever hashed.
constexpr std::int64_t kCdOffChainId = 0;
constexpr std::int64_t kCdOffManagerId = 32;
constexpr std::int64_t kCdOffManagerAddr = 64;
constexpr std::int64_t kCdOffValidators = 72;
constexpr std::int64_t kCdOffNodeIdPool = 80;
constexpr std::int64_t kCdSize = 88;

constexpr std::int64_t kCvNodeIdStart = 0;
constexpr std::int64_t kCvNodeIdLen = 4;
constexpr std::int64_t kCvBlsKey = 8;
constexpr std::int64_t kCvWeight = 56;
constexpr std::int64_t kCvStride = 64;

void put_u32(std::uint8_t* p, std::uint32_t v) {
    for (int i = 0; i < 4; ++i) p[i] = static_cast<std::uint8_t>(v >> (8 * i));
}
void put_u64(std::uint8_t* p, std::uint64_t v) {
    for (int i = 0; i < 8; ++i) p[i] = static_cast<std::uint8_t>(v >> (8 * i));
}

std::pair<std::int64_t, std::int64_t> write_owner_addrs(zap::Builder& b,
                                                        const std::vector<ShortId>& addrs) {
    if (addrs.empty()) return {0, 0};
    auto lb = b.start_list(kAddrStride);
    for (const auto& a : addrs) lb.add_bytes(a.span());
    // add_bytes counts BYTES, so the element count is the caller's to supply.
    return {lb.offset(), static_cast<std::int64_t>(addrs.size())};
}

PChainOwner read_owner(const zap::Object& root, std::int64_t threshold_off, std::int64_t addrs_off) {
    PChainOwner o;
    o.threshold = root.u32(threshold_off);
    const auto list = root.list_stride(addrs_off, kAddrStride);
    for (int i = 0; i < list.size(); ++i)
        o.addresses.push_back(ShortId::from(list.object(i, kAddrStride).bytes_fixed(0, kAddrStride)));
    return o;
}

}  // namespace

// ── the envelope

Result<Hash> Hash::build(const Id& hash) {
    zap::Builder b(zap::kHeaderSize + kHashSize);
    auto ob = b.start_object(kHashSize);
    ob.set_u8(kOffPKind, static_cast<std::uint8_t>(PKind::Hash));
    ob.set_bytes_fixed(kHashOffHash, hash.span());
    ob.finish_as_root();
    Hash h;
    h.hash = hash;
    h.bytes = b.finish();
    return h;
}

Result<AddressedCall> AddressedCall::build(std::span<const std::uint8_t> source_address,
                                           std::span<const std::uint8_t> payload) {
    zap::Builder b(zap::kHeaderSize + kAcSize + source_address.size() + payload.size() + 64);
    auto ob = b.start_object(kAcSize);
    ob.set_u8(kOffPKind, static_cast<std::uint8_t>(PKind::AddressedCall));
    ob.set_bytes(kAcOffSource, source_address);
    ob.set_bytes(kAcOffPayload, payload);
    ob.finish_as_root();
    AddressedCall a;
    a.source_address.assign(source_address.begin(), source_address.end());
    a.payload.assign(payload.begin(), payload.end());
    a.bytes = b.finish();
    return a;
}

Result<Envelope> parse_envelope(std::span<const std::uint8_t> b) {
    const auto msg = zap::Message::parse(b);
    if (!msg) return fail(Err::BufferTooSmall, "warp payload is not a zap message");
    const auto root = msg->root();
    switch (static_cast<PKind>(root.u8(kOffPKind))) {
        case PKind::Hash: {
            Hash h;
            h.hash = Id::from(root.bytes_fixed(kHashOffHash, kIdLen));
            h.bytes.assign(b.begin(), b.end());
            return Envelope{std::move(h)};
        }
        case PKind::AddressedCall: {
            AddressedCall a;
            const auto src = root.bytes(kAcOffSource);
            a.source_address.assign(src.begin(), src.end());
            const auto p = root.bytes(kAcOffPayload);
            a.payload.assign(p.begin(), p.end());
            a.bytes.assign(b.begin(), b.end());
            return Envelope{std::move(a)};
        }
    }
    return fail(Err::WrongPayloadType,
                "warp payload kind " + std::to_string(root.u8(kOffPKind)) + " names nothing");
}

// ── what an L1 says

Result<RegisterL1Validator> RegisterL1Validator::build(const Id& chain_id, const NodeId& node_id,
                                                       const signer::PublicKeyBytes& key,
                                                       std::uint64_t expiry,
                                                       const PChainOwner& remaining_balance_owner,
                                                       const PChainOwner& disable_owner,
                                                       std::uint64_t weight) {
    zap::Builder b(zap::kHeaderSize + kRvSize +
                   static_cast<std::int64_t>(remaining_balance_owner.addresses.size() +
                                             disable_owner.addresses.size()) *
                       kAddrStride +
                   64);
    const auto [rem_off, rem_count] = write_owner_addrs(b, remaining_balance_owner.addresses);
    const auto [dis_off, dis_count] = write_owner_addrs(b, disable_owner.addresses);
    auto ob = b.start_object(kRvSize);
    ob.set_u8(kOffMKind, static_cast<std::uint8_t>(MKind::RegisterL1Validator));
    ob.set_bytes_fixed(kRvOffChainId, chain_id.span());
    ob.set_bytes_fixed(kRvOffBlsKey, {key.data(), key.size()});
    ob.set_u64(kRvOffExpiry, expiry);
    ob.set_u64(kRvOffWeight, weight);
    ob.set_bytes(kRvOffNodeId, node_id.span());
    ob.set_u32(kRvOffRemThreshold, remaining_balance_owner.threshold);
    ob.set_list(kRvOffRemAddrs, rem_off, rem_count);
    ob.set_u32(kRvOffDisThreshold, disable_owner.threshold);
    ob.set_list(kRvOffDisAddrs, dis_off, dis_count);
    ob.finish_as_root();

    RegisterL1Validator m;
    m.chain_id = chain_id;
    m.node_id.assign(node_id.b.begin(), node_id.b.end());
    m.bls_public_key = key;
    m.expiry = expiry;
    m.remaining_balance_owner = remaining_balance_owner;
    m.disable_owner = disable_owner;
    m.weight = weight;
    m.bytes = b.finish();
    return m;
}

Status RegisterL1Validator::verify() const {
    // The primary network is not an L1 and does not register validators this
    // way; it has its own transaction.
    if (chain_id == kPrimaryNetworkId) return fail(Err::InvalidChainID);
    if (weight == 0) return fail(Err::InvalidWeight);
    if (node_id.size() != kNodeIdLen) return fail(Err::InvalidNodeID, "a node id is 20 bytes");
    bool empty = true;
    for (auto x : node_id)
        if (x != 0) empty = false;
    if (empty) return fail(Err::InvalidNodeID, "the empty node id is disallowed");

    if (auto s = remaining_balance_owner.verify(); !s) return fail(Err::InvalidOwner, s.error().message());
    if (auto s = disable_owner.verify(); !s) return fail(Err::InvalidOwner, s.error().message());
    return ok();
}

Id RegisterL1Validator::validation_id() const { return id_from_hash(sha256(bytes)); }

Result<L1ValidatorRegistration> L1ValidatorRegistration::build(const Id& validation_id, bool registered) {
    zap::Builder b(zap::kHeaderSize + kRegSize);
    auto ob = b.start_object(kRegSize);
    ob.set_u8(kOffMKind, static_cast<std::uint8_t>(MKind::L1ValidatorRegistration));
    ob.set_bytes_fixed(kRegOffValidationId, validation_id.span());
    ob.set_bool(kRegOffRegistered, registered);
    ob.finish_as_root();
    L1ValidatorRegistration m;
    m.validation_id = validation_id;
    m.registered = registered;
    m.bytes = b.finish();
    return m;
}

Result<L1ValidatorWeight> L1ValidatorWeight::build(const Id& validation_id, std::uint64_t nonce,
                                                   std::uint64_t weight) {
    zap::Builder b(zap::kHeaderSize + kVwSize);
    auto ob = b.start_object(kVwSize);
    ob.set_u8(kOffMKind, static_cast<std::uint8_t>(MKind::L1ValidatorWeight));
    ob.set_bytes_fixed(kVwOffValidationId, validation_id.span());
    ob.set_u64(kVwOffNonce, nonce);
    ob.set_u64(kVwOffWeight, weight);
    ob.finish_as_root();
    L1ValidatorWeight m;
    m.validation_id = validation_id;
    m.nonce = nonce;
    m.weight = weight;
    m.bytes = b.finish();
    return m;
}

Result<ChainToL1Conversion> ChainToL1Conversion::build(const Id& id) {
    zap::Builder b(zap::kHeaderSize + kConvSize);
    auto ob = b.start_object(kConvSize);
    ob.set_u8(kOffMKind, static_cast<std::uint8_t>(MKind::ChainToL1Conversion));
    ob.set_bytes_fixed(kConvOffId, id.span());
    ob.finish_as_root();
    ChainToL1Conversion m;
    m.id = id;
    m.bytes = b.finish();
    return m;
}

// The ONE canonical encoding of the conversion data, which is also the preimage
// of its id — so the id and the bytes can never diverge.
Result<std::vector<std::uint8_t>> ConversionData::encode() const {
    zap::Builder b(zap::kHeaderSize + kCdSize +
                   static_cast<std::int64_t>(validators.size()) * kCvStride +
                   static_cast<std::int64_t>(manager_address.size()) + 256);

    std::vector<std::uint8_t> node_id_pool;
    std::int64_t vdr_off = 0, vdr_count = 0;
    if (!validators.empty()) {
        auto lb = b.start_list(kCvStride);
        for (const auto& v : validators) {
            std::uint8_t e[kCvStride] = {};
            put_u32(e + kCvNodeIdStart, static_cast<std::uint32_t>(node_id_pool.size()));
            put_u32(e + kCvNodeIdLen, static_cast<std::uint32_t>(v.node_id.size()));
            node_id_pool.insert(node_id_pool.end(), v.node_id.begin(), v.node_id.end());
            std::memcpy(e + kCvBlsKey, v.bls_public_key.data(), v.bls_public_key.size());
            put_u64(e + kCvWeight, v.weight);
            lb.add_bytes({e, sizeof(e)});
        }
        vdr_off = lb.offset();
        vdr_count = static_cast<std::int64_t>(validators.size());
    }

    auto ob = b.start_object(kCdSize);
    ob.set_bytes_fixed(kCdOffChainId, chain_id.span());
    ob.set_bytes_fixed(kCdOffManagerId, manager_chain_id.span());
    ob.set_bytes(kCdOffManagerAddr, manager_address);
    ob.set_list(kCdOffValidators, vdr_off, vdr_count);
    ob.set_bytes(kCdOffNodeIdPool, node_id_pool);
    ob.finish_as_root();
    return b.finish();
}

Result<Id> ConversionData::conversion_id() const {
    auto bytes = encode();
    if (!bytes) return std::unexpected(bytes.error());
    return id_from_hash(sha256(bytes.value()));
}

Result<Message> parse_message(std::span<const std::uint8_t> b) {
    const auto msg = zap::Message::parse(b);
    if (!msg) return fail(Err::BufferTooSmall, "warp message payload is not a zap message");
    const auto root = msg->root();
    switch (static_cast<MKind>(root.u8(kOffMKind))) {
        case MKind::ChainToL1Conversion: {
            ChainToL1Conversion m;
            m.id = Id::from(root.bytes_fixed(kConvOffId, kIdLen));
            m.bytes.assign(b.begin(), b.end());
            return Message{std::move(m)};
        }
        case MKind::RegisterL1Validator: {
            RegisterL1Validator m;
            m.chain_id = Id::from(root.bytes_fixed(kRvOffChainId, kIdLen));
            const auto node = root.bytes(kRvOffNodeId);
            m.node_id.assign(node.begin(), node.end());
            const auto key = root.bytes_fixed(kRvOffBlsKey, signer::kPublicKeyLen);
            if (key.size() == signer::kPublicKeyLen)
                std::memcpy(m.bls_public_key.data(), key.data(), key.size());
            m.expiry = root.u64(kRvOffExpiry);
            m.weight = root.u64(kRvOffWeight);
            m.remaining_balance_owner = read_owner(root, kRvOffRemThreshold, kRvOffRemAddrs);
            m.disable_owner = read_owner(root, kRvOffDisThreshold, kRvOffDisAddrs);
            m.bytes.assign(b.begin(), b.end());
            return Message{std::move(m)};
        }
        case MKind::L1ValidatorRegistration: {
            L1ValidatorRegistration m;
            m.validation_id = Id::from(root.bytes_fixed(kRegOffValidationId, kIdLen));
            m.registered = root.boolean(kRegOffRegistered);
            m.bytes.assign(b.begin(), b.end());
            return Message{std::move(m)};
        }
        case MKind::L1ValidatorWeight: {
            L1ValidatorWeight m;
            m.validation_id = Id::from(root.bytes_fixed(kVwOffValidationId, kIdLen));
            m.nonce = root.u64(kVwOffNonce);
            m.weight = root.u64(kVwOffWeight);
            m.bytes.assign(b.begin(), b.end());
            return Message{std::move(m)};
        }
    }
    return fail(Err::WrongPayloadType,
                "warp message kind " + std::to_string(root.u8(kOffMKind)) + " names nothing");
}

}  // namespace lux::platformvm::warpmsg
