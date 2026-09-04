// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// executor.cpp — the state transitions.
//
// Rendered from Go vms/platformvm/txs/executor. Every refusal below carries the
// name of the sentinel error it renders, so a test can assert WHY a transaction
// was rejected rather than only that it was.

#include "lux/platformvm/executor.hpp"

#include "lux/platformvm/flow.hpp"
#include "lux/platformvm/safemath.hpp"

#include <algorithm>

namespace lux::platformvm::executor {
namespace {

// The UTXOs a transaction consumes and produces. Go: lux.Consume / lux.Produce.
void consume(state::Chain& s, const std::vector<TransferableInput>& ins) {
    for (const auto& in : ins) s.delete_utxo(in.input_id());
}

void produce(state::Chain& s, const Id& tx_id, const std::vector<TransferableOutput>& outs) {
    for (std::size_t i = 0; i < outs.size(); ++i) {
        UTXO u;
        u.utxo = UtxoId{tx_id, static_cast<std::uint32_t>(i)};
        u.asset = outs[i].asset;
        u.stake_lock = outs[i].stake_lock;
        u.out = outs[i].out;
        s.add_utxo(u);
    }
}

// Read every UTXO an input names, in order. A missing one is a refusal.
Result<std::vector<UTXO>> read_utxos(const state::Chain& s, const std::vector<TransferableInput>& ins) {
    std::vector<UTXO> out;
    out.reserve(ins.size());
    for (const auto& in : ins) {
        auto u = s.get_utxo(in.input_id());
        if (!u) return fail(Err::NotFound, "consumed utxo " + in.input_id().hex() + " is not there");
        out.push_back(u.value());
    }
    return out;
}

// Go: Backend.FlowChecker.VerifySpend. The fee is the "produced" that has no
// output.
Status flow_check(const Backend& backend, const state::Chain& s, const txs::UnsignedTx& unsigned_tx,
                  const std::vector<TransferableInput>& ins, const std::vector<TransferableOutput>& outs,
                  const std::vector<txs::Credential>& creds, std::uint64_t fee) {
    auto utxos = read_utxos(s, ins);
    if (!utxos) return std::unexpected(utxos.error());
    flow::Produced produced;
    if (fee != 0) produced[backend.runtime.utxo_asset_id] = fee;
    auto st = flow::verify_spend_utxos(backend.fx, unsigned_tx.bytes(), utxos.value(), ins, outs, creds,
                                       std::move(produced), backend.now);
    if (!st) return fail(Err::FlowCheckFailed, st.error().message());
    return ok();
}

// Go: verifyAuthorization. The LAST credential authorises the modification; the
// rest authorise the spending.
Result<std::vector<txs::Credential>> verify_authorization(const Backend& backend, const txs::Tx& tx,
                                                          const txs::Owner& owner, const txs::Auth& auth) {
    if (tx.creds.empty()) return fail(Err::WrongNumberOfCredentials);
    const std::size_t base_len = tx.creds.size() - 1;
    auto st = backend.fx.verify_permission(tx.unsigned_tx->bytes(), auth, tx.creds[base_len], owner,
                                           backend.now);
    if (!st) return fail(Err::NotAuthorized, st.error().message());
    return std::vector<txs::Credential>(tx.creds.begin(), tx.creds.begin() + static_cast<long>(base_len));
}

Result<std::vector<txs::Credential>> verify_chain_authorization(const Backend& backend, const state::Chain& s,
                                                                const txs::Tx& tx, const Id& chain_id,
                                                                const txs::Auth& auth) {
    auto owner = s.network_owner(chain_id);
    if (!owner) return fail(Err::ChainNotFound, "network " + chain_id.hex() + " has no owner");
    return verify_authorization(backend, tx, owner.value(), auth);
}

// Go: verifyPoAChainAuthorization. A network that has been transformed or
// promoted is immutable: its own rules govern it now, not the P-chain owner.
Result<std::vector<txs::Credential>> verify_poa_chain_authorization(const Backend& backend,
                                                                    const state::Chain& s, const txs::Tx& tx,
                                                                    const Id& chain_id,
                                                                    const txs::Auth& auth) {
    auto creds = verify_chain_authorization(backend, s, tx, chain_id, auth);
    if (!creds) return creds;
    if (s.network_transformation(chain_id)) return fail(Err::NotAuthorized, chain_id.hex() + " is immutable");
    if (s.network_conversion(chain_id)) return fail(Err::NotAuthorized, chain_id.hex() + " is immutable");
    return creds;
}

// Go: GetRewardsCalculator. A network that has not been transformed uses the
// primary network's schedule.
reward::Calculator rewards_for(const Backend& backend, const state::Chain& s, const Id& chain_id) {
    if (chain_id == kPrimaryNetworkId) return reward::Calculator(backend.reward_config);
    auto t = s.network_transformation(chain_id);
    if (!t) return reward::Calculator(backend.reward_config);
    const auto* tt = dynamic_cast<const txs::TransformChainTx*>(t.value().unsigned_tx.get());
    if (tt == nullptr) return reward::Calculator(backend.reward_config);
    reward::Config c;
    c.max_consumption_rate = tt->max_consumption_rate();
    c.min_consumption_rate = tt->min_consumption_rate();
    c.minting_period = backend.reward_config.minting_period;
    c.supply_cap = tt->maximum_supply();
    return reward::Calculator(c);
}

struct ValidatorRules {
    Id asset_id{};
    std::uint64_t min_validator_stake = 0;
    std::uint64_t max_validator_stake = 0;
    std::uint64_t min_stake_duration = 0;
    std::uint64_t max_stake_duration = 0;
    std::uint32_t min_delegation_fee = 0;
};

Result<ValidatorRules> validator_rules(const Backend& backend, const state::Chain& s, const Id& chain_id) {
    if (chain_id == kPrimaryNetworkId) {
        return ValidatorRules{backend.runtime.utxo_asset_id, backend.policy.min_validator_stake,
                              backend.policy.max_validator_stake,  backend.policy.min_stake_duration,
                              backend.policy.max_stake_duration,   backend.policy.min_delegation_fee};
    }
    auto t = s.network_transformation(chain_id);
    if (!t) return fail(Err::ChainNotFound, "network " + chain_id.hex() + " was never transformed");
    const auto* tt = dynamic_cast<const txs::TransformChainTx*>(t.value().unsigned_tx.get());
    if (tt == nullptr) return fail(Err::IsNotTransformChainTx);
    return ValidatorRules{tt->asset_id(),          tt->min_validator_stake(), tt->max_validator_stake(),
                          tt->min_stake_duration(), tt->max_stake_duration(),  tt->min_delegation_fee()};
}

struct DelegatorRules {
    Id asset_id{};
    std::uint64_t min_delegator_stake = 0;
    std::uint64_t max_validator_stake = 0;
    std::uint64_t min_stake_duration = 0;
    std::uint64_t max_stake_duration = 0;
    std::uint64_t max_validator_weight_factor = 0;
};

Result<DelegatorRules> delegator_rules(const Backend& backend, const state::Chain& s, const Id& chain_id) {
    if (chain_id == kPrimaryNetworkId) {
        // The delegator floor is deliberately NOT governed: it is the rule about
        // who may delegate at all, and delegators are the one constituency that
        // cannot defend itself by voting, because the vote belongs to the
        // validator they delegate to.
        return DelegatorRules{backend.runtime.utxo_asset_id, backend.policy.min_delegator_stake,
                              backend.policy.max_validator_stake, backend.policy.min_stake_duration,
                              backend.policy.max_stake_duration,  kMaxValidatorWeightFactor};
    }
    auto t = s.network_transformation(chain_id);
    if (!t) return fail(Err::ChainNotFound, "network " + chain_id.hex() + " was never transformed");
    const auto* tt = dynamic_cast<const txs::TransformChainTx*>(t.value().unsigned_tx.get());
    if (tt == nullptr) return fail(Err::IsNotTransformChainTx);
    return DelegatorRules{tt->asset_id(),           tt->min_delegator_stake(),
                          tt->max_validator_stake(), tt->min_stake_duration(),
                          tt->max_stake_duration(),  tt->max_validator_weight_factor()};
}

// Go: verifyChainValidatorPrimaryNetworkRequirements. A node may only validate
// a network for a window inside the one it validates the primary network for.
Status verify_primary_network_requirements(const state::Chain& s, const txs::Validator& v) {
    auto primary = get_validator(s, kPrimaryNetworkId, v.node_id);
    if (!primary) return fail(Err::NotValidator, v.node_id.hex() + " is not a primary network validator");
    if (!txs::bounded_by(s.timestamp(), v.end, primary.value().start_time, primary.value().end_time))
        return fail(Err::PeriodMismatch);
    return ok();
}

// Go: standardTxExecutor.putStaker. A staker enters the CURRENT set immediately,
// with the chain time as its start; its potential reward is fixed here, and the
// supply moves by exactly that much.
Status put_staker(const Backend& backend, state::Chain& s, const Id& tx_id, const txs::UnsignedTx& tx) {
    auto view = txs::staker_of(tx);
    if (!view) return std::unexpected(view.error());
    if (!view.value()) return fail(Err::WrongTxType, "transaction admits no staker");
    const txs::StakerView v = *view.value();

    const std::uint64_t chain_time = s.timestamp();
    std::uint64_t potential_reward = 0;
    if (!txs::is_permissioned_validator(v.current_priority)) {
        auto supply = s.current_supply(v.chain_id);
        if (!supply) return std::unexpected(supply.error());
        const auto rewards = rewards_for(backend, s, v.chain_id);
        const std::uint64_t seconds = v.end > chain_time ? v.end - chain_time : 0;
        potential_reward = rewards.calculate(static_cast<reward::Duration>(seconds) * reward::kSecond,
                                             v.weight, supply.value());
        s.set_current_supply(v.chain_id, supply.value() + potential_reward);
    }

    auto staker = state::new_current_staker(tx_id, v, chain_time, potential_reward);
    if (!staker) return std::unexpected(staker.error());
    const auto& st = staker.value();

    if (txs::is_current_validator(st.priority)) return s.put_current_validator(st);
    if (txs::is_current_delegator(st.priority)) {
        s.put_current_delegator(st);
        return ok();
    }
    if (txs::is_pending_validator(st.priority)) return s.put_pending_validator(st);
    if (txs::is_pending_delegator(st.priority)) {
        s.put_pending_delegator(st);
        return ok();
    }
    return fail(Err::InvalidState, "staker has no set to enter");
}

// Go: overDelegated.
Result<bool> over_delegated(const state::Chain& s, const state::Staker& validator, std::uint64_t weight_limit,
                            std::uint64_t delegator_weight, std::uint64_t start, std::uint64_t end) {
    auto max_weight = get_max_weight(s, validator, start, end);
    if (!max_weight) return std::unexpected(max_weight.error());
    auto total = add64(max_weight.value(), delegator_weight);
    if (!total) return true;
    return total.value() > weight_limit;
}

// ── the standard visitor

class Standard final : public txs::Visitor {
  public:
    Standard(const Backend& backend, const txs::Tx& tx, state::Diff& layer)
        : b_(backend), tx_(tx), s_(layer) {}

