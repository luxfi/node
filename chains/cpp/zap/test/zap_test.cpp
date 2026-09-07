// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// zap_test.cpp — the wire-format suite for the generated codec, ported from Go
// zap_test.go (github.com/luxfi/zap). It lives beside the codec rather than
// beside one of its callers: ZAP is shared by every C++ chain here, so a chain
// is the wrong home for the proof that it reads and writes what it says.
//
// It builds under the P-chain's test harness (pvm_zap_test) because that is
// where the harness is; nothing in it is P-chain specific.
//
// Every case here asserts the same behaviour as its Go original, including the
// regression cases that pin the hostile-buffer rules: an unsigned forward
// pointer for a byte tail, a floor at the header for every pointer, and a
// length that cannot outrun the buffer it names. Those are not tidiness — a
// reader that answers a crafted buffer with real bytes is a chain that can be
// told two different things by one transaction.

#include "harness.hpp"
#include "lux/zap.hpp"

#include <cstring>
#include <string>

using namespace lux::platformvm;
using namespace lux::zap;

namespace {
std::span<const std::uint8_t> sp(const std::vector<std::uint8_t>& v) { return {v.data(), v.size()}; }
std::span<const std::uint8_t> str(const char* s) {
    return {reinterpret_cast<const std::uint8_t*>(s), std::strlen(s)};
}
std::string as_string(std::span<const std::uint8_t> b) {
    return std::string(reinterpret_cast<const char*>(b.data()), b.size());
}
}  // namespace

// Go: TestBuilder
TEST(Builder) {
    Builder b(256);
    b.write_bytes(str("hello world"));

    auto ob = b.start_object(24);
    ob.set_u32(0, 42);
    ob.set_u64(8, 0xDEADBEEF);
    ob.set_bool(16, true);
    ob.finish_as_root();

    const auto data = b.finish();
    const auto msg = Message::parse(sp(data));
    REQUIRE(msg.has_value());
    const auto root = msg->root();
    REQUIRE_EQ_NUM(42, root.u32(0));
    REQUIRE_U64(0xDEADBEEFull, root.u64(8));
    REQUIRE(root.boolean(16));
}

// Go: TestPrimitives
TEST(Primitives) {
    Builder b(256);
    auto ob = b.start_object(64);
    ob.set_u8(0, static_cast<std::uint8_t>(static_cast<std::int8_t>(-42)));
    ob.set_u16(2, static_cast<std::uint16_t>(static_cast<std::int16_t>(-1000)));
    ob.set_u32(4, static_cast<std::uint32_t>(static_cast<std::int32_t>(-100000)));
    ob.set_u64(8, static_cast<std::uint64_t>(static_cast<std::int64_t>(-1000000000)));
    ob.set_u8(16, 255);
    ob.set_u16(18, 65535);
    ob.set_u32(20, 4294967295u);
    ob.set_u64(24, 18446744073709551615ull);
    ob.finish_as_root();

    const auto data = b.finish();
    const auto msg = Message::parse(sp(data));
    REQUIRE(msg.has_value());
    const auto root = msg->root();
    REQUIRE_EQ_NUM(-42, static_cast<std::int8_t>(root.u8(0)));
    REQUIRE_EQ_NUM(-1000, static_cast<std::int16_t>(root.u16(2)));
    REQUIRE_EQ_NUM(-100000, static_cast<std::int32_t>(root.u32(4)));
    REQUIRE_EQ_NUM(-1000000000LL, static_cast<std::int64_t>(root.u64(8)));
    REQUIRE_EQ_NUM(255, root.u8(16));
    REQUIRE_EQ_NUM(65535, root.u16(18));
    REQUIRE_U64(4294967295ull, root.u32(20));
    REQUIRE_U64(18446744073709551615ull, root.u64(24));
}

// Go: TestList
TEST(List) {
    Builder b(256);
    auto lb = b.start_list(4);
    lb.add_u32(100);
    lb.add_u32(200);
    lb.add_u32(300);

    auto ob = b.start_object(16);
    ob.set_u32(0, 999);
    ob.set_list(4, lb.offset(), lb.count());
    ob.finish_as_root();

    const auto data = b.finish();
    const auto msg = Message::parse(sp(data));
    REQUIRE(msg.has_value());
    const auto root = msg->root();
    REQUIRE_EQ_NUM(999, root.u32(0));

    const auto list = root.list(4);
    REQUIRE_EQ_NUM(3, list.len());
    REQUIRE_EQ_NUM(100, list.u32(0));
    REQUIRE_EQ_NUM(200, list.u32(1));
    REQUIRE_EQ_NUM(300, list.u32(2));
}

