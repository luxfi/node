// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// consensus_mode.hpp — under which consensus posture the DEX may carry real
// money, and what it must say when it does.
//
// There are exactly two legal value modes plus the zero value. The guard is
// exhaustive and anything unmodelled falls through to refusal, so there is never
// a silent third state. The labeled CFT-parity mode is a legitimate launch
// posture, but it is not Byzantine finality, and the guard makes it say so.

#pragma once

#include "lux/dexvm/id.hpp"

#include <cstdint>
#include <string>
#include <string_view>

namespace lux::dexvm {

enum class ConsensusMode : std::uint8_t {
    // No value mode declared. Value activation under it is always refused.
    Unset = 0,
    // Post-quantum BFT with quorum finality. Byzantine fault tolerant, so a
    // value activation under it may legitimately claim Byzantine-finality
    // safety.
    QuorumFinality = 1,
    // A DELIBERATE, LABELED crash-fault-tolerant parity mode: the validator set
    // is assumed honest-but-crash-prone, NOT Byzantine. Value may activate under
    // it only with the launch safety bundle asserted, and only while surfacing
    // an explicit no-Byzantine-finality claim.
    HonestValidatorLabeled = 2,
};

std::string_view to_string(ConsensusMode m);

// parse_consensus_mode parses the canonical token. An unknown token is refused —
// it must not silently become a third state. The empty string is UNSET.
Result<ConsensusMode> parse_consensus_mode(std::string_view s);

// LaunchAssertions is the safety bundle a labeled-CFT activation must satisfy:
// the auditable record that the parity launch runs with the compensating
// controls that justify it. Every field must hold; a false field is a refusal.
struct LaunchAssertions {
    // Per-asset / per-market notional caps are enforced, bounding the blast
    // radius of a crash fault or operator error during the launch window.
    bool caps_on = false;
    // The registry admits only the three real kinds and no synthetic asset,
    // market or liquidity is enabled — the property this whole chain enforces.
    bool real_assets_only = false;
    // The halt control is wired and reachable, so the chain can be stopped fast
    // if the honest-validator assumption is violated.
    bool halt_ready = false;

    bool ok() const { return caps_on && real_assets_only && halt_ready; }
};

// kNoByzantineFinalityClaim is the EXACT status string a labeled-CFT activation
// surfaces. It is a constant so a status surface and an audit tool can match it
// byte for byte; the posture must never be quietly presented as Byzantine-final.
inline constexpr std::string_view kNoByzantineFinalityClaim =
    "DEX value active under HONEST_VALIDATOR_LABELED (CFT parity): no Byzantine-finality claim";

// ValueModeStatus is a successful guard's outcome: the mode that authorised the
// activation and, for labeled CFT parity, the disclaimer to surface. For quorum
// finality the status is empty — the finality is genuine, so nothing is
// disclaimed.
struct ValueModeStatus {
    ConsensusMode mode = ConsensusMode::Unset;
    std::string status;
};

// guard_value_activation is THE value guard.
//
//   value disabled            -> nothing to authorise; the unset status, no error
//   QUORUM_FINALITY           -> permitted; empty status
//   HONEST_VALIDATOR_LABELED  -> permitted only with the full bundle; status is
//                                the no-Byzantine-finality claim
//   anything else             -> REFUSED
//
// The default arm refuses, so an unmodelled or zero mode can never accidentally
// authorise value.
Result<ValueModeStatus> guard_value_activation(bool dex_native_value_enabled, ConsensusMode mode,
                                               LaunchAssertions assertions);

}  // namespace lux::dexvm