    Status base_tx(const txs::BaseTxUnsigned& t) override {
        if (auto st = shape(t); !st) return st;
        auto fee = b_.fees->calculate(t);
        if (!fee) return std::unexpected(fee.error());
        if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), tx_.creds, fee.value()); !st) return st;
        move(t);
        return ok();
    }

    Status create_network_tx(const txs::CreateNetworkTx& t) override {
        if (auto st = shape(t); !st) return st;
        auto fee = b_.fees->calculate(t);
        if (!fee) return std::unexpected(fee.error());
        if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), tx_.creds, fee.value()); !st) return st;
        move(t);
        // The network's id IS this transaction's id — the network is the
        // transaction, so nothing has to be assigned or agreed.
        s_.add_network(tx_.tx_id);
        s_.set_network_owner(tx_.tx_id, t.owner());
        return ok();
    }

    Status create_chain_tx(const txs::CreateChainTx& t) override {
        if (auto st = shape(t); !st) return st;
        if (!t.blockchain_name().empty() && s_.chain_name_taken(t.blockchain_name()))
            return fail(Err::DuplicateChain, "chain name " + t.blockchain_name() + " is already taken");
        auto creds = verify_poa_chain_authorization(b_, s_, tx_, t.chain_id(), t.chain_auth());
        if (!creds) return std::unexpected(creds.error());
        auto fee = b_.fees->calculate(t);
        if (!fee) return std::unexpected(fee.error());
        if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), creds.value(), fee.value()); !st)
            return st;
        move(t);
        s_.add_chain(tx_);
        return ok();
    }

    Status transfer_chain_ownership_tx(const txs::TransferChainOwnershipTx& t) override {
        if (auto st = shape(t); !st) return st;
        auto creds = verify_chain_authorization(b_, s_, tx_, t.chain(), t.chain_auth());
        if (!creds) return std::unexpected(creds.error());
        auto fee = b_.fees->calculate(t);
        if (!fee) return std::unexpected(fee.error());
        if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), creds.value(), fee.value()); !st)
            return st;
        s_.set_network_owner(t.chain(), t.owner());
        move(t);
        return ok();
    }

    Status add_chain_validator_tx(const txs::AddChainValidatorTx& t) override {
        if (auto st = shape(t); !st) return st;
        const std::uint64_t chain_time = s_.timestamp();
        const std::uint64_t duration = t.end_time() > chain_time ? t.end_time() - chain_time : 0;
        if (duration < b_.policy.min_stake_duration) return fail(Err::StakeTooShort);
        if (duration > b_.policy.max_stake_duration) return fail(Err::StakeTooLong);

        if (b_.bootstrapped) {
            if (get_validator(s_, t.chain(), t.validator().node_id))
                return fail(Err::DuplicateValidator, t.validator().node_id.hex() + " already validates " +
                                                         t.chain().hex());
            if (auto st = verify_primary_network_requirements(s_, t.validator()); !st) return st;
            auto creds = verify_chain_authorization(b_, s_, tx_, t.chain(), t.chain_auth());
            if (!creds) return std::unexpected(creds.error());
            auto fee = b_.fees->calculate(t);
            if (!fee) return std::unexpected(fee.error());
            if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), creds.value(), fee.value()); !st)
                return st;
        }
        if (auto st = put_staker(b_, s_, tx_.tx_id, t); !st) return st;
        move(t);
        return ok();
    }

    Status remove_chain_validator_tx(const txs::RemoveChainValidatorTx& t) override {
        if (auto st = shape(t); !st) return st;
        bool is_current = true;
        auto vdr = s_.get_current_validator(t.chain(), t.node_id());
        if (!vdr) {
            vdr = s_.get_pending_validator(t.chain(), t.node_id());
            is_current = false;
        }
        if (!vdr) return fail(Err::NotValidator, t.node_id().hex() + " does not validate " + t.chain().hex());
        // Only a PERMISSIONED validator can be removed by its network's owner. A
        // permissionless one leaves by being paid, which is the whole difference.
        if (!txs::is_permissioned_validator(vdr.value().priority))
            return fail(Err::RemovePermissionlessValidator);

        if (b_.bootstrapped) {
            auto creds = verify_chain_authorization(b_, s_, tx_, t.chain(), t.chain_auth());
            if (!creds) return std::unexpected(creds.error());
            auto fee = b_.fees->calculate(t);
            if (!fee) return std::unexpected(fee.error());
            if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), creds.value(), fee.value()); !st)
                return st;
        }
        if (is_current) {
            s_.delete_current_validator(vdr.value());
        } else {
            s_.delete_pending_validator(vdr.value());
        }
        move(t);
        return ok();
    }

    Status add_permissionless_validator_tx(const txs::AddPermissionlessValidatorTx& t) override {
        if (auto st = shape(t); !st) return st;
        if (b_.bootstrapped) {
            const std::uint64_t chain_time = s_.timestamp();
            const std::uint64_t duration = t.end_time() > chain_time ? t.end_time() - chain_time : 0;
            auto rules = validator_rules(b_, s_, t.chain());
            if (!rules) return std::unexpected(rules.error());
            const auto& r = rules.value();
            const auto stake = t.stake_outs();
            if (stake.empty()) return fail(Err::NoStake);
            const Id staked_asset = stake.front().asset_id();

            if (t.validator().weight < r.min_validator_stake) return fail(Err::WeightTooSmall);
            if (t.validator().weight > r.max_validator_stake) return fail(Err::WeightTooLarge);
            if (t.delegation_shares() < r.min_delegation_fee) return fail(Err::InsufficientDelegationFee);
            if (duration < r.min_stake_duration) return fail(Err::StakeTooShort);
            if (duration > r.max_stake_duration) return fail(Err::StakeTooLong);
            if (!(staked_asset == r.asset_id))
                return fail(Err::WrongStakedAssetID, r.asset_id.hex() + " != " + staked_asset.hex());

            if (get_validator(s_, t.chain(), t.validator().node_id))
                return fail(Err::DuplicateValidator,
                            t.validator().node_id.hex() + " already validates " + t.chain().hex());
            if (!(t.chain() == kPrimaryNetworkId))
                if (auto st = verify_primary_network_requirements(s_, t.validator()); !st) return st;

            auto outs = t.outputs();
            outs.insert(outs.end(), stake.begin(), stake.end());
            auto fee = b_.fees->calculate(t);
            if (!fee) return std::unexpected(fee.error());
            if (auto st = flow_check(b_, s_, t, t.inputs(), outs, tx_.creds, fee.value()); !st) return st;
        }
        if (auto st = put_staker(b_, s_, tx_.tx_id, t); !st) return st;
        move(t);
        return ok();
    }

    Status add_permissionless_delegator_tx(const txs::AddPermissionlessDelegatorTx& t) override {
        if (auto st = shape(t); !st) return st;
        if (b_.bootstrapped) {
            const std::uint64_t chain_time = s_.timestamp();
            const std::uint64_t end = t.end_time();
            const std::uint64_t duration = end > chain_time ? end - chain_time : 0;
            auto rules = delegator_rules(b_, s_, t.chain());
            if (!rules) return std::unexpected(rules.error());
            const auto& r = rules.value();
            const auto stake = t.stake_outs();
            if (stake.empty()) return fail(Err::NoStake);
            const Id staked_asset = stake.front().asset_id();

            if (t.validator().weight < r.min_delegator_stake) return fail(Err::WeightTooSmall);
            if (duration < r.min_stake_duration) return fail(Err::StakeTooShort);
            if (duration > r.max_stake_duration) return fail(Err::StakeTooLong);
            if (!(staked_asset == r.asset_id))
                return fail(Err::WrongStakedAssetID, r.asset_id.hex() + " != " + staked_asset.hex());

            auto vdr = get_validator(s_, t.chain(), t.validator().node_id);
            if (!vdr) return fail(Err::NotValidator, t.validator().node_id.hex() + " does not validate " +
                                                         t.chain().hex());
            auto limit = mul64(r.max_validator_weight_factor, vdr.value().weight);
            const std::uint64_t maximum_weight =
                std::min(limit ? limit.value() : UINT64_MAX, r.max_validator_stake);

            if (!txs::bounded_by(chain_time, end, vdr.value().start_time, vdr.value().end_time))
                return fail(Err::PeriodMismatch);
            auto over = over_delegated(s_, vdr.value(), maximum_weight, t.validator().weight, chain_time, end);
            if (!over) return std::unexpected(over.error());
            if (over.value()) return fail(Err::OverDelegated);

            // Only a permissionless validator can be delegated to: a delegator's
            // reward is split with the validator's transaction, and a
            // permissioned one has no share to split.
            if (!(t.chain() == kPrimaryNetworkId) && txs::is_permissioned_validator(vdr.value().priority))
                return fail(Err::DelegateToPermissionedValidator);

            auto outs = t.outputs();
            outs.insert(outs.end(), stake.begin(), stake.end());
            auto fee = b_.fees->calculate(t);
            if (!fee) return std::unexpected(fee.error());
            if (auto st = flow_check(b_, s_, t, t.inputs(), outs, tx_.creds, fee.value()); !st) return st;
        }
        if (auto st = put_staker(b_, s_, tx_.tx_id, t); !st) return st;
        move(t);
        return ok();
    }

    // The legacy scheduled-staker flow has no role under a chain that admits
    // stakers immediately. These are refused permanently rather than quietly.
    Status add_validator_tx(const txs::AddValidatorTx&) override {
        return fail(Err::AddValidatorTxNotPermitted);
    }
    Status add_delegator_tx(const txs::AddDelegatorTx&) override {
        return fail(Err::AddDelegatorTxNotPermitted);
    }
    Status transform_chain_tx(const txs::TransformChainTx&) override {
        return fail(Err::WrongTxType, "TransformChainTx is not permitted");
    }
    Status reward_validator_tx(const txs::RewardValidatorTx&) override { return fail(Err::WrongTxType); }

    // The transactions this port does not execute. They are ABSENT rather than
    // approximated: an import that does not read the other chain's shared memory
    // would be an import that accepts money nobody sent, and an L1 transaction
    // that does not verify its cross-chain message would be a validator set
    // anyone could rewrite. Each returns the reason.
    // An import spends outputs another chain produced. This chain has never
    // seen them, so it asks — and a node that cannot ask refuses.
    Status import_tx(const txs::ImportTx& t) override {
        if (auto st = shape(t); !st) return st;

        const auto imported = t.imported_inputs();
        std::vector<Id> keys;
        keys.reserve(imported.size());
        for (const auto& in : imported) {
            keys.push_back(in.input_id());
            effects_.inputs.insert(in.input_id());
        }

        if (b_.bootstrapped) {
            // The source must be a chain this one shares memory with, and never
            // itself: importing from yourself is spending twice.
            if (t.source_chain() == b_.runtime.chain_id)
                return fail(Err::WrongChainID, "cannot import from this chain");
            if (b_.shared_memory == nullptr)
                return fail(Err::InvalidState, "no shared memory to read the imported outputs from");
            auto peer = b_.shared_memory->get(t.source_chain(), keys);
            if (!peer) return std::unexpected(peer.error());

            auto local = read_utxos(s_, t.inputs());
            if (!local) return std::unexpected(local.error());
            std::vector<UTXO> utxos = local.value();
            utxos.insert(utxos.end(), peer.value().begin(), peer.value().end());

            std::vector<TransferableInput> ins = t.inputs();
            ins.insert(ins.end(), imported.begin(), imported.end());

            auto fee = b_.fees->calculate(t);
            if (!fee) return std::unexpected(fee.error());
            flow::Produced produced;
            if (fee.value() != 0) produced[b_.runtime.utxo_asset_id] = fee.value();
            auto st = flow::verify_spend_utxos(b_.fx, t.bytes(), utxos, ins, t.outputs(), tx_.creds,
                                               std::move(produced), b_.now);
            if (!st) return fail(Err::FlowCheckFailed, st.error().message());
        }

        move(t);
        // The removal is recorded whether or not it was verified, so the shared
        // state is right if this node later starts verifying.
        effects_.atomic_requests[t.source_chain()].remove = std::move(keys);
        return ok();
    }

    // An export produces outputs another chain will hand out. They leave this
    // chain's UTXO set entirely and enter the shared memory instead.
    Status export_tx(const txs::ExportTx& t) override {
        if (auto st = shape(t); !st) return st;
        if (b_.bootstrapped && t.destination_chain() == b_.runtime.chain_id)
            return fail(Err::WrongChainID, "cannot export to this chain");

        const auto own = t.outputs();
        const auto exported = t.exported_outputs();
        std::vector<TransferableOutput> outs = own;
        outs.insert(outs.end(), exported.begin(), exported.end());

        auto fee = b_.fees->calculate(t);
        if (!fee) return std::unexpected(fee.error());
        if (auto st = flow_check(b_, s_, t, t.inputs(), outs, tx_.creds, fee.value()); !st) return st;

        move(t);

        atomic::Requests& req = effects_.atomic_requests[t.destination_chain()];
        for (std::size_t i = 0; i < exported.size(); ++i) {
            UTXO u;
            u.utxo = UtxoId{tx_.tx_id, static_cast<std::uint32_t>(own.size() + i)};
            u.asset = exported[i].asset;
            u.stake_lock = exported[i].stake_lock;
            u.out = exported[i].out;
            req.put.push_back(atomic::Element{u.id(), u, exported[i].out.owners.addrs});
        }
        return ok();
    }

    Status convert_network_tx(const txs::ConvertNetworkTx&) override {
        return fail(Err::WrongTxType, "ConvertNetworkTx is not executed by this port");
    }
    Status register_l1_validator_tx(const txs::RegisterL1ValidatorTx&) override {
        return fail(Err::WrongTxType, "RegisterL1ValidatorTx needs the warp seam, which this port does not have");
    }
    Status set_l1_validator_weight_tx(const txs::SetL1ValidatorWeightTx&) override {
        return fail(Err::WrongTxType, "SetL1ValidatorWeightTx needs the warp seam, which this port does not have");
    }
    Status increase_l1_validator_balance_tx(const txs::IncreaseL1ValidatorBalanceTx&) override {
        return fail(Err::WrongTxType, "IncreaseL1ValidatorBalanceTx is not executed by this port");
    }
    Status disable_l1_validator_tx(const txs::DisableL1ValidatorTx&) override {
        return fail(Err::WrongTxType, "DisableL1ValidatorTx is not executed by this port");
    }

  private:
    // Every arm starts the same way: the transaction must be well formed, and
    // its memo must be empty.
    Status shape(const txs::SpendingTx& t) const {
        if (auto st = tx_.syntactic_verify(b_.runtime); !st) return st;
        return verify_memo_field_length(t.memo());
    }

    void move(const txs::SpendingTx& t) {
        for (const auto& in : t.inputs()) effects_.inputs.insert(in.input_id());
        consume(s_, t.inputs());
        produce(s_, tx_.tx_id, t.outputs());
    }

    const Backend& b_;
    const txs::Tx& tx_;
    state::Diff& s_;

  public:
    Effects effects_;
};

