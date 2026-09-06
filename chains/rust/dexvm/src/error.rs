// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What this chain refuses for, and how a refusal is identified.
//!
//! A refusal has two halves and they answer different questions. Its IDENTITY
//! is what Go's callers ask `errors.Is` about and branch on; its MESSAGE is
//! what a human reads. The reference builds the second from the first —
//! `fmt.Errorf("%w: ERC20 token address must be 20 bytes, got 19", ErrBadRef)`
//! — and anything reading a refusal for what it MEANS unwraps back to the
//! sentinel first.
//!
//! Keeping the two apart is load-bearing rather than tidy. The wrapped text of
//! that refusal ends "UTXO assetID must be 32 bytes"; a reader that classified
//! the sentence would see the word `utxo` and call a malformed reference a
//! missing ledger entry. [`Error::sentinel`] is the unwrapped half, exactly what
//! Go's `root(err)` returns, and it is what the differential evaluator reads.
//!
//! An id inside a message renders as hex. Go renders `ids.ID` as cb58 there;
//! no decision anywhere depends on the rendering, and carrying a base58 codec
//! for the benefit of an error string would be a second encoding to keep in
//! step.

use std::fmt;

use crate::asset::AssetKind;
use crate::ids::{self, Id};

/// Why the chain said no.
///
/// Every variant names a rule. There is deliberately no `Other(String)`: a
/// catch-all is where a rule goes to stop being checked.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    // ---- what an asset IS ----
    /// The kind is not one of the three admissible classes.
    InvalidKind,
    /// A token offered where a kind belongs, including an ASCII ticker
    /// masquerading as one.
    UnknownKind(String),
    /// The on-chain reference does not have the shape its kind requires.
    BadRef(String),
    /// The asset names no source chain.
    EmptyChainId,

    // ---- what a market IS ----
    /// One asset on both sides.
    SameAsset,
    /// A market does not span networks.
    NetworkMismatch {
        market: u32,
        base: u32,
        quote: u32,
    },
    DuplicateMarket(Id),

    // ---- what may be admitted ----
    /// The structural form of "synthetic asset": no registration, so no id to
    /// resolve, so no market over it.
    UnknownAsset(Id),
    AssetDisabled(Id),
    KindNotAllowed(AssetKind),
    DuplicateAsset(Id),
    RiskTierOutOfRange(u8),
    /// A symbol being used as an identity. Assets are keyed by AssetID; a
    /// symbol is display only.
    TickerSymbol(String),
    /// The chain has nothing where the asset says it lives. This is what makes
    /// a synthetic asset unrepresentable rather than merely discouraged.
    NotReal {
        kind: AssetKind,
        network: u32,
        why: String,
    },
    DecimalsMismatch {
        declared: u8,
        on_chain: u8,
        kind: AssetKind,
    },

    // ---- under which posture value may activate ----
    UnknownMode(String),
    ValueModeUnset,
    LaunchAssertionsUnmet {
        caps_on: bool,
        real_assets_only: bool,
        halt_ready: bool,
    },

    /// Context in front, identity preserved — Go's `fmt.Errorf("ctx: %w", err)`.
    At {
        at: &'static str,
        cause: Box<Error>,
    },
}

impl Error {
    /// Wrap with context, the way the reference does: the sentence grows at the
    /// front and the identity underneath is untouched.
    pub fn at(self, at: &'static str) -> Error {
        Error::At {
            at,
            cause: Box::new(self),
        }
    }

    /// The deepest refusal under this one — Go's `root(err)`.
    pub fn root(&self) -> &Error {
        match self {
            Error::At { cause, .. } => cause.root(),
            other => other,
        }
    }

