// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Under which consensus posture the DEX may activate native value, and what an
//! activation must then say about itself.
//!
//! This is the one decision on this chain that moves real money, so the set of
//! postures is closed and the refusal is the default. Two postures are legal
//! and everything else — including declaring nothing — is refused.

use std::fmt;
use std::str::FromStr;

use crate::error::{Error, Result};

/// The CLOSED set of consensus postures under which value may activate.
///
/// The reference carries this as a `uint8` and needs a default arm to refuse the
/// values outside the set. Here the set IS the type, so there is no fourth value
/// to fall through to and no arm to forget: an unmodelled posture is not
/// something the guard has to catch, it is something a caller cannot construct.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
#[repr(u8)]
pub enum ConsensusMode {
    /// No value mode declared. Activation under it is always refused.
    #[default]
    Unset = 0,
    /// Post-quantum BFT with quorum finality. It is Byzantine fault tolerant, so
    /// an activation under it may legitimately claim Byzantine-finality safety.
    QuorumFinality = 1,
    /// A DELIBERATE, LABELED crash-fault-tolerant parity mode: the validator set
    /// is assumed honest-but-crash-prone, NOT Byzantine. It is a legitimate
    /// launch posture and it MUST NOT be presented as Byzantine finality —
    /// which is why activating under it requires the launch bundle and carries
    /// an explicit disclaimer.
    HonestValidatorLabeled = 2,
}

impl fmt::Display for ConsensusMode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            ConsensusMode::QuorumFinality => "QUORUM_FINALITY",
            ConsensusMode::HonestValidatorLabeled => "HONEST_VALIDATOR_LABELED",
            ConsensusMode::Unset => "UNSET",
        })
    }
}

/// A posture from its canonical token. An unknown token is refused rather than
/// silently becoming a third state; the empty token IS `UNSET`, because a caller
/// who declared nothing has declared the unset posture and the guard refuses it
/// on its own terms.
impl FromStr for ConsensusMode {
    type Err = Error;

    fn from_str(s: &str) -> Result<ConsensusMode> {
        match s {
            "QUORUM_FINALITY" => Ok(ConsensusMode::QuorumFinality),
            "HONEST_VALIDATOR_LABELED" => Ok(ConsensusMode::HonestValidatorLabeled),
            "" | "UNSET" => Ok(ConsensusMode::Unset),
            other => Err(Error::UnknownMode(other.to_string())),
        }
    }
}

/// The safety bundle a `HONEST_VALIDATOR_LABELED` activation must satisfy: the
/// explicit, auditable record that the CFT-parity launch is running with the
/// compensating controls that justify it. Every field must hold; a false one is
/// a refusal.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct LaunchAssertions {
    /// Per-asset and per-market notional caps are enforced, bounding the blast
    /// radius of a crash fault or an operator error during the launch window.
    pub caps_on: bool,
    /// The asset registry admits only `EVM_NATIVE`/`ERC20`/`UTXO` and no
    /// synthetic asset, market or liquidity is enabled — the property this whole
    /// crate enforces.
    pub real_assets_only: bool,
    /// The halt control is wired and reachable, so the chain can be stopped fast
    /// if the honest-validator assumption is violated.
    pub halt_ready: bool,
}

impl LaunchAssertions {
    /// The whole bundle, or nothing.
    pub fn complete(self) -> bool {
        self.caps_on && self.real_assets_only && self.halt_ready
    }
}

/// The exact status string a `HONEST_VALIDATOR_LABELED` activation surfaces. It
/// is a constant so a status surface and an audit can match it byte for byte;
/// the launch posture must never be silently presented as Byzantine-final.
pub const NO_BYZANTINE_FINALITY_CLAIM: &str =
    "DEX value active under HONEST_VALIDATOR_LABELED (CFT parity): no Byzantine-finality claim";

/// What a successful guard check returns: the posture that authorised the
/// activation and, for the labeled CFT-parity posture, the disclaimer it must
/// surface. For `QUORUM_FINALITY` the status is empty — the Byzantine finality
/// is genuine, so no disclaimer is owed.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ValueModeStatus {
    pub mode: ConsensusMode,
    pub status: &'static str,
}

