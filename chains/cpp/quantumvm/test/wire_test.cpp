// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// wire_test.cpp — ported from Go chains/quantumvm/wire_test.go.

#include "fixtures.hpp"

#include "lux/quantumvm/wire.hpp"

#include <cstring>

using namespace qvmtest;
using namespace lux::quantumvm::wire;

namespace {

BlockFields sample_block() {
    BlockFields b;
    b.timestamp = 1'700'000'500;
    b.height = 9;
    b.parent_id = random_id();
    b.chain_id = kTestChain;
    b.network_id = kTestNetwork;
    b.transactions = {stamped_tx(1, "op-a"), stamped_tx(2, "op-bb")};
    return b;
}

void put_u32_le(Bytes& b, std::size_t at, std::uint32_t v) {
    for (int i = 0; i < 4; ++i) b[at + static_cast<std::size_t>(i)] = static_cast<std::uint8_t>(v >> (8 * i));
}

std::uint32_t get_u32_le(const Bytes& b, std::size_t at) {
    std::uint32_t v = 0;
    for (int i = 3; i >= 0; --i) v = (v << 8) | b[at + static_cast<std::size_t>(i)];
    return v;
}

// The first little-endian occurrence of v, on a four-byte boundary.
std::size_t find_u32(const Bytes& b, std::uint32_t v) {
    for (std::size_t i = 0; i + 4 <= b.size(); i += 4)
        if (get_u32_le(b, i) == v) return i;
    return b.size();
}

}  // namespace

// TestBlockWireRoundTrip: everything the block committed to comes back, the
// transaction set above all. Leaving it out was what made a received block hold
// zero transactions — and a signature check over zero transactions checks
// nothing.
TEST(BlockWireRoundTrip) {
    const BlockFields blk = sample_block();
    const Bytes wire = block_bytes(blk);
    REQUIRE(!wire.empty());
    const Id want_id = of(view(wire));

    auto got = parse_block_bytes(view(wire));
    REQUIRE_OK(got);
    REQUIRE_EQ(blk.timestamp, got->timestamp);
    REQUIRE_EQ(blk.height, got->height);
    REQUIRE_EQ(blk.parent_id, got->parent_id);
    REQUIRE_EQ(blk.chain_id, got->chain_id);
    REQUIRE_EQ(blk.network_id, got->network_id);

    REQUIRE_MSG(got->transactions.size() == blk.transactions.size(),
                "the parser dropped the transaction set");
    for (std::size_t i = 0; i < blk.transactions.size(); ++i) {
        REQUIRE_MSG(blk.transactions[i]->id() == got->transactions[i]->id(),
                    "transaction " + std::to_string(i) + " changed identity");
        const ByteView a = blk.transactions[i]->bytes();
        const ByteView b = got->transactions[i]->bytes();
        REQUIRE_MSG(a.size() == b.size() && std::equal(a.begin(), a.end(), b.begin()),
                    "transaction " + std::to_string(i) + " changed its signed bytes");
        REQUIRE_MSG(blk.transactions[i]->signature()->signature ==
                        got->transactions[i]->signature()->signature,
                    "transaction " + std::to_string(i) + " lost its signature");
    }

    // Re-serializing a parsed block is byte-identical, which is what makes the
    // id a function of the block rather than of its encoder.
    REQUIRE_MSG(block_bytes(*got) == wire, "re-serialization changed the bytes");
    REQUIRE_EQ(want_id, of(view(block_bytes(*got))));
}

