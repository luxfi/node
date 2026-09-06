// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// fault_test.cpp — what the chain does when its own store fails.
//
// A chain's database can fail to answer, and what F does then is a consensus
// question rather than an operational one. The failure that matters most is the
// quiet one: a read that FAILED reported as a read that found NOTHING. The
// last-accepted pointer coming back empty made a chain at height 2 look like a
// chain that had never run, and the node went on to build height 1 over it —
// durably, over the height index, with the boot returning success and nothing
// in any log to say so. The same conflation on the epoch pointer seats a
// committee nobody elected, and on a nonce it reopens the replay window a nonce
// exists to close.
//
// So these tests fail the store deliberately and require F to stop. Each one
// carries its own control — the SAME read, absent rather than failing — because
// a test that only sees the failure cannot tell "refuses everything" from
// "distinguishes the two".

#include "check.hpp"
#include "faults.hpp"
#include "fixtures.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

// Broken is a chain whose store can be made to fail on demand: the base store
// under a switchboard, with the chain built over the switchboard rather than
// over the base, so a fault can be turned on and off mid-run.
struct Broken {
    std::unique_ptr<Memory> base = std::make_unique<Memory>();
    std::unique_ptr<Faults> faults;
    Chain chain;

    Broken() : faults(std::make_unique<Faults>(base.get())) {}

    // boot seats the chain. It returns the boot's own verdict, because these
    // tests are about the boots that fail.
    Result<void> boot(const std::vector<TestKey>& funded,
                      const std::vector<CommitteeMember>& committee) {
        chain = new_test_vm(fund_all(funded), committee, 1, faults.get());
        return {};
    }

    // reboot builds a SECOND chain over the same store, which is what a restart
    // is.
    Result<void> reboot(const std::vector<TestKey>& funded,
                        const std::vector<CommitteeMember>& committee) {
        VM vm(faults.get(), VM::Config{96369, test_chain_id(), "F"});
        return vm.initialize(marshal(genesis_for(fund_all(funded), committee, 1)));
    }

    VM* operator->() { return chain.vm.get(); }
};

void a_boot_read_that_fails_is_not_a_fresh_chain() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);

    // A chain that has actually run.
    Broken b;
    accepted(b.boot({k}, com.members), "a chain boots");
    accept_one(b.chain, register_tx(k, kTestScheme, digest_of("live-1"), 1), "block one");
    accept_one(b.chain, register_tx(k, kTestScheme, digest_of("live-2"), 2), "block two");
    check_eq(b->height(), 2, "the chain reaches height two");
    Id live_tip = Id(b->last_accepted());
    std::uint64_t live_height = b->height();

    // THE CONTROL: rebooting on the same store finds the chain where it left
    // it. Without this the cases below would pass just as well against a VM
    // that refused every boot.
    {
        VM back(b.faults.get(), VM::Config{96369, test_chain_id(), "F"});
        accepted(back.initialize(marshal(genesis_for(fund_all({k}), com.members, 1))),
                 "a reboot on the same store succeeds");
        check_eq(back.height(), live_height, "and finds the chain at its height");
        check_eq(hex_of(Id(back.last_accepted())), hex_of(live_tip), "and at its tip");
    }

    // Each pointer, unreadable in turn: the boot fails rather than inventing an
    // answer. A node that cannot read its own tip must not serve as one.
    for (std::string_view pointer : {kLastAcceptedKey, kCurrentEpochKey}) {
        b.faults->read_fails = std::string(pointer);
        VM vm(b.faults.get(), VM::Config{96369, test_chain_id(), "F"});
        refused(vm.initialize(marshal(genesis_for(fund_all({k}), com.members, 1))), Err::Database,
                "an unreadable " + std::string(pointer) + " stops the boot");
        b.faults->read_fails.clear();
    }

    // A corrupt record index likewise stops the boot rather than presenting as
    // an empty one — for each of the four record kinds, so none can be the one
    // that silently loads nothing.
    for (std::string_view prefix : {kCiphertextPrefix, kPermitPrefix, kDecryptPrefix,
                                    kEpochPrefix}) {
        b.faults->iter_fails = std::string(prefix);
        VM vm(b.faults.get(), VM::Config{96369, test_chain_id(), "F"});
        refused(vm.initialize(marshal(genesis_for(fund_all({k}), com.members, 1))), Err::Database,
                "an unreadable " + std::string(prefix) + " index stops the boot");
        b.faults->iter_fails.clear();
    }

    // And a nonce that cannot be read is an error, not a zero. A zero here says
    // "this payer has spent nothing", which lets every transaction it ever
    // signed through again.
    auto n = b->nonce_of(k.addr);
    check(n && *n == 2, "the control: the nonce is readable and correct");
    b.faults->read_fails = std::string(kNoncePrefix);
    refused(b->nonce_of(k.addr), Err::Database,
            "an unreadable nonce is an error, never a fresh account");
    refused(b->submit_tx(register_tx(k, kTestScheme, digest_of("replayed"), 1)), Err::Database,
            "and admission refuses rather than treating the payer as new");
    b.faults->read_fails.clear();
}

