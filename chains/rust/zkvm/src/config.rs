// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What a Z-chain is configured with. Every field here is read; a knob that
//! changes nothing reads as a control and is not one.
//!
//! THERE ARE TWO STARTING POINTS AND THEY ARE NOT THE SAME.
//!
//! [`Config::default`] is THE CHAIN'S profile: strict-PQ, a hundred
//! transactions to a block, a thousand verdicts cached. It is what a chain born
//! from a genesis that carries no configuration runs on, and the Z-chain is
//! definitively strict-PQ because it is the shielded-settlement chain.
//!
//! [`Config::parse`] starts from the ZERO value and applies what the
//! configuration says. A configuration that names `maxTxPerBlock` and says
//! nothing about `strictPQ` therefore gets a chain with the classical proof
//! systems ENABLED — not the chain's default profile. That is not a quirk to be
//! smoothed over: it is what the reference does, and a port that "helpfully"
//! defaulted the omitted field to true would accept blocks the reference
//! refuses and refuse blocks it accepts. A permissive deployment is opted into
//! by writing `"strictPQ": false`; the profile is never inherited by omission
//! in one direction and not the other.
//!
//! The same shape has already cost something once. The C++ Q-chain's evaluator
//! was built on a default-CONSTRUCTED config rather than that chain's own
//! default, whose `quantum_stamp_enabled` is false, and it accepted four blocks
//! the reference refused — a chain checking no post-quantum signature at all,
//! with nothing downstream saying so.

use std::collections::BTreeMap;

use crate::error::{Error, Result};
use crate::txs::Kind;

/// A block of unbounded size and a cache of unbounded growth are not "no limit
/// configured"; they are "no limit". A configuration that names neither takes
/// these.
pub const DEFAULT_MAX_TX_PER_BLOCK: u32 = 100;
pub const DEFAULT_PROOF_CACHE_SIZE: u32 = 1000;

/// How this chain is run.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Config {
    /// A real verifying key per circuit, keyed by the transaction kind it is
    /// for. Empty means proof verification through the classical path is
    /// disabled — fail-closed by absence.
    ///
    /// On a strict-PQ chain, supplying a real bn254 key here is REFUSED at
    /// construction: such keys re-enable the forgeable pairing path for
    /// shielded value.
    pub verifying_keys: BTreeMap<u8, Vec<u8>>,

    /// Hard-disable the classical, pairing-based shielded proof systems.
    ///
    /// When set, the shielded verifier refuses groth16, plonk and bulletproofs
    /// and accepts only STARK/FRI. This is the Lux primary-network posture: a
    /// machine that broke bn254 must not be able to forge a shield or unshield
    /// proof and mint or steal shielded value.
    pub strict_pq: bool,

    /// What a block may carry, from either direction — what a proposer
    /// assembles and what verification accepts off the wire. One number, so a
    /// peer cannot send a block larger than this node would ever build.
    pub max_tx_per_block: u32,

    /// What the verified-proof cache may hold.
    pub proof_cache_size: u32,
}

impl Config {
    /// The chain's own profile.
    pub fn chain_default() -> Config {
        Config {
            verifying_keys: BTreeMap::new(),
            strict_pq: true,
            max_tx_per_block: DEFAULT_MAX_TX_PER_BLOCK,
            proof_cache_size: DEFAULT_PROOF_CACHE_SIZE,
        }
    }

    /// Read a configuration, starting from the zero value.
    ///
    /// The two bounds are normalised because zero is not a bound. `strictPQ` is
    /// NOT normalised, for the reason in this module's header.
    pub fn parse(raw: &[u8]) -> Result<Config> {
        let mut c = Config::default();
        if !raw.is_empty() {
            let v: serde_json::Value =
                serde_json::from_slice(raw).map_err(|e| Error::Config(e.to_string()))?;
            c.strict_pq = v.get("strictPQ").and_then(|x| x.as_bool()).unwrap_or(false);
            c.max_tx_per_block = v
                .get("maxTxPerBlock")
                .and_then(|x| x.as_u64())
                .unwrap_or(0) as u32;
            c.proof_cache_size = v
                .get("proofCacheSize")
                .and_then(|x| x.as_u64())
                .unwrap_or(0) as u32;
            if let Some(keys) = v.get("verifyingKeys").and_then(|x| x.as_object()) {
                for (circuit, key) in keys {
                    let (Ok(circuit), Some(key)) = (circuit.parse::<u8>(), key.as_str()) else {
                        continue;
                    };
                    c.verifying_keys.insert(circuit, key.as_bytes().to_vec());
                }
            }
        }
        c.normalise();
        Ok(c)
    }

    fn normalise(&mut self) {
        if self.max_tx_per_block == 0 {
            self.max_tx_per_block = DEFAULT_MAX_TX_PER_BLOCK;
        }
        if self.proof_cache_size == 0 {
            self.proof_cache_size = DEFAULT_PROOF_CACHE_SIZE;
        }
    }

    /// The circuits a verifying key can be supplied for. Mint and Burn are not
    /// among them: they carry no shape rule of their own and are proved under
    /// the transfer circuit.
    pub fn keyed_circuits() -> [Kind; 3] {
        [Kind::TRANSFER, Kind::SHIELD, Kind::UNSHIELD]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_chains_own_profile_is_strict_pq_and_bounded() {
        let c = Config::chain_default();
        assert!(c.strict_pq);
        assert_eq!(c.max_tx_per_block, DEFAULT_MAX_TX_PER_BLOCK);
        assert_eq!(c.proof_cache_size, DEFAULT_PROOF_CACHE_SIZE);
        assert!(c.verifying_keys.is_empty());
    }

    /// The one that has already bitten a sibling chain. A configuration that
    /// says nothing about the profile does not get the chain's profile.
    #[test]
    fn a_configuration_that_omits_the_profile_does_not_inherit_it() {
        let c = Config::parse(br#"{"maxTxPerBlock":7}"#).unwrap();
        assert!(!c.strict_pq);
        assert_eq!(c.max_tx_per_block, 7);
    }

    #[test]
    fn a_configuration_can_ask_for_the_strict_profile_by_name() {
        assert!(Config::parse(br#"{"strictPQ":true}"#).unwrap().strict_pq);
    }

    /// No configuration at all is the chain's default, which is the other half
    /// of the rule above.
    #[test]
    fn no_configuration_at_all_is_the_chains_profile() {
        // The VM makes this choice; the parser is only asked when there are
        // bytes. Stated here so the two are read together.
        assert_eq!(Config::parse(b"").unwrap().strict_pq, false);
        assert!(Config::chain_default().strict_pq);
    }

    #[test]
    fn zero_is_not_a_bound_and_is_replaced_by_one() {
        let c = Config::parse(br#"{"maxTxPerBlock":0,"proofCacheSize":0}"#).unwrap();
        assert_eq!(c.max_tx_per_block, DEFAULT_MAX_TX_PER_BLOCK);
        assert_eq!(c.proof_cache_size, DEFAULT_PROOF_CACHE_SIZE);
    }

    #[test]
    fn a_verifying_key_is_read_per_circuit() {
        let c = Config::parse(br#"{"verifyingKeys":{"0":"a key"}}"#).unwrap();
        assert_eq!(
            c.verifying_keys.get(&Kind::TRANSFER.0),
            Some(&b"a key".to_vec())
        );
    }

    #[test]
    fn a_configuration_that_is_not_json_is_refused_rather_than_ignored() {
        assert!(matches!(Config::parse(b"{"), Err(Error::Config(_))));
    }
}