// ── the proposal visitor

class Proposal final : public txs::Visitor {
  public:
    Proposal(const Backend& backend, const txs::Tx& tx, state::Diff& on_commit, state::Diff& on_abort)
        : b_(backend), tx_(tx), commit_(on_commit), abort_(on_abort) {}

    Status reward_validator_tx(const txs::RewardValidatorTx& t) override {
        if (t.tx_id().empty()) return fail(Err::InvalidID);
        // The chain emits this about itself; nobody signs it.
        if (!tx_.creds.empty()) return fail(Err::WrongNumberOfCredentials);

        const auto stakers = commit_.current_stakers();
        if (stakers.empty()) return fail(Err::NotFound, "no staker to reward");
        const state::Staker to_reward = stakers.front();

        // The proposal must name the staker that is actually next, or a
        // proposer could choose whom to pay.
        if (!(to_reward.tx_id == t.tx_id()))
            return fail(Err::RemoveWrongStaker, to_reward.tx_id.hex() + " != " + t.tx_id().hex());
        if (to_reward.end_time != commit_.timestamp())
            return fail(Err::RemoveStakerTooEarly,
                        std::to_string(commit_.timestamp()) + " < " + std::to_string(to_reward.end_time));

        auto staker_tx = commit_.get_tx(to_reward.tx_id);
        if (!staker_tx) return std::unexpected(staker_tx.error());
        const txs::UnsignedTx& u = *staker_tx.value().first.unsigned_tx;

        switch (u.kind()) {
            case txs::Kind::AddValidator:
            case txs::Kind::AddPermissionlessValidator: {
                if (auto st = reward_validator(u, to_reward); !st) return st;
                commit_.delete_current_validator(to_reward);
                abort_.delete_current_validator(to_reward);
                break;
            }
            case txs::Kind::AddDelegator:
            case txs::Kind::AddPermissionlessDelegator: {
                if (auto st = reward_delegator(u, to_reward); !st) return st;
                commit_.delete_current_delegator(to_reward);
                abort_.delete_current_delegator(to_reward);
                break;
            }
            default:
                // A permissioned staker leaves by the clock, so by the time the
                // chain proposes a payment none should be left.
                return fail(Err::ShouldBePermissionlessStaker);
        }

        // If the reward is refused, the supply that was minted for it goes back.
        auto supply = abort_.current_supply(to_reward.chain_id);
        if (!supply) return std::unexpected(supply.error());
        auto reduced = sub64(supply.value(), to_reward.potential_reward);
        if (!reduced) return std::unexpected(reduced.error());
        abort_.set_current_supply(to_reward.chain_id, reduced.value());
        return ok();
    }

