// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// vm_test.cpp — the whole chain, through the node's VM seam.
//
// Ported from Go vms/platformvm/vm_test.go and block/executor's verifier and
// acceptor tests. These drive the chain the way the node drives it: build,
// verify, accept, and read the state back — so a rule that only holds inside a
// unit test fails here.

#include "harness.hpp"
#include "lux/platformvm/vm.hpp"
#include "signing.hpp"

using namespace lux::platformvm;
namespace ex = lux::platformvm::executor;

namespace {

Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
NodeId node_of(std::uint8_t b) {
    NodeId v{};
    for (std::size_t i = 0; i < kNodeIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

const Id kLux = id_of(0x10);
const Id kPChain = id_of(0x20);
constexpr std::uint32_t kNetworkId = 96369;
constexpr std::uint64_t kGenesisTime = 1'000'000;
constexpr std::uint64_t kSupply = 360'000'000'000'000;

pvmtest::Key& key() {
    static pvmtest::Key k(1);
    return k;
}
const signer::ProofOfPossession& pop() {
    static pvmtest::BlsKey k(7);
    return k.pop();
}

OutputOwners mine() { return OutputOwners{0, 1, {key().address()}}; }

const ex::FlatFee& fees() {
    static ex::FlatFee f(1'000'000);
    return f;
}

ex::Backend make_backend() {
    ex::Backend b;
    b.runtime = Runtime{kNetworkId, kPChain, kLux};
    b.policy.min_validator_stake = 2'000'000'000;
    b.policy.max_validator_stake = 3'000'000'000'000;
    b.policy.min_delegator_stake = 25'000'000;
    b.policy.min_stake_duration = 24 * 60 * 60;
    b.policy.max_stake_duration = 365 * 24 * 60 * 60;
    b.policy.min_delegation_fee = 20'000;
    b.reward_config.max_consumption_rate = 120'000;
    b.reward_config.min_consumption_rate = 100'000;
    b.reward_config.minting_period = 365 * reward::kDay;
    b.reward_config.supply_cap = 720'000'000'000'000;
    b.fees = &fees();
    b.bootstrapped = true;
    b.now = kGenesisTime;
    return b;
}

vm::Genesis genesis(std::uint64_t funds = 10'000'000'000) {
    vm::Genesis g;
    g.timestamp = kGenesisTime;
    g.initial_supply = kSupply;
    UTXO u;
    u.utxo = UtxoId{id_of(0xA0), 0};
    u.asset = kLux;
    u.out = TransferOutput{funds, mine()};
    g.utxos.push_back(u);
    return g;
}

TransferableOutput out_to_me(std::uint64_t amt) {
    return TransferableOutput{kLux, 0, TransferOutput{amt, mine()}};
}

BaseTx envelope(std::uint64_t funds, std::vector<TransferableOutput> outs) {
    BaseTx b;
    b.network_id = kNetworkId;
    b.blockchain_id = kPChain;
    b.outs = std::move(outs);
    TransferableInput in;
    in.utxo = UtxoId{id_of(0xA0), 0};
    in.asset = kLux;
    in.in = TransferInput{funds, {0}};
    b.ins = {in};
    return b;
}

txs::Tx sign(std::shared_ptr<txs::UnsignedTx> u) {
    txs::Tx tx;
    tx.unsigned_tx = std::move(u);
    tx.creds.push_back(key().sign(tx.unsigned_tx->bytes()));
    (void)tx.initialize();
    return tx;
}

txs::Tx join_tx(std::uint64_t funds, std::uint64_t stake, std::uint64_t end) {
    const std::uint64_t change = funds - stake - 1'000'000;
    auto u = txs::AddPermissionlessValidatorTx::create(
        envelope(funds, {out_to_me(change)}), txs::Validator{node_of(0x90), 0, end, stake}, kPrimaryNetworkId,
        signer::Signer{pop()}, {TransferableOutput{kLux, 0, TransferOutput{stake, mine()}}}, mine(), mine(),
        20'000);
    return sign(u.value());
}

}  // namespace

// The seam's own questions, answered.
TEST(TheChainIdentifiesItself) {
    vm::PlatformVM chain(kPChain, make_backend(), genesis());
    lux::node::Id want{};
    std::memcpy(want.data(), kPChain.b.data(), kIdLen);
    REQUIRE(chain.chain_id() == want);
    REQUIRE_EQ(std::string("P"), chain.alias());
    REQUIRE_U64(0u, chain.last_accepted_height());
    REQUIRE_U64(kGenesisTime, chain.accepted().timestamp());
}

// A validator joins through the whole path: submitted, built into a block,
// verified, accepted — and only then in the set.
TEST(AValidatorJoinsThroughABlock) {
    vm::PlatformVM chain(kPChain, make_backend(), genesis());
    const std::uint64_t end = kGenesisTime + 90 * 24 * 60 * 60;
    const auto tx = join_tx(10'000'000'000, 5'000'000'000, end);
    chain.submit(tx);

    auto blk = chain.build();
    REQUIRE(blk != nullptr);
    REQUIRE_U64(1u, blk->height());

    // Nothing has happened to the chain yet.
    REQUIRE_ERR(chain.accepted().get_current_validator(kPrimaryNetworkId, node_of(0x90)), Err::NotFound);

    REQUIRE(blk->verify());
    // The commitment is real: it is what executing the block produced.
    REQUIRE(!(blk->root() == lux::node::kEmptyId));
    REQUIRE_ERR(chain.accepted().get_current_validator(kPrimaryNetworkId, node_of(0x90)), Err::NotFound);

    blk->accept();
    REQUIRE_OK(chain.accepted().get_current_validator(kPrimaryNetworkId, node_of(0x90)));
    REQUIRE(chain.last_accepted() == blk->id());
    REQUIRE_U64(1u, chain.last_accepted_height());
    REQUIRE_EQ_NUM(0, chain.mempool_size());
}

// A block travels as bytes: parsed by another node, it has the same id and
// verifies to the same root. That is the whole reason the bytes are canonical.
TEST(ABlockCrossesTheWire) {
    vm::PlatformVM sender(kPChain, make_backend(), genesis());
    vm::PlatformVM receiver(kPChain, make_backend(), genesis());
    const std::uint64_t end = kGenesisTime + 90 * 24 * 60 * 60;
    sender.submit(join_tx(10'000'000'000, 5'000'000'000, end));

    auto built = sender.build();
    REQUIRE(built != nullptr);
    REQUIRE(built->verify());

    const std::vector<std::uint8_t> wire(built->bytes().begin(), built->bytes().end());
    auto received = receiver.parse(wire);
    REQUIRE(received != nullptr);
    REQUIRE(received->id() == built->id());
    REQUIRE(received->parent() == built->parent());
    REQUIRE_U64(built->height(), received->height());

    // The receiver re-executes rather than taking the sender's word for the root.
    REQUIRE(received->verify());
    REQUIRE(received->root() == built->root());

    received->accept();
    built->accept();
    REQUIRE(receiver.last_accepted() == sender.last_accepted());
    REQUIRE(state::state_root(receiver.accepted()) == state::state_root(sender.accepted()));
}

// Garbage does not parse, and a block whose parent is unknown does not verify.
TEST(TheChainRefusesWhatItCannotPlace) {
    vm::PlatformVM chain(kPChain, make_backend(), genesis());
    const std::vector<std::uint8_t> junk = {1, 2, 3, 4};
    REQUIRE(chain.parse(junk) == nullptr);

    auto orphan = block::StandardBlock::create(kGenesisTime, id_of(0xCC), 1, {});
    REQUIRE_OK(orphan);
    const std::vector<std::uint8_t> wire(orphan.value()->bytes().begin(), orphan.value()->bytes().end());
    auto parsed = chain.parse(wire);
    REQUIRE(parsed != nullptr);
    REQUIRE(!parsed->verify());
}

// A block at the wrong height is refused, however well formed it is.
TEST(HeightMustFollowItsParent) {
    vm::PlatformVM chain(kPChain, make_backend(), genesis());
    const auto genesis_id = chain.last_accepted();
    Id parent{};
    std::memcpy(parent.b.data(), genesis_id.data(), kIdLen);

    auto wrong = block::StandardBlock::create(kGenesisTime, parent, 7, {});
    REQUIRE_OK(wrong);
    const std::vector<std::uint8_t> wire(wrong.value()->bytes().begin(), wrong.value()->bytes().end());
    auto parsed = chain.parse(wire);
    REQUIRE(parsed != nullptr);
    REQUIRE(!parsed->verify());
}

// The reward decision, end to end: a proposal block and the two option blocks
// that could follow it. Whichever is accepted, the staker leaves; only one pays.
TEST(TheRewardDecision) {
    const std::uint64_t end = kGenesisTime + 90 * 24 * 60 * 60;

    // Two chains from the same genesis, so the same proposal can be settled
    // both ways and the two outcomes compared.
    // The harness's REQUIRE returns from the enclosing function, so the run
    // writes its answers out rather than returning them.
    std::uint64_t potential_out = 0, supply_before = 0, supply_after = 0;
    auto run = [&](bool commit) {
        vm::PlatformVM chain(kPChain, make_backend(), genesis());
        chain.submit(join_tx(10'000'000'000, 5'000'000'000, end));
        auto join = chain.build();
        REQUIRE_MSG(join != nullptr, "the join block was not built");
        REQUIRE_MSG(join->verify(), "the join block did not verify");
        join->accept();

        const auto admitted = chain.accepted().get_current_validator(kPrimaryNetworkId, node_of(0x90));
        REQUIRE_MSG(admitted.has_value(), "the validator did not join");
        const std::uint64_t potential = admitted.value().potential_reward;
        const std::uint64_t supply = chain.accepted().current_supply(kPrimaryNetworkId).value();

        // Move the wall clock to the staker's end: now the chain's own business
        // is to settle the reward, and the builder says so.
        chain.set_wall_clock(end);
        auto proposal = chain.build();
        REQUIRE_MSG(proposal != nullptr, "the proposal block was not built");
        auto* inner = dynamic_cast<vm::VmBlock*>(proposal.get());
        REQUIRE_MSG(inner != nullptr, "the built block is not a VM block");
        REQUIRE_MSG(inner->inner().kind() == block::Kind::Proposal, "the builder did not propose");
        REQUIRE_MSG(proposal->verify(), inner->refusal());
        proposal->accept();

        // Neither outcome has been applied: the staker is still there.
        REQUIRE_MSG(chain.accepted().get_current_validator(kPrimaryNetworkId, node_of(0x90)).has_value(),
                    "the proposal removed the staker before the vote");

        Id proposal_id{};
        std::memcpy(proposal_id.b.data(), proposal->id().data(), kIdLen);
        auto option = commit ? std::static_pointer_cast<block::Block>(
                                   block::CommitBlock::create(end, proposal_id, proposal->height() + 1).value())
                             : std::static_pointer_cast<block::Block>(
                                   block::AbortBlock::create(end, proposal_id, proposal->height() + 1).value());
        const std::vector<std::uint8_t> wire(option->bytes().begin(), option->bytes().end());
        auto opt = chain.parse(wire);
        REQUIRE_MSG(opt != nullptr, "the option block did not parse");
        auto* opt_inner = dynamic_cast<vm::VmBlock*>(opt.get());
        REQUIRE_MSG(opt->verify(), opt_inner->refusal());
        opt->accept();

        // Either way the staker is gone.
        REQUIRE_MSG(!chain.accepted().get_current_validator(kPrimaryNetworkId, node_of(0x90)).has_value(),
                    "the staker was not removed");
        potential_out = potential;
        supply_before = supply;
        supply_after = chain.accepted().current_supply(kPrimaryNetworkId).value();
    };

    run(true);
    // On commit the minted supply stays where it is: the reward was paid.
    REQUIRE(potential_out > 0);
    REQUIRE_U64(supply_before, supply_after);

    run(false);
    // On abort the supply minted for a reward nobody got is given back.
    REQUIRE_U64(supply_before - potential_out, supply_after);
}

// A block whose clock runs backwards, or too far ahead of real time, is refused.
TEST(TheClockIsBounded) {
    vm::PlatformVM chain(kPChain, make_backend(), genesis());
    const auto genesis_id = chain.last_accepted();
    Id parent{};
    std::memcpy(parent.b.data(), genesis_id.data(), kIdLen);

    auto backwards = block::StandardBlock::create(kGenesisTime - 1, parent, 1, {});
    REQUIRE_OK(backwards);
    std::vector<std::uint8_t> wire(backwards.value()->bytes().begin(), backwards.value()->bytes().end());
    auto b1 = chain.parse(wire);
    REQUIRE(!b1->verify());

    auto ahead = block::StandardBlock::create(kGenesisTime + ex::kSyncBound + 1, parent, 1, {});
    REQUIRE_OK(ahead);
    wire.assign(ahead.value()->bytes().begin(), ahead.value()->bytes().end());
    auto b2 = chain.parse(wire);
    REQUIRE(!b2->verify());
}

// A block that changes nothing should never have been issued, and is refused.
TEST(AnEmptyBlockIsRefused) {
    vm::PlatformVM chain(kPChain, make_backend(), genesis());
    const auto genesis_id = chain.last_accepted();
    Id parent{};
    std::memcpy(parent.b.data(), genesis_id.data(), kIdLen);

    auto empty = block::StandardBlock::create(kGenesisTime, parent, 1, {});
    REQUIRE_OK(empty);
    const std::vector<std::uint8_t> wire(empty.value()->bytes().begin(), empty.value()->bytes().end());
    auto parsed = chain.parse(wire);
    REQUIRE(parsed != nullptr);
    REQUIRE(!parsed->verify());

    // And the builder does not offer one.
    REQUIRE(chain.build() == nullptr);
}

// Two transactions spending the same output cannot ride in one block: both
// would verify against the state as it stood before either ran.
TEST(ConflictingTxsCannotShareABlock) {
    vm::PlatformVM chain(kPChain, make_backend(), genesis());
    const std::uint64_t end = kGenesisTime + 90 * 24 * 60 * 60;

    // Two different transactions, each spending the one funding output.
    const auto a = join_tx(10'000'000'000, 5'000'000'000, end);
    const auto b = join_tx(10'000'000'000, 6'000'000'000, end);
    REQUIRE(!(a.tx_id == b.tx_id));

    auto blk = block::StandardBlock::create(kGenesisTime, [&] {
        Id p{};
        std::memcpy(p.b.data(), chain.last_accepted().data(), kIdLen);
        return p;
    }(), 1, {a, b});
    REQUIRE_OK(blk);
    const std::vector<std::uint8_t> wire(blk.value()->bytes().begin(), blk.value()->bytes().end());
    auto parsed = chain.parse(wire);
    REQUIRE(parsed != nullptr);
    REQUIRE(!parsed->verify());

    // The builder does not make that mistake: it takes one and leaves the other.
    chain.submit(a);
    chain.submit(b);
    auto built = chain.build();
    REQUIRE(built != nullptr);
    REQUIRE(built->verify());
    auto* inner = dynamic_cast<vm::VmBlock*>(built.get());
    REQUIRE_EQ_NUM(1, inner->inner().decision_txs().size());
}
