// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// store_test — the durable log, and what survives a restart.
//
// The property under test is the one a chain cannot do without: what `commit`
// returned for is still there in the next process. The interesting cases are the
// ones where it should NOT be — an uncommitted write, and a commit interrupted
// half-written — because a store that kept those would let a restarted node
// disagree with what it actually signed.

#include "lux/dexvm/store.hpp"

#include "check.hpp"

#include <cstdio>
#include <string>
#include <unistd.h>

using namespace lux::dexvm;
using namespace lux::dexvm::test;

namespace {

ByteView key_of(const std::string& s) {
    return ByteView(reinterpret_cast<const std::uint8_t*>(s.data()), s.size());
}
std::string text_of(const Bytes& b) { return std::string(b.begin(), b.end()); }

// A path in a directory that removes itself.
class TempPath {
public:
    TempPath() {
        char tmpl[] = "/tmp/dexvm-store-XXXXXX";
        path_ = ::mkdtemp(tmpl);
        path_ += "/log";
    }
    ~TempPath() {
        ::unlink(path_.c_str());
        ::unlink((path_ + ".compact").c_str());
        ::rmdir(path_.substr(0, path_.rfind('/')).c_str());
    }
    const std::string& path() const { return path_; }

private:
    std::string path_;
};

void memory_is_honest_about_being_ephemeral() {
    std::printf("the memory store, which promises nothing and says so\n");
    store::Memory m;
    m.put(key_of("b"), key_of("2"));
    m.put(key_of("a"), key_of("1"));
    check(m.rows() == 2, "two rows written");
    auto got = m.get(key_of("a"));
    check(got && text_of(*got) == "1", "and read back");
    admitted(m.commit(), "its commit succeeds — there is simply nothing to flush");

    m.erase(key_of("a"));
    check(!m.get(key_of("a")).has_value(), "an erased row is gone");
    check(m.rows() == 1, "and the count follows");
}

void ordered_prefix_scan() {
    std::printf("the ordered scan, which the execution root folds over\n");
    store::Memory m;
    for (const char* k : {"a3", "b1", "a1", "a2", "c9"}) m.put(key_of(k), key_of(k));

    std::string seen;
    m.each(key_of("a"), [&](ByteView k, ByteView) {
        seen += std::string(reinterpret_cast<const char*>(k.data()), k.size());
        seen += " ";
        return true;
    });
    check_eq(seen, "a1 a2 a3 ", "a prefix scan yields ascending order, and only the prefix");

    // A scan that stops is honoured, because an execution that has seen enough
    // should not pay for the rest of the set.
    int count = 0;
    m.each(key_of("a"), [&](ByteView, ByteView) {
        ++count;
        return false;
    });
    check(count == 1, "and returning false stops it");
}

void a_commit_survives_the_process() {
    std::printf("a file store, across two opens\n");
    TempPath tp;
    {
        auto s = store::File::open(tp.path());
        admitted(s, "the log opens");
        if (!s) return;
        (*s)->put(key_of("k1"), key_of("v1"));
        (*s)->put(key_of("k2"), key_of("v2"));
        admitted((*s)->commit(), "and the commit flushes");

        // Written but NOT committed: it is visible here, because the block being
        // verified must read what it just wrote.
        (*s)->put(key_of("k3"), key_of("v3"));
        auto got = (*s)->get(key_of("k3"));
        check(got && text_of(*got) == "v3", "an uncommitted write is visible in-process");
    }
    {
        auto s = store::File::open(tp.path());
        admitted(s, "the log reopens");
        if (!s) return;
        auto a = (*s)->get(key_of("k1"));
        auto b = (*s)->get(key_of("k2"));
        check(a && text_of(*a) == "v1", "the committed rows are still there");
        check(b && text_of(*b) == "v2", "…both of them");
        check(!(*s)->get(key_of("k3")).has_value(),
              "and the uncommitted one is not — it never happened");
    }
}

void an_erase_survives_too() {
    std::printf("an erase is a commit like any other\n");
    TempPath tp;
    {
        auto s = store::File::open(tp.path());
        if (!s) {
            check(false, "open");
            return;
        }
        (*s)->put(key_of("gone"), key_of("x"));
        (*s)->put(key_of("stays"), key_of("y"));
        admitted((*s)->commit(), "two rows committed");
        (*s)->erase(key_of("gone"));
        admitted((*s)->commit(), "and one erased");
    }
    {
        auto s = store::File::open(tp.path());
        if (!s) {
            check(false, "reopen");
            return;
        }
        check(!(*s)->get(key_of("gone")).has_value(), "the erased row does not come back");
        check((*s)->get(key_of("stays")).has_value(), "and the other one does");
    }
}

void a_half_written_commit_never_happened() {
    std::printf("a commit interrupted mid-append never happened\n");
    TempPath tp;
    {
        auto s = store::File::open(tp.path());
        if (!s) {
            check(false, "open");
            return;
        }
        (*s)->put(key_of("good"), key_of("1"));
        admitted((*s)->commit(), "one good commit");
    }
    // Append a truncated record: the shape a process killed mid-write leaves.
    {
        std::FILE* f = std::fopen(tp.path().c_str(), "ab");
        check(f != nullptr, "the log can be appended to");
        if (f) {
            const std::map<Bytes, std::optional<Bytes>> batch{
                {to_bytes(key_of("torn")), std::optional<Bytes>(to_bytes(key_of("2")))}};
            const Bytes record = store::encode_batch(batch);
            std::fwrite(record.data(), 1, record.size() / 2, f);  // half of it
            std::fclose(f);
        }
    }
    {
        auto s = store::File::open(tp.path());
        admitted(s, "the log still opens");
        if (!s) return;
        check((*s)->get(key_of("good")).has_value(), "the whole record is still there");
        check(!(*s)->get(key_of("torn")).has_value(), "and the torn one is not");

        // The truncation is durable: the next commit writes after the good
        // record, not after the garbage.
        (*s)->put(key_of("after"), key_of("3"));
        admitted((*s)->commit(), "a later commit lands");
    }
    {
        auto s = store::File::open(tp.path());
        if (!s) {
            check(false, "reopen");
            return;
        }
        check((*s)->get(key_of("after")).has_value(), "and reopens holding it");
        check(!(*s)->get(key_of("torn")).has_value(), "with the torn record still gone");
    }
}

void the_record_format_is_stated() {
    std::printf("the log record, which a test can state because it is exposed\n");
    const std::map<Bytes, std::optional<Bytes>> batch{
        {to_bytes(key_of("put")), std::optional<Bytes>(to_bytes(key_of("value")))},
        {to_bytes(key_of("del")), std::nullopt},
    };
    const Bytes record = store::encode_batch(batch);
    auto back = store::decode_batch(view(record));
    admitted(back, "a batch round-trips");
    if (!back) return;
    check(back->size() == 2, "with both rows");
    check((*back)[to_bytes(key_of("put"))].has_value(), "the put carries a value");
    check(!(*back)[to_bytes(key_of("del"))].has_value(), "and the erase carries none");

    // A record that is not a ZAP message is refused, not guessed at.
    const Bytes garbage{0x00, 0x01, 0x02, 0x03};
    refused_any(store::decode_batch(view(garbage)), "garbage does not decode");
}

}  // namespace

int main() {
    memory_is_honest_about_being_ephemeral();
    ordered_prefix_scan();
    a_commit_survives_the_process();
    an_erase_survives_too();
    a_half_written_commit_never_happened();
    the_record_format_is_stated();
    return report("store");
}
