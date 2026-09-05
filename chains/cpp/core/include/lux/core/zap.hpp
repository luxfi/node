// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// zap.hpp — ZAP, the zero-copy structural codec, and the ONLY serialization in
// this node. Ported field-for-field from the Go definition (luxfi/zap
// builder.go + zap.go), because the wire is a contract: a transaction built
// here must hash to the same id as one built in Go, or the two implementations
// are two chains.
//
// It lives here, above any one chain, for the same reason: two chains in one
// binary carrying two codecs is two answers to "what is this frame", which is a
// fork surface inside a single process. There is one answer.
//
//   header 16B: Magic "ZAP\0" | Version u16 | Flags u16 | RootOffset u32 | Size u32
//   body:       8-byte-aligned objects, lists and byte runs; every pointer is
//               RELATIVE to the position of the pointer field itself.
//
// All multi-byte integers are little-endian.
//
// READ SIDE IS A VIEW. Message/Object/List alias a caller-owned buffer and copy
// nothing; the buffer must outlive them. That is the point of the format, and it
// is why every tx type in this port caches its own wire bytes.

#pragma once

#include <cstdint>
#include <cstring>
#include <span>
#include <string>
#include <string_view>
#include <vector>

namespace lux::core::zap {

inline constexpr int kHeaderSize = 16;
inline constexpr int kAlignment = 8;
inline constexpr std::uint16_t kVersion1 = 1;
inline constexpr std::uint16_t kVersion2 = 2;
inline constexpr std::uint16_t kVersion = kVersion2;
inline constexpr char kMagic[4] = {'Z', 'A', 'P', '\0'};

// ---- little-endian primitives (one definition, used by both sides) ----

inline void put_u16(std::uint8_t* p, std::uint16_t v) {
    p[0] = std::uint8_t(v);
    p[1] = std::uint8_t(v >> 8);
}
inline void put_u32(std::uint8_t* p, std::uint32_t v) {
    p[0] = std::uint8_t(v);
    p[1] = std::uint8_t(v >> 8);
    p[2] = std::uint8_t(v >> 16);
    p[3] = std::uint8_t(v >> 24);
}
inline void put_u64(std::uint8_t* p, std::uint64_t v) {
    for (int i = 0; i < 8; ++i) p[i] = std::uint8_t(v >> (8 * i));
}
inline std::uint16_t get_u16(const std::uint8_t* p) {
    return std::uint16_t(p[0]) | std::uint16_t(std::uint16_t(p[1]) << 8);
}
inline std::uint32_t get_u32(const std::uint8_t* p) {
    return std::uint32_t(p[0]) | (std::uint32_t(p[1]) << 8) | (std::uint32_t(p[2]) << 16) |
           (std::uint32_t(p[3]) << 24);
}
inline std::uint64_t get_u64(const std::uint8_t* p) {
    std::uint64_t v = 0;
    for (int i = 7; i >= 0; --i) v = (v << 8) | p[i];
    return v;
}

class Builder;

// ObjectBuilder writes one object's fixed section. StartObject RESERVES the
// whole fixed section up front (zero-filled), so SetBytes/SetText can append
// their tail immediately after it and patch their own relative pointer on the
// spot — there is no deferred patch list, and the emitted bytes are identical to
// Go's.
class ObjectBuilder {
public:
    ObjectBuilder(Builder* b, int start_pos, int data_size)
        : b_(b), start_pos_(start_pos), data_size_(data_size) {}

    void set_bool(int off, bool v) { set_u8(off, v ? 1 : 0); }
    void set_u8(int off, std::uint8_t v);
    void set_u16(int off, std::uint16_t v);
    void set_u32(int off, std::uint32_t v);
    void set_u64(int off, std::uint64_t v);
    // SetBytesFixed: copies v IN PLACE inside the fixed section (a declared
    // fixed-width byte field). Empty is a no-op, as in Go.
    void set_bytes_fixed(int off, std::span<const std::uint8_t> v);
    // SetBytes: a variable-length tail — writes {relOffset u32, length u32} and
    // appends the payload at the builder cursor.
    void set_bytes(int off, std::span<const std::uint8_t> v);
    void set_text(int off, std::string_view v);
    void set_object(int off, int obj_offset);
    void set_list(int off, int list_offset, int length);
    void reserve_fixed(int data_size) { ensure_field(data_size); }

    int finish() const { return start_pos_; }
    int finish_as_root();

    int data_size() const { return data_size_; }

private:
    void ensure_field(int end_offset);

