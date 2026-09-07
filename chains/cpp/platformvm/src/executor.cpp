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
        if (!u) return fail(Err::NotFound, "consumed utxo " + hex(in.input_id()) + " is not there");
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
    if (!owner) return fail(Err::ChainNotFound, "network " + hex(chain_id) + " has no owner");
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
    if (s.network_transformation(chain_id)) return fail(Err::NotAuthorized, hex(chain_id) + " is immutable");
    if (s.network_conversion(chain_id)) return fail(Err::NotAuthorized, hex(chain_id) + " is immutable");
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
        // A joiner accepts the policy in force at the moment it joins, so the
        // instant to resolve is the chain's own clock. With no governed history
        // this is the compiled-in policy, unchanged.
        const auto p = backend.policy_at(static_cast<std::int64_t>(s.timestamp()));
        return ValidatorRules{backend.runtime.utxo_asset_id, p.min_validator_stake,
                              p.max_validator_stake,         p.min_stake_duration,
                              p.max_stake_duration,          p.min_delegation_fee};
    }
    auto t = s.network_transformation(chain_id);
    if (!t) return fail(Err::ChainNotFound, "network " + hex(chain_id) + " was never transformed");
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
        // The delegator floor is the one threshold here that no vote reaches;
        // everything else is read at the moment the delegator commits.
        const auto p = backend.policy_at(static_cast<std::int64_t>(s.timestamp()));
        return DelegatorRules{backend.runtime.utxo_asset_id, backend.policy.min_delegator_stake,
                              p.max_validator_stake,         p.min_stake_duration,
                              p.max_stake_duration,          kMaxValidatorWeightFactor};
    }
    auto t = s.network_transformation(chain_id);
    if (!t) return fail(Err::ChainNotFound, "network " + hex(chain_id) + " was never transformed");
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
    if (!primary) return fail(Err::NotValidator, hex(v.node_id) + " is not a primary network validator");
    if (!txs::bounded_by(s.timestamp(), v.end, primary.value().start_time, primary.value().end_time))
        return fail(Err::PeriodMismatch);
    return ok();
}

// Go: verifyL1Conversion. The message must have come from the chain and the
// address the conversion recorded — otherwise anyone with a chain could speak
// for this L1.
Status verify_l1_conversion(const state::Chain& s, const Id& chain_id, const Id& source_chain,
                            std::span<const std::uint8_t> source_address) {
    auto conv = s.network_conversion(chain_id);
    if (!conv) return fail(Err::CouldNotLoadConversion, hex(chain_id));
    if (!(conv.value().chain_id == source_chain))
        return fail(Err::WrongWarpSourceChain,
                    "expected " + hex(conv.value().chain_id) + ", got " + hex(source_chain));
    if (conv.value().addr.size() != source_address.size() ||
        !std::equal(conv.value().addr.begin(), conv.value().addr.end(), source_address.begin()))
        return fail(Err::WrongWarpSourceAddress);
    return ok();
}

// The three layers a warp-carrying transaction wraps its message in: the signed
// envelope, the addressed call that says who sent it, and the L1's own message.
struct WarpCall {
    warp::Message message;
    warpmsg::AddressedCall call;
};

