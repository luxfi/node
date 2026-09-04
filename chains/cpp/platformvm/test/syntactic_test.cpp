// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// syntactic_test.cpp — what a transaction must be before anyone looks at state.
//
// Ported case for case from the Go per-type suites:
// add_permissionless_validator_tx_test.go, add_permissionless_delegator_tx_test.go,
// add_validator_test.go, add_delegator_test.go, add_chain_validator_test.go,
// create_blockchain_test.go, transform_chain_tx_test.go,
// transfer_chain_ownership_tx_test.go, remove_chain_validator_tx_test.go and the
// four L1 suites. Each asserts the SAME sentinel the Go original does, because a
// rule that rejects for the wrong reason is a rule that will one day accept for
// the wrong reason.

#include "harness.hpp"
#include "lux/platformvm/txs.hpp"
#include "signing.hpp"

using namespace lux::platformvm;
using namespace lux::platformvm::txs;

namespace {

constexpr std::uint32_t kNetworkId = 96369;

Id id_of(std::uint8_t b) {
    Id v{};
    for (std::size_t i = 0; i < kIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
ShortId short_of(std::uint8_t b) {
    ShortId v{};
    for (std::size_t i = 0; i < kShortIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}
NodeId node_of(std::uint8_t b) {
    NodeId v{};
    for (std::size_t i = 0; i < kNodeIdLen; ++i) v.b[i] = static_cast<std::uint8_t>(b + i);
    return v;
}

const Id kLux = id_of(0x10);
const Id kChain = id_of(0x20);

Runtime rt() { return Runtime{kNetworkId, kChain, kLux}; }

BaseTx valid_base() {
    BaseTx b;
    b.network_id = kNetworkId;
    b.blockchain_id = kChain;
    return b;
}

BaseTx invalid_base() {
    BaseTx b;
    b.network_id = 0;  // the wrong network
    b.blockchain_id = kChain;
    return b;
}

Owner good_owner() { return Owner{0, 1, {short_of(0x30)}}; }
// Threshold above the number of addresses: nobody can ever spend it.
Owner unspendable() { return Owner{0, 1, {}}; }

TransferableOutput out(const Id& asset, std::uint64_t amt) {
    return TransferableOutput{asset, 0, TransferOutput{amt, Owner{0, 0, {}}}};
}

const signer::ProofOfPossession& pop() {
    static pvmtest::BlsKey k(11);
    return k.pop();
}

}  // namespace

// Go: TestAddPermissionlessValidatorTxSyntacticVerify — every case, in order.
TEST(AddPermissionlessValidatorSyntacticVerify) {
    const auto r = rt();
    const Id net = id_of(0x40);

    auto build = [&](const BaseTx& base, const Validator& v, const Id& chain, const signer::Signer& sig,
                     const std::vector<TransferableOutput>& stake, const Owner& val_owner,
                     const Owner& del_owner, std::uint32_t shares) {
        return AddPermissionlessValidatorTx::create(base, v, chain, sig, stake, val_owner, del_owner, shares);
    };

    {  // empty nodeID
        auto tx = build(valid_base(), Validator{}, net, signer::Empty{}, {}, good_owner(), good_owner(), 0);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::EmptyNodeID);
    }
    {  // no provided stake
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 0}, net, signer::Empty{}, {}, good_owner(),
                        good_owner(), 0);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::NoStake);
    }
    {  // too many shares
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 0}, net, signer::Empty{},
                        {out(id_of(2), 1)}, good_owner(), good_owner(), 1'000'001);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::TooManyShares);
    }
    {  // the wrong network
        auto tx = build(invalid_base(), Validator{node_of(1), 0, 0, 0}, net, signer::Empty{},
                        {out(id_of(2), 1)}, good_owner(), good_owner(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {  // a rewards owner nobody can spend
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 1}, net, signer::Empty{},
                        {out(id_of(2), 1)}, unspendable(), unspendable(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::OutputUnspendable);
    }
    {  // the primary network requires a registered key, and this has none
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 1}, kPrimaryNetworkId, signer::Empty{},
                        {out(id_of(2), 1)}, good_owner(), good_owner(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::InvalidSigner);
    }
    {  // and a network of its own must NOT register one
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 2}, net, signer::Signer{pop()},
                        {out(id_of(2), 1), out(id_of(2), 1)}, good_owner(), good_owner(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::InvalidSigner);
    }
    {  // a stake output nobody can spend
        TransferableOutput bad{id_of(2), 0, TransferOutput{1, Owner{0, 1, {}}}};
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 1}, net, signer::Empty{}, {bad},
                        good_owner(), good_owner(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::OutputUnspendable);
    }
    {  // stake that overflows a uint64
        const Id asset = id_of(2);
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 1}, net, signer::Empty{},
                        {out(asset, UINT64_MAX), out(asset, 2)}, good_owner(), good_owner(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::Overflow);
    }
    {  // two different staked assets
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 1}, net, signer::Empty{},
                        {out(id_of(2), 1), out(id_of(3), 1)}, good_owner(), good_owner(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::MultipleStakedAssets);
    }
    {  // stake outputs out of order
        const Id asset = id_of(2);
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 3}, net, signer::Empty{},
                        {out(asset, 2), out(asset, 1)}, good_owner(), good_owner(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::OutputsNotSorted);
    }
    {  // the claimed weight is not the stake
        const Id asset = id_of(2);
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 1}, net, signer::Empty{},
                        {out(asset, 1), out(asset, 1)}, good_owner(), good_owner(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::ValidatorWeightMismatch);
    }
    {  // a valid validator of a network of its own
        const Id asset = id_of(2);
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 2}, net, signer::Empty{},
                        {out(asset, 1), out(asset, 1)}, good_owner(), good_owner(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
    {  // a valid primary-network validator, with a real proof of possession
        const Id asset = id_of(2);
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 2}, kPrimaryNetworkId, signer::Signer{pop()},
                        {out(asset, 1), out(asset, 1)}, good_owner(), good_owner(), 1'000'000);
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
}

