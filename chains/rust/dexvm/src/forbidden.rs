// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The deny half of admission.
//!
//! Reality — the [`crate::registry::ChainVerifier`] — is the positive gate.
//! These are the negative one: cheap, total predicates that recognise the
//! SHAPES of the anti-patterns the DEX must never carry, so the startup gate can
//! refuse them by name even when the token behind them is real on-chain.
//!
//! A brand is not something reality can police. A token deployed on a
//! white-label universe's chain exists just as hard as one on ours; only its
//! label says which universe it serves. That is why the label scan is here and
//! not in the verifier.

use crate::error::{Error, Result};

/// Labels that mark a synthetic or mock liquidity source — fabricated depth
/// that does not correspond to real on-chain reserves. The DEX quotes real
/// reserves only.
const MOCK_LIQUIDITY_TOKENS: &[&str] = &[
    "mock",
    "synthetic",
    "fake",
    "phantom",
    "placeholder",
    "testliquidity",
    "mockliquidity",
    "d-native", // the explicitly-forbidden "D-native asset" class
    "dnative",
];

/// Labels that name an OFF-NETWORK white-label universe a Lux-native DEX must
/// never carry assets from. The DEX serves the Lux primary network and its
/// L1s/L2s, never a foreign branded universe.
const FORBIDDEN_UNIVERSE_LABELS: &[&str] = &["liquid", "partner"];

/// Whether a chain's human label names a forbidden off-network white-label
/// universe. Case-insensitive substring match — no Lux chain label contains
/// these tokens, so the match is precise to the forbidden brands.
pub fn is_forbidden_universe_label(label: &str) -> bool {
    let l = label.to_lowercase();
    FORBIDDEN_UNIVERSE_LABELS.iter().any(|t| l.contains(t))
}

/// Whether a label names mock, synthetic or phantom liquidity.
pub fn is_mock_liquidity_ref(label: &str) -> bool {
    let l = label.to_lowercase();
    MOCK_LIQUIDITY_TOKENS.iter().any(|t| l.contains(t))
}

/// The alphabet of a ticker or a pair id: `A-Z`, `0-9` and the market
/// separators, with at least one letter. A human name like "USD Coin" has a
/// space and lowercase, so it is not tickerish; a pair id like `LUX/USDC` is.
fn is_upper_tickerish(s: &str) -> bool {
    let mut has_letter = false;
    for c in s.chars() {
        match c {
            'A'..='Z' => has_letter = true,
            '0'..='9' => {}
            '/' | '-' | ':' | '@' | '_' => {}
            _ => return false,
        }
    }
    has_letter
}

/// Whether a string is being used as an asset IDENTITY rather than a display
/// symbol.
///
/// An AssetID is 32 bytes; a bare ticker used where an id is expected is the
/// anti-pattern. A symbol may legitimately BE "LUX" — what is refused is a
/// PAIR-shaped symbol like `LUX/USDC` or `LUX-USDC@venue`, because a real
/// per-asset symbol never names two assets.
pub fn looks_like_ascii_ticker_id(s: &str) -> bool {
    if s.is_empty() {
        return false;
    }
    let has_separator = s.contains(['/', '-', ':', '@', '_']);
    has_separator && is_upper_tickerish(s)
}

/// The per-asset deny gate.
///
/// It refuses an asset whose SOURCE CHAIN is labeled a forbidden off-network
/// white-label universe, or whose symbol or name names mock liquidity, or whose
/// symbol is an ASCII-ticker id.
pub fn assert_no_forbidden_asset_refs(symbol: &str, name: &str, chain_label: &str) -> Result<()> {
    if is_forbidden_universe_label(chain_label) {
        return Err(Error::other(format!(
            "registry: asset source chain {chain_label:?} is a forbidden off-network white-label universe"
        )));
    }
    if is_mock_liquidity_ref(symbol) || is_mock_liquidity_ref(name) {
        return Err(Error::other(
            "registry: asset symbol/name names mock/synthetic/phantom liquidity",
        ));
    }
    if looks_like_ascii_ticker_id(symbol) {
        return Err(Error::other(format!(
            "registry: asset symbol {symbol:?} is an ASCII-ticker id; assets are keyed by AssetID"
        )));
    }
    Ok(())
}

// ---- hex helpers (the canonicalRef / venueConfig JSON encoding) -------------

/// `0x`-prefixed lowercase hex — how a manifest carries a reference.
pub fn to_hex(b: &[u8]) -> String {
    let mut s = String::with_capacity(b.len() * 2 + 2);
    s.push_str("0x");
    for x in b {
        s.push_str(&format!("{x:02x}"));
    }
    s
}

/// The inverse, accepting an optional `0x`/`0X` prefix. An empty body decodes
/// to no bytes.
pub fn from_hex(s: &str) -> Result<Vec<u8>> {
    let t = s.trim();
    let t = t
        .strip_prefix("0x")
        .or_else(|| t.strip_prefix("0X"))
        .unwrap_or(t);
    if t.is_empty() {
        return Ok(Vec::new());
    }
    if !t.len().is_multiple_of(2) || !t.bytes().all(|c| c.is_ascii_hexdigit()) {
        return Err(Error::other(format!(
            "registry: invalid hex reference {t:?}"
        )));
    }
    Ok((0..t.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&t[i..i + 2], 16).expect("checked hex"))
        .collect())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_white_label_brands_are_caught_in_any_case() {
        for l in [
            "Liquidity L1 universe",
            "Liquid EVM",
            "LIQUID DEX",
            "partner chain",
        ] {
            assert!(is_forbidden_universe_label(l), "{l}");
        }
        for l in ["Lux C-Chain", "Lux X-Chain (testnet)", "", "Zoo"] {
            assert!(!is_forbidden_universe_label(l), "{l}");
        }
    }

    #[test]
    fn a_pair_shaped_symbol_is_an_id_wearing_a_label() {
        for s in ["LUX/USDC", "BTC:USD", "A_B", "LUX-USDC@VENUE"] {
            assert!(looks_like_ascii_ticker_id(s), "{s}");
        }
        // A plain symbol is fine — an asset may legitimately be called LUX.
        for s in ["LUX", "USDC", "USD Coin", "", "Wrapped LUX"] {
            assert!(!looks_like_ascii_ticker_id(s), "{s}");
        }
    }

    #[test]
    fn one_lowercase_letter_takes_a_pair_id_out_of_the_alphabet() {
        // The reference's PROSE names `LUX-USDC@venue` as a caught shape; its
        // CODE does not catch it, because the alphabet is upper-case only and
        // one lowercase letter fails the whole string. Verified against the Go
        // predicate directly, not read off the comment: Go answers false here
        // and true for the all-caps form.
        //
        // The code is the contract — a port that "fixed" this to match the
        // comment would refuse a symbol the Go and C++ homes admit, which is a
        // divergence in what registers, on the value path.
        assert!(!looks_like_ascii_ticker_id("LUX-USDC@venue"));
        assert!(looks_like_ascii_ticker_id("LUX-USDC@VENUE"));
    }

    #[test]
    fn hex_round_trips_and_refuses_garbage() {
        assert_eq!(to_hex(&[0xde, 0xad]), "0xdead");
        assert_eq!(from_hex("0xdead").unwrap(), vec![0xde, 0xad]);
        assert_eq!(from_hex("DEAD").unwrap(), vec![0xde, 0xad]);
        assert_eq!(from_hex("  0X00  ").unwrap(), vec![0]);
        assert!(from_hex("").unwrap().is_empty());
        assert!(from_hex("0xzz").is_err());
        assert!(from_hex("0xabc").is_err());
    }
}
