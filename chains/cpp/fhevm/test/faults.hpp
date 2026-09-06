// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// faults.hpp — a store that fails one chosen operation and passes the rest
// through.
//
// A chain's database can fail to answer, and what F does then is a consensus
// question rather than an operational one. The failure that matters most is the
// quiet one: a read that FAILED reported as a read that found NOTHING. The
// last-accepted pointer coming back empty made a chain at height 2 look like a
// chain that had never run, and the node went on to build height 1 over it —
// durably, over the height index, with the boot returning success and nothing
// in any log to say so.
//
// Each switch is selected by key prefix, so a test can fail exactly the read or
// the write it is about and leave the rest of the chain working. That is what
// lets every case carry its own control: the SAME operation, absent rather than
// failing.

#pragma once

#include "lux/fhevm/store.hpp"

#include <optional>
#include <string>

namespace lux::fhevm::test {

class Faults final : public Store {
public:
    explicit Faults(Store* under) : under_(under) {}

    // Reads of keys under this prefix fail; empty disables.
    std::string read_fails;
    // Writes of keys under this prefix fail; empty disables.
    std::string put_fails;
    // The commit itself fails.
    bool commit_fails = false;
    // Iteration over this prefix reports an error rather than yielding rows.
    std::string iter_fails;
    // Everything fails, the way a closed database does.
    bool closed = false;

    Result<std::optional<Bytes>> get(ByteView key) const override {
        if (closed || hit(key, read_fails)) return disk();
        return under_->get(key);
    }

    Result<void> put(ByteView key, ByteView value) override {
        if (closed || hit(key, put_fails)) return disk();
        return under_->put(key, value);
    }

    Result<void> erase(ByteView key) override {
        if (closed || hit(key, put_fails)) return disk();
        return under_->erase(key);
    }

    Result<void> each(ByteView prefix,
                      const std::function<bool(ByteView, ByteView)>& f) const override {
        if (closed) return disk();
        // A corrupt index presents as an ERROR, not as an empty one — which is
        // the whole distinction these tests exist to draw.
        if (!iter_fails.empty() && starts_with(prefix, iter_fails)) return disk();
        return under_->each(prefix, f);
    }

    Result<void> commit() override {
        if (closed || commit_fails) return disk();
        return under_->commit();
    }

    void abort() override { under_->abort(); }

private:
    static bool starts_with(ByteView key, std::string_view prefix) {
        return key.size() >= prefix.size() &&
               std::equal(prefix.begin(), prefix.end(), key.begin());
    }
    static bool hit(ByteView key, const std::string& prefix) {
        return !prefix.empty() && starts_with(key, prefix);
    }
    static std::unexpected<Error> disk() {
        return fail(Err::Database, "the disk did not answer");
    }

    Store* under_;
};

}  // namespace lux::fhevm::test
