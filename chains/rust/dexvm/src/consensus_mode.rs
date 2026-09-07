// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Under which consensus posture the DEX may activate NATIVE VALUE.
//!
//! There are exactly two legal value modes plus the zero value. The guard is
//! exhaustive and anything unmodelled falls through to refusal, so there is
//! never a silent third state.

use crate::error::{Code, Error, Result};

/// The CLOSED set of consensus postures.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum ConsensusMode {
    /// No value mode declared. Value activation under it is always refused.
    #[default]
    Unset,
    /// Post-quantum BFT with quorum finality. It is Byzantine fault tolerant,
    /// so a value activation under it may legitimately claim
    /// Byzantine-finality safety.
    QuorumFinality,
    /// A DELIBERATE, LABELED crash-fault-tolerant parity mode: the validator set
    /// is assumed honest-but-crash-prone, NOT Byzantine. It is a legitimate
    /// launch posture, but it MUST NOT be presented as Byzantine-finality.
    /// Value under it is permitted ONLY with the full launch safety bundle, and
    /// it surfaces an explicit no-Byzantine-finality status.
    HonestValidatorLabeled,
    /// A mode outside the closed set. It exists so a caller CAN name one — the
    /// guard's job is to refuse it, and a guard that could not be handed an
    /// unmodelled value could not be shown to refuse one.
    Other(u8),
}

impl ConsensusMode {
    /// The canonical token.
    pub fn as_str(self) -> &'static str {
        match self {
            ConsensusMode::QuorumFinality => "QUORUM_FINALITY",
            ConsensusMode::HonestValidatorLabeled => "HONEST_VALIDATOR_LABELED",
            _ => "UNSET",
        }
    }
}

impl std::fmt::Display for ConsensusMode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// Parse the canonical token. An unknown token is refused — it must not
/// silently become a third state.
pub fn parse_consensus_mode(s: &str) -> Result<ConsensusMode> {
    match s {
        "QUORUM_FINALITY" => Ok(ConsensusMode::QuorumFinality),
        "HONEST_VALIDATOR_LABELED" => Ok(ConsensusMode::HonestValidatorLabeled),
        "" | "UNSET" => Ok(ConsensusMode::Unset),
        other => Err(Error::other(format!(
            "registry: unknown consensus mode {other:?} (only QUORUM_FINALITY or HONEST_VALIDATOR_LABELED)"
        ))),
    }
}

/// The safety bundle a labeled-CFT activation MUST satisfy: the explicit,
/// auditable record that the parity launch runs with the compensating controls
/// that justify it. Every field must hold; a false one is a refusal.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct LaunchAssertions {
    /// Per-asset and per-market notional caps are enforced, bounding the blast
    /// radius of a crash fault or operator error during the launch window.
    pub caps_on: bool,
    /// The asset registry admits only the three real kinds and no synthetic
    /// asset, market or liquidity is enabled.
    pub real_assets_only: bool,
    /// The halt control is wired and reachable, so the chain can be stopped
    /// fast if the honest-validator assumption is violated.
    pub halt_ready: bool,
}

impl LaunchAssertions {
    fn ok(&self) -> bool {
        self.caps_on && self.real_assets_only && self.halt_ready
    }
}

/// The EXACT status string a labeled-CFT activation surfaces. It is a constant
/// so a status surface and any audit tooling can match it byte-for-byte; the
/// launch posture must never be silently presented as Byzantine-final.
pub const NO_BYZANTINE_FINALITY_CLAIM: &str =
    "DEX value active under HONEST_VALIDATOR_LABELED (CFT parity): no Byzantine-finality claim";

/// The outcome of a successful value-activation check: the mode that authorised
/// it and, for labeled CFT parity, the disclaimer to surface. For quorum
/// finality the status is empty — Byzantine finality is genuine, so no
/// disclaimer is required.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ValueModeStatus {
    pub mode: ConsensusMode,
    pub status: String,
}

/// Whether the DEX may activate native value under the given consensus mode,
/// and the status the activation must surface.
///
/// ```text
/// !dex_native_value_enabled  -> nothing to gate; the unset status, no error.
/// QUORUM_FINALITY            -> permitted; status "".
/// HONEST_VALIDATOR_LABELED   -> permitted ONLY with the full bundle;
///                               status = NO_BYZANTINE_FINALITY_CLAIM.
/// anything else              -> REFUSED.
/// ```
///
/// The final arm refuses, so an unmodelled or default mode can never
/// accidentally authorise value.
pub fn guard_value_activation(
    dex_native_value_enabled: bool,
    mode: ConsensusMode,
    assertions: LaunchAssertions,
) -> Result<ValueModeStatus> {
    if !dex_native_value_enabled {
        // No native value requested — nothing to authorise. Not an error; the
        // DEX runs in non-value mode.
        return Ok(ValueModeStatus {
            mode,
            status: String::new(),
        });
    }
    match mode {
        ConsensusMode::QuorumFinality => Ok(ValueModeStatus {
            mode,
            status: String::new(),
        }),
        ConsensusMode::HonestValidatorLabeled => {
            if !assertions.ok() {
                return Err(Error::note(
                    Code::LaunchAssertionsUnmet,
                    format!(
                        "capsOn={} realAssetsOnly={} haltReady={}",
                        assertions.caps_on, assertions.real_assets_only, assertions.halt_ready
                    ),
                ));
            }
            Ok(ValueModeStatus {
                mode,
                status: NO_BYZANTINE_FINALITY_CLAIM.to_string(),
            })
        }
        ConsensusMode::Unset => Err(Error::of(Code::ValueModeUnset)),
        // Closed enum: any value outside the two legal modes is refused. There
        // is never a silent third state.
        ConsensusMode::Other(v) => Err(Error::detail(Code::ValueModeIllegal, v.to_string())),
    }
}
