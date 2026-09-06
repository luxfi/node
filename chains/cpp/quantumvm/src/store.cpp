// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/quantumvm/store.hpp"

#include <zap/zap.hpp>

#include <fcntl.h>
#include <unistd.h>

#include <cerrno>
#include <cstring>
#include <vector>

namespace lux::quantumvm::store {
namespace {

// The log record: five sections, all packed lists over one blob.
constexpr std::int64_t kOffOps = 0;       // bytes: one per row, 1 = put, 0 = delete
constexpr std::int64_t kOffKeyLens = 8;   // u32 list
constexpr std::int64_t kOffKeyBlob = 16;  // bytes
constexpr std::int64_t kOffValLens = 24;  // u32 list
constexpr std::int64_t kOffValBlob = 32;  // bytes
constexpr std::int64_t kRecordSize = 40;

// The log is rewritten when it holds this much more than the rows need. Two is
// the usual amortization: every row is rewritten at most once per doubling.
constexpr std::size_t kCompactRatio = 2;
constexpr std::size_t kCompactFloor = 64 * 1024;

Bytes to_bytes(ByteView v) { return Bytes(v.begin(), v.end()); }

std::int64_t write_u32_list(zap::Builder& b, const std::vector<std::uint32_t>& xs) {
    auto lb = b.start_list(4);
    for (std::uint32_t x : xs) lb.add_u32(x);
    return lb.finish().first;
}

std::vector<std::uint32_t> read_u32_list(const zap::Object& o, std::int64_t field) {
    const zap::List l = o.list_stride(field, 4);
    std::vector<std::uint32_t> out(static_cast<std::size_t>(l.size() < 0 ? 0 : l.size()));
    for (int i = 0; i < l.size(); ++i) out[static_cast<std::size_t>(i)] = l.u32(i);
    return out;
}

Status errno_fail(Err code, const std::string& what) {
    return fail(code, what + ": " + std::strerror(errno));
}

}  // namespace

Bytes encode_batch(const Batch& batch) {
    Bytes ops, key_blob, val_blob;
    std::vector<std::uint32_t> key_lens, val_lens;
    ops.reserve(batch.size());
    key_lens.reserve(batch.size());
    val_lens.reserve(batch.size());

    for (const auto& [key, val] : batch) {
        ops.push_back(val.has_value() ? 1 : 0);
        key_lens.push_back(static_cast<std::uint32_t>(key.size()));
        key_blob.insert(key_blob.end(), key.begin(), key.end());
        val_lens.push_back(static_cast<std::uint32_t>(val ? val->size() : 0));
        if (val) val_blob.insert(val_blob.end(), val->begin(), val->end());
    }

    zap::Builder b(zap::kHeaderSize + kRecordSize + ops.size() + key_blob.size() + val_blob.size() +
                   8 * batch.size() + 64);
    const std::int64_t key_lens_off = write_u32_list(b, key_lens);
    const std::int64_t val_lens_off = write_u32_list(b, val_lens);

    auto ob = b.start_object(kRecordSize);
    ob.set_bytes(kOffOps, view(ops));
    ob.set_list(kOffKeyLens, key_lens_off, static_cast<std::int64_t>(key_lens.size()));
    ob.set_bytes(kOffKeyBlob, view(key_blob));
    ob.set_list(kOffValLens, val_lens_off, static_cast<std::int64_t>(val_lens.size()));
    ob.set_bytes(kOffValBlob, view(val_blob));
    ob.finish_as_root();
    return b.finish();
}

Result<Batch> decode_batch(ByteView record) {
    const auto msg = zap::Message::parse(record);
    if (!msg) return fail(Err::StoreCorrupt, "record does not parse");

    const zap::Object root = msg->root();
    const auto ops = root.bytes(kOffOps);
    const auto key_lens = read_u32_list(root, kOffKeyLens);
    const auto key_blob = root.bytes(kOffKeyBlob);
    const auto val_lens = read_u32_list(root, kOffValLens);
    const auto val_blob = root.bytes(kOffValBlob);

    const std::size_t n = ops.size();
    if (key_lens.size() != n || val_lens.size() != n)
        return fail(Err::StoreCorrupt, "row count mismatch");

    Batch out;
    std::size_t k_pos = 0, v_pos = 0;
    for (std::size_t i = 0; i < n; ++i) {
        const std::size_t k_len = key_lens[i];
        const std::size_t v_len = val_lens[i];
        if (k_pos + k_len > key_blob.size() || v_pos + v_len > val_blob.size())
            return fail(Err::StoreCorrupt, "row " + std::to_string(i) + " out of bounds");
        Bytes key = to_bytes(key_blob.subspan(k_pos, k_len));
        k_pos += k_len;
        if (ops[i] != 0)
            out[std::move(key)] = to_bytes(val_blob.subspan(v_pos, v_len));
        else
            out[std::move(key)] = std::nullopt;
        v_pos += v_len;
    }
    return out;
}

// ── Memory

Result<Bytes> Memory::get(ByteView key) const {
    if (closed_) return fail(Err::StoreClosed);
    auto it = rows_.find(to_bytes(key));
    if (it == rows_.end()) return fail(Err::NotFound);
    return it->second;
}

Status Memory::write(const Batch& batch) {
    if (closed_) return fail(Err::StoreClosed);
    for (const auto& [key, val] : batch) {
        if (val)
            rows_[key] = *val;
        else
            rows_.erase(key);
    }
    return ok();
}

Status Memory::close() {
    closed_ = true;
    return ok();
}

// ── File

File::~File() {
    if (fd_ >= 0) ::close(fd_);
}

Result<std::unique_ptr<File>> File::open(const std::string& path) {
    const int fd = ::open(path.c_str(), O_RDWR | O_CREAT | O_CLOEXEC, 0600);
    if (fd < 0) return fail(Err::StoreUnwritable, "cannot open " + path + ": " + std::strerror(errno));
    std::unique_ptr<File> f(new File(path, fd));
    if (auto r = f->replay(); !r) return std::unexpected(r.error());
    return f;
}

Status File::replay() {
    Bytes buf;
    const off_t end = ::lseek(fd_, 0, SEEK_END);
    if (end < 0) return errno_fail(Err::StoreUnwritable, "cannot size " + path_);
    buf.resize(static_cast<std::size_t>(end));
    if (end > 0) {
        if (::lseek(fd_, 0, SEEK_SET) < 0) return errno_fail(Err::StoreUnwritable, "cannot rewind " + path_);
        std::size_t got = 0;
        while (got < buf.size()) {
            const ssize_t n = ::read(fd_, buf.data() + got, buf.size() - got);
            if (n < 0) {
                if (errno == EINTR) continue;
                return errno_fail(Err::StoreUnwritable, "cannot read " + path_);
            }
            if (n == 0) break;
            got += static_cast<std::size_t>(n);
        }
        buf.resize(got);
    }

    std::size_t cursor = 0;
    while (cursor < buf.size()) {
        const ByteView rest(buf.data() + cursor, buf.size() - cursor);
        const auto msg = zap::Message::parse(rest);
        // A trailing record that does not parse is a commit that was
        // interrupted. It never happened, so the log is cut back to the last
        // record that did.
        if (!msg) break;
        const std::size_t len = msg->size();
        if (len == 0 || len > rest.size()) break;
        auto batch = decode_batch(rest.subspan(0, len));
        if (!batch) break;
        for (auto& [key, val] : *batch) {
            auto it = rows_.find(key);
            if (it != rows_.end()) live_bytes_ -= it->first.size() + it->second.size();
            if (val) {
                live_bytes_ += key.size() + val->size();
                rows_[key] = std::move(*val);
            } else if (it != rows_.end()) {
                rows_.erase(it);
            }
        }
        cursor += len;
    }

    log_bytes_ = cursor;
    if (cursor != buf.size() && ::ftruncate(fd_, static_cast<off_t>(cursor)) != 0)
        return errno_fail(Err::StoreUnwritable, "cannot truncate " + path_);
    if (::lseek(fd_, static_cast<off_t>(cursor), SEEK_SET) < 0)
        return errno_fail(Err::StoreUnwritable, "cannot seek " + path_);
    return ok();
}

Result<Bytes> File::get(ByteView key) const {
    if (fd_ < 0) return fail(Err::StoreClosed);
    auto it = rows_.find(to_bytes(key));
    if (it == rows_.end()) return fail(Err::NotFound);
    return it->second;
}

Status File::append(const Bytes& record) {
    std::size_t written = 0;
    while (written < record.size()) {
        const ssize_t n = ::write(fd_, record.data() + written, record.size() - written);
        if (n < 0) {
            if (errno == EINTR) continue;
            return errno_fail(Err::StoreUnwritable, "cannot write " + path_);
        }
        written += static_cast<std::size_t>(n);
    }
    // Durable when write() returns, or the crash story above is fiction.
    if (::fsync(fd_) != 0) return errno_fail(Err::StoreUnwritable, "cannot sync " + path_);
    log_bytes_ += record.size();
    return ok();
}

Status File::write(const Batch& batch) {
    if (fd_ < 0) return fail(Err::StoreClosed);
    if (batch.empty()) return ok();

    const Bytes record = encode_batch(batch);
    if (auto r = append(record); !r) return r;

    for (const auto& [key, val] : batch) {
        auto it = rows_.find(key);
        if (it != rows_.end()) live_bytes_ -= it->first.size() + it->second.size();
        if (val) {
            live_bytes_ += key.size() + val->size();
            rows_[key] = *val;
        } else if (it != rows_.end()) {
            rows_.erase(it);
        }
    }

    if (log_bytes_ > kCompactFloor && log_bytes_ > kCompactRatio * live_bytes_) return compact();
    return ok();
}

Status File::compact() {
    // One record holding every row, written beside the log and renamed over it.
    // The rename is the atomic point: a later open finds either the old log or
    // the whole new one, never a mixture.
    Batch all;
    for (const auto& [key, val] : rows_) all[key] = val;
    const Bytes record = encode_batch(all);

    const std::string tmp = path_ + ".compact";
    const int fd = ::open(tmp.c_str(), O_RDWR | O_CREAT | O_TRUNC | O_CLOEXEC, 0600);
    if (fd < 0) return errno_fail(Err::StoreUnwritable, "cannot open " + tmp);
    std::size_t written = 0;
    while (written < record.size()) {
        const ssize_t n = ::write(fd, record.data() + written, record.size() - written);
        if (n < 0) {
            if (errno == EINTR) continue;
            const std::string why = std::strerror(errno);
            ::close(fd);
            ::unlink(tmp.c_str());
            return fail(Err::StoreUnwritable, "cannot write " + tmp + ": " + why);
        }
        written += static_cast<std::size_t>(n);
    }
    if (::fsync(fd) != 0) {
        const std::string why = std::strerror(errno);
        ::close(fd);
        ::unlink(tmp.c_str());
        return fail(Err::StoreUnwritable, "cannot sync " + tmp + ": " + why);
    }
    ::close(fd);
    if (::rename(tmp.c_str(), path_.c_str()) != 0) {
        const std::string why = std::strerror(errno);
        ::unlink(tmp.c_str());
        return fail(Err::StoreUnwritable, "cannot rename over " + path_ + ": " + why);
    }

    ::close(fd_);
    fd_ = ::open(path_.c_str(), O_RDWR | O_CLOEXEC);
    if (fd_ < 0) return errno_fail(Err::StoreUnwritable, "cannot reopen " + path_);
    if (::lseek(fd_, 0, SEEK_END) < 0) return errno_fail(Err::StoreUnwritable, "cannot seek " + path_);
    log_bytes_ = record.size();
    return ok();
}

Status File::close() {
    if (fd_ >= 0) {
        ::close(fd_);
        fd_ = -1;
    }
    return ok();
}

// ── Version

Result<Bytes> Version::get(ByteView key) const {
    if (closed_) return fail(Err::StoreClosed);
    auto it = staged_.find(to_bytes(key));
    if (it != staged_.end()) {
        if (!it->second) return fail(Err::NotFound);
        return *it->second;
    }
    return base_->get(key);
}

Result<bool> Version::has(ByteView key) const {
    auto got = get(key);
    if (got) return true;
    if (got.error().code == Err::NotFound) return false;
    return std::unexpected(got.error());
}

Status Version::put(ByteView key, ByteView value) {
    if (closed_) return fail(Err::StoreClosed);
    staged_[to_bytes(key)] = to_bytes(value);
    return ok();
}

Status Version::del(ByteView key) {
    if (closed_) return fail(Err::StoreClosed);
    staged_[to_bytes(key)] = std::nullopt;
    return ok();
}

Status Version::commit() {
    if (closed_) return fail(Err::StoreClosed);
    if (staged_.empty()) return ok();
    if (auto r = base_->write(staged_); !r) return r;
    staged_.clear();
    return ok();
}

void Version::abort() { staged_.clear(); }

Status Version::close() {
    staged_.clear();
    closed_ = true;
    return ok();
}

}  // namespace lux::quantumvm::store
