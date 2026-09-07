// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// store_test.cpp — the byte-keyed map the state rests on.
//
// Two things are being held to account here. The first is the record format:
// the golden vector below is the string the X-CHAIN's store writes for the same
// batch — checked by compiling chains/cpp/xvm/src/store.cpp against it, not by
// asserting it — so one log opens under either C++ chain. The second is what
// happens when a process dies mid-write, which is the only interesting question
// a durable store has.

#include "harness.hpp"
#include "lux/platformvm/store.hpp"

#include <fcntl.h>
#include <unistd.h>

#include <cstdio>
#include <string>
#include <vector>

using namespace lux::platformvm;
using namespace lux::platformvm::store;

namespace {

Bytes bytes_of(const std::string& s) { return Bytes(s.begin(), s.end()); }

ByteView view_of(const std::string& s) {
    return ByteView(reinterpret_cast<const std::uint8_t*>(s.data()), s.size());
}

std::string hex(const Bytes& b) {
    static const char* d = "0123456789abcdef";
    std::string out;
    out.reserve(2 * b.size());
    for (auto x : b) {
        out.push_back(d[x >> 4]);
        out.push_back(d[x & 0xf]);
    }
    return out;
}

// A path of this test's own. The file is removed when the guard goes.
struct Scratch {
    std::string path;
    explicit Scratch(const std::string& name) {
        path = "/tmp/lux-pvm-store-" + std::to_string(::getpid()) + "-" + name;
        ::unlink(path.c_str());
    }
    ~Scratch() {
        ::unlink(path.c_str());
        ::unlink((path + ".compact").c_str());
    }
};

Batch batch_of(const std::vector<std::pair<std::string, std::optional<std::string>>>& rows) {
    Batch b;
    for (const auto& [k, v] : rows) {
        if (v.has_value())
            b[bytes_of(k)] = bytes_of(*v);
        else
            b[bytes_of(k)] = std::nullopt;
    }
    return b;
}

// Append raw bytes to a file, the way a killed process leaves them.
bool append_raw(const std::string& path, const Bytes& b) {
    const int fd = ::open(path.c_str(), O_WRONLY | O_APPEND);
    if (fd < 0) return false;
    const bool ok = ::write(fd, b.data(), b.size()) == static_cast<ssize_t>(b.size());
    ::close(fd);
    return ok;
}

std::size_t file_size(const std::string& path) {
    const int fd = ::open(path.c_str(), O_RDONLY);
    if (fd < 0) return 0;
    const off_t n = ::lseek(fd, 0, SEEK_END);
    ::close(fd);
    return n < 0 ? 0 : static_cast<std::size_t>(n);
}

std::vector<std::pair<Bytes, Bytes>> collect(const Store& s, const std::string& prefix) {
    std::vector<std::pair<Bytes, Bytes>> out;
    s.each(view_of(prefix), [&](ByteView k, ByteView v) {
        out.emplace_back(Bytes(k.begin(), k.end()), Bytes(v.begin(), v.end()));
        return true;
    });
    return out;
}

}  // namespace

TEST(ARecordRoundTripsThroughItsOwnEncoding) {
    const Batch b = batch_of({{"a", "one"}, {"bb", std::nullopt}, {"ccc", std::string{}}});
    auto back = decode_batch(view(encode_batch(b)));
    REQUIRE(back.has_value());
    REQUIRE(back.value() == b);
}

// The bytes, not just the shape. This exact string is what
// chains/cpp/xvm/src/store.cpp writes for the same batch: one record format,
// two C++ chains, one log either can open.
TEST(TheRecordIsTheBytesEveryLanguageWrites) {
    const Batch b = batch_of({{"k1", "v1"}, {"k2", std::nullopt}});
    const Bytes got = encode_batch(b);
    REQUIRE_EQ(std::string("5a415000020000002000000050000000020000000200000002000000000000"
                           "002800000002000000e8ffffff020000001a00000004000000e0ffffff020000"
                           "000e0000000200000001006b316b327631"),
               hex(got));
    auto back = decode_batch(view(got));
    REQUIRE(back.has_value());
    REQUIRE(back.value() == b);
}

TEST(MemoryScansAPrefixInAscendingOrderAndStopsAtIt) {
    Memory m;
    m.put(view_of("p\x02"), view_of("two"));
    m.put(view_of("p\x01"), view_of("one"));
    m.put(view_of("q"), view_of("other"));
    const auto rows = collect(m, "p");
    REQUIRE_EQ_NUM(2, rows.size());
    REQUIRE(rows[0].first == bytes_of("p\x01"));
    REQUIRE(rows[1].first == bytes_of("p\x02"));
    REQUIRE(m.commit().has_value());
}