// Go: TestByteList
TEST(ByteList) {
    Builder b(256);
    auto lb = b.start_list(1);
    lb.add_bytes(str("hello"));

    auto ob = b.start_object(16);
    ob.set_list(0, lb.offset(), lb.count());
    ob.finish_as_root();

    const auto data = b.finish();
    const auto msg = Message::parse(sp(data));
    REQUIRE(msg.has_value());
    REQUIRE(as_string(msg->root().list(0).bytes()) == "hello");
}

// Go: TestNestedObject
TEST(NestedObject) {
    Builder b(256);
    auto inner = b.start_object(8);
    inner.set_u32(0, 111);
    inner.set_u32(4, 222);
    const auto inner_off = inner.finish();

    auto outer = b.start_object(16);
    outer.set_u32(0, 333);
    outer.set_object(4, inner_off);
    outer.finish_as_root();

    const auto data = b.finish();
    const auto msg = Message::parse(sp(data));
    REQUIRE(msg.has_value());
    const auto root = msg->root();
    REQUIRE_EQ_NUM(333, root.u32(0));

    const auto obj = root.object(4);
    REQUIRE(!obj.is_null());
    REQUIRE_EQ_NUM(111, obj.u32(0));
    REQUIRE_EQ_NUM(222, obj.u32(4));
}

// Go: TestTextRoundTrip
TEST(TextRoundTrip) {
    Builder b(256);
    auto ob = b.start_object(24);
    ob.set_u32(0, 42);
    ob.set_text(4, "Alice");
    ob.set_u32(12, static_cast<std::uint32_t>(30));
    ob.finish_as_root();

    const auto data = b.finish();
    const auto msg = Message::parse(sp(data));
    REQUIRE(msg.has_value());
    const auto root = msg->root();
    REQUIRE_EQ_NUM(42, root.u32(0));
    REQUIRE(root.text(4) == "Alice");
    REQUIRE_EQ_NUM(30, static_cast<std::int32_t>(root.u32(12)));
}

// Go: TestMultipleTextFields
TEST(MultipleTextFields) {
    Builder b(256);
    auto ob = b.start_object(24);
    ob.set_text(0, "hello");
    ob.set_text(8, "world");
    ob.set_text(16, "!");
    ob.finish_as_root();

    const auto data = b.finish();
    const auto msg = Message::parse(sp(data));
    REQUIRE(msg.has_value());
    const auto root = msg->root();
    REQUIRE(root.text(0) == "hello");
    REQUIRE(root.text(8) == "world");
    REQUIRE(root.text(16) == "!");
}

// Go: TestNestedObjectWithText
TEST(NestedObjectWithText) {
    Builder b(512);
    auto inner = b.start_object(16);
    inner.set_text(0, "inner-text");
    inner.set_u32(8, 999);
    const auto inner_off = inner.finish();

    auto outer = b.start_object(16);
    outer.set_text(0, "outer-text");
    outer.set_object(8, inner_off);
    outer.finish_as_root();

    const auto data = b.finish();
    const auto msg = Message::parse(sp(data));
    REQUIRE(msg.has_value());
    const auto root = msg->root();
    REQUIRE(root.text(0) == "outer-text");
    const auto obj = root.object(8);
    REQUIRE(!obj.is_null());
    REQUIRE(obj.text(0) == "inner-text");
    REQUIRE_EQ_NUM(999, obj.u32(8));
}

// Go: TestInvalidMagic
TEST(InvalidMagic) {
    const char* raw = "INVALID_MAGIC___";
    REQUIRE(!Message::parse(str(raw)).has_value());
}

// Go: TestBufferTooSmall
TEST(BufferTooSmall) {
    const std::uint8_t raw[3] = {1, 2, 3};
    REQUIRE(!Message::parse({raw, 3}).has_value());
}

// Go: TestBytesNegativeRelOffsetRejected — a byte tail's pointer is UNSIGNED, so
// a sign-extended bit pattern becomes a huge forward offset and is refused.
TEST(BytesNegativeRelOffsetRejected) {
    Builder b(128);
    auto ob = b.start_object(12);
    ob.set_u32(0, 0xDEADBEEF);
    ob.set_bytes(4, str("hello"));
    ob.finish_as_root();
    auto buf = b.finish();

    const std::size_t root_off = get_u32(buf.data() + 8);
    put_u32(buf.data() + root_off + 4, 0xFFFFFFE0u);

    const auto msg = Message::parse(sp(buf));
    REQUIRE(msg.has_value());
    REQUIRE(msg->root().bytes(4).empty());
}

