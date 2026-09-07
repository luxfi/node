// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The document a chain starts from, read the way the reference reads it.
//!
//! A genesis file is an OPERATOR's artefact. One file is handed to every
//! implementation of this chain, and what it produces is the genesis block's
//! id, the initial state root and the first shielded outputs. Two
//! implementations that read the same file differently do not run the same
//! chain: one refuses to start, or worse, both start and disagree about the
//! id of block zero.
//!
//! So the shape here is NOT this crate's choice. It is what
//! `encoding/json` does with the reference's `Genesis`, `Transaction` and
//! `SetupParams` structs, down to the parts nobody would design on purpose:
//!
//!   - A transaction is a JSON OBJECT, field by field. It is not the wire
//!     bytes in a string — the reference refuses that, and a genesis this
//!     accepted would be one no reference node could boot.
//!   - `[]byte` is standard base64, `ids.ID` is CB58 (or a native chain
//!     letter), a number must be an integer that fits the field it names.
//!   - `null` anywhere is "say nothing", never "set zero" — for a slice it
//!     leaves nothing, for a number it leaves what was there.
//!   - A field name is matched exactly if it can be, and case-insensitively
//!     if it cannot: `{"TimeStamp":7}` names the timestamp.
//!   - An unknown field is ignored; a field of the wrong TYPE is a refusal,
//!     including `setupParams`, whose contents this chain never reads. What
//!     is refused has to match, or a file one node boots stops another.
//!
//! `testdata/golden.json` carries twenty-five of these documents together
//! with what the reference answered for each — accepted or refused, and for
//! an accepted one every decoded field, the ids, the seeded UTXO records, the
//! initial root and the genesis block id. `tests/golden.rs` checks this
//! against those answers rather than against my reading of that code.
//!
//! ONE PLACE WHERE FIDELITY IS NOT REACHABLE, stated rather than hidden: a
//! document that spells one field two different ways in two different cases
//! (`{"TIMESTAMP":1,"TimeStamp":2}`) is resolved by the reference in
//! DOCUMENT order, and the JSON value model here is ordered by key. Exact
//! spellings, and any single spelling in any case, agree.

use serde_json::{Map, Value};

use crate::error::{Error, Result};
use crate::ids::{Id, EMPTY, ID_LEN};
use crate::tx::{ShieldedOutput, Transaction, TransparentInput, TransparentOutput, ZkProof};

/// What a chain starts from.
///
/// A genesis that names no timestamp is stamped 0, NOT "now". The timestamp is
/// hashed into the genesis block's id, so reading the wall clock here gives
/// every node a different genesis id — a different chain — for the same
/// genesis file, and a different one again after each restart.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Genesis {
    pub timestamp: i64,
    pub initial_txs: Vec<Transaction>,
}

impl Genesis {
    /// Read a genesis document.
    ///
    /// An empty file, and a file that says `null`, are the empty genesis: a
    /// chain that starts with nothing rather than an error.
    pub fn parse(raw: &[u8]) -> Result<Genesis> {
        if raw.is_empty() {
            return Ok(Genesis::default());
        }
        let doc: Value = serde_json::from_slice(raw)
            .map_err(|e| Error::BadRequest(format!("genesis is not JSON: {e}")))?;
        let obj = match &doc {
            Value::Null => return Ok(Genesis::default()),
            Value::Object(o) => o,
            other => {
                return Err(Error::BadRequest(format!(
                    "genesis is {}, not an object",
                    kind(other)
                )))
            }
        };

        let mut g = Genesis::default();
        if let Some(v) = field(obj, "timestamp") {
            g.timestamp = i64_of(v, "timestamp")?;
        }
        if let Some(v) = field(obj, "initialTransactions") {
            for (i, item) in list_of(v, "initialTransactions")?.iter().enumerate() {
                g.initial_txs
                    .push(transaction(item).map_err(|e| at(&format!("transaction {i}"), e))?);
            }
        }
        // The trusted-setup parameters are read and not kept: this chain gets
        // its verifying keys from its configuration, and the Groth16 CRS, the
        // PLONK SRS and the FHE public parameters name systems whose verifier
        // is elsewhere or nowhere. They are still CHECKED, because a document
        // the reference refuses must be refused here too.
        if let Some(v) = field(obj, "setupParams") {
            setup_params(v)?;
        }
        Ok(g)
    }
}