    // Every other kind is a standard transaction, and a block that puts one in
    // the proposal slot is malformed.
    Status base_tx(const txs::BaseTxUnsigned&) override { return fail(Err::WrongTxType); }
    Status import_tx(const txs::ImportTx&) override { return fail(Err::WrongTxType); }
    Status export_tx(const txs::ExportTx&) override { return fail(Err::WrongTxType); }
    Status create_network_tx(const txs::CreateNetworkTx&) override { return fail(Err::WrongTxType); }
    Status convert_network_tx(const txs::ConvertNetworkTx&) override { return fail(Err::WrongTxType); }
    Status create_chain_tx(const txs::CreateChainTx&) override { return fail(Err::WrongTxType); }
    Status transfer_chain_ownership_tx(const txs::TransferChainOwnershipTx&) override {
        return fail(Err::WrongTxType);
    }
    Status remove_chain_validator_tx(const txs::RemoveChainValidatorTx&) override {
        return fail(Err::WrongTxType);
    }
    Status transform_chain_tx(const txs::TransformChainTx&) override { return fail(Err::WrongTxType); }
    Status register_l1_validator_tx(const txs::RegisterL1ValidatorTx&) override {
        return fail(Err::WrongTxType);
    }
    Status set_l1_validator_weight_tx(const txs::SetL1ValidatorWeightTx&) override {
        return fail(Err::WrongTxType);
    }
    Status increase_l1_validator_balance_tx(const txs::IncreaseL1ValidatorBalanceTx&) override {
        return fail(Err::WrongTxType);
    }
    Status disable_l1_validator_tx(const txs::DisableL1ValidatorTx&) override { return fail(Err::WrongTxType); }

