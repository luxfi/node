// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! JSON, written and read the way the Go chain writes and reads it.
//!
//! F puts JSON in two places, and both are consensus surfaces.
//!
//! A RECORD is JSON in the database. Two validators replaying one block must
//! write the same bytes, so the encoding has to be a function of the data and
//! nothing else: fields in declaration order, integers in plain decimal, a fixed
//! byte array as a list of numbers, a byte SLICE as base64, a nil slice as
//! `null`, and Go's `omitempty` omitting exactly what Go omits — a zero number,
//! an empty string, an empty slice, and never a fixed array, which has no empty
//! form.
//!
//! A PAYLOAD is JSON on the wire. It is signed, so its bytes are the payer's, and
//! it is priced by the byte, so its length is the payer's too. Reading it has to
//! match Go exactly in what it ACCEPTS as well as what it produces, and Go's
//! reader has two habits worth writing down: a fixed byte array takes any length
//! — short is zero-filled, long is discarded — and a member the schema does not
//! name is refused outright, because F turns `DisallowUnknownFields` on. The
//! first is why [`byte_array`] does not simply require the width; the second is
//! why every payload here is read field by field rather than by a decoder that
//! ignores what it does not know.

use base64::Engine;
// `Value` deserializes itself; the trait has to be in scope to say so.
use serde::Deserialize;
use serde_json::Value;

use crate::ids;

/// Why a value was not what the schema said.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Error(pub String);

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}

impl std::error::Error for Error {}

fn err(msg: impl Into<String>) -> Error {
    Error(msg.into())
}

// ------------------------------------------------------------------ writing --

/// A string, escaped the way `encoding/json` escapes one — including the HTML
/// characters Go escapes by default, which a port that used a plain JSON writer
/// would leave alone and hash differently.
pub fn write_string(out: &mut String, s: &str) {
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            '<' => out.push_str("\\u003c"),
            '>' => out.push_str("\\u003e"),
            '&' => out.push_str("\\u0026"),
            '\u{2028}' => out.push_str("\\u2028"),
            '\u{2029}' => out.push_str("\\u2029"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
}

/// A fixed byte array: a list of numbers, which is what Go writes for `[N]byte`.
pub fn write_byte_array(out: &mut String, b: &[u8]) {
    out.push('[');
    for (i, v) in b.iter().enumerate() {
        if i > 0 {
            out.push(',');
        }
        out.push_str(&v.to_string());
    }
    out.push(']');
}

/// A byte slice: base64, which is what Go writes for `[]byte`.
pub fn write_base64(out: &mut String, b: &[u8]) {
    write_string(out, &base64::engine::general_purpose::STANDARD.encode(b));
}

/// A byte slice that may be absent. Go writes `null` for a nil one.
pub fn write_base64_or_null(out: &mut String, b: Option<&[u8]>) {
    match b {
        Some(v) => write_base64(out, v),
        None => out.push_str("null"),
    }
}

/// Writes `"name":` — the separator before it when this is not the first member.
pub fn write_key(out: &mut String, first: &mut bool, name: &str) {
    if *first {
        *first = false;
    } else {
        out.push(',');
    }
    write_string(out, name);
    out.push(':');
}

// ------------------------------------------------------------------ reading --

/// The parsed value, refusing anything after it. Go's decoder reports trailing
/// content through `More()`; this reports it as a failed parse, which is the same
/// verdict said once.
pub fn parse(payload: &[u8], op: &str) -> Result<Value, Error> {
    let mut de = serde_json::Deserializer::from_slice(payload);
    let v = match Value::deserialize(&mut de) {
        Ok(v) => v,
        Err(e) => return Err(err(format!("{op}: {e}"))),
    };
    de.end().map_err(|_| err(format!("{op}: trailing content")))?;
    Ok(v)
}

/// The object's members, refusing any the schema does not name.
pub fn object<'a>(
    v: &'a Value,
    op: &str,
    allowed: &[&str],
) -> Result<&'a serde_json::Map<String, Value>, Error> {
    let obj = v
        .as_object()
        .ok_or_else(|| err(format!("{op}: cannot unmarshal into a struct")))?;
    for name in obj.keys() {
        if !allowed.contains(&name.as_str()) {
            return Err(err(format!("{op}: unknown field {name:?}")));
        }
    }
    Ok(obj)
}

