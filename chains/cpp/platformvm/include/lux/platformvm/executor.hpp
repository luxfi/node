// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// executor.hpp — what a transaction DOES to the validator set.
//
// Rendered from Go vms/platformvm/txs/executor (standard_tx_executor.go,
// proposal_tx_executor.go, staker_tx_verification.go, state_changes.go). This is
// the part that makes the P-chain a public good rather than a ledger: a node
// joins the validator set by staking, and there is no allowlist, no admin key
// and no argument anywhere below to pass one.
//
// Three entry points, matching the reference:
//
//   verify_new_chain_time  may a block move the clock to this instant?
//   advance_time_to        what changes when it does
//   standard_tx            a transaction someone submitted
//   proposal_tx            the transaction the chain emits about itself
//
// A proposal is executed against TWO layers at once, the commit layer and the
// abort layer, because a reward is decided by the vote that follows and both
// outcomes must already be computed when it does.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/atomic.hpp"
#include "lux/platformvm/complexity.hpp"
#include "lux/platformvm/fx.hpp"
#include "lux/platformvm/gas.hpp"
#include "lux/platformvm/reward.hpp"
#include "lux/platformvm/state.hpp"
#include "lux/platformvm/uptime.hpp"
#include "lux/platformvm/txs.hpp"

#include <cstdint>
#include <functional>
#include <memory>
#include <set>
#include <vector>

namespace lux::platformvm::executor {

// Go: executor.SyncBound / MaxValidatorWeightFactor.
inline constexpr std::uint64_t kSyncBound = 10;  // seconds
inline constexpr std::uint64_t kMaxValidatorWeightFactor = 5;

// The primary network's staking policy. Rendered from
// vms/platformvm/stakingparams.Params plus the two Config fields that are not
// governed. Durations are seconds, matching the wire.
struct StakingPolicy {
    std::uint64_t min_validator_stake = 0;
    std::uint64_t max_validator_stake = 0;
    std::uint64_t min_delegator_stake = 0;
    std::uint64_t min_stake_duration = 0;
    std::uint64_t max_stake_duration = 0;
    std::uint32_t min_delegation_fee = 0;
    std::uint32_t uptime_requirement = 0;
};

// Go: fee.Calculator. One method, so it stays one method.
class FeeCalculator {
  public:
    virtual ~FeeCalculator() = default;
    virtual Result<std::uint64_t> calculate(const txs::UnsignedTx& tx) const = 0;
};

// A fee that does not depend on the transaction — Go's StaticConfig for a chain
// that charges one price. It is what a caller driving the executor directly
// hands it; a running chain prices by complexity (below) and reads the price off
// its own state, so nobody can choose a cheaper one.
class FlatFee final : public FeeCalculator {
  public:
    explicit FlatFee(std::uint64_t fee = 0) : fee_(fee) {}
    Result<std::uint64_t> calculate(const txs::UnsignedTx&) const override { return fee_; }

  private:
    std::uint64_t fee_;
};

// The LP-103 calculator: a transaction's four-dimensional complexity, merged by
// the chain's weights, times the price the excess implies. This is what makes a
// congested chain expensive and an idle one cheap, per dimension.
class DynamicFee final : public FeeCalculator {
  public:
    DynamicFee(gas::Dimensions weights, gas::Price price) : weights_(weights), price_(price) {}
    Result<std::uint64_t> calculate(const txs::UnsignedTx& tx) const override {
        auto complexity = fee::tx_complexity(tx);
        if (!complexity) return std::unexpected(complexity.error());
        auto g = complexity.value().to_gas(weights_);
        if (!g) return std::unexpected(g.error());
        return gas::cost(g.value(), price_);
    }

  private:
    gas::Dimensions weights_;
    gas::Price price_;
};

// Everything an execution needs that is not the state or the transaction.
struct Backend {
    Runtime runtime;
    StakingPolicy policy;
    reward::Config reward_config;
    gas::Config gas_config;
    // What this execution charges. A running chain replaces it per block with
    // the calculator its own fee state implies; a caller driving the executor
    // directly supplies one.
    const FeeCalculator* fees = nullptr;
    fx::Fx fx{true};

    // How the node measures whether a validator was there. Uptime is a fact
    // about connections rather than about state, so it enters through this one
    // question. Absent means the reward gate cannot be evaluated, and the
    // caller is told so rather than given a default.
    const uptime::Calculator* uptimes = nullptr;

    // The other chains' side of the ledger. An import is the one transaction
    // that spends something this chain has never seen, so without this it is
    // refused rather than believed.
    const atomic::SharedMemory* shared_memory = nullptr;

    // Go: Backend.Bootstrapped. False means this node is still replaying history
    // the network already agreed on; it re-checks everything once caught up.
    bool bootstrapped = true;

    // The clock a transaction's locktimes are judged against. Chain time is what
    // the reference uses for admission, so this defaults to it.
    std::uint64_t now = 0;
};

// Go: executor.VerifyNewChainTime. The clock may only move forward, only as far
// as the next staker change, and only about as far as real time has gone.
Status verify_new_chain_time(std::uint64_t new_chain_time, std::uint64_t now, const state::Chain& current);

// Go: executor.AdvanceTimeTo. Applies everything that follows from moving the
// clock to `new_chain_time`, and answers whether the validator set changed.
Result<bool> advance_time_to(const Backend& backend, state::Chain& parent, std::uint64_t new_chain_time);

// What executing one transaction leaves behind beyond the state it changed:
// which outputs it consumed (so two transactions in one block cannot both spend
// one), and what it asks the shared memory to do once the block is accepted.
struct Effects {
    std::set<Id> inputs;
    std::map<Id, atomic::Requests> atomic_requests;
};

// Go: executor.StandardTx. Executes a submitted transaction against `layer`.
Result<Effects> standard_tx(const Backend& backend, const txs::Tx& tx, state::Diff& layer);

// Go: executor.ProposalTx. Executes the chain's own transaction against both
// outcomes, so the vote that follows only has to pick one.
Status proposal_tx(const Backend& backend, const txs::Tx& tx, state::Diff& on_commit, state::Diff& on_abort);

// Go: state.PickFeeCalculator. The price is read off the chain's own excess, so
// a caller cannot choose a cheaper one.
DynamicFee pick_fee_calculator(const gas::Config& config, const state::Chain& chain);

// Go: executor.GetValidator — current first, then pending.
Result<state::Staker> get_validator(const state::Chain& chain, const Id& chain_id, const NodeId& node_id);

// Go: executor.GetMaxWeight — the largest total weight this validator will carry
// between two instants, its own weight included.
Result<std::uint64_t> get_max_weight(const state::Chain& chain, const state::Staker& validator,
                                     std::uint64_t start, std::uint64_t end);

}  // namespace lux::platformvm::executor