    // A staker transaction in the proposal slot is refused permanently: only a
    // standard block may admit a staker.
    Status add_validator_tx(const txs::AddValidatorTx&) override {
        return fail(Err::ProposedAddStakerTxNotPermitted);
    }
    Status add_chain_validator_tx(const txs::AddChainValidatorTx&) override {
        return fail(Err::ProposedAddStakerTxNotPermitted);
    }
    Status add_delegator_tx(const txs::AddDelegatorTx&) override {
        return fail(Err::ProposedAddStakerTxNotPermitted);
    }
    Status add_permissionless_validator_tx(const txs::AddPermissionlessValidatorTx&) override {
        return fail(Err::ProposedAddStakerTxNotPermitted);
    }
    Status add_permissionless_delegator_tx(const txs::AddPermissionlessDelegatorTx&) override {
        return fail(Err::ProposedAddStakerTxNotPermitted);
    }

  private:
    // Go: proposalTxExecutor.rewardValidatorTx.
    Status reward_validator(const txs::UnsignedTx& u, const state::Staker& validator) {
        const auto stake = txs::stake_of(u);
        if (stake.empty()) return fail(Err::NoStake);
        const auto outputs = u.outputs();
        const Id stake_asset = stake.front().asset_id();
        const Id tx_id = validator.tx_id;

        // The stake itself comes back whether or not the reward does.
        for (std::size_t i = 0; i < stake.size(); ++i) {
            UTXO utxo;
            utxo.utxo = UtxoId{tx_id, static_cast<std::uint32_t>(outputs.size() + i)};
            utxo.asset = stake[i].asset;
            utxo.stake_lock = stake[i].stake_lock;
            utxo.out = stake[i].out;
            commit_.add_utxo(utxo);
            abort_.add_utxo(utxo);
        }

        std::size_t offset = 0;
        if (validator.potential_reward > 0) {
            auto owner = validation_rewards_owner(u);
            if (!owner) return std::unexpected(owner.error());
            UTXO utxo;
            utxo.utxo = UtxoId{tx_id, static_cast<std::uint32_t>(outputs.size() + stake.size())};
            utxo.asset = stake_asset;
            utxo.out = TransferOutput{validator.potential_reward, owner.value()};
            commit_.add_utxo(utxo);
            commit_.add_reward_utxo(tx_id, utxo);
            ++offset;
        }

        auto accrued = commit_.delegatee_reward(validator.chain_id, validator.node_id);
        if (!accrued) return std::unexpected(accrued.error());
        if (accrued.value() == 0) return ok();

        auto owner = delegation_rewards_owner(u);
        if (!owner) return std::unexpected(owner.error());
        TransferOutput out{accrued.value(), owner.value()};

        UTXO on_commit;
        on_commit.utxo = UtxoId{tx_id, static_cast<std::uint32_t>(outputs.size() + stake.size() + offset)};
        on_commit.asset = stake_asset;
        on_commit.out = out;
        commit_.add_utxo(on_commit);
        commit_.add_reward_utxo(tx_id, on_commit);

        // On the abort path there is no validator reward, so the delegatee
        // reward takes the index the validator reward would have had.
        UTXO on_abort;
        on_abort.utxo = UtxoId{tx_id, static_cast<std::uint32_t>(outputs.size() + stake.size())};
        on_abort.asset = stake_asset;
        on_abort.out = out;
        abort_.add_utxo(on_abort);
        abort_.add_reward_utxo(tx_id, on_abort);
        return ok();
    }