    Builder* b_;
    int start_pos_;
    int data_size_;
};

// ListBuilder appends list elements at the builder cursor. The stride is
// implicit in which add_* the caller invokes, exactly as in Go.
class ListBuilder {
public:
    ListBuilder(Builder* b, int start_pos) : b_(b), start_pos_(start_pos) {}

    void add_u8(std::uint8_t v);
    void add_u32(std::uint32_t v);
    void add_u64(std::uint64_t v);
    // add_bytes appends raw bytes and counts BYTES (not elements) — the Go
    // semantics every fixed-stride list writer relies on, which is why those
    // writers pass the element count to set_list themselves.
    void add_bytes(std::span<const std::uint8_t> v);
    // add_object_ptr appends a 4-byte SIGNED relative pointer to an object at
    // absolute position target_pos (0 = null element).
    void add_object_ptr(int target_pos);

    // finish returns (offset, count).
    std::pair<int, int> finish() const { return {start_pos_, count_}; }

private:
    Builder* b_;
    int start_pos_;
    int count_ = 0;
};

class Builder {
public:
    explicit Builder(int capacity = 256) {
        if (capacity < kHeaderSize) capacity = 256;
        buf_.assign(std::size_t(capacity), 0);
        pos_ = kHeaderSize;
        std::memcpy(buf_.data(), kMagic, 4);
        put_u16(buf_.data() + 4, kVersion);
    }

    void reset() {
        pos_ = kHeaderSize;
        root_offset_ = 0;
    }

    ObjectBuilder start_object(int data_size) {
        align(kAlignment);
        ObjectBuilder ob(this, pos_, data_size);
        ob.reserve_fixed(data_size);
        return ob;
    }

    ListBuilder start_list(int /*elem_size*/) {
        align(kAlignment);
        return ListBuilder(this, pos_);
    }

    int write_bytes(std::span<const std::uint8_t> data) {
        if (data.empty()) return 0;
        align(kAlignment);
        int offset = pos_;
        grow(int(data.size()));
        std::memcpy(buf_.data() + pos_, data.data(), data.size());
        pos_ += int(data.size());
        return offset;
    }

    // finish writes the root offset + total size and returns the message bytes.
    std::vector<std::uint8_t> finish() {
        put_u32(buf_.data() + 8, std::uint32_t(root_offset_));
        put_u32(buf_.data() + 12, std::uint32_t(pos_));
        return std::vector<std::uint8_t>(buf_.begin(), buf_.begin() + pos_);
    }