/// A fixed byte array, on Go's terms: a shorter list zero-fills, a longer one has
/// its tail discarded, and an absent member is all zeros.
pub fn byte_array<const N: usize>(
    obj: &serde_json::Map<String, Value>,
    name: &str,
    op: &str,
) -> Result<[u8; N], Error> {
    let mut out = [0u8; N];
    let v = match obj.get(name) {
        None | Some(Value::Null) => return Ok(out),
        Some(v) => v,
    };
    let list = v
        .as_array()
        .ok_or_else(|| err(format!("{op}: {name}: cannot unmarshal into a byte array")))?;
    for (i, item) in list.iter().enumerate() {
        if i >= N {
            break;
        }
        out[i] = u8_of(item, name, op)?;
    }
    Ok(out)
}

fn u8_of(v: &Value, name: &str, op: &str) -> Result<u8, Error> {
    let n = v
        .as_u64()
        .ok_or_else(|| err(format!("{op}: {name}: cannot unmarshal into uint8")))?;
    u8::try_from(n).map_err(|_| err(format!("{op}: {name}: number overflows uint8")))
}

/// An unsigned integer, refusing a value that does not fit.
pub fn u64_field(
    obj: &serde_json::Map<String, Value>,
    name: &str,
    op: &str,
) -> Result<u64, Error> {
    match obj.get(name) {
        None | Some(Value::Null) => Ok(0),
        Some(v) => v
            .as_u64()
            .ok_or_else(|| err(format!("{op}: {name}: cannot unmarshal into a uint"))),
    }
}

pub fn u32_field(
    obj: &serde_json::Map<String, Value>,
    name: &str,
    op: &str,
) -> Result<u32, Error> {
    let n = u64_field(obj, name, op)?;
    u32::try_from(n).map_err(|_| err(format!("{op}: {name}: number overflows uint32")))
}

pub fn u8_field(obj: &serde_json::Map<String, Value>, name: &str, op: &str) -> Result<u8, Error> {
    let n = u64_field(obj, name, op)?;
    u8::try_from(n).map_err(|_| err(format!("{op}: {name}: number overflows uint8")))
}

/// A signed integer. `Level` is one, and it may legitimately be negative — which
/// is why the rule that refuses a negative level is a rule and not a type.
pub fn i64_field(
    obj: &serde_json::Map<String, Value>,
    name: &str,
    op: &str,
) -> Result<i64, Error> {
    match obj.get(name) {
        None | Some(Value::Null) => Ok(0),
        Some(v) => v
            .as_i64()
            .ok_or_else(|| err(format!("{op}: {name}: cannot unmarshal into an int"))),
    }
}

pub fn string_field(
    obj: &serde_json::Map<String, Value>,
    name: &str,
    op: &str,
) -> Result<String, Error> {
    match obj.get(name) {
        None | Some(Value::Null) => Ok(String::new()),
        Some(Value::String(s)) => Ok(s.clone()),
        Some(_) => Err(err(format!("{op}: {name}: cannot unmarshal into a string"))),
    }
}

/// A byte slice: base64 in a string, or `null` for none.
pub fn bytes_field(
    obj: &serde_json::Map<String, Value>,
    name: &str,
    op: &str,
) -> Result<Vec<u8>, Error> {
    match obj.get(name) {
        None | Some(Value::Null) => Ok(Vec::new()),
        Some(Value::String(s)) => base64::engine::general_purpose::STANDARD
            .decode(s)
            .map_err(|_| err(format!("{op}: {name}: illegal base64 data"))),
        Some(_) => Err(err(format!("{op}: {name}: cannot unmarshal into a byte slice"))),
    }
}

/// An account, written as Go writes one: cb58 in a string.
pub fn account_field(
    obj: &serde_json::Map<String, Value>,
    name: &str,
    op: &str,
) -> Result<crate::fee::Account, Error> {
    match obj.get(name) {
        None | Some(Value::Null) => Ok([0u8; ids::SHORT_ID_LEN]),
        Some(Value::String(s)) => {
            ids::short_id_from_string(s).map_err(|e| err(format!("{op}: {name}: {e}")))
        }
        Some(_) => Err(err(format!("{op}: {name}: cannot unmarshal into an address"))),
    }
}

/// A node id, written as Go writes one: `NodeID-` and cb58.
pub fn node_id_field(
    obj: &serde_json::Map<String, Value>,
    name: &str,
    op: &str,
) -> Result<ids::NodeId, Error> {
    match obj.get(name) {
        None | Some(Value::Null) => Ok([0u8; ids::SHORT_ID_LEN]),
        Some(Value::String(s)) => {
            ids::node_id_from_string(s).map_err(|e| err(format!("{op}: {name}: {e}")))
        }
        Some(_) => Err(err(format!("{op}: {name}: cannot unmarshal into a node id"))),
    }
}

