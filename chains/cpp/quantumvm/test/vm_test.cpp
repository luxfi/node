// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm_test.cpp — ported from Go chains/quantumvm/vm_test.go.

#include "fixtures.hpp"

#include <set>
#include <thread>

using namespace qvmtest;

namespace {

BlockPtr build_on(QuantumVM& vm) {
    (void)vm.pool().add(stamped_tx(next_nonce(), "op"));
    auto blk = vm.build_block();
    return blk ? *blk : nullptr;
}

BlockPtr advance(QuantumVM& vm, int n) {
    BlockPtr last;
    for (int i = 0; i < n; ++i) {
        last = build_on(vm);
        if (!last || !last->verify() || !last->accept()) return nullptr;
    }
    return last;
}

}  // namespace

// TestAnEmptyConfigStartsAUsableVM. The config is normalised in exactly one
// place — initialize — so a VM built by hand and one built from the defaults
// start from the same rules, and neither comes up batching nothing.
TEST(AnEmptyConfigStartsAUsableVM) {
    store::Memory db;
    QuantumVM vm{config::Config{}};
    Init init;
    init.db = &db;
    init.node_id = random_node_id();
    init.chain_id = kTestChain;
    init.network_id = kTestNetwork;
    REQUIRE_OK(vm.initialize(init));

    REQUIRE(vm.configuration().max_parallel_txs > 0);
    REQUIRE(vm.configuration().parallel_batch_size > 0);
    REQUIRE_EQ(config::kAlgorithmDefault, vm.configuration().quantum_algorithm_version);
    REQUIRE_EQ(config::kCommitteeMin, vm.configuration().committee);
    // The settled config and the stock one agree on the parameter set. Two
    // spellings of the default is a chain whose algorithm depends on which door
    // its operator came through.
    REQUIRE_EQ(config::default_config().quantum_algorithm_version,
               vm.configuration().quantum_algorithm_version);
    REQUIRE(!empty(tip_of(vm)));

    // And it builds, verifies and accepts on those settled values.
    vm.clock().set(from_seconds(kChainTime));
    REQUIRE_OK(vm.pool().add(signed_tx(vm, 1, "work")));
    auto blk = vm.build_block();
    REQUIRE_OK(blk);
    REQUIRE_OK((*blk)->verify());
    REQUIRE_OK((*blk)->accept());
}

// An operator who configures a parameter set that is not real gets a refusal at
// boot, not a chain quietly signing under a different one.
TEST(InitializeRefusesAnAlgorithmThatDoesNotExist) {
    config::Config cfg = config::default_config();
    cfg.quantum_algorithm_version = 42;
    store::Memory db;
    QuantumVM vm{cfg};
    Init init;
    init.db = &db;
    init.node_id = random_node_id();
    init.chain_id = kTestChain;
    init.network_id = kTestNetwork;
    REQUIRE_ERR(vm.initialize(init), Err::UnsupportedAlgorithm);
}

// The threshold is derived from the committee, and ⌊2n/3⌋+1 is unanimity for
// every n below four: one absent validator halts the chain, one dishonest
// validator decides it, and the consensus core will not even build the key set
// for it.
TEST(InitializeRefusesACommitteeThatSurvivesNoFault) {
    for (int n : {1, 2, 3}) {
        config::Config cfg = config::default_config();
        cfg.committee = n;
        store::Memory db;
        QuantumVM vm{cfg};
        Init init;
        init.db = &db;
        init.node_id = random_node_id();
        init.chain_id = kTestChain;
        init.network_id = kTestNetwork;
        REQUIRE_MSG(!vm.initialize(init), "a committee of " + std::to_string(n) + " was accepted");
    }
}