fn setup_params(v: &Value) -> Result<()> {
    let obj = match v {
        Value::Null => return Ok(()),
        Value::Object(o) => o,
        other => {
            return Err(Error::BadRequest(format!(
                "setupParams is {}, not an object",
                kind(other)
            )))
        }
    };
    for name in ["powersOfTau", "verifyingKey", "plonkSRS", "fhePublicParams"] {
        if let Some(f) = field(obj, name) {
            bytes_of(f, name)?;
        }
    }
    Ok(())
}

fn transaction(v: &Value) -> Result<Transaction> {
    let obj = match v {
        // A `null` element leaves a transaction that is nothing at all. The
        // reference decodes it to a nil pointer and dereferences it later; the
        // one honest reading here is that a genesis cannot name no
        // transaction where a transaction goes.
        Value::Null => return Err(Error::BadRequest("a genesis transaction is null".into())),
        Value::Object(o) => o,
        other => {
            return Err(Error::BadRequest(format!(
                "a genesis transaction is {}, not an object",
                kind(other)
            )))
        }
    };
    let mut tx = Transaction::default();
    if let Some(f) = field(obj, "id") {
        tx.id = id_of(f, "id")?;
    }
    if let Some(f) = field(obj, "type") {
        tx.kind = u8_of(f, "type")?;
    }
    if let Some(f) = field(obj, "version") {
        tx.version = u8_of(f, "version")?;
    }
    if let Some(f) = field(obj, "transparentInputs") {
        for (i, item) in list_of(f, "transparentInputs")?.iter().enumerate() {
            tx.transparent_inputs.push(
                transparent_input(item).map_err(|e| at(&format!("transparentInput {i}"), e))?,
            );
        }
    }
    if let Some(f) = field(obj, "transparentOutputs") {
        for (i, item) in list_of(f, "transparentOutputs")?.iter().enumerate() {
            tx.transparent_outputs.push(
                transparent_output(item).map_err(|e| at(&format!("transparentOutput {i}"), e))?,
            );
        }
    }
    if let Some(f) = field(obj, "nullifiers") {
        for (i, item) in list_of(f, "nullifiers")?.iter().enumerate() {
            tx.nullifiers
                .push(bytes_of(item, "nullifier").map_err(|e| at(&format!("nullifier {i}"), e))?);
        }
    }
    if let Some(f) = field(obj, "outputs") {
        for (i, item) in list_of(f, "outputs")?.iter().enumerate() {
            tx.outputs
                .push(shielded_output(item).map_err(|e| at(&format!("output {i}"), e))?);
        }
    }
    if let Some(f) = field(obj, "proof") {
        tx.proof = proof(f)?;
    }
    if let Some(f) = field(obj, "fee") {
        tx.fee = u64_of(f, "fee")?;
    }
    if let Some(f) = field(obj, "expiry") {
        tx.expiry = u64_of(f, "expiry")?;
    }
    if let Some(f) = field(obj, "memo") {
        tx.memo = bytes_of(f, "memo")?;
    }
    Ok(tx)
}

fn transparent_input(v: &Value) -> Result<TransparentInput> {
    let obj = object(v, "a transparent input")?;
    let mut i = TransparentInput::default();
    if let Some(f) = field(obj, "txId") {
        i.tx_id = id_of(f, "txId")?;
    }
    if let Some(f) = field(obj, "outputIdx") {
        i.output_idx = u32_of(f, "outputIdx")?;
    }
    if let Some(f) = field(obj, "amount") {
        i.amount = u64_of(f, "amount")?;
    }
    if let Some(f) = field(obj, "address") {
        i.address = bytes_of(f, "address")?;
    }
    Ok(i)
}

