// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The chain's foundational parameters.
//!
//! Every field here governs behaviour. Q-Chain charges no user fee at all
//! (LP-0130 §6 — finality-cert inclusion is a validator obligation, not
//! purchasable blockspace), so there is no fee schedule: a number nothing reads
//! is a price the chain does not actually charge, and reporting one over RPC
//! tells operators otherwise.

use std::time::Duration;

use crate::error::{Error, Result};

/// The parameter set an unset config settles on: ML-DSA-65, NIST level 3.
///
/// [`Config::default`] names this same constant, so a config the operator
/// filled in and one left blank land on the same parameter set. Two spellings
/// of the default meant a chain signed under ML-DSA-44 or ML-DSA-65 depending
/// on which door it came through.
pub const ALGORITHM_DEFAULT: u32 = 2;

/// The smallest committee that survives one Byzantine validator.
///
/// BFT tolerates f faults out of n ≥ 3f+1, so f ≥ 1 needs n ≥ 4. Below that the
/// quorum ⌊2n/3⌋+1 is the whole committee: one absent validator stops the chain
/// and one dishonest validator decides it.
pub const COMMITTEE_MIN: usize = 4;

/// How many validators must agree, for a committee of `n`.
///
/// The classical ⌊2n/3⌋+1, which for `n ≥ COMMITTEE_MIN` always lands strictly
/// between 2 and n — the range the consensus core accepts.
pub fn quorum(n: usize) -> usize {
    n * 2 / 3 + 1
}

/// What the VM was told to be.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Config {
    /// How many transactions may sit in the pool at once.
    pub max_parallel_txs: usize,

    /// 1 = ML-DSA-44, 2 = ML-DSA-65, 3 = ML-DSA-87. The parameter set fixes
    /// every key and signature width. Zero means unset and settles on
    /// ML-DSA-65; anything else is refused.
    pub quantum_algorithm_version: u32,

    /// Whether a transaction's stamp is checked at all.
    pub quantum_stamp_enabled: bool,

    /// How long a stamp stays valid after it is made.
    pub quantum_stamp_window: Duration,

    /// How many transactions one verification batch holds — and therefore how
    /// many a block carries.
    pub parallel_batch_size: usize,

    /// Whether Corona threshold key support is announced.
    pub corona_enabled: bool,

    /// The batch size at which hardware batch verification takes over.
    pub gpu_batch_threshold: usize,

    /// How many validators the finality committee holds. The threshold is
    /// derived from it, so it is the one number that decides how many faults
    /// the chain survives. Zero means unset and settles on [`COMMITTEE_MIN`];
    /// anything below that is refused.
    pub committee: usize,
}

impl Default for Config {
    fn default() -> Self {
        Config {
            max_parallel_txs: 100,
            quantum_algorithm_version: ALGORITHM_DEFAULT,
            quantum_stamp_enabled: true,
            quantum_stamp_window: Duration::from_secs(30),
            parallel_batch_size: 10,
            corona_enabled: true,
            gpu_batch_threshold: 8,
            committee: COMMITTEE_MIN,
        }
    }
}

impl Config {
    /// Replace any non-positive sizing with its default, and refuse an
    /// algorithm that does not exist.
    ///
    /// Each sizing divides or bounds a loop, so zero is not a weaker setting —
    /// it is a VM that batches nothing and builds empty blocks. The algorithm
    /// is different: an unrecognised number used to fall through to ML-DSA-65
    /// inside the signer, so an operator who asked for something else got a
    /// chain signing under a parameter set nobody chose, and never heard about
    /// it.
    pub fn validate(&mut self) -> Result<()> {
        match self.quantum_algorithm_version {
            0 => self.quantum_algorithm_version = ALGORITHM_DEFAULT,
            1..=3 => {}
            other => {
                let named = "1=ML-DSA-44, 2=ML-DSA-65, 3=ML-DSA-87";
                return Err(Error::Config(format!(
                    "quantum algorithm {other} does not exist ({named})"
                )));
            }
        }
        if self.max_parallel_txs == 0 {
            self.max_parallel_txs = 100;
        }
        if self.parallel_batch_size == 0 {
            self.parallel_batch_size = 10;
        }
        if self.gpu_batch_threshold == 0 {
            self.gpu_batch_threshold = 8;
        }
        if self.quantum_stamp_window.is_zero() {
            self.quantum_stamp_window = Duration::from_secs(30);
        }
        if self.committee == 0 {
            self.committee = COMMITTEE_MIN;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Go: config.TestQuorum — the quorum for the sizes the chain is run at.
    #[test]
    fn the_quorum_is_two_thirds_plus_one() {
        assert_eq!(quorum(4), 3);
        assert_eq!(quorum(5), 4);
        assert_eq!(quorum(7), 5);
        assert_eq!(quorum(10), 7);
        assert_eq!(quorum(100), 67);
    }

    #[test]
    fn a_quorum_is_strictly_between_two_and_the_committee() {
        for n in COMMITTEE_MIN..200 {
            let t = quorum(n);
            assert!(t > 2, "n={n} t={t}");
            assert!(t <= n, "n={n} t={t}");
            // More than two thirds: a second disjoint quorum cannot exist.
            assert!(t * 3 > n * 2, "n={n} t={t}");
        }
    }

    #[test]
    fn zero_settles_on_the_default_and_a_filled_in_config_lands_there_too() {
        let mut blank = Config {
            max_parallel_txs: 0,
            quantum_algorithm_version: 0,
            quantum_stamp_enabled: true,
            quantum_stamp_window: Duration::ZERO,
            parallel_batch_size: 0,
            corona_enabled: true,
            gpu_batch_threshold: 0,
            committee: 0,
        };
        blank.validate().unwrap();
        assert_eq!(blank, Config::default());
    }

    #[test]
    fn an_algorithm_that_does_not_exist_is_refused_not_silently_replaced() {
        let mut c = Config {
            quantum_algorithm_version: 9,
            ..Config::default()
        };
        let err = c.validate().unwrap_err();
        assert!(matches!(err, Error::Config(_)), "{err}");
        // And the field is left as the operator wrote it: a refusal does not
        // quietly rewrite the thing it refused.
        assert_eq!(c.quantum_algorithm_version, 9);
    }

    #[test]
    fn every_named_algorithm_is_accepted() {
        for v in 1..=3u32 {
            let mut c = Config {
                quantum_algorithm_version: v,
                ..Config::default()
            };
            c.validate().unwrap();
            assert_eq!(c.quantum_algorithm_version, v);
        }
    }
}
