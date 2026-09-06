// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// store.hpp — somewhere to put bytes that are still there after a restart.
//
// A chain that only remembers what is in memory cannot restart as a node: it
// comes back naming genesis while its peers hold it to the tip it already told
// them about. So the accepted state is written down.
//
// TWO LAYERS, because the Go reference has two and the difference is what makes
// a block atomic:
//
//   Store    the durable byte map. Its ONE promise is that a batch is all there
//            or none of it is — which is what makes a height atomic, since a
//            block's whole effect is one batch.
//
//   Version  the staging layer over it (Go's versiondb). Writes buffer here and
//            reach the Store only at commit(); reads see the buffer over what
//            has been committed, so a block being applied reads its own writes
//            back. abort() discards what was staged — and that matters more
//            than it looks: staged writes that are not discarded are not
//            discarded LATER either, they are flushed wholesale by the next
//            commit that succeeds, so a block whose accept returned an error
//            still reached the store, riding in on an unrelated block minutes
//            afterwards.
//
// This is deliberately not a port of the reference's on-disk layout. That
// layout is not consensus: what a block commits to is its content hash, and two
// nodes that agree on every block id agree completely no matter what shape
// either wrote its own copy in. The obligation here is narrower and total:
// everything the accepted state holds goes in, comes back identical, and a
// machine that loses power mid-write comes back at the last height it finished
// rather than half way into the next one.

#pragma once

#include "lux/quantumvm/error.hpp"
#include "lux/quantumvm/id.hpp"

#include <cstddef>
#include <map>
#include <memory>
#include <optional>
#include <string>

namespace lux::quantumvm::store {

// A write set: a value to put, or nothing to delete.
using Batch = std::map<Bytes, std::optional<Bytes>>;

class Store {
  public:
    virtual ~Store() = default;

    // fail(Err::NotFound) when the store holds no such key. That is the ONE
    // reason for an empty answer; every other reason is a failure, and the two
    // must never be collapsed — a chain that reads a failed lookup as "empty"
    // writes genesis over a live tip.
    virtual Result<Bytes> get(ByteView key) const = 0;
    virtual Status write(const Batch& batch) = 0;
    virtual Status close() = 0;
};

// A store with no disk under it. Its durability is nothing and it claims
// nothing: it exists so the chain can be exercised without a filesystem.
class Memory final : public Store {
  public:
    Result<Bytes> get(ByteView key) const override;
    Status write(const Batch& batch) override;
    Status close() override;

    std::size_t rows() const { return rows_.size(); }
    bool closed() const { return closed_; }

  private:
    std::map<Bytes, Bytes> rows_;
    bool closed_ = false;
};

// A store that survives the process: one append-only log of ZAP-framed
// batches. A ZAP message declares its own length, so the log needs no framing
// of its own to know where one record ends and the next begins.
//
// A batch is applied on the way back in only when it parses whole, so a machine
// that lost power part way through a write comes back without that batch and
// with every batch before it. That is the whole crash story: the last height
// either finished or did not happen.
class File final : public Store {
  public:
    static Result<std::unique_ptr<File>> open(const std::string& path);
    ~File() override;

    File(const File&) = delete;
    File& operator=(const File&) = delete;

    Result<Bytes> get(ByteView key) const override;
    Status write(const Batch& batch) override;
    Status close() override;

    std::size_t rows() const { return rows_.size(); }
    std::size_t log_bytes() const { return log_bytes_; }
    std::size_t live_bytes() const { return live_bytes_; }

  private:
    File(std::string path, int fd) : path_(std::move(path)), fd_(fd) {}
    Status replay();
    Status append(const Bytes& record);
    Status compact();

    std::string path_;
    int fd_ = -1;
    std::map<Bytes, Bytes> rows_;
    std::size_t log_bytes_ = 0;
    std::size_t live_bytes_ = 0;
};

// The log record, exposed because a store's own format is worth testing.
Bytes encode_batch(const Batch& batch);
Result<Batch> decode_batch(ByteView record);

// The staging layer. Everything the VM reads and writes goes through one of
// these; commit() is the only thing that reaches the disk.
class Version {
  public:
    explicit Version(Store* base) : base_(base) {}

    Result<Bytes> get(ByteView key) const;
    Result<bool> has(ByteView key) const;
    Status put(ByteView key, ByteView value);
    Status del(ByteView key);

    // Everything staged since the last commit or abort becomes durable
    // together, or none of it does.
    Status commit();
    // Discards what was staged. Called on every exit from a commit path, so a
    // failed write leaves nothing behind for the next one to flush.
    void abort();

    Status close();
    bool closed() const { return closed_; }

  private:
    Store* base_ = nullptr;
    Batch staged_;
    bool closed_ = false;
};

}  // namespace lux::quantumvm::store