// Go: TestAddPermissionlessDelegatorTxSyntacticVerify.
TEST(AddPermissionlessDelegatorSyntacticVerify) {
    const auto r = rt();
    const Id net = id_of(0x40);
    auto build = [&](const BaseTx& base, const Validator& v, const std::vector<TransferableOutput>& stake,
                     const Owner& owner) {
        return AddPermissionlessDelegatorTx::create(base, v, net, stake, owner);
    };

    {
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 0}, {}, good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::NoStake);
    }
    {
        auto tx = build(invalid_base(), Validator{node_of(1), 0, 0, 1}, {out(id_of(2), 1)}, good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 1}, {out(id_of(2), 1)}, unspendable());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::OutputUnspendable);
    }
    {
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 1},
                        {out(id_of(2), 1), out(id_of(3), 1)}, good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::MultipleStakedAssets);
    }
    {
        const Id asset = id_of(2);
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 3}, {out(asset, 2), out(asset, 1)},
                        good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::OutputsNotSorted);
    }
    {
        const Id asset = id_of(2);
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 1}, {out(asset, UINT64_MAX), out(asset, 2)},
                        good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::Overflow);
    }
    {
        const Id asset = id_of(2);
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 1}, {out(asset, 1), out(asset, 1)},
                        good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::DelegatorWeightMismatch);
    }
    {
        const Id asset = id_of(2);
        auto tx = build(valid_base(), Validator{node_of(1), 0, 0, 2}, {out(asset, 1), out(asset, 1)},
                        good_owner());
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
}