// TestInitializeRefusesANodeWithNoIdentity.
//
// Without an identity the VM took the EMPTY node id and started anyway, so every
// node that came up that way signed under one shared name: their signatures
// arrived as duplicates of each other and no threshold above one could be met.
TEST(InitializeRefusesANodeWithNoIdentity) {
    store::Memory db;
    QuantumVM vm{config::default_config()};
    Init init;
    init.db = &db;
    init.chain_id = kTestChain;
    init.network_id = kTestNetwork;
    init.node_id = "";
    REQUIRE_ERR(vm.initialize(init), Err::NoIdentity);
}

// TestEveryNodeSignsUnderItsOwnName.
//
// The threshold counts DISTINCT validator ids, so the id has to name the NODE.
// It named the CHAIN — which every node of a chain shares, by definition — so
// each peer's signature arrived as a duplicate of the first, the count never
// passed one, and no threshold above one could ever be met.
TEST(EveryNodeSignsUnderItsOwnName) {
    store::Memory da, dbb, dc;
    Booted a = boot_vm_on(quiet_config(), &da);
    Booted b = boot_vm_on(quiet_config(), &dbb);
    Booted c = boot_vm_on(quiet_config(), &dc);
    REQUIRE_OK(a.status);
    REQUIRE_OK(b.status);
    REQUIRE_OK(c.status);

    REQUIRE_MSG(a->chain_id() == b->chain_id(), "precondition: one chain");
    REQUIRE_MSG(a->chain_id() == c->chain_id(), "precondition: one chain");

    std::set<std::string> names;
    for (const QuantumVM* vm : {a.vm.get(), b.vm.get(), c.vm.get()}) {
        const std::string id = vm->bridge()->validator_id();
        REQUIRE_MSG(!id.empty(), "the node signs as nobody");
        REQUIRE_MSG(id != text(vm->chain_id()), "the node signs under the chain's name");
        names.insert(id);
    }
    REQUIRE_MSG(names.size() == 3, "three nodes of one chain signed under fewer than three names");
}

// A block with nothing in it costs a round of consensus and settles nothing.
TEST(BuildBlockRefusesAnEmptyMempool) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    REQUIRE_ERR(vm->build_block(), Err::NoPendingTxs);
}

// Transactions that are in the pool but cannot be verified do not make a block.
TEST(BuildBlockRefusesWhenNothingVerifies) {
    store::Memory db;
    Booted vm = boot_vm_on(config::default_config(), &db);  // stamps ON
    REQUIRE_OK(vm.status);
    REQUIRE_OK(vm->pool().add(stamped_tx(1, "junk")));
    REQUIRE_ERR(vm->build_block(), Err::ParallelProcessingFailed);
}

// TestBuildBlockEvictsWhatItCannotVerify.
//
// A quantum stamp only ages. A transaction that fails verification now fails
// forever, and left in the pool it holds its slot for the life of the process —
// enough of them and the pool is full, admission refuses everything, and the
// chain accepts no new work at all.
TEST(BuildBlockEvictsWhatItCannotVerify) {
    config::Config cfg = config::default_config();
    cfg.max_parallel_txs = 4;
    store::Memory db;
    Booted vm = boot_vm_on(cfg, &db);
    REQUIRE_OK(vm.status);

    for (int i = 0; i < 4; ++i) REQUIRE_OK(vm->pool().add(stamped_tx(static_cast<std::uint64_t>(i), "junk")));
    REQUIRE_EQ(std::size_t{4}, vm->pool().count());
    REQUIRE_ERR(vm->pool().add(stamped_tx(99, "more")), Err::PoolFull);

    REQUIRE_ERR(vm->build_block(), Err::ParallelProcessingFailed);
    REQUIRE_MSG(vm->pool().count() == 0,
                "unverifiable transactions kept their pool slots for good");
    REQUIRE_MSG(vm->pool().add(signed_tx(*vm, 100, "real work")).has_value(),
                "the chain could not accept new work behind the wedged transactions");
}