// TestBlockIDIsContentHash: the id is sha256(bytes) and is NOT stored in the
// wire — so it is never the empty id, and every field moves it.
TEST(BlockIDIsContentHash) {
    const BlockFields blk = sample_block();
    const Bytes wire = block_bytes(blk);
    const Id id = of(view(wire));
    REQUIRE_MSG(!empty(id), "block id must never be the empty id");

    auto got = parse_block_bytes(view(wire));
    REQUIRE_OK(got);
    REQUIRE_EQ(id, of(view(block_bytes(*got))));

    std::vector<BlockFields> variants;
    {
        BlockFields c = blk;
        c.height++;
        variants.push_back(c);
    }
    {
        BlockFields c = blk;
        c.parent_id = random_id();
        variants.push_back(c);
    }
    {
        BlockFields c = blk;
        c.chain_id = kOtherChain;
        variants.push_back(c);
    }
    {
        BlockFields c = blk;
        c.network_id++;
        variants.push_back(c);
    }
    {
        BlockFields c = blk;
        c.timestamp += 1;
        variants.push_back(c);
    }
    {
        BlockFields c = blk;
        c.transactions = {stamped_tx(99, "different")};
        variants.push_back(c);
    }

    std::vector<Id> seen{id};
    for (const auto& c : variants) {
        const Id got_id = of(view(block_bytes(c)));
        REQUIRE(!empty(got_id));
        for (const Id& prior : seen)
            REQUIRE_MSG(got_id != prior, "distinct block content produced a colliding id");
        seen.push_back(got_id);
    }
}

// TestOneBlockHasOneEncoding.
//
// A zap message declares its own SIZE and its own ROOT OFFSET, and both are the
// sender's to choose — so the container has degrees of freedom the content does
// not. Rejecting bytes past the DECLARED size rejected nothing: append a byte,
// bump the size field, and one logical block has a second id. Two hundred and
// fifty-six of them, from one byte of padding. Relocating the root struct is the
// same trick on the other field.
TEST(OneBlockHasOneEncoding) {
    const BlockFields blk = sample_block();
    const Bytes wire = block_bytes(blk);
    const Id id = of(view(wire));

    for (int pad = 0; pad < 256; ++pad) {
        Bytes padded = wire;
        padded.push_back(static_cast<std::uint8_t>(pad));
        put_u32_le(padded, 12, static_cast<std::uint32_t>(padded.size()));

        // Precondition: it is still a well-formed zap message whose declared
        // size covers the padding, and whose content hash has moved.
        const auto msg = zap::Message::parse(view(padded));
        REQUIRE(msg.has_value());
        REQUIRE_EQ(padded.size(), msg->size());
        REQUIRE(of(view(padded)) != id);

        REQUIRE_ERR(parse_block_bytes(view(padded)), Err::NonCanonical);
    }

    // The root struct, relocated. Every pointer in the object is relative to its
    // own field, so shifting the whole body leaves the content identical and the
    // bytes different.
    Bytes shifted;
    shifted.insert(shifted.end(), wire.begin(), wire.begin() + zap::kHeaderSize);
    shifted.insert(shifted.end(), 8, 0);
    shifted.insert(shifted.end(), wire.begin() + zap::kHeaderSize, wire.end());
    put_u32_le(shifted, 8, get_u32_le(wire, 8) + 8);
    put_u32_le(shifted, 12, static_cast<std::uint32_t>(shifted.size()));
    REQUIRE_ERR(parse_block_bytes(view(shifted)), Err::NonCanonical);

    // A root offset pointing into the wire header, where the fields would be
    // read out of the magic and the size.
    Bytes into_header = wire;
    put_u32_le(into_header, 8, 0);
    REQUIRE_FAILS(parse_block_bytes(view(into_header)));
}

// Bytes past the declared size.
TEST(BlockWireRejectsTrailing) {
    Bytes wire = block_bytes(sample_block());
    wire.push_back(0x00);
    REQUIRE_ERR(parse_block_bytes(view(wire)), Err::TrailingBytes);
}