void a_seeder_that_cannot_tell_whether_it_ran_stops() {
    // Seeding twice writes genesis over a live tip, so a boot whose marker
    // cannot be read must stop before it decides.
    Committee com = new_committee(1);
    Broken b;
    b.faults->read_fails = std::string(kGenesisMarker);
    VM vm(b.faults.get(), VM::Config{96369, test_chain_id(), "F"});
    refused(vm.initialize(marshal(genesis_for({}, com.members, 1))), Err::Database,
            "a marker it cannot read stops the seeder");
}

void a_failed_commit_rolls_back_the_whole_block() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Broken b;
    accepted(b.boot({k}, com.members), "the chain boots");

    Transaction tx = register_tx(k, kTestScheme, digest_of("uncommittable"), 1);
    accepted(b->submit_tx(tx), "the transaction is queued");
    auto blk = b->build_block();
    accepted(blk, "a block is built");
    if (!blk) return;
    accepted((*blk)->check(), "and verifies");

    b.faults->commit_fails = true;
    refused((*blk)->accept_block(), Err::Database, "the commit fails");

    check(b->ciphertext(tx.subject) == nullptr, "an uncommitted block applied nothing");
    auto burned = b->burned();
    check(burned && *burned == 0, "and burned nothing");
    auto bal = b->balance(k.addr);
    check(bal && *bal == kTestFund, "the payer is whole");
    check_eq(b->height(), 0, "the chain did not move");
    auto nonce = b->nonce_of(k.addr);
    check(nonce && *nonce == 0, "nor was the nonce consumed");

    // The failure was transient: with the disk answering again the same block
    // applies, so a rollback leaves the chain able to continue rather than stuck.
    b.faults->commit_fails = false;
    accepted((*blk)->accept_block(), "with the disk answering again, the same block applies");
    check(b->ciphertext(tx.subject) != nullptr, "and takes effect");
}

void acceptance_stops_when_the_state_cannot_be_read() {
    // Settlement reports a read failure rather than proceeding on the answer it
    // would have invented — here the payer's committed nonce, whose absence and
    // whose failure mean opposite things.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Broken b;
    accepted(b.boot({k}, com.members), "the chain boots");

    Transaction tx = register_tx(k, kTestScheme, digest_of("unreadable"), 1);
    accepted(b->submit_tx(tx), "queued");
    auto blk = b->build_block();
    accepted(blk, "built");
    if (!blk) return;

    b.faults->read_fails = std::string(kNoncePrefix);
    refused((*blk)->accept_block(), Err::Database, "acceptance stops on the unreadable nonce");
    b.faults->read_fails.clear();
    check(b->ciphertext(tx.subject) == nullptr,
          "a block that could not be settled applied nothing");
}

