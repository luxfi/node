// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The three names this chain uses, and the one alphabet they are written in.
//!
//! [`Id`] is 32 bytes — a transaction id, a block id, a chain id, a handle, a
//! permit, a digest. [`Account`] is 20 — a fee payer, a grantee, a callback.
//! [`NodeId`] is also 20 and is a different thing: a seat on the committee.
//! Go keeps them apart as `ids.ID`, `ids.ShortID` and `ids.NodeID`, and so does
//! this, because a 20-byte value that is both an account and a seat is a value
//! two rules can disagree about.
//!
//! CB58 is how the last two are written when they cross a JSON boundary:
//! base58 of the bytes followed by the last four of their SHA-256. A node id is
//! written with `NodeID-` in front of it; an account is written bare. That is
//! not a style — a payload naming a grantee and a payload naming a seat are
//! told apart by it.

use sha2::{Digest, Sha256};

/// A 32-byte name.
pub type Id = lux_node::vm::Id;

/// The empty id — the parent of the genesis block, and the value a chain
/// without an identity would derive every signature and every block id under.
pub const EMPTY: Id = [0u8; 32];

/// A 20-byte account: a fee payer, and the subject of authorization.
pub type Account = [u8; 20];

/// A 20-byte committee seat.
pub type NodeId = [u8; 20];

/// How long the CB58 checksum is.
const CHECKSUM: usize = 4;

/// The prefix a node id is written with.
pub const NODE_ID_PREFIX: &str = "NodeID-";

/// What a CB58 word can be refused for. The words are Go's.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Cb58Error {
    Base58,
    MissingChecksum,
    BadChecksum,
    WrongLength,
    MissingPrefix,
}

impl std::fmt::Display for Cb58Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Cb58Error::Base58 => write!(f, "base58 decoding error"),
            Cb58Error::MissingChecksum => {
                write!(f, "input string is smaller than the checksum size")
            }
            Cb58Error::BadChecksum => write!(f, "invalid input checksum"),
            Cb58Error::WrongLength => write!(f, "couldn't decode ID to bytes"),
            Cb58Error::MissingPrefix => write!(f, "insufficient NodeID length"),
        }
    }
}

/// Decode a CB58 word: base58, then the trailing four bytes checked against
/// the SHA-256 of everything before them.
pub fn cb58(word: &str) -> Result<Vec<u8>, Cb58Error> {
    let raw = bs58::decode(word).into_vec().map_err(|_| Cb58Error::Base58)?;
    if raw.len() < CHECKSUM {
        return Err(Cb58Error::MissingChecksum);
    }
    let (body, sum) = raw.split_at(raw.len() - CHECKSUM);
    let want = Sha256::digest(body);
    if sum != &want[want.len() - CHECKSUM..] {
        return Err(Cb58Error::BadChecksum);
    }
    Ok(body.to_vec())
}

/// Write a CB58 word.
pub fn cb58_encode(body: &[u8]) -> String {
    let mut checked = body.to_vec();
    let sum = Sha256::digest(body);
    checked.extend_from_slice(&sum[sum.len() - CHECKSUM..]);
    bs58::encode(checked).into_string()
}

/// Read an account from the CB58 word a payload carries.
///
/// An empty word and the literal `null` are the zero account rather than a
/// refusal: that is what Go's `UnmarshalText` does, and an account nobody named
/// is the zero account in both.
pub fn account_from_str(word: &str) -> Result<Account, Cb58Error> {
    if word.is_empty() || word == "null" {
        return Ok([0u8; 20]);
    }
    let body = cb58(word)?;
    body.as_slice().try_into().map_err(|_| Cb58Error::WrongLength)
}

/// Read a committee seat from the prefixed CB58 word a payload carries.
pub fn node_id_from_str(word: &str) -> Result<NodeId, Cb58Error> {
    if word.is_empty() || word == "null" {
        return Ok([0u8; 20]);
    }
    if word.len() <= NODE_ID_PREFIX.len() {
        return Err(Cb58Error::MissingPrefix);
    }
    let Some(rest) = word.strip_prefix(NODE_ID_PREFIX) else {
        return Err(Cb58Error::MissingPrefix);
    };
    let body = cb58(rest)?;
    body.as_slice().try_into().map_err(|_| Cb58Error::WrongLength)
}

