// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Which networks bear value, and which are a developer's to experiment on.
//!
//! The distinction governs one thing: whether a synthetic asset, market or
//! liquidity flag may be set at all. On a value-bearing network it may not, at
//! any setting. On a dev network a developer may opt in — and value there is
//! still guarded by the consensus posture in [`crate::mode`], which this class
//! does not touch.

use std::fmt;

/// A network's class, from its id.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
#[repr(u8)]
pub enum NetworkClass {
    /// Devnet and localnet, and every sovereign L1 id — synthetic flags MAY be
    /// set.
    Dev = 0,
    /// Testnet — synthetic flags forbidden.
    Testnet = 1,
    /// Mainnet — synthetic flags forbidden.
    Mainnet = 2,
}

impl NetworkClass {
    /// Whether this class forbids every synthetic flag. Mainnet and testnet
    /// both do: a testnet whose depth is fabricated teaches the wrong number.
    pub fn value_bearing(self) -> bool {
        matches!(self, NetworkClass::Mainnet | NetworkClass::Testnet)
    }
}

impl fmt::Display for NetworkClass {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            NetworkClass::Mainnet => "mainnet",
            NetworkClass::Testnet => "testnet",
            NetworkClass::Dev => "dev",
        })
    }
}

/// The class of a Lux network id, under the convention-fixed ids: 1 mainnet,
/// 2 testnet, 3 local, 1337 localnet. Every other id — which is every sovereign
/// L1's own primary network — is dev class for the purpose of the synthetic-flag
/// permission, and nothing else.
pub fn network_class_for(network: u32) -> NetworkClass {
    match network {
        1 => NetworkClass::Mainnet,
        2 => NetworkClass::Testnet,
        _ => NetworkClass::Dev,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_convention_fixed_ids_name_their_own_classes() {
        assert_eq!(network_class_for(1), NetworkClass::Mainnet);
        assert_eq!(network_class_for(2), NetworkClass::Testnet);
        assert_eq!(network_class_for(3), NetworkClass::Dev);
        assert_eq!(network_class_for(1337), NetworkClass::Dev);
    }

    #[test]
    fn a_sovereign_l1_is_dev_class_for_this_one_question() {
        // A sovereign L1's primary network id equals its EVM chain id, so the
        // ids are large and arbitrary. None of them is mainnet or testnet HERE,
        // which decides synthetic flags and nothing else — value on those
        // networks is guarded by the consensus posture.
        for id in [8675309u32, 36963, 200200, 494949, 96369] {
            assert_eq!(network_class_for(id), NetworkClass::Dev);
            assert!(!network_class_for(id).value_bearing());
        }
    }

    #[test]
    fn both_value_bearing_classes_forbid_a_synthetic_flag() {
        assert!(NetworkClass::Mainnet.value_bearing());
        assert!(NetworkClass::Testnet.value_bearing());
        assert!(!NetworkClass::Dev.value_bearing());
    }

    #[test]
    fn a_class_renders_as_its_own_token() {
        assert_eq!(NetworkClass::Mainnet.to_string(), "mainnet");
        assert_eq!(NetworkClass::Testnet.to_string(), "testnet");
        assert_eq!(NetworkClass::Dev.to_string(), "dev");
    }
}
