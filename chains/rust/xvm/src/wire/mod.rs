// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The wire format of the fx primitives — the shapes the X-Chain and the
//! P-Chain both spend and both own.
//!
//! Every primitive on the wire is `[TypeKind:1][ShapeKind:1][ZAP message]`.
//! The first byte names the fx FAMILY (secp256k1, nft, property, …) and the
//! second names the SHAPE within it (a transfer output, an input, a mint
//! authority, a credential). Two bytes, two questions, answered separately —
//! where a single dense slot id used to braid "which fx?" and "which shape?"
//! into one number and let a decoder read an input as an output and get
//! plausible garbage back.
//!
//! A message is self-delimiting: its ZAP header carries its own size, so a run
//! of envelopes can be walked without a separate length list, and an envelope
//! carrying even one byte past its message is refused as non-canonical — that
//! tail would change the id a transaction hashes to without changing what it
//! means.

pub mod containers;
pub mod shapes;

use lux_zap::zap;

/// The fx family that owns a primitive. `0x00` is reserved and refused by every
/// fx-owned shape.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Hash, PartialOrd, Ord)]
#[repr(u8)]
pub enum TypeKind {
    Reserved = 0x00,
    Secp256k1 = 0x01,
    Mldsa = 0x02,
    Slhdsa = 0x03,
    Ed25519 = 0x04,
    Secp256r1 = 0x05,
    Schnorr = 0x06,
    Bls12381 = 0x07,
    /// An application fx built on secp256k1 credentials.
    Nft = 0x08,
    /// The other one.
    Property = 0x09,
    /// A byte in the family space that names no family this chain knows.
    Unknown = 0xFF,
}

impl TypeKind {
    pub fn from_u8(v: u8) -> TypeKind {
        match v {
            0x00 => TypeKind::Reserved,
            0x01 => TypeKind::Secp256k1,
            0x02 => TypeKind::Mldsa,
            0x03 => TypeKind::Slhdsa,
            0x04 => TypeKind::Ed25519,
            0x05 => TypeKind::Secp256r1,
            0x06 => TypeKind::Schnorr,
            0x07 => TypeKind::Bls12381,
            0x08 => TypeKind::Nft,
            0x09 => TypeKind::Property,
            _ => TypeKind::Unknown,
        }
    }

    pub fn as_u8(self) -> u8 {
        match self {
            TypeKind::Unknown => 0xFF,
            other => other as u8,
        }
    }
}

/// The primitive shape within a family. `0x00` is reserved.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Hash, PartialOrd, Ord)]
#[repr(u8)]
pub enum ShapeKind {
    Reserved = 0x00,
    TransferOutput = 0x01,
    TransferInput = 0x02,
    MintOutput = 0x03,
    MintInput = 0x04,
    MintOperation = 0x05,
    Credential = 0x06,
    AttestationOut = 0x07,
    AttestationIn = 0x08,
    OutputOwners = 0x09,
    Utxo = 0x0A,
    TransferableOut = 0x0B,
    TransferableIn = 0x0C,
    PchainOwner = 0x0D,
    SignedTx = 0x0E,
    LockedOutput = 0x0F,
    NftMintOutput = 0x10,
    NftTransferOutput = 0x11,
    XvmBaseTx = 0x12,
    NftMintOperation = 0x13,
    NftTransferOp = 0x14,
    OwnedOutput = 0x15,
    BurnOperation = 0x16,
    Unknown = 0xFF,
}

impl ShapeKind {
    pub fn from_u8(v: u8) -> ShapeKind {
        match v {
            0x00 => ShapeKind::Reserved,
            0x01 => ShapeKind::TransferOutput,
            0x02 => ShapeKind::TransferInput,
            0x03 => ShapeKind::MintOutput,
            0x04 => ShapeKind::MintInput,
            0x05 => ShapeKind::MintOperation,
            0x06 => ShapeKind::Credential,
            0x07 => ShapeKind::AttestationOut,
            0x08 => ShapeKind::AttestationIn,
            0x09 => ShapeKind::OutputOwners,
            0x0A => ShapeKind::Utxo,
            0x0B => ShapeKind::TransferableOut,
            0x0C => ShapeKind::TransferableIn,
            0x0D => ShapeKind::PchainOwner,
            0x0E => ShapeKind::SignedTx,
            0x0F => ShapeKind::LockedOutput,
            0x10 => ShapeKind::NftMintOutput,
            0x11 => ShapeKind::NftTransferOutput,
            0x12 => ShapeKind::XvmBaseTx,
            0x13 => ShapeKind::NftMintOperation,
            0x14 => ShapeKind::NftTransferOp,
            0x15 => ShapeKind::OwnedOutput,
            0x16 => ShapeKind::BurnOperation,
            _ => ShapeKind::Unknown,
        }
    }

    pub fn as_u8(self) -> u8 {
        match self {
            ShapeKind::Unknown => 0xFF,
            other => other as u8,
        }
    }
}

/// What an envelope can be refused for.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    /// The family byte is not the one this reader was called for.
    WrongTypeKind,
    /// The shape byte is not the one this reader was called for.
    WrongShapeKind,
    /// Shorter than the two-byte prefix, or a message that ends early.
    ShortEnvelope,
    /// Bytes past the end of the self-delimiting message. Canonical envelopes
    /// consume every byte; a tail would change the id without changing the
    /// meaning.
    TrailingBytes,
    /// The ZAP header itself is wrong.
    Zap(zap::Error),
    /// A semantic gate on the decoded values.
    Owner(&'static str),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::WrongTypeKind => {
                write!(
                    f,
                    "wire: TypeKind discriminator does not match expected fx family"
                )
            }
            Error::WrongShapeKind => write!(
                f,
                "wire: ShapeKind discriminator does not match expected primitive shape"
            ),
            Error::ShortEnvelope => {
                write!(f, "wire: envelope shorter than 2-byte discriminator prefix")
            }
            Error::TrailingBytes => write!(
                f,
                "wire: trailing bytes after zap message (non-canonical envelope)"
            ),
            Error::Zap(e) => write!(f, "{e}"),
            Error::Owner(m) => write!(f, "{m}"),
        }
    }
}

