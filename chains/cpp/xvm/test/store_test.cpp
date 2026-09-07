// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// store_test.cpp — what survives, and what does not.
//
// The claim under test is the one a chain rests on: after `commit` returns, a
// process that dies and comes back finds exactly what it wrote, and a commit
// that was interrupted finds none of it. Both are checked against a real file,
// reopened — a store tested only through the handle that wrote it proves
// nothing about durability.

#include "check.hpp"

#include "lux/xvm/store.hpp"

#include <cstdio>
#include <string>
#include <unistd.h>
#include <vector>

using namespace lux::xvm;
using namespace lux::xvm::test;

namespace {

Bytes b(const std::string& s) { return Bytes(s.begin(), s.end()); }
std::string s(const Bytes& v) { return std::string(v.begin(), v.end()); }

std::string temp_path(const char* name) {
    return "/tmp/xvm-store-" + std::to_string(::getpid()) + "-" + name;
}

void memory_reads_what_it_wrote() {
    store::Memory m;
    m.put(view(b("a")), view(b("one")));
    m.put(view(b("b")), view(b("two")));
    auto got = m.get(view(b("a")));
    check(got.has_value() && s(*got) == "one", "a memory store reads back a row");
    m.erase(view(b("a")));
    check(!m.get(view(b("a"))).has_value(), "and an erased row is gone");
    check(m.commit().has_value(), "its commit is a no-op that says so honestly");
}

void the_scan_is_ordered_and_bounded() {
    store::Memory m;
    m.put(view(b("ub")), view(b("2")));
    m.put(view(b("ua")), view(b("1")));
    m.put(view(b("uc")), view(b("3")));
    m.put(view(b("t!")), view(b("other")));
    m.put(view(b("v!")), view(b("other")));

    std::vector<std::string> seen;
    m.each(view(b("u")), [&](ByteView key, ByteView) {
        seen.push_back(s(Bytes(key.begin(), key.end())));
        return true;
    });
    check(seen.size() == 3, "a scan sees only its own prefix");
    check(seen.size() == 3 && seen[0] == "ua" && seen[1] == "ub" && seen[2] == "uc",
          "and sees it in ascending key order");

    // The ascending promise is what an execution root folds over, so the walk
    // must also be stoppable without seeing the rest.
    int count = 0;
    m.each(view(b("u")), [&](ByteView, ByteView) {
        ++count;
        return false;
    });
    check(count == 1, "returning false stops the walk");
}

void a_commit_survives_a_reopen() {
    const std::string path = temp_path("survive");
    ::unlink(path.c_str());
    {
        auto f = store::File::open(path);
        check(f.has_value(), "open a fresh store");
        if (!f) return;
        (*f)->put(view(b("k1")), view(b("v1")));
        (*f)->put(view(b("k2")), view(b("v2")));
        check((*f)->commit().has_value(), "commit");
        (*f)->put(view(b("k3")), view(b("v3")));
        (*f)->erase(view(b("k1")));
        check((*f)->commit().has_value(), "commit again");
    }
    {
        auto f = store::File::open(path);
        check(f.has_value(), "reopen the store");
        if (!f) return;
        auto k2 = (*f)->get(view(b("k2")));
        auto k3 = (*f)->get(view(b("k3")));
        check(k2.has_value() && s(*k2) == "v2", "the first commit is still there");
        check(k3.has_value() && s(*k3) == "v3", "so is the second");
        check(!(*f)->get(view(b("k1"))).has_value(), "and the erase is too — absence persists");
        check((*f)->rows() == 2, "nothing else came back");
    }
    ::unlink(path.c_str());
}

void an_uncommitted_write_does_not_survive() {
    const std::string path = temp_path("uncommitted");
    ::unlink(path.c_str());
    {
        auto f = store::File::open(path);
        check(f.has_value(), "open");
        if (!f) return;
        (*f)->put(view(b("kept")), view(b("yes")));
        check((*f)->commit().has_value(), "commit the one that should survive");
        (*f)->put(view(b("lost")), view(b("no")));
        // No commit. The process ends here.
        auto seen = (*f)->get(view(b("lost")));
        check(seen.has_value(), "an uncommitted write IS visible to this handle");
    }
    {
        auto f = store::File::open(path);
        check(f.has_value(), "reopen");
        if (!f) return;
        check((*f)->get(view(b("kept"))).has_value(), "the committed row is there");
        check(!(*f)->get(view(b("lost"))).has_value(),
              "the uncommitted one never happened");
    }
    ::unlink(path.c_str());
}

void a_torn_record_is_cut_back() {
    const std::string path = temp_path("torn");
    ::unlink(path.c_str());
    std::size_t good_len = 0;
    {
        auto f = store::File::open(path);
        check(f.has_value(), "open");
        if (!f) return;
        (*f)->put(view(b("first")), view(b("1")));
        check((*f)->commit().has_value(), "the commit that completes");
        good_len = (*f)->log_bytes();
        (*f)->put(view(b("second")), view(b("2")));
        check((*f)->commit().has_value(), "the commit that will be torn");
    }
    // Cut the second record in half: a process killed inside `write`.
    {
        std::FILE* fp = std::fopen(path.c_str(), "r+b");
        check(fp != nullptr, "open the log to tear it");
        if (fp == nullptr) return;
        std::fseek(fp, 0, SEEK_END);
        const long len = std::ftell(fp);
        std::fclose(fp);
        check(len > long(good_len), "the second record is on disk");
        check(::truncate(path.c_str(), off_t(good_len) + (len - long(good_len)) / 2) == 0,
              "tear it");
    }
    {
        auto f = store::File::open(path);
        check(f.has_value(), "reopen after the tear");
        if (!f) return;
        check((*f)->get(view(b("first"))).has_value(), "the completed commit is intact");
        check(!(*f)->get(view(b("second"))).has_value(), "the torn one never happened");
        check((*f)->log_bytes() == good_len, "and the log was cut back to the last whole record");
        // The store is usable again, which is the point of cutting it back.
        (*f)->put(view(b("third")), view(b("3")));
        check((*f)->commit().has_value(), "and it can be written to again");
    }
    {
        auto f = store::File::open(path);
        check(f.has_value() && (*f)->get(view(b("third"))).has_value(),
              "the write after recovery survives too");
    }
    ::unlink(path.c_str());
}

void the_log_is_rewritten_when_it_outgrows_the_rows() {
    const std::string path = temp_path("compact");
    ::unlink(path.c_str());
    {
        auto f = store::File::open(path);
        check(f.has_value(), "open");
        if (!f) return;
        // One key, overwritten until the log is mostly history.
        const Bytes value(4096, 0xAB);
        for (int i = 0; i < 64; ++i) {
            (*f)->put(view(b("hot")), view(value));
            check((*f)->commit().has_value(), i == 0 ? "commit" : "");
        }
        check((*f)->log_bytes() < 64 * value.size(),
              "the log was rewritten rather than grown forever");
        check((*f)->rows() == 1, "and it still holds exactly one row");
    }
    {
        auto f = store::File::open(path);
        check(f.has_value(), "reopen a rewritten log");
        if (!f) return;
        auto got = (*f)->get(view(b("hot")));
        check(got.has_value() && got->size() == 4096 && (*got)[0] == 0xAB,
              "the surviving value is the last one written");
        check((*f)->rows() == 1, "and nothing else came back with it");
    }
    ::unlink(path.c_str());
}

void the_record_is_a_zap_message() {
    std::map<Bytes, std::optional<Bytes>> batch;
    batch[b("put")] = b("value");
    batch[b("gone")] = std::nullopt;
    batch[b("empty")] = Bytes{};
    const Bytes record = store::encode_batch(batch);

    auto back = store::decode_batch(view(record));
    check(back.has_value(), "a batch decodes");
    if (!back) return;
    check(back->size() == 3, "with every row");
    check((*back)[b("put")].has_value() && s(*(*back)[b("put")]) == "value", "a put keeps its value");
    check(!(*back)[b("gone")].has_value(), "an erase stays an erase, not an empty value");
    check((*back)[b("empty")].has_value() && (*back)[b("empty")]->empty(),
          "and an empty value stays a value");

    // The record declares its own length, which is what lets the log hold many
    // of them end to end with no framing of its own.
    Bytes twice = record;
    twice.insert(twice.end(), record.begin(), record.end());
    auto first = store::decode_batch(view(twice));
    check(first.has_value() && first->size() == 3, "a record parses out of a longer buffer");

    check(!store::decode_batch(view(b("not a zap message at all"))).has_value(),
          "and a buffer that is not one is refused");
}

}  // namespace

int main() {
    std::printf("store\n");
    memory_reads_what_it_wrote();
    the_scan_is_ordered_and_bounded();
    a_commit_survives_a_reopen();
    an_uncommitted_write_does_not_survive();
    a_torn_record_is_cut_back();
    the_log_is_rewritten_when_it_outgrows_the_rows();
    the_record_is_a_zap_message();
    return report("store");
}