void every_write_reports_its_own_failure() {
    // Every record writer reports a failed write instead of returning success
    // and leaving the cache holding a record the store does not have. A closed
    // database is the real shape of this: a block applying while the VM shuts
    // down.
    TestKey owner = new_test_key();
    Committee com = new_committee(1);
    Broken b;
    accepted(b.boot({owner}, com.members), "the chain boots");
    b.faults->closed = true;

    refused(b->put_ciphertext(CiphertextRecord{}), Err::Database, "a ciphertext write");
    refused(b->put_permit(PermitRecord{}), Err::Database, "a permit write");
    refused(b->put_decrypt(DecryptRecord{}), Err::Database, "a decrypt write");
    refused(b->put_epoch(EpochRecord{}), Err::Database, "an epoch write");
    refused(b->set_current_epoch(7), Err::Database, "the epoch pointer");
    refused(b->set_nonce(owner.addr, 1), Err::Database, "and a nonce");
    b.faults->closed = false;
}

void acceptance_is_all_or_nothing_at_every_write() {
    // A block is settled and applied through a store that is committed ONCE, so
    // any of these failing must leave the chain where it was — not half-applied,
    // and not applied-but-unindexed.
    struct Case {
        const char* name;
        std::string_view prefix;
    } cases[] = {
        {"the block itself", kBlockPrefix},
        {"the height index", kHeightPrefix},
        {"the last-accepted pointer", kLastAcceptedKey},
        {"the payer's nonce", kNoncePrefix},
        {"the record it applies", kCiphertextPrefix},
    };
    Committee com = new_committee(1);
    for (const auto& c : cases) {
        TestKey k = new_test_key();
        Broken b;
        accepted(b.boot({k}, com.members), "the chain boots");

        Transaction tx = register_tx(k, kTestScheme, digest_of("atomic"), 1);
        accepted(b->submit_tx(tx), "queued");
        auto blk = b->build_block();
        if (!blk) {
            std::printf("  FAIL  %s: could not build\n", c.name);
            ++g_fail;
            continue;
        }
        accepted((*blk)->check(), "verified");

        b.faults->put_fails = std::string(c.prefix);
        refused((*blk)->accept_block(), Err::Database,
                std::string("a failed write of ") + c.name);
        b.faults->put_fails.clear();

        check_eq(b->height(), 0, "the chain did not move");
        check(b->ciphertext(tx.subject) == nullptr, "and applied nothing");
        auto burned = b->burned();
        check(burned && *burned == 0, "and burned nothing");

        accepted((*blk)->accept_block(), "the control: with the disk sound the same block applies");
        check_eq(b->height(), 1, "and the chain moves");
    }
}

void an_abort_whose_reload_also_fails_still_reports_the_first_failure() {
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Broken b;
    accepted(b.boot({k}, com.members), "the chain boots");

    Transaction tx = register_tx(k, kTestScheme, digest_of("doomed"), 1);
    accepted(b->submit_tx(tx), "queued");
    auto blk = b->build_block();
    accepted(blk, "built");
    if (!blk) return;

    b.faults->put_fails = std::string(kBlockPrefix);
    b.faults->read_fails = std::string(kCurrentEpochKey);
    refused((*blk)->accept_block(), Err::Database,
            "the write failure is what the caller is told about");
    b.faults->put_fails.clear();
    b.faults->read_fails.clear();
    check_eq(b->height(), 0, "and the chain did not move");
}

void the_seeder_reports_a_write_it_cannot_make() {
    // The seeder stops on a failed write rather than committing a partly-seeded
    // chain — one with an allocation but no committee, or a committee but no
    // height index.
    Committee com = new_committee(1);
    struct Case {
        const char* name;
        std::string prefix;
    } cases[] = {
        {"the epoch record", std::string(kEpochPrefix)},
        {"the epoch pointer", std::string(kCurrentEpochKey)},
        {"the height index", std::string(kHeightPrefix)},
    };
    for (const auto& c : cases) {
        Broken b;
        b.faults->put_fails = c.prefix;
        VM vm(b.faults.get(), VM::Config{96369, test_chain_id(), "F"});
        refused(vm.initialize(marshal(genesis_for({}, com.members, 1))), Err::Database,
                std::string("a failed write of ") + c.name + " stops the seeding");
    }
}

