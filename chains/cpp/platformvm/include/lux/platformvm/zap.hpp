// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// zap.hpp — the Zero-copy Application Protocol message format, in C++.
//
// This is the SERIALIZATION of the P-chain: every transaction, every block and
// every stored record on this chain is a ZAP buffer and nothing else. There is
// no codec, no version map, no schema registry — a field is an offset and a
// width, and a tx type is the one byte at offset 0 of its root object.
//
// Rendered from the Go reference github.com/luxfi/zap v1.2.7 (zap.go,
// builder.go). Byte-for-byte identical: a buffer written here parses there and
// a buffer written there parses here, because both are the same arithmetic over
// the same layout, not two encoders that agree by convention.
//
//   Header (16 bytes)
//     magic  "ZAP\0"   4 bytes @ 0
//     version u16 LE     2 @ 4    (1 = legacy schema, 2 = current)
//     flags   u16 LE     2 @ 6
//     root    u32 LE     4 @ 8    absolute offset of the root object
//     size    u32 LE     4 @ 12   total message length, header included
//
// Everything after the header is the data segment: fixed-size object payloads,
// list payloads, and variable byte tails. Every multi-byte integer is little
// endian. A pointer field is RELATIVE to the position of the pointer itself,
// which is what lets a whole message move without rewriting it.
//
// Reading is total: an out-of-range read answers zero, not a fault. That is the
// property the Go reference relies on to hand a hostile buffer straight to a
// typed accessor without a validation pass first — bounds live in one place
// (here) rather than in every caller. Callers still validate MEANING; the wire
// layer only promises that a malformed buffer cannot reach past its end.

#pragma once

#include <cstddef>
#include <cstdint>
#include <cstring>
#include <optional>
#include <span>
#include <string_view>
#include <vector>

namespace lux::platformvm::zap {

inline constexpr std::size_t kHeaderSize = 16;
inline constexpr std::size_t kAlignment = 8;
inline constexpr std::uint16_t kVersion1 = 1;
inline constexpr std::uint16_t kVersion2 = 2;
inline constexpr std::uint16_t kVersion = kVersion2;
inline constexpr char kMagic[4] = {'Z', 'A', 'P', '\0'};

// ── little-endian primitive reads/writes; the ONE place byte order is spelled

inline std::uint16_t load_u16(const std::uint8_t* p) {
    return static_cast<std::uint16_t>(p[0]) | (static_cast<std::uint16_t>(p[1]) << 8);
}
inline std::uint32_t load_u32(const std::uint8_t* p) {
    return static_cast<std::uint32_t>(p[0]) | (static_cast<std::uint32_t>(p[1]) << 8) |
           (static_cast<std::uint32_t>(p[2]) << 16) | (static_cast<std::uint32_t>(p[3]) << 24);
}
inline std::uint64_t load_u64(const std::uint8_t* p) {
    return static_cast<std::uint64_t>(load_u32(p)) | (static_cast<std::uint64_t>(load_u32(p + 4)) << 32);
}
inline void store_u16(std::uint8_t* p, std::uint16_t v) {
    p[0] = static_cast<std::uint8_t>(v);
    p[1] = static_cast<std::uint8_t>(v >> 8);
}
inline void store_u32(std::uint8_t* p, std::uint32_t v) {
    p[0] = static_cast<std::uint8_t>(v);
    p[1] = static_cast<std::uint8_t>(v >> 8);
    p[2] = static_cast<std::uint8_t>(v >> 16);
    p[3] = static_cast<std::uint8_t>(v >> 24);
}
inline void store_u64(std::uint8_t* p, std::uint64_t v) {
    store_u32(p, static_cast<std::uint32_t>(v));
    store_u32(p + 4, static_cast<std::uint32_t>(v >> 32));
}

class List;

// Object is a zero-copy view of one fixed-size record inside a message. It
// carries the whole buffer so every read can bound itself; an Object at offset
// 0 is the null object and every read through it answers zero.
class Object {
  public:
    Object() = default;
    Object(const std::uint8_t* data, std::size_t size, std::int64_t offset)
        : d_(data), n_(size), off_(offset) {}

    bool is_null() const { return off_ == 0 || d_ == nullptr; }
    std::int64_t offset() const { return off_; }