    // -- internals the two sub-builders drive --
    void grow(int n) {
        if (pos_ + n <= int(buf_.size())) return;
        std::size_t new_cap = buf_.size() * 2;
        if (new_cap < std::size_t(pos_ + n)) new_cap = std::size_t(pos_ + n);
        buf_.resize(new_cap, 0);
    }
    void align(int alignment) {
        int padding = (alignment - (pos_ % alignment)) % alignment;
        grow(padding);
        for (int i = 0; i < padding; ++i) buf_[std::size_t(pos_++)] = 0;
    }
    std::uint8_t* at(int off) { return buf_.data() + off; }
    int pos() const { return pos_; }
    void set_pos(int p) { pos_ = p; }
    void set_root(int off) { root_offset_ = off; }

private:
    std::vector<std::uint8_t> buf_;
    int pos_ = kHeaderSize;
    int root_offset_ = 0;
};

// ---- ObjectBuilder / ListBuilder bodies (need the complete Builder) ----

inline void ObjectBuilder::ensure_field(int end_offset) {
    int needed = start_pos_ + end_offset;
    if (needed > b_->pos()) {
        b_->grow(needed - b_->pos());
        for (int i = b_->pos(); i < needed; ++i) *b_->at(i) = 0;
        b_->set_pos(needed);
    }
}

inline void ObjectBuilder::set_u8(int off, std::uint8_t v) {
    ensure_field(off + 1);
    *b_->at(start_pos_ + off) = v;
}
inline void ObjectBuilder::set_u16(int off, std::uint16_t v) {
    ensure_field(off + 2);
    put_u16(b_->at(start_pos_ + off), v);
}
inline void ObjectBuilder::set_u32(int off, std::uint32_t v) {
    ensure_field(off + 4);
    put_u32(b_->at(start_pos_ + off), v);
}
inline void ObjectBuilder::set_u64(int off, std::uint64_t v) {
    ensure_field(off + 8);
    put_u64(b_->at(start_pos_ + off), v);
}
inline void ObjectBuilder::set_bytes_fixed(int off, std::span<const std::uint8_t> v) {
    if (v.empty()) return;
    ensure_field(off + int(v.size()));
    std::memcpy(b_->at(start_pos_ + off), v.data(), v.size());
}
inline void ObjectBuilder::set_bytes(int off, std::span<const std::uint8_t> v) {
    if (v.empty()) {
        put_u32(b_->at(start_pos_ + off), 0);
        put_u32(b_->at(start_pos_ + off + 4), 0);
        return;
    }
    int data_pos = b_->pos();
    b_->grow(int(v.size()));
    std::memcpy(b_->at(b_->pos()), v.data(), v.size());
    b_->set_pos(b_->pos() + int(v.size()));

    int field_abs = start_pos_ + off;
    std::int32_t rel = std::int32_t(data_pos - field_abs);
    put_u32(b_->at(field_abs), std::uint32_t(rel));
    put_u32(b_->at(field_abs + 4), std::uint32_t(v.size()));
}
inline void ObjectBuilder::set_text(int off, std::string_view v) {
    set_bytes(off, std::span<const std::uint8_t>(
                       reinterpret_cast<const std::uint8_t*>(v.data()), v.size()));
}
inline void ObjectBuilder::set_object(int off, int obj_offset) {
    ensure_field(off + 4);
    if (obj_offset == 0) {
        put_u32(b_->at(start_pos_ + off), 0);
        return;
    }
    std::int32_t rel = std::int32_t(obj_offset - (start_pos_ + off));
    put_u32(b_->at(start_pos_ + off), std::uint32_t(rel));
}
inline void ObjectBuilder::set_list(int off, int list_offset, int length) {
    ensure_field(off + 8);
    if (list_offset == 0 || length == 0) {
        put_u32(b_->at(start_pos_ + off), 0);
        put_u32(b_->at(start_pos_ + off + 4), 0);
        return;
    }
    std::int32_t rel = std::int32_t(list_offset - (start_pos_ + off));
    put_u32(b_->at(start_pos_ + off), std::uint32_t(rel));
    put_u32(b_->at(start_pos_ + off + 4), std::uint32_t(length));
}
inline int ObjectBuilder::finish_as_root() {
    int off = finish();
    b_->set_root(off);
    return off;
}

inline void ListBuilder::add_u8(std::uint8_t v) {
    b_->grow(1);
    *b_->at(b_->pos()) = v;
    b_->set_pos(b_->pos() + 1);
    ++count_;
}
inline void ListBuilder::add_u32(std::uint32_t v) {
    b_->grow(4);
    put_u32(b_->at(b_->pos()), v);
    b_->set_pos(b_->pos() + 4);
    ++count_;
}
inline void ListBuilder::add_u64(std::uint64_t v) {
    b_->grow(8);
    put_u64(b_->at(b_->pos()), v);
    b_->set_pos(b_->pos() + 8);
    ++count_;
}
inline void ListBuilder::add_bytes(std::span<const std::uint8_t> v) {
    b_->grow(int(v.size()));
    if (!v.empty()) std::memcpy(b_->at(b_->pos()), v.data(), v.size());
    b_->set_pos(b_->pos() + int(v.size()));
    count_ += int(v.size());
}
inline void ListBuilder::add_object_ptr(int target_pos) {
    b_->grow(4);
    if (target_pos == 0) {
        put_u32(b_->at(b_->pos()), 0);
    } else {
        std::int32_t rel = std::int32_t(target_pos - b_->pos());
        put_u32(b_->at(b_->pos()), std::uint32_t(rel));
    }
    b_->set_pos(b_->pos() + 4);
    ++count_;
}

// ================= read side =================
//
// Object and List are SELF-CONTAINED views (buffer pointer + size + position),
// not back-pointers into a Message. That is the one deliberate shape change from
// Go, and it is a C++ lifetime argument rather than a wire one: a Go Object
// holds *Message and the GC keeps it alive, while a C++ Object copied out of a
// local Message would dangle. The bytes read, and every bound checked, are
// identical.

class Object;

class List {
public:
    List() = default;
    List(const std::uint8_t* data, std::size_t size, int offset, int length)
        : data_(data), size_(size), offset_(offset), length_(length) {}

    int len() const { return length_; }
    bool is_null() const { return data_ == nullptr; }

