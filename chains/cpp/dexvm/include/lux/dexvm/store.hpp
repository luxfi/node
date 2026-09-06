// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// store.hpp — where the chain's state actually rests.
//
// A chain that forgets what it accepted when the process exits is not a chain:
// it would re-sign a height it already signed. So the state has a store under
// it, and the store is a byte-keyed map with ONE durability point — `commit`.
//
//   Store   the interface: get, put, erase, ordered scan, commit
//   Memory  a store that never outlives the process (tests, and a chain that is
//           deliberately ephemeral)
//   File    a store that does, appending each commit to a log and replaying it
//           on open
//
// The log's records are ZAP messages, one per commit, concatenated. That is not
// a convenience: ZAP is the only serialization in this chain, and a message
// already declares its own length, so the log needs no framing of its own to
// know where one record ends and the next begins.
//
// A commit is durable when `commit` returns — it fsyncs. A process killed
// mid-append leaves a partial trailing record, which replay detects (the record
// does not parse, or does not fit) and truncates: a half-written commit never
// happened, which is exactly what atomicity means here.

#pragma once

#include "lux/dexvm/id.hpp"

#include <cstddef>
#include <functional>
#include <map>
#include <memory>
#include <optional>
#include <string>

namespace lux::dexvm::store {

inline constexpr const char* kErrOpen = "cannot open store";
inline constexpr const char* kErrWrite = "cannot write store";
inline constexpr const char* kErrCorruptRecord = "store record does not parse";

struct Store {
    virtual ~Store() = default;

    virtual std::optional<Bytes> get(ByteView key) const = 0;
    virtual void put(ByteView key, ByteView value) = 0;
    virtual void erase(ByteView key) = 0;

    // each walks every row whose key begins with `prefix`, in ascending key
    // order, until `f` returns false. Ascending order is a promise, not an
    // accident: the execution root folds over the rows in exactly it.
    virtual void each(ByteView prefix,
                      const std::function<bool(ByteView key, ByteView value)>& f) const = 0;

    // commit makes everything written since the last commit durable.
    virtual Result<void> commit() = 0;
};

// Memory is a store with no disk under it. Its commit is a no-op — honestly so:
// there is nothing to flush, and nothing survives the process.
class Memory final : public Store {
public:
    std::optional<Bytes> get(ByteView key) const override;
    void put(ByteView key, ByteView value) override;
    void erase(ByteView key) override;
    void each(ByteView prefix,
              const std::function<bool(ByteView, ByteView)>& f) const override;
    Result<void> commit() override { return {}; }

    std::size_t rows() const { return rows_.size(); }

private:
    std::map<Bytes, Bytes> rows_;
};

// File is a store that survives the process.
//
// The whole map is held in memory and the file is the durable log of how it got
// that way. That is a deliberate bound and it is stated rather than hidden: the
// admitted asset and market set is what the execution root folds over on every
// block, so it is read in full every height anyway. A registry that outgrew
// memory would need a different store, and it can have one — that is what the
// interface is for.
class File final : public Store {
public:
    static Result<std::unique_ptr<File>> open(const std::string& path);
    ~File() override;

    File(const File&) = delete;
    File& operator=(const File&) = delete;

    std::optional<Bytes> get(ByteView key) const override;
    void put(ByteView key, ByteView value) override;
    void erase(ByteView key) override;
    void each(ByteView prefix,
              const std::function<bool(ByteView, ByteView)>& f) const override;
    Result<void> commit() override;

    std::size_t rows() const { return rows_.size(); }
    // The bytes the log occupies, and the bytes the rows themselves need. The
    // log is rewritten when the first outgrows the second far enough that
    // replaying it costs more than rewriting it.
    std::size_t log_bytes() const { return log_bytes_; }
    std::size_t live_bytes() const { return live_bytes_; }

private:
    File(std::string path, int fd) : path_(std::move(path)), fd_(fd) {}

    Result<void> replay();
    Result<void> append(const Bytes& record);
    Result<void> compact();

    std::string path_;
    int fd_ = -1;
    std::map<Bytes, Bytes> rows_;
    // What has been written but not yet flushed: a value, or nullopt for an
    // erase. One entry per key, because a key written twice before a commit only
    // needs its last value.
    std::map<Bytes, std::optional<Bytes>> staged_;
    std::size_t log_bytes_ = 0;
    std::size_t live_bytes_ = 0;
};

// encode_batch / decode_batch are the log record: exposed because a format is
// only pinned by a test that can state it.
Bytes encode_batch(const std::map<Bytes, std::optional<Bytes>>& batch);
Result<std::map<Bytes, std::optional<Bytes>>> decode_batch(ByteView record);

}  // namespace lux::dexvm::store
