// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// executor_test.cpp — what a transaction does to the validator set.
//
// Ported from Go vms/platformvm/txs/executor: standard_tx_executor_test.go
// (admission), proposal_tx_executor_test.go (the reward decision) and
// state_changes_test.go (the clock). The signatures are real — the same
// first-party ECDSA the fx recovers — so these run the whole path a block runs.
//
// The case that matters most is the one that says a node with enough stake and
// a valid signature JOINS, with nothing else asked of it. That is the property
// that makes this chain a public good, and it is asserted, not assumed.

#include "harness.hpp"
#include "lux/platformvm/atomic.hpp"
#include "lux/platformvm/executor.hpp"
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
constexpr std::uint64_t kChainTime = 1'000'000;
constexpr std::uint64_t kSupply = 360'000'000'000'000;  // 360M LUX in µLUX

pvmtest::Key& key() {
    static pvmtest::Key k(1);
    return k;
}

// A primary-network validator MUST register a BLS key with a real proof of
// possession; the transaction refuses one without. The proof below is a genuine
// pairing-checked signature, produced by the same blst the chain verifies with.
const signer::ProofOfPossession& pop() {
    static pvmtest::BlsKey k(7);
    return k.pop();
}

Runtime runtime() { return Runtime{kNetworkId, kPChain, kLux}; }

ex::StakingPolicy policy() {
    ex::StakingPolicy p;
    p.min_validator_stake = 2'000'000'000;      // 2000 LUX
    p.max_validator_stake = 3'000'000'000'000;  // 3M LUX
    p.min_delegator_stake = 25'000'000;         // 25 LUX
    p.min_stake_duration = 24 * 60 * 60;
    p.max_stake_duration = 365 * 24 * 60 * 60;
    p.min_delegation_fee = 20'000;
    return p;
}

reward::Config reward_config() {
    reward::Config c;
    c.max_consumption_rate = 120'000;
    c.min_consumption_rate = 100'000;
    c.minting_period = 365 * reward::kDay;
    c.supply_cap = 720'000'000'000'000;
    return c;
}

const ex::FlatFee& fees() {
    static ex::FlatFee f(1'000'000);  // 1 LUX
    return f;
}

ex::Backend backend() {
    ex::Backend b;
    b.runtime = runtime();
    b.policy = policy();
    b.reward_config = reward_config();
    b.fees = &fees();
    b.bootstrapped = true;
    b.now = kChainTime;
    return b;
}

OutputOwners mine() { return OutputOwners{0, 1, {key().address()}}; }

// A funded state: one big UTXO owned by the test key, and a supply to mint from.
state::MemState funded(std::uint64_t amount) {
    state::MemState s;
    s.set_timestamp(kChainTime);
    s.set_current_supply(kPrimaryNetworkId, kSupply);
    UTXO u;
    u.utxo = UtxoId{id_of(0xA0), 0};
    u.asset = kLux;
    u.out = TransferOutput{amount, mine()};
    s.add_utxo(u);
    return s;
}

TransferableInput funding_input(std::uint64_t amount) {
    TransferableInput in;
    in.utxo = UtxoId{id_of(0xA0), 0};
    in.asset = kLux;
    in.in = TransferInput{amount, {0}};
    return in;
}

BaseTx envelope(std::uint64_t funds, std::vector<TransferableOutput> outs) {
    BaseTx b;
    b.network_id = kNetworkId;
    b.blockchain_id = kPChain;
    b.outs = std::move(outs);
    b.ins = {funding_input(funds)};
    return b;
}

TransferableOutput out_to_me(std::uint64_t amt) {
    return TransferableOutput{kLux, 0, TransferOutput{amt, mine()}};
}

// Build a signed transaction from an unsigned one, with one credential per
// input (which is what the flow check requires).
txs::Tx sign(std::shared_ptr<txs::UnsignedTx> u, std::size_t creds = 1) {
    txs::Tx tx;
    tx.unsigned_tx = std::move(u);
    for (std::size_t i = 0; i < creds; ++i) tx.creds.push_back(key().sign(tx.unsigned_tx->bytes()));
    (void)tx.initialize();
    return tx;
}

