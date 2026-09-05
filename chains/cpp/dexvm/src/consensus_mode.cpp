// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

#include "lux/dexvm/consensus_mode.hpp"

namespace lux::dexvm {

std::string_view to_string(ConsensusMode m) {
    switch (m) {
        case ConsensusMode::QuorumFinality:         return "QUORUM_FINALITY";
        case ConsensusMode::HonestValidatorLabeled: return "HONEST_VALIDATOR_LABELED";
        default:                                    return "UNSET";
    }
}

Result<ConsensusMode> parse_consensus_mode(std::string_view s) {
    if (s == "QUORUM_FINALITY") return ConsensusMode::QuorumFinality;
    if (s == "HONEST_VALIDATOR_LABELED") return ConsensusMode::HonestValidatorLabeled;
    if (s.empty() || s == "UNSET") return ConsensusMode::Unset;
    return fail("registry: unknown consensus mode \"" + std::string(s) +
                "\" (only QUORUM_FINALITY or HONEST_VALIDATOR_LABELED)");
}

Result<ValueModeStatus> guard_value_activation(bool dex_native_value_enabled, ConsensusMode mode,
                                               LaunchAssertions assertions) {
    if (!dex_native_value_enabled) {
        // No native value requested — nothing to authorise. Not an error: the
        // DEX runs in non-value (paper) mode.
        return ValueModeStatus{mode, ""};
    }
    switch (mode) {
        case ConsensusMode::QuorumFinality:
            return ValueModeStatus{mode, ""};
        case ConsensusMode::HonestValidatorLabeled:
            if (!assertions.ok())
                return fail_note(Err::LaunchAssertionsUnmet,
                                 "capsOn=" + std::string(assertions.caps_on ? "true" : "false") +
                                     " realAssetsOnly=" +
                                     std::string(assertions.real_assets_only ? "true" : "false") +
                                     " haltReady=" +
                                     std::string(assertions.halt_ready ? "true" : "false"));
            return ValueModeStatus{mode, std::string(kNoByzantineFinalityClaim)};
        case ConsensusMode::Unset:
            return fail(Err::ValueModeUnset);
        default:
            // Closed enum: any value outside the two legal modes is refused.
            // There is never a silent third state.
            return fail(Err::ValueModeIllegal,
                        std::to_string(std::uint32_t(std::uint8_t(mode))));
    }
}

}  // namespace lux::dexvm