    // Go: proposalTxExecutor.rewardDelegatorTx.
    Status reward_delegator(const txs::UnsignedTx& u, const state::Staker& delegator) {
        const auto stake = txs::stake_of(u);
        if (stake.empty()) return fail(Err::NoStake);
        const auto outputs = u.outputs();
        const Id stake_asset = stake.front().asset_id();
        const Id tx_id = delegator.tx_id;

        for (std::size_t i = 0; i < stake.size(); ++i) {
            UTXO utxo;
            utxo.utxo = UtxoId{tx_id, static_cast<std::uint32_t>(outputs.size() + i)};
            utxo.asset = stake[i].asset;
            utxo.stake_lock = stake[i].stake_lock;
            utxo.out = stake[i].out;
            commit_.add_utxo(utxo);
            abort_.add_utxo(utxo);
        }

        auto validator = commit_.get_current_validator(delegator.chain_id, delegator.node_id);
        if (!validator) return std::unexpected(validator.error());
        auto vdr_tx = commit_.get_tx(validator.value().tx_id);
        if (!vdr_tx) return std::unexpected(vdr_tx.error());
        const txs::UnsignedTx& vu = *vdr_tx.value().first.unsigned_tx;
        auto shares = validator_shares(vu);
        if (!shares) return std::unexpected(shares.error());

        const auto split = reward::split(delegator.potential_reward, shares.value());
        const std::uint64_t delegatee_reward = split.from_shares;
        const std::uint64_t delegator_reward = split.remainder;

        if (delegator_reward > 0) {
            auto owner = delegation_rewards_owner(u);
            if (!owner) return std::unexpected(owner.error());
            UTXO utxo;
            utxo.utxo = UtxoId{tx_id, static_cast<std::uint32_t>(outputs.size() + stake.size())};
            utxo.asset = stake_asset;
            utxo.out = TransferOutput{delegator_reward, owner.value()};
            commit_.add_utxo(utxo);
            commit_.add_reward_utxo(tx_id, utxo);
        }

        if (delegatee_reward == 0) return ok();

        // The validator's cut is not paid now: it is deferred to the moment the
        // validator itself leaves, so it accumulates in the ledger instead.
        auto previous = commit_.delegatee_reward(validator.value().chain_id, validator.value().node_id);
        if (!previous) return std::unexpected(previous.error());
        auto total = add64(previous.value(), delegatee_reward);
        if (!total) return std::unexpected(total.error());
        return commit_.set_delegatee_reward(validator.value().chain_id, validator.value().node_id,
                                            total.value());
    }

