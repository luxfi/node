// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// store_test.cpp — persistence: what survives the process, what a decision
// stages, and what an interrupted commit leaves behind.
//
// NOT MEMORY-ONLY. A chain that forgets what it accepted when the process exits
// would re-sign a height it already signed and forget which notes were spent, so
// the durable path is exercised here against a real file.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/zkvm/store.hpp"

#include <cstdio>
#include <fcntl.h>
#include <string>
#include <unistd.h>
#include <vector>

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

std::string temp_path(const char* tag) {
    return std::string("/tmp/zkvm-store-") + tag + "-" + std::to_string(::getpid());
}

std::vector<std::string> scan(const store::Store& s, const Bytes& prefix) {
    std::vector<std::string> out;
    (void)s.each(view(prefix), [&](ByteView k, ByteView v) {
        out.push_back(hex_of(k) + "=" + hex_of(v));
        return true;
    });
    return out;
}

void a_memory_store_is_a_map() {
    std::printf("the memory store: rows, ordered scan, and honest absence\n");
    store::Memory m;
    check_ok(m.put(view(b("b")), view(b("2"))), "a row is written");
    check_ok(m.put(view(b("a")), view(b("1"))), "and another");
    check_ok(m.put(view(b("c")), view(b("3"))), "and another");

    auto got = m.get(view(b("a")));
    check_ok(got, "a row reads back");
    check(got && got->has_value() && **got == b("1"), "with the value it was given");

    auto missing = m.get(view(b("zz")));
    check_ok(missing, "a row that is not there is not an ERROR");
    check(missing && !missing->has_value(), "it is an absence");

    const auto rows = scan(m, {});
    check(rows.size() == 3, "every row is scanned");
    check(rows.size() == 3 && rows[0] < rows[1] && rows[1] < rows[2],
          "in ascending key order, which the sets rebuilt at boot depend on");

    check_ok(m.erase(view(b("b"))), "a row is erased");
    check(m.rows() == 2, "and is gone");
}

void the_view_stages_and_commits_whole() {
    std::printf("\nthe view stages a decision's writes and commits them whole\n");
    store::Memory base;
    store::View v(base);

    check_ok(v.put(view(b("k")), view(b("staged"))), "a decision writes through the view");
    auto seen = v.get(view(b("k")));
    check(seen && seen->has_value(), "and reads back what it just wrote");
    auto base_seen = base.get(view(b("k")));
    check(base_seen && !base_seen->has_value(), "while committed state has not moved");

    check_ok(v.commit(), "the commit lands");
    base_seen = base.get(view(b("k")));
    check(base_seen && base_seen->has_value() && **base_seen == b("staged"),
          "and now committed state holds it");

    // abort discards the whole batch — this is what makes a half-applied
    // decision unwritable.
    check_ok(v.put(view(b("k")), view(b("changed"))), "a second decision stages a change");
    check_ok(v.erase(view(b("gone"))), "and an erase");
    v.abort();
    check(!v.has_staged(), "abort discards everything staged");
    auto after = v.get(view(b("k")));
    check(after && after->has_value() && **after == b("staged"),
          "and the view is back to what committed state says");
}

void a_staged_row_shadows_and_a_staged_erase_hides() {
    std::printf("\na scan through the view sees ONE merged sequence\n");
    store::Memory base;
    check_ok(base.put(view(b("p:a")), view(b("1"))), "committed row a");
    check_ok(base.put(view(b("p:b")), view(b("2"))), "committed row b");

    store::View v(base);
    check_ok(v.put(view(b("p:b")), view(b("22"))), "b is restated");
    check_ok(v.put(view(b("p:c")), view(b("3"))), "c is added");
    check_ok(v.erase(view(b("p:a"))), "a is erased");

    const auto rows = scan(v, b("p:"));
    check(rows.size() == 2, "the scan sees two rows");
    check(rows.size() == 2 && rows[0] == hex_of(b("p:b")) + "=" + hex_of(b("22")),
          "the staged value shadows the committed one");
    check(rows.size() == 2 && rows[1] == hex_of(b("p:c")) + "=" + hex_of(b("3")),
          "the staged addition is there, in key order");
    // A staged erase HIDES the committed row rather than letting the scan
    // resurrect it — a spent-set rebuild that saw it would claim a note spent
    // that the decision under way had just discarded.
    for (const auto& r : rows) check(r.find(hex_of(b("p:a"))) != 0, "the erased row is hidden");
}

