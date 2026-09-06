// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// replay_test.cpp — F COMMITS NO STATE ROOT, and this is what stands in its
// place.
//
// The package's design turns on determinism: F owns its persistence rather than
// writing through the FHE runtime's registry, because that registry stamps the
// wall clock and two validators replaying one block would then store different
// bytes. That argument is only worth making if the property it protects is
// actually checked — and a chain with no state root has nothing in consensus
// that checks it. Two validators could diverge and neither would learn.
//
// So the property is checked here instead, the only way it can be from outside
// consensus: replay the same blocks on two independently-built nodes and
// require the resulting databases to be byte-identical. That catches a wall
// clock, a random value, an unordered map walk and a float, all at once, and it
// catches them by the EFFECT they have rather than by the names they are spelled
// with.
//
// Committing a root IN consensus needs something this VM does not have: a state
// layer per in-flight block. Today verify reads committed state, so a proposer
// cannot know its post-state until acceptance, and a root checked only at
// acceptance would be a halt an adversary could trigger by proposing a wrong
// one — strictly worse than no root. The same missing layer is why a child of a
// verified-but-unaccepted block from the SAME payer cannot be verified. One
// architectural change closes both; neither is closed by half of it.

#include "check.hpp"
#include "fixtures.hpp"

using namespace lux::fhevm;
using namespace lux::fhevm::test;

