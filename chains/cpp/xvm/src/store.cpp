// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/xvm/store.hpp"

#include "lux/xvm/zap.hpp"

#include <fcntl.h>
#include <unistd.h>

#include <cerrno>
#include <cstring>
#include <vector>

namespace lux::xvm::store {
namespace {

// The log record's object: five sections, all packed lists over one blob — the
// same shape every variable section in this chain uses.
constexpr int kOffOps = 0;       // bytes: one per row, 1 = put, 0 = erase
constexpr int kOffKeyLens = 8;   // u32 list
constexpr int kOffKeyBlob = 16;  // bytes
constexpr int kOffValLens = 24;  // u32 list
constexpr int kOffValBlob = 32;  // bytes
constexpr int kSize = 40;

constexpr std::uint32_t kU32Stride = 4;

// The log is rewritten when it holds this much more than the rows need. Two is
// the usual amortization: every row is rewritten at most once per doubling.
constexpr std::size_t kCompactRatio = 2;
constexpr std::size_t kCompactFloor = 64 * 1024;

Bytes to_bytes(ByteView v) { return Bytes(v.begin(), v.end()); }

bool has_prefix(const Bytes& key, ByteView prefix) {
    if (key.size() < prefix.size()) return false;
    return std::memcmp(key.data(), prefix.data(), prefix.size()) == 0;
}

int write_u32_list(zap::Builder& b, const std::vector<std::uint32_t>& xs) {
    auto lb = b.start_list(int(kU32Stride));
    for (std::uint32_t x : xs) lb.add_u32(x);
    return lb.finish().first;
}

std::vector<std::uint32_t> read_u32_list(const zap::Object& o, int ptr_off) {
    auto l = o.list_stride(ptr_off, kU32Stride);
    std::vector<std::uint32_t> out(std::size_t(l.len()));
    for (int i = 0; i < l.len(); ++i) out[std::size_t(i)] = l.u32(i);
    return out;
}

}  // namespace

// ================= the record =================

Bytes encode_batch(const std::map<Bytes, std::optional<Bytes>>& batch) {
    Bytes ops, key_blob, val_blob;
    std::vector<std::uint32_t> key_lens, val_lens;
    ops.reserve(batch.size());
    key_lens.reserve(batch.size());
    val_lens.reserve(batch.size());

    for (const auto& [key, val] : batch) {
        ops.push_back(val.has_value() ? 1 : 0);
        key_lens.push_back(std::uint32_t(key.size()));
        key_blob.insert(key_blob.end(), key.begin(), key.end());
        const std::size_t n = val.has_value() ? val->size() : 0;
        val_lens.push_back(std::uint32_t(n));
        if (val.has_value()) val_blob.insert(val_blob.end(), val->begin(), val->end());
    }

    zap::Builder b(zap::kHeaderSize + kSize + int(ops.size()) + int(key_blob.size()) +
                   int(val_blob.size()) + 8 * int(batch.size()) + 64);
    const int key_lens_off = write_u32_list(b, key_lens);
    const int val_lens_off = write_u32_list(b, val_lens);

    auto ob = b.start_object(kSize);
    ob.set_bytes(kOffOps, view(ops));
    ob.set_list(kOffKeyLens, key_lens_off, int(key_lens.size()));
    ob.set_bytes(kOffKeyBlob, view(key_blob));
    ob.set_list(kOffValLens, val_lens_off, int(val_lens.size()));
    ob.set_bytes(kOffValBlob, view(val_blob));
    ob.finish_as_root();
    return b.finish();
}

Result<std::map<Bytes, std::optional<Bytes>>> decode_batch(ByteView record) {
    zap::Message msg;
    std::string err;
    if (!zap::Message::parse(record, &msg, &err))
        return std::unexpected(std::string(kErrCorruptRecord) + ": " + err);

    const zap::Object root = msg.root();
    const auto ops = root.bytes(kOffOps);
    const auto key_lens = read_u32_list(root, kOffKeyLens);
    const auto key_blob = root.bytes(kOffKeyBlob);
    const auto val_lens = read_u32_list(root, kOffValLens);
    const auto val_blob = root.bytes(kOffValBlob);

    const std::size_t n = ops.size();
    if (key_lens.size() != n || val_lens.size() != n)
        return std::unexpected(std::string(kErrCorruptRecord) + ": row count mismatch");

    std::map<Bytes, std::optional<Bytes>> out;
    std::size_t k_pos = 0, v_pos = 0;
    for (std::size_t i = 0; i < n; ++i) {
        const std::size_t k_len = key_lens[i];
        const std::size_t v_len = val_lens[i];
        if (k_pos + k_len > key_blob.size() || v_pos + v_len > val_blob.size())
            return std::unexpected(std::string(kErrCorruptRecord) + ": row " +
                                   std::to_string(i) + " out of bounds");
        Bytes key = to_bytes(key_blob.subspan(k_pos, k_len));
        k_pos += k_len;
        if (ops[i] != 0) {
            out[std::move(key)] = to_bytes(val_blob.subspan(v_pos, v_len));
        } else {
            out[std::move(key)] = std::nullopt;
        }
        v_pos += v_len;
    }
    return out;
}

// ================= Memory =================

std::optional<Bytes> Memory::get(ByteView key) const {
    auto it = rows_.find(to_bytes(key));
    if (it == rows_.end()) return std::nullopt;
    return it->second;
}

void Memory::put(ByteView key, ByteView value) { rows_[to_bytes(key)] = to_bytes(value); }

void Memory::erase(ByteView key) { rows_.erase(to_bytes(key)); }

void Memory::each(ByteView prefix,
                  const std::function<bool(ByteView, ByteView)>& f) const {
    for (auto it = rows_.lower_bound(to_bytes(prefix)); it != rows_.end(); ++it) {
        if (!has_prefix(it->first, prefix)) break;
        if (!f(view(it->first), view(it->second))) return;
    }
}

// ================= File =================

File::~File() {
    if (fd_ >= 0) ::close(fd_);
}

Result<std::unique_ptr<File>> File::open(const std::string& path) {
    const int fd = ::open(path.c_str(), O_RDWR | O_CREAT | O_CLOEXEC, 0600);
    if (fd < 0)
        return std::unexpected(std::string(kErrOpen) + " " + path + ": " +
                               std::strerror(errno));
    std::unique_ptr<File> f(new File(path, fd));
    if (auto r = f->replay(); !r) return std::unexpected(r.error());
    return f;
}

Result<void> File::replay() {
    // Read the whole log. It is the only read of the file's life, and the map it
    // builds is what every later get answers from.
    Bytes buf;
    const off_t end = ::lseek(fd_, 0, SEEK_END);
    if (end < 0)
        return std::unexpected(std::string(kErrOpen) + " " + path_ + ": " +
                               std::strerror(errno));
    buf.resize(std::size_t(end));
    if (end > 0) {
        if (::lseek(fd_, 0, SEEK_SET) < 0)
            return std::unexpected(std::string(kErrOpen) + " " + path_ + ": " +
                                   std::strerror(errno));
        std::size_t got = 0;
        while (got < buf.size()) {
            const ssize_t n = ::read(fd_, buf.data() + got, buf.size() - got);
            if (n < 0)
                return std::unexpected(std::string(kErrOpen) + " " + path_ + ": " +
                                       std::strerror(errno));
            if (n == 0) break;
            got += std::size_t(n);
        }
        buf.resize(got);
    }

    std::size_t cursor = 0;
    while (cursor < buf.size()) {
        ByteView rest(buf.data() + cursor, buf.size() - cursor);
        zap::Message msg;
        std::string err;
        // A trailing record that does not parse is a commit that was
        // interrupted. It never happened, so the log is cut back to the last
        // record that did.
        if (!zap::Message::parse(rest, &msg, &err)) break;
        const std::size_t len = msg.size();
        if (len == 0 || len > rest.size()) break;
        auto batch = decode_batch(rest.subspan(0, len));
        if (!batch) break;
        for (auto& [key, val] : *batch) {
            if (val.has_value()) {
                auto it = rows_.find(key);
                if (it != rows_.end()) {
                    live_bytes_ -= it->first.size() + it->second.size();
                }
                live_bytes_ += key.size() + val->size();
                rows_[key] = std::move(*val);
            } else {
                auto it = rows_.find(key);
                if (it != rows_.end()) {
                    live_bytes_ -= it->first.size() + it->second.size();
                    rows_.erase(it);
                }
            }
        }
        cursor += len;
    }

    log_bytes_ = cursor;
    if (cursor != buf.size()) {
        if (::ftruncate(fd_, off_t(cursor)) != 0)
            return std::unexpected(std::string(kErrWrite) + " " + path_ + ": " +
                                   std::strerror(errno));
    }
    if (::lseek(fd_, off_t(cursor), SEEK_SET) < 0)
        return std::unexpected(std::string(kErrOpen) + " " + path_ + ": " +
                               std::strerror(errno));
    return {};
}

std::optional<Bytes> File::get(ByteView key) const {
    auto it = rows_.find(to_bytes(key));
    if (it == rows_.end()) return std::nullopt;
    return it->second;
}

void File::put(ByteView key, ByteView value) {
    Bytes k = to_bytes(key);
    auto it = rows_.find(k);
    if (it != rows_.end()) {
        live_bytes_ -= it->first.size() + it->second.size();
        it->second = to_bytes(value);
        live_bytes_ += it->first.size() + it->second.size();
    } else {
        live_bytes_ += k.size() + value.size();
        rows_[k] = to_bytes(value);
    }
    staged_[std::move(k)] = to_bytes(value);
}

void File::erase(ByteView key) {
    Bytes k = to_bytes(key);
    auto it = rows_.find(k);
    if (it != rows_.end()) {
        live_bytes_ -= it->first.size() + it->second.size();
        rows_.erase(it);
    }
    staged_[std::move(k)] = std::nullopt;
}

void File::each(ByteView prefix, const std::function<bool(ByteView, ByteView)>& f) const {
    for (auto it = rows_.lower_bound(to_bytes(prefix)); it != rows_.end(); ++it) {
        if (!has_prefix(it->first, prefix)) break;
        if (!f(view(it->first), view(it->second))) return;
    }
}

Result<void> File::append(const Bytes& record) {
    std::size_t written = 0;
    while (written < record.size()) {
        const ssize_t n = ::write(fd_, record.data() + written, record.size() - written);
        if (n < 0) {
            if (errno == EINTR) continue;
            return std::unexpected(std::string(kErrWrite) + " " + path_ + ": " +
                                   std::strerror(errno));
        }
        written += std::size_t(n);
    }
    if (::fsync(fd_) != 0)
        return std::unexpected(std::string(kErrWrite) + " " + path_ + ": " +
                               std::strerror(errno));
    log_bytes_ += record.size();
    return {};
}

Result<void> File::commit() {
    if (staged_.empty()) return {};
    const Bytes record = encode_batch(staged_);
    if (auto r = append(record); !r) return r;
    staged_.clear();

    if (log_bytes_ > kCompactFloor && log_bytes_ > kCompactRatio * live_bytes_)
        return compact();
    return {};
}

Result<void> File::compact() {
    // One record holding every row, written beside the log and renamed over it.
    // The rename is the atomic point: either the old log or the whole new one is
    // what a later open finds, never a mixture.
    std::map<Bytes, std::optional<Bytes>> all;
    for (const auto& [key, val] : rows_) all[key] = val;
    const Bytes record = encode_batch(all);

    const std::string tmp = path_ + ".compact";
    const int fd = ::open(tmp.c_str(), O_RDWR | O_CREAT | O_TRUNC | O_CLOEXEC, 0600);
    if (fd < 0)
        return std::unexpected(std::string(kErrWrite) + " " + tmp + ": " +
                               std::strerror(errno));
    std::size_t written = 0;
    while (written < record.size()) {
        const ssize_t n = ::write(fd, record.data() + written, record.size() - written);
        if (n < 0) {
            if (errno == EINTR) continue;
            const std::string why = std::strerror(errno);
            ::close(fd);
            ::unlink(tmp.c_str());
            return std::unexpected(std::string(kErrWrite) + " " + tmp + ": " + why);
        }
        written += std::size_t(n);
    }
    if (::fsync(fd) != 0) {
        const std::string why = std::strerror(errno);
        ::close(fd);
        ::unlink(tmp.c_str());
        return std::unexpected(std::string(kErrWrite) + " " + tmp + ": " + why);
    }
    ::close(fd);
    if (::rename(tmp.c_str(), path_.c_str()) != 0) {
        const std::string why = std::strerror(errno);
        ::unlink(tmp.c_str());
        return std::unexpected(std::string(kErrWrite) + " " + path_ + ": " + why);
    }

    ::close(fd_);
    fd_ = ::open(path_.c_str(), O_RDWR | O_CLOEXEC);
    if (fd_ < 0)
        return std::unexpected(std::string(kErrOpen) + " " + path_ + ": " +
                               std::strerror(errno));
    if (::lseek(fd_, 0, SEEK_END) < 0)
        return std::unexpected(std::string(kErrOpen) + " " + path_ + ": " +
                               std::strerror(errno));
    log_bytes_ = record.size();
    return {};
}

}  // namespace lux::xvm::store