    std::uint8_t u8(std::int64_t field) const {
        const std::int64_t pos = off_ + field;
        if (d_ == nullptr || pos < 0 || static_cast<std::uint64_t>(pos) >= n_) return 0;
        return d_[pos];
    }
    bool boolean(std::int64_t field) const { return u8(field) != 0; }
    std::uint16_t u16(std::int64_t field) const {
        const std::int64_t pos = off_ + field;
        if (d_ == nullptr || pos < 0 || static_cast<std::uint64_t>(pos) + 2 > n_) return 0;
        return load_u16(d_ + pos);
    }
    std::uint32_t u32(std::int64_t field) const {
        const std::int64_t pos = off_ + field;
        if (d_ == nullptr || pos < 0 || static_cast<std::uint64_t>(pos) + 4 > n_) return 0;
        return load_u32(d_ + pos);
    }
    std::uint64_t u64(std::int64_t field) const {
        const std::int64_t pos = off_ + field;
        if (d_ == nullptr || pos < 0 || static_cast<std::uint64_t>(pos) + 8 > n_) return 0;
        return load_u64(d_ + pos);
    }

    // A fixed-width byte run living IN the payload (an id, a public key), as
    // opposed to bytes() which follows a pointer to a variable tail.
    std::span<const std::uint8_t> bytes_fixed(std::int64_t field, std::int64_t len) const {
        if (len <= 0 || d_ == nullptr) return {};
        const std::int64_t pos = off_ + field;
        if (pos < 0 || static_cast<std::uint64_t>(pos + len) > n_) return {};
        return {d_ + pos, static_cast<std::size_t>(len)};
    }

    // A variable byte tail: {relOffset u32, length u32}. relOffset is an
    // UNSIGNED forward pointer — a crafted high-bit value becomes a huge
    // positive and is rejected by the end bound rather than aliasing backwards.
    std::span<const std::uint8_t> bytes(std::int64_t field) const {
        const std::int64_t pos = off_ + field;
        if (d_ == nullptr || pos < 0 || static_cast<std::uint64_t>(pos) + 4 > n_) return {};
        const std::uint32_t rel = load_u32(d_ + pos);
        if (rel == 0) return {};  // null
        const std::int64_t len_pos = pos + 4;
        if (static_cast<std::uint64_t>(len_pos) + 4 > n_) return {};
        const std::uint32_t len = load_u32(d_ + len_pos);
        const std::int64_t abs = pos + static_cast<std::int64_t>(rel);
        // A tail can never live inside the wire header.
        if (abs < static_cast<std::int64_t>(kHeaderSize)) return {};
        if (static_cast<std::uint64_t>(abs) + len > n_) return {};
        return {d_ + abs, static_cast<std::size_t>(len)};
    }

    std::string_view text(std::int64_t field) const {
        const auto b = bytes(field);
        if (b.empty()) return {};
        return {reinterpret_cast<const char*>(b.data()), b.size()};
    }

    // A nested object pointer. relOffset is SIGNED here: an honest builder that
    // finalized the child first points backwards.
    Object object(std::int64_t field) const {
        const std::int64_t pos = off_ + field;
        if (d_ == nullptr || pos < 0 || static_cast<std::uint64_t>(pos) + 4 > n_) return {};
        const std::int32_t rel = static_cast<std::int32_t>(load_u32(d_ + pos));
        if (rel == 0) return {};
        const std::int64_t abs = pos + rel;
        if (abs < static_cast<std::int64_t>(kHeaderSize) || static_cast<std::uint64_t>(abs) >= n_) return {};
        return Object(d_, n_, abs);
    }

    List list(std::int64_t field) const;
    // list_stride applies the tighter acceptance test length*stride <= bytes
    // remaining, so an attacker-set count cannot make a caller loop 4 billion
    // times over an empty buffer.
    List list_stride(std::int64_t field, std::uint32_t min_stride) const;

  private:
    const std::uint8_t* d_ = nullptr;
    std::size_t n_ = 0;
    std::int64_t off_ = 0;
};

// List is a zero-copy view of a run of same-shaped elements. The stride is not
// on the wire — it belongs to the schema, so it is supplied by the accessor.
class List {
  public:
    List() = default;
    List(const std::uint8_t* data, std::size_t size, std::int64_t offset, std::int64_t len)
        : d_(data), n_(size), off_(offset), len_(len) {}

    int size() const { return static_cast<int>(len_); }
    bool is_null() const { return d_ == nullptr; }

