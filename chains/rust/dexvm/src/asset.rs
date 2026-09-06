// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The D-Chain's identity primitive: what an asset IS, and how its 32-byte name
//! is derived from where it really lives.
//!
//! One property is structural rather than incidental: every asset the DEX can
//! credit or debit corresponds to a REAL object on-chain — an ERC-20 contract on
//! the C-Chain, the C-Chain native coin, or a UTXO asset on a UTXO chain. There
//! is no synthetic class, no ASCII-ticker identity and no declared-but-unbacked
//! credit. An asset's identity is a hash of where it lives, so two parties given
//! the same chain derive the same AssetID and a fabricated asset has no
//! preimage.
//!
//! The fold is length-prefixed and domain-separated, and both halves are
//! load-bearing. The length prefix makes it injective: `(network=1, ref=0x02)`
//! and `(network=0x0102, ref=)` cannot produce one byte stream. The kind tag
//! makes the three classes disjoint: an ERC-20 whose 20-byte address equals the
//! low bytes of a UTXO assetID cannot collide.
//!
//! These bytes are consensus. `tests/golden.rs` holds the values the Go
//! reference printed, and a change here that moves them is a fork.

use std::fmt;
use std::str::FromStr;

use crate::error::{Error, Result};
use crate::ids::{self, Id};

/// The CLOSED set of asset classes the DEX admits. There are exactly three.
/// There is no synthetic, D-native or declared class — adding one is a breaking
/// change to the wire identity and is intentionally hard.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord)]
#[repr(u8)]
pub enum AssetKind {
    /// The zero value, never admissible: a zero-initialised record fails closed.
    Invalid = 0,
    /// The C-Chain native coin. Its reference is the fixed marker.
    EvmNative = 1,
    /// A token deployed on the C-Chain. Its reference is the 20-byte contract
    /// address.
    Erc20 = 2,
    /// An asset native to a UTXO chain. Its reference is the 32-byte
    /// source-chain assetID.
    Utxo = 3,
}

impl AssetKind {
    /// Whether this is one of the three admissible kinds.
    pub fn valid(self) -> bool {
        !matches!(self, AssetKind::Invalid)
    }

    /// The byte the fold domain-separates on.
    pub fn tag(self) -> u8 {
        self as u8
    }
}

/// The canonical wire and JSON token. These exact strings appear in manifests
/// and in the allowed-kinds policy; they are the contract, not cosmetics.
impl fmt::Display for AssetKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            AssetKind::EvmNative => "EVM_NATIVE",
            AssetKind::Erc20 => "ERC20",
            AssetKind::Utxo => "UTXO",
            AssetKind::Invalid => "INVALID",
        })
    }
}

/// A kind from its canonical token, never from a bare integer. An unknown token
/// — including an ASCII ticker masquerading as a kind — fails closed.
impl FromStr for AssetKind {
    type Err = Error;

    fn from_str(s: &str) -> Result<AssetKind> {
        match s.trim() {
            "EVM_NATIVE" => Ok(AssetKind::EvmNative),
            "ERC20" => Ok(AssetKind::Erc20),
            "UTXO" => Ok(AssetKind::Utxo),
            // The token is matched trimmed and REPORTED untrimmed, which is the
            // reference's pairing: a caller who wrote a stray space needs to see
            // the space in the refusal to know what it wrote.
            _ => Err(Error::UnknownKind(s.to_string())),
        }
    }
}

/// The fixed reference for the C-Chain native coin: 20 zero bytes, the EVM's own
/// sentinel for "the native coin". EVM_NATIVE has no contract address, so
/// folding a fixed kind-tagged marker means every party derives the same native
/// AssetID for a given `(network, C-chain)` and nobody can invent a second
/// native asset.
pub const EVM_NATIVE_MARKER: [u8; 20] = [0u8; 20];

/// Domain-separation tags, folded as the FIRST field of every preimage so the
/// two identity spaces are disjoint. Versioned so a future migration can
/// re-domain without ambiguity.
const DOMAIN_ASSET_V1: &[u8] = b"lux:dex:asset:v1";
const DOMAIN_MARKET_V1: &[u8] = b"lux:dex:market:v1";

