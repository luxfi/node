// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// warp_test.cpp — a message another chain signed, and the proof enough of it did.
//
// Ported from Go vms/platformvm/warp: unsigned_message_test.go, message_test.go,
// signature_test.go and validator_test.go. The verification case is not a
// self-check: the message, the three validators, the bit vector and the
// aggregated signature all came out of the Go reference, so a port that
// aggregated differently, indexed the bits differently, or used the wrong domain
// tag fails here.

#include "golden.hpp"
#include "harness.hpp"
#include "lux/platformvm/warp.hpp"

#include <string>

using namespace lux::platformvm;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
NodeId node_of(std::uint8_t b) {
    NodeId v{};
    for (std::size_t i = 0; i < kNodeIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

std::vector<std::uint8_t> unhex(const char* s) {
    auto nib = [](char c) -> int {
        if (c >= '0' && c <= '9') return c - '0';
        if (c >= 'a' && c <= 'f') return c - 'a' + 10;
        return -1;
    };
    std::vector<std::uint8_t> out;
    for (std::size_t i = 0; s[i] != 0 && s[i + 1] != 0; i += 2)
        out.push_back(static_cast<std::uint8_t>(nib(s[i]) * 16 + nib(s[i + 1])));
    return out;
}

std::string hex(std::span<const std::uint8_t> v) {
    static const char* d = "0123456789abcdef";
    std::string s;
    for (auto b : v) {
        s.push_back(d[b >> 4]);
        s.push_back(d[b & 0xf]);
    }
    return s;
}

std::vector<std::uint8_t> payload() {
    const std::string p = "hello-from-the-other-chain";
    return {p.begin(), p.end()};
}

// The three validators the Go reference built, in the canonical order a bit
// vector indexes: by uncompressed public key, ascending. Equal weights, so a
// two-of-three signature is exactly two-thirds.
std::map<NodeId, std::pair<std::vector<std::uint8_t>, std::uint64_t>> reference_set(
    std::uint64_t w0 = 100, std::uint64_t w1 = 100, std::uint64_t w2 = 100) {
    return {
        {node_of(1), {unhex(pvmgold::warp_vdr0_pk), w0}},
        {node_of(2), {unhex(pvmgold::warp_vdr1_pk), w1}},
        {node_of(3), {unhex(pvmgold::warp_vdr2_pk), w2}},
    };
}

warp::BitSetSignature reference_signature(std::vector<std::uint8_t> signers = {0x03}) {
    warp::BitSetSignature s;
    s.signers = std::move(signers);
    const auto raw = unhex(pvmgold::warp_agg_sig);
    std::copy(raw.begin(), raw.end(), s.signature.begin());
    return s;
}

}  // namespace

// Go: warp.NewUnsignedMessage — bytes and id.
TEST(UnsignedMessageWire) {
    auto m = warp::UnsignedMessage::build(96369, id_of(0xC0), payload());
    REQUIRE_OK(m);
    REQUIRE_EQ(std::string(pvmgold::warp_unsigned), hex(m.value().bytes));
    REQUIRE_EQ(std::string(pvmgold::warp_unsigned_id), m.value().id.hex());

    auto back = warp::UnsignedMessage::parse(m.value().bytes);
    REQUIRE_OK(back);
    REQUIRE_EQ_NUM(96369, back.value().network_id);
    REQUIRE_EQ(id_of(0xC0), back.value().source_chain_id);
    REQUIRE_EQ(payload(), back.value().payload);
    REQUIRE_EQ(m.value().id, back.value().id);
}

// Go: warp.NewMessage — the container plus its signature.
TEST(MessageWire) {
    auto u = warp::UnsignedMessage::build(96369, id_of(0xC0), payload());
    REQUIRE_OK(u);
    auto m = warp::Message::build(u.value(), reference_signature());
    REQUIRE_OK(m);
    REQUIRE_EQ(std::string(pvmgold::warp_message), hex(m.value().bytes));

    auto back = warp::Message::parse(m.value().bytes);
    REQUIRE_OK(back);
    REQUIRE_EQ(u.value().bytes, back.value().unsigned_message.bytes);
    REQUIRE_EQ(u.value().id, back.value().unsigned_message.id);
    REQUIRE(back.value().signature == reference_signature());
}

// The bit vector is a big-endian big integer, and one with a leading zero byte
// denotes the same set with different bytes — which would be two messages with
// one meaning, under two ids.
TEST(TheBitVectorIsCanonical) {
    REQUIRE_EQ_NUM(2, reference_signature({0x03}).num_signers().value());
    REQUIRE_EQ_NUM(1, reference_signature({0x04}).num_signers().value());
    REQUIRE_EQ_NUM(0, reference_signature({}).num_signers().value());
    REQUIRE_EQ_NUM(9, reference_signature({0xff, 0x01}).num_signers().value());
    REQUIRE_ERR(reference_signature({0x00, 0x03}).num_signers(), Err::InvalidBitSet);
}

// Go: warp.FlattenValidatorSet. Two nodes sharing a key are ONE signer with the
// sum of their weights — one aggregate cannot tell them apart, so the set must
// not either — and a keyless validator's stake still counts toward the total.
TEST(FlattenTheValidatorSet) {
    auto set = warp::flatten(reference_set());
    REQUIRE_OK(set);
    REQUIRE_EQ_NUM(3, set.value().validators.size());
    REQUIRE_U64(300u, set.value().total_weight);

    // Ordered by the uncompressed key, which is what the bit vector indexes.
    auto shared = reference_set();
    shared[node_of(4)] = {unhex(pvmgold::warp_vdr0_pk), 50};
    auto merged = warp::flatten(shared);
    REQUIRE_OK(merged);
    REQUIRE_EQ_NUM(3, merged.value().validators.size());
    REQUIRE_U64(350u, merged.value().total_weight);
    REQUIRE_U64(150u, merged.value().validators[0].weight);
    REQUIRE_EQ_NUM(2, merged.value().validators[0].node_ids.size());

    // A validator with no key cannot sign, and its stake still counts.
    auto keyless = reference_set();
    keyless[node_of(5)] = {{}, 400};
    auto with_keyless = warp::flatten(keyless);
    REQUIRE_OK(with_keyless);
    REQUIRE_EQ_NUM(3, with_keyless.value().validators.size());
    REQUIRE_U64(700u, with_keyless.value().total_weight);
}

// Go: warp.FilterValidators. A signature cannot name a validator that is not
// in the set.
TEST(FilterSigners) {
    auto set = warp::flatten(reference_set());
    REQUIRE_OK(set);
    const auto& vdrs = set.value().validators;

    auto two = warp::filter(std::vector<std::uint8_t>{0x03}, vdrs);
    REQUIRE_OK(two);
    REQUIRE_EQ_NUM(2, two.value().size());
    REQUIRE(two.value()[0].public_key == vdrs[0].public_key);
    REQUIRE(two.value()[1].public_key == vdrs[1].public_key);

    auto third = warp::filter(std::vector<std::uint8_t>{0x04}, vdrs);
    REQUIRE_OK(third);
    REQUIRE_EQ_NUM(1, third.value().size());
    REQUIRE(third.value()[0].public_key == vdrs[2].public_key);

    // Bit 3 names a fourth validator, and there are three.
    REQUIRE_ERR(warp::filter(std::vector<std::uint8_t>{0x08}, vdrs), Err::UnknownValidator);
    REQUIRE_ERR(warp::filter(std::vector<std::uint8_t>{0x00, 0x03}, vdrs), Err::InvalidBitSet);
}

// Go: warp.VerifyWeight. Computed without dividing, so nothing rounds in the
// attacker's favour.
TEST(VerifyWeight) {
    REQUIRE_OK(warp::verify_weight(200, 300, 2, 3));   // exactly two-thirds
    REQUIRE_OK(warp::verify_weight(300, 300, 2, 3));
    REQUIRE_ERR(warp::verify_weight(199, 300, 2, 3), Err::InsufficientWeight);
    REQUIRE_OK(warp::verify_weight(0, 0, 2, 3));  // an empty set is trivially met

    // Nothing overflows on the way: the comparison is at full width.
    REQUIRE_OK(warp::verify_weight(UINT64_MAX, UINT64_MAX, 1, 1));
    REQUIRE_ERR(warp::verify_weight(UINT64_MAX - 1, UINT64_MAX, 1, 1), Err::InsufficientWeight);
}

// The one that matters: the Go reference's own aggregate over the Go
// reference's own message verifies here, and every way of breaking it fails.
TEST(VerifyTheReferenceSignature) {
    auto u = warp::UnsignedMessage::build(96369, id_of(0xC0), payload());
    REQUIRE_OK(u);
    auto set = warp::flatten(reference_set());
    REQUIRE_OK(set);
    const auto sig = reference_signature();

    // Two of three equal weights is exactly two-thirds.
    REQUIRE_OK(warp::verify(sig, u.value(), 96369, set.value(), 2, 3));

    // Three-quarters is more than two of three.
    REQUIRE_ERR(warp::verify(sig, u.value(), 96369, set.value(), 3, 4), Err::InsufficientWeight);

    // The message must have been signed for THIS network.
    REQUIRE_ERR(warp::verify(sig, u.value(), 96370, set.value(), 2, 3), Err::WrongNetworkID);

    // Claiming a signer who did not sign breaks the aggregate.
    auto over_claimed = sig;
    over_claimed.signers = {0x07};
    REQUIRE_ERR(warp::verify(over_claimed, u.value(), 96370, set.value(), 2, 3), Err::WrongNetworkID);
    REQUIRE_ERR(warp::verify(over_claimed, u.value(), 96369, set.value(), 2, 3),
                Err::InvalidWarpSignature);

    // Claiming the wrong two is the same failure.
    auto wrong_pair = sig;
    wrong_pair.signers = {0x06};
    REQUIRE_ERR(warp::verify(wrong_pair, u.value(), 96369, set.value(), 2, 3), Err::InvalidWarpSignature);

    // A different message is not this message.
    auto other = warp::UnsignedMessage::build(96369, id_of(0xC0), std::vector<std::uint8_t>{1, 2, 3});
    REQUIRE_OK(other);
    REQUIRE_ERR(warp::verify(sig, other.value(), 96369, set.value(), 2, 3), Err::InvalidWarpSignature);

    // And a flipped bit in the signature itself — whether it stops the
    // signature decompressing or merely stops the pairing, the answer is the
    // same refusal.
    auto tampered = sig;
    tampered.signature[0] ^= 0x01;
    REQUIRE_ERR(warp::verify(tampered, u.value(), 96369, set.value(), 2, 3), Err::InvalidWarpSignature);
    auto tampered_tail = sig;
    tampered_tail.signature[95] ^= 0x01;
    REQUIRE_ERR(warp::verify(tampered_tail, u.value(), 96369, set.value(), 2, 3),
                Err::InvalidWarpSignature);
}

// A keyless validator's weight raises the bar without being able to help clear
// it. That is what stops a chain from cheapening its own quorum.
TEST(KeylessWeightRaisesTheBar) {
    auto u = warp::UnsignedMessage::build(96369, id_of(0xC0), payload());
    REQUIRE_OK(u);
    auto padded = reference_set();
    padded[node_of(9)] = {{}, 1'000};
    auto set = warp::flatten(padded);
    REQUIRE_OK(set);
    REQUIRE_U64(1'300u, set.value().total_weight);

    // The same two signers now hold 200 of 1300, which is not two-thirds.
    REQUIRE_ERR(warp::verify(reference_signature(), u.value(), 96369, set.value(), 2, 3),
                Err::InsufficientWeight);
}

// A signature scheme this port does not implement parses to a refusal, not to a
// signature that verifies trivially.
TEST(AnUnimplementedSchemeIsRefused) {
    auto u = warp::UnsignedMessage::build(96369, id_of(0xC0), payload());
    REQUIRE_OK(u);
    auto m = warp::Message::build(u.value(), reference_signature());
    REQUIRE_OK(m);

    // The signature buffer is the second byte field of the message object; its
    // own first byte is the scheme.
    auto b = m.value().bytes;
    const std::uint32_t root = static_cast<std::uint32_t>(b[8]) | (static_cast<std::uint32_t>(b[9]) << 8) |
                               (static_cast<std::uint32_t>(b[10]) << 16) |
                               (static_cast<std::uint32_t>(b[11]) << 24);
    const std::int64_t slot = static_cast<std::int64_t>(root) + 8;  // signature bytes @8
    const std::int32_t rel = static_cast<std::int32_t>(
        static_cast<std::uint32_t>(b[slot]) | (static_cast<std::uint32_t>(b[slot + 1]) << 8) |
        (static_cast<std::uint32_t>(b[slot + 2]) << 16) | (static_cast<std::uint32_t>(b[slot + 3]) << 24));
    const std::int64_t sig_buf = slot + rel;
    // Inside that buffer, the root object's first byte is the scheme kind.
    const std::uint32_t sig_root =
        static_cast<std::uint32_t>(b[sig_buf + 8]) | (static_cast<std::uint32_t>(b[sig_buf + 9]) << 8) |
        (static_cast<std::uint32_t>(b[sig_buf + 10]) << 16) |
        (static_cast<std::uint32_t>(b[sig_buf + 11]) << 24);
    b[sig_buf + sig_root] = 0x01;  // the post-quantum scheme
    REQUIRE_ERR(warp::Message::parse(b), Err::UnknownWarpSignature);
}