    std::uint8_t u8(int i) const {
        if (i < 0 || i >= length_) return 0;
        std::size_t pos = std::size_t(offset_) + std::size_t(i);
        if (pos >= size_) return 0;
        return data_[pos];
    }
    std::uint32_t u32(int i) const {
        if (i < 0 || i >= length_) return 0;
        std::size_t pos = std::size_t(offset_) + std::size_t(i) * 4;
        if (pos + 4 > size_) return 0;
        return get_u32(data_ + pos);
    }
    std::uint64_t u64(int i) const {
        if (i < 0 || i >= length_) return 0;
        std::size_t pos = std::size_t(offset_) + std::size_t(i) * 8;
        if (pos + 8 > size_) return 0;
        return get_u64(data_ + pos);
    }
    // object(i, elem_size) — an INLINE fixed-stride element.
    inline Object object(int i, int elem_size) const;
    // object_ptr(i) — an OUT-OF-LINE element: a 4-byte SIGNED relative pointer,
    // dereferenced exactly as Object::object does.
    inline Object object_ptr(int i) const;

    std::span<const std::uint8_t> bytes() const {
        if (!data_ || std::size_t(offset_ + length_) > size_) return {};
        return {data_ + offset_, std::size_t(length_)};
    }

private:
    const std::uint8_t* data_ = nullptr;
    std::size_t size_ = 0;
    int offset_ = 0;
    int length_ = 0;
};

class Object {
public:
    Object() = default;
    Object(const std::uint8_t* data, std::size_t size, int offset)
        : data_(data), size_(size), offset_(offset) {}

    bool is_null() const { return data_ == nullptr || offset_ == 0; }
    int offset() const { return offset_; }
    const std::uint8_t* buffer() const { return data_; }
    std::size_t buffer_size() const { return size_; }

    bool boolean(int off) const { return u8(off) != 0; }

    std::uint8_t u8(int off) const {
        if (!data_) return 0;
        std::int64_t pos = offset_ + off;
        if (pos < 0 || std::uint64_t(pos) >= size_) return 0;
        return data_[pos];
    }
    std::uint16_t u16(int off) const {
        if (!data_) return 0;
        std::int64_t pos = offset_ + off;
        if (pos < 0 || std::uint64_t(pos) + 2 > size_) return 0;
        return get_u16(data_ + pos);
    }
    std::uint32_t u32(int off) const {
        if (!data_) return 0;
        std::int64_t pos = offset_ + off;
        if (pos < 0 || std::uint64_t(pos) + 4 > size_) return 0;
        return get_u32(data_ + pos);
    }
    std::uint64_t u64(int off) const {
        if (!data_) return 0;
        std::int64_t pos = offset_ + off;
        if (pos < 0 || std::uint64_t(pos) + 8 > size_) return 0;
        return get_u64(data_ + pos);
    }

    // n inline bytes at off; an empty span when the span leaves the buffer.
    std::span<const std::uint8_t> bytes_fixed_slice(int off, int n) const {
        if (!data_ || n <= 0) return {};
        std::int64_t pos = offset_ + off;
        if (pos < 0 || std::uint64_t(pos) + std::uint64_t(n) > size_) return {};
        return {data_ + pos, std::size_t(n)};
    }

    // Wire-format rule: a bytes relOffset is an UNSIGNED forward pointer, and a
    // target inside the wire header is rejected — that is the pointer-escape
    // gate, and it is why a crafted relOffset cannot alias the fixed section.
    std::span<const std::uint8_t> bytes(int off) const {
        if (!data_) return {};
        std::int64_t pos = offset_ + off;
        if (pos < 0 || std::uint64_t(pos) + 4 > size_) return {};
        std::uint32_t rel = get_u32(data_ + pos);
        if (rel == 0) return {};
        std::uint64_t len_pos = std::uint64_t(pos) + 4;
        if (len_pos + 4 > size_) return {};
        std::uint32_t length = get_u32(data_ + len_pos);
        std::uint64_t abs_pos = std::uint64_t(pos) + rel;
        if (abs_pos < std::uint64_t(kHeaderSize)) return {};
        if (abs_pos + length > size_) return {};
        return {data_ + abs_pos, std::size_t(length)};
    }

    std::string_view text(int off) const {
        auto b = bytes(off);
        if (b.empty()) return {};
        return std::string_view(reinterpret_cast<const char*>(b.data()), b.size());
    }

    // Wire-format rule: an object relOffset is SIGNED — a builder may finalize a
    // nested object before its parent — and any target inside the header is
    // rejected.
    Object object(int off) const {
        if (!data_) return {};
        std::int64_t pos = offset_ + off;
        if (pos < 0 || std::uint64_t(pos) + 4 > size_) return {};
        std::int32_t rel = std::int32_t(get_u32(data_ + pos));
        if (rel == 0) return {};
        std::int64_t abs = pos + rel;
        if (abs < kHeaderSize || std::uint64_t(abs) >= size_) return {};
        return Object(data_, size_, int(abs));
    }