Result<WarpCall> open_warp_call(std::span<const std::uint8_t> raw) {
    auto message = warp::Message::parse(raw);
    if (!message) return std::unexpected(message.error());
    auto envelope = warpmsg::parse_envelope(message.value().unsigned_message.payload);
    if (!envelope) return std::unexpected(envelope.error());
    const auto* call = std::get_if<warpmsg::AddressedCall>(&envelope.value());
    if (call == nullptr) return fail(Err::WrongPayloadType, "the warp payload is not an addressed call");
    return WarpCall{std::move(message.value()), *call};
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

        // A sovereign network runs its OWN validator set from birth. Same
        // primitive as the promotion below, so a set is established exactly one
        // way whichever transaction establishes it.
        if (t.security_mode().sovereign())
            return register_own_set(tx_.tx_id, t.validators(), t.manager_chain_id(),
                                    t.manager_address());
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
                return fail(Err::DuplicateValidator, hex(t.validator().node_id) + " already validates " +
                                                         hex(t.chain()));
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
        if (!vdr) return fail(Err::NotValidator, hex(t.node_id()) + " does not validate " + hex(t.chain()));
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
                return fail(Err::WrongStakedAssetID, hex(r.asset_id) + " != " + hex(staked_asset));

            if (get_validator(s_, t.chain(), t.validator().node_id))
                return fail(Err::DuplicateValidator,
                            hex(t.validator().node_id) + " already validates " + hex(t.chain()));
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
                return fail(Err::WrongStakedAssetID, hex(r.asset_id) + " != " + hex(staked_asset));

            auto vdr = get_validator(s_, t.chain(), t.validator().node_id);
            if (!vdr) return fail(Err::NotValidator, hex(t.validator().node_id) + " does not validate " +
                                                         hex(t.chain()));
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
    //
    // The empty node id comes FIRST, and the order is the answer, not a
    // formality: Go checks it before it reaches the refusal
    // (standard_tx_executor.go AddValidatorTx — errEmptyNodeID, then
    // verifyAddValidatorTx). Refusing the kind first swallows the emptiness and
    // reports "not permitted" where the reference reports "nodeID cannot be
    // empty" — two implementations giving a differently-classed answer to the
    // same bytes, which is what the differential is for.
    Status add_validator_tx(const txs::AddValidatorTx& t) override {
        if (t.validator().node_id == kEmptyNodeId) return fail(Err::EmptyNodeID);
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

    // The promotion: an existing network establishes its OWN validator set and
    // names the authority that may change it. The network's owner authorises
    // it, and every validator that starts ACTIVE has its balance spent here —
    // otherwise the LUX backing those balances would be minted.
    Status convert_network_tx(const txs::ConvertNetworkTx& t) override {
        if (auto st = shape(t); !st) return st;

        auto creds = verify_poa_chain_authorization(b_, s_, tx_, t.network(), t.auth());
        if (!creds) return std::unexpected(creds.error());

        auto fee = b_.fees->calculate(t);
        if (!fee) return std::unexpected(fee.error());
        std::uint64_t total = fee.value();
        for (const auto& v : t.validators()) {
            if (v.balance == 0) continue;
            auto sum = add64(total, v.balance);
            if (!sum) return std::unexpected(sum.error());
            total = sum.value();
        }

        if (auto st = register_own_set(t.network(), t.validators(), t.manager_chain_id(),
                                       t.manager_address());
            !st)
            return st;

        if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), creds.value(), total); !st) return st;
        move(t);
        return ok();
    }
    // An L1 tells the P-chain to start tracking a validator. The balance the
    // transaction carries is prepaid fee, so it is spent like one.
    Status register_l1_validator_tx(const txs::RegisterL1ValidatorTx& t) override {
        if (auto st = shape(t); !st) return st;
        const std::uint64_t now = s_.timestamp();

        auto fee = b_.fees->calculate(t);
        if (!fee) return std::unexpected(fee.error());
        auto total = add64(fee.value(), t.balance());
        if (!total) return std::unexpected(total.error());
        if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), tx_.creds, total.value()); !st)
            return st;

        auto opened = open_warp_call(t.message());
        if (!opened) return std::unexpected(opened.error());
        auto payload = warpmsg::parse_message(opened.value().call.payload);
        if (!payload) return std::unexpected(payload.error());
        const auto* msg = std::get_if<warpmsg::RegisterL1Validator>(&payload.value());
        if (msg == nullptr) return fail(Err::WrongPayloadType, "not a registration");
        if (auto st = msg->verify(); !st) return st;

        if (auto st = verify_l1_conversion(s_, msg->chain_id,
                                           opened.value().message.unsigned_message.source_chain_id,
                                           opened.value().call.source_address);
            !st)
            return st;

        // The expiry bounds how long the chain has to remember this message in
        // order to refuse a replay of it.
        if (msg->expiry <= now)
            return fail(Err::WarpMessageExpired, "expired at " + std::to_string(msg->expiry) +
                                                     ", now " + std::to_string(now));
        if (msg->expiry - now > kRegisterL1ValidatorExpiryWindow)
            return fail(Err::WarpMessageNotYetAllowed,
                        std::to_string(msg->expiry - now) + " seconds out, limit " +
                            std::to_string(kRegisterL1ValidatorExpiryWindow));

        const Id validation_id = msg->validation_id();
        const l1::ExpiryEntry expiry{msg->expiry, validation_id};
        // The whole replay defence: the chain remembers every registration it
        // has seen until the moment that registration could no longer be issued.
        if (s_.has_expiry(expiry)) return fail(Err::WarpMessageAlreadyIssued, hex(validation_id));

        // The message says which key; the TRANSACTION proves whoever sent it
        // holds that key. Neither alone is enough.
        const signer::ProofOfPossession pop{msg->bls_public_key, t.proof_of_possession()};
        if (auto st = pop.verify(); !st) return st;

        if (msg->node_id.size() != kNodeIdLen) return fail(Err::InvalidNodeIDLength);

        l1::Validator v;
        v.validation_id = validation_id;
        v.chain_id = msg->chain_id;
        v.node_id = node_id_from(msg->node_id);
        auto uncompressed = signer::uncompress_for_set(msg->bls_public_key);
        if (!uncompressed) return std::unexpected(uncompressed.error());
        v.public_key = uncompressed.value();
        v.remaining_balance_owner =
            txs::marshal_owner(txs::Owner{0, msg->remaining_balance_owner.threshold,
                                          msg->remaining_balance_owner.addresses});
        v.deactivation_owner = txs::marshal_owner(
            txs::Owner{0, msg->disable_owner.threshold, msg->disable_owner.addresses});
        v.start_time = now;
        v.weight = msg->weight;
        v.min_nonce = 0;
        v.end_accumulated_fee = 0;  // a zero balance leaves it inactive

        if (t.balance() != 0) {
            if (s_.num_active_l1_validators() >= b_.validator_fee_config.capacity)
                return fail(Err::MaxNumActiveValidators);
            // The balance is stored as the accrued-fee mark it can pay up to, so
            // deactivation is a comparison rather than a per-validator decrement.
            auto mark = add64(t.balance(), s_.accrued_fees());
            if (!mark) return std::unexpected(mark.error());
            v.end_accumulated_fee = mark.value();
        }

        if (auto st = s_.put_l1_validator(v); !st) return st;
        move(t);
        s_.put_expiry(expiry);
        return ok();
    }

    // An L1 tells the P-chain a validator's new weight. Zero removes it.
    Status set_l1_validator_weight_tx(const txs::SetL1ValidatorWeightTx& t) override {
        if (auto st = shape(t); !st) return st;
        auto fee = b_.fees->calculate(t);
        if (!fee) return std::unexpected(fee.error());
        if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), tx_.creds, fee.value()); !st)
            return st;

        auto opened = open_warp_call(t.message());
        if (!opened) return std::unexpected(opened.error());
        auto payload = warpmsg::parse_message(opened.value().call.payload);
        if (!payload) return std::unexpected(payload.error());
        const auto* msg = std::get_if<warpmsg::L1ValidatorWeight>(&payload.value());
        if (msg == nullptr) return fail(Err::WrongPayloadType, "not a weight change");
        // The largest nonce is reserved for the change that removes a validator,
        // so that the increment below can never overflow for one that stays.
        if (msg->nonce == UINT64_MAX && msg->weight != 0) return fail(Err::NonceReservedForRemoval);

        auto found = s_.get_l1_validator(msg->validation_id);
        if (!found) return fail(Err::CouldNotLoadL1Validator, hex(msg->validation_id));
        l1::Validator v = found.value();

        // The nonce is the whole replay defence for weight: an old message
        // cannot be re-sent over a newer one.
        if (msg->nonce < v.min_nonce)
            return fail(Err::StaleNonce, std::to_string(msg->nonce) + " < " + std::to_string(v.min_nonce));

        if (auto st = verify_l1_conversion(s_, v.chain_id,
                                           opened.value().message.unsigned_message.source_chain_id,
                                           opened.value().call.source_address);
            !st)
            return st;

        if (msg->weight == 0) {
            // A chain with no validators is a chain nobody can ever speak for
            // again, so the last one cannot be removed.
            auto weight = s_.weight_of_l1_validators(v.chain_id);
            if (!weight) return std::unexpected(weight.error());
            if (weight.value() == v.weight) return fail(Err::RemovingLastValidator);

            if (v.end_accumulated_fee != 0) {
                if (auto st = refund_remaining_balance(t.outputs().size(), v); !st) return st;
            }
        }

        v.min_nonce = msg->nonce + 1;
        v.weight = msg->weight;
        if (auto st = s_.put_l1_validator(v); !st) return st;
        move(t);
        return ok();
    }

    // Anyone may top up an L1 validator's balance; nobody has to be authorised
    // to pay someone else's fees.
    Status increase_l1_validator_balance_tx(const txs::IncreaseL1ValidatorBalanceTx& t) override {
        if (auto st = shape(t); !st) return st;
        auto fee = b_.fees->calculate(t);
        if (!fee) return std::unexpected(fee.error());
        auto total = add64(fee.value(), t.balance());
        if (!total) return std::unexpected(total.error());
        if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), tx_.creds, total.value()); !st)
            return st;

        auto found = s_.get_l1_validator(t.validation_id());
        if (!found) return fail(Err::CouldNotLoadL1Validator, hex(t.validation_id()));
        l1::Validator v = found.value();

        // A top-up of an inactive validator activates it, and there is only so
        // much room for active ones.
        if (v.end_accumulated_fee == 0) {
            if (s_.num_active_l1_validators() >= b_.validator_fee_config.capacity)
                return fail(Err::MaxNumActiveValidators);
            v.end_accumulated_fee = s_.accrued_fees();
        }
        auto mark = add64(v.end_accumulated_fee, t.balance());
        if (!mark) return std::unexpected(mark.error());
        v.end_accumulated_fee = mark.value();

        if (auto st = s_.put_l1_validator(v); !st) return st;
        move(t);
        return ok();
    }

    // Switching a validator off is the ONE L1 operation the P-chain authorises
    // itself, against the deactivation owner the registration named.
    Status disable_l1_validator_tx(const txs::DisableL1ValidatorTx& t) override {
        if (auto st = shape(t); !st) return st;

        auto found = s_.get_l1_validator(t.validation_id());
        if (!found) return fail(Err::CouldNotLoadL1Validator, hex(t.validation_id()));
        l1::Validator v = found.value();

        auto owner = txs::unmarshal_owner(v.deactivation_owner);
        if (!owner) return fail(Err::InvalidState, "the deactivation owner is malformed");
        auto creds = verify_authorization(b_, tx_, owner.value(), t.disable_auth());
        if (!creds) return std::unexpected(creds.error());

        auto fee = b_.fees->calculate(t);
        if (!fee) return std::unexpected(fee.error());
        if (auto st = flow_check(b_, s_, t, t.inputs(), t.outputs(), creds.value(), fee.value()); !st)
            return st;

        move(t);

        // Already off: nothing to refund and nothing to change.
        if (v.end_accumulated_fee == 0) return ok();
        if (auto st = refund_remaining_balance(t.outputs().size(), v); !st) return st;
        v.end_accumulated_fee = 0;
        return s_.put_l1_validator(v);
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

    // Go: registerOwnSet. Seeds a network's own validator set and records the
    // authority that may change it. The ONE primitive behind both the ∅→Network
    // constructor and the Network→Network promotion.
    Status register_own_set(const Id& network_id, const std::vector<txs::NetworkValidator>& vdrs,
                            const Id& manager_chain_id, std::span<const std::uint8_t> manager_address) {
        const std::uint64_t start_time = s_.timestamp();
        const std::uint64_t accrued = s_.accrued_fees();

        warpmsg::ConversionData data;
        data.chain_id = network_id;
        data.manager_chain_id = manager_chain_id;
        data.manager_address.assign(manager_address.begin(), manager_address.end());
        data.validators.reserve(vdrs.size());

        for (std::size_t i = 0; i < vdrs.size(); ++i) {
            const auto& v = vdrs[i];
            if (v.node_id.size() != kNodeIdLen) return fail(Err::InvalidNodeIDLength);

            // The possession pairing was already run by SyntacticVerify over
            // these same bytes, so this is the decode alone. Still fail-closed:
            // a key that does not parse is refused, because a validator stored
            // keyless carries weight in the quorum denominator with no way for
            // anyone to vote toward it.
            auto uncompressed = signer::uncompress_for_set(v.pop.public_key);
            if (!uncompressed) return std::unexpected(uncompressed.error());

            l1::Validator record;
            // The name is DERIVED — the network's id with the index appended —
            // rather than assigned, so genesis validators need nothing agreed.
            record.validation_id = append_id(network_id, static_cast<std::uint32_t>(i));
            record.chain_id = network_id;
            record.node_id = node_id_from(v.node_id);
            record.public_key = uncompressed.value();
            record.remaining_balance_owner = txs::marshal_owner(
                txs::Owner{0, v.remaining_balance_owner.threshold, v.remaining_balance_owner.addresses});
            record.deactivation_owner = txs::marshal_owner(
                txs::Owner{0, v.deactivation_owner.threshold, v.deactivation_owner.addresses});
            record.start_time = start_time;
            record.weight = v.weight;
            record.min_nonce = 0;
            record.end_accumulated_fee = 0;  // a zero balance leaves it inactive

            if (v.balance != 0) {
                if (s_.num_active_l1_validators() >= b_.validator_fee_config.capacity)
                    return fail(Err::MaxNumActiveValidators);
                auto mark = add64(v.balance, accrued);
                if (!mark) return std::unexpected(mark.error());
                record.end_accumulated_fee = mark.value();
            }
            if (auto st = s_.put_l1_validator(record); !st) return st;

            data.validators.push_back(
                warpmsg::ConversionValidator{v.node_id, v.pop.public_key, v.weight});
        }

        // The conversion id is the hash of the set as it was established, and it
        // is what every later message about this L1 refers to.
        auto conversion_id = data.conversion_id();
        if (!conversion_id) return std::unexpected(conversion_id.error());
        s_.set_network_conversion(network_id,
                                  state::NetToL1Conversion{manager_chain_id,
                                                           {manager_address.begin(), manager_address.end()},
                                                           conversion_id.value()});
        return ok();
    }

    // What an L1 validator prepaid and did not spend, back to the owner the
    // registration named, as the output after the transaction\'s own.
    Status refund_remaining_balance(std::size_t own_outputs, const l1::Validator& v) {
        auto owner = txs::unmarshal_owner(v.remaining_balance_owner);
        if (!owner) return fail(Err::InvalidState, "the remaining-balance owner is malformed");
        const std::uint64_t accrued = s_.accrued_fees();
        // Unreachable if the fee state is sound; kept because the alternative to
        // an impossible refusal here is minting LUX out of a corrupt record.
        if (v.end_accumulated_fee <= accrued)
            return fail(Err::InvalidState, "the validator should already have been disabled");
        UTXO u;
        u.utxo = UtxoId{tx_.tx_id, static_cast<std::uint32_t>(own_outputs)};
        u.asset = b_.runtime.utxo_asset_id;
        u.out = TransferOutput{v.end_accumulated_fee - accrued, owner.value()};
        s_.add_utxo(u);
        return ok();
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
        if (t.tx_id() == kEmptyId) return fail(Err::InvalidID);
        // The chain emits this about itself; nobody signs it.
        if (!tx_.creds.empty()) return fail(Err::WrongNumberOfCredentials);

        const auto stakers = commit_.current_stakers();
        if (stakers.empty()) return fail(Err::NotFound, "no staker to reward");
        const state::Staker to_reward = stakers.front();

        // The proposal must name the staker that is actually next, or a
        // proposer could choose whom to pay.
        if (!(to_reward.tx_id == t.tx_id()))
            return fail(Err::RemoveWrongStaker, hex(to_reward.tx_id) + " != " + hex(t.tx_id()));
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

Status verify_warp_messages(const txs::UnsignedTx& tx, std::uint32_t network_id,
                            const warp::CanonicalValidatorSet& source_set) {
    // Only the two transactions that CARRY a message have one to check. Every
    // other kind answers yes because there is nothing to answer about.
    std::span<const std::uint8_t> raw;
    switch (tx.kind()) {
        case txs::Kind::RegisterL1Validator: {
            static thread_local std::vector<std::uint8_t> buf;
            buf = static_cast<const txs::RegisterL1ValidatorTx&>(tx).message();
            raw = buf;
            break;
        }
        case txs::Kind::SetL1ValidatorWeight: {
            static thread_local std::vector<std::uint8_t> buf;
            buf = static_cast<const txs::SetL1ValidatorWeightTx&>(tx).message();
            raw = buf;
            break;
        }
        default:
            return ok();
    }

    auto message = warp::Message::parse(raw);
    if (!message) return std::unexpected(message.error());
    return warp::verify(message.value().signature, message.value().unsigned_message, network_id, source_set,
                        kWarpQuorumNumerator, kWarpQuorumDenominator);
}

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

    // And it charges every ACTIVE L1 validator for the time that passed. The
    // charge is one number for all of them — the accrued mark — so deactivating
    // the ones that can no longer pay is walking a prefix rather than touching
    // every record.
    {
        const l1::FeeState validator_fee{static_cast<gas::Gas>(changes.num_active_l1_validators()),
                                         changes.l1_validator_excess()};
        const std::uint64_t cost = validator_fee.cost_of(backend.validator_fee_config, seconds);
        auto accrued = add64(changes.accrued_fees(), cost);
        if (!accrued) return fail(Err::Overflow, "the accrued validator fees overflow");

        // Walk the PARENT's list: a walk over something being changed is a walk
        // over nothing in particular.
        for (auto v : parent.active_l1_validators()) {
            if (v.end_accumulated_fee > accrued.value()) break;
            v.end_accumulated_fee = 0;  // out of money: off, but still in the set
            if (auto st = changes.put_l1_validator(v); !st) return std::unexpected(st.error());
            changed = true;
        }

        changes.set_l1_validator_excess(
            validator_fee.advance_time(backend.validator_fee_config.target, seconds).excess);
        changes.set_accrued_fees(accrued.value());
    }

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