// The permissionless validator transaction the whole chain exists for.
txs::Tx join(std::uint64_t funds, std::uint64_t stake, std::uint64_t end, std::uint8_t node = 0x90,
             std::uint32_t shares = 20'000, const Id& chain = kPrimaryNetworkId, const Id& asset = kLux) {
    const std::uint64_t change = funds - stake - 1'000'000;
    auto u = txs::AddPermissionlessValidatorTx::create(
        envelope(funds, {out_to_me(change)}), txs::Validator{node_of(node), 0, end, stake}, chain,
        // The primary network requires a registered key; a network of its own
        // does not, and the transaction refuses the wrong one either way.
        chain == kPrimaryNetworkId ? signer::Signer{pop()} : signer::Signer{signer::Empty{}},
        {TransferableOutput{asset, 0, TransferOutput{stake, mine()}}}, mine(), mine(), shares);
    return sign(u.value());
}

txs::Tx delegate(std::uint64_t funds, std::uint64_t stake, std::uint64_t end, std::uint8_t node = 0x90) {
    const std::uint64_t change = funds - stake - 1'000'000;
    auto u = txs::AddPermissionlessDelegatorTx::create(
        envelope(funds, {out_to_me(change)}), txs::Validator{node_of(node), 0, end, stake},
        kPrimaryNetworkId, {TransferableOutput{kLux, 0, TransferOutput{stake, mine()}}}, mine());
    return sign(u.value());
}

}  // namespace

// The heart of it: a node with enough stake and a valid signature joins, and
// nothing else is asked of it. No allowlist, no key, no approval.
TEST(APermissionlessValidatorJoins) {
    auto base = funded(10'000'000'000);
    const auto b = backend();
    state::Diff layer(&base);

    const auto tx = join(10'000'000'000, 5'000'000'000, kChainTime + 90 * 24 * 60 * 60);
    REQUIRE_OK(ex::standard_tx(b, tx, layer));

    auto vdr = layer.get_current_validator(kPrimaryNetworkId, node_of(0x90));
    REQUIRE_OK(vdr);
    REQUIRE_U64(5'000'000'000u, vdr.value().weight);
    REQUIRE_U64(kChainTime, vdr.value().start_time);
    REQUIRE_EQ(tx.tx_id, vdr.value().tx_id);

    // The reward is fixed at admission, and the supply moves by exactly it.
    const auto supply = layer.current_supply(kPrimaryNetworkId);
    REQUIRE_OK(supply);
    REQUIRE_U64(kSupply + vdr.value().potential_reward, supply.value());
    REQUIRE(vdr.value().potential_reward > 0);

    // The funding UTXO is gone and the change is there.
    REQUIRE_ERR(layer.get_utxo(UtxoId{id_of(0xA0), 0}.input_id()), Err::NotFound);
    REQUIRE_OK(layer.get_utxo(UtxoId{tx.tx_id, 0}.input_id()));

    // And it lands in the materialised state when the layer is applied.
    REQUIRE_OK(layer.apply(base));
    REQUIRE_OK(base.get_current_validator(kPrimaryNetworkId, node_of(0x90)));
}

// Go: ErrWeightTooSmall / ErrWeightTooLarge / ErrInsufficientDelegationFee /
// ErrStakeTooShort / ErrStakeTooLong / ErrWrongStakedAssetID.
TEST(ValidatorAdmissionRules) {
    const auto b = backend();

    {  // below the floor
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b, join(10'000'000'000, 1'000'000'000, kChainTime + 90 * 24 * 60 * 60),
                                    layer),
                    Err::WeightTooSmall);
    }
    {  // above the ceiling
        auto base = funded(9'000'000'000'000);
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b,
                                    join(9'000'000'000'000, 4'000'000'000'000,
                                         kChainTime + 90 * 24 * 60 * 60),
                                    layer),
                    Err::WeightTooLarge);
    }
    {  // charging delegators less than the floor
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b,
                                    join(10'000'000'000, 5'000'000'000, kChainTime + 90 * 24 * 60 * 60,
                                         0x90, 19'999),
                                    layer),
                    Err::InsufficientDelegationFee);
    }
    {  // too short
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b, join(10'000'000'000, 5'000'000'000, kChainTime + 60), layer),
                    Err::StakeTooShort);
    }
    {  // too long
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        REQUIRE_ERR(
            ex::standard_tx(b, join(10'000'000'000, 5'000'000'000, kChainTime + 400 * 24 * 60 * 60), layer),
            Err::StakeTooLong);
    }
    {  // staking something that is not the chain's asset
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b,
                                    join(10'000'000'000, 5'000'000'000, kChainTime + 90 * 24 * 60 * 60,
                                         0x90, 20'000, kPrimaryNetworkId, id_of(0x11)),
                                    layer),
                    Err::WrongStakedAssetID);
    }
}