// TestBuildBlockStampsNoEarlierThanItsParent.
//
// Peers' clocks differ, and the skew allowance tolerates that: a slightly fast
// proposer stamps a block ahead of this node's clock and this node accepts it.
// Building the next one then reads a clock that trails its own tip — and a block
// stamped there is one verify refuses for going backwards.
TEST(BuildBlockStampsNoEarlierThanItsParent) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const BlockPtr parent = advance(*vm, 1);
    REQUIRE(parent != nullptr);

    vm->clock().set(from_seconds(parent->timestamp() - kMaxFutureSkewSeconds / 2));
    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);
    REQUIRE_MSG(blk->timestamp() >= parent->timestamp(), "the builder stamped behind its own parent");
    REQUIRE_MSG(blk->verify().has_value(), "a node built a block it will not itself verify");
}

// TestBuildBlockRefusesWhenItsClockTrailsTheTipTooFar.
//
// Clamping the timestamp forward to the parent's keeps a slightly slow node
// building; but a node whose clock trails the tip by MORE than the skew
// allowance clamps to a value its own verify then rejects. The node's clock is
// what is wrong, so the node says so.
TEST(BuildBlockRefusesWhenItsClockTrailsTheTipTooFar) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const BlockPtr parent = advance(*vm, 1);
    REQUIRE(parent != nullptr);

    // Exactly at the allowance still builds, and still verifies.
    vm->clock().set(from_seconds(parent->timestamp() - kMaxFutureSkewSeconds));
    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);
    REQUIRE_OK(blk->verify());
    REQUIRE_OK(blk->accept());

    // One second past it, the node refuses rather than producing a block it will
    // not itself accept.
    vm->clock().set(from_seconds(blk->timestamp() - kMaxFutureSkewSeconds - 1));
    REQUIRE_OK(vm->pool().add(stamped_tx(77, "op")));
    REQUIRE_ERR(vm->build_block(), Err::ClockBehindTip);
}

// Nothing bounded a block anywhere: parse, verify and commit each walked
// whatever arrived, so a peer decided how much memory this node allocated.
TEST(ABlockTooLargeToHoldIsRefusedEverywhere) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    auto genesis = vm->block_at(tip_of(*vm));
    REQUIRE_OK(genesis);

    BlockPtr huge = block_on(*vm, **genesis, stamped_tx(1, std::string(wire::kMaxBlockSize, 'x')));
    REQUIRE_MSG(huge->bytes().size() > wire::kMaxBlockSize, "precondition: over the bound");

    REQUIRE_ERR(vm->parse_block(huge->bytes()), Err::BlockTooLarge);
    REQUIRE_ERR(huge->verify(), Err::BlockTooLarge);

    // And the builder will not produce one either.
    REQUIRE_OK(vm->pool().add(stamped_tx(2, std::string(wire::kMaxBlockSize, 'y'))));
    REQUIRE_ERR(vm->build_block(), Err::BlockTooLarge);
}

TEST(BuildBlockRefusesDuringShutdown) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    REQUIRE_OK(vm->pool().add(stamped_tx(1, "op")));
    REQUIRE_OK(vm->shutdown());
    REQUIRE_ERR(vm->build_block(), Err::VMShutdown);
}

// The engine may call it more than once, and the second call must not report a
// failure to close what is already closed.
TEST(ShutdownIsIdempotent) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    REQUIRE_OK(vm->shutdown());
    REQUIRE_OK(vm->shutdown());
    REQUIRE_MSG(!vm->health().healthy, "a shut-down VM reported itself healthy");
}

// After shutdown the staging layer is closed, so nothing can be read or staged —
// and accept must say so instead of reporting a block it did not store.
TEST(AcceptOnAClosedStoreFailsRatherThanClaimingSuccess) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);
    REQUIRE_OK(vm->shutdown());
    REQUIRE_FAILS(blk->accept());
}

