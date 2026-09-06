// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// store.hpp — where the F-Chain's state actually rests, and the one place a
// block's writes become durable.
//
// The shape is Go's versiondb, and the shape is the point: everything a block
// writes is BUFFERED and visible to its own later reads, and nothing reaches
// the disk until the block is accepted. commit() is that moment; abort() is the
// other half — a block that fails part way leaves nothing behind, so a fee
// burn and the operation it paid for either both land or neither does.
//
//   Store   the interface: get, put, erase, ordered prefix scan, commit, abort
//   Memory  a store that never outlives the process (tests, ephemeral chains)
//   File    a store that does, appending each commit to a log and replaying it
//           on open
//
// The log's records are ZAP messages, one per commit, concatenated: ZAP is the
// only serialization this chain uses, and a message already declares its own
// length, so the log needs no framing of its own.
//
// A commit is durable when commit() returns — it fsyncs. A process killed
// mid-append leaves a partial trailing record, which replay detects and
// truncates: a half-written commit never happened, which is what atomicity
// means here.

#pragma once

#include "lux/fhevm/error.hpp"
#include "lux/fhevm/id.hpp"

#include <algorithm>
#include <functional>
#include <map>
#include <memory>
#include <optional>
#include <string>

namespace lux::fhevm {

// Rows are ordered by their key bytes, ascending, and that order is a PROMISE
// rather than an accident: the caches a node rebuilds on boot and the listing
// two nodes compare after a replay are both folded in it. Saying so with a
// comparator rather than leaning on the container's default keeps the promise
// where a reader can find it.
struct ByteLess {
    bool operator()(const Bytes& a, const Bytes& b) const {
        return std::lexicographical_compare(a.begin(), a.end(), b.begin(), b.end());
    }
};

using Rows = std::map<Bytes, Bytes, ByteLess>;
using Staged = std::map<Bytes, std::optional<Bytes>, ByteLess>;

struct Store {
    virtual ~Store() = default;

    // get returns the value, or nullopt when the key is ABSENT. A failure to
    // READ is reported through fail() — conflating the two is how a live chain
    // reads as a fresh one.
    virtual Result<std::optional<Bytes>> get(ByteView key) const = 0;
    virtual Result<void> put(ByteView key, ByteView value) = 0;
    virtual Result<void> erase(ByteView key) = 0;

    // each walks every row whose key begins with prefix, in ascending key
    // order, until f returns false. Ascending order is a promise: the caches a
    // node rebuilds on boot are folded in it.
    virtual Result<void> each(ByteView prefix,
                              const std::function<bool(ByteView key, ByteView value)>& f) const = 0;

    // commit makes everything written since the last commit durable.
    virtual Result<void> commit() = 0;
    // abort drops it instead. The two are the whole of a block's decision.
    virtual void abort() = 0;

    Result<bool> has(ByteView key) const;
};

// Memory is a store with no disk under it. Its commit moves the staged rows
// into the committed ones and is honestly durable for as long as the process
// lives, which is exactly as long as the chain does.
class Memory final : public Store {
public:
    Result<std::optional<Bytes>> get(ByteView key) const override;
    Result<void> put(ByteView key, ByteView value) override;
    Result<void> erase(ByteView key) override;
    Result<void> each(ByteView prefix,
                      const std::function<bool(ByteView, ByteView)>& f) const override;
    Result<void> commit() override;
    void abort() override;

    std::size_t rows() const { return rows_.size(); }
    std::size_t staged() const { return staged_.size(); }

private:
    Rows rows_;
    Staged staged_;
};

// File is a store that survives the process. The whole map is held in memory
// and the file is the durable log of how it got that way — a deliberate bound,
// stated rather than hidden: F's records are read in full on boot anyway.
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
    void abort() override;

    std::size_t rows() const { return rows_.size(); }

private:
    File(std::string path, int fd) : path_(std::move(path)), fd_(fd) {}

    Result<void> replay();

    std::string path_;
    int fd_ = -1;
    Rows rows_;
    Staged staged_;
};

// encode_batch / decode_batch are the log record, exposed because a format is
// only pinned by a test that can state it.
Bytes encode_batch(const Staged& batch);
Result<Staged> decode_batch(ByteView record);

// key builds a namespaced key: prefix bytes then the id. Every namespace in
// this chain is a string prefix and a fixed-width name, so it is written once.
Bytes key(std::string_view prefix, ByteView name);
Bytes key(std::string_view prefix, std::uint64_t name);

}  // namespace lux::fhevm
