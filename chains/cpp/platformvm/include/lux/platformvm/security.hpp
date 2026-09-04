// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// security.hpp — a network's security configuration, as two orthogonal axes.
//
// Rendered from Go vms/platformvm/security. Pure values; no wire, no state. The
// three consumers that need it — the tx wire, the executor, and state — each
// compose it rather than restating it.
//
//   RestakeParent — the network leans on its parent's validator set
//   own set       — whether it runs its OWN set, described by (Admission, Manager)
//
// Their product spans every operating mode: a restaked L2 (RestakeParent,
// NoOwnSet), a sovereign L1 (¬RestakeParent, Open|Gated), and a hybrid L2 that
// restakes its parent AND runs an additive own set.

#pragma once

#include "lux/platformvm/error.hpp"

#include <cstdint>

namespace lux::platformvm::security {

// How a network's own validator set admits members.
enum class Admission : std::uint8_t {
    NoOwnSet = 0,  // no set of its own; security is entirely restaked
    Open = 1,      // permissionless: any staker meeting Threshold joins
    Gated = 2,     // membership admitted by the Manager
};

// The authority that governs a network's own validator set.
enum class Manager : std::uint8_t {
    PChain = 0,   // governed by P-chain txs, authorised by the network Owner
    Contract = 1, // governed by a staking contract on some chain
};

struct Mode {
    bool restake_parent = false;
    Admission admission = Admission::NoOwnSet;
    std::uint64_t threshold = 0;  // minimum stake for Open admission; 0 otherwise
    Manager manager = Manager::PChain;

    bool sovereign() const { return admission != Admission::NoOwnSet; }

    friend bool operator==(const Mode&, const Mode&) = default;

    // Enum ranges and the cross-axis invariant. Deliberately does NOT reach into
    // tx-level data (genesis validators, manager address) — those consistency
    // checks belong to the tx that carries the Mode.
    lux::platformvm::Status valid() const {
        if (static_cast<std::uint8_t>(admission) > static_cast<std::uint8_t>(Admission::Gated))
            return fail(Err::UnknownAdmission);
        if (static_cast<std::uint8_t>(manager) > static_cast<std::uint8_t>(Manager::Contract))
            return fail(Err::UnknownManager);
        if (!restake_parent && admission == Admission::NoOwnSet) return fail(Err::NoSecurity);
        return ok();
    }
};

}  // namespace lux::platformvm::security