// Go: ErrDuplicateValidator — one node, one entry per network.
TEST(DuplicateValidatorIsRefused) {
    auto base = funded(10'000'000'000);
    const auto b = backend();
    state::Diff layer(&base);

    REQUIRE_OK(ex::standard_tx(b, join(10'000'000'000, 5'000'000'000, kChainTime + 90 * 24 * 60 * 60), layer));
    REQUIRE_OK(layer.apply(base));

    // A second transaction for the same node, funded again.
    UTXO u;
    u.utxo = UtxoId{id_of(0xA0), 0};
    u.asset = kLux;
    u.out = TransferOutput{10'000'000'000, mine()};
    base.add_utxo(u);
    state::Diff second(&base);
    REQUIRE_ERR(
        ex::standard_tx(b, join(10'000'000'000, 5'000'000'000, kChainTime + 90 * 24 * 60 * 60), second),
        Err::DuplicateValidator);
}

// Go: ErrAddValidatorTxNotPermitted / ErrAddDelegatorTxNotPermitted. The legacy
// scheduled flow is refused permanently, not silently ignored.
TEST(LegacyStakerTxsAreRefused) {
    auto base = funded(10'000'000'000);
    const auto b = backend();
    state::Diff layer(&base);

    auto av = txs::AddValidatorTx::create(envelope(10'000'000'000, {out_to_me(4'000'000'000)}),
                                          txs::Validator{node_of(0x91), 0, kChainTime + 100, 5'000'000'000},
                                          {TransferableOutput{kLux, 0, TransferOutput{5'000'000'000, mine()}}},
                                          mine(), 20'000);
    REQUIRE_ERR(ex::standard_tx(b, sign(av.value()), layer), Err::AddValidatorTxNotPermitted);

    auto ad = txs::AddDelegatorTx::create(envelope(10'000'000'000, {out_to_me(4'000'000'000)}),
                                          txs::Validator{node_of(0x91), 0, kChainTime + 100, 5'000'000'000},
                                          {TransferableOutput{kLux, 0, TransferOutput{5'000'000'000, mine()}}},
                                          mine());
    REQUIRE_ERR(ex::standard_tx(b, sign(ad.value()), layer), Err::AddDelegatorTxNotPermitted);
}

// A delegator joins a validator, and the two refusals that guard the validator:
// the window must be inside the validator's, and the total must stay under the
// weight limit.
TEST(DelegationRules) {
    auto base = funded(10'000'000'000);
    const auto b = backend();
    const std::uint64_t vdr_end = kChainTime + 90 * 24 * 60 * 60;

    {
        state::Diff layer(&base);
        REQUIRE_OK(ex::standard_tx(b, join(10'000'000'000, 5'000'000'000, vdr_end), layer));
        REQUIRE_OK(layer.apply(base));
    }

    auto refund = [&] {
        UTXO u;
        u.utxo = UtxoId{id_of(0xA0), 0};
        u.asset = kLux;
        u.out = TransferOutput{10'000'000'000, mine()};
        base.add_utxo(u);
    };

    {  // a delegation inside the validator's window is accepted
        refund();
        state::Diff layer(&base);
        REQUIRE_OK(ex::standard_tx(b, delegate(10'000'000'000, 1'000'000'000, vdr_end), layer));
        const auto ds = layer.current_delegators(kPrimaryNetworkId, node_of(0x90));
        REQUIRE_EQ_NUM(1, ds.size());
        REQUIRE_U64(1'000'000'000u, ds[0].weight);
    }
    {  // ending after the validator does
        refund();
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b, delegate(10'000'000'000, 1'000'000'000, vdr_end + 1), layer),
                    Err::PeriodMismatch);
    }
    {  // below the delegator floor
        refund();
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b, delegate(10'000'000'000, 1'000'000, vdr_end), layer),
                    Err::WeightTooSmall);
    }
    {  // more than five times the validator's own weight
        refund();
        state::Diff layer(&base);
        // The validator stakes 5000 LUX, so the limit is 25000 LUX total; a
        // 21000 LUX delegation takes it over.
        UTXO big;
        big.utxo = UtxoId{id_of(0xA1), 0};
        big.asset = kLux;
        big.out = TransferOutput{30'000'000'000, mine()};
        base.add_utxo(big);
        auto u = txs::AddPermissionlessDelegatorTx::create(
            [&] {
                BaseTx e;
                e.network_id = kNetworkId;
                e.blockchain_id = kPChain;
                e.outs = {out_to_me(30'000'000'000 - 21'000'000'000 - 1'000'000)};
                TransferableInput in;
                in.utxo = UtxoId{id_of(0xA1), 0};
                in.asset = kLux;
                in.in = TransferInput{30'000'000'000, {0}};
                e.ins = {in};
                return e;
            }(),
            txs::Validator{node_of(0x90), 0, vdr_end, 21'000'000'000}, kPrimaryNetworkId,
            {TransferableOutput{kLux, 0, TransferOutput{21'000'000'000, mine()}}}, mine());
        REQUIRE_ERR(ex::standard_tx(b, sign(u.value()), layer), Err::OverDelegated);
    }
    {  // delegating to a node that validates nothing
        refund();
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b, delegate(10'000'000'000, 1'000'000'000, vdr_end, 0x99), layer),
                    Err::NotValidator);
    }
}

// Go: txs.ErrRemovePrimaryNetworkValidator and
// executor.ErrRemovePermissionlessValidator. A validator that joined by staking
// leaves by being paid, never by someone else's transaction — and the primary
// network's set cannot be edited by anyone at all.
TEST(APermissionlessValidatorCannotBeRemoved) {
    auto base = funded(10'000'000'000);
    const auto b = backend();

    // The primary network refuses the transaction on its face: there is no
    // owner who could authorise it, so it never reaches the executor.
    {
        state::Diff layer(&base);
        auto rm = txs::RemoveChainValidatorTx::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}),
                                                       node_of(0x90), kPrimaryNetworkId, txs::Auth{0});
        REQUIRE_ERR(ex::standard_tx(b, sign(rm.value(), 2), layer), Err::RemovePrimaryNetworkValidator);
    }

    // On a network that HAS an owner, the owner may remove a validator it
    // admitted — but not one that staked its way in.
    const Id net = id_of(0x50);
    base.add_network(net);
    base.set_network_owner(net, mine());

    state::Staker staked;
    staked.tx_id = id_of(0x33);
    staked.chain_id = net;
    staked.node_id = node_of(0x90);
    staked.weight = 5'000'000'000;
    staked.end_time = kChainTime + 90 * 24 * 60 * 60;
    staked.next_time = staked.end_time;
    staked.priority = txs::Priority::ChainPermissionlessValidatorCurrent;
    REQUIRE_OK(base.put_current_validator(staked));

    {
        state::Diff layer(&base);
        auto rm = txs::RemoveChainValidatorTx::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}),
                                                       node_of(0x90), net, txs::Auth{0});
        REQUIRE_ERR(ex::standard_tx(b, sign(rm.value(), 2), layer), Err::RemovePermissionlessValidator);
    }

    // A validator the owner admitted IS removable by the owner.
    state::Staker admitted;
    admitted.tx_id = id_of(0x34);
    admitted.chain_id = net;
    admitted.node_id = node_of(0x91);
    admitted.weight = 1;
    admitted.end_time = kChainTime + 90 * 24 * 60 * 60;
    admitted.next_time = admitted.end_time;
    admitted.priority = txs::Priority::ChainPermissionedValidatorCurrent;
    REQUIRE_OK(base.put_current_validator(admitted));

    {
        state::Diff layer(&base);
        auto rm = txs::RemoveChainValidatorTx::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}),
                                                       node_of(0x91), net, txs::Auth{0});
        REQUIRE_OK(ex::standard_tx(b, sign(rm.value(), 2), layer));
        REQUIRE_ERR(layer.get_current_validator(net, node_of(0x91)), Err::NotFound);
    }
}

// Go: executor.VerifyNewChainTime — three refusals and an acceptance.
TEST(VerifyNewChainTime) {
    state::MemState s;
    s.set_timestamp(kChainTime);

    // Backwards.
    REQUIRE_ERR(ex::verify_new_chain_time(kChainTime - 1, kChainTime, s), Err::ChildBlockEarlierThanParent);
    // Too far ahead of real time.
    REQUIRE_ERR(ex::verify_new_chain_time(kChainTime + ex::kSyncBound + 1, kChainTime, s),
                Err::ChildBlockBeyondSyncBound);
    // Within the bound, with no stakers to stop it.
    REQUIRE_OK(ex::verify_new_chain_time(kChainTime + ex::kSyncBound, kChainTime, s));

    // Past a staker change.
    state::Staker st;
    st.tx_id = id_of(7);
    st.chain_id = kPrimaryNetworkId;
    st.node_id = node_of(0x90);
    st.end_time = kChainTime + 5;
    st.next_time = kChainTime + 5;
    st.priority = txs::Priority::PrimaryNetworkValidatorCurrent;
    REQUIRE_OK(s.put_current_validator(st));
    REQUIRE_OK(ex::verify_new_chain_time(kChainTime + 5, kChainTime + 100, s));
    REQUIRE_ERR(ex::verify_new_chain_time(kChainTime + 6, kChainTime + 100, s),
                Err::ChildBlockAfterStakerChangeTime);
}

// Go: executor.AdvanceTimeTo. A permissioned validator leaves by the clock; a
// permissionless one does NOT — it leaves by the transaction that pays it, and
// dropping it here would take its stake without paying for it.
TEST(AdvanceTimeRemovesOnlyPermissionedValidators) {
    state::MemState s;
    s.set_timestamp(kChainTime);
    s.set_current_supply(kPrimaryNetworkId, kSupply);
    const auto b = backend();

    state::Staker permissioned;
    permissioned.tx_id = id_of(1);
    permissioned.chain_id = id_of(0x50);
    permissioned.node_id = node_of(0x90);
    permissioned.weight = 1;
    permissioned.end_time = kChainTime + 10;
    permissioned.next_time = kChainTime + 10;
    permissioned.priority = txs::Priority::ChainPermissionedValidatorCurrent;
    REQUIRE_OK(s.put_current_validator(permissioned));

    state::Staker permissionless;
    permissionless.tx_id = id_of(2);
    permissionless.chain_id = kPrimaryNetworkId;
    permissionless.node_id = node_of(0x91);
    permissionless.weight = 5'000'000'000;
    permissionless.end_time = kChainTime + 10;
    permissionless.next_time = kChainTime + 10;
    permissionless.priority = txs::Priority::PrimaryNetworkValidatorCurrent;
    REQUIRE_OK(s.put_current_validator(permissionless));

    auto changed = ex::advance_time_to(b, s, kChainTime + 10);
    REQUIRE_OK(changed);
    REQUIRE(changed.value());
    REQUIRE_U64(kChainTime + 10, s.timestamp());
    REQUIRE_ERR(s.get_current_validator(id_of(0x50), node_of(0x90)), Err::NotFound);
    REQUIRE_OK(s.get_current_validator(kPrimaryNetworkId, node_of(0x91)));
}

// A pending staker whose start time has arrived moves into the current set, and
// its reward is fixed at that moment.
TEST(AdvanceTimePromotesPendingStakers) {
    state::MemState s;
    s.set_timestamp(kChainTime);
    s.set_current_supply(kPrimaryNetworkId, kSupply);
    const auto b = backend();

    state::Staker pending;
    pending.tx_id = id_of(3);
    pending.chain_id = kPrimaryNetworkId;
    pending.node_id = node_of(0x92);
    pending.weight = 5'000'000'000;
    pending.start_time = kChainTime + 5;
    pending.end_time = kChainTime + 90 * 24 * 60 * 60;
    pending.next_time = kChainTime + 5;
    pending.priority = txs::Priority::PrimaryNetworkValidatorPending;
    REQUIRE_OK(s.put_pending_validator(pending));

    auto changed = ex::advance_time_to(b, s, kChainTime + 5);
    REQUIRE_OK(changed);
    REQUIRE(changed.value());

    auto now_current = s.get_current_validator(kPrimaryNetworkId, node_of(0x92));
    REQUIRE_OK(now_current);
    REQUIRE(now_current.value().priority == txs::Priority::PrimaryNetworkValidatorCurrent);
    REQUIRE_U64(pending.end_time, now_current.value().next_time);
    REQUIRE(now_current.value().potential_reward > 0);
    REQUIRE_ERR(s.get_pending_validator(kPrimaryNetworkId, node_of(0x92)), Err::NotFound);
    REQUIRE_U64(kSupply + now_current.value().potential_reward,
                s.current_supply(kPrimaryNetworkId).value());
}

// The reward decision: both outcomes are computed before the vote, and the
// abort path gives back the supply that was minted for a reward nobody got.
TEST(RewardValidatorTx) {
    auto base = funded(10'000'000'000);
    const auto b = backend();
    const std::uint64_t end = kChainTime + 90 * 24 * 60 * 60;

    txs::Tx staker_tx = join(10'000'000'000, 5'000'000'000, end);
    {
        state::Diff layer(&base);
        REQUIRE_OK(ex::standard_tx(b, staker_tx, layer));
        layer.add_tx(staker_tx, status::Status::Committed);
        REQUIRE_OK(layer.apply(base));
    }
    const auto admitted = base.get_current_validator(kPrimaryNetworkId, node_of(0x90));
    REQUIRE_OK(admitted);
    const std::uint64_t potential = admitted.value().potential_reward;
    const std::uint64_t supply_after_admission = base.current_supply(kPrimaryNetworkId).value();

    // The chain time must be the staker's end time before it can be paid.
    {
        state::Diff commit(&base);
        state::Diff abort(&base);
        txs::Tx reward;
        reward.unsigned_tx = txs::RewardValidatorTx::create(staker_tx.tx_id);
        (void)reward.initialize();
        REQUIRE_ERR(ex::proposal_tx(b, reward, commit, abort), Err::RemoveStakerTooEarly);
    }

    base.set_timestamp(end);

    // Naming the wrong staker is refused: a proposer does not choose whom to pay.
    {
        state::Diff commit(&base);
        state::Diff abort(&base);
        txs::Tx reward;
        reward.unsigned_tx = txs::RewardValidatorTx::create(id_of(0xEE));
        (void)reward.initialize();
        REQUIRE_ERR(ex::proposal_tx(b, reward, commit, abort), Err::RemoveWrongStaker);
    }

    // The real thing.
    state::Diff commit(&base);
    state::Diff abort(&base);
    txs::Tx reward;
    reward.unsigned_tx = txs::RewardValidatorTx::create(staker_tx.tx_id);
    (void)reward.initialize();
    REQUIRE_OK(ex::proposal_tx(b, reward, commit, abort));

    // Both outcomes remove the staker and return the stake.
    REQUIRE_ERR(commit.get_current_validator(kPrimaryNetworkId, node_of(0x90)), Err::NotFound);
    REQUIRE_ERR(abort.get_current_validator(kPrimaryNetworkId, node_of(0x90)), Err::NotFound);

    // On commit the reward exists as a UTXO; on abort it does not, and the
    // supply that was minted for it is given back.
    const auto rewards = commit.reward_utxos(staker_tx.tx_id);
    REQUIRE_EQ_NUM(1, rewards.size());
    REQUIRE_U64(potential, rewards[0].out.amt);
    REQUIRE(commit.reward_utxos(staker_tx.tx_id).size() == 1);
    REQUIRE(abort.reward_utxos(staker_tx.tx_id).empty());

    REQUIRE_U64(supply_after_admission, commit.current_supply(kPrimaryNetworkId).value());
    REQUIRE_U64(supply_after_admission - potential, abort.current_supply(kPrimaryNetworkId).value());

    // The staked amount comes back either way, as the output after the tx's own.
    const auto stake_back = commit.get_utxo(UtxoId{staker_tx.tx_id, 1}.input_id());
    REQUIRE_OK(stake_back);
    REQUIRE_U64(5'000'000'000u, stake_back.value().out.amt);
}

// A signed reward proposal is refused: the chain emits this about itself, and a
// signature on it would mean someone else did.
TEST(RewardValidatorTxTakesNoCredentials) {
    auto base = funded(10'000'000'000);
    const auto b = backend();
    state::Diff commit(&base);
    state::Diff abort(&base);

    txs::Tx reward;
    reward.unsigned_tx = txs::RewardValidatorTx::create(id_of(7));
    reward.creds.push_back(key().sign(reward.unsigned_tx->bytes()));
    (void)reward.initialize();
    REQUIRE_ERR(ex::proposal_tx(b, reward, commit, abort), Err::WrongNumberOfCredentials);
}

// A staker transaction in the proposal slot is refused permanently: only a
// standard block may admit a staker.
TEST(ProposalSlotRefusesStakerTxs) {
    auto base = funded(10'000'000'000);
    const auto b = backend();
    state::Diff commit(&base);
    state::Diff abort(&base);
    const auto tx = join(10'000'000'000, 5'000'000'000, kChainTime + 90 * 24 * 60 * 60);
    REQUIRE_ERR(ex::proposal_tx(b, tx, commit, abort), Err::ProposedAddStakerTxNotPermitted);
}

// The flow check is not decorative: money cannot be conjured, and the fee has to
// come from somewhere.
TEST(TheFlowCheckHolds) {
    auto base = funded(10'000'000'000);
    const auto b = backend();
    state::Diff layer(&base);

    // Producing more than was consumed.
    auto u = txs::BaseTxUnsigned::create(envelope(10'000'000'000, {out_to_me(10'000'000'001)}));
    REQUIRE_ERR(ex::standard_tx(b, sign(u.value()), layer), Err::FlowCheckFailed);

    // Spending everything and leaving nothing for the fee.
    auto v = txs::BaseTxUnsigned::create(envelope(10'000'000'000, {out_to_me(10'000'000'000)}));
    REQUIRE_ERR(ex::standard_tx(b, sign(v.value()), layer), Err::FlowCheckFailed);

    // Leaving exactly the fee.
    auto w = txs::BaseTxUnsigned::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}));
    REQUIRE_OK(ex::standard_tx(b, sign(w.value()), layer));
}

// A transaction signed by someone who does not own the funds is refused.
TEST(AStrangerCannotSpend) {
    auto base = funded(10'000'000'000);
    const auto b = backend();
    state::Diff layer(&base);

    pvmtest::Key other(9);
    REQUIRE(!(other.address() == key().address()));

    auto u = txs::BaseTxUnsigned::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}));
    txs::Tx tx;
    tx.unsigned_tx = u.value();
    tx.creds.push_back(other.sign(tx.unsigned_tx->bytes()));
    (void)tx.initialize();
    REQUIRE_ERR(ex::standard_tx(b, tx, layer), Err::FlowCheckFailed);
}

// Networks: the network's id IS the transaction's id, and its owner is recorded.
TEST(CreateNetworkTx) {
    auto base = funded(10'000'000'000);
    const auto b = backend();
    state::Diff layer(&base);

    const security::Mode sec{true, security::Admission::NoOwnSet, 0, security::Manager::PChain};
    auto u = txs::CreateNetworkTx::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}),
                                           kPrimaryNetworkId, mine(), sec, {}, kEmptyId, {});
    const auto tx = sign(u.value());
    REQUIRE_OK(ex::standard_tx(b, tx, layer));
    REQUIRE(layer.has_network(tx.tx_id));
    auto owner = layer.network_owner(tx.tx_id);
    REQUIRE_OK(owner);
    REQUIRE_EQ(mine(), owner.value());
}