/// The canonical on-chain reference for a `(kind, ref)` pair. This is the SINGLE
/// place that decides what a well-formed reference looks like per kind —
/// derivation and registration both call it, so the shape rule lives in exactly
/// one spot.
///
/// - `EVM_NATIVE` — the marker (20 zero bytes) and nothing else, so no caller
///   can smuggle another reference into the native kind.
/// - `ERC20` — a 20-byte address, and not the zero address: that is the native
///   marker, never a token.
/// - `UTXO` — a 32-byte, non-zero source-chain assetID.
pub fn canonical_ref_for(kind: AssetKind, reference: &[u8]) -> Result<Vec<u8>> {
    let bad = |detail: String| Err(Error::BadRef(detail));
    match kind {
        AssetKind::EvmNative => {
            if reference.len() != 20 {
                return bad(format!(
                    "EVM_NATIVE marker must be 20 bytes, got {}",
                    reference.len()
                ));
            }
            if !all_zero(reference) {
                return bad("EVM_NATIVE reference must be the native marker (address zero)".into());
            }
            // Normalised to the canonical marker, so a caller cannot pass a
            // distinct all-zero-but-differently-typed slice.
            Ok(EVM_NATIVE_MARKER.to_vec())
        }
        AssetKind::Erc20 => {
            if reference.len() != 20 {
                return bad(format!(
                    "ERC20 token address must be 20 bytes, got {}",
                    reference.len()
                ));
            }
            if all_zero(reference) {
                return bad("ERC20 token address must not be the zero address".into());
            }
            Ok(reference.to_vec())
        }
        AssetKind::Utxo => Ok(utxo_asset_id(reference)?.to_vec()),
        AssetKind::Invalid => Err(Error::InvalidKind),
    }
}

/// The source-chain assetID a UTXO reference names.
///
/// It is the same rule as the `UTXO` arm above, said once and returning the id
/// rather than the bytes, because the chain lookup needs the id. The reference
/// copies a short reference into a 32-byte array, which zero-pads it into a
/// DIFFERENT asset; this refuses it, and the refusal is the same one the shape
/// check makes.
pub fn utxo_asset_id(reference: &[u8]) -> Result<Id> {
    match ids::from_slice(reference) {
        None => Err(Error::BadRef(format!(
            "UTXO assetID must be 32 bytes, got {}",
            reference.len()
        ))),
        Some(id) if ids::is_empty(&id) => {
            Err(Error::BadRef("UTXO assetID must not be empty".into()))
        }
        Some(id) => Ok(id),
    }
}

/// The canonical 32-byte identity of a real on-chain asset: a length-prefixed
/// SHA-256 fold over, in order,
///
/// ```text
/// "lux:dex:asset:v1" | networkID | sourceChainID | kind | canonicalRef
/// ```
///
/// `source_chain` is the C-Chain id for `EVM_NATIVE`/`ERC20` and the UTXO source
/// chain id for `UTXO`. The result lives in the same identity space the on-chain
/// atomic objects already use, so a registered AssetID is directly comparable to
/// the asset a real cross-chain object carries. It is never a string ticker.
///
/// The empty source chain is refused BEFORE the reference is looked at, which is
/// the reference's order: an asset that names no chain has not named a bad
/// reference, it has named nowhere.
pub fn derive_asset_id(
    network: u32,
    source_chain: Id,
    kind: AssetKind,
    reference: &[u8],
) -> Result<Id> {
    if ids::is_empty(&source_chain) {
        return Err(Error::EmptyChainId);
    }
    let canonical = canonical_ref_for(kind, reference)?;

    let mut f = Folder::default();
    f.tag(DOMAIN_ASSET_V1);
    f.u32(network);
    f.bytes(&source_chain);
    f.u8(kind.tag());
    f.bytes(&canonical);
    Ok(f.sum())
}

/// A market's identity, from its two asset identities and the venue
/// configuration:
///
/// ```text
/// "lux:dex:market:v1" | networkID | baseAssetID | quoteAssetID | venueConfig
/// ```
///
/// Both sides are themselves canonical AssetIDs, so a market is pinned to real
/// assets by construction: there is no AssetID for a synthetic asset, so there
/// is no market name over one. `venue` is the canonical serialization of the
/// parameters — tick, lot, fee tier — that distinguish two venues on one pair.
///
/// The fold is ordered, not symmetric: base and quote enter at different
/// positions, so a pair and its reverse are two markets. A symmetric fold would
/// name one market for both sides of a book.
pub fn market_id(network: u32, base: Id, quote: Id, venue: &[u8]) -> Id {
    let mut f = Folder::default();
    f.tag(DOMAIN_MARKET_V1);
    f.u32(network);
    f.bytes(&base);
    f.bytes(&quote);
    f.bytes(venue);
    f.sum()
}

