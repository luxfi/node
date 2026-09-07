// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Bytes as text, in the one place that does it.
//!
//! Three surfaces need it — an id in a log line, a transaction in a JSON-RPC
//! argument, a genesis document — and three implementations of it is three
//! chances for one of them to disagree about case, about an odd length, or
//! about what to do with a character that is not a digit.

use crate::error::{Error, Result};

/// Lower case, two characters per byte.
pub fn encode(bytes: &[u8]) -> String {
    const DIGITS: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push(DIGITS[(b >> 4) as usize] as char);
        s.push(DIGITS[(b & 0x0F) as usize] as char);
    }
    s
}

/// The inverse. Either case is read; an odd length or a character that is not
/// a hex digit is a refusal, not a truncation.
pub fn decode(s: &str) -> Result<Vec<u8>> {
    let b = s.as_bytes();
    if !b.len().is_multiple_of(2) {
        return Err(Error::BadRequest("hex of odd length".into()));
    }
    let mut out = Vec::with_capacity(b.len() / 2);
    for pair in b.as_chunks::<2>().0 {
        out.push(nibble(pair[0])? << 4 | nibble(pair[1])?);
    }
    Ok(out)
}

fn nibble(c: u8) -> Result<u8> {
    match c {
        b'0'..=b'9' => Ok(c - b'0'),
        b'a'..=b'f' => Ok(c - b'a' + 10),
        b'A'..=b'F' => Ok(c - b'A' + 10),
        _ => Err(Error::BadRequest("not hex".into())),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn it_round_trips() {
        let bytes: Vec<u8> = (0..=255u8).collect();
        assert_eq!(decode(&encode(&bytes)).unwrap(), bytes);
        assert_eq!(encode(&[]), "");
        assert_eq!(decode("").unwrap(), Vec::<u8>::new());
    }

    #[test]
    fn it_writes_lower_case_and_reads_either() {
        assert_eq!(encode(&[0xAB, 0x0F]), "ab0f");
        assert_eq!(decode("AB0F").unwrap(), vec![0xAB, 0x0F]);
        assert_eq!(decode("aB0f").unwrap(), vec![0xAB, 0x0F]);
    }

    #[test]
    fn what_is_not_hex_is_refused_rather_than_truncated() {
        // The failure that matters: reading a prefix and stopping would turn a
        // corrupt argument into a shorter, valid-looking value.
        assert!(decode("00zz").is_err());
        assert!(decode("abc").is_err());
        assert!(decode("00 11").is_err());
    }
}