fn transparent_output(v: &Value) -> Result<TransparentOutput> {
    let obj = object(v, "a transparent output")?;
    let mut o = TransparentOutput::default();
    if let Some(f) = field(obj, "amount") {
        o.amount = u64_of(f, "amount")?;
    }
    if let Some(f) = field(obj, "address") {
        o.address = bytes_of(f, "address")?;
    }
    if let Some(f) = field(obj, "assetId") {
        o.asset_id = id_of(f, "assetId")?;
    }
    Ok(o)
}

fn shielded_output(v: &Value) -> Result<ShieldedOutput> {
    let obj = object(v, "a shielded output")?;
    let mut o = ShieldedOutput::default();
    if let Some(f) = field(obj, "commitment") {
        o.commitment = bytes_of(f, "commitment")?;
    }
    if let Some(f) = field(obj, "encryptedNote") {
        o.encrypted_note = bytes_of(f, "encryptedNote")?;
    }
    if let Some(f) = field(obj, "ephemeralPubKey") {
        o.ephemeral_pub_key = bytes_of(f, "ephemeralPubKey")?;
    }
    if let Some(f) = field(obj, "outputProof") {
        o.output_proof = bytes_of(f, "outputProof")?;
    }
    Ok(o)
}

fn proof(v: &Value) -> Result<Option<ZkProof>> {
    let obj = match v {
        // The proof is a POINTER in the reference: `null` leaves none, which
        // is a transaction `validate_basic` then refuses.
        Value::Null => return Ok(None),
        Value::Object(o) => o,
        other => {
            return Err(Error::BadRequest(format!(
                "a proof is {}, not an object",
                kind(other)
            )))
        }
    };
    let mut p = ZkProof::default();
    if let Some(f) = field(obj, "proofType") {
        p.proof_type = string_of(f, "proofType")?;
    }
    if let Some(f) = field(obj, "proofData") {
        p.proof_data = bytes_of(f, "proofData")?;
    }
    if let Some(f) = field(obj, "publicInputs") {
        for (i, item) in list_of(f, "publicInputs")?.iter().enumerate() {
            p.public_inputs.push(
                bytes_of(item, "publicInput").map_err(|e| at(&format!("publicInput {i}"), e))?,
            );
        }
    }
    Ok(Some(p))
}

// ---- what a value has to be ------------------------------------------------

/// The field a name asks for: the exact spelling if the document has it, and
/// otherwise a spelling that differs only in case.
fn field<'a>(obj: &'a Map<String, Value>, name: &str) -> Option<&'a Value> {
    if let Some(v) = obj.get(name) {
        return Some(v);
    }
    obj.iter()
        .find(|(k, _)| k.eq_ignore_ascii_case(name))
        .map(|(_, v)| v)
}

fn object<'a>(v: &'a Value, what: &str) -> Result<&'a Map<String, Value>> {
    match v {
        Value::Object(o) => Ok(o),
        other => Err(Error::BadRequest(format!(
            "{what} is {}, not an object",
            kind(other)
        ))),
    }
}

/// A list, or nothing. `null` is an absent list, never an empty one — the
/// difference does not survive into a Rust `Vec`, and does not have to: both
/// leave the field with nothing in it.
fn list_of<'a>(v: &'a Value, name: &str) -> Result<&'a [Value]> {
    match v {
        Value::Null => Ok(&[]),
        Value::Array(a) => Ok(a),
        other => Err(Error::BadRequest(format!(
            "{name} is {}, not a list",
            kind(other)
        ))),
    }
}

fn string_of(v: &Value, name: &str) -> Result<String> {
    match v {
        Value::Null => Ok(String::new()),
        Value::String(s) => Ok(s.clone()),
        other => Err(Error::BadRequest(format!(
            "{name} is {}, not a string",
            kind(other)
        ))),
    }
}

fn i64_of(v: &Value, name: &str) -> Result<i64> {
    match v {
        Value::Null => Ok(0),
        Value::Number(n) => n.as_i64().ok_or_else(|| {
            Error::BadRequest(format!("{name} is {n}, which is not a whole number"))
        }),
        other => Err(Error::BadRequest(format!(
            "{name} is {}, not a number",
            kind(other)
        ))),
    }
}