    std::uint8_t u8(int i) const {
        if (i < 0 || i >= len_) return 0;
        const std::int64_t pos = off_ + i;
        if (static_cast<std::uint64_t>(pos) >= n_) return 0;
        return d_[pos];
    }
    std::uint32_t u32(int i) const {
        if (i < 0 || i >= len_) return 0;
        const std::int64_t pos = off_ + static_cast<std::int64_t>(i) * 4;
        if (static_cast<std::uint64_t>(pos) + 4 > n_) return 0;
        return load_u32(d_ + pos);
    }
    std::uint64_t u64(int i) const {
        if (i < 0 || i >= len_) return 0;
        const std::int64_t pos = off_ + static_cast<std::int64_t>(i) * 8;
        if (static_cast<std::uint64_t>(pos) + 8 > n_) return 0;
        return load_u64(d_ + pos);
    }
    // Element i of an INLINE object list: a fixed-stride record, not a pointer.
    Object object(int i, std::int64_t elem_size) const {
        if (i < 0 || i >= len_) return {};
        return Object(d_, n_, off_ + static_cast<std::int64_t>(i) * elem_size);
    }
    std::span<const std::uint8_t> raw() const {
        if (d_ == nullptr || static_cast<std::uint64_t>(off_ + len_) > n_) return {};
        return {d_ + off_, static_cast<std::size_t>(len_)};
    }

  private:
    const std::uint8_t* d_ = nullptr;
    std::size_t n_ = 0;
    std::int64_t off_ = 0;
    std::int64_t len_ = 0;
};

inline List Object::list(std::int64_t field) const {
    const std::int64_t pos = off_ + field;
    if (d_ == nullptr || pos < 0 || static_cast<std::uint64_t>(pos) + 8 > n_) return {};
    const std::int32_t rel = static_cast<std::int32_t>(load_u32(d_ + pos));
    if (rel == 0) return {};
    const std::uint32_t len = load_u32(d_ + pos + 4);
    if (static_cast<std::uint64_t>(len) > n_) return {};
    const std::int64_t abs = pos + rel;
    if (abs < static_cast<std::int64_t>(kHeaderSize) || static_cast<std::uint64_t>(abs) >= n_) return {};
    return List(d_, n_, abs, len);
}

inline List Object::list_stride(std::int64_t field, std::uint32_t min_stride) const {
    const std::int64_t pos = off_ + field;
    if (d_ == nullptr || pos < 0 || static_cast<std::uint64_t>(pos) + 8 > n_) return {};
    const std::int32_t rel = static_cast<std::int32_t>(load_u32(d_ + pos));
    if (rel == 0) return {};
    const std::uint32_t len = load_u32(d_ + pos + 4);
    const std::int64_t abs = pos + rel;
    if (abs < static_cast<std::int64_t>(kHeaderSize) || static_cast<std::uint64_t>(abs) >= n_) return {};
    const std::uint64_t remaining = n_ - static_cast<std::uint64_t>(abs);
    if (min_stride > 0) {
        if (static_cast<std::uint64_t>(len) * static_cast<std::uint64_t>(min_stride) > remaining) return {};
    } else if (static_cast<std::uint64_t>(len) > n_) {
        return {};
    }
    return List(d_, n_, abs, len);
}

// Message is a parsed buffer. It does not own its bytes — whoever holds the
// buffer holds them; a Message is only the validated window onto them.
class Message {
  public:
    Message() = default;

    static std::optional<Message> parse(std::span<const std::uint8_t> data) {
        if (data.size() < kHeaderSize) return std::nullopt;
        if (std::memcmp(data.data(), kMagic, 4) != 0) return std::nullopt;
        const std::uint16_t version = load_u16(data.data() + 4);
        if (version != kVersion1 && version != kVersion2) return std::nullopt;
        const std::uint32_t size = load_u32(data.data() + 12);
        if (size < kHeaderSize || static_cast<std::size_t>(size) > data.size()) return std::nullopt;
        return Message(data.data(), size);
    }

    bool valid() const { return d_ != nullptr; }
    std::span<const std::uint8_t> bytes() const { return {d_, n_}; }
    std::size_t size() const { return n_; }
    std::uint16_t version() const { return d_ ? load_u16(d_ + 4) : 0; }
    std::uint16_t flags() const { return d_ ? load_u16(d_ + 6) : 0; }
    Object root() const {
        if (d_ == nullptr) return {};
        return Object(d_, n_, static_cast<std::int64_t>(load_u32(d_ + 8)));
    }

