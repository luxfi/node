// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// store.hpp — somewhere to put bytes that are still there after a restart.
//
// A chain that only remembers what is in memory cannot restart as a node: it
// comes back knowing nothing, and a node that knows nothing has to be told what
// it decided by the peers whose claims it exists to check. So the accepted
// state is written down.
//
// This is deliberately not a port of the reference's on-disk layout. That
// layout — key encodings, height diffs, batched commits — is about two thousand
// lines and NONE of it is consensus: what a block commits to is its execution's
// root, computed by running the block, and two nodes that agree on every root
// agree completely no matter what shape either wrote its own copy in. So the
// obligation here is narrower and total: everything the accepted state holds
// goes in, comes back identical, and a machine that loses power mid-write comes
// back at the last height it finished rather than half way into the next one.
//
// The interface is a byte map with one property that matters: a batch is all
// there or none of it is. That is what makes a height atomic — a block's whole
// effect is one commit, so a chain never comes back having applied part of a
// block.

#pragma once

#include "lux/platformvm/error.hpp"

#include <cstdint>
#include <functional>
#include <map>
#include <memory>
#include <optional>
#include <span>
#include <string>
#include <vector>

namespace lux::platformvm::store {

using Bytes = std::vector<std::uint8_t>;

// What a scan hands back, in ascending key order.
using Visit = std::function<void(std::span<const std::uint8_t>, std::span<const std::uint8_t>)>;

// A byte map whose writes become durable together or not at all.
//
// Writes are buffered until commit(), and reads see the buffer over what has
// been committed — a block being applied reads its own writes back, which is
// what lets one commit hold a whole height.
class Store {
  public:
    virtual ~Store() = default;

    virtual void put(std::span<const std::uint8_t> key, std::span<const std::uint8_t> value) = 0;
    virtual void del(std::span<const std::uint8_t> key) = 0;
    virtual Result<Bytes> get(std::span<const std::uint8_t> key) const = 0;
    virtual void scan(std::span<const std::uint8_t> prefix, const Visit& fn) const = 0;

    // Everything written since the last commit becomes durable together. A
    // store that cannot promise that refuses rather than pretending.
    virtual Status commit() = 0;
};

// A store in memory. It has no durability to offer and does not claim any; it
// exists so the write-through state can be exercised without a filesystem, and
// so a chain that has not been given anywhere to write still runs.
class Memory final : public Store {
  public:
    void put(std::span<const std::uint8_t> key, std::span<const std::uint8_t> value) override;
    void del(std::span<const std::uint8_t> key) override;
    Result<Bytes> get(std::span<const std::uint8_t> key) const override;
    void scan(std::span<const std::uint8_t> prefix, const Visit& fn) const override;
    Status commit() override;

    std::size_t size() const { return live_.size(); }

  private:
    std::map<Bytes, Bytes> live_;
    // Nothing means deleted. A pending entry shadows the live one.
    std::map<Bytes, std::optional<Bytes>> pending_;
};

// A store on disk: one append-only file of batches, each ending in a record
// that says how many bytes preceded it and what they hash to.
//
// A batch is applied on the way back in only when its closing record is there
// and its checksum matches, so a machine that lost power part way through a
// write comes back without that batch and with every batch before it. That is
// the whole crash story: the last height either finished or did not happen.
//
// The file is rewritten on open, holding exactly what is live. A log that only
// ever grew would be a chain that gets slower to start the longer it has run.
class File final : public Store {
  public:
    ~File() override;

    // Opens `path`, replaying what is there. A file that does not exist is an
    // empty store, which is how a chain starts.
    static Result<std::unique_ptr<File>> open(const std::string& path);

    void put(std::span<const std::uint8_t> key, std::span<const std::uint8_t> value) override;
    void del(std::span<const std::uint8_t> key) override;
    Result<Bytes> get(std::span<const std::uint8_t> key) const override;
    void scan(std::span<const std::uint8_t> prefix, const Visit& fn) const override;
    Status commit() override;

    std::size_t size() const { return live_.size(); }

  private:
    File() = default;
    Status rewrite();  // compact: one batch holding exactly what is live

    std::string path_;
    int fd_ = -1;
    std::map<Bytes, Bytes> live_;
    std::map<Bytes, std::optional<Bytes>> pending_;
};

}  // namespace lux::platformvm::store