fn u64_of(v: &Value, name: &str) -> Result<u64> {
    match v {
        Value::Null => Ok(0),
        Value::Number(n) => n.as_u64().ok_or_else(|| {
            Error::BadRequest(format!(
                "{name} is {n}, which is not a whole number that fits"
            ))
        }),
        other => Err(Error::BadRequest(format!(
            "{name} is {}, not a number",
            kind(other)
        ))),
    }
}

fn u32_of(v: &Value, name: &str) -> Result<u32> {
    let n = u64_of(v, name)?;
    u32::try_from(n).map_err(|_| Error::BadRequest(format!("{name} is {n}, which does not fit")))
}

fn u8_of(v: &Value, name: &str) -> Result<u8> {
    let n = u64_of(v, name)?;
    u8::try_from(n).map_err(|_| Error::BadRequest(format!("{name} is {n}, which does not fit")))
}

/// Bytes, as standard base64. `null` is no bytes.
fn bytes_of(v: &Value, name: &str) -> Result<Vec<u8>> {
    match v {
        Value::Null => Ok(Vec::new()),
        Value::String(s) => base64(s).map_err(|e| at(name, e)),
        other => Err(Error::BadRequest(format!(
            "{name} is {}, not base64 text",
            kind(other)
        ))),
    }
}

/// An id, as CB58 — or a native chain's letter, or the empty string for the
/// empty id. `null` names no id, which is also the empty one.
fn id_of(v: &Value, name: &str) -> Result<Id> {
    match v {
        Value::Null => Ok(EMPTY),
        Value::String(s) => {
            if s.is_empty() {
                return Ok(EMPTY);
            }
            if let Some(id) = native_chain(s) {
                return Ok(id);
            }
            let raw = cb58(s).map_err(|e| at(name, e))?;
            crate::ids::from_slice(&raw).ok_or_else(|| {
                Error::BadRequest(format!(
                    "{name} decodes to {} bytes, and an id is {ID_LEN}",
                    raw.len()
                ))
            })
        }
        other => Err(Error::BadRequest(format!(
            "{name} is {}, not an id",
            kind(other)
        ))),
    }
}

/// The native chains are named by one letter, or by that letter after
/// thirty-two `1`s — which is what CB58 prints for an id that is all zero but
/// its last byte. That is the whole of the rule, so it is the whole of what is
/// written here.
fn native_chain(s: &str) -> Option<Id> {
    const LETTERS: &[u8] = b"PCXQABMFZGIKD";
    let letter = match s.as_bytes() {
        [c] => c.to_ascii_uppercase(),
        b if b.len() == 33 && b[..32] == [b'1'; 32] => b[32],
        _ => return None,
    };
    if !LETTERS.contains(&letter) {
        return None;
    }
    let mut id = EMPTY;
    id[ID_LEN - 1] = letter;
    Some(id)
}

fn kind(v: &Value) -> &'static str {
    match v {
        Value::Null => "null",
        Value::Bool(_) => "a boolean",
        Value::Number(_) => "a number",
        Value::String(_) => "a string",
        Value::Array(_) => "a list",
        Value::Object(_) => "an object",
    }
}

fn at(where_: &str, e: Error) -> Error {
    Error::BadRequest(format!("{where_}: {e}"))
}

// ---- the two encodings a genesis document is written in ---------------------