// ── crossing chains
//
// An import is the one transaction that spends something this chain has never
// seen, so it has to ask the other chain. Go: standardTxExecutor.ImportTx /
// ExportTx.
namespace {

// The other chain's side of the ledger, as a test can state it.
struct FakeSharedMemory final : atomic::SharedMemory {
    std::map<Id, std::map<Id, UTXO>> by_chain;

    Result<std::vector<UTXO>> get(const Id& peer, const std::vector<Id>& keys) const override {
        const auto chain = by_chain.find(peer);
        if (chain == by_chain.end()) return fail(Err::NotFound, "unknown peer chain");
        std::vector<UTXO> out;
        for (const auto& k : keys) {
            const auto it = chain->second.find(k);
            // A key the peer chain did not produce is a refusal, never a zero:
            // a zero here would be money out of nowhere.
            if (it == chain->second.end()) return fail(Err::NotFound, "no such output on the peer chain");
            out.push_back(it->second);
        }
        return out;
    }
};

const Id kXChain = id_of(0x60);

}  // namespace

TEST(ImportTx) {
    // The output the other chain produced, and the input that spends it.
    UTXO remote;
    remote.utxo = UtxoId{id_of(0xB0), 0};
    remote.asset = kLux;
    remote.out = TransferOutput{4'000'000'000, mine()};

    FakeSharedMemory sm;
    sm.by_chain[kXChain][remote.id()] = remote;

    auto b = backend();
    b.shared_memory = &sm;

    TransferableInput imported;
    imported.utxo = remote.utxo;
    imported.asset = kLux;
    imported.in = TransferInput{4'000'000'000, {0}};

    auto build = [&](const Id& source) {
        // 10 LUX local + 4 LUX imported in; 13 LUX out; 1 LUX fee.
        auto u = txs::ImportTx::create(envelope(10'000'000'000, {out_to_me(13'000'000'000)}), source,
                                       {imported});
        txs::Tx tx;
        tx.unsigned_tx = u.value();
        // One credential per input, local then imported, in that order.
        tx.creds.push_back(key().sign(tx.unsigned_tx->bytes()));
        tx.creds.push_back(key().sign(tx.unsigned_tx->bytes()));
        (void)tx.initialize();
        return tx;
    };

    {  // the happy path
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        const auto tx = build(kXChain);
        auto effects = ex::standard_tx(b, tx, layer);
        REQUIRE_OK(effects);

        // Both the local and the remote output are spent.
        REQUIRE_ERR(layer.get_utxo(UtxoId{id_of(0xA0), 0}.input_id()), Err::NotFound);
        REQUIRE(effects.value().inputs.count(remote.id()) == 1);

        // And the shared memory is asked to drop what was imported.
        const auto& req = effects.value().atomic_requests.at(kXChain);
        REQUIRE_EQ_NUM(1, req.remove.size());
        REQUIRE_EQ(remote.id(), req.remove[0]);
        REQUIRE(req.put.empty());

        // The change is here.
        auto change = layer.get_utxo(UtxoId{tx.tx_id, 0}.input_id());
        REQUIRE_OK(change);
        REQUIRE_U64(13'000'000'000u, change.value().out.amt);
    }
    {  // importing from a chain the node cannot reach
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b, build(id_of(0x61)), layer), Err::NotFound);
    }
    {  // importing from this very chain is spending twice
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b, build(kPChain), layer), Err::WrongChainID);
    }
    {  // a node with no shared memory refuses rather than believing the tx
        auto without = backend();
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(without, build(kXChain), layer), Err::InvalidState);
    }
    {  // and the money still has to add up
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        auto u = txs::ImportTx::create(envelope(10'000'000'000, {out_to_me(14'000'000'000)}), kXChain,
                                        {imported});
        txs::Tx tx;
        tx.unsigned_tx = u.value();
        tx.creds.push_back(key().sign(tx.unsigned_tx->bytes()));
        tx.creds.push_back(key().sign(tx.unsigned_tx->bytes()));
        (void)tx.initialize();
        REQUIRE_ERR(ex::standard_tx(b, tx, layer), Err::FlowCheckFailed);
    }
}

