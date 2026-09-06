// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The admitted set, and the one door an asset enters through.
//!
//! There is no path that admits an unverified asset. [`Registry::register`] is
//! the only writer, and it proves three things before it writes: the record is
//! well formed, its kind is in the active policy, and the thing it describes is
//! REAL on its target network. A synthetic asset has nothing for the verifier to
//! find, so it never gets an id, so no market can name it.
//!
//! The reference guards its map with a mutex because a `*Registry` is shared by
//! every caller. Here the exclusive borrow is the same guarantee, checked by the
//! compiler instead of at run time: `register` takes `&mut self` and `resolve`
//! takes `&self`, and a caller sharing one across threads wraps it once at the
//! edge rather than paying for a lock on every read.

use std::collections::{BTreeSet, HashMap};

use crate::asset::{self, AssetKind};
use crate::error::{Error, Result};
use crate::ids::{self, Id};
use crate::market::Market;

/// An operator-assigned risk classification. It is metadata: it does not gate
/// admissibility — only reality does — and it exists so the venue can set
/// conservative caps on newer assets. Tier 0 is the safest.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord)]
pub struct RiskTier(pub u8);

impl RiskTier {
    /// The defined range: 0 (primary-network native and canonical stables)
    /// through 3 (experimental, tightest caps).
    pub const HIGHEST: u8 = 3;

    pub fn valid(self) -> bool {
        self.0 <= Self::HIGHEST
    }
}

/// A single registered, real, on-chain asset.
///
/// Its AssetID is DERIVED from the canonical fields — never supplied — so the
/// identity and the description cannot disagree.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Asset {
    /// The Lux network this asset lives on.
    pub network: u32,
    /// The SOURCE chain: the C-Chain for `EVM_NATIVE`/`ERC20`, the UTXO source
    /// chain for `UTXO`.
    pub chain: Id,
    pub kind: AssetKind,
    /// The on-chain reference: the 20-byte token address, the 20-byte native
    /// marker, or the 32-byte UTXO assetID.
    pub reference: Vec<u8>,
    /// The asset's on-chain decimal precision, checked against what the chain
    /// reports.
    pub decimals: u8,
    /// Display metadata. NOT identity — the AssetID does not hash them — and not
    /// a ticker id: an asset is keyed by AssetID, never by symbol.
    pub symbol: String,
    pub name: String,
    /// Whether this asset, and markets over it, may trade. A disabled asset
    /// stays registered and auditable; it simply admits no markets.
    pub enabled: bool,
    pub risk: RiskTier,
}

impl Asset {
    /// The canonical AssetID for this record, derived from its real fields.
    pub fn id(&self) -> Result<Id> {
        asset::derive_asset_id(self.network, self.chain, self.kind, &self.reference)
    }

    /// Whether the record is internally well formed, BEFORE any chain read: a
    /// valid kind, a reference of the right shape, a tier in range, a source
    /// chain, and a symbol that is a label rather than an identity.
    ///
    /// A record that fails this can never be real, so it is refused early and
    /// cheaply. It does NOT check reality — that is [`Asset::verify_on_chain`].
    pub fn validate_shape(&self) -> Result<()> {
        if !self.kind.valid() {
            return Err(Error::InvalidKind);
        }
        if !self.risk.valid() {
            return Err(Error::RiskTierOutOfRange(self.risk.0));
        }
        asset::canonical_ref_for(self.kind, &self.reference)?;
        if ids::is_empty(&self.chain) {
            return Err(Error::EmptyChainId);
        }
        if looks_like_ticker_id(&self.symbol) {
            return Err(Error::TickerSymbol(self.symbol.clone()));
        }
        Ok(())
    }

    /// Whether the asset is real, against live chain state, and whether its
    /// declared decimals are the ones the chain reports.
    ///
    /// This is the gate that makes a synthetic asset unrepresentable rather than
    /// merely discouraged: there is nothing on any chain for the verifier to
    /// find, so it answers no and the asset is never admitted.
    pub fn verify_on_chain(&self, chain: &dyn ChainVerifier) -> Result<()> {
        self.validate_shape()?;
        let found = match self.kind {
            AssetKind::Erc20 => chain.verify_erc20(self.network, self.chain, &self.reference),
            AssetKind::EvmNative => chain.verify_evm_native(self.network, self.chain),
            AssetKind::Utxo => chain.verify_utxo_asset(
                self.network,
                self.chain,
                asset::utxo_asset_id(&self.reference)?,
            ),
            AssetKind::Invalid => return Err(Error::InvalidKind),
        };
        let on_chain = found.map_err(|why| Error::NotReal {
            kind: self.kind,
            network: self.network,
            why,
        })?;
        if on_chain != self.decimals {
            return Err(Error::DecimalsMismatch {
                declared: self.decimals,
                on_chain,
                kind: self.kind,
            });
        }
        Ok(())
    }
}