    static Result<txs::Owner> validation_rewards_owner(const txs::UnsignedTx& u) {
        switch (u.kind()) {
            case txs::Kind::AddValidator:
                return static_cast<const txs::AddValidatorTx&>(u).rewards_owner();
            case txs::Kind::AddPermissionlessValidator:
                return static_cast<const txs::AddPermissionlessValidatorTx&>(u).validator_rewards_owner();
            default:
                return fail(Err::WrongTxType);
        }
    }

    static Result<txs::Owner> delegation_rewards_owner(const txs::UnsignedTx& u) {
        switch (u.kind()) {
            case txs::Kind::AddValidator:
                return static_cast<const txs::AddValidatorTx&>(u).rewards_owner();
            case txs::Kind::AddPermissionlessValidator:
                return static_cast<const txs::AddPermissionlessValidatorTx&>(u).delegator_rewards_owner();
            case txs::Kind::AddDelegator:
                return static_cast<const txs::AddDelegatorTx&>(u).delegation_rewards_owner();
            case txs::Kind::AddPermissionlessDelegator:
                return static_cast<const txs::AddPermissionlessDelegatorTx&>(u).delegation_rewards_owner();
            default:
                return fail(Err::WrongTxType);
        }
    }

    // A delegator may only point at a validator whose transaction HAS a share to
    // split. A permissioned validator has none, which is why this refuses.
    static Result<std::uint32_t> validator_shares(const txs::UnsignedTx& u) {
        switch (u.kind()) {
            case txs::Kind::AddValidator:
                return static_cast<const txs::AddValidatorTx&>(u).delegation_shares();
            case txs::Kind::AddPermissionlessValidator:
                return static_cast<const txs::AddPermissionlessValidatorTx&>(u).delegation_shares();
            default:
                return fail(Err::WrongTxType);
        }
    }

    const Backend& b_;
    const txs::Tx& tx_;
    state::Diff& commit_;
    state::Diff& abort_;
};

}  // namespace

DynamicFee pick_fee_calculator(const gas::Config& config, const state::Chain& chain) {
    const gas::Price price =
        gas::calculate_price(config.min_price, chain.fee_state().excess, config.excess_conversion_constant);
    return DynamicFee(config.weights, price);
}

Result<state::Staker> get_validator(const state::Chain& chain, const Id& chain_id, const NodeId& node_id) {
    auto current = chain.get_current_validator(chain_id, node_id);
    if (current) return current;
    if (current.error().code != Err::NotFound) return current;
    return chain.get_pending_validator(chain_id, node_id);
}

