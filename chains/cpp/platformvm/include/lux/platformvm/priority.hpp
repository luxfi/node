// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// priority.hpp — how stakers scheduled for the same instant are ordered.
//
// Rendered from Go vms/platformvm/txs/priorities.go. Two stakers whose next
// event falls at the same second must still have ONE order, or two honest nodes
// build different blocks from the same state. Priority is that order, and its
// numeric values are therefore consensus.
//
// The grouping is not arbitrary: permissioned validators leave the current set
// by the advance of time alone, so they must be removed BEFORE any
// permissionless staker, which leaves by a reward transaction issued after time
// has advanced.

#pragma once

#include <array>
#include <cstdint>

namespace lux::platformvm::txs {

enum class Priority : std::uint8_t {
    // Pending, in the order they move into the current set.
    PrimaryNetworkDelegatorLegacyPending = 1,
    PrimaryNetworkValidatorPending = 2,
    PrimaryNetworkDelegatorPermissionlessPending = 3,
    ChainPermissionlessValidatorPending = 4,
    ChainPermissionlessDelegatorPending = 5,
    ChainPermissionedValidatorPending = 6,

    // Current, in the order they leave it.
    ChainPermissionedValidatorCurrent = 7,
    ChainPermissionlessDelegatorCurrent = 8,
    ChainPermissionlessValidatorCurrent = 9,
    PrimaryNetworkDelegatorCurrent = 10,
    PrimaryNetworkValidatorCurrent = 11,
};

inline bool is_current_validator(Priority p) {
    return p == Priority::PrimaryNetworkValidatorCurrent ||
           p == Priority::ChainPermissionedValidatorCurrent ||
           p == Priority::ChainPermissionlessValidatorCurrent;
}

inline bool is_pending_validator(Priority p) {
    return p == Priority::PrimaryNetworkValidatorPending ||
           p == Priority::ChainPermissionedValidatorPending ||
           p == Priority::ChainPermissionlessValidatorPending;
}

inline bool is_current_delegator(Priority p) {
    return p == Priority::PrimaryNetworkDelegatorCurrent ||
           p == Priority::ChainPermissionlessDelegatorCurrent;
}

inline bool is_pending_delegator(Priority p) {
    return p == Priority::PrimaryNetworkDelegatorPermissionlessPending ||
           p == Priority::PrimaryNetworkDelegatorLegacyPending ||
           p == Priority::ChainPermissionlessDelegatorPending;
}

inline bool is_validator(Priority p) { return is_current_validator(p) || is_pending_validator(p); }

inline bool is_permissioned_validator(Priority p) {
    return p == Priority::ChainPermissionedValidatorCurrent ||
           p == Priority::ChainPermissionedValidatorPending;
}

// The current-set priority a pending staker takes when its start time arrives.
// A priority that is not a pending one has no image here.
inline Priority pending_to_current(Priority p) {
    switch (p) {
        case Priority::PrimaryNetworkDelegatorLegacyPending:
        case Priority::PrimaryNetworkDelegatorPermissionlessPending:
            return Priority::PrimaryNetworkDelegatorCurrent;
        case Priority::PrimaryNetworkValidatorPending:
            return Priority::PrimaryNetworkValidatorCurrent;
        case Priority::ChainPermissionlessValidatorPending:
            return Priority::ChainPermissionlessValidatorCurrent;
        case Priority::ChainPermissionlessDelegatorPending:
            return Priority::ChainPermissionlessDelegatorCurrent;
        case Priority::ChainPermissionedValidatorPending:
            return Priority::ChainPermissionedValidatorCurrent;
        default:
            return p;
    }
}

}  // namespace lux::platformvm::txs
