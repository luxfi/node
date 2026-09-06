// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! An admitted trading pair.
//!
//! Both sides are pinned to registered, real, enabled assets by construction: a
//! market cannot be created unless both AssetIDs resolve. Its own id is DERIVED
//! from the two assets and the venue, never supplied, so a market's identity is
//! structurally bound to the real assets underneath it. There is no AssetID for
//! a synthetic asset, so there is no market name over one.

use crate::asset::market_id;
use crate::error::{Error, Result};
use crate::ids::Id;
use crate::registry::Registry;

/// A trading pair over two registered assets.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Market {
    /// A market does not span networks: both assets must live on this one.
    pub network: u32,
    /// Canonical AssetIDs of registered assets. The order is the book's: base
    /// against quote, and the reverse pair is a different market.
    pub base: Id,
    pub quote: Id,
    /// The canonical serialization of the venue parameters — tick, lot, fee
    /// tier — that distinguish two venues on one pair.
    pub venue: Vec<u8>,
    /// Whether the market trades. A disabled market is still pinned to real
    /// assets; it simply admits no orders.
    pub enabled: bool,
}

impl Market {
    /// The canonical MarketID.
    pub fn id(&self) -> Id {
        market_id(self.network, self.base, self.quote, &self.venue)
    }
}

impl Registry {
    /// Admit a market, only if BOTH sides resolve to a registered, enabled, real
    /// asset on the SAME network as the market. This is where "no synthetic
    /// market" stops being a policy and becomes a shape: a synthetic asset has
    /// no id, so it does not resolve, so the market cannot be created.
    ///
    /// The order is deliberate and fail-closed — resolve base, resolve quote,
    /// refuse a self-pair, refuse a network mismatch, refuse a duplicate — and
    /// any refusal leaves the registry exactly as it was.
    pub fn create_market(&mut self, m: Market) -> Result<Id> {
        let base = self
            .must_resolve_enabled(m.base)
            .map_err(|e| e.at("market base side"))?
            .network;
        let quote = self
            .must_resolve_enabled(m.quote)
            .map_err(|e| e.at("market quote side"))?
            .network;
        if m.base == m.quote {
            return Err(Error::SameAsset);
        }
        if m.network != base || m.network != quote {
            return Err(Error::NetworkMismatch {
                market: m.network,
                base,
                quote,
            });
        }

        let id = m.id();
        if self.markets.contains_key(&id) {
            return Err(Error::DuplicateMarket(id));
        }
        self.markets.insert(id, m);
        Ok(id)
    }

    /// The market for an id, or nothing if it is not registered.
    pub fn resolve_market(&self, id: Id) -> Option<&Market> {
        self.markets.get(&id)
    }

