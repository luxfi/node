// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// store.hpp — where the Z-Chain's state actually rests, and the view a block
// writes through until it commits.
//
// A chain that forgets what it accepted when the process exits is not a chain:
// it would re-sign a height it already signed, and its shielded pool would
// forget which notes were spent. So the state has a store under it.
//
//   Store   the interface: get, put, erase, ordered scan, commit
//   Memory  a store that never outlives the process (tests, and a chain that is
//           deliberately ephemeral)
//   File    a store that does, appending each commit to a log and replaying it
//           on open
//   View    committed state plus what the block in progress has staged. commit
//           makes the whole batch durable; abort discards it whole. This is
//           Go's versiondb, and it is what makes a half-applied block
//           unwritable.
//
// A READ THAT FAILED IS NOT AN ABSENT ROW. get returns Result<optional<Bytes>>:
// the error is "the disk is gone", the empty optional is "no such row". On this
// chain the difference is a double spend — reporting a spent-set read failure
// as "not spent" is exactly how an already-spent note gets spent again.
//
// The log's records are ZAP messages, one per commit, concatenated. A message
// already declares its own length, so the log needs no framing of its own. A
// process killed mid-append leaves a partial trailing record, which replay
// detects and truncates: a half-written commit never happened.

#pragma once

#include "lux/zkvm/id.hpp"
#include "lux/zkvm/wire.hpp"

#include <cstddef>
#include <functional>
#include <map>
#include <memory>
#include <optional>
#include <string>

namespace lux::zkvm::store {

template <class T>
using Result = wire::Result<T>;

inline constexpr const char* kErrOpen = "cannot open store";
inline constexpr const char* kErrWrite = "cannot write store";
inline constexpr const char* kErrCorruptRecord = "store record does not parse";

struct Store {
    virtual ~Store() = default;

    // nullopt = no such row. An error = the row could not be read, which is a
    // different fact and is never rendered as absence.
    virtual Result<std::optional<Bytes>> get(ByteView key) const = 0;
    virtual Result<void> put(ByteView key, ByteView value) = 0;
    virtual Result<void> erase(ByteView key) = 0;

    // each walks every row whose key begins with prefix, in ascending key
    // order, until f returns false. Ascending order is a promise, not an
    // accident: the sets rebuilt at boot are enumerated by it.
    virtual Result<void> each(ByteView prefix,
                              const std::function<bool(ByteView, ByteView)>& f) const = 0;

    virtual Result<void> commit() = 0;
};

// Memory is a store with no disk under it. Its commit is a no-op — honestly so:
// there is nothing to flush, and nothing survives the process.
class Memory final : public Store {
public:
    Result<std::optional<Bytes>> get(ByteView key) const override;
    Result<void> put(ByteView key, ByteView value) override;
    Result<void> erase(ByteView key) override;
    Result<void> each(ByteView prefix,
                      const std::function<bool(ByteView, ByteView)>& f) const override;
    Result<void> commit() override { return {}; }

    std::size_t rows() const { return rows_.size(); }

private:
    ByteMap<Bytes> rows_;
};

// File is a store that survives the process.
//
// The whole map is held in memory and the file is the durable log of how it got
// that way. That is a deliberate bound, stated rather than hidden: the spent set
// is loaded in full at boot anyway — it is what refuses a double spend — so a
// cache that could evict it would only add a disk read to the hot path. A chain
// whose set outgrows memory needs a different store, and it can have one; that
// is what the interface is for.
class File final : public Store {
public:
    static Result<std::unique_ptr<File>> open(const std::string& path);
    ~File() override;

    File(const File&) = delete;
    File& operator=(const File&) = delete;

    Result<std::optional<Bytes>> get(ByteView key) const override;
    Result<void> put(ByteView key, ByteView value) override;
    Result<void> erase(ByteView key) override;
    Result<void> each(ByteView prefix,
                      const std::function<bool(ByteView, ByteView)>& f) const override;
    Result<void> commit() override;

    std::size_t rows() const { return rows_.size(); }
    std::size_t log_bytes() const { return log_bytes_; }
    std::size_t live_bytes() const { return live_bytes_; }

private:
    File(std::string path, int fd) : path_(std::move(path)), fd_(fd) {}

    Result<void> replay();
    Result<void> append(const Bytes& record);
    Result<void> compact();

    std::string path_;
    int fd_ = -1;
    ByteMap<Bytes> rows_;
    ByteMap<std::optional<Bytes>> staged_;
    std::size_t log_bytes_ = 0;
    std::size_t live_bytes_ = 0;
};

// View is what a block writes through, and what every read sees: committed
// state plus whatever the block in progress has staged. Go: versiondb.
//
// commit lands the whole batch in the base store in ONE step; abort discards it
// whole. Nothing a block wrote can survive a decision that failed, and nothing
// it wrote can be missing from one that succeeded.
class View final : public Store {
public:
    explicit View(Store& base) : base_(&base) {}

    Result<std::optional<Bytes>> get(ByteView key) const override;
    Result<void> put(ByteView key, ByteView value) override;
    Result<void> erase(ByteView key) override;
    Result<void> each(ByteView prefix,
                      const std::function<bool(ByteView, ByteView)>& f) const override;
    Result<void> commit() override;

    // abort discards everything staged since the last commit.
    void abort() { staged_.clear(); }

    bool has_staged() const { return !staged_.empty(); }

    Store& base() { return *base_; }

private:
    Store* base_;
    ByteMap<std::optional<Bytes>> staged_;
};

// encode_batch / decode_batch are the log record: exposed because a format is
// only pinned by a test that can state it.
Bytes encode_batch(const ByteMap<std::optional<Bytes>>& batch);
Result<ByteMap<std::optional<Bytes>>> decode_batch(ByteView record);

}  // namespace lux::zkvm::store