void a_file_store_survives_the_process() {
    std::printf("\nthe file store survives the process\n");
    const std::string path = temp_path("durable");
    ::unlink(path.c_str());

    {
        auto f = store::File::open(path);
        check_ok(f, "a fresh log opens");
        if (!f) return;
        check_ok((*f)->put(view(b("spent:n1")), view(b("height"))), "a spend is recorded");
        check_ok((*f)->put(view(b("utxo:c1")), view(b("note"))), "and an output");
        check_ok((*f)->commit(), "the commit fsyncs");
    }

    {
        auto f = store::File::open(path);
        check_ok(f, "the log reopens");
        if (!f) return;
        auto got = (*f)->get(view(b("spent:n1")));
        check(got && got->has_value() && **got == b("height"),
              "and the spend is still there after the process died");
        check((*f)->rows() == 2, "with every row replayed");
    }

    // A write that was never committed never happened.
    {
        auto f = store::File::open(path);
        if (!f) return;
        check_ok((*f)->put(view(b("uncommitted")), view(b("x"))), "a row is written");
    }
    {
        auto f = store::File::open(path);
        check_ok(f, "the log reopens again");
        if (f) {
            auto got = (*f)->get(view(b("uncommitted")));
            check(got && !got->has_value(), "an uncommitted write is not in the log");
        }
    }
    ::unlink(path.c_str());
}

void an_interrupted_commit_never_happened() {
    std::printf("\na commit interrupted mid-append never happened\n");
    const std::string path = temp_path("torn");
    ::unlink(path.c_str());

    std::size_t good_len = 0;
    {
        auto f = store::File::open(path);
        check_ok(f, "a fresh log opens");
        if (!f) return;
        check_ok((*f)->put(view(b("a")), view(b("1"))), "the first batch is written");
        check_ok((*f)->commit(), "and committed");
        good_len = (*f)->log_bytes();
        check_ok((*f)->put(view(b("b")), view(b("2"))), "a second batch is written");
        check_ok((*f)->commit(), "and committed");
    }

    // Cut the log inside the second record: a process killed mid-append leaves
    // exactly this.
    const int fd = ::open(path.c_str(), O_RDWR);
    check(fd >= 0, "the log can be truncated for the test");
    if (fd >= 0) {
        const off_t total = ::lseek(fd, 0, SEEK_END);
        check(std::size_t(total) > good_len, "there is a second record to cut into");
        const int cut = ::ftruncate(fd, off_t(good_len + (std::size_t(total) - good_len) / 2));
        check(cut == 0, "the log is cut inside the second record");
        ::close(fd);
    }

    auto f = store::File::open(path);
    check_ok(f, "the torn log still opens");
    if (f) {
        auto a = (*f)->get(view(b("a")));
        check(a && a->has_value(), "the committed batch is there");
        auto bb = (*f)->get(view(b("b")));
        check(bb && !bb->has_value(), "and the half-written one is not");
        check((*f)->log_bytes() == good_len, "the log was cut back to the last whole record");
    }
    ::unlink(path.c_str());
}

void the_record_format_is_pinned() {
    std::printf("\nthe log record is a ZAP message, and says so\n");
    ByteMap<std::optional<Bytes>> batch;
    batch[b("put")] = b("value");
    batch[b("del")] = std::nullopt;
    const Bytes record = store::encode_batch(batch);

    auto back = store::decode_batch(view(record));
    check_ok(back, "a record decodes");
    if (!back) return;
    check(back->size() == 2, "into both rows");
    check((*back)[b("put")].has_value() && *(*back)[b("put")] == b("value"),
          "a put keeps its value");
    check(back->count(b("del")) == 1 && !(*back)[b("del")].has_value(),
          "and an erase is an erase, not an empty value");

    Bytes broken = record;
    broken[0] ^= 0xFF;
    check_err(store::decode_batch(view(broken)), store::kErrCorruptRecord,
              "a corrupt record does not decode");
}

}  // namespace

int main() {
    a_memory_store_is_a_map();
    the_view_stages_and_commits_whole();
    a_staged_row_shadows_and_a_staged_erase_hides();
    a_file_store_survives_the_process();
    an_interrupted_commit_never_happened();
    the_record_format_is_pinned();
    return report("store");
}