// Go: TestBytesMaxUintRelOffsetRejected
TEST(BytesMaxUintRelOffsetRejected) {
    Builder b(128);
    auto ob = b.start_object(12);
    ob.set_bytes(4, str("hello"));
    ob.finish_as_root();
    auto buf = b.finish();

    const std::size_t root_off = get_u32(buf.data() + 8);
    put_u32(buf.data() + root_off + 4, 0xFFFFFFFFu);

    const auto msg = Message::parse(sp(buf));
    REQUIRE(msg.has_value());
    REQUIRE(msg->root().bytes(4).empty());
}

// Go: TestRedRound2_HIGH1_UncappedListLength
TEST(UncappedListLength) {
    Builder b(128);
    auto lb = b.start_list(4);
    lb.add_u32(42);
    auto ob = b.start_object(8);
    ob.set_list(0, lb.offset(), lb.count());
    ob.finish_as_root();
    auto buf = b.finish();

    const std::size_t root_off = get_u32(buf.data() + 8);
    put_u32(buf.data() + root_off + 4, 0xFFFFFFFFu);

    const auto msg = Message::parse(sp(buf));
    REQUIRE(msg.has_value());
    const auto list = msg->root().list(0);
    REQUIRE(list.len() != static_cast<int>(0xFFFFFFFF));
    REQUIRE(static_cast<std::size_t>(list.len()) <= buf.size());
}

// Go: TestNewV1_ListStrideTighterClamp
TEST(ListStrideTighterClamp) {
    Builder b(512);
    auto lb = b.start_list(4);
    for (int i = 0; i < 32; ++i) lb.add_u32(static_cast<std::uint32_t>(i));
    auto ob = b.start_object(8);
    ob.set_list(0, lb.offset(), lb.count());
    ob.finish_as_root();
    auto buf = b.finish();

    const std::size_t root_off = get_u32(buf.data() + 8);
    put_u32(buf.data() + root_off + 4, 100);

    const auto msg = Message::parse(sp(buf));
    REQUIRE(msg.has_value());
    // The bare accessor cannot know the stride, so it applies only the
    // permissive baseline and accepts.
    REQUIRE_EQ_NUM(100, msg->root().list(0).len());
    // Told the stride, the same buffer is refused.
    REQUIRE(msg->root().list_stride(0, 4).is_null());
}

// Go: TestNewV1_ListStrideAcceptsHonestLength
TEST(ListStrideAcceptsHonestLength) {
    Builder b(256);
    auto lb = b.start_list(4);
    for (int i = 0; i < 5; ++i) lb.add_u32(static_cast<std::uint32_t>(0xAA00 + i));
    auto ob = b.start_object(8);
    ob.set_list(0, lb.offset(), lb.count());
    ob.finish_as_root();
    const auto buf = b.finish();

    const auto msg = Message::parse(sp(buf));
    REQUIRE(msg.has_value());
    const auto list = msg->root().list_stride(0, 4);
    REQUIRE(!list.is_null());
    REQUIRE_EQ_NUM(5, list.len());
    for (int i = 0; i < 5; ++i) REQUIRE_EQ_NUM(0xAA00 + i, list.u32(i));
}

// Go: TestRedRound2_HIGH2_BackwardListPointer
TEST(BackwardListPointer) {
    Builder b(256);
    auto lb = b.start_list(4);
    lb.add_u32(0xAA);
    lb.add_u32(0xBB);
    auto outer = b.start_object(8);
    outer.set_list(0, lb.offset(), lb.count());
    outer.finish_as_root();
    auto buf = b.finish();

    const std::int64_t root_off = get_u32(buf.data() + 8);
    put_u32(buf.data() + root_off, static_cast<std::uint32_t>(static_cast<std::int32_t>(-root_off)));

    const auto msg = Message::parse(sp(buf));
    REQUIRE(msg.has_value());
    REQUIRE_EQ_NUM(0, msg->root().list(0).len());
}

