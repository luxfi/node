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
#include "lux/platformvm/uptime.hpp"
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
    // LP-103: price by bandwidth alone at one µLUX per byte, which keeps the
    // arithmetic in these cases readable while still going through the real
    // mechanism — the chain reads its price off its own excess.
    b.gas_config.weights[gas::Bandwidth] = 1;
    b.gas_config.max_capacity = 1'000'000;
    b.gas_config.max_per_second = 1'000;
    b.gas_config.target_per_second = 100;
    b.gas_config.min_price = 1;
    b.gas_config.excess_conversion_constant = 100'000;
    b.bootstrapped = true;
    b.now = kGenesisTime;
    return b;
}

vm::Genesis genesis(std::uint64_t funds = 10'000'000'000) {
    vm::Genesis g;
    g.timestamp = kGenesisTime;
    g.fee_state = gas::State{1'000'000, 0};
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

// A plain payment spending the change a previous transaction left. It carries
// no policy of its own; it exists so a block can be built, and a block is how
// the chain's clock moves.
txs::Tx pay_tx(const Id& src, std::uint64_t amount, std::uint64_t fee) {
    BaseTx b;
    b.network_id = kNetworkId;
    b.blockchain_id = kPChain;
    b.outs = {out_to_me(amount - fee)};
    TransferableInput in;
    in.utxo = UtxoId{src, 0};
    in.asset = kLux;
    in.in = TransferInput{amount, {0}};
    b.ins = {in};
    return sign(txs::BaseTxUnsigned::create(b).value());
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

// LP-103 is live: a block spends the chain's gas capacity and raises its excess,
// and the price the next block pays is read off that excess.
TEST(ABlockSpendsTheChainsGas) {
    vm::PlatformVM chain(kPChain, make_backend(), genesis());
    const std::uint64_t end = kGenesisTime + 90 * 24 * 60 * 60;
    chain.submit(join_tx(10'000'000'000, 5'000'000'000, end));

    const auto before = chain.accepted().fee_state();
    REQUIRE_U64(0u, before.excess);

    auto blk = chain.build();
    REQUIRE(blk != nullptr);
    REQUIRE(blk->verify());
    blk->accept();

    const auto after = chain.accepted().fee_state();
    REQUIRE(after.excess > 0);
    REQUIRE(after.capacity < before.capacity);

    // The price follows the excess, so the same transaction now costs more.
    const auto cheap = ex::pick_fee_calculator(chain.backend().gas_config, chain.accepted());
    const auto at_zero = ex::DynamicFee(chain.backend().gas_config.weights,
                                        gas::calculate_price(chain.backend().gas_config.min_price, 0,
                                                             chain.backend().gas_config
                                                                 .excess_conversion_constant));
    auto probe = txs::BaseTxUnsigned::create(envelope(10'000'000'000, {out_to_me(1)}));
    REQUIRE_OK(probe);
    auto now_price = cheap.calculate(*probe.value());
    auto then_price = at_zero.calculate(*probe.value());
    REQUIRE_OK(now_price);
    REQUIRE_OK(then_price);
    REQUIRE(now_price.value() >= then_price.value());
}

// A block asking for more gas than the chain has is refused as a block, before
// any of its transactions run.
TEST(ABlockBeyondCapacityIsRefused) {
    auto b = make_backend();
    // A chain with almost no capacity left.
    auto g = genesis();
    g.fee_state = gas::State{1, 0};
    vm::PlatformVM chain(kPChain, b, g);

    const std::uint64_t end = kGenesisTime + 90 * 24 * 60 * 60;
    const auto tx = join_tx(10'000'000'000, 5'000'000'000, end);

    Id parent{};
    std::memcpy(parent.b.data(), chain.last_accepted().data(), kIdLen);
    auto blk = block::StandardBlock::create(kGenesisTime, parent, 1, {tx});
    REQUIRE_OK(blk);
    const std::vector<std::uint8_t> wire(blk.value()->bytes().begin(), blk.value()->bytes().end());
    auto parsed = chain.parse(wire);
    REQUIRE(parsed != nullptr);
    REQUIRE(!parsed->verify());
    auto* inner = dynamic_cast<vm::VmBlock*>(parsed.get());
    REQUIRE(inner->refusal().find("InsufficientCapacity") != std::string::npos);

    // And with no room for the clock to advance — so no capacity to refill —
    // the builder offers nothing rather than a block that cannot be accepted.
    chain.set_wall_clock(kGenesisTime - ex::kSyncBound);
    chain.submit(tx);
    REQUIRE(chain.build() == nullptr);

    // Give the clock room and the capacity refills, so the same transaction
    // becomes affordable. That is the mechanism working, not a loophole.
    chain.set_wall_clock(kGenesisTime + 100);
    auto later = chain.build();
    REQUIRE(later != nullptr);
    REQUIRE(later->verify());
}

// The reward gate. A proposal block has two children, and which one this node
// prefers is the validator's own uptime against the requirement that bound it.
namespace {
struct FixedUptime final : uptime::Calculator {
    explicit FixedUptime(double p) : percent(p) {}
    Result<double> percent_from(const NodeId&, const Id&, std::uint64_t) const override {
        if (percent < 0) return fail(Err::NotFound, "no uptime recorded");
        return percent;
    }
    double percent;
};
}  // namespace

TEST(TheRewardGate) {
    const std::uint64_t end = kGenesisTime + 90 * 24 * 60 * 60;

    // uptime: what the node measured; expect_commit: which child it prefers.
    auto run = [&](double uptime_percent, bool expect_commit) {
        auto b = make_backend();
        b.policy.uptime_requirement = 800'000;  // 80%
        FixedUptime u(uptime_percent);
        b.uptimes = &u;

        vm::PlatformVM chain(kPChain, b, genesis());
        chain.submit(join_tx(10'000'000'000, 5'000'000'000, end));
        auto join = chain.build();
        REQUIRE_MSG(join != nullptr, "the join block was not built");
        REQUIRE_MSG(join->verify(), "the join block did not verify");
        join->accept();

        chain.set_wall_clock(end);
        auto proposal = chain.build();
        REQUIRE_MSG(proposal != nullptr, "the proposal block was not built");
        auto* inner = dynamic_cast<vm::VmBlock*>(proposal.get());
        REQUIRE_MSG(proposal->verify(), inner->refusal());
        proposal->accept();

        const auto* pb = dynamic_cast<const block::ProposalBlock*>(&inner->inner());
        REQUIRE_MSG(pb != nullptr, "the built block is not a proposal");
        auto opts = chain.options(*pb);
        REQUIRE_MSG(opts.has_value(), "options were not produced");

        auto* preferred = dynamic_cast<vm::VmBlock*>(opts.value().first.get());
        auto* alternate = dynamic_cast<vm::VmBlock*>(opts.value().second.get());
        const auto want = expect_commit ? block::Kind::Commit : block::Kind::Abort;
        const auto other = expect_commit ? block::Kind::Abort : block::Kind::Commit;
        REQUIRE_MSG(preferred->inner().kind() == want, "the wrong child was preferred");
        REQUIRE_MSG(alternate->inner().kind() == other, "the wrong child was the alternate");

        // Both children are real blocks the chain will accept.
        REQUIRE_MSG(opts.value().first->verify(), preferred->refusal());
    };

    run(0.95, true);   // met the requirement: pay it
    run(0.80, true);   // exactly the requirement: pay it
    run(0.79, false);  // short of it: do not
    // A node that cannot measure errs toward paying, because the failure can be
    // caused by the proposer and a lost reward cannot be given back.
    run(-1.0, true);
}

// The node asks the chain who validates, and the answer changes exactly when a
// block that changes the set is accepted.
TEST(TheChainAnswersWhoValidates) {
    vm::PlatformVM chain(kPChain, make_backend(), genesis());
    const std::uint64_t end = kGenesisTime + 90 * 24 * 60 * 60;

    auto before = chain.validator_set(kPrimaryNetworkId);
    REQUIRE_OK(before);
    REQUIRE(before.value().empty());
    auto empty_root = chain.validator_set_root(kPrimaryNetworkId);
    REQUIRE_OK(empty_root);
    REQUIRE_EQ(kEmptyId, empty_root.value());

    chain.submit(join_tx(10'000'000'000, 5'000'000'000, end));
    auto blk = chain.build();
    REQUIRE(blk != nullptr);
    REQUIRE(blk->verify());

    // Verifying changes nothing: the set is what the ACCEPTED state says.
    REQUIRE(chain.validator_set(kPrimaryNetworkId).value().empty());

    blk->accept();
    auto after = chain.validator_set(kPrimaryNetworkId);
    REQUIRE_OK(after);
    REQUIRE_EQ_NUM(1, after.value().size());
    REQUIRE_U64(5'000'000'000u, after.value().at(node_of(0x90)).weight);
    REQUIRE(after.value().at(node_of(0x90)).public_key.has_value());

    auto root = chain.validator_set_root(kPrimaryNetworkId);
    REQUIRE_OK(root);
    REQUIRE(!(root.value() == kEmptyId));
}

// Who validated at a height that has already passed. A message signed then is
// checked now, so the chain has to be able to say — and it says it by taking
// the set it holds and undoing everything since, rather than by keeping every
// past set.
//
// The removal below settles across a proposal and its option block, which are
// two layers landing at DIFFERENT heights but through the same accept path, so
// this also drives the composition the record has to do.
TEST(TheChainAnswersWhoValidatedThen) {
    const std::uint64_t end = kGenesisTime + 90 * 24 * 60 * 60;
    vm::PlatformVM chain(kPChain, make_backend(), genesis());

    // Height 0: nobody.
    REQUIRE(chain.validator_set_at(kPrimaryNetworkId, 0).value().empty());

    chain.submit(join_tx(10'000'000'000, 5'000'000'000, end));
    auto join = chain.build();
    REQUIRE(join != nullptr);
    REQUIRE(join->verify());
    join->accept();

    // Height 1: the validator is in, and the set at height 0 is still nobody.
    const auto at_one = chain.validator_set(kPrimaryNetworkId);
    REQUIRE_OK(at_one);
    REQUIRE_EQ_NUM(1, at_one.value().size());
    REQUIRE(chain.validator_set_at(kPrimaryNetworkId, 0).value().empty());
    REQUIRE(chain.validator_set_at(kPrimaryNetworkId, 1).value() == at_one.value());

    // Settle the stake: proposal, then the commit that pays it.
    chain.set_wall_clock(end);
    auto proposal = chain.build();
    REQUIRE(proposal != nullptr);
    REQUIRE(proposal->verify());
    proposal->accept();

    Id proposal_id{};
    std::memcpy(proposal_id.b.data(), proposal->id().data(), kIdLen);
    auto commit = block::CommitBlock::create(end, proposal_id, proposal->height() + 1);
    REQUIRE_OK(commit);
    const std::vector<std::uint8_t> wire(commit.value()->bytes().begin(), commit.value()->bytes().end());
    auto opt = chain.parse(wire);
    REQUIRE(opt != nullptr);
    REQUIRE(opt->verify());
    opt->accept();

    // Now: nobody. Then: the validator, with the weight and the key it had.
    REQUIRE(chain.validator_set(kPrimaryNetworkId).value().empty());
    const auto then = chain.validator_set_at(kPrimaryNetworkId, 1);
    REQUIRE_OK(then);
    REQUIRE(then.value() == at_one.value());
    REQUIRE_U64(5'000'000'000u, then.value().at(node_of(0x90)).weight);
    REQUIRE(then.value().at(node_of(0x90)).public_key.has_value());

    // And the commitment a vote binds is the one that set had.
    REQUIRE_EQ(validators::set_root(at_one.value()), validators::set_root(then.value()));

    // A height this chain has not reached has no set, and answering one would
    // be inventing it.
    REQUIRE_ERR(chain.validator_set_at(kPrimaryNetworkId, chain.last_accepted_height() + 1),
                Err::InvalidState);
}

// The reward gate reads the uptime rule that was in force when the validator
// BONDED, not the one in force when the reward is settled.
//
// This is the property that separates governance from expropriation. Without
// it, a stake majority could raise the bar the day before a rival's stake
// matures and take its reward — and the vote would look, from the outside, like
// ordinary policy. The validator below bonded under an 80% rule and kept 85%; a
// later vote to 90% must not reach back and take what it earned.
TEST(TheRewardGateJudgesTheTermsThatWereAgreed) {
    const std::uint64_t end = kGenesisTime + 90 * 24 * 60 * 60;

    // A day after the chain starts, and long before the stake matures.
    const std::int64_t voted_at = static_cast<std::int64_t>(kGenesisTime) + 24 * 60 * 60;

    auto run = [&](const char* label, const staking::History& h, bool expect_commit) {
        auto b = make_backend();
        b.policy.uptime_requirement = 800'000;  // what an ungoverned chain uses
        b.staking = h;
        FixedUptime u(0.85);  // above the old rule, below the new one
        b.uptimes = &u;

        vm::PlatformVM chain(kPChain, b, genesis());
        chain.submit(join_tx(10'000'000'000, 5'000'000'000, end));
        auto join = chain.build();
        REQUIRE_MSG(join != nullptr, "the join block was not built");
        REQUIRE_MSG(join->verify(), "the join block did not verify");
        join->accept();
        Id join_id{};
        {
            const auto included = dynamic_cast<vm::VmBlock*>(join.get())->inner().decision_txs();
            REQUIRE_MSG(included.size() == 1, "the join block did not carry its transaction");
            join_id = included[0].tx_id;
        }

        // The moment it actually bound itself, which is later than the genesis
        // clock: the builder advances time to the block it builds.
        // The moment it actually bound itself, which is later than the genesis
        // clock: the builder advances time to the block it builds.
        const auto admitted = chain.accepted().get_current_validator(kPrimaryNetworkId, node_of(0x90));
        REQUIRE_MSG(admitted.has_value(), "the validator did not join");
        REQUIRE_MSG(static_cast<std::int64_t>(admitted.value().start_time) < voted_at,
                    "the vote did not land after the bond");

        // Carry the chain's clock PAST the vote, so that reading the policy at
        // the moment of the check and reading it at the moment of the bond are
        // two different answers. Without this the test would pass either way.
        chain.set_wall_clock(static_cast<std::uint64_t>(voted_at) + 1);
        chain.submit(pay_tx(join_id, 4'999'000'000, 1'000'000));
        auto later = chain.build();
        REQUIRE_MSG(later != nullptr, "the clock-advancing block was not built");
        auto* later_inner = dynamic_cast<vm::VmBlock*>(later.get());
        REQUIRE_MSG(later->verify(), later_inner->refusal());
        later->accept();
        REQUIRE_MSG(static_cast<std::int64_t>(chain.accepted().timestamp()) > voted_at,
                    "the chain clock did not pass the vote");

        chain.set_wall_clock(end);
        auto proposal = chain.build();
        REQUIRE_MSG(proposal != nullptr, "the proposal block was not built");
        auto* inner = dynamic_cast<vm::VmBlock*>(proposal.get());
        REQUIRE_MSG(proposal->verify(), inner->refusal());
        proposal->accept();

        const auto* pb = dynamic_cast<const block::ProposalBlock*>(&inner->inner());
        REQUIRE_MSG(pb != nullptr, "the built block is not a proposal");
        auto opts = chain.options(*pb);
        REQUIRE_MSG(opts.has_value(), "options were not produced");
        auto* preferred = dynamic_cast<vm::VmBlock*>(opts.value().first.get());
        REQUIRE_MSG(preferred->inner().kind() == (expect_commit ? block::Kind::Commit : block::Kind::Abort),
                    std::string("the wrong child was preferred for ") + label);
    };

    staking::Params lenient{2'000 * staking::kLux, 5 * staking::kGigaLux, 60, 365 * 24 * 60 * 60,
                            20'000,                800'000};
    staking::Params strict = lenient;
    strict.uptime_requirement = 900'000;

    // The vote lands AFTER the validator bonded: it is judged at 80% and paid.
    run("a vote after it bonded",
        staking::History{{staking::Entry{0, lenient}, staking::Entry{voted_at, strict}}}, true);

    // The same rule, voted in BEFORE it bonded: it accepted 90% and falls short.
    run("a vote before it bonded", staking::History{{staking::Entry{0, strict}}}, false);

    // And with no history at all the chain behaves exactly as it did before
    // there was one: the compiled-in 80%.
    run("no history at all", staking::History{}, true);
}
