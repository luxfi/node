// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// status.hpp — what happened to a transaction, and what a blockchain is doing.
//
// Rendered from Go vms/platformvm/status. The numeric values are the wire and
// the API: the gaps in Status (0,4,5,6,8) are historical and stay holes, because
// a value that stops naming a state must not let the ones after it shift down.

#pragma once

#include "lux/platformvm/error.hpp"

#include <cstdint>
#include <string_view>

namespace lux::platformvm::status {

enum class Status : std::uint32_t {
    Unknown = 0,
    Committed = 4,
    Aborted = 5,
    Processing = 6,
    Dropped = 8,
};

inline std::string_view to_string(Status s) {
    switch (s) {
        case Status::Unknown: return "Unknown";
        case Status::Committed: return "Committed";
        case Status::Aborted: return "Aborted";
        case Status::Processing: return "Processing";
        case Status::Dropped: return "Dropped";
    }
    return "Invalid status";
}

inline lux::platformvm::Status verify(Status s) {
    switch (s) {
        case Status::Unknown:
        case Status::Committed:
        case Status::Aborted:
        case Status::Processing:
        case Status::Dropped:
            return ok();
    }
    return fail(Err::InvalidState, "unknown status");
}

enum class BlockchainStatus : std::uint32_t {
    UnknownChain = 0,
    Created = 1,
    Preferred = 2,
    Validating = 3,
    Syncing = 4,
};

inline std::string_view to_string(BlockchainStatus s) {
    switch (s) {
        case BlockchainStatus::UnknownChain: return "Unknown";
        case BlockchainStatus::Created: return "Created";
        case BlockchainStatus::Preferred: return "Preferred";
        case BlockchainStatus::Validating: return "Validating";
        case BlockchainStatus::Syncing: return "Syncing";
    }
    return "Invalid blockchain status";
}

inline lux::platformvm::Status verify(BlockchainStatus s) {
    switch (s) {
        case BlockchainStatus::UnknownChain:
        case BlockchainStatus::Created:
        case BlockchainStatus::Preferred:
        case BlockchainStatus::Validating:
        case BlockchainStatus::Syncing:
            return ok();
    }
    return fail(Err::InvalidState, "unknown blockchain status");
}

}  // namespace lux::platformvm::status
