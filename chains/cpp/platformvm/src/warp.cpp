// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// warp.cpp — the message wire, and the aggregated proof over it.
//
// Rendered from Go vms/platformvm/warp.

#include "lux/platformvm/warp.hpp"

#include "lux/platformvm/gen/warp_zap.hpp"
#include "lux/platformvm/safemath.hpp"

#include <algorithm>
#include <cstring>

namespace lux::platformvm::warp {
namespace {

// The wire kinds a signature can be. Only the first is a scheme this port
// implements; the rest are named so a refusal can say WHICH one it met.
enum class WKind : std::uint8_t {
    BitSetSignature = 0x00,
    CoronaSignature = 0x01,
    EncryptedWarpPayload = 0x02,
    HybridBLSCoronaSignature = 0x03,
    TeleportMessage = 0x04,
    TeleportTransferPayload = 0x05,
    TeleportAttestPayload = 0x06,
};

// The bit vector is a big-endian big integer. Bit i of it is bit (i % 8) of the
// byte (n - 1 - i / 8), which is what Go's big.Int.Bit does over big.Int.Bytes.
bool bit_set(std::span<const std::uint8_t> b, std::size_t i) {
    const std::size_t byte_from_end = i / 8;
    if (byte_from_end >= b.size()) return false;
    return (b[b.size() - 1 - byte_from_end] >> (i % 8)) & 1u;
}

// The position one past the highest set bit — Go's big.Int.BitLen.
std::size_t bit_len(std::span<const std::uint8_t> b) {
    for (std::size_t i = 0; i < b.size(); ++i) {
        if (b[i] == 0) continue;
        std::size_t top = 0;
        for (std::uint8_t v = b[i]; v != 0; v >>= 1) ++top;
        return (b.size() - 1 - i) * 8 + top;
    }
    return 0;
}

std::size_t popcount(std::span<const std::uint8_t> b) {
    std::size_t n = 0;
    for (auto x : b)
        for (std::uint8_t v = x; v != 0; v >>= 1) n += v & 1u;
    return n;
}

// A vector that denotes the same set with a shorter encoding is not this vector.
// Go asserts len(Bits.Bytes()) == len(Signers), and Bits.Bytes() drops leading
// zeros, so this is that check.
bool canonical_bits(std::span<const std::uint8_t> b) { return b.empty() || b[0] != 0; }

}  // namespace

Result<UnsignedMessage> UnsignedMessage::build(std::uint32_t network_id, const Id& source_chain_id,
                                               std::span<const std::uint8_t> payload) {
    UnsignedMessage m;
    m.network_id = network_id;
    m.source_chain_id = source_chain_id;
    m.payload.assign(payload.begin(), payload.end());
    m.bytes = wire::NewUnsigned(wire::UnsignedInput{
        .NetworkID = network_id, .Source = source_chain_id, .Payload = payload});
    m.id = sha256(m.bytes);
    return m;
}

Result<UnsignedMessage> UnsignedMessage::parse(std::span<const std::uint8_t> b) {
    const auto zm = wire::WrapUnsigned(b);
    if (!zm) return fail(Err::BufferTooSmall, "warp: unsigned message is not a zap message");
    UnsignedMessage m;
    m.network_id = zm->NetworkID();
    m.source_chain_id = id_from(zm->Source());
    const auto payload = zm->Payload();
    m.payload.assign(payload.begin(), payload.end());
    m.bytes.assign(b.begin(), b.end());
    m.id = sha256(m.bytes);
    return m;
}

Result<int> BitSetSignature::num_signers() const {
    if (!canonical_bits(signers)) return fail(Err::InvalidBitSet, "the bit vector is not canonical");
    return static_cast<int>(popcount(signers));
}

Result<Message> Message::build(const UnsignedMessage& unsigned_message, const BitSetSignature& sig) {
    const auto sig_bytes = wire::NewBitSet(
        wire::BitSetInput{.Kind = static_cast<std::uint8_t>(WKind::BitSetSignature),
                          .Signature = sig.signature,
                          .Signers = {sig.signers.data(), sig.signers.size()}});

    Message m;
    m.unsigned_message = unsigned_message;
    m.signature = sig;
    m.bytes = wire::NewSigned(wire::SignedInput{
        .Unsigned = {unsigned_message.bytes.data(), unsigned_message.bytes.size()},
        .Signature = {sig_bytes.data(), sig_bytes.size()}});
    return m;
}

Result<Message> Message::parse(std::span<const std::uint8_t> b) {
    const auto zm = wire::WrapSigned(b);
    if (!zm) return fail(Err::BufferTooSmall, "warp: message is not a zap message");

    auto unsigned_message = UnsignedMessage::parse(zm->Unsigned());
    if (!unsigned_message) return std::unexpected(unsigned_message.error());

    const auto sig_zm = wire::WrapBitSet(zm->Signature());
    if (!sig_zm) return fail(Err::BufferTooSmall, "warp: signature is not a zap message");

    const auto kind = static_cast<WKind>(sig_zm->Kind());
    if (kind != WKind::BitSetSignature)
        return fail(Err::UnknownWarpSignature,
                    "warp signature kind " + std::to_string(static_cast<int>(kind)) +
                        " is a scheme this port does not implement");

    BitSetSignature sig;
    // The typed accessor is TOTAL — a field the buffer is too short to hold
    // reads as zero rather than faulting. Here that would turn a truncated
    // message into a signature of ninety-six zeros, and this is the one place
    // that answered a short field with a refusal. It still does: the raw read
    // is short exactly when the buffer cannot hold the field.
    const auto raw = sig_zm->object().bytes_fixed(wire::kBitSetSignatureOff, signer::kSignatureLen);
    if (raw.size() != signer::kSignatureLen)
        return fail(Err::BufferTooSmall, "warp: the signature field is short");
    std::memcpy(sig.signature.data(), raw.data(), signer::kSignatureLen);
    const auto signers = sig_zm->Signers();
    sig.signers.assign(signers.begin(), signers.end());

    Message m;
    m.unsigned_message = std::move(unsigned_message.value());
    m.signature = std::move(sig);
    m.bytes.assign(b.begin(), b.end());
    return m;
}

Result<CanonicalValidatorSet> flatten(
    const std::map<NodeId, std::pair<std::vector<std::uint8_t>, std::uint64_t>>& set) {
    CanonicalValidatorSet out;
    // Two nodes sharing a key are ONE signer with the sum of their weights: one
    // aggregate signature cannot tell them apart, so the set must not either.
    std::map<std::vector<std::uint8_t>, Validator> by_key;

    for (const auto& [node_id, entry] : set) {
        const auto& [key, weight] = entry;
        auto total = add64(out.total_weight, weight);
        if (!total) return fail(Err::Overflow, "the validator set's weight overflows");
        out.total_weight = total.value();

        // A validator with no key still counts toward the total: its stake is
        // part of what a quorum has to beat, even though it cannot sign.
        if (key.size() != 96) continue;
        // A key that is not a key is not a signer either; the reference skips
        // it rather than refusing the whole set.
        const auto compressed = signer::compress_public_key(key);
        if (!compressed) continue;

        auto it = by_key.find(key);
        if (it == by_key.end()) {
            Validator v;
            v.public_key = *compressed;
            v.weight = weight;
            v.node_ids = {node_id};
            by_key.emplace(key, std::move(v));
            continue;
        }
        auto merged = add64(it->second.weight, weight);
        if (!merged) return fail(Err::Overflow, "a merged validator's weight overflows");
        it->second.weight = merged.value();
        it->second.node_ids.push_back(node_id);
    }

    out.validators.reserve(by_key.size());
    for (auto& [key, v] : by_key) {
        (void)key;
        out.validators.push_back(std::move(v));
    }
    // Ordered by the key a bit vector indexes by, which is the uncompressed
    // form's order — the same order std::map already walked.
    return out;
}

Result<std::vector<Validator>> filter(std::span<const std::uint8_t> signers,
                                      const std::vector<Validator>& validators) {
    if (!canonical_bits(signers)) return fail(Err::InvalidBitSet, "the bit vector is not canonical");
    const std::size_t len = bit_len(signers);
    if (len > validators.size())
        return fail(Err::UnknownValidator, "the signature names validator " + std::to_string(len - 1) +
                                               " of " + std::to_string(validators.size()));
    std::vector<Validator> out;
    for (std::size_t i = 0; i < validators.size(); ++i)
        if (bit_set(signers, i)) out.push_back(validators[i]);
    return out;
}

Result<std::uint64_t> sum_weight(const std::vector<Validator>& validators) {
    std::uint64_t weight = 0;
    for (const auto& v : validators) {
        auto sum = add64(weight, v.weight);
        if (!sum) return fail(Err::Overflow, "the signers' weight overflows");
        weight = sum.value();
    }
    return weight;
}

Status verify_weight(std::uint64_t sig_weight, std::uint64_t total_weight, std::uint64_t quorum_num,
                     std::uint64_t quorum_den) {
    // quorum_num · total ≤ quorum_den · sig, at full width, so nothing rounds in
    // the attacker's favour.
    BigUint scaled_total = BigUint::from_u64(total_weight);
    scaled_total.mul_u64(quorum_num);
    BigUint scaled_sig = BigUint::from_u64(sig_weight);
    scaled_sig.mul_u64(quorum_den);
    if (scaled_total.cmp(scaled_sig) > 0)
        return fail(Err::InsufficientWeight, std::to_string(quorum_num) + "*" +
                                                 std::to_string(total_weight) + " > " +
                                                 std::to_string(quorum_den) + "*" +
                                                 std::to_string(sig_weight));
    return ok();
}

Status verify(const BitSetSignature& sig, const UnsignedMessage& msg, std::uint32_t network_id,
              const CanonicalValidatorSet& validators, std::uint64_t quorum_num,
              std::uint64_t quorum_den) {
    if (msg.network_id != network_id)
        return fail(Err::WrongNetworkID, "the message was signed for another network");

    auto signers = filter(sig.signers, validators.validators);
    if (!signers) return std::unexpected(signers.error());

    auto weight = sum_weight(signers.value());
    if (!weight) return std::unexpected(weight.error());
    if (auto st = verify_weight(weight.value(), validators.total_weight, quorum_num, quorum_den); !st)
        return st;

    std::vector<signer::PublicKeyBytes> keys;
    keys.reserve(signers.value().size());
    for (const auto& v : signers.value()) keys.push_back(v.public_key);
    const auto aggregate = signer::aggregate_public_keys(keys);
    if (!aggregate) return fail(Err::InvalidPublicKey, "the signers' keys do not aggregate");

    if (!signer::verify_signature(*aggregate, sig.signature, msg.bytes))
        return fail(Err::InvalidWarpSignature);
    return ok();
}

}  // namespace lux::platformvm::warp