impl std::error::Error for Error {}

impl From<zap::Error> for Error {
    fn from(e: zap::Error) -> Self {
        Error::Zap(e)
    }
}

/// The discriminator prefix on every envelope.
pub const ENVELOPE_PREFIX: usize = 2;

/// The (family, shape) pair at the head of an envelope, without touching the
/// message. What a dispatcher reads before it commits to a shape.
pub fn peek_discriminator(b: &[u8]) -> Result<(TypeKind, ShapeKind), Error> {
    if b.len() < ENVELOPE_PREFIX {
        return Err(Error::ShortEnvelope);
    }
    Ok((TypeKind::from_u8(b[0]), ShapeKind::from_u8(b[1])))
}

/// Split the prefix off, handing back the message bytes behind it.
pub(crate) fn read_envelope_prefix(b: &[u8]) -> Result<(TypeKind, ShapeKind, &[u8]), Error> {
    if b.len() < ENVELOPE_PREFIX {
        return Err(Error::ShortEnvelope);
    }
    Ok((
        TypeKind::from_u8(b[0]),
        ShapeKind::from_u8(b[1]),
        &b[ENVELOPE_PREFIX..],
    ))
}

/// Take the first envelope off a packed run and hand back the rest.
///
/// The length comes from the ZAP header's own size field, which is why a run of
/// variable-width envelopes needs no separate length list. This is the ONE
/// walker: credentials on a signed transaction, the owner groups of an NFT
/// mint, anything else packed the same way.
pub fn next_envelope(blob: &[u8]) -> Result<(&[u8], &[u8]), Error> {
    if blob.len() < ENVELOPE_PREFIX {
        return Err(Error::ShortEnvelope);
    }
    let zap_start = ENVELOPE_PREFIX;
    if zap_start + zap::HEADER_SIZE > blob.len() {
        return Err(Error::ShortEnvelope);
    }
    let zap_size = u32::from_le_bytes([
        blob[zap_start + 12],
        blob[zap_start + 13],
        blob[zap_start + 14],
        blob[zap_start + 15],
    ]) as usize;
    let env_end = zap_start + zap_size;
    if zap_size < zap::HEADER_SIZE || env_end > blob.len() {
        return Err(Error::ShortEnvelope);
    }
    Ok((&blob[..env_end], &blob[env_end..]))
}

/// Put a discriminator in front of a message.
pub(crate) fn write_envelope_prefix(tk: TypeKind, sk: ShapeKind, msg: Vec<u8>) -> Vec<u8> {
    let mut out = Vec::with_capacity(ENVELOPE_PREFIX + msg.len());
    out.push(tk.as_u8());
    out.push(sk.as_u8());
    out.extend_from_slice(&msg);
    out
}

/// Parse an envelope, checking the shape and (when the shape is fx-owned) that
/// the family byte is not the reserved one.
pub(crate) fn open<'a>(
    b: &'a [u8],
    want: ShapeKind,
    fx_owned: bool,
) -> Result<(TypeKind, zap::Message<'a>), Error> {
    let (tk, sk, msg_bytes) = read_envelope_prefix(b)?;
    if sk != want {
        return Err(Error::WrongShapeKind);
    }
    if fx_owned && tk == TypeKind::Reserved {
        return Err(Error::WrongTypeKind);
    }
    let msg = zap::Message::parse(msg_bytes)?;
    Ok((tk, msg))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_two_bytes_answer_two_separate_questions() {
        let env = [TypeKind::Nft.as_u8(), ShapeKind::Credential.as_u8(), 0, 0];
        assert_eq!(
            peek_discriminator(&env).unwrap(),
            (TypeKind::Nft, ShapeKind::Credential)
        );
    }

    #[test]
    fn a_byte_naming_no_family_reads_as_unknown_not_as_some_other_family() {
        assert_eq!(TypeKind::from_u8(0x7F), TypeKind::Unknown);
        assert_eq!(ShapeKind::from_u8(0x7F), ShapeKind::Unknown);
    }

    #[test]
    fn peek_refuses_a_buffer_shorter_than_the_prefix() {
        assert_eq!(peek_discriminator(&[1]).unwrap_err(), Error::ShortEnvelope);
    }

    #[test]
    fn next_envelope_walks_a_packed_run_by_each_messages_own_size() {
        let a = shapes::new_credential(TypeKind::Secp256k1, 0, &[1u8; 65], &[]);
        let b = shapes::new_credential(TypeKind::Nft, 0, &[2u8; 130], &[]);
        let mut blob = a.clone();
        blob.extend_from_slice(&b);

        let (first, rest) = next_envelope(&blob).unwrap();
        assert_eq!(first, &a[..]);
        let (second, rest) = next_envelope(rest).unwrap();
        assert_eq!(second, &b[..]);
        assert!(rest.is_empty());
    }

    #[test]
    fn next_envelope_refuses_a_run_that_ends_inside_a_message() {
        let a = shapes::new_credential(TypeKind::Secp256k1, 0, &[1u8; 65], &[]);
        assert_eq!(
            next_envelope(&a[..a.len() - 1]).unwrap_err(),
            Error::ShortEnvelope
        );
    }
}