/// Write a committee seat the way a payload carries it.
pub fn node_id_to_string(id: &NodeId) -> String {
    format!("{NODE_ID_PREFIX}{}", cb58_encode(id))
}

/// An id whose every byte is `b`. The corpus names its chains this way.
pub fn filled(b: u8) -> Id {
    [b; 32]
}

/// Lowercase hex, the way every id is printed into a conformance row.
pub fn hex(b: &[u8]) -> String {
    let mut s = String::with_capacity(b.len() * 2);
    for x in b {
        s.push(char::from_digit((x >> 4) as u32, 16).unwrap());
        s.push(char::from_digit((x & 15) as u32, 16).unwrap());
    }
    s
}

/// Read lowercase or uppercase hex. `None` on an odd length or a non-hex digit.
pub fn unhex(s: &str) -> Option<Vec<u8>> {
    if !s.len().is_multiple_of(2) {
        return None;
    }
    let b = s.as_bytes();
    (0..s.len() / 2)
        .map(|i| {
            let hi = (b[i * 2] as char).to_digit(16)?;
            let lo = (b[i * 2 + 1] as char).to_digit(16)?;
            Some(((hi << 4) | lo) as u8)
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    // The account the corpus's payer holds, as its grant payload writes it.
    const PAYER_CB58: &str = "NG42sa8bZqRUjcaoq9igh5toYw8mXmw7H";
    const PAYER_HEX: &str = "e933697f7a3d671b8c294452465230d4d433d337";

    // The seat the corpus's committee holds.
    const SEAT: &str = "NodeID-eA9ctCNq41iLLnQErtTUVdxpRyZ8QSc1";

    #[test]
    fn an_account_reads_back_as_the_bytes_go_wrote() {
        let a = account_from_str(PAYER_CB58).expect("cb58");
        assert_eq!(hex(&a), PAYER_HEX);
        assert_eq!(cb58_encode(&a), PAYER_CB58);
    }

    #[test]
    fn a_seat_reads_back_through_its_prefix() {
        let n = node_id_from_str(SEAT).expect("cb58");
        assert_eq!(n, [7u8; 20]);
        assert_eq!(node_id_to_string(&n), SEAT);
    }

    #[test]
    fn a_seat_written_without_its_prefix_is_refused() {
        // The same word, bare. It decodes perfectly as an ACCOUNT and is not a
        // seat, which is the whole reason the prefix is there.
        let bare = SEAT.strip_prefix(NODE_ID_PREFIX).unwrap();
        assert_eq!(node_id_from_str(bare), Err(Cb58Error::MissingPrefix));
        assert_eq!(account_from_str(bare).unwrap(), [7u8; 20]);
    }

    #[test]
    fn a_flipped_character_fails_the_checksum() {
        let mut w: Vec<char> = PAYER_CB58.chars().collect();
        w[0] = if w[0] == 'N' { 'P' } else { 'N' };
        let broken: String = w.into_iter().collect();
        assert_eq!(account_from_str(&broken), Err(Cb58Error::BadChecksum));
    }

    #[test]
    fn a_word_shorter_than_its_checksum_is_refused() {
        assert_eq!(account_from_str("2").unwrap_err(), Cb58Error::MissingChecksum);
    }

    #[test]
    fn an_absent_name_is_the_zero_name_rather_than_a_refusal() {
        assert_eq!(account_from_str("").unwrap(), [0u8; 20]);
        assert_eq!(account_from_str("null").unwrap(), [0u8; 20]);
        assert_eq!(node_id_from_str("").unwrap(), [0u8; 20]);
        assert_eq!(node_id_from_str("null").unwrap(), [0u8; 20]);
    }

    #[test]
    fn a_cb58_word_of_the_wrong_width_is_not_an_account() {
        let thirty_two = cb58_encode(&[9u8; 32]);
        assert_eq!(account_from_str(&thirty_two), Err(Cb58Error::WrongLength));
    }

    #[test]
    fn hex_round_trips() {
        assert_eq!(unhex("00ff1a").unwrap(), vec![0x00, 0xff, 0x1a]);
        assert_eq!(hex(&[0x00, 0xff, 0x1a]), "00ff1a");
        assert!(unhex("abc").is_none());
        assert!(unhex("zz").is_none());
    }
}