/// Proof that an asset is real, read from live chain state.
///
/// It is injected so that the admission rule is identical whether it runs
/// offline against a target network's RPC or at node start against local state.
/// Each call answers with the asset's on-chain decimals, or with why the chain
/// has nothing there.
pub trait ChainVerifier {
    /// A contract exists at `address` on the given C-Chain of the given network,
    /// and these are its `decimals()`.
    fn verify_erc20(
        &self,
        network: u32,
        c_chain: Id,
        address: &[u8],
    ) -> std::result::Result<u8, String>;

    /// The C-Chain is the expected native chain for the network, and these are
    /// the native decimals.
    fn verify_evm_native(&self, network: u32, c_chain: Id) -> std::result::Result<u8, String>;

    /// The assetID exists on the given source chain of the given network, and
    /// this is its denomination.
    fn verify_utxo_asset(
        &self,
        network: u32,
        source_chain: Id,
        asset: Id,
    ) -> std::result::Result<u8, String>;
}

/// The set of admitted real assets, keyed by canonical AssetID, and the markets
/// pinned to them. It is the authority every admission decision consults.
#[derive(Clone, Debug, Default)]
pub struct Registry {
    /// The active allowed-kinds policy. An asset whose kind is absent is refused
    /// at registration even if it is real — the policy is a second, narrower
    /// gate on top of reality. It is fixed at construction, because a policy
    /// that could be widened after the set was audited would not be one.
    allowed: BTreeSet<AssetKind>,
    assets: HashMap<Id, Asset>,
    pub(crate) markets: HashMap<Id, Market>,
}

impl Registry {
    /// An empty registry permitting the given kinds. With none it admits
    /// nothing, which is the fail-closed direction. The canonical production
    /// policy is all three.
    pub fn new(allowed: impl IntoIterator<Item = AssetKind>) -> Registry {
        Registry {
            allowed: allowed.into_iter().filter(|k| k.valid()).collect(),
            assets: HashMap::new(),
            markets: HashMap::new(),
        }
    }

    /// The canonical policy: the three real kinds and nothing else.
    pub fn with_real_kinds() -> Registry {
        Registry::new([AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo])
    }

    /// Whether the active policy admits this kind.
    pub fn allows_kind(&self, kind: AssetKind) -> bool {
        self.allowed.contains(&kind)
    }

    /// Admit one asset, after proving it is well formed, of an allowed kind, and
    /// real on its target network. The derived AssetID is returned so a caller
    /// can pin markets to it.
    pub fn register(&mut self, a: Asset, chain: &dyn ChainVerifier) -> Result<Id> {
        a.validate_shape()?;
        if !self.allows_kind(a.kind) {
            return Err(Error::KindNotAllowed(a.kind));
        }
        a.verify_on_chain(chain)?;
        let id = a.id()?;
        if self.assets.contains_key(&id) {
            return Err(Error::DuplicateAsset(id));
        }
        self.assets.insert(id, a);
        Ok(id)
    }

    /// The registered asset for an id, or nothing for any unregistered — which
    /// is to say synthetic — one. This is the predicate the market gate and the
    /// boot gate refuse on.
    pub fn resolve(&self, id: Id) -> Option<&Asset> {
        self.assets.get(&id)
    }

    /// The asset for an id, refusing one that is unknown or disabled. The strict
    /// resolver the market gate uses.
    pub fn must_resolve_enabled(&self, id: Id) -> Result<&Asset> {
        match self.assets.get(&id) {
            None => Err(Error::UnknownAsset(id)),
            Some(a) if !a.enabled => Err(Error::AssetDisabled(id)),
            Some(a) => Ok(a),
        }
    }