// A peer catching up asks by height; the index is written in the same commit as
// the block, so the two cannot disagree.
TEST(HeightIndexAnswersEveryAcceptedHeight) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    std::vector<Id> accepted{tip_of(*vm)};
    for (int i = 0; i < 4; ++i) {
        BlockPtr b = advance(*vm, 1);
        REQUIRE(b != nullptr);
        accepted.push_back(b->id());
    }

    for (std::size_t height = 0; height < accepted.size(); ++height) {
        auto got = vm->block_id_at_height(height);
        REQUIRE_MSG(got.has_value(), "no answer for height " + std::to_string(height));
        REQUIRE_MSG(*got == accepted[height], "wrong block at height " + std::to_string(height));
    }

    REQUIRE_ERR(vm->block_id_at_height(999), Err::NoBlockAtHeight);
}

// The index answers with an id or an error, never with a truncated one that
// would name a different block.
TEST(HeightIndexRefusesAMalformedEntry) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    const Bytes k = height_key(42);
    REQUIRE_OK(vm->state().put(view(k), view(bytes_of("short"))));
    REQUIRE_ERR(vm->block_id_at_height(42), Err::NoBlockAtHeight);
}

// An unknown id is an error, never a zero-valued block that would then verify
// against nothing.
TEST(GetBlockRefusesWhatItDoesNotHold) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    REQUIRE_FAILS(vm->block(random_id()));
    REQUIRE(vm->get(random_id()) == nullptr);
}

// The store holds bytes, and bytes that are not a block must fail rather than
// decode to a zero block.
TEST(GetBlockRefusesCorruptedBytes) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    const Id id = random_id();
    REQUIRE_OK(vm->state().put(view(id), view(bytes_of("not a block"))));
    REQUIRE_FAILS(vm->block(id));
}

// The responder serves through the parser and the requester admits through it
// too; a disagreement means the fleet serves what it will not accept.
TEST(ParseBlockRoundTripsWhatBuildBlockProduced) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    BlockPtr blk = build_on(*vm);
    REQUIRE(blk != nullptr);

    auto parsed = vm->parse_block(blk->bytes());
    REQUIRE_OK(parsed);
    REQUIRE_EQ(blk->id(), (*parsed)->id());
    REQUIRE_EQ(blk->height(), (*parsed)->height());
    REQUIRE_EQ(blk->transactions().size(), (*parsed)->transactions().size());

    REQUIRE_FAILS(vm->parse_block(view(bytes_of("garbage"))));
    REQUIRE(vm->parse(view(bytes_of("garbage"))) == nullptr);
}

// An idle VM must not claim there is a block to build, and work arriving must
// wake the builder.
TEST(WaitForEventWakesOnWork) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    REQUIRE_MSG(!vm->wait_for_event(std::chrono::milliseconds(30)),
                "an idle VM claimed there was a block to build");
    REQUIRE_OK(vm->pool().add(stamped_tx(1, "op")));
    REQUIRE_MSG(vm->wait_for_event(std::chrono::seconds(5)),
                "work arrived and consensus was never told");
}

TEST(HealthCheckReportsTheChain) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    REQUIRE_OK(vm->pool().add(stamped_tx(1, "op")));

    const Health h = vm->health();
    REQUIRE(h.healthy);
    REQUIRE_EQ(std::string(kVersion), h.version);
    REQUIRE_EQ(std::size_t{1}, h.pending_txs);
}

// The seam questions, answered.
TEST(TheSeamGetsItsFiveAnswers) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    REQUIRE_EQ(std::string("Q"), vm->alias());
    REQUIRE_EQ(kTestChain, vm->chain_id());

    (void)vm->pool().add(stamped_tx(next_nonce(), "op"));
    auto built = vm->build();
    REQUIRE(built != nullptr);
    REQUIRE(built->verify());
    built->accept();

    REQUIRE_EQ(built->id(), vm->last_accepted());
    REQUIRE_EQ(std::uint64_t{1}, vm->last_accepted_height());

    auto got = vm->get(built->id());
    REQUIRE(got != nullptr);
    REQUIRE_EQ(built->id(), got->id());

    const auto wire = built->bytes();
    auto parsed = vm->parse(wire);
    REQUIRE(parsed != nullptr);
    REQUIRE_EQ(built->id(), parsed->id());
    REQUIRE_EQ(built->root(), parsed->root());

    vm->prefer(built->id());  // recorded, decides nothing on this chain
}

