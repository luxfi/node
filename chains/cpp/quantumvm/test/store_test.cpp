// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// store_test.cpp — the durability the Go reference gets from luxfi/database and
// versiondb, asserted here because this port carries its own.
//
// Go's suite tests the CHAIN's use of that store (block_test.go, edges_test.go);
// this file tests the store itself, because a store this tree wrote is a store
// this tree has to prove.

#include "fixtures.hpp"

#include <cstdio>
#include <string>
#include <unistd.h>

using namespace qvmtest;

namespace {

std::string temp_path(const char* name) {
    return std::string("/tmp/qvm-store-") + std::to_string(::getpid()) + "-" + name;
}

Bytes k(const std::string& s) { return bytes_of(s); }

}  // namespace

// A batch is all there or none of it is, and reads see what was written.
TEST(MemoryHoldsWhatItWasGiven) {
    store::Memory db;
    REQUIRE_ERR(db.get(view(k("absent"))), Err::NotFound);

    store::Batch batch;
    batch[k("a")] = bytes_of("one");
    batch[k("b")] = bytes_of("two");
    REQUIRE_OK(db.write(batch));

    auto a = db.get(view(k("a")));
    REQUIRE_OK(a);
    REQUIRE_EQ(bytes_of("one"), *a);
    REQUIRE_EQ(std::size_t{2}, db.rows());

    store::Batch erase;
    erase[k("a")] = std::nullopt;
    REQUIRE_OK(db.write(erase));
    REQUIRE_ERR(db.get(view(k("a"))), Err::NotFound);
}

// A closed store answers nothing and takes nothing: it says so rather than
// pretending a write landed.
TEST(AClosedStoreRefuses) {
    store::Memory db;
    REQUIRE_OK(db.close());
    REQUIRE_ERR(db.get(view(k("a"))), Err::StoreClosed);
    REQUIRE_ERR(db.write({}), Err::StoreClosed);
}

// The staging layer reads its own writes back before they are durable, which is
// what lets one commit hold a whole height.
TEST(TheStagingLayerReadsItsOwnWrites) {
    store::Memory db;
    store::Version v(&db);

    REQUIRE_OK(v.put(view(k("a")), view(bytes_of("staged"))));
    auto staged = v.get(view(k("a")));
    REQUIRE_OK(staged);
    REQUIRE_EQ(bytes_of("staged"), *staged);
    // and the store underneath has not seen it yet
    REQUIRE_ERR(db.get(view(k("a"))), Err::NotFound);

    REQUIRE_OK(v.commit());
    auto durable = db.get(view(k("a")));
    REQUIRE_OK(durable);
    REQUIRE_EQ(bytes_of("staged"), *durable);
}

// Abort discards what was staged. Staged writes that are not discarded are not
// discarded LATER either — they are flushed, wholesale, by the next commit that
// succeeds.
TEST(AbortLeavesNothingForTheNextCommit) {
    store::Memory db;
    store::Version v(&db);

    REQUIRE_OK(v.put(view(k("orphan")), view(bytes_of("never"))));
    v.abort();
    REQUIRE_ERR(v.get(view(k("orphan"))), Err::NotFound);

    REQUIRE_OK(v.put(view(k("kept")), view(bytes_of("yes"))));
    REQUIRE_OK(v.commit());
    REQUIRE_ERR(db.get(view(k("orphan"))), Err::NotFound);
    REQUIRE_OK(db.get(view(k("kept"))));
}

// A delete staged over a live row hides it from reads before it is committed.
TEST(AStagedDeleteHidesTheRowUnderIt) {
    store::Memory db;
    store::Batch batch;
    batch[k("a")] = bytes_of("live");
    REQUIRE_OK(db.write(batch));

    store::Version v(&db);
    REQUIRE_OK(v.del(view(k("a"))));
    REQUIRE_ERR(v.get(view(k("a"))), Err::NotFound);
    auto held = v.has(view(k("a")));
    REQUIRE_OK(held);
    REQUIRE(!*held);

    REQUIRE_OK(v.commit());
    REQUIRE_ERR(db.get(view(k("a"))), Err::NotFound);
}

