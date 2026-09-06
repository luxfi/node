// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// warpmsg.cpp — the envelope, and the four things an L1 says.
//
// Rendered from Go vms/platformvm/warp/payload and vms/platformvm/warp/message.

#include "lux/platformvm/warpmsg.hpp"

#include "lux/platformvm/gen/warpmsg_zap.hpp"
#include "lux/platformvm/sha256.hpp"

#include <cstring>

namespace lux::platformvm::warpmsg {
namespace {

// ── envelope kinds. Appending a payload appends an id; never reorder.
enum class PKind : std::uint8_t { Hash = 0, AddressedCall = 1 };

// ── message kinds
enum class MKind : std::uint8_t {
    ChainToL1Conversion = 0,
    RegisterL1Validator = 1,
    L1ValidatorRegistration = 2,
    L1ValidatorWeight = 3,
};

std::vector<std::array<std::uint8_t, kShortIdLen>> owner_addrs(const std::vector<ShortId>& addrs) {
    std::vector<std::array<std::uint8_t, kShortIdLen>> out;
    out.reserve(addrs.size());
    for (const auto& a : addrs) out.push_back(a.b);
    return out;
}

PChainOwner read_owner(std::uint32_t threshold, const zap::List& addrs) {
    PChainOwner o;
    o.threshold = threshold;
    for (int i = 0; i < addrs.size(); ++i)
        o.addresses.push_back(ShortId::from(addrs.object(i, kShortIdLen).bytes_fixed(0, kShortIdLen)));
    return o;
}

}  // namespace

// ── the envelope

Result<Hash> Hash::build(const Id& hash) {
    Hash h;
    h.hash = hash;
    h.bytes = wire::NewDigest(
        wire::DigestInput{.Kind = static_cast<std::uint8_t>(PKind::Hash), .Hash = hash.b});
    return h;
}

Result<AddressedCall> AddressedCall::build(std::span<const std::uint8_t> source_address,
                                           std::span<const std::uint8_t> payload) {
    AddressedCall a;
    a.source_address.assign(source_address.begin(), source_address.end());
    a.payload.assign(payload.begin(), payload.end());
    a.bytes = wire::NewCall(wire::CallInput{.Kind = static_cast<std::uint8_t>(PKind::AddressedCall),
                                            .Source = source_address,
                                            .Payload = payload});
    return a;
}

Result<Envelope> parse_envelope(std::span<const std::uint8_t> b) {
    const auto msg = wire::WrapTag(b);
    if (!msg) return fail(Err::BufferTooSmall, "warp payload is not a zap message");
    switch (static_cast<PKind>(msg->Kind())) {
        case PKind::Hash: {
            Hash h;
            h.hash = Id::from(wire::Digest(msg->object()).Hash());
            h.bytes.assign(b.begin(), b.end());
            return Envelope{std::move(h)};
        }
        case PKind::AddressedCall: {
            const wire::Call c(msg->object());
            AddressedCall a;
            const auto src = c.Source();
            a.source_address.assign(src.begin(), src.end());
            const auto p = c.Payload();
            a.payload.assign(p.begin(), p.end());
            a.bytes.assign(b.begin(), b.end());
            return Envelope{std::move(a)};
        }
    }
    return fail(Err::WrongPayloadType,
                "warp payload kind " + std::to_string(msg->Kind()) + " names nothing");
}

// ── what an L1 says

Result<RegisterL1Validator> RegisterL1Validator::build(const Id& chain_id, const NodeId& node_id,
                                                       const signer::PublicKeyBytes& key,
                                                       std::uint64_t expiry,
                                                       const PChainOwner& remaining_balance_owner,
                                                       const PChainOwner& disable_owner,
                                                       std::uint64_t weight) {
    RegisterL1Validator m;
    m.chain_id = chain_id;
    m.node_id.assign(node_id.b.begin(), node_id.b.end());
    m.bls_public_key = key;
    m.expiry = expiry;
    m.remaining_balance_owner = remaining_balance_owner;
    m.disable_owner = disable_owner;
    m.weight = weight;
    m.bytes = wire::NewRegister(
        wire::RegisterInput{.Kind = static_cast<std::uint8_t>(MKind::RegisterL1Validator),
                            .ChainID = chain_id.b,
                            .BLSKey = key,
                            .Expiry = expiry,
                            .Weight = weight,
                            .NodeID = node_id.span(),
                            .RemoveThreshold = remaining_balance_owner.threshold,
                            .RemoveAddrs = owner_addrs(remaining_balance_owner.addresses),
                            .DisableThreshold = disable_owner.threshold,
                            .DisableAddrs = owner_addrs(disable_owner.addresses)});
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
    L1ValidatorRegistration m;
    m.validation_id = validation_id;
    m.registered = registered;
    m.bytes = wire::NewRegistration(
        wire::RegistrationInput{.Kind = static_cast<std::uint8_t>(MKind::L1ValidatorRegistration),
                                .ValidationID = validation_id.b,
                                .Registered = registered});
    return m;
}

Result<L1ValidatorWeight> L1ValidatorWeight::build(const Id& validation_id, std::uint64_t nonce,
                                                   std::uint64_t weight) {
    L1ValidatorWeight m;
    m.validation_id = validation_id;
    m.nonce = nonce;
    m.weight = weight;
    m.bytes = wire::NewReweight(wire::ReweightInput{.Kind = static_cast<std::uint8_t>(MKind::L1ValidatorWeight),
                                                .ValidationID = validation_id.b,
                                                .Nonce = nonce,
                                                .Weight = weight});
    return m;
}

Result<ChainToL1Conversion> ChainToL1Conversion::build(const Id& id) {
    ChainToL1Conversion m;
    m.id = id;
    m.bytes = wire::NewConversion(wire::ConversionInput{
        .Kind = static_cast<std::uint8_t>(MKind::ChainToL1Conversion), .ID = id.b});
    return m;
}

// The ONE canonical encoding of the conversion data, which is also the preimage
// of its id — so the id and the bytes can never diverge.
Result<std::vector<std::uint8_t>> ConversionData::encode() const {
    std::vector<std::uint8_t> node_id_pool;
    std::vector<wire::ValidatorInput> entries;
    entries.reserve(validators.size());
    for (const auto& v : validators) {
        entries.push_back(wire::ValidatorInput{
            .NodeIDStart = static_cast<std::uint32_t>(node_id_pool.size()),
            .NodeIDLen = static_cast<std::uint32_t>(v.node_id.size()),
            .BLSKey = v.bls_public_key,
            .Weight = v.weight});
        node_id_pool.insert(node_id_pool.end(), v.node_id.begin(), v.node_id.end());
    }

    return wire::NewConversions(
        wire::ConversionsInput{.ChainID = chain_id.b,
                               .ManagerChainID = manager_chain_id.b,
                               .ManagerAddress = manager_address,
                               .Validators = std::move(entries),
                               .NodeIDPool = {node_id_pool.data(), node_id_pool.size()}});
}

Result<Id> ConversionData::conversion_id() const {
    auto bytes = encode();
    if (!bytes) return std::unexpected(bytes.error());
    return id_from_hash(sha256(bytes.value()));
}

Result<Message> parse_message(std::span<const std::uint8_t> b) {
    const auto msg = wire::WrapTag(b);
    if (!msg) return fail(Err::BufferTooSmall, "warp message payload is not a zap message");
    const auto root = msg->object();
    switch (static_cast<MKind>(msg->Kind())) {
        case MKind::ChainToL1Conversion: {
            ChainToL1Conversion m;
            m.id = Id::from(wire::Conversion(root).ID());
            m.bytes.assign(b.begin(), b.end());
            return Message{std::move(m)};
        }
        case MKind::RegisterL1Validator: {
            const wire::Register r(root);
            RegisterL1Validator m;
            m.chain_id = Id::from(r.ChainID());
            const auto node = r.NodeID();
            m.node_id.assign(node.begin(), node.end());
            const auto key = r.BLSKey();
            std::memcpy(m.bls_public_key.data(), key.data(), signer::kPublicKeyLen);
            m.expiry = r.Expiry();
            m.weight = r.Weight();
            m.remaining_balance_owner = read_owner(r.RemoveThreshold(), r.RemoveAddrs());
            m.disable_owner = read_owner(r.DisableThreshold(), r.DisableAddrs());
            m.bytes.assign(b.begin(), b.end());
            return Message{std::move(m)};
        }
        case MKind::L1ValidatorRegistration: {
            const wire::Registration g(root);
            L1ValidatorRegistration m;
            m.validation_id = Id::from(g.ValidationID());
            m.registered = g.Registered();
            m.bytes.assign(b.begin(), b.end());
            return Message{std::move(m)};
        }
        case MKind::L1ValidatorWeight: {
            const wire::Reweight w(root);
            L1ValidatorWeight m;
            m.validation_id = Id::from(w.ValidationID());
            m.nonce = w.Nonce();
            m.weight = w.Weight();
            m.bytes.assign(b.begin(), b.end());
            return Message{std::move(m)};
        }
    }
    return fail(Err::WrongPayloadType,
                "warp message kind " + std::to_string(msg->Kind()) + " names nothing");
}

}  // namespace lux::platformvm::warpmsg