// TestStampBlockSurvivesAShortMessage.
//
// The signing lane used the first 32 bytes of whatever it was handed as a key,
// so any message under 32 bytes took the slice out of range and killed the
// process.
TEST(StampBlockSurvivesAShortMessage) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    for (const Bytes& msg : {Bytes{}, bytes_of("x"), bytes_of("under thirty-two bytes")})
        REQUIRE_MSG(vm->stamp_block(random_id(), 7, view(msg)).has_value(),
                    "a " + std::to_string(msg.size()) + "-byte message was refused");
}

// TestStampBlockSignsWithTheNodesOwnKey, and the stamp verifies against the
// message it attests — which it could not do while the node was not registered
// with its own consensus core.
TEST(StampBlockSignsWithTheNodesOwnKey) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);

    const Bytes msg = bytes_of("a round digest to attest");
    auto stamp = vm->stamp_block(random_id(), 1, view(msg));
    REQUIRE_OK(stamp);
    REQUIRE_MSG(std::holds_alternative<quasar::QuasarSig>(*stamp),
                "the bridge is up and produced no Quasar signature");
    REQUIRE_OK(vm->verify_stamp(view(msg), *stamp));
    REQUIRE_FAILS(vm->verify_stamp(view(bytes_of("a different digest")), *stamp));
}

// With no bridge and no block id, the stamp is an ML-DSA signature over the
// message, and it verifies.
TEST(StampBlockFallsBackToMLDSA) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    vm->drop_bridge();

    const Bytes msg = bytes_of("a round digest to attest");
    auto stamp = vm->stamp_block(kEmptyId, 1, view(msg));
    REQUIRE_OK(stamp);
    REQUIRE(std::holds_alternative<quantum::QuantumSignature>(*stamp));
    REQUIRE_OK(vm->verify_stamp(view(msg), *stamp));
    REQUIRE_FAILS(vm->verify_stamp(view(bytes_of("another message")), *stamp));
}

// TestVerifyStampChecksTheSignatureAgainstTheMessage.
//
// It took no message, so no arm could check a signature against anything: each
// looked at the stamp's own SHAPE, and shape is what the sender chose. A
// two-byte aggregate declaring three signers passed; so did a one-byte BLS
// signature. A self-declared signer count is not evidence of anything.
TEST(VerifyStampChecksTheSignatureAgainstTheMessage) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const auto bridge = vm->bridge();
    const int threshold = bridge->threshold();
    const Bytes msg = bytes_of("the message under attestation");

    auto sig = bridge->sign_block(random_id(), view(msg), 1);
    REQUIRE_OK(sig);
    REQUIRE_OK(vm->verify_stamp(view(msg), Stamp(*sig)));

    quasar::QuasarSig forged;
    forged.bls = Bytes{1};
    forged.validator_id = bridge->validator_id();
    REQUIRE_MSG(!vm->verify_stamp(view(msg), Stamp(forged)),
                "a one-byte BLS signature was accepted");
    REQUIRE_MSG(!vm->verify_stamp(view(bytes_of("another message")), Stamp(*sig)),
                "a signature was accepted for a message it does not sign");

    // An aggregate: a self-declared signer count buys nothing.
    quasar::AggregatedSignature two_bytes;
    two_bytes.bls_aggregated = Bytes{1, 2};
    two_bytes.signer_count = threshold;
    REQUIRE_MSG(!vm->verify_stamp(view(msg), Stamp(two_bytes)),
                "two bytes declaring a quorum were accepted as an aggregate");

    quasar::AggregatedSignature inflated;
    inflated.bls_aggregated = Bytes{1};
    inflated.signer_count = threshold + 100;
    REQUIRE_MSG(!vm->verify_stamp(view(msg), Stamp(inflated)),
                "declaring more signers made a forgery verify");

    quasar::AggregatedSignature bodyless;
    bodyless.signer_count = threshold;
    REQUIRE_FAILS(vm->verify_stamp(view(msg), Stamp(bodyless)));

    // An ML-DSA stamp is checked the same way.
    REQUIRE_FAILS(vm->verify_stamp(view(msg), Stamp(quantum::QuantumSignature{})));
    REQUIRE_ERR(vm->verify_stamp(view(msg), Stamp{}), Err::NoStamp);

    // With no bridge there is nothing to check a Quasar stamp against, so it is
    // refused rather than waved through.
    vm->drop_bridge();
    REQUIRE_FAILS(vm->verify_stamp(view(msg), Stamp(*sig)));
    quasar::AggregatedSignature any;
    any.bls_aggregated = Bytes{1};
    any.signer_count = threshold;
    REQUIRE_FAILS(vm->verify_stamp(view(msg), Stamp(any)));
}

