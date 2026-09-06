// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// state_test.cpp — the three stores the shielded pool rests on: the spent set,
// the UTXO set, and the committed state root.
//
// The through-line is one rule: A READ THAT FAILED IS NOT AN ABSENT ROW.
// Reporting a spent-set read failure as "not spent" is exactly how an
// already-spent note gets spent again, and a chain that boots on an unreadable
// root believing the shielded state is empty disagrees with the network on every
// block it sees, permanently.

#include "check.hpp"
#include "fixtures.hpp"

#include "lux/zkvm/nullifier.hpp"
#include "lux/zkvm/root.hpp"
#include "lux/zkvm/store.hpp"
#include "lux/zkvm/utxo.hpp"

#include <functional>

using namespace lux::zkvm;
using namespace lux::zkvm::test;

namespace {

// Broken fails every READ with something that is not an absence. It is the store
// a dead disk gives you.
class Broken final : public store::Store {
public:
    explicit Broken(store::Store& real) : real_(&real) {}

    store::Result<std::optional<Bytes>> get(ByteView) const override {
        return std::unexpected("disk gone");
    }
    store::Result<void> put(ByteView k, ByteView v) override { return real_->put(k, v); }
    store::Result<void> erase(ByteView k) override { return real_->erase(k); }
    store::Result<void> each(ByteView p,
                             const std::function<bool(ByteView, ByteView)>& f) const override {
        return real_->each(p, f);
    }
    store::Result<void> commit() override { return real_->commit(); }

private:
    store::Store* real_;
};

// Unscannable fails the ENUMERATION, which is what a boot does.
class Unscannable final : public store::Store {
public:
    store::Result<std::optional<Bytes>> get(ByteView) const override {
        return std::optional<Bytes>{};
    }
    store::Result<void> put(ByteView, ByteView) override { return {}; }
    store::Result<void> erase(ByteView) override { return {}; }
    store::Result<void> each(ByteView,
                             const std::function<bool(ByteView, ByteView)>&) const override {
        return std::unexpected("iterator gone");
    }
    store::Result<void> commit() override { return {}; }
};

void the_spent_set_follows_the_records() {
    std::printf("the spent set is rebuilt from the records, and counted off them\n");

    store::Memory db;
    // The record landed; a counter write, had there been one, did not.
    const Bytes key = make_nullifier_key(view(b("orphan")));
    check_ok(db.put(view(key), view(Bytes(8, 0))), "an orphan record is on disk");

    auto ndb = NullifierDb::open(db);
    check_ok(ndb, "the set opens");
    if (!ndb) return;
    auto spend = (*ndb)->spent_at(view(b("orphan")));
    check(spend && spend->spent, "the set is rebuilt from the records");
    check((*ndb)->count() == 1,
          "and the count is read off them, so it cannot disagree with what it describes");
}

void the_spent_set_is_permanent() {
    std::printf("\na note is spent once, and stays spent\n");
    store::Memory db;
    auto ndb = NullifierDb::open(db);
    if (!ndb) {
        check(false, "the set opens");
        return;
    }
    check((*ndb)->count() == 0, "a fresh chain has spent nothing");

    check_ok((*ndb)->mark_spent(view(b("a")), 1), "a note is spent");
    check_ok((*ndb)->mark_spent(view(b("b")), 2), "and another");
    check((*ndb)->count() == 2, "the count follows");

    check_err((*ndb)->mark_spent(view(b("a")), 3), kErrNullifierSpent,
              "spending the same note again is refused");
    check((*ndb)->count() == 2, "and changes nothing");

    auto at = (*ndb)->spent_at(view(b("b")));
    check(at && at->spent && at->height == 2, "the set records WHICH block spent the note");

    auto never = (*ndb)->spent_at(view(b("never")));
    check(never && !never->spent, "and an unspent note reads as unspent");
}

void a_failed_read_is_not_an_unspent_note() {
    std::printf("\na read that FAILED is not an unspent note\n");
    store::Memory real;
    Broken broken(real);
    auto ndb = NullifierDb::open(broken);
    check_ok(ndb, "the set opens over a store whose reads fail");
    if (!ndb) return;
    auto r = (*ndb)->spent_at(view(b("n")));
    check_err(r, "disk gone", "the failure is reported as a failure");
}

void a_read_does_not_write_the_set() {
    std::printf("\na read does not memoise what it loaded\n");
    store::Memory db;
    auto ndb = NullifierDb::open(db);
    if (!ndb) {
        check(false, "the set opens");
        return;
    }
    check_ok((*ndb)->mark_spent(view(b("n")), 4), "a note is spent");

    // A record on disk that the in-memory set does not know about is what a
    // memoising read would fill in.
    (*ndb)->forget_cache();
    auto r = (*ndb)->spent_at(view(b("n")));
    check(r && r->spent && r->height == 4, "the record on disk still reads as spent");
    check((*ndb)->count() == 0, "and the read did not change the set it read");
}

void a_short_record_is_not_a_height() {
    std::printf("\na record that is not eight bytes is not a height\n");
    // Blocks are stored in the same store under their own keys, so the nullifier
    // prefix can turn up over a value that is not a height. The boot scan passes
    // over anything that is not eight bytes; the read path has to agree with it,
    // or reading one of those is a crash where a miss was meant.
    store::Memory db;
    check_ok(db.put(view(make_nullifier_key(view(b("n")))), view(Bytes{1})),
             "a one-byte value sits under the prefix");
    auto ndb = NullifierDb::open(db);
    check_ok(ndb, "the set opens");
    if (!ndb) return;
    check((*ndb)->count() == 0, "the boot scan passed over it");
    check_err((*ndb)->spent_at(view(b("n"))), kErrNotAHeight, "and so does the read path");
}

void a_set_that_cannot_be_read_does_not_open() {
    std::printf("\na set that cannot be read at startup is not an empty set\n");
    Unscannable dead;
    check_err(NullifierDb::open(dead), "iterator gone", "the spent set refuses to open");
    check_err(UtxoDb::open(dead), "iterator gone", "and so does the UTXO set");
}

void the_utxo_set_survives_a_restart() {
    std::printf("\nthe UTXO set is rebuilt at boot, so a restart reaches the same verdict\n");
    store::Memory db;

    Utxo u;
    u.tx_id = id_of(7);
    u.commitment = b("commitment");
    u.height = 3;

    auto live = UtxoDb::open(db);
    check_ok(live, "the set opens");
    if (!live) return;
    check_ok((*live)->add(u), "an output is created");
    check_err((*live)->add(u), kErrUtxoExists, "and a duplicate commitment is refused");
    check((*live)->count() == 1, "the count follows the set");

    auto restarted = UtxoDb::open(db);
    check_ok(restarted, "a restarted node opens the same records");
    if (!restarted) return;
    check_err((*restarted)->add(u), kErrUtxoExists,
              "and reaches the SAME verdict a running one did");
    check((*restarted)->count() == 1, "with the same count");
}

void the_utxo_body_comes_back_whole() {
    std::printf("\nan output's body comes back whole, not just its key\n");
    store::Memory db;
    auto live = UtxoDb::open(db);
    if (!live) {
        check(false, "the set opens");
        return;
    }
    for (std::uint8_t i = 0; i < 3; ++i) {
        Utxo u;
        u.tx_id = id_of(i);
        u.output_index = i;
        u.commitment = Bytes{'c', i};
        u.ciphertext = Bytes{'n', i};
        u.height = 5;
        check_ok((*live)->add(u), "an output is created");
    }

    auto restarted = UtxoDb::open(db);
    if (!restarted) {
        check(false, "the set reopens");
        return;
    }
    check((*restarted)->count() == 3, "the set is rebuilt from the records");
    for (std::uint8_t i = 0; i < 3; ++i) {
        auto got = (*restarted)->get(view(Bytes{'c', i}));
        check_ok(got, "an output reads back");
        if (got) {
            check(got->height == 5, "at the height it was created");
            check(got->ciphertext == (Bytes{'n', i}), "with its ciphertext, not just its key");
        }
    }
    check_err((*restarted)->get(view(b("never"))), kErrNoUtxo,
              "and a commitment nobody created is a miss");
}

void a_failed_utxo_read_is_not_an_absent_output() {
    std::printf("\na failed UTXO read is not an absent output\n");
    store::Memory real;
    Broken broken(real);
    auto udb = UtxoDb::open(broken);
    check_ok(udb, "the set opens");
    if (!udb) return;
    auto r = (*udb)->get(view(b("c")));
    check_err(r, "disk gone", "a read that failed says so");
    check(r.has_value() == false && r.error().find(kErrNoUtxo) == std::string::npos,
          "and is NOT reported as 'no such utxo'");
}

void the_root_is_a_pure_fold_that_only_finalize_advances() {
    std::printf("\nthe state root: a pure fold, advanced only by finalize\n");
    store::Memory db;
    auto root = Root::open(db);
    check_ok(root, "the root opens");
    if (!root) return;

    check_eq(hex_of((*root)->get()), hex_of(Id{}), "a fresh chain has the empty root");

    const std::vector<Transaction> txs = {block_tx(1)};
    const Id first = (*root)->after(txs);
    const Id again = (*root)->after(txs);
    check_eq(hex_of(first), hex_of(again),
             "computing a root twice gives the same answer — the fold mutates nothing");
    check_eq(hex_of((*root)->get()), hex_of(Id{}), "and the committed root has not moved");

    check_ok((*root)->finalize(first), "finalize advances it");
    check_eq(hex_of((*root)->get()), hex_of(first), "to the value it was given");

    // The fold is over the committed root, so it moves with it.
    check(hex_of((*root)->after(txs)) != hex_of(first),
          "and the next fold builds on the new committed root");

    auto reopened = Root::open(db);
    check_ok(reopened, "the root reopens");
    if (reopened) check_eq(hex_of((*reopened)->get()), hex_of(first), "at the committed value");
}

void an_unreadable_root_is_not_an_empty_root() {
    std::printf("\nan unreadable root is not an empty root\n");
    store::Memory real;
    Broken broken(real);
    check_err(Root::open(broken), "disk gone", "the root refuses to open");

    // A root of the wrong length is not a root either.
    store::Memory db;
    check_ok(db.put(ByteView(reinterpret_cast<const std::uint8_t*>(kRootKey), 10),
                    view(Bytes(7, 0))),
             "a seven-byte value sits under the root key");
    check_err(Root::open(db), "want 32", "and it is refused by length");
}

void the_root_has_one_definition() {
    std::printf("\nthe root has exactly ONE definition\n");
    // committed ‖ every output commitment (tx order) ‖ every nullifier (tx order)
    store::Memory db;
    auto root = Root::open(db);
    if (!root) {
        check(false, "the root opens");
        return;
    }
    const std::vector<Transaction> txs = {block_tx(1)};

    Hasher h;
    h.write(view((*root)->get()));
    for (const auto& tx : txs)
        for (const auto& o : tx.outputs) h.write(view(o.commitment));
    for (const auto& tx : txs)
        for (const auto& n : tx.nullifiers) h.write(view(n));

    check_eq(hex_of((*root)->after(txs)), hex_of(h.sum()),
             "and it is that fold, on every node, with or without an accelerator");
}

}  // namespace

int main() {
    the_spent_set_follows_the_records();
    the_spent_set_is_permanent();
    a_failed_read_is_not_an_unspent_note();
    a_read_does_not_write_the_set();
    a_short_record_is_not_a_height();
    a_set_that_cannot_be_read_does_not_open();
    the_utxo_set_survives_a_restart();
    the_utxo_body_comes_back_whole();
    a_failed_utxo_read_is_not_an_absent_output();
    the_root_is_a_pure_fold_that_only_finalize_advances();
    an_unreadable_root_is_not_an_empty_root();
    the_root_has_one_definition();
    return report("state");
}