/// Whether the DEX may activate native value under this posture, and what the
/// activation must then say.
///
/// - value not requested — nothing to authorise; the DEX runs in paper mode.
/// - `QUORUM_FINALITY` — permitted, no disclaimer.
/// - `HONEST_VALIDATOR_LABELED` — permitted only with the whole launch bundle,
///   and it carries the no-Byzantine-finality claim.
/// - `UNSET` — refused.
pub fn guard_value_activation(
    value_enabled: bool,
    mode: ConsensusMode,
    assertions: LaunchAssertions,
) -> Result<ValueModeStatus> {
    if !value_enabled {
        // No native value requested, so there is nothing to authorise. That is
        // not a refusal: the posture is reported back as it was given.
        return Ok(ValueModeStatus { mode, status: "" });
    }
    match mode {
        ConsensusMode::QuorumFinality => Ok(ValueModeStatus { mode, status: "" }),
        ConsensusMode::HonestValidatorLabeled => {
            if !assertions.complete() {
                return Err(Error::LaunchAssertionsUnmet {
                    caps_on: assertions.caps_on,
                    real_assets_only: assertions.real_assets_only,
                    halt_ready: assertions.halt_ready,
                });
            }
            Ok(ValueModeStatus {
                mode,
                status: NO_BYZANTINE_FINALITY_CLAIM,
            })
        }
        ConsensusMode::Unset => Err(Error::ValueModeUnset),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const WHOLE_BUNDLE: LaunchAssertions = LaunchAssertions {
        caps_on: true,
        real_assets_only: true,
        halt_ready: true,
    };

    #[test]
    fn a_posture_round_trips_through_its_canonical_token() {
        for m in [
            ConsensusMode::Unset,
            ConsensusMode::QuorumFinality,
            ConsensusMode::HonestValidatorLabeled,
        ] {
            assert_eq!(m.to_string().parse::<ConsensusMode>().unwrap(), m);
        }
        assert_eq!("".parse::<ConsensusMode>().unwrap(), ConsensusMode::Unset);
        assert_eq!(
            "BFT".parse::<ConsensusMode>().unwrap_err(),
            Error::UnknownMode("BFT".into())
        );
    }

    #[test]
    fn declaring_nothing_never_authorises_value() {
        assert_eq!(
            guard_value_activation(true, ConsensusMode::Unset, WHOLE_BUNDLE).unwrap_err(),
            Error::ValueModeUnset
        );
        // Even with every assertion made. The bundle is what makes the LABELED
        // posture legal; it is not a substitute for declaring one.
        assert_eq!(ConsensusMode::default(), ConsensusMode::Unset);
    }

    #[test]
    fn byzantine_finality_needs_no_disclaimer_and_cft_parity_does() {
        let bft =
            guard_value_activation(true, ConsensusMode::QuorumFinality, WHOLE_BUNDLE).unwrap();
        assert_eq!(bft.status, "");
        let cft = guard_value_activation(true, ConsensusMode::HonestValidatorLabeled, WHOLE_BUNDLE)
            .unwrap();
        assert_eq!(cft.status, NO_BYZANTINE_FINALITY_CLAIM);
    }

    #[test]
    fn the_labeled_posture_needs_the_whole_bundle_not_part_of_it() {
        for missing in [
            LaunchAssertions {
                caps_on: false,
                ..WHOLE_BUNDLE
            },
            LaunchAssertions {
                real_assets_only: false,
                ..WHOLE_BUNDLE
            },
            LaunchAssertions {
                halt_ready: false,
                ..WHOLE_BUNDLE
            },
        ] {
            assert!(matches!(
                guard_value_activation(true, ConsensusMode::HonestValidatorLabeled, missing),
                Err(Error::LaunchAssertionsUnmet { .. })
            ));
        }
    }

    #[test]
    fn paper_mode_authorises_nothing_and_refuses_nothing() {
        // No value requested: every posture passes, and the one that came in is
        // the one reported back, with no disclaimer attached to a claim nobody
        // made.
        for m in [
            ConsensusMode::Unset,
            ConsensusMode::QuorumFinality,
            ConsensusMode::HonestValidatorLabeled,
        ] {
            let st = guard_value_activation(false, m, LaunchAssertions::default()).unwrap();
            assert_eq!(st.mode, m);
            assert_eq!(st.status, "");
        }
    }
}