namespace {

struct Seed {
    Id handle{};
    Id permit_id{};
};

Seed seed_permit(Chain& c, const TestKey& owner, const TestKey& grantee, std::uint32_t ops,
                 std::int64_t expiry) {
    Seed s;
    Transaction reg = register_tx(owner, kTestScheme, digest_of("seed-" + owner.hex_addr()), 1);
    accept_one(c, reg, "seed: register");
    s.handle = reg.subject;
    accept_one(c, grant_tx(owner, s.handle, grantee.addr, ops, expiry, 2), "seed: grant");
    s.permit_id = derive_permit_id(s.handle, owner.addr, grantee.addr, ops, expiry, 2);
    return s;
}

// replay_node builds a node from bytes identical to every other node's, so that
// any difference in the result comes from applying the block and nothing else.
// That includes the chain id: these nodes are validators of ONE chain, as
// production's are, and the id is not a parameter because there is nothing for
// a caller to vary. A harness that handed each node its own would be describing
// a shape that does not exist, and would excuse the binding it should be
// checking.
Chain replay_node(const Genesis& g) {
    Chain c;
    c.owned = std::make_unique<Memory>();
    c.store = c.owned.get();
    c.vm = std::make_unique<VM>(c.store, VM::Config{96369, test_chain_id(), "F"});
    auto ok = c.vm->initialize(marshal(g));
    if (!ok) {
        std::printf("  FAIL  a replay node did not boot: %s\n", ok.error().message().c_str());
        ++g_fail;
    }
    return c;
}

void a_replay_is_byte_identical() {
    // The property the whole persistence decision rests on: two validators
    // handed the same blocks write the same database.
    TestKey owner = new_test_key();
    TestKey grantee = new_test_key();
    Committee com = new_committee(3);
    std::vector<TestKey> funded = com.keys;
    funded.insert(funded.end(), {owner, grantee});

    Genesis g = genesis_for(fund_all(funded), com.members, 2);

    // The producer runs the whole lifecycle, so the blocks exercise every
    // operation and every record type.
    Chain producer = replay_node(g);
    producer.vm->clock().set(1'700'000'100);

    Seed s = seed_permit(producer, owner, grantee, kPermitOpDecrypt, 0);
    accept_one(producer, request_tx(grantee, kTestScheme, s.handle, s.permit_id, 0, 1),
               "the request");
    Id request_id = derive_request_id(s.handle, grantee.addr, 1);
    Id result = digest_of("agreed-result");
    accept_one(producer, fulfill_tx(com.keys[0], request_id, result, 1), "an attestation");
    accept_one(producer, fulfill_tx(com.keys[1], request_id, result, 1), "and another");
    Committee next = new_committee(3);
    Bytes next_pk{'e', 'p', 'o', 'c', 'h', '-', '1'};
    accept_one(producer, advance_tx(com.keys[0], 1, next.members, 2, next_pk, 2), "a rotation vote");
    accept_one(producer, advance_tx(com.keys[1], 1, next.members, 2, next_pk, 2), "and the second");
    accept_one(producer, revoke_tx(owner, s.permit_id, 3), "and a revocation");

    check_eq(producer.vm->height(), 8, "one block per operation, every kind exercised");
    check_eq(producer.vm->current_epoch_number(), 1, "and the committee rotated");

    // Collect the chain as it went out on the wire.
    std::vector<Bytes> wire;
    for (std::uint64_t h = 1; h <= producer.vm->height(); ++h) {
        auto id = producer.vm->block_id_at_height(h);
        if (!id) {
            std::printf("  FAIL  height %llu is not indexed\n", (unsigned long long)h);
            ++g_fail;
            return;
        }
        auto blk = producer.vm->get_block(*id);
        if (!blk) {
            std::printf("  FAIL  block at height %llu is not stored\n", (unsigned long long)h);
            ++g_fail;
            return;
        }
        auto b = (*blk)->bytes();
        wire.emplace_back(b.begin(), b.end());
    }
    check(wire.size() == 8, "the whole chain is collected off the wire");

    // Two fresh nodes replay it. Their clocks differ from the producer's and
    // from each other's, because chain time must come from the block.
    Chain a = replay_node(g);
    a.vm->clock().set(1'900'000'000);
    Chain b = replay_node(g);
    b.vm->clock().set(2'100'000'000);

    for (Chain* node : {&a, &b}) {
        for (const Bytes& raw : wire) {
            auto blk = node->vm->parse_block(view(raw));
            if (!blk) {
                std::printf("  FAIL  a replaying node could not parse a block: %s\n",
                            blk.error().message().c_str());
                ++g_fail;
                return;
            }
            auto ok = (*blk)->check();
            if (!ok) {
                std::printf("  FAIL  a replaying node could not verify a block: %s\n",
                            ok.error().message().c_str());
                ++g_fail;
                return;
            }
            (*blk)->verify();
            ok = (*blk)->accept_block();
            if (!ok) {
                std::printf("  FAIL  a replaying node could not accept a block: %s\n",
                            ok.error().message().c_str());
                ++g_fail;
                return;
            }
        }
    }

    check_eq(producer.vm->dump(), a.vm->dump(),
             "a replaying node writes byte-for-byte what the producer wrote");
    check_eq(a.vm->dump(), b.vm->dump(), "and two replaying nodes agree with each other");
    check_eq(hex_of(producer.vm->records_root()), hex_of(a.vm->records_root()),
             "which the records root says in one value");

    // The replayed state is not merely equal, it is the right state.
    check_eq(a.vm->current_epoch_number(), 1, "the replayed chain rotated too");
    const DecryptRecord* rec = a.vm->decrypt(request_id);
    check(rec != nullptr && rec->status == RequestStatus::Completed, "its request completed");
    check(rec != nullptr && rec->result_handle == result, "on the result the committee agreed");
    const PermitRecord* pm = a.vm->permit(s.permit_id);
    check(pm != nullptr && pm->status == kStatusRevoked, "and its permit is revoked");
}

void a_replay_is_independent_of_the_wall_clock() {
    // The specific hazard the persistence decision was made to avoid: a record
    // whose timestamp came from the validator rather than the block. Both nodes
    // replay with wildly different clocks; if any stored field read a clock, the
    // dumps diverge.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    std::vector<TestKey> funded = com.keys;
    funded.push_back(k);
    Genesis g = genesis_for(fund_all(funded), com.members, 1);

    Chain producer = replay_node(g);
    producer.vm->clock().set(1'700'000'500);
    accept_one(producer, register_tx(k, kTestScheme, digest_of("stamped"), 1), "one block");
    auto tip = producer.vm->get_block(Id(producer.vm->last_accepted()));
    check(tip.has_value(), "the producer has a tip");
    if (!tip) return;
    auto span = (*tip)->bytes();
    Bytes raw(span.begin(), span.end());

    Chain follower = replay_node(g);
    follower.vm->clock().set(1'700'090'000);  // a day later
    auto blk = follower.vm->parse_block(view(raw));
    accepted(blk, "the follower parses it");
    if (!blk) return;
    accepted((*blk)->check(), "verifies it");
    accepted((*blk)->accept_block(), "and accepts it");

    check_eq(producer.vm->dump(), follower.vm->dump(),
             "and writes exactly what the producer wrote");

    const CiphertextRecord* rec =
        follower.vm->ciphertext(derive_handle(digest_of("stamped"), kTestScheme));
    check(rec != nullptr, "the record is there");
    check(rec != nullptr && rec->registered_at == 1'700'000'500,
          "carrying the block's time, not the node's");
}

void the_dump_is_a_function_of_the_data() {
    // The comparison above proves nothing unless the dump can actually DIFFER.
    // Two chains that accepted different blocks must not agree.
    TestKey k = new_test_key();
    Committee com = new_committee(1);
    std::vector<TestKey> funded = com.keys;
    funded.push_back(k);
    Genesis g = genesis_for(fund_all(funded), com.members, 1);

    Chain a = replay_node(g);
    a.vm->clock().set(1'700'000'500);
    Chain b = replay_node(g);
    b.vm->clock().set(1'700'000'500);
    check_eq(a.vm->dump(), b.vm->dump(), "two fresh nodes on one genesis agree");

    accept_one(a, register_tx(k, kTestScheme, digest_of("only-on-a"), 1), "a block on one of them");
    check(a.vm->dump() != b.vm->dump(), "and one block apart, they do not");
    check(hex_of(a.vm->records_root()) != hex_of(b.vm->records_root()),
          "which the root says too");

    // And the same block on both brings them back together, so what the dump
    // tracks is the DATA and not the order of operations that produced it.
    auto tip = a.vm->get_block(Id(a.vm->last_accepted()));
    if (!tip) return;
    auto span = (*tip)->bytes();
    Bytes raw(span.begin(), span.end());
    auto blk = b.vm->parse_block(view(raw));
    accepted(blk, "the other node takes the same block");
    if (!blk) return;
    accepted((*blk)->check(), "verifies it");
    accepted((*blk)->accept_block(), "and accepts it");
    check_eq(a.vm->dump(), b.vm->dump(), "after which they agree again");
}

void a_store_survives_the_process() {
    // The other half of persistence: a chain that forgets what it accepted when
    // the process exits is not a chain — it would re-sign a height it already
    // signed. The durable store's log is replayed on open, and a commit that
    // never finished never happened.
    const std::string path = "/tmp/fhevm-replay-store.log";
    std::remove(path.c_str());

    TestKey k = new_test_key();
    Committee com = new_committee(1);
    Genesis g = genesis_for(fund_all({k}), com.members, 1);

    Id tip{};
    std::uint64_t height = 0;
    std::string dumped;
    {
        auto file = File::open(path);
        accepted(file, "a durable store opens");
        if (!file) return;
        Chain c;
        c.store = file->get();
        c.vm = std::make_unique<VM>(c.store, VM::Config{96369, test_chain_id(), "F"});
        accepted(c.vm->initialize(marshal(g)), "a chain boots on it");
        c.vm->clock().set(kTestGenesisTime);
        accept_one(c, register_tx(k, kTestScheme, digest_of("durable"), 1), "and accepts a block");
        tip = Id(c.vm->last_accepted());
        height = c.vm->height();
        dumped = c.vm->dump();
    }

    {
        auto file = File::open(path);
        accepted(file, "the store re-opens");
        if (!file) return;
        VM vm(file->get(), VM::Config{96369, test_chain_id(), "F"});
        accepted(vm.initialize(marshal(g)), "and the chain boots again");
        check_eq(vm.height(), height, "at the height it left off");
        check_eq(hex_of(Id(vm.last_accepted())), hex_of(tip), "on the tip it left off");
        check_eq(vm.dump(), dumped, "holding exactly what it wrote");
        check(vm.ciphertext(derive_handle(digest_of("durable"), kTestScheme)) != nullptr,
              "including the record the block applied");
    }

    // A process killed mid-append leaves a partial trailing record. Replay
    // detects it and truncates: a half-written commit never happened.
    {
        std::FILE* f = std::fopen(path.c_str(), "ab");
        check(f != nullptr, "the log can be appended to");
        if (f != nullptr) {
            const char junk[9] = "ZAP\0\1\0\0\0";
            std::fwrite(junk, 1, 8, f);
            std::fclose(f);
        }
        auto file = File::open(path);
        accepted(file, "the store opens over a torn tail");
        if (file) {
            VM vm(file->get(), VM::Config{96369, test_chain_id(), "F"});
            accepted(vm.initialize(marshal(g)), "and the chain still boots");
            check_eq(vm.dump(), dumped, "holding exactly what it held before");
        }
    }
    std::remove(path.c_str());
}

}  // namespace

int main() {
    std::printf("fhevm — two nodes, one chain, and the database they must agree on\n\n");
    a_replay_is_byte_identical();
    a_replay_is_independent_of_the_wall_clock();
    the_dump_is_a_function_of_the_data();
    a_store_survives_the_process();
    return report("replay");
}