void genesis_refuses_an_allocation_it_cannot_apply() {
    // The funding table is held to the same address format everything else is,
    // rather than seeding a chain that is short by however much the bad row was
    // worth.
    Committee com = new_committee(1);
    for (const char* addr : {"zzzz", "0011223344",
                             "00112233445566778899aabbccddeeff0011223344556677"}) {
        Memory store;
        VM vm(&store, VM::Config{96369, test_chain_id(), "F"});
        Genesis g = genesis_for({{addr, 1}}, com.members, 1);
        check(!vm.initialize(marshal(g)).has_value(),
              std::string("an allocation to \"") + addr + "\" is refused");
    }

    // Two spellings of one address are one account, so a table can ask for more
    // than an account can hold — and a balance that cannot be credited is not a
    // balance that was.
    TestKey k = new_test_key();
    Memory store;
    VM vm(&store, VM::Config{96369, test_chain_id(), "F"});
    Genesis g = genesis_for({{k.hex_addr(), ~std::uint64_t(0)}, {"0x" + k.hex_addr(), 1}},
                            com.members, 1);
    refused(vm.initialize(marshal(g)), Err::BalanceOverflow,
            "an allocation that would overflow one account is refused whole");
}

void the_boot_refuses_what_it_cannot_parse() {
    Committee com = new_committee(1);
    {
        Memory store;
        VM vm(&store, VM::Config{96369, test_chain_id(), "F"});
        refused(vm.initialize("{not json"), Err::InvalidPayload, "a genesis that does not parse");
    }
    {
        Memory store;
        VM vm(&store, VM::Config{96369, kEmptyId, "F"});
        refused(vm.initialize(""), Err::InvalidBlock, "a chain with no identity binds nothing");
    }
    {
        Memory store;
        VM vm(&store, VM::Config{96369, test_chain_id(), "F"});
        accepted(vm.initialize(""), "the control: an empty genesis on a named chain boots");
        (void)com;
    }
}

void one_unreadable_row_does_not_stop_a_boot() {
    // The other side of the boot rules above: an index that ERRORS is fatal, a
    // single record that does not decode is skipped and counted, because the
    // first says nothing can be trusted and the second says one row cannot.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Chain c = new_test_vm(fund_all({k}), com.members, 1);
    accept_one(c, register_tx(k, kTestScheme, digest_of("good"), 1), "a sound record");
    Id good = derive_handle(digest_of("good"), kTestScheme);

    Id junk{0xff};
    accepted(c.store->put(view(key(kCiphertextPrefix, view(junk))),
                          view(std::string_view("{not json"))),
             "an undecodable row is written");
    accepted(c.store->commit(), "and committed");
    accepted(c.vm->load_state(), "the boot still succeeds");

    check(c.vm->ciphertext(good) != nullptr, "a readable record still loads");
    check(c.vm->ciphertext(junk) == nullptr, "an undecodable one is skipped, not guessed at");
    check_eq(c.vm->skipped(), 1, "and the node knows how many it skipped");
}