    List list(int off) const {
        if (!data_) return {};
        std::int64_t pos = offset_ + off;
        if (pos < 0 || std::uint64_t(pos) + 8 > size_) return {};
        std::int32_t rel = std::int32_t(get_u32(data_ + pos));
        if (rel == 0) return {};
        std::uint32_t length = get_u32(data_ + pos + 4);
        if (std::uint64_t(length) > size_) return {};
        std::int64_t abs = pos + rel;
        if (abs < kHeaderSize || std::uint64_t(abs) >= size_) return {};
        return List(data_, size_, int(abs), int(length));
    }

    // list_stride applies the tighter clamp length*min_stride <= what remains of
    // the buffer, so an attacker-set length is rejected up front rather than at
    // every per-element accessor.
    List list_stride(int off, std::uint32_t min_stride) const {
        if (!data_) return {};
        std::int64_t pos = offset_ + off;
        if (pos < 0 || std::uint64_t(pos) + 8 > size_) return {};
        std::int32_t rel = std::int32_t(get_u32(data_ + pos));
        if (rel == 0) return {};
        std::uint32_t length = get_u32(data_ + pos + 4);
        std::int64_t abs = pos + rel;
        if (abs < kHeaderSize || std::uint64_t(abs) >= size_) return {};
        std::uint64_t buf_rem = size_ - std::uint64_t(abs);
        if (min_stride > 0) {
            if (std::uint64_t(length) * std::uint64_t(min_stride) > buf_rem) return {};
        } else if (std::uint64_t(length) > std::uint64_t(size_)) {
            return {};
        }
        return List(data_, size_, int(abs), int(length));
    }

private:
    const std::uint8_t* data_ = nullptr;
    std::size_t size_ = 0;
    int offset_ = 0;
};

inline Object List::object(int i, int elem_size) const {
    if (i < 0 || i >= length_) return {};
    return Object(data_, size_, offset_ + i * elem_size);
}
inline Object List::object_ptr(int i) const {
    if (i < 0 || i >= length_) return {};
    std::size_t pos = std::size_t(offset_) + std::size_t(i) * 4;
    if (pos + 4 > size_) return {};
    std::int32_t rel = std::int32_t(get_u32(data_ + pos));
    if (rel == 0) return {};
    std::int64_t abs = std::int64_t(pos) + rel;
    if (abs < kHeaderSize || std::uint64_t(abs) >= size_) return {};
    return Object(data_, size_, int(abs));
}

// Message is a VIEW over caller-owned bytes: it validates the header once and
// hands out Objects. The buffer must outlive every view taken from it.
class Message {
public:
    Message() = default;

    // parse validates magic, version and the declared size, exactly as Go's
    // zap.Parse does, and says why on a violation instead of returning a
    // half-valid message.
    static bool parse(std::span<const std::uint8_t> data, Message* out, std::string* err) {
        if (data.size() < std::size_t(kHeaderSize)) {
            if (err) *err = "zap: buffer too small";
            return false;
        }
        if (std::memcmp(data.data(), kMagic, 4) != 0) {
            if (err) *err = "zap: invalid magic bytes";
            return false;
        }
        std::uint16_t version = get_u16(data.data() + 4);
        if (version != kVersion1 && version != kVersion2) {
            if (err) *err = "zap: unsupported version";
            return false;
        }
        std::uint32_t size = get_u32(data.data() + 12);
        if (int(size) < kHeaderSize || std::size_t(size) > data.size()) {
            if (err) *err = "zap: buffer too small";
            return false;
        }
        *out = Message(data.data(), std::size_t(size));
        return true;
    }

    bool valid() const { return data_ != nullptr; }
    std::uint16_t version() const { return get_u16(data_ + 4); }
    std::uint16_t flags() const { return get_u16(data_ + 6); }
    std::size_t size() const { return size_; }
    std::span<const std::uint8_t> bytes() const { return {data_, size_}; }
    const std::uint8_t* data() const { return data_; }

    Object root() const { return Object(data_, size_, int(get_u32(data_ + 8))); }

private:
    Message(const std::uint8_t* d, std::size_t n) : data_(d), size_(n) {}

    const std::uint8_t* data_ = nullptr;
    std::size_t size_ = 0;
};

}  // namespace lux::core::zap