// Go: TestRedRound2_HIGH2_BackwardObjectPointer
TEST(BackwardObjectPointer) {
    Builder b(256);
    auto inner = b.start_object(8);
    inner.set_u32(0, 0xCAFEBABE);
    const auto inner_off = inner.finish();
    auto outer = b.start_object(8);
    outer.set_object(0, inner_off);
    outer.finish_as_root();
    auto buf = b.finish();

    const std::int64_t root_off = get_u32(buf.data() + 8);
    put_u32(buf.data() + root_off, static_cast<std::uint32_t>(static_cast<std::int32_t>(-root_off)));

    const auto msg = Message::parse(sp(buf));
    REQUIRE(msg.has_value());
    REQUIRE(msg->root().object(0).is_null());
}

// Go: TestRedRound2_HIGH2_BackwardBytesPointer
TEST(BackwardBytesPointer) {
    Builder b(128);
    auto ob = b.start_object(12);
    ob.set_bytes(4, str("hello"));
    ob.finish_as_root();
    auto buf = b.finish();

    const std::int64_t root_off = get_u32(buf.data() + 8);
    const auto rel = static_cast<std::uint32_t>(-(root_off + 4));
    put_u32(buf.data() + root_off + 4, rel);
    put_u32(buf.data() + root_off + 8, 4);

    const auto msg = Message::parse(sp(buf));
    REQUIRE(msg.has_value());
    REQUIRE(msg->root().bytes(4).empty());
}

// Go: TestRedRound2_MEDIUM1_VersionParse
TEST(VersionParse) {
    auto header = [](std::uint16_t version, std::uint32_t root, std::uint32_t size) {
        std::vector<std::uint8_t> h(kHeaderSize, 0);
        std::memcpy(h.data(), kMagic, 4);
        put_u16(h.data() + 4, version);
        put_u32(h.data() + 8, root);
        put_u32(h.data() + 12, size);
        return h;
    };
    const auto v1 = header(kVersion1, kHeaderSize, kHeaderSize);
    REQUIRE(Message::parse(sp(v1)).has_value());
    const auto v2 = header(kVersion2, kHeaderSize, kHeaderSize);
    REQUIRE(Message::parse(sp(v2)).has_value());
    const auto bad = header(99, kHeaderSize, kHeaderSize);
    REQUIRE(!Message::parse(sp(bad)).has_value());
}

// Go: TestRedRound2_MEDIUM1_NewBuilderEmitsV2
TEST(BuilderEmitsV2) {
    Builder b(128);
    auto ob = b.start_object(8);
    ob.set_u32(0, 42);
    ob.finish_as_root();
    const auto buf = b.finish();
    REQUIRE_EQ_NUM(kVersion2, get_u16(buf.data() + 4));
    const auto msg = Message::parse(sp(buf));
    REQUIRE(msg.has_value());
    REQUIRE_EQ_NUM(kVersion2, msg->version());
}

// Go: TestRedRound2_V18_SizeZeroRejected
TEST(SizeZeroRejected) {
    std::vector<std::uint8_t> h(kHeaderSize, 0);
    std::memcpy(h.data(), kMagic, 4);
    put_u16(h.data() + 4, kVersion2);
    put_u32(h.data() + 8, 0);
    put_u32(h.data() + 12, 0);
    REQUIRE(!Message::parse(sp(h)).has_value());
}

// Go: TestRedRound2_F1_NegativeBitPatternSweep — every high-bit pointer refused.
TEST(NegativeBitPatternSweep) {
    for (std::uint32_t v = 0xFFFFFFE0u;; ++v) {
        Builder b(128);
        auto ob = b.start_object(12);
        ob.set_u32(0, 0xDEADBEEF);
        ob.set_bytes(4, str("hello"));
        ob.finish_as_root();
        auto buf = b.finish();

        const std::size_t root_off = get_u32(buf.data() + 8);
        put_u32(buf.data() + root_off + 4, v);
        const auto msg = Message::parse(sp(buf));
        REQUIRE(msg.has_value());
        REQUIRE(msg->root().bytes(4).empty());
        if (v == 0xFFFFFFFFu) break;
    }
}

// The self-delimiting length: the split point between a signed transaction's
// unsigned prefix and its credential suffix.
TEST(MessageLengthIsTheSplitPoint) {
    Builder b(128);
    auto ob = b.start_object(8);
    ob.set_u64(0, 7);
    ob.finish_as_root();
    auto buf = b.finish();
    const auto n = message_length(sp(buf));
    REQUIRE(n.has_value());
    REQUIRE_EQ_NUM(buf.size(), *n);

    // Append a tail; the length still names only the leading message.
    buf.push_back(0xAB);
    const auto n2 = message_length(sp(buf));
    REQUIRE(n2.has_value());
    REQUIRE_EQ_NUM(buf.size() - 1, *n2);
}