// Go: TestAddValidatorTxSyntacticVerify — the legacy primary-network entry
// still has to be well formed, because history has to keep replaying.
TEST(AddValidatorSyntacticVerify) {
    const auto r = rt();
    auto stake = [&](std::uint64_t amt, const Owner& o) {
        return std::vector<TransferableOutput>{TransferableOutput{kLux, 0, TransferOutput{amt, o}}};
    };

    {  // the wrong network
        auto tx = AddValidatorTx::create(invalid_base(), Validator{node_of(1), 0, 0, 1},
                                          stake(1, good_owner()), good_owner(), 0);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {  // a stake output nobody can spend
        auto tx = AddValidatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 1},
                                          stake(1, unspendable()), good_owner(), 0);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::OutputUnspendable);
    }
    {  // a rewards owner nobody can spend
        auto tx = AddValidatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 1},
                                          stake(1, good_owner()), unspendable(), 0);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::OutputUnspendable);
    }
    {  // charging delegators more than everything
        auto tx = AddValidatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 1},
                                          stake(1, good_owner()), good_owner(), 1'000'001);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::TooManyShares);
    }
    {  // staking something that is not the chain's asset
        std::vector<TransferableOutput> other = {
            TransferableOutput{id_of(0x77), 0, TransferOutput{1, good_owner()}}};
        auto tx = AddValidatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 1}, other, good_owner(), 0);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::StakeMustBeLUX);
    }
    {  // valid
        auto tx = AddValidatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 1},
                                          stake(1, good_owner()), good_owner(), 0);
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
}

// Go: TestAddDelegatorTxSyntacticVerify.
TEST(AddDelegatorSyntacticVerify) {
    const auto r = rt();
    auto stake = [&](const Id& asset, std::uint64_t amt) {
        return std::vector<TransferableOutput>{TransferableOutput{asset, 0, TransferOutput{amt, good_owner()}}};
    };

    {
        auto tx = AddDelegatorTx::create(invalid_base(), Validator{node_of(1), 0, 0, 1}, stake(kLux, 1),
                                          good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {  // weight that is not the stake
        auto tx = AddDelegatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 2}, stake(kLux, 1),
                                          good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::DelegatorWeightMismatch);
    }
    {
        auto tx = AddDelegatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 1}, stake(id_of(0x77), 1),
                                          good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::StakeMustBeLUX);
    }
    {
        auto tx = AddDelegatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 1}, stake(kLux, 1),
                                          good_owner());
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
}

// Go: TestAddChainValidatorTxSyntacticVerify — the primary network is not a
// chain anyone registers a permissioned validator on.
TEST(AddChainValidatorSyntacticVerify) {
    const auto r = rt();
    const Id net = id_of(0x40);

    {
        auto tx = AddChainValidatorTx::create(invalid_base(), Validator{node_of(1), 0, 0, 1}, net, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {
        auto tx = AddChainValidatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 1}, kPrimaryNetworkId,
                                               Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::AddPrimaryNetworkValidator);
    }
    {  // no weight at all
        auto tx = AddChainValidatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 0}, net, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WeightTooSmallSyntactic);
    }
    {  // an authorisation whose indices repeat
        auto tx = AddChainValidatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 1}, net, Auth{1, 1});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::InputIndicesNotSortedUnique);
    }
    {
        auto tx = AddChainValidatorTx::create(valid_base(), Validator{node_of(1), 0, 0, 1}, net, Auth{0, 1});
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
}