  private:
    Message(const std::uint8_t* d, std::size_t n) : d_(d), n_(n) {}
    const std::uint8_t* d_ = nullptr;
    std::size_t n_ = 0;
};

// The self-delimiting length of the leading message in a buffer: the size field
// at offset 12. This is the split point between the unsigned prefix of a signed
// tx and the credential suffix that follows it.
inline std::optional<std::size_t> message_length(std::span<const std::uint8_t> b) {
    if (b.size() < kHeaderSize) return std::nullopt;
    const std::size_t n = load_u32(b.data() + 12);
    if (n < kHeaderSize || n > b.size()) return std::nullopt;
    return n;
}

class ObjectBuilder;
class ListBuilder;

// Builder writes a message. Fields are written eagerly: StartObject reserves the
// whole fixed payload, so a variable tail can be appended and its pointer
// patched on the spot. Nothing is deferred, so the bytes a Set call produces
// never depend on what a later call does.
class Builder {
  public:
    explicit Builder(std::size_t capacity = 256, std::uint16_t version = kVersion2) {
        if (capacity < kHeaderSize) capacity = 256;
        buf_.assign(capacity, 0);
        pos_ = kHeaderSize;
        std::memcpy(buf_.data(), kMagic, 4);
        store_u16(buf_.data() + 4, version);
    }

    void reset() {
        pos_ = kHeaderSize;
        root_ = 0;
    }

    ObjectBuilder start_object(std::int64_t data_size);
    ListBuilder start_list(std::int64_t elem_size);

    std::int64_t write_bytes(std::span<const std::uint8_t> data) {
        if (data.empty()) return 0;
        align(kAlignment);
        const std::int64_t off = pos_;
        grow(data.size());
        std::memcpy(buf_.data() + pos_, data.data(), data.size());
        pos_ += static_cast<std::int64_t>(data.size());
        return off;
    }

    std::vector<std::uint8_t> finish() {
        store_u32(buf_.data() + 8, static_cast<std::uint32_t>(root_));
        store_u32(buf_.data() + 12, static_cast<std::uint32_t>(pos_));
        return std::vector<std::uint8_t>(buf_.begin(), buf_.begin() + pos_);
    }

    std::vector<std::uint8_t> finish_with_flags(std::uint16_t flags) {
        store_u16(buf_.data() + 6, flags);
        return finish();
    }

  private:
    friend class ObjectBuilder;
    friend class ListBuilder;

    void grow(std::size_t n) {
        if (static_cast<std::size_t>(pos_) + n <= buf_.size()) return;
        std::size_t cap = buf_.size() * 2;
        if (cap < static_cast<std::size_t>(pos_) + n) cap = static_cast<std::size_t>(pos_) + n;
        buf_.resize(cap, 0);
    }

    void align(std::size_t alignment) {
        const std::size_t padding = (alignment - (static_cast<std::size_t>(pos_) % alignment)) % alignment;
        grow(padding);
        for (std::size_t i = 0; i < padding; ++i) buf_[pos_++] = 0;
    }

    std::vector<std::uint8_t> buf_;
    std::int64_t pos_ = 0;
    std::int64_t root_ = 0;
};

// ObjectBuilder writes one object's fixed payload and the tails its pointer
// fields name. It is a value: it holds where the object started, nothing else.
class ObjectBuilder {
  public:
    ObjectBuilder() = default;
    ObjectBuilder(Builder* b, std::int64_t start, std::int64_t data_size)
        : b_(b), start_(start), data_size_(data_size) {}

    void set_bool(std::int64_t field, bool v) { set_u8(field, v ? 1 : 0); }
    void set_u8(std::int64_t field, std::uint8_t v) {
        ensure_field(field + 1);
        b_->buf_[start_ + field] = v;
    }
    void set_u16(std::int64_t field, std::uint16_t v) {
        ensure_field(field + 2);
        store_u16(b_->buf_.data() + start_ + field, v);
    }
    void set_u32(std::int64_t field, std::uint32_t v) {
        ensure_field(field + 4);
        store_u32(b_->buf_.data() + start_ + field, v);
    }
    void set_u64(std::int64_t field, std::uint64_t v) {
        ensure_field(field + 8);
        store_u64(b_->buf_.data() + start_ + field, v);
    }
    void set_bytes_fixed(std::int64_t field, std::span<const std::uint8_t> v) {
        if (v.empty()) return;
        ensure_field(field + static_cast<std::int64_t>(v.size()));
        std::memcpy(b_->buf_.data() + start_ + field, v.data(), v.size());
    }
    void set_bytes(std::int64_t field, std::span<const std::uint8_t> v) {
        ensure_field(field + 8);
        std::uint8_t* cell = b_->buf_.data() + start_ + field;
        if (v.empty()) {
            store_u32(cell, 0);
            store_u32(cell + 4, 0);
            return;
        }
        const std::int64_t data_pos = b_->pos_;
        b_->grow(v.size());
        std::memcpy(b_->buf_.data() + b_->pos_, v.data(), v.size());
        b_->pos_ += static_cast<std::int64_t>(v.size());
        // grow may have reallocated; re-derive the cell.
        cell = b_->buf_.data() + start_ + field;
        const std::int64_t field_abs = start_ + field;
        store_u32(cell, static_cast<std::uint32_t>(static_cast<std::int32_t>(data_pos - field_abs)));
        store_u32(cell + 4, static_cast<std::uint32_t>(v.size()));
    }
    void set_text(std::int64_t field, std::string_view v) {
        set_bytes(field, {reinterpret_cast<const std::uint8_t*>(v.data()), v.size()});
    }
    void set_object(std::int64_t field, std::int64_t obj_offset) {
        ensure_field(field + 4);
        std::uint8_t* cell = b_->buf_.data() + start_ + field;
        if (obj_offset == 0) {
            store_u32(cell, 0);
            return;
        }
        store_u32(cell, static_cast<std::uint32_t>(static_cast<std::int32_t>(obj_offset - (start_ + field))));
    }
    void set_list(std::int64_t field, std::int64_t list_offset, std::int64_t length) {
        ensure_field(field + 8);
        std::uint8_t* cell = b_->buf_.data() + start_ + field;
        if (list_offset == 0 || length == 0) {
            store_u32(cell, 0);
            store_u32(cell + 4, 0);
            return;
        }
        store_u32(cell, static_cast<std::uint32_t>(static_cast<std::int32_t>(list_offset - (start_ + field))));
        store_u32(cell + 4, static_cast<std::uint32_t>(length));
    }

