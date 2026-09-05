// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/fhevm/store.hpp"

#include "lux/zap/zap.hpp"

#include <fcntl.h>
#include <unistd.h>

#include <cstring>
#include <vector>

namespace lux::fhevm {
namespace {

// The log record's one object: a list of {key, value, present} entries. present
// distinguishes an erase from a write of empty bytes.
constexpr int kBatchEntries = 0;
constexpr int kBatchSize = 8;
constexpr int kEntryKey = 0;
constexpr int kEntryValue = 8;
constexpr int kEntryPresent = 16;
constexpr int kEntrySize = 24;

Bytes to_bytes(ByteView v) { return Bytes(v.begin(), v.end()); }

}  // namespace

Result<bool> Store::has(ByteView k) const {
    auto v = get(k);
    if (!v) return std::unexpected(v.error());
    return v->has_value();
}

Bytes key(std::string_view prefix, ByteView name) {
    Bytes k(prefix.begin(), prefix.end());
    k.insert(k.end(), name.begin(), name.end());
    return k;
}

Bytes key(std::string_view prefix, std::uint64_t name) {
    Bytes k(prefix.begin(), prefix.end());
    for (int i = 0; i < 8; ++i) k.push_back(std::uint8_t(name >> (56 - 8 * i)));
    return k;
}

// ---- Memory ------------------------------------------------------------------

Result<std::optional<Bytes>> Memory::get(ByteView k) const {
    Bytes kb = to_bytes(k);
    auto s = staged_.find(kb);
    if (s != staged_.end()) return s->second;
    auto it = rows_.find(kb);
    if (it == rows_.end()) return std::optional<Bytes>{};
    return std::optional<Bytes>(it->second);
}

Result<void> Memory::put(ByteView k, ByteView v) {
    staged_[to_bytes(k)] = to_bytes(v);
    return {};
}

Result<void> Memory::erase(ByteView k) {
    staged_[to_bytes(k)] = std::nullopt;
    return {};
}

Result<void> Memory::each(ByteView prefix,
                          const std::function<bool(ByteView, ByteView)>& f) const {
    // The staged rows shadow the committed ones, so the walk is over their
    // merge — a block reads what it just wrote, in key order, exactly as the
    // committed store would have served it.
    Rows merged;
    Bytes p = to_bytes(prefix);
    auto matches = [&](const Bytes& k) {
        return k.size() >= p.size() && std::equal(p.begin(), p.end(), k.begin());
    };
    for (const auto& [k, v] : rows_) {
        if (matches(k)) merged[k] = v;
    }
    for (const auto& [k, v] : staged_) {
        if (!matches(k)) continue;
        if (v.has_value()) {
            merged[k] = *v;
        } else {
            merged.erase(k);
        }
    }
    for (const auto& [k, v] : merged) {
        if (!f(view(k), view(v))) break;
    }
    return {};
}

Result<void> Memory::commit() {
    for (const auto& [k, v] : staged_) {
        if (v.has_value()) {
            rows_[k] = *v;
        } else {
            rows_.erase(k);
        }
    }
    staged_.clear();
    return {};
}

void Memory::abort() { staged_.clear(); }

// ---- the log record ----------------------------------------------------------

Bytes encode_batch(const Staged& batch) {
    zap::Builder b(256 + int(batch.size()) * 64);
    std::vector<int> entry_offsets;
    entry_offsets.reserve(batch.size());
    for (const auto& [k, v] : batch) {
        auto ob = b.start_object(kEntrySize);
        ob.set_bytes(kEntryKey, view(k));
        ob.set_bytes(kEntryValue, v.has_value() ? view(*v) : ByteView{});
        ob.set_u8(kEntryPresent, v.has_value() ? 1 : 0);
        entry_offsets.push_back(ob.finish());
    }
    auto lb = b.start_list(4);
    for (int off : entry_offsets) lb.add_object_ptr(off);
    auto [list_off, list_len] = lb.finish();

    auto root = b.start_object(kBatchSize);
    root.set_list(kBatchEntries, list_off, list_len);
    root.finish_as_root();
    return b.finish();
}

Result<Staged> decode_batch(ByteView record) {
    zap::Message msg;
    std::string err;
    if (!zap::Message::parse(record, &msg, &err)) return fail(Err::Database, err);
    if (msg.size() != record.size()) return fail(Err::Database, "store record trailing bytes");
    zap::Object root = msg.root();
    zap::List entries = root.list(kBatchEntries);
    Staged out;
    for (int i = 0; i < entries.len(); ++i) {
        zap::Object e = entries.object_ptr(i);
        if (e.is_null()) return fail(Err::Database, "store record does not parse");
        Bytes k = to_bytes(e.bytes(kEntryKey));
        if (e.u8(kEntryPresent) != 0) {
            out[k] = to_bytes(e.bytes(kEntryValue));
        } else {
            out[k] = std::nullopt;
        }
    }
    return out;
}

// ---- File --------------------------------------------------------------------

Result<std::unique_ptr<File>> File::open(const std::string& path) {
    int fd = ::open(path.c_str(), O_RDWR | O_CREAT, 0600);
    if (fd < 0) return fail(Err::Database, "cannot open store " + path);
    std::unique_ptr<File> f(new File(path, fd));
    auto r = f->replay();
    if (!r) return std::unexpected(r.error());
    return f;
}

File::~File() {
    if (fd_ >= 0) ::close(fd_);
}

Result<void> File::replay() {
    off_t end = ::lseek(fd_, 0, SEEK_END);
    if (end < 0) return fail(Err::Database, "cannot read store");
    if (end == 0) return {};
    Bytes all;
    all.resize(std::size_t(end));
    if (::lseek(fd_, 0, SEEK_SET) < 0) return fail(Err::Database, "cannot read store");
    std::size_t got = 0;
    while (got < all.size()) {
        ssize_t n = ::read(fd_, all.data() + got, all.size() - got);
        if (n <= 0) return fail(Err::Database, "cannot read store");
        got += std::size_t(n);
    }

    std::size_t pos = 0;
    while (pos + std::size_t(zap::kHeaderSize) <= all.size()) {
        std::uint32_t len = zap::get_u32(all.data() + pos + 12);
        // A record that does not fit is the tail of a commit that never
        // finished: it never happened, so the log is truncated to before it.
        if (len < std::uint32_t(zap::kHeaderSize) || pos + len > all.size()) break;
        auto batch = decode_batch(ByteView(all.data() + pos, len));
        if (!batch) break;
        for (const auto& [k, v] : *batch) {
            if (v.has_value()) {
                rows_[k] = *v;
            } else {
                rows_.erase(k);
            }
        }
        pos += len;
    }
    if (pos != all.size()) {
        if (::ftruncate(fd_, off_t(pos)) != 0) return fail(Err::Database, "cannot write store");
    }
    if (::lseek(fd_, off_t(pos), SEEK_SET) < 0) return fail(Err::Database, "cannot write store");
    return {};
}

Result<std::optional<Bytes>> File::get(ByteView k) const {
    Bytes kb = to_bytes(k);
    auto s = staged_.find(kb);
    if (s != staged_.end()) return s->second;
    auto it = rows_.find(kb);
    if (it == rows_.end()) return std::optional<Bytes>{};
    return std::optional<Bytes>(it->second);
}

Result<void> File::put(ByteView k, ByteView v) {
    staged_[to_bytes(k)] = to_bytes(v);
    return {};
}

Result<void> File::erase(ByteView k) {
    staged_[to_bytes(k)] = std::nullopt;
    return {};
}

Result<void> File::each(ByteView prefix, const std::function<bool(ByteView, ByteView)>& f) const {
    Rows merged;
    Bytes p = to_bytes(prefix);
    auto matches = [&](const Bytes& k) {
        return k.size() >= p.size() && std::equal(p.begin(), p.end(), k.begin());
    };
    for (const auto& [k, v] : rows_) {
        if (matches(k)) merged[k] = v;
    }
    for (const auto& [k, v] : staged_) {
        if (!matches(k)) continue;
        if (v.has_value()) {
            merged[k] = *v;
        } else {
            merged.erase(k);
        }
    }
    for (const auto& [k, v] : merged) {
        if (!f(view(k), view(v))) break;
    }
    return {};
}

Result<void> File::commit() {
    if (staged_.empty()) return {};
    Bytes record = encode_batch(staged_);
    std::size_t written = 0;
    while (written < record.size()) {
        ssize_t n = ::write(fd_, record.data() + written, record.size() - written);
        if (n <= 0) return fail(Err::Database, "cannot write store");
        written += std::size_t(n);
    }
    if (::fsync(fd_) != 0) return fail(Err::Database, "cannot write store");
    for (const auto& [k, v] : staged_) {
        if (v.has_value()) {
            rows_[k] = *v;
        } else {
            rows_.erase(k);
        }
    }
    staged_.clear();
    return {};
}

void File::abort() { staged_.clear(); }

}  // namespace lux::fhevm