// A store on disk comes back holding what it was told, across a reopen.
TEST(TheFileStoreSurvivesAReopen) {
    const std::string path = temp_path("reopen");
    ::unlink(path.c_str());

    {
        auto db = store::File::open(path);
        REQUIRE_OK(db);
        store::Version v(db->get());
        REQUIRE_OK(v.put(view(k("tip")), view(bytes_of("a block id"))));
        REQUIRE_OK(v.commit());
    }

    auto reopened = store::File::open(path);
    REQUIRE_OK(reopened);
    auto got = (*reopened)->get(view(k("tip")));
    REQUIRE_OK(got);
    REQUIRE_EQ(bytes_of("a block id"), *got);
    ::unlink(path.c_str());
}

// A machine that lost power part way through a write comes back WITHOUT that
// batch and with every batch before it. That is the whole crash story: the last
// height either finished or did not happen.
TEST(AHalfWrittenBatchNeverHappened) {
    const std::string path = temp_path("torn");
    ::unlink(path.c_str());

    {
        auto db = store::File::open(path);
        REQUIRE_OK(db);
        store::Batch first;
        first[k("finished")] = bytes_of("yes");
        REQUIRE_OK((*db)->write(first));

        store::Batch second;
        second[k("torn")] = bytes_of("no");
        REQUIRE_OK((*db)->write(second));
    }

    // Truncate the tail: the second record is now a commit that was interrupted.
    std::FILE* f = std::fopen(path.c_str(), "rb");
    REQUIRE(f != nullptr);
    std::fseek(f, 0, SEEK_END);
    const long size = std::ftell(f);
    std::fclose(f);
    REQUIRE(size > 8);
    REQUIRE(::truncate(path.c_str(), size - 8) == 0);

    auto reopened = store::File::open(path);
    REQUIRE_OK(reopened);
    REQUIRE_OK((*reopened)->get(view(k("finished"))));
    REQUIRE_MSG(!(*reopened)->get(view(k("torn"))),
                "a batch that was cut off mid-write came back as if it had landed");
    ::unlink(path.c_str());
}

// The record format round-trips every shape a batch can take, deletes included.
TEST(TheLogRecordRoundTrips) {
    store::Batch batch;
    batch[k("put")] = bytes_of("value");
    batch[k("empty")] = Bytes{};
    batch[k("gone")] = std::nullopt;

    auto back = store::decode_batch(view(store::encode_batch(batch)));
    REQUIRE_OK(back);
    REQUIRE_EQ(batch.size(), back->size());
    REQUIRE(back->at(k("put")).has_value());
    REQUIRE_EQ(bytes_of("value"), *back->at(k("put")));
    REQUIRE(back->at(k("empty")).has_value());
    REQUIRE(back->at(k("empty"))->empty());
    REQUIRE(!back->at(k("gone")).has_value());

    // Bytes that are not a record are refused rather than decoded to an empty
    // batch, which would read as "this commit wrote nothing".
    REQUIRE_ERR(store::decode_batch(view(bytes_of("not a record"))), Err::StoreCorrupt);
}

// The whole chain, over a file: a node writes blocks, stops, and comes back at
// the height it finished.
TEST(AChainOnDiskComesBackAtItsTip) {
    const std::string path = temp_path("chain");
    ::unlink(path.c_str());

    Id tip{};
    {
        auto db = store::File::open(path);
        REQUIRE_OK(db);
        Booted vm = boot_vm_on(quiet_config(), db->get());
        REQUIRE_OK(vm.status);
        for (int i = 0; i < 3; ++i) {
            REQUIRE_OK(vm->pool().add(stamped_tx(next_nonce(), "op")));
            auto blk = vm->build_block();
            REQUIRE_OK(blk);
            REQUIRE_OK((*blk)->verify());
            REQUIRE_OK((*blk)->accept());
            tip = (*blk)->id();
        }
    }

    auto db = store::File::open(path);
    REQUIRE_OK(db);
    Booted restarted = boot_vm_on(quiet_config(), db->get());
    REQUIRE_OK(restarted.status);
    REQUIRE_MSG(tip_of(*restarted.vm) == tip, "the chain came back at a different tip");
    REQUIRE_EQ(std::uint64_t{3}, height_of(*restarted.vm));
    auto stored = restarted->block(tip);
    REQUIRE_OK(stored);
    REQUIRE_EQ(std::uint64_t{3}, (*stored)->height());
    ::unlink(path.c_str());
}