TEST(WhatACommitWroteIsThereWhenTheStoreIsOpenedAgain) {
    Scratch s("reopen");
    {
        auto f = File::open(s.path);
        REQUIRE(f.has_value());
        f.value()->put(view_of("a"), view_of("1"));
        f.value()->put(view_of("b"), view_of("2"));
        REQUIRE(f.value()->commit().has_value());
        f.value()->erase(view_of("a"));
        f.value()->put(view_of("c"), view_of("3"));
        REQUIRE(f.value()->commit().has_value());
    }
    auto f = File::open(s.path);
    REQUIRE(f.has_value());
    REQUIRE(!f.value()->get(view_of("a")).has_value());
    REQUIRE(f.value()->get(view_of("b")).value() == bytes_of("2"));
    REQUIRE(f.value()->get(view_of("c")).value() == bytes_of("3"));
    REQUIRE_EQ_NUM(2, f.value()->rows());
}

TEST(WhatWasNeverCommittedIsNotThere) {
    Scratch s("uncommitted");
    {
        auto f = File::open(s.path);
        REQUIRE(f.has_value());
        f.value()->put(view_of("kept"), view_of("1"));
        REQUIRE(f.value()->commit().has_value());
        f.value()->put(view_of("dropped"), view_of("2"));
        // No commit. The process ends here.
    }
    auto f = File::open(s.path);
    REQUIRE(f.has_value());
    REQUIRE(f.value()->get(view_of("kept")).value() == bytes_of("1"));
    REQUIRE(!f.value()->get(view_of("dropped")).has_value());
}

TEST(ACommitTornInHalfNeverHappened) {
    Scratch s("torn");
    {
        auto f = File::open(s.path);
        REQUIRE(f.has_value());
        f.value()->put(view_of("kept"), view_of("1"));
        REQUIRE(f.value()->commit().has_value());
    }
    // The bytes of a real second commit, cut off partway — exactly what a
    // process killed inside write leaves behind.
    const Bytes whole = encode_batch(batch_of({{"torn", "2"}}));
    REQUIRE(append_raw(s.path, Bytes(whole.begin(), whole.begin() + whole.size() / 2)));

    auto f = File::open(s.path);
    REQUIRE(f.has_value());
    REQUIRE(f.value()->get(view_of("kept")).value() == bytes_of("1"));
    REQUIRE(!f.value()->get(view_of("torn")).has_value());
    // And the torn bytes are gone from the file, so the next append lands on a
    // record boundary.
    REQUIRE_EQ_NUM(f.value()->log_bytes(), file_size(s.path));
}

TEST(ATailOfGarbageNeverHappenedEither) {
    Scratch s("garbage");
    {
        auto f = File::open(s.path);
        REQUIRE(f.has_value());
        f.value()->put(view_of("kept"), view_of("1"));
        REQUIRE(f.value()->commit().has_value());
    }
    REQUIRE(append_raw(s.path, Bytes{0xde, 0xad, 0xbe, 0xef, 0x00, 0x11, 0x22}));
    {
        auto f = File::open(s.path);
        REQUIRE(f.has_value());
        REQUIRE(f.value()->get(view_of("kept")).value() == bytes_of("1"));
        // The store is usable afterwards, not merely readable.
        f.value()->put(view_of("next"), view_of("2"));
        REQUIRE(f.value()->commit().has_value());
    }
    auto f = File::open(s.path);
    REQUIRE(f.has_value());
    REQUIRE(f.value()->get(view_of("next")).value() == bytes_of("2"));
}

TEST(TheLogIsRewrittenOnceItHoldsMuchMoreThanTheRowsNeed) {
    Scratch s("compact");
    const Bytes value(4096, 7);
    {
        auto f = File::open(s.path);
        REQUIRE(f.has_value());
        // The same two keys, over and over: the rows stay small while the log
        // grows, which is the condition compaction exists for.
        for (int i = 0; i < 64; ++i) {
            f.value()->put(view_of("a"), view(value));
            f.value()->put(view_of("b"), view(value));
            REQUIRE(f.value()->commit().has_value());
        }
        REQUIRE_MSG(f.value()->log_bytes() <= 2 * f.value()->live_bytes() + 64 * 1024,
                    "the log never came back down against the rows");
    }
    auto f = File::open(s.path);
    REQUIRE(f.has_value());
    REQUIRE_EQ_NUM(2, f.value()->rows());
    REQUIRE(f.value()->get(view_of("a")).value() == value);
    REQUIRE(f.value()->get(view_of("b")).value() == value);
}