// TestTheTransactionListMustPartitionTheBlob. The lengths say where each
// transaction ends; bytes no length names are bytes the block commits to and
// nothing reads, and a count larger than the blob can hold is an allocation a
// peer chose.
TEST(TheTransactionListMustPartitionTheBlob) {
    const BlockFields blk = sample_block();
    const Bytes wire = block_bytes(blk);

    const auto msg = zap::Message::parse(view(wire));
    REQUIRE(msg.has_value());
    const zap::Object root = msg->root();
    const zap::List lens = wire::Block(root).TxLens();
    REQUIRE_EQ(2, lens.size());

    const std::uint32_t first = lens.u32(0);
    const std::size_t lens_at = find_u32(wire, first);
    REQUIRE(lens_at < wire.size());

    for (std::uint32_t bad : {0u, 1u, first - 1, first + 1, 1u << 30}) {
        Bytes tampered = wire;
        put_u32_le(tampered, lens_at, bad);
        REQUIRE_MSG(!parse_block_bytes(view(tampered)),
                    "a transaction length of " + std::to_string(bad) + " partitioned the blob");
    }

    // And an absurd count is refused BEFORE it is allocated for.
    const std::size_t count_at = static_cast<std::size_t>(root.offset() + kBlockTxLensOff + 4);
    Bytes tampered = wire;
    put_u32_le(tampered, count_at, static_cast<std::uint32_t>(wire.size()));
    REQUIRE_ERR(parse_block_bytes(view(tampered)), Err::TxCountAbsurd);
}