    /// What the root refusal MEANS, as the reference spells it: the sentinel's
    /// own text where `registry` exports one, and the whole message where it
    /// raises words with no identity behind them (Go's `fmt.Errorf` with no
    /// `%w`, which `root` returns unchanged).
    pub fn sentinel(&self) -> String {
        use Error::*;
        match self.root() {
            InvalidKind => "registry: asset kind is not EVM_NATIVE, ERC20 or UTXO".into(),
            BadRef(_) => "registry: canonical reference does not match asset kind".into(),
            EmptyChainId => "registry: asset source chain id is empty".into(),
            SameAsset => "registry: market base and quote assets are identical".into(),
            NetworkMismatch { .. } => {
                "registry: market network does not match asset network".into()
            }
            DuplicateMarket(_) => "registry: market already exists".into(),
            UnknownAsset(_) => "registry: asset is not registered (unknown/synthetic)".into(),
            AssetDisabled(_) => "registry: asset is registered but disabled".into(),
            KindNotAllowed(_) => "registry: asset kind not in allowed-kinds policy".into(),
            DuplicateAsset(_) => "registry: asset already registered".into(),
            ValueModeUnset => concat!(
                "registry: refuse DEX value activation — consensus mode is UNSET ",
                "(no Byzantine-finality and no labeled CFT-parity declared)"
            )
            .into(),
            LaunchAssertionsUnmet { .. } => concat!(
                "registry: refuse HONEST_VALIDATOR_LABELED value activation — ",
                "caps-on + real-assets-only + halt-ready not all asserted"
            )
            .into(),
            // The verifier's own words are what the reference wraps here, so
            // they are what is underneath: "asset ERC20 not real on network 1"
            // is context around whatever the chain said when it looked.
            NotReal { why, .. } => why.clone(),
            // The rest carry no sentinel: the reference raises them with words
            // alone, so their own text is what a classifier reads.
            bare => bare.to_string(),
        }
    }
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        use Error::*;
        match self {
            InvalidKind | EmptyChainId | SameAsset | ValueModeUnset => {
                f.write_str(&self.sentinel())
            }
            UnknownKind(token) => write!(
                f,
                "registry: unknown asset kind {token:?} (only EVM_NATIVE, ERC20, UTXO)"
            ),
            BadRef(detail) => write!(f, "{}: {detail}", self.sentinel()),
            NetworkMismatch {
                market,
                base,
                quote,
            } => write!(
                f,
                "{}: market={market} base={base} quote={quote}",
                self.sentinel()
            ),
            DuplicateMarket(id) | UnknownAsset(id) | AssetDisabled(id) | DuplicateAsset(id) => {
                write!(f, "{}: {}", self.sentinel(), ids::hex(id))
            }
            KindNotAllowed(kind) => write!(f, "{}: {kind}", self.sentinel()),
            RiskTierOutOfRange(tier) => write!(f, "registry: risk tier {tier} out of range"),
            TickerSymbol(symbol) => write!(
                f,
                "registry: symbol {symbol:?} looks like an ASCII-ticker asset id; \
                 symbols are display-only, assets are keyed by AssetID"
            ),
            NotReal { kind, network, why } => write!(
                f,
                "registry: asset {kind} not real on network {network}: {why}"
            ),
            DecimalsMismatch {
                declared,
                on_chain,
                kind,
            } => write!(
                f,
                "registry: declared decimals {declared} != on-chain decimals {on_chain} \
                 for {kind} asset"
            ),
            UnknownMode(token) => write!(
                f,
                "registry: unknown consensus mode {token:?} \
                 (only QUORUM_FINALITY or HONEST_VALIDATOR_LABELED)"
            ),
            LaunchAssertionsUnmet {
                caps_on,
                real_assets_only,
                halt_ready,
            } => write!(
                f,
                "{} (capsOn={caps_on} realAssetsOnly={real_assets_only} haltReady={halt_ready})",
                self.sentinel()
            ),
            At { at, cause } => write!(f, "{at}: {cause}"),
        }
    }
}

impl std::error::Error for Error {}

pub type Result<T> = std::result::Result<T, Error>;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_wrapped_refusal_keeps_the_identity_underneath() {
        let e = Error::BadRef("UTXO assetID must be 32 bytes, got 31".into())
            .at("market base side")
            .at("startup");
        assert_eq!(
            e.sentinel(),
            "registry: canonical reference does not match asset kind"
        );
        assert_eq!(
            *e.root(),
            Error::BadRef("UTXO assetID must be 32 bytes, got 31".into())
        );
    }

    // The reason the two halves are apart at all: the SENTENCE names a UTXO and
    // the REFUSAL is about a reference's shape. A reader that classified the
    // sentence would answer the wrong question, and it would answer it only for
    // the UTXO arm — which is how one arm of one refusal goes wrong alone.
    #[test]
    fn the_message_says_utxo_and_the_identity_does_not() {
        let e = Error::BadRef("UTXO assetID must be 32 bytes, got 31".into());
        assert!(e.to_string().to_lowercase().contains("utxo"));
        assert!(!e.sentinel().to_lowercase().contains("utxo"));
    }

    #[test]
    fn a_refusal_with_no_sentinel_is_its_own_message() {
        let e = Error::UnknownKind("LUX".into());
        assert_eq!(e.sentinel(), e.to_string());
        assert_eq!(
            e.to_string(),
            "registry: unknown asset kind \"LUX\" (only EVM_NATIVE, ERC20, UTXO)"
        );
    }

    #[test]
    fn context_reads_front_to_back() {
        let e = Error::UnknownAsset(ids::filled(7)).at("market quote side");
        assert!(e
            .to_string()
            .starts_with("market quote side: registry: asset is not registered"));
    }
}