/// Standard base64, padded — what Go's JSON encoder writes for a `[]byte`.
///
/// Line breaks are skipped, because the reference's decoder skips them and a
/// document wrapped by an editor would otherwise be a different document. The
/// unused bits of a final partial group are dropped rather than refused, for
/// the same reason: this is a reader of what that encoder wrote, not a stricter
/// one of my own.
pub fn base64(s: &str) -> Result<Vec<u8>> {
    fn sextet(c: u8) -> Option<u8> {
        Some(match c {
            b'A'..=b'Z' => c - b'A',
            b'a'..=b'z' => c - b'a' + 26,
            b'0'..=b'9' => c - b'0' + 52,
            b'+' => 62,
            b'/' => 63,
            _ => return None,
        })
    }
    let text: Vec<u8> = s.bytes().filter(|c| *c != b'\r' && *c != b'\n').collect();
    if !text.len().is_multiple_of(4) {
        return Err(Error::BadRequest(format!(
            "base64 of {} characters is not a whole number of groups",
            text.len()
        )));
    }
    let mut out = Vec::with_capacity(text.len() / 4 * 3);
    let mut i = 0;
    while i < text.len() {
        let group = &text[i..i + 4];
        i += 4;
        let pad = group.iter().filter(|c| **c == b'=').count();
        // Padding is a tail: it ends the text, and never more than two of it.
        if pad > 2 || (pad > 0 && i != text.len()) {
            return Err(Error::BadRequest("base64 padding is not at the end".into()));
        }
        if pad > 0 && (group[3] != b'=' || (pad == 2 && group[2] != b'=')) {
            return Err(Error::BadRequest("base64 padding is not at the end".into()));
        }
        let mut acc: u32 = 0;
        for c in &group[..4 - pad] {
            let s = sextet(*c).ok_or_else(|| {
                Error::BadRequest(format!("{:?} is not a base64 character", *c as char))
            })?;
            acc = (acc << 6) | s as u32;
        }
        acc <<= 6 * pad;
        let bytes = acc.to_be_bytes();
        out.extend_from_slice(&bytes[1..4 - pad]);
    }
    Ok(out)
}

/// CB58: base58 over the bytes with four checksum bytes after them, the last
/// four of their SHA-256. It is how the reference prints every id, so it is
/// how a genesis document names one.
pub fn cb58(s: &str) -> Result<Vec<u8>> {
    let decoded = base58(s)?;
    if decoded.len() < 4 {
        return Err(Error::BadRequest(
            "a CB58 string is shorter than its own checksum".into(),
        ));
    }
    let (raw, checksum) = decoded.split_at(decoded.len() - 4);
    if crate::hash::sha256(raw)[28..] != *checksum {
        return Err(Error::BadRequest("CB58 checksum does not match".into()));
    }
    Ok(raw.to_vec())
}

