// Copyright (C) 2026, Lux Industries, Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// uptime.hpp — how much of its term a validator was actually there for.
//
// Rendered from the uptime.Calculator surface the P-chain reads
// (github.com/luxfi/validators/uptime, as vms/platformvm/block/executor/options.go
// uses it). Uptime is MEASURED by the node — it is a fact about connections, not
// about the chain's state — so it enters the chain through this one question and
// no other. That is why it is an interface here rather than a table: a VM that
// could compute its own uptime could also decide its own reward.

#pragma once

#include "lux/platformvm/error.hpp"
#include "lux/platformvm/ids.hpp"

#include <cstdint>

namespace lux::platformvm::uptime {

class Calculator {
  public:
    virtual ~Calculator() = default;

    // The fraction of [since, now] this node was connected for, in [0, 1].
    // Go: CalculateUptimePercentFrom.
    virtual Result<double> percent_from(const NodeId& node_id, const Id& chain_id,
                                        std::uint64_t since) const = 0;
};

}  // namespace lux::platformvm::uptime