/// A length-prefixed, domain-separated preimage, folded with SHA-256.
///
/// Every field is a big-endian `u64` length followed by its bytes. That is the
/// whole injectivity argument, so there is exactly one writer of it.
#[derive(Default, Clone, Debug)]
pub struct Folder {
    buf: Vec<u8>,
}

impl Folder {
    pub fn raw(&mut self, b: &[u8]) {
        self.buf.extend_from_slice(&(b.len() as u64).to_be_bytes());
        self.buf.extend_from_slice(b);
    }
    /// A domain-separation constant. The same encoding as a field; named for
    /// intent.
    pub fn tag(&mut self, b: &[u8]) {
        self.raw(b)
    }
    /// A variable-length field.
    pub fn bytes(&mut self, b: &[u8]) {
        self.raw(b)
    }
    /// A single-byte field — the kind tag.
    pub fn u8(&mut self, v: u8) {
        self.raw(&[v])
    }
    /// A 32-bit field — the network id — big-endian.
    pub fn u32(&mut self, v: u32) {
        self.raw(&v.to_be_bytes())
    }

    pub fn sum(&self) -> Id {
        ids::sha256(&self.buf)
    }

    /// The accumulated preimage. Exposed because a format is only pinned by a
    /// test that can state it.
    pub fn preimage(&self) -> &[u8] {
        &self.buf
    }
}

fn all_zero(b: &[u8]) -> bool {
    b.iter().all(|&x| x == 0)
}

#[cfg(test)]
mod tests {
    use super::*;

    const C_CHAIN: u8 = 3;
    const X_CHAIN: u8 = 2;

    fn erc20_ref() -> Vec<u8> {
        vec![0xC0; 20]
    }

    #[test]
    fn a_kind_round_trips_through_its_canonical_token() {
        for k in [AssetKind::EvmNative, AssetKind::Erc20, AssetKind::Utxo] {
            assert_eq!(k.to_string().parse::<AssetKind>().unwrap(), k);
            assert!(k.valid());
        }
        assert!(!AssetKind::Invalid.valid());
        assert_eq!(AssetKind::Invalid.to_string(), "INVALID");
    }

    #[test]
    fn a_ticker_is_not_a_kind() {
        // The anti-pattern this package exists to make unrepresentable: an
        // ASCII ticker where an identity belongs.
        assert_eq!(
            "LUX".parse::<AssetKind>().unwrap_err(),
            Error::UnknownKind("LUX".into())
        );
        assert_eq!(
            "".parse::<AssetKind>().unwrap_err(),
            Error::UnknownKind("".into())
        );
        assert_eq!(
            "INVALID".parse::<AssetKind>().unwrap_err(),
            Error::UnknownKind("INVALID".into())
        );
        // Matched trimmed, reported untrimmed. A caller who wrote a stray space
        // is told what it wrote.
        assert_eq!(" ERC20 ".parse::<AssetKind>().unwrap(), AssetKind::Erc20);
        assert_eq!(
            " LUX ".parse::<AssetKind>().unwrap_err(),
            Error::UnknownKind(" LUX ".into())
        );
    }

    #[test]
    fn the_native_coin_has_exactly_one_reference() {
        let native = canonical_ref_for(AssetKind::EvmNative, &EVM_NATIVE_MARKER).unwrap();
        assert_eq!(native, EVM_NATIVE_MARKER.to_vec());
        // A token cannot masquerade as the coin.
        assert!(matches!(
            canonical_ref_for(AssetKind::EvmNative, &erc20_ref()),
            Err(Error::BadRef(_))
        ));
        assert!(matches!(
            canonical_ref_for(AssetKind::EvmNative, &[0u8; 19]),
            Err(Error::BadRef(_))
        ));
    }

    #[test]
    fn a_token_is_twenty_bytes_and_not_the_native_marker() {
        canonical_ref_for(AssetKind::Erc20, &erc20_ref()).unwrap();
        assert!(matches!(
            canonical_ref_for(AssetKind::Erc20, &[0xC0; 19]),
            Err(Error::BadRef(_))
        ));
        assert!(matches!(
            canonical_ref_for(AssetKind::Erc20, &EVM_NATIVE_MARKER),
            Err(Error::BadRef(_))
        ));
    }

