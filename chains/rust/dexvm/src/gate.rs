// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The single fail-closed startup gate.
//!
//! It runs once, before the chain accepts work, over the already-populated
//! registry. It returns success ONLY when the registry is fully real and the
//! policy is locked down for the network. Anything ambiguous fails closed.

use crate::asset::AssetKind;
use crate::error::{Code, Error, Result};
use crate::forbidden::assert_no_forbidden_asset_refs;
use crate::ids::{self, Id};
use crate::registry::Registry;
use serde::{Deserialize, Serialize};

/// Which networks forbid a synthetic flag outright.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NetworkClass {
    /// devnet / localnet — synthetic flags MAY be set.
    Dev,
    /// testnet — synthetic flags FORBIDDEN.
    Testnet,
    /// mainnet — synthetic flags FORBIDDEN.
    Mainnet,
}

impl NetworkClass {
    /// Whether this class forbids any synthetic flag. Mainnet and testnet both
    /// carry value for the purpose of this gate.
    fn value_bearing(self) -> bool {
        matches!(self, NetworkClass::Mainnet | NetworkClass::Testnet)
    }
}

impl std::fmt::Display for NetworkClass {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match self {
            NetworkClass::Mainnet => "mainnet",
            NetworkClass::Testnet => "testnet",
            NetworkClass::Dev => "dev",
        })
    }
}

/// The class of a Lux networkID, using the convention-fixed ids (1 mainnet,
/// 2 testnet, 3 local, 1337 localnet). Every other id — 3, 1337 and every
/// sovereign L1 id — is dev-class for the purpose of synthetic-flag permission.
/// Value on those is still guarded by the consensus-mode guard; this class only
/// governs the synthetic flags.
pub fn network_class_for(network_id: u32) -> NetworkClass {
    match network_id {
        1 => NetworkClass::Mainnet,
        2 => NetworkClass::Testnet,
        _ => NetworkClass::Dev,
    }
}

/// The backend-enforced, fail-closed configuration for the DEX's
/// real-assets-only posture.
///
/// Every field's SAFE value is its default, so a default-initialised policy is
/// the locked-down policy. These are not front-end toggles: they live in the VM
/// config and are read once at initialisation. A front end cannot relax them.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct DexAssetPolicy {
    /// On a value-bearing network any true value fails startup. On a dev network
    /// a true value is permitted — developer opt-in — but is still subject to
    /// the forbidden-reference scan: an off-network universe is never allowed.
    #[serde(rename = "dexAllowSyntheticAssets", default)]
    pub allow_synthetic_assets: bool,
    #[serde(rename = "dexAllowSyntheticMarkets", default)]
    pub allow_synthetic_markets: bool,
    #[serde(rename = "dexAllowMockLiquidity", default)]
    pub allow_mock_liquidity: bool,
    /// The active `dexAllowedAssetKinds` set. Empty is the canonical default of
    /// all three; an explicit set may only ever be a SUBSET of them.
    #[serde(rename = "dexAllowedAssetKinds", default)]
    pub allowed_asset_kinds: Vec<AssetKind>,
}

impl DexAssetPolicy {
    /// The canonical locked-down policy: no synthetic anything, all three real
    /// kinds allowed.
    pub fn default_policy() -> DexAssetPolicy {
        DexAssetPolicy {
            allow_synthetic_assets: false,
            allow_synthetic_markets: false,
            allow_mock_liquidity: false,
            allowed_asset_kinds: vec![AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo],
        }
    }

    /// The effective allowed-kind set: the canonical three when unset, else
    /// exactly the configured subset.
    pub fn allowed_kinds_or_default(&self) -> Vec<AssetKind> {
        if self.allowed_asset_kinds.is_empty() {
            vec![AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo]
        } else {
            self.allowed_asset_kinds.clone()
        }
    }

    /// Whether any synthetic or mock flag is set. The node uses it to
    /// MACHINE-DERIVE the labeled-launch "real-assets-only" assertion rather
    /// than trusting an operator-supplied claim.
    pub fn any_synthetic_flag(&self) -> bool {
        self.allow_synthetic_assets || self.allow_synthetic_markets || self.allow_mock_liquidity
    }
}

/// Refuse startup under a synthetic configuration.
///
/// This is called once at initialisation, BEFORE the chain accepts work, with
/// the already-populated registry and the VM's network class and policy. It
/// refuses if ANY of the following hold:
///
/// 1. `dexAllowedAssetKinds` contains a non-real kind — it may only ever be a
///    subset of the three.
/// 2. The network is value-bearing AND any synthetic flag is true.
/// 3. Any registered asset carries a forbidden reference: an off-network
///    white-label universe chain, mock liquidity, or an ASCII-ticker id. The
///    positive reality check already happened at registration; this is the
///    residual deny-scan, which reality alone would not catch.
/// 4. Any ENABLED market references an asset that is not registered, is
///    disabled, or spans a network mismatch.
///
/// `chain_label_for` maps a source chain id to its human label so the
/// off-network scan can run; a chain with no label simply cannot be a known
/// white-label universe, and its asset already passed the reality gate.
pub fn refuse_under_synthetic_config(
    class: NetworkClass,
    policy: &DexAssetPolicy,
    reg: &Registry,
    chain_label_for: Option<&dyn Fn(Id) -> String>,
) -> Result<()> {
    // (1) allowed-kinds must be a subset of the three real kinds.
    for k in policy.allowed_kinds_or_default() {
        if !k.valid() {
            return Err(Error::detail(
                Code::BadAllowedKind,
                format!("{:?}", k.as_str()),
            ));
        }
    }

    // (2) no synthetic flags on a value-bearing network.
    if class.value_bearing() && policy.any_synthetic_flag() {
        return Err(Error::note(
            Code::SyntheticOnValueNet,
            format!(
                "network={class} synthAssets={} synthMarkets={} mockLiq={}",
                policy.allow_synthetic_assets,
                policy.allow_synthetic_markets,
                policy.allow_mock_liquidity
            ),
        ));
    }

    // (3) deny-scan every registered asset. Run before the market scan so a
    // tainted asset is reported at its source.
    for (id, a) in reg.assets() {
        let label = chain_label_for.map(|f| f(a.chain_id)).unwrap_or_default();
        assert_no_forbidden_asset_refs(&a.symbol, &a.name, &label)
            .map_err(|e| e.wrap(format!("registered asset {}", ids::cb58(&id))))?;
    }

    // (4) every ENABLED market must resolve both sides to a registered, enabled
    // asset on the matching network. A market over a synthetic asset has no
    // AssetID to resolve, so this catches it structurally.
    for (id, m) in reg.markets() {
        if !m.enabled {
            continue;
        }
        let base = reg.must_resolve_enabled(&m.base_asset_id).map_err(|e| {
            Error::detail(
                Code::EnabledMarketUnknownAsset,
                format!("market {} base: {}", ids::cb58(&id), e),
            )
        })?;
        let quote = reg.must_resolve_enabled(&m.quote_asset_id).map_err(|e| {
            Error::detail(
                Code::EnabledMarketUnknownAsset,
                format!("market {} quote: {}", ids::cb58(&id), e),
            )
        })?;
        if m.network_id != base.network_id || m.network_id != quote.network_id {
            return Err(Error::detail(
                Code::EnabledMarketUnknownAsset,
                format!(
                    "market {} network mismatch (market={} base={} quote={})",
                    ids::cb58(&id),
                    m.network_id,
                    base.network_id,
                    quote.network_id
                ),
            ));
        }
    }
    Ok(())
}