Result<std::uint64_t> get_max_weight(const state::Chain& chain, const state::Staker& validator,
                                     std::uint64_t start, std::uint64_t end) {
    std::uint64_t current_weight = validator.weight;
    for (const auto& d : chain.current_delegators(validator.chain_id, validator.node_id)) {
        auto sum = add64(current_weight, d.weight);
        if (!sum) return std::unexpected(sum.error());
        current_weight = sum.value();
    }

    state::StakerDiffWalk walk(chain.current_delegators(validator.chain_id, validator.node_id),
                               chain.pending_delegators(validator.chain_id, validator.node_id));
    std::uint64_t current_max = 0;
    while (walk.next()) {
        const state::Staker d = walk.value();
        const bool added = walk.is_added();
        if (d.next_time > end) break;
        if (d.next_time >= start) current_max = std::max(current_max, current_weight);
        if (added) {
            auto sum = add64(current_weight, d.weight);
            if (!sum) return std::unexpected(sum.error());
            current_weight = sum.value();
        } else {
            auto diff = sub64(current_weight, d.weight);
            if (!diff) return std::unexpected(diff.error());
            current_weight = diff.value();
        }
    }
    return std::max(current_max, current_weight);
}

Status verify_new_chain_time(std::uint64_t new_chain_time, std::uint64_t now, const state::Chain& current) {
    const std::uint64_t chain_time = current.timestamp();
    if (new_chain_time < chain_time)
        return fail(Err::ChildBlockEarlierThanParent, std::to_string(new_chain_time) + " < " +
                                                          std::to_string(chain_time));
    if (new_chain_time > now + kSyncBound)
        return fail(Err::ChildBlockBeyondSyncBound,
                    std::to_string(new_chain_time) + " > " + std::to_string(now) + " + " +
                        std::to_string(kSyncBound));
    const std::uint64_t next = state::next_staker_change_time(current, new_chain_time);
    if (new_chain_time > next)
        return fail(Err::ChildBlockAfterStakerChangeTime,
                    std::to_string(new_chain_time) + " > " + std::to_string(next));
    return ok();
}

Result<bool> advance_time_to(const Backend& backend, state::Chain& parent, std::uint64_t new_chain_time) {
    state::Diff changes(&parent);
    bool changed = false;

    // Promote every pending staker whose start time has arrived. The parent's
    // list is walked, not the layer's: a walk over something being changed is a
    // walk over nothing in particular.
    for (const auto& to_remove : parent.pending_stakers()) {
        if (to_remove.start_time > new_chain_time) break;

        state::Staker to_add = to_remove;
        to_add.next_time = to_remove.end_time;
        to_add.priority = txs::pending_to_current(to_remove.priority);

        if (to_remove.priority == txs::Priority::ChainPermissionedValidatorPending) {
            if (auto st = changes.put_current_validator(to_add); !st) return std::unexpected(st.error());
            changes.delete_pending_validator(to_remove);
            changed = true;
            continue;
        }

        auto supply = changes.current_supply(to_remove.chain_id);
        if (!supply) return std::unexpected(supply.error());
        const auto rewards = rewards_for(backend, parent, to_remove.chain_id);
        const std::uint64_t seconds =
            to_remove.end_time > to_remove.start_time ? to_remove.end_time - to_remove.start_time : 0;
        to_add.potential_reward = rewards.calculate(
            static_cast<reward::Duration>(seconds) * reward::kSecond, to_remove.weight, supply.value());
        changes.set_current_supply(to_remove.chain_id, supply.value() + to_add.potential_reward);

        if (txs::is_pending_validator(to_remove.priority)) {
            if (auto st = changes.put_current_validator(to_add); !st) return std::unexpected(st.error());
            changes.delete_pending_validator(to_remove);
        } else if (txs::is_pending_delegator(to_remove.priority)) {
            changes.put_current_delegator(to_add);
            changes.delete_pending_delegator(to_remove);
        } else {
            return fail(Err::InvalidState, "pending staker has an unexpected priority");
        }
        changed = true;
    }

    // Remove every current staker whose end time has arrived — but ONLY the
    // permissioned ones. A permissionless staker leaves by the transaction that
    // pays it; advancing the clock alone must never drop it unpaid.
    for (const auto& to_remove : parent.current_stakers()) {
        if (to_remove.end_time > new_chain_time) break;
        if (to_remove.priority != txs::Priority::ChainPermissionedValidatorCurrent) break;
        changes.delete_current_validator(to_remove);
        changed = true;
    }

    // The chain's own clock also refills its fee capacity and drains its excess,
    // which is what makes an idle chain cheap again.
    const std::uint64_t previous = parent.timestamp();
    const std::uint64_t seconds = new_chain_time > previous ? new_chain_time - previous : 0;
    changes.set_fee_state(changes.fee_state().advance_time(backend.gas_config.max_capacity,
                                                           backend.gas_config.max_per_second,
                                                           backend.gas_config.target_per_second, seconds));

    changes.set_timestamp(new_chain_time);
    if (auto st = changes.apply(parent); !st) return std::unexpected(st.error());
    return changed;
}

Result<Effects> standard_tx(const Backend& backend, const txs::Tx& tx, state::Diff& layer) {
    if (!backend.fees) return fail(Err::InvalidState, "no fee calculator");
    Standard e(backend, tx, layer);
    if (auto st = tx.unsigned_tx->visit(e); !st) return std::unexpected(st.error());
    return std::move(e.effects_);
}

Status proposal_tx(const Backend& backend, const txs::Tx& tx, state::Diff& on_commit, state::Diff& on_abort) {
    Proposal e(backend, tx, on_commit, on_abort);
    return tx.unsigned_tx->visit(e);
}

}  // namespace lux::platformvm::executor