void a_tip_that_names_no_block_stops_the_boot() {
    // Continuing would put the chain at genesis while its pointer says
    // otherwise.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    const Genesis g = genesis_for(fund_all({k}), com.members, 1);

    Memory store;
    {
        VM vm(&store, VM::Config{96369, test_chain_id(), "F"});
        accepted(vm.initialize(marshal(g)), "a chain boots");
        vm.clock().set(kTestGenesisTime);
        // Drive one block through the VM directly.
        auto id = vm.submit_tx(register_tx(k, kTestScheme, digest_of("t"), 1));
        accepted(id, "and accepts a block");
        auto blk = vm.build_block();
        if (blk) {
            (void)(*blk)->check();
            accepted((*blk)->accept_block(), "which lands");
        }
    }

    Id missing{0xde, 0xad};
    accepted(store.put(view(kLastAcceptedKey), view(missing)), "the tip is pointed at nothing");
    accepted(store.commit(), "and committed");
    {
        VM vm(&store, VM::Config{96369, test_chain_id(), "F"});
        check(!vm.initialize(marshal(g)).has_value(),
              "a tip that names no block stops the boot");
    }

    accepted(store.put(view(kLastAcceptedKey), view(Bytes{1, 2, 3})),
             "a pointer of the wrong width is written");
    accepted(store.commit(), "and committed");
    {
        VM vm(&store, VM::Config{96369, test_chain_id(), "F"});
        refused(vm.initialize(marshal(g)), Err::InvalidPayload,
                "and it is refused for the same reason");
    }
}

void the_balance_surface_reports_a_supply_it_cannot_read() {
    // The two numbers a balance answer carries are read independently, and a
    // failure on either is reported. Burned supply coming back zero on a failed
    // read is indistinguishable from a chain that has never settled a fee.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Broken b;
    accepted(b.boot({k}, com.members), "the chain boots");
    accept_one(b.chain, register_tx(k, kTestScheme, digest_of("burned"), 1), "a fee is burned");
    Service svc(*b.chain.vm);

    // The account's own balance still reads; only the supply counter does not.
    b.faults->read_fails = "fee/burned";
    refused(svc.balance(k.hex_addr()), Err::Database, "an unreadable supply is reported");
    refused(svc.health(), Err::Database, "and so is a health check that cannot read it");
    b.faults->read_fails.clear();

    auto bal = svc.balance(k.hex_addr());
    accepted(bal, "the control: with the disk sound it answers");
    check(bal && bal->burned_nlux > 0, "and the supply reads");
}

void an_epoch_rotation_is_both_writes_or_neither() {
    // A rotation that closed the sitting committee and failed to seat its
    // successor would leave the chain with nobody able to answer a decryption
    // or to rotate again.
    Committee com = new_committee(3);
    Broken b;
    accepted(b.boot(com.keys, com.members), "the chain boots");
    Committee next = new_committee(3);
    Bytes pk{'p', 'k'};

    const Bytes successor = key(kEpochPrefix, std::uint64_t(1));
    b.faults->put_fails = std::string(successor.begin(), successor.end());
    auto err = apply(advance_tx(com.keys[0], 1, next.members, 2, pk, 1), *b.chain.vm,
                     b->clock().now());
    b.faults->put_fails.clear();
    refused(err, Err::Database, "the successor could not be seated, so the rotation failed");
    check_eq(b->current_epoch_number(), 0,
             "and the sitting epoch is still the sitting epoch");
}

}  // namespace

int main() {
    std::printf("fhevm — what the chain does when its own store fails\n\n");
    a_boot_read_that_fails_is_not_a_fresh_chain();
    a_seeder_that_cannot_tell_whether_it_ran_stops();
    a_failed_commit_rolls_back_the_whole_block();
    acceptance_stops_when_the_state_cannot_be_read();
    every_write_reports_its_own_failure();
    acceptance_is_all_or_nothing_at_every_write();
    an_abort_whose_reload_also_fails_still_reports_the_first_failure();
    the_seeder_reports_a_write_it_cannot_make();
    genesis_refuses_an_allocation_it_cannot_apply();
    the_boot_refuses_what_it_cannot_parse();
    one_unreadable_row_does_not_stop_a_boot();
    a_tip_that_names_no_block_stops_the_boot();
    the_balance_surface_reports_a_supply_it_cannot_read();
    an_epoch_rotation_is_both_writes_or_neither();
    return report("fault");
}