TEST(ExportTx) {
    auto b = backend();

    auto build = [&](const Id& destination) {
        // 10 LUX in; 5 LUX stays, 4 LUX leaves, 1 LUX fee.
        auto u = txs::ExportTx::create(envelope(10'000'000'000, {out_to_me(5'000'000'000)}), destination,
                                       {out_to_me(4'000'000'000)});
        return sign(u.value());
    };

    {
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        const auto tx = build(kXChain);
        auto effects = ex::standard_tx(b, tx, layer);
        REQUIRE_OK(effects);

        // The exported output is NOT in this chain's set — it left.
        REQUIRE_OK(layer.get_utxo(UtxoId{tx.tx_id, 0}.input_id()));
        REQUIRE_ERR(layer.get_utxo(UtxoId{tx.tx_id, 1}.input_id()), Err::NotFound);

        // It is in the request the other chain will be handed instead, indexed
        // under the addresses that chain will look it up by.
        const auto& req = effects.value().atomic_requests.at(kXChain);
        REQUIRE(req.remove.empty());
        REQUIRE_EQ_NUM(1, req.put.size());
        REQUIRE_U64(4'000'000'000u, req.put[0].utxo.out.amt);
        REQUIRE_EQ(UtxoId({tx.tx_id, 1}).input_id(), req.put[0].key);
        REQUIRE_EQ(std::vector<ShortId>({key().address()}), req.put[0].traits);
    }
    {  // exporting to this very chain is not an export
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        REQUIRE_ERR(ex::standard_tx(b, build(kPChain), layer), Err::WrongChainID);
    }
    {  // and the money still has to add up: the exported output is produced too
        auto base = funded(10'000'000'000);
        state::Diff layer(&base);
        auto u = txs::ExportTx::create(envelope(10'000'000'000, {out_to_me(9'000'000'000)}), kXChain,
                                        {out_to_me(4'000'000'000)});
        REQUIRE_ERR(ex::standard_tx(b, sign(u.value()), layer), Err::FlowCheckFailed);
    }
}