    void reserve_fixed(std::int64_t data_size) { ensure_field(data_size); }

    std::int64_t finish() const { return start_; }
    std::int64_t finish_as_root() const {
        b_->root_ = start_;
        return start_;
    }

  private:
    void ensure_field(std::int64_t end_offset) {
        const std::int64_t needed = start_ + end_offset;
        if (needed <= b_->pos_) return;
        b_->grow(static_cast<std::size_t>(needed - b_->pos_));
        for (std::int64_t i = b_->pos_; i < needed; ++i) b_->buf_[i] = 0;
        b_->pos_ = needed;
    }

    Builder* b_ = nullptr;
    std::int64_t start_ = 0;
    std::int64_t data_size_ = 0;
};

// ListBuilder appends elements. Note that add_bytes counts BYTES, matching the
// Go reference: a caller writing fixed-stride records supplies the real element
// count to set_list itself. Keeping the quirk is deliberate — a "fix" here
// silently changes the length field on the wire.
class ListBuilder {
  public:
    ListBuilder() = default;
    ListBuilder(Builder* b, std::int64_t start) : b_(b), start_(start) {}

    void add_u8(std::uint8_t v) {
        b_->grow(1);
        b_->buf_[b_->pos_++] = v;
        ++count_;
    }
    void add_u32(std::uint32_t v) {
        b_->grow(4);
        store_u32(b_->buf_.data() + b_->pos_, v);
        b_->pos_ += 4;
        ++count_;
    }
    void add_u64(std::uint64_t v) {
        b_->grow(8);
        store_u64(b_->buf_.data() + b_->pos_, v);
        b_->pos_ += 8;
        ++count_;
    }
    void add_bytes(std::span<const std::uint8_t> data) {
        b_->grow(data.size());
        if (!data.empty()) std::memcpy(b_->buf_.data() + b_->pos_, data.data(), data.size());
        b_->pos_ += static_cast<std::int64_t>(data.size());
        count_ += static_cast<std::int64_t>(data.size());
    }
    void add_object_ptr(std::int64_t target_pos) {
        b_->grow(4);
        if (target_pos == 0) {
            store_u32(b_->buf_.data() + b_->pos_, 0);
        } else {
            store_u32(b_->buf_.data() + b_->pos_,
                      static_cast<std::uint32_t>(static_cast<std::int32_t>(target_pos - b_->pos_)));
        }
        b_->pos_ += 4;
        ++count_;
    }

    std::int64_t offset() const { return start_; }
    std::int64_t count() const { return count_; }

  private:
    Builder* b_ = nullptr;
    std::int64_t start_ = 0;
    std::int64_t count_ = 0;
};

inline ObjectBuilder Builder::start_object(std::int64_t data_size) {
    align(kAlignment);
    ObjectBuilder ob(this, pos_, data_size);
    ob.reserve_fixed(data_size);
    return ob;
}

inline ListBuilder Builder::start_list(std::int64_t /*elem_size*/) {
    align(kAlignment);
    return ListBuilder(this, pos_);
}

}  // namespace lux::platformvm::zap