// Go: TestCreateChainTxSyntacticVerify.
TEST(CreateChainSyntacticVerify) {
    const auto r = rt();
    const Id net = id_of(0x40);
    const Id vm = id_of(0x50);

    {  // no VM to run
        auto tx = CreateChainTx::create(valid_base(), net, "yeet", kEmptyId, {}, {}, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::InvalidVMID);
    }
    {  // the primary network is not a network you create a chain on
        auto tx = CreateChainTx::create(valid_base(), kPrimaryNetworkId, "yeet", vm, {}, {}, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::CantValidatePrimaryNetwork);
    }
    {  // a name longer than the wire allows
        auto tx = CreateChainTx::create(valid_base(), net, std::string(kMaxNameLen + 1, 'a'), vm, {}, {},
                                         Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::NameTooLong);
    }
    {  // a name with a character outside printable ASCII letters, digits, space
        auto tx = CreateChainTx::create(valid_base(), net, "\xe2\x8c\x98", vm, {}, {}, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::IllegalNameCharacter);
    }
    {  // genesis larger than the cap
        const std::vector<std::uint8_t> genesis(kMaxGenesisLen + 1, 0);
        auto tx = CreateChainTx::create(valid_base(), net, "yeet", vm, {}, genesis, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::GenesisTooLong);
    }
    {  // fx ids out of order
        auto tx = CreateChainTx::create(valid_base(), net, "yeet", vm, {id_of(2), id_of(1)}, {}, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::FxIDsNotSortedAndUnique);
    }
    {
        auto tx = CreateChainTx::create(valid_base(), net, "My Chain 7", vm, {id_of(1), id_of(2)},
                                         std::vector<std::uint8_t>{1, 2, 3}, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
}

// Go: TestTransferChainOwnershipTxSyntacticVerify and
// TestRemoveChainValidatorTxSyntacticVerify — the primary network's ownership
// and its validator set are not anyone's to hand over or edit.
TEST(ChainOwnershipAndRemovalSyntacticVerify) {
    const auto r = rt();
    const Id net = id_of(0x40);

    {
        auto tx = TransferChainOwnershipTx::create(invalid_base(), net, Auth{0}, good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {
        auto tx = TransferChainOwnershipTx::create(valid_base(), kPrimaryNetworkId, Auth{0}, good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::TransferPermissionlessChain);
    }
    {
        auto tx = TransferChainOwnershipTx::create(valid_base(), net, Auth{1, 1}, good_owner());
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::InputIndicesNotSortedUnique);
    }
    {
        auto tx = TransferChainOwnershipTx::create(valid_base(), net, Auth{0}, good_owner());
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }

    {
        auto tx = RemoveChainValidatorTx::create(invalid_base(), node_of(1), net, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {
        auto tx = RemoveChainValidatorTx::create(valid_base(), node_of(1), kPrimaryNetworkId, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::RemovePrimaryNetworkValidator);
    }
    {
        auto tx = RemoveChainValidatorTx::create(valid_base(), node_of(1), net, Auth{1, 1});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::InputIndicesNotSortedUnique);
    }
    {
        auto tx = RemoveChainValidatorTx::create(valid_base(), node_of(1), net, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
}

// Go: TestTransformChainTxSyntacticVerify — eighteen refusals, and the reason
// each exists is that a network that transforms itself into nonsense would then
// have to be believed.
TEST(TransformChainSyntacticVerify) {
    const auto r = rt();
    const Id net = id_of(0x40);

    auto valid = [&] {
        TransformChainTx::Params p;
        p.chain = net;
        p.asset_id = id_of(0x77);
        p.initial_supply = 10;
        p.maximum_supply = 10;
        p.min_consumption_rate = 0;
        p.max_consumption_rate = 1'000'000;
        p.min_validator_stake = 1;
        p.max_validator_stake = 10;
        p.min_stake_duration = 1;
        p.max_stake_duration = 1;
        p.min_delegation_fee = 1'000'000;
        p.min_delegator_stake = 1;
        p.max_validator_weight_factor = 1;
        p.uptime_requirement = 1'000'000;
        return p;
    };
    auto check = [&](TransformChainTx::Params p, Err want) {
        auto tx = TransformChainTx::create(valid_base(), p, Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), want);
    };

    {
        auto p = valid();
        p.chain = kPrimaryNetworkId;
        check(p, Err::CantTransformPrimaryNetwork);
    }
    {
        auto p = valid();
        p.asset_id = kEmptyId;
        check(p, Err::TransformEmptyAssetID);
    }
    {
        auto p = valid();
        p.asset_id = kLux;
        check(p, Err::AssetIDCantBeLUX);
    }
    {
        auto p = valid();
        p.initial_supply = 0;
        check(p, Err::InitialSupplyZero);
    }
    {
        auto p = valid();
        p.initial_supply = 11;
        check(p, Err::InitialSupplyGreaterThanMaxSupply);
    }
    {
        auto p = valid();
        p.min_consumption_rate = 1'000'001;
        p.max_consumption_rate = 1'000'001;
        check(p, Err::MaxConsumptionRateTooLarge);
    }
    {
        auto p = valid();
        p.min_consumption_rate = 1'000'000;
        p.max_consumption_rate = 999'999;
        check(p, Err::MinConsumptionRateTooLarge);
    }
    {
        auto p = valid();
        p.min_validator_stake = 0;
        check(p, Err::MinValidatorStakeZero);
    }
    {
        auto p = valid();
        p.min_validator_stake = 11;
        check(p, Err::MinValidatorStakeAboveSupply);
    }
    {
        auto p = valid();
        p.min_validator_stake = 10;
        p.max_validator_stake = 9;
        check(p, Err::MinValidatorStakeAboveMax);
    }
    {
        auto p = valid();
        p.max_validator_stake = 11;
        check(p, Err::MaxValidatorStakeTooLarge);
    }
    {
        auto p = valid();
        p.min_stake_duration = 0;
        check(p, Err::MinStakeDurationZero);
    }
    {
        auto p = valid();
        p.min_stake_duration = 2;
        check(p, Err::MinStakeDurationTooLarge);
    }
    {
        auto p = valid();
        p.min_delegation_fee = 1'000'001;
        check(p, Err::MinDelegationFeeTooLarge);
    }
    {
        auto p = valid();
        p.min_delegator_stake = 0;
        check(p, Err::MinDelegatorStakeZero);
    }
    {
        auto p = valid();
        p.max_validator_weight_factor = 0;
        check(p, Err::MaxValidatorWeightFactorZero);
    }
    {
        auto p = valid();
        p.uptime_requirement = 1'000'001;
        check(p, Err::UptimeRequirementTooLarge);
    }
    {  // an authorisation whose indices repeat
        auto tx = TransformChainTx::create(valid_base(), valid(), Auth{1, 1});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::InputIndicesNotSortedUnique);
    }
    {
        auto tx = TransformChainTx::create(invalid_base(), valid(), Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {
        auto tx = TransformChainTx::create(valid_base(), valid(), Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
}

// Go: the four L1 suites.
TEST(L1TxsSyntacticVerify) {
    const auto r = rt();
    signer::SignatureBytes proof{};
    for (std::size_t i = 0; i < proof.size(); ++i) proof[i] = static_cast<std::uint8_t>(i);

    {
        auto tx = RegisterL1ValidatorTx::create(invalid_base(), 1, proof, std::vector<std::uint8_t>{1});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {
        auto tx = RegisterL1ValidatorTx::create(valid_base(), 1, proof, std::vector<std::uint8_t>{1});
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
    {
        auto tx = SetL1ValidatorWeightTx::create(invalid_base(), std::vector<std::uint8_t>{1});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {
        auto tx = SetL1ValidatorWeightTx::create(valid_base(), std::vector<std::uint8_t>{1});
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
    {  // topping up by nothing is not a top-up
        auto tx = IncreaseL1ValidatorBalanceTx::create(valid_base(), id_of(1), 0);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::ZeroBalance);
    }
    {
        auto tx = IncreaseL1ValidatorBalanceTx::create(invalid_base(), id_of(1), 5);
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {
        auto tx = IncreaseL1ValidatorBalanceTx::create(valid_base(), id_of(1), 5);
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
    {
        auto tx = DisableL1ValidatorTx::create(invalid_base(), id_of(1), Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::WrongNetworkID);
    }
    {
        auto tx = DisableL1ValidatorTx::create(valid_base(), id_of(1), Auth{1, 1});
        REQUIRE_OK(tx);
        REQUIRE_ERR(tx.value()->syntactic_verify(r), Err::InputIndicesNotSortedUnique);
    }
    {
        auto tx = DisableL1ValidatorTx::create(valid_base(), id_of(1), Auth{0});
        REQUIRE_OK(tx);
        REQUIRE_OK(tx.value()->syntactic_verify(r));
    }
}

// Go: TestPriorities — the groups, and the order between them. These numbers
// are the wire and the tie-break, so they are asserted by value.
TEST(Priorities) {
    REQUIRE_EQ_NUM(1, static_cast<int>(Priority::PrimaryNetworkDelegatorLegacyPending));
    REQUIRE_EQ_NUM(2, static_cast<int>(Priority::PrimaryNetworkValidatorPending));
    REQUIRE_EQ_NUM(3, static_cast<int>(Priority::PrimaryNetworkDelegatorPermissionlessPending));
    REQUIRE_EQ_NUM(4, static_cast<int>(Priority::ChainPermissionlessValidatorPending));
    REQUIRE_EQ_NUM(5, static_cast<int>(Priority::ChainPermissionlessDelegatorPending));
    REQUIRE_EQ_NUM(6, static_cast<int>(Priority::ChainPermissionedValidatorPending));
    REQUIRE_EQ_NUM(7, static_cast<int>(Priority::ChainPermissionedValidatorCurrent));
    REQUIRE_EQ_NUM(8, static_cast<int>(Priority::ChainPermissionlessDelegatorCurrent));
    REQUIRE_EQ_NUM(9, static_cast<int>(Priority::ChainPermissionlessValidatorCurrent));
    REQUIRE_EQ_NUM(10, static_cast<int>(Priority::PrimaryNetworkDelegatorCurrent));
    REQUIRE_EQ_NUM(11, static_cast<int>(Priority::PrimaryNetworkValidatorCurrent));

    // A permissioned validator is removed from the current set BEFORE any
    // permissionless staker, because it leaves by the clock and they leave by
    // being paid. That is what the lowest current priority buys.
    REQUIRE(static_cast<int>(Priority::ChainPermissionedValidatorCurrent) <
            static_cast<int>(Priority::ChainPermissionlessDelegatorCurrent));

    REQUIRE(is_current_validator(Priority::PrimaryNetworkValidatorCurrent));
    REQUIRE(is_current_validator(Priority::ChainPermissionedValidatorCurrent));
    REQUIRE(is_current_validator(Priority::ChainPermissionlessValidatorCurrent));
    REQUIRE(!is_current_validator(Priority::PrimaryNetworkDelegatorCurrent));

    REQUIRE(is_current_delegator(Priority::PrimaryNetworkDelegatorCurrent));
    REQUIRE(is_current_delegator(Priority::ChainPermissionlessDelegatorCurrent));

    REQUIRE(is_pending_validator(Priority::PrimaryNetworkValidatorPending));
    REQUIRE(is_pending_delegator(Priority::PrimaryNetworkDelegatorLegacyPending));

    REQUIRE(is_permissioned_validator(Priority::ChainPermissionedValidatorCurrent));
    REQUIRE(is_permissioned_validator(Priority::ChainPermissionedValidatorPending));
    REQUIRE(!is_permissioned_validator(Priority::ChainPermissionlessValidatorCurrent));

    // Every pending priority has exactly one current image, and it is in the
    // matching group.
    REQUIRE(pending_to_current(Priority::PrimaryNetworkDelegatorLegacyPending) ==
            Priority::PrimaryNetworkDelegatorCurrent);
    REQUIRE(pending_to_current(Priority::PrimaryNetworkDelegatorPermissionlessPending) ==
            Priority::PrimaryNetworkDelegatorCurrent);
    REQUIRE(pending_to_current(Priority::PrimaryNetworkValidatorPending) ==
            Priority::PrimaryNetworkValidatorCurrent);
    REQUIRE(pending_to_current(Priority::ChainPermissionlessValidatorPending) ==
            Priority::ChainPermissionlessValidatorCurrent);
    REQUIRE(pending_to_current(Priority::ChainPermissionlessDelegatorPending) ==
            Priority::ChainPermissionlessDelegatorCurrent);
    REQUIRE(pending_to_current(Priority::ChainPermissionedValidatorPending) ==
            Priority::ChainPermissionedValidatorCurrent);
}

// Go: TestBoundedBy.
TEST(BoundedBy) {
    REQUIRE(bounded_by(1, 2, 1, 2));
    REQUIRE(bounded_by(1, 2, 0, 3));
    REQUIRE(!bounded_by(0, 2, 1, 3));  // starts too early
    REQUIRE(!bounded_by(1, 4, 1, 3));  // ends too late
    REQUIRE(!bounded_by(3, 2, 1, 4));  // inverted
}