    #[test]
    fn a_utxo_asset_is_a_thirty_two_byte_non_zero_id() {
        canonical_ref_for(AssetKind::Utxo, &ids::filled(0x50)).unwrap();
        assert!(matches!(
            canonical_ref_for(AssetKind::Utxo, &[0x50; 31]),
            Err(Error::BadRef(_))
        ));
        assert!(matches!(
            canonical_ref_for(AssetKind::Utxo, &ids::EMPTY),
            Err(Error::BadRef(_))
        ));
    }

    #[test]
    fn an_asset_that_names_no_chain_is_refused_before_its_reference_is_read() {
        // Both wrong: no chain AND a 19-byte token address. The chain is the
        // answer, because that is the order the reference decides in — and a
        // port that checked the reference first would report the second fault
        // for a caller that made two.
        assert_eq!(
            derive_asset_id(1, ids::EMPTY, AssetKind::Erc20, &[0xC0; 19]).unwrap_err(),
            Error::EmptyChainId
        );
    }

    #[test]
    fn every_field_of_an_asset_identity_moves_it() {
        let base =
            derive_asset_id(1, ids::filled(C_CHAIN), AssetKind::Erc20, &erc20_ref()).unwrap();
        let others = [
            derive_asset_id(2, ids::filled(C_CHAIN), AssetKind::Erc20, &erc20_ref()).unwrap(),
            derive_asset_id(1, ids::filled(C_CHAIN + 1), AssetKind::Erc20, &erc20_ref()).unwrap(),
            derive_asset_id(1, ids::filled(C_CHAIN), AssetKind::Erc20, &[0xC1; 20]).unwrap(),
            derive_asset_id(
                1,
                ids::filled(C_CHAIN),
                AssetKind::EvmNative,
                &EVM_NATIVE_MARKER,
            )
            .unwrap(),
            derive_asset_id(1, ids::filled(X_CHAIN), AssetKind::Utxo, &ids::filled(0x50)).unwrap(),
        ];
        for other in others {
            assert_ne!(
                base, other,
                "an identity that ignored a field would let one registration name two assets"
            );
        }
    }

    #[test]
    fn the_kind_tag_separates_two_references_that_are_otherwise_alike() {
        // The ERC-20 address 0x0000…01 and the UTXO assetID 0x00…01 agree on
        // every byte they share. Only the kind tag and the length prefix keep
        // them apart.
        let mut token = [0u8; 20];
        token[19] = 1;
        let mut asset = ids::EMPTY;
        asset[31] = 1;
        let a = derive_asset_id(1, ids::filled(C_CHAIN), AssetKind::Erc20, &token).unwrap();
        let b = derive_asset_id(1, ids::filled(C_CHAIN), AssetKind::Utxo, &asset).unwrap();
        assert_ne!(a, b);
    }

    #[test]
    fn a_market_is_not_symmetric_in_its_two_sides() {
        let base = ids::filled(0x10);
        let quote = ids::filled(0x11);
        let venue = b"tick=1,lot=1";
        assert_ne!(
            market_id(1, base, quote, venue),
            market_id(1, quote, base, venue)
        );
        assert_ne!(
            market_id(1, base, quote, venue),
            market_id(1, base, quote, b"tick=2,lot=1")
        );
        assert_ne!(
            market_id(1, base, quote, venue),
            market_id(2, base, quote, venue)
        );
    }

    #[test]
    fn the_fold_is_injective_across_a_field_boundary() {
        // Without the length prefix these two would share a preimage.
        let mut one = Folder::default();
        one.bytes(&[0x01]);
        one.bytes(&[0x02]);
        let mut two = Folder::default();
        two.bytes(&[0x01, 0x02]);
        two.bytes(&[]);
        assert_ne!(one.preimage(), two.preimage());
        assert_ne!(one.sum(), two.sum());
    }

    #[test]
    fn a_field_is_its_length_then_its_bytes() {
        let mut f = Folder::default();
        f.u32(1);
        assert_eq!(f.preimage(), &[0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 1]);
        let mut g = Folder::default();
        g.u8(3);
        assert_eq!(g.preimage(), &[0, 0, 0, 0, 0, 0, 0, 1, 3]);
    }
}