    /// How many assets are registered.
    pub fn len(&self) -> usize {
        self.assets.len()
    }

    pub fn is_empty(&self) -> bool {
        self.assets.is_empty()
    }

    /// The registered assets, in unspecified order.
    pub fn assets(&self) -> impl Iterator<Item = (&Id, &Asset)> {
        self.assets.iter()
    }
}

/// Whether a string is being used as an asset IDENTITY rather than as a display
/// symbol.
///
/// An AssetID is a 32-byte hash; a bare pair like `LUX/USDC` used where an id
/// belongs is the anti-pattern. A symbol may perfectly well BE `LUX` — what is
/// refused is a symbol that names two assets, because a per-asset label never
/// does.
pub fn looks_like_ticker_id(s: &str) -> bool {
    const SEPARATORS: [char; 5] = ['/', '-', ':', '@', '_'];
    if s.is_empty() {
        return false;
    }
    s.contains(SEPARATORS) && is_upper_tickerish(s)
}

/// Whether `s` is drawn only from the alphabet of a ticker — `A-Z`, `0-9` and
/// the market separators — with at least one letter. A human name like
/// `USD Coin` has a space and lowercase and is not tickerish; a pair id like
/// `LUX/USDC` is.
fn is_upper_tickerish(s: &str) -> bool {
    let mut letter = false;
    for c in s.chars() {
        match c {
            'A'..='Z' => letter = true,
            '0'..='9' | '/' | '-' | ':' | '@' | '_' => {}
            _ => return false,
        }
    }
    letter
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::asset::EVM_NATIVE_MARKER;

    const C_CHAIN: u8 = 3;
    const X_CHAIN: u8 = 2;

    /// A chain that holds exactly what it was told to hold. The refusal paths
    /// are exercised for real rather than stubbed to always-true: what is not in
    /// here is not on any chain.
    #[derive(Default)]
    struct Chain {
        erc20: HashMap<Vec<u8>, u8>,
        native: HashMap<Id, u8>,
        utxo: HashMap<Id, u8>,
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
        fn verify_evm_native(&self, _network: u32, c_chain: Id) -> std::result::Result<u8, String> {
            self.native
                .get(&c_chain)
                .copied()
                .ok_or_else(|| "not the native chain for this network".to_string())
        }
        fn verify_utxo_asset(
            &self,
            _network: u32,
            _source_chain: Id,
            asset: Id,
        ) -> std::result::Result<u8, String> {
            self.utxo
                .get(&asset)
                .copied()
                .ok_or_else(|| "no such asset on that chain".to_string())
        }
    }

    fn real_chain() -> Chain {
        let mut c = Chain::default();
        c.erc20.insert(vec![0xC0; 20], 6);
        c.native.insert(ids::filled(C_CHAIN), 18);
        c.utxo.insert(ids::filled(0x50), 9);
        c
    }

    fn token() -> Asset {
        Asset {
            network: 1,
            chain: ids::filled(C_CHAIN),
            kind: AssetKind::Erc20,
            reference: vec![0xC0; 20],
            decimals: 6,
            symbol: "USDC".into(),
            name: "USD Coin".into(),
            enabled: true,
            risk: RiskTier(1),
        }
    }

    fn coin() -> Asset {
        Asset {
            network: 1,
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

    #[test]
    fn a_real_asset_is_admitted_and_keyed_by_its_derived_id() {
        let mut r = Registry::with_real_kinds();
        let id = r.register(token(), &real_chain()).unwrap();
        assert_eq!(id, token().id().unwrap());
        assert_eq!(r.resolve(id), Some(&token()));
        assert_eq!(r.len(), 1);
    }

    #[test]
    fn an_asset_no_chain_holds_is_never_admitted() {
        let mut r = Registry::with_real_kinds();
        let mut fabricated = token();
        fabricated.reference = vec![0xBE; 20];
        let err = r.register(fabricated, &real_chain()).unwrap_err();
        assert!(matches!(err, Error::NotReal { .. }), "{err}");
        assert!(r.is_empty());
    }

    #[test]
    fn a_declared_decimal_that_the_chain_does_not_report_is_refused() {
        let mut r = Registry::with_real_kinds();
        let mut lying = token();
        lying.decimals = 18;
        assert!(matches!(
            r.register(lying, &real_chain()),
            Err(Error::DecimalsMismatch { .. })
        ));
    }

    #[test]
    fn a_real_asset_of_a_kind_the_policy_excludes_is_still_refused() {
        // The policy is a second, narrower gate on top of reality: the token IS
        // on the chain, and the registry still says no.
        let mut r = Registry::new([AssetKind::EvmNative]);
        assert!(matches!(
            r.register(token(), &real_chain()),
            Err(Error::KindNotAllowed(AssetKind::Erc20))
        ));
        r.register(coin(), &real_chain()).unwrap();
    }

    #[test]
    fn an_empty_policy_admits_nothing() {
        let mut r = Registry::new([]);
        assert!(!r.allows_kind(AssetKind::Erc20));
        assert!(r.register(token(), &real_chain()).is_err());
    }

    #[test]
    fn one_asset_cannot_be_registered_twice() {
        let mut r = Registry::with_real_kinds();
        r.register(token(), &real_chain()).unwrap();
        assert!(matches!(
            r.register(token(), &real_chain()),
            Err(Error::DuplicateAsset(_))
        ));
        assert_eq!(r.len(), 1);
    }

    #[test]
    fn a_malformed_record_is_refused_before_any_chain_is_read() {
        // The verifier would answer for this address; the shape check never
        // gets there. A verifier that is never called is the point: a record
        // that cannot be real is refused cheaply.
        let mut r = Registry::with_real_kinds();
        let mut short = token();
        short.reference = vec![0xC0; 19];
        assert!(matches!(
            r.register(short, &Chain::default()),
            Err(Error::BadRef(_))
        ));
    }

    #[test]
    fn a_disabled_asset_resolves_and_does_not_admit() {
        let mut r = Registry::with_real_kinds();
        let mut off = token();
        off.enabled = false;
        let id = r.register(off, &real_chain()).unwrap();
        assert!(r.resolve(id).is_some(), "it stays registered and auditable");
        assert!(matches!(
            r.must_resolve_enabled(id),
            Err(Error::AssetDisabled(_))
        ));
    }

    #[test]
    fn an_unregistered_id_is_the_structural_form_of_synthetic() {
        let r = Registry::with_real_kinds();
        assert_eq!(r.resolve(ids::filled(0xAB)), None);
        assert!(matches!(
            r.must_resolve_enabled(ids::filled(0xAB)),
            Err(Error::UnknownAsset(_))
        ));
    }

    #[test]
    fn every_kind_is_admitted_through_the_same_door() {
        let mut r = Registry::with_real_kinds();
        let utxo = Asset {
            network: 1,
            chain: ids::filled(X_CHAIN),
            kind: AssetKind::Utxo,
            reference: ids::filled(0x50).to_vec(),
            decimals: 9,
            symbol: "AVAX".into(),
            name: "an X-Chain asset".into(),
            enabled: true,
            risk: RiskTier(2),
        };
        for a in [token(), coin(), utxo] {
            r.register(a, &real_chain()).unwrap();
        }
        assert_eq!(r.len(), 3);
        assert_eq!(r.assets().count(), 3);
    }

    #[test]
    fn a_tier_outside_the_defined_range_is_refused() {
        let mut r = Registry::with_real_kinds();
        let mut wild = token();
        wild.risk = RiskTier(4);
        assert!(matches!(
            r.register(wild, &real_chain()),
            Err(Error::RiskTierOutOfRange(4))
        ));
    }

    #[test]
    fn a_symbol_may_be_a_ticker_and_may_not_be_a_pair() {
        // The symbol field is display only. A symbol that names two assets is
        // an id wearing a label's clothes.
        assert!(!looks_like_ticker_id("LUX"));
        assert!(!looks_like_ticker_id("USD Coin"));
        assert!(!looks_like_ticker_id(""));
        assert!(!looks_like_ticker_id("Wrapped-Ether"));
        assert!(looks_like_ticker_id("LUX/USDC"));
        assert!(looks_like_ticker_id("LUX-USDC"));
        assert!(looks_like_ticker_id("LUX-USDC@VENUE"));

        let mut r = Registry::with_real_kinds();
        let mut pair = token();
        pair.symbol = "LUX/USDC".into();
        assert!(matches!(
            r.register(pair, &real_chain()),
            Err(Error::TickerSymbol(_))
        ));
    }
}