/// An id, written as Go writes one: its native chain word if it has one, else
/// cb58.
pub fn id_field(obj: &serde_json::Map<String, Value>, name: &str, op: &str) -> Result<ids::Id, Error> {
    match obj.get(name) {
        None | Some(Value::Null) => Ok(ids::EMPTY),
        Some(Value::String(s)) => id_from_string(s).map_err(|e| err(format!("{op}: {name}: {e}"))),
        Some(_) => Err(err(format!("{op}: {name}: cannot unmarshal into an id"))),
    }
}

/// Reads back what [`ids::id_string`] wrote, native chain words included.
pub fn id_from_string(s: &str) -> Result<ids::Id, &'static str> {
    for letter in *b"PCXQABMFZGIKD" {
        let mut candidate = ids::EMPTY;
        candidate[31] = letter;
        if ids::native_chain_string(&candidate).as_deref() == Some(s) {
            return Ok(candidate);
        }
    }
    let raw = ids::from_cb58(s)?;
    if raw.len() != 32 {
        return Err("wrong id length");
    }
    let mut out = ids::EMPTY;
    out.copy_from_slice(&raw);
    Ok(out)
}

/// A list of objects, or none.
pub fn array<'a>(
    obj: &'a serde_json::Map<String, Value>,
    name: &str,
    op: &str,
) -> Result<Option<&'a Vec<Value>>, Error> {
    match obj.get(name) {
        None | Some(Value::Null) => Ok(None),
        Some(Value::Array(a)) => Ok(Some(a)),
        Some(_) => Err(err(format!("{op}: {name}: cannot unmarshal into a list"))),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_string_is_escaped_the_way_go_escapes_one() {
        let mut out = String::new();
        write_string(&mut out, "a<b>c&d\"e\\f\ng\th");
        // `encoding/json` escapes the three HTML characters by default, so a port
        // that left them alone would write different bytes for the same record.
        assert_eq!(out, "\"a\\u003cb\\u003ec\\u0026d\\\"e\\\\f\\ng\\th\"");
    }

    #[test]
    fn a_fixed_array_is_numbers_and_a_slice_is_base64() {
        let mut out = String::new();
        write_byte_array(&mut out, &[1, 2, 255]);
        assert_eq!(out, "[1,2,255]");
        out.clear();
        write_base64(&mut out, b"k");
        assert_eq!(out, "\"aw==\"");
        out.clear();
        write_base64(&mut out, b"");
        assert_eq!(out, "\"\"");
        out.clear();
        write_base64_or_null(&mut out, None);
        assert_eq!(out, "null");
    }

    #[test]
    fn a_fixed_array_takes_any_length_the_way_gos_reader_does() {
        let v = parse(br#"{"d":[1,2]}"#, "t").unwrap();
        let obj = object(&v, "t", &["d"]).unwrap();
        assert_eq!(byte_array::<4>(obj, "d", "t").unwrap(), [1, 2, 0, 0], "short zero-fills");

        let v = parse(br#"{"d":[1,2,3,4,5,6]}"#, "t").unwrap();
        let obj = object(&v, "t", &["d"]).unwrap();
        assert_eq!(byte_array::<4>(obj, "d", "t").unwrap(), [1, 2, 3, 4], "long is discarded");

        let v = parse(br#"{}"#, "t").unwrap();
        let obj = object(&v, "t", &["d"]).unwrap();
        assert_eq!(byte_array::<4>(obj, "d", "t").unwrap(), [0, 0, 0, 0], "absent is zero");
    }

    #[test]
    fn a_member_the_schema_does_not_name_is_refused() {
        let v = parse(br#"{"known":1,"body":"smuggled"}"#, "t").unwrap();
        assert!(object(&v, "t", &["known"]).is_err());
        assert!(object(&v, "t", &["known", "body"]).is_ok());
    }

    #[test]
    fn content_after_the_value_is_refused() {
        assert!(parse(br#"{"a":1}"#, "t").is_ok());
        assert!(parse(b"{\"a\":1}   \n", "t").is_ok(), "whitespace is not content");
        assert!(parse(br#"{"a":1}{"b":2}"#, "t").is_err());
        assert!(parse(b"{not json", "t").is_err());
    }

    #[test]
    fn an_id_round_trips_through_its_written_form() {
        let mut id = ids::EMPTY;
        id[31] = b'F';
        assert_eq!(id_from_string(&ids::id_string(&id)).unwrap(), id);
        let ordinary: ids::Id = [7u8; 32];
        assert_eq!(id_from_string(&ids::id_string(&ordinary)).unwrap(), ordinary);
    }
}
