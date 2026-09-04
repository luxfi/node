// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// l1.hpp — a validator of a sovereign network, and the fee it pays to stay one.
//
// Rendered from Go vms/platformvm/state/l1_validator.go, state/expiry.go and
// vms/platformvm/validators/fee (the LP-77 continuous fee).
//
// An L1 validator is not a staker. A staker bonds a stake and is paid for the
// time it stood; an L1 validator PAYS, continuously, for the P-chain's trouble
// in tracking it — so its lifetime is a balance rather than an end time, and it
// leaves when that balance runs out rather than when a clock strikes.
//
// THE ORDER IS BY WHEN THE MONEY RUNS OUT. Validators are walked in increasing
// EndAccumulatedFee, so advancing the clock deactivates exactly the prefix that
// can no longer pay, and stops at the first one that can. That is why the
// balance is stored as an ABSOLUTE accumulated-fee mark rather than as a
// remaining amount: a remaining amount would have to be decremented for every
// validator on every tick.
//
// A weight of zero means REMOVED, and an EndAccumulatedFee of zero means
// INACTIVE. Those are different: an inactive validator is still in the set and
// still weighs on it, it simply cannot vote and is not charged.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/gas.hpp"
#include "lux/platformvm/ids.hpp"

#include <compare>
#include <cstdint>
#include <vector>

namespace lux::platformvm::l1 {

// Go: state.L1Validator. An LP-77 validator.
struct Validator {
    // The name of the registration message that created it. Not stored beside
    // the record in the reference because it is the key; kept here because a
    // value that cannot say what it is called is hard to pass around.
    Id validation_id{};

    Id chain_id{};
    NodeId node_id{};
    // The UNCOMPRESSED BLS key, always populated.
    std::vector<std::uint8_t> public_key;
    // The canonical owner encodings: who gets the remaining balance back, and
    // who may switch this validator off.
    std::vector<std::uint8_t> remaining_balance_owner;
    std::vector<std::uint8_t> deactivation_owner;

    std::uint64_t start_time = 0;
    // Zero means REMOVED.
    std::uint64_t weight = 0;
    // The smallest nonce that may change the weight next. MaxUint64 is only
    // valid for the change that removes the validator.
    std::uint64_t min_nonce = 0;
    // The accumulated-fee mark this validator can pay up to. Zero means
    // INACTIVE — in the set, weighing on it, not charged and not voting.
    std::uint64_t end_accumulated_fee = 0;

    friend bool operator==(const Validator&, const Validator&) = default;

    bool is_deleted() const { return weight == 0; }
    bool is_active() const { return weight != 0 && end_accumulated_fee != 0; }

    // What the validator set sees. An inactive validator has no node and no key
    // there: it holds weight but cannot be sampled, so surfacing its node would
    // let a quorum wait for a vote that can never come.
    NodeId effective_node_id() const { return is_active() ? node_id : NodeId{}; }
    std::vector<std::uint8_t> effective_public_key() const {
        return is_active() ? public_key : std::vector<std::uint8_t>{};
    }

    // Go: L1Validator.Compare. By when the money runs out, then by name.
    int compare(const Validator& o) const {
        if (end_accumulated_fee != o.end_accumulated_fee)
            return end_accumulated_fee < o.end_accumulated_fee ? -1 : 1;
        if (validation_id == o.validation_id) return 0;
        return validation_id < o.validation_id ? -1 : 1;
    }
    bool less(const Validator& o) const { return compare(o) < 0; }

    // Everything except weight, nonce and balance is fixed for the life of a
    // validation id. A write that changes any of it is not an update to this
    // validator; it is a different validator wearing its name.
    bool immutable_fields_unmodified(const Validator& o) const {
        if (!(validation_id == o.validation_id)) return true;
        return chain_id == o.chain_id && node_id == o.node_id && public_key == o.public_key &&
               remaining_balance_owner == o.remaining_balance_owner &&
               deactivation_owner == o.deactivation_owner && start_time == o.start_time;
    }
};

struct ValidatorLess {
    bool operator()(const Validator& a, const Validator& b) const { return a.less(b); }
};

// Go: state.ExpiryEntry. A registration message that may still be issued, and
// the moment after which it may not. Ordered by that moment, then by name, so
// advancing the clock drops exactly the prefix that has passed.
struct ExpiryEntry {
    std::uint64_t timestamp = 0;
    Id validation_id{};

    friend bool operator==(const ExpiryEntry&, const ExpiryEntry&) = default;

    int compare(const ExpiryEntry& o) const {
        if (timestamp != o.timestamp) return timestamp < o.timestamp ? -1 : 1;
        if (validation_id == o.validation_id) return 0;
        return validation_id < o.validation_id ? -1 : 1;
    }
    bool less(const ExpiryEntry& o) const { return compare(o) < 0; }

    // Go: ExpiryEntry.Marshal — big-endian timestamp then the id, so the byte
    // order and the value order are the same order.
    std::vector<std::uint8_t> marshal() const;
    static Result<ExpiryEntry> unmarshal(std::span<const std::uint8_t> b);
};

struct ExpiryLess {
    bool operator()(const ExpiryEntry& a, const ExpiryEntry& b) const { return a.less(b); }
};

// ── the continuous fee (Go: vms/platformvm/validators/fee)
//
// The price of being an L1 validator rises when there are more of them than the
// chain targets and falls when there are fewer, by the same fake-exponential
// curve the transaction fee uses. A validator prepays; the mark it prepaid to
// is its EndAccumulatedFee.

struct FeeConfig {
    gas::Gas capacity = 0;
    gas::Gas target = 0;
    gas::Price min_price = 0;
    gas::Gas excess_conversion_constant = 0;
};

struct FeeState {
    // How many validators there are right now.
    gas::Gas current = 0;
    // How far above target the chain has been running.
    gas::Gas excess = 0;

    friend bool operator==(const FeeState&, const FeeState&) = default;

    // Go: fee.State.AdvanceTime. Excess moves toward zero below target and away
    // from it above, saturating rather than wrapping at either end.
    FeeState advance_time(gas::Gas target, std::uint64_t seconds) const;

    // What `seconds` of being a validator costs from here. Saturates at
    // MaxUint64 rather than wrapping: an unpayable price is unpayable, not free.
    std::uint64_t cost_of(const FeeConfig& c, std::uint64_t seconds) const;

    // How long `funds` buys, capped at `max_seconds`. The inverse of cost_of,
    // and the reason a validator can be told when it will run out.
    std::uint64_t seconds_remaining(const FeeConfig& c, std::uint64_t max_seconds,
                                    std::uint64_t funds) const;
};

}  // namespace lux::platformvm::l1