    /// The admitted markets, in unspecified order.
    pub fn markets(&self) -> impl Iterator<Item = (&Id, &Market)> {
        self.markets.iter()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::asset::{AssetKind, EVM_NATIVE_MARKER};
    use crate::ids;
    use crate::registry::{Asset, ChainVerifier, RiskTier};
    use std::collections::HashMap;

    const C_CHAIN: u8 = 3;

    struct Chain {
        erc20: HashMap<Vec<u8>, u8>,
    }

    impl ChainVerifier for Chain {
        fn verify_erc20(
            &self,
            _network: u32,
            _c_chain: Id,
            address: &[u8],
        ) -> std::result::Result<u8, String> {
            self.erc20
                .get(address)
                .copied()
                .ok_or_else(|| "no code at address".to_string())
        }
        fn verify_evm_native(
            &self,
            _network: u32,
            _c_chain: Id,
        ) -> std::result::Result<u8, String> {
            Ok(18)
        }
        fn verify_utxo_asset(
            &self,
            _network: u32,
            _source_chain: Id,
            _asset: Id,
        ) -> std::result::Result<u8, String> {
            Err("no such asset on that chain".into())
        }
    }

    fn chain() -> Chain {
        let mut erc20 = HashMap::new();
        erc20.insert(vec![0xC0; 20], 6);
        erc20.insert(vec![0xC1; 20], 8);
        Chain { erc20 }
    }

    fn token(reference: u8, decimals: u8, network: u32) -> Asset {
        Asset {
            network,
            chain: ids::filled(C_CHAIN),
            kind: AssetKind::Erc20,
            reference: vec![reference; 20],
            decimals,
            symbol: "TKN".into(),
            name: "a token".into(),
            enabled: true,
            risk: RiskTier(1),
        }
    }

    fn coin(network: u32) -> Asset {
        Asset {
            network,
            chain: ids::filled(C_CHAIN),
            kind: AssetKind::EvmNative,
            reference: EVM_NATIVE_MARKER.to_vec(),
            decimals: 18,
            symbol: "LUX".into(),
            name: "Lux".into(),
            enabled: true,
            risk: RiskTier(0),
        }
    }

    /// A registry holding two real assets on network 1, and their ids.
    fn two_assets() -> (Registry, Id, Id) {
        let mut r = Registry::with_real_kinds();
        let base = r.register(coin(1), &chain()).unwrap();
        let quote = r.register(token(0xC0, 6, 1), &chain()).unwrap();
        (r, base, quote)
    }

    fn market(network: u32, base: Id, quote: Id) -> Market {
        Market {
            network,
            base,
            quote,
            venue: b"tick=1,lot=1".to_vec(),
            enabled: true,
        }
    }

    #[test]
    fn a_market_over_two_real_assets_is_admitted_under_its_derived_id() {
        let (mut r, base, quote) = two_assets();
        let m = market(1, base, quote);
        let id = r.create_market(m.clone()).unwrap();
        assert_eq!(id, m.id());
        assert_eq!(r.resolve_market(id), Some(&m));
        assert_eq!(r.markets().count(), 1);
    }

    #[test]
    fn a_market_over_an_asset_nobody_registered_cannot_be_created() {
        // The structural form of "no synthetic market": there is no AssetID for
        // a synthetic asset, so the side does not resolve.
        let (mut r, base, _) = two_assets();
        let err = r
            .create_market(market(1, base, ids::filled(0xAB)))
            .unwrap_err();
        assert!(matches!(err.root(), Error::UnknownAsset(_)), "{err}");
        assert!(err.to_string().starts_with("market quote side: "));
        assert_eq!(r.markets().count(), 0);
    }

    #[test]
    fn a_disabled_asset_admits_no_market() {
        let mut r = Registry::with_real_kinds();
        let base = r.register(coin(1), &chain()).unwrap();
        let mut off = token(0xC0, 6, 1);
        off.enabled = false;
        let quote = r.register(off, &chain()).unwrap();
        let err = r.create_market(market(1, base, quote)).unwrap_err();
        assert!(matches!(err.root(), Error::AssetDisabled(_)), "{err}");
    }

    #[test]
    fn a_market_names_two_assets_not_one() {
        let (mut r, base, _) = two_assets();
        assert_eq!(
            r.create_market(market(1, base, base)).unwrap_err(),
            Error::SameAsset
        );
    }

    #[test]
    fn a_market_does_not_span_networks() {
        let mut r = Registry::with_real_kinds();
        let base = r.register(coin(1), &chain()).unwrap();
        let quote = r.register(token(0xC1, 8, 2), &chain()).unwrap();
        assert!(matches!(
            r.create_market(market(1, base, quote)),
            Err(Error::NetworkMismatch {
                market: 1,
                base: 1,
                quote: 2
            })
        ));
    }

    #[test]
    fn one_market_cannot_be_admitted_twice() {
        let (mut r, base, quote) = two_assets();
        r.create_market(market(1, base, quote)).unwrap();
        assert!(matches!(
            r.create_market(market(1, base, quote)),
            Err(Error::DuplicateMarket(_))
        ));
        assert_eq!(r.markets().count(), 1);
    }

    #[test]
    fn the_reverse_pair_and_another_venue_are_other_markets() {
        let (mut r, base, quote) = two_assets();
        let forward = r.create_market(market(1, base, quote)).unwrap();
        let reverse = r.create_market(market(1, quote, base)).unwrap();
        let other_venue = r
            .create_market(Market {
                venue: b"tick=2,lot=1".to_vec(),
                ..market(1, base, quote)
            })
            .unwrap();
        assert_ne!(forward, reverse);
        assert_ne!(forward, other_venue);
        assert_eq!(r.markets().count(), 3);
    }
}