// The other half: an aggregate built from a verified quorum verifies, over the
// message it was built on and no other.
TEST(VerifyStampAcceptsAQuorumAggregate) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    const auto bridge = vm->bridge();

    const Id block_id = random_id();
    const Bytes msg(block_id.begin(), block_id.end());
    REQUIRE_OK(bridge->sign_block(block_id, view(msg), 1));

    for (const char* peer : {"peer-1", "peer-2"}) {
        REQUIRE_OK(bridge->add_validator(peer, 1));
        auto peer_sig = bridge->core()->sign(peer, view(msg));
        REQUIRE_OK(peer_sig);
        REQUIRE_OK(bridge->add_signature(block_id, &*peer_sig));
    }

    auto final_state = bridge->try_finalize(block_id);
    REQUIRE_OK(final_state);
    REQUIRE(final_state->finalized);
    REQUIRE_OK(vm->verify_stamp(view(msg), Stamp(final_state->aggregate)));
    REQUIRE_FAILS(vm->verify_stamp(view(bytes_of("another block")), Stamp(final_state->aggregate)));
}

// Q-Chain sells no blockspace: cert inclusion is a validator obligation, so no
// amount buys it (LP-0130 §6).
TEST(FeePolicyRefusesEveryUserTransaction) {
    store::Memory db;
    Booted vm = boot_vm_on(quiet_config(), &db);
    REQUIRE_OK(vm.status);
    REQUIRE(vm->fee_policy() != nullptr);

    for (std::uint64_t paid : {std::uint64_t{0}, std::uint64_t{1'000'000}, std::uint64_t{1'000'000'000}}) {
        auto tx = std::make_shared<BaseTransaction>(kChainTime, paid, Bytes{});
        tx->set_signature(*stamped_tx(paid, "x")->signature());
        tx->set_fee(paid);
        REQUIRE_EQ(paid, tx->fee());
        REQUIRE_ERR(vm->issue_tx(*tx), Err::ChainAcceptsNoUserTxs);
    }
    REQUIRE_MSG(vm->pool().count() == 0, "a refused transaction still reached the pool");

    // Consensus-internal work reaches the pool directly, which is the only way in.
    REQUIRE_OK(vm->pool().add(stamped_tx(1, "cert")));
    REQUIRE_EQ(std::size_t{1}, vm->pool().count());
}

// An unset policy refuses rather than admitting everything.
TEST(IssueTxWithoutAPolicyFailsClosed) {
    QuantumVM vm{quiet_config()};  // never initialized: no policy
    REQUIRE_ERR(vm.issue_tx(*stamped_tx(1, "op")), Err::NoFeePolicy);
    REQUIRE_EQ(std::size_t{0}, vm.pool().count());
}