TEST(BaseTransactionWireIsDeterministic) {
    auto mk = [] { return std::make_shared<BaseTransaction>(1'700'000'000, 42, bytes_of("payload")); };
    const auto a = mk();
    const auto b = mk();
    const ByteView wa = a->bytes();
    const ByteView wb = b->bytes();
    REQUIRE(!wa.empty());
    REQUIRE_MSG(wa.size() == wb.size() && std::equal(wa.begin(), wa.end(), wb.begin()),
                "the tx wire is not deterministic");

    // The signature is NOT part of the signed bytes.
    auto signed_one = mk();
    signed_one->set_signature(*stamped_tx(42, "payload")->signature());
    const ByteView ws = signed_one->bytes();
    REQUIRE_MSG(wa.size() == ws.size() && std::equal(wa.begin(), wa.end(), ws.begin()),
                "the quantum signature reached the signature preimage");
}

// TestTheEnvelopeCarriesTheSignatureAndTheBodyCarriesNone. Two wires because a
// transaction is two things: what was signed, and what was signed plus the
// signature. Folding them into one would put the signature inside its own
// preimage.
TEST(TheEnvelopeCarriesTheSignatureAndTheBodyCarriesNone) {
    const auto tx = stamped_tx(7, "payload");
    const Bytes env = marshal_tx(*tx);

    auto got = unmarshal_tx(view(env));
    REQUIRE_OK(got);
    const ByteView a = tx->bytes();
    const ByteView b = (*got)->bytes();
    REQUIRE_MSG(a.size() == b.size() && std::equal(a.begin(), a.end(), b.begin()),
                "the signature preimage changed in flight");
    REQUIRE_EQ(tx->id(), (*got)->id());
    REQUIRE_EQ(tx->signature()->signature, (*got)->signature()->signature);
    REQUIRE_EQ(tx->signature()->quantum_stamp, (*got)->signature()->quantum_stamp);
    REQUIRE_EQ(tx->signature()->timestamp, (*got)->signature()->timestamp);
    REQUIRE_MSG((*got)->signature()->public_key == (*got)->signature()->corona_key,
                "the corona key is not the public key it is derived from");
    REQUIRE_MSG(marshal_tx(**got) == env, "the envelope is not canonical");

    // A transaction with no signature still rides, and comes back with none.
    auto bare = std::make_shared<BaseTransaction>(kChainTime, 3, bytes_of("bare"));
    auto back = unmarshal_tx(view(marshal_tx(*bare)));
    REQUIRE_OK(back);
    const ByteView ba = bare->bytes();
    const ByteView bb = (*back)->bytes();
    REQUIRE(ba.size() == bb.size() && std::equal(ba.begin(), ba.end(), bb.begin()));
    REQUIRE(( *back)->signature()->signature.empty());

    // Bytes that are not an envelope are refused rather than decoded to zeros.
    Bytes truncated(env.begin(), env.end() - 1);
    Bytes extended = env;
    extended.push_back(0);
    for (const Bytes& bad : {Bytes{}, bytes_of("garbage"), truncated, extended})
        REQUIRE_MSG(!unmarshal_tx(view(bad)),
                    "a " + std::to_string(bad.size()) + "-byte non-envelope decoded");
}

// TestBaseTransactionIDNonEmpty is the regression guard for the tx pool: the id
// is sha256(bytes) and never the empty id, so distinct transactions occupy
// distinct pool slots.
TEST(BaseTransactionIDNonEmpty) {
    const auto tx = std::make_shared<BaseTransaction>(1'700'000'000, 42, bytes_of("payload"));
    const Id id = tx->id();
    REQUIRE_MSG(!empty(id), "tx id must never be the empty id");
    REQUIRE_EQ(of(tx->bytes()), id);

    const auto other = std::make_shared<BaseTransaction>(1'700'000'000, 43, bytes_of("payload"));
    REQUIRE_MSG(id != other->id(), "distinct transactions must have distinct ids");
}

// TestWireRefusesAHeaderThatIsNotThere.
//
// A field read past the end of the buffer answers zero rather than failing, so a
// truncated wire did not decode to nothing — it decoded to height 0, time 0 and
// the empty parent. Every possible truncation named that one value, each under a
// different id.
TEST(WireRefusesAHeaderThatIsNotThere) {
    zap::Builder b(zap::kHeaderSize + kBlockSize);
    auto ob = b.start_object(8);  // eight bytes where the header needs a hundred
    ob.set_u64(kBlockTimestampOff, 0);
    ob.finish_as_root();
    const Bytes short_wire = b.finish();

    // It is a well-formed ZAP message: the refusal has to come from us.
    REQUIRE(zap::Message::parse(view(short_wire)).has_value());
    REQUIRE_ERR(parse_block_bytes(view(short_wire)), Err::ShortHeader);
}

// A block over the wire bound is refused before anything walks it.
TEST(ParseRefusesABlockOverTheBound) {
    BlockFields blk = sample_block();
    blk.transactions = {stamped_tx(1, std::string(kMaxBlockSize, 'x'))};
    const Bytes wire = block_bytes(blk);
    REQUIRE_MSG(wire.size() > kMaxBlockSize, "precondition: over the bound");
    REQUIRE_ERR(parse_block_bytes(view(wire)), Err::BlockTooLarge);
}

// An empty block round-trips: genesis is one, and it is the block the whole
// chain is anchored to. Any gate that demands a transaction here refuses it.
TEST(AnEmptyBlockRoundTrips) {
    BlockFields blk = sample_block();
    blk.transactions.clear();
    const Bytes wire = block_bytes(blk);
    auto got = parse_block_bytes(view(wire));
    REQUIRE_OK(got);
    REQUIRE(got->transactions.empty());
    REQUIRE_MSG(block_bytes(*got) == wire, "an empty block does not re-serialize identically");
}

// ── against Go, on the same bytes
//
// A transaction that carries no signature still fills the envelope's signature
// fields, and the one that is not obvious is the TIME. Go substitutes a
// zero-valued struct, whose Timestamp is the zero time.Time — year 1, not the
// epoch — and writes UnixNano() of that: a fixed negative count, not 0. Writing
// 0 here gave one transaction two encodings, so the block carrying it had two
// ids and the two implementations finalized different chains.
//
// The constants below are not this port's opinion about Go. They are what
// chains/quantumvm's own marshalTx and Block.Bytes emitted, read off the
// reference implementation. Two of them would agree even with the bug — the
// body, and the payload — and they are here so that a failure names the field
// that moved rather than reporting that two long strings differ.

namespace {

// Go: BaseTransaction{timestamp: time.Unix(1000, 0), nonce: 100,
//                     data: []byte("quantum-instruction-payload")}
constexpr Seconds kGoTxTime = 1000;
constexpr std::uint64_t kGoTxNonce = 100;
constexpr const char* kGoTxData = "quantum-instruction-payload";

// marshalTx's signature PREIMAGE. The bug does not reach this, so it agreeing
// is what proves both sides are serializing the same transaction.
constexpr const char* kGoTxBody =
    "5a415000020000001000000043000000e80300000000000064000000000000000800"
    "00001b0000007175616e74756d2d696e737472756374696f6e2d7061796c6f6164";

// The ENVELOPE. Everything the preimage has, wrapped, with the signature fields
// null and envTime @16 carrying 00001a3deb03b2a1 — little-endian
// -6795364578871345152.
constexpr const char* kGoTxEnvelope =
    "5a41500002000000100000008300000030000000430000000000000000000000"
    "00001a3deb03b2a1000000000000000000000000000000000000000000000000"
    "5a415000020000001000000043000000e80300000000000064000000000000000800"
    "00001b0000007175616e74756d2d696e737472756374696f6e2d7061796c6f6164";

// Go: Block{timestamp: time.Unix(1000,0), height: 1, parentID: 32×0x01,
//           chainID: 32×0x1e, networkID: 1, transactions: [that tx]}
constexpr const char* kGoBlock =
    "5a4150000200000018000000030100008300000000000000e803000000000000"
    "0100000000000000010101010101010101010101010101010101010101010101"
    "01010101010101011e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e1e"
    "1e1e1e1e1e1e1e1e0100000000000000a0ffffff010000000800000083000000"
    "5a41500002000000100000008300000030000000430000000000000000000000"
    "00001a3deb03b2a1000000000000000000000000000000000000000000000000"
    "5a415000020000001000000043000000e8030000000000006400000000000000"
    "080000001b0000007175616e74756d2d696e737472756374696f6e2d7061796c"
    "6f6164";

// sha256 of exactly those bytes, as Go's computeID reports it.
constexpr const char* kGoBlockID = "2thq7BJZUJF3PQMQgMmeFoELS7xnSPFvGBvDr3DbWNN4e9eivL";

}  // namespace

// A transaction with no signature has to serialize here exactly as it does in
// Go, byte for byte, or the two implementations disagree about what a block IS.
TEST(AnUnsignedTransactionSerializesAsGoDoes) {
    BaseTransaction tx(kGoTxTime, kGoTxNonce, bytes_of(kGoTxData));
    REQUIRE_MSG(tx.signature() == nullptr,
                "this case is about the ABSENT signature; the fixture carries one");

    // Same transaction on both sides — otherwise the envelope comparison below
    // would be comparing two different things.
    REQUIRE_EQ(std::string(kGoTxBody), hex(tx.bytes()));

    const Bytes env = marshal_tx(tx);
    REQUIRE_MSG(hex(view(env)) == std::string(kGoTxEnvelope),
                "the envelope Go writes for an unsigned transaction is not the envelope this "
                "writes:\n  go  " + std::string(kGoTxEnvelope) + "\n  cpp " + hex(view(env)));

    // What Go writes decodes here, and re-encodes to itself — the property that
    // lets a block travel between the two without changing id in flight.
    auto back = unmarshal_tx(view(env));
    REQUIRE_OK(back);
    REQUIRE_EQ(kZeroTimeNanos, (*back)->signature()->timestamp);
    REQUIRE_MSG(hex(view(marshal_tx(**back))) == std::string(kGoTxEnvelope),
                "a decoded envelope did not re-encode to the bytes it came from");
}

// And the same, one level up, where the bytes become the thing consensus votes
// on: a block carrying that transaction must hash to the id Go computes for it.
TEST(ABlockCarryingAnUnsignedTransactionHasGosID) {
    BlockFields blk;
    blk.timestamp = kGoTxTime;
    blk.height = 1;
    blk.parent_id.fill(0x01);
    blk.chain_id.fill(0x1e);
    blk.network_id = 1;
    blk.transactions = {
        std::make_shared<BaseTransaction>(kGoTxTime, kGoTxNonce, bytes_of(kGoTxData))};

    const Bytes wire = block_bytes(blk);
    REQUIRE_MSG(hex(view(wire)) == std::string(kGoBlock),
                "block wire diverged from Go:\n  go  " + std::string(kGoBlock) + "\n  cpp " +
                    hex(view(wire)));
    REQUIRE_EQ(std::string(kGoBlockID), text(of(view(wire))));

    // It is canonical by this chain's own rule as well, so the agreement is not
    // an accident of two encoders that both happen to be wrong.
    auto got = parse_block_bytes(view(wire));
    REQUIRE_OK(got);
    REQUIRE_EQ(std::size_t{1}, got->transactions.size());
    REQUIRE_MSG(block_bytes(*got) == wire, "the block did not re-serialize identically");
}