/// Base58, Bitcoin's alphabet. A leading `1` is a leading zero byte.
fn base58(s: &str) -> Result<Vec<u8>> {
    const ALPHABET: &[u8; 58] = b"123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";
    let mut out: Vec<u8> = Vec::with_capacity(s.len());
    for c in s.bytes() {
        let mut carry = ALPHABET.iter().position(|a| *a == c).ok_or_else(|| {
            Error::BadRequest(format!("{:?} is not a base58 character", c as char))
        })? as u32;
        for byte in out.iter_mut().rev() {
            carry += 58 * (*byte as u32);
            *byte = (carry & 0xFF) as u8;
            carry >>= 8;
        }
        while carry > 0 {
            out.insert(0, (carry & 0xFF) as u8);
            carry >>= 8;
        }
    }
    // Every leading '1' is a zero byte that the arithmetic above cannot carry.
    let zeros = s.bytes().take_while(|c| *c == b'1').count();
    let mut full = vec![0u8; zeros];
    full.extend_from_slice(&out);
    Ok(full)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn an_absent_document_is_the_empty_genesis() {
        assert_eq!(Genesis::parse(b"").unwrap(), Genesis::default());
        assert_eq!(Genesis::parse(b"null").unwrap(), Genesis::default());
        assert_eq!(Genesis::parse(b"{}").unwrap(), Genesis::default());
    }

    #[test]
    fn a_genesis_that_names_no_time_is_stamped_zero() {
        // Never "now": the stamp is hashed into the genesis id, so a clock
        // read here gives every node a different chain.
        assert_eq!(Genesis::parse(b"{}").unwrap().timestamp, 0);
    }

    #[test]
    fn a_field_is_found_however_it_is_cased() {
        assert_eq!(Genesis::parse(br#"{"TimeStamp":7}"#).unwrap().timestamp, 7);
        assert_eq!(Genesis::parse(br#"{"timestamp":7}"#).unwrap().timestamp, 7);
    }

    #[test]
    fn an_unknown_field_is_ignored_and_a_mistyped_one_is_refused() {
        assert_eq!(
            Genesis::parse(br#"{"nonsense":[1,2],"timestamp":5}"#)
                .unwrap()
                .timestamp,
            5
        );
        assert!(Genesis::parse(br#"{"timestamp":"5"}"#).is_err());
        assert!(Genesis::parse(br#"{"timestamp":1.5}"#).is_err());
        // setupParams is read by nothing here and checked anyway: a document
        // the reference refuses has to be refused here too.
        assert!(Genesis::parse(br#"{"setupParams":5}"#).is_err());
        assert!(Genesis::parse(br#"{"setupParams":{"powersOfTau":7}}"#).is_err());
    }

    #[test]
    fn a_transaction_is_an_object_and_not_its_own_wire_bytes() {
        // The wire form belongs to a peer; a genesis names fields. Accepting
        // hex here would boot a chain from a file no reference node reads.
        assert!(Genesis::parse(br#"{"initialTransactions":["5a415000"]}"#).is_err());
        let g = Genesis::parse(br#"{"initialTransactions":[{"fee":4}]}"#).unwrap();
        assert_eq!(g.initial_txs.len(), 1);
        assert_eq!(g.initial_txs[0].fee, 4);
    }

    #[test]
    fn a_number_has_to_fit_the_field_it_names() {
        assert!(Genesis::parse(br#"{"initialTransactions":[{"type":250}]}"#).is_ok());
        assert!(Genesis::parse(br#"{"initialTransactions":[{"type":300}]}"#).is_err());
        assert!(Genesis::parse(br#"{"initialTransactions":[{"fee":-1}]}"#).is_err());
    }

    #[test]
    fn base64_is_the_encoding_bytes_arrive_in() {
        assert_eq!(base64("").unwrap(), b"");
        assert_eq!(base64("bWVtbw==").unwrap(), b"memo");
        assert_eq!(base64("bnVsbDE=").unwrap(), b"null1");
        assert_eq!(base64("bWVt\nbw==").unwrap(), b"memo");
        assert!(base64("!!").is_err());
        assert!(base64("bWVtbw=").is_err());
        assert!(base64("bWVt=bw=").is_err());
    }

    #[test]
    fn cb58_is_checked_before_it_is_believed() {
        // The empty id, as the reference prints it.
        let empty = "11111111111111111111111111111111LpoYY";
        assert_eq!(cb58(empty).unwrap(), vec![0u8; 32]);
        // One byte of the payload moved: the checksum no longer stands.
        let mut bad: Vec<char> = empty.chars().collect();
        bad[0] = '2';
        assert!(cb58(&bad.into_iter().collect::<String>()).is_err());
        assert!(cb58("not-cb58").is_err());
        assert!(cb58("1").is_err());
    }

    #[test]
    fn a_native_chain_is_named_by_its_letter() {
        let mut z = EMPTY;
        z[ID_LEN - 1] = b'Z';
        assert_eq!(native_chain("Z"), Some(z));
        assert_eq!(native_chain("z"), Some(z));
        assert_eq!(native_chain("11111111111111111111111111111111Z"), Some(z));
        assert_eq!(native_chain("Y"), None);
        assert_eq!(native_chain("ZZ"), None);
    }

    #[test]
    fn an_id_is_refused_rather_than_truncated() {
        assert!(Genesis::parse(br#"{"initialTransactions":[{"id":"not-cb58"}]}"#).is_err());
        // CB58 that decodes to the wrong number of bytes is not a short id.
        assert!(Genesis::parse(br#"{"initialTransactions":[{"id":"1112P2VvR"}]}"#).is_err());
        let g = Genesis::parse(br#"{"initialTransactions":[{"id":""}]}"#).unwrap();
        assert_eq!(g.initial_txs[0].id, EMPTY);
    }
}
