// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! JSON as Go's `encoding/json` does it, because on this chain that is a
//! consensus question and not a formatting one.
//!
//! READING. Go resolves a member name EXACTLY first, and failing that by a
//! case-insensitive match; with two members naming one field the LAST one
//! wins; `null` for a member leaves it at its zero value and is not an error;
//! a member the struct does not name is ignored; a number reaching an integer
//! must BE an integer, because Go hands the literal to `strconv`; and bytes
//! after the value are refused.
//!
//! WRITING. Fields come out in declaration order. A `[]byte` is base64 and a
//! `[N]byte` is a list of numbers — the same bytes, two renderings, and which
//! one a field gets is decided by its Go TYPE and by nothing else. `<`, `>`
//! and `&` are escaped, a nil slice or map is `null`, `omitempty` omits an
//! empty one entirely, and map keys come out sorted.
//!
//! The writer is what the block id is taken over, so every one of those is
//! load-bearing: a field written any other way names a different block.

use base64::Engine;
use serde_json::Value;

use crate::gotime::Time;
use crate::ids::{self, Id, NodeId};

pub type Error = String;

/// Two names that reach one field. Go matches a member name exactly, and
/// failing that by `unicode.SimpleFold` — which is NOT ASCII case folding: the
/// Kelvin sign folds onto `k` and the long s onto `s`, and a field name
/// carrying either letter is reachable by a member spelled with the rune.
fn fold_eq(a: &str, b: &str) -> bool {
    let canon = |c: char| -> char {
        match c {
            '\u{212a}' => 'k',
            '\u{17f}' => 's',
            c if c.is_ascii() => c.to_ascii_lowercase(),
            c => c,
        }
    };
    let mut x = a.chars().map(canon);
    let mut y = b.chars().map(canon);
    loop {
        match (x.next(), y.next()) {
            (None, None) => return true,
            (Some(p), Some(q)) if p == q => continue,
            _ => return false,
        }
    }
}

/// The member named `name`. Go walks the members in DOCUMENT order and decodes
/// each one that resolves to the field, so the LAST of several wins — whether
/// it resolved exactly or by folding.
pub fn member<'a>(obj: &'a Value, name: &str) -> Option<&'a Value> {
    let map = obj.as_object()?;
    let mut found = None;
    for (k, v) in map {
        if k == name || fold_eq(k, name) {
            found = Some(v);
        }
    }
    found
}

/// A member that is absent, or present and `null`, leaves the field alone.
fn present<'a>(obj: &'a Value, name: &str) -> Option<&'a Value> {
    match member(obj, name) {
        None | Some(Value::Null) => None,
        Some(v) => Some(v),
    }
}

fn wrong(name: &str, want: &str) -> Error {
    format!("json: cannot unmarshal into Go struct field {name} of type {want}")
}

pub fn object(v: &Value) -> Result<&Value, Error> {
    if v.is_object() {
        Ok(v)
    } else {
        Err("json: cannot unmarshal into Go value of type struct".into())
    }
}

pub fn u64_of(obj: &Value, name: &str) -> Result<u64, Error> {
    match present(obj, name) {
        None => Ok(0),
        Some(v) => v.as_u64().ok_or_else(|| wrong(name, "uint64")),
    }
}

pub fn i64_of(obj: &Value, name: &str) -> Result<i64, Error> {
    match present(obj, name) {
        None => Ok(0),
        Some(v) => v.as_i64().ok_or_else(|| wrong(name, "int64")),
    }
}

pub fn u32_of(obj: &Value, name: &str) -> Result<u32, Error> {
    let n = u64_of(obj, name)?;
    u32::try_from(n).map_err(|_| format!("json: number {n} overflows uint32 field {name}"))
}

pub fn u8_of(obj: &Value, name: &str) -> Result<u8, Error> {
    let n = u64_of(obj, name)?;
    u8::try_from(n).map_err(|_| format!("json: number {n} overflows uint8 field {name}"))
}

pub fn string_of(obj: &Value, name: &str) -> Result<String, Error> {
    match present(obj, name) {
        None => Ok(String::new()),
        Some(v) => v
            .as_str()
            .map(str::to_owned)
            .ok_or_else(|| wrong(name, "string")),
    }
}

pub fn id_of(obj: &Value, name: &str) -> Result<Id, Error> {
    match present(obj, name) {
        None => Ok(ids::EMPTY),
        Some(v) => {
            let s = v.as_str().ok_or_else(|| wrong(name, "ids.ID"))?;
            ids::id_from_str(s)
        }
    }
}

pub fn node_id_of(obj: &Value, name: &str) -> Result<NodeId, Error> {
    match present(obj, name) {
        None => Ok([0u8; 20]),
        Some(v) => {
            let s = v.as_str().ok_or_else(|| wrong(name, "ids.NodeID"))?;
            ids::node_id_from_str(s)
        }
    }
}

pub fn node_ids_of(obj: &Value, name: &str) -> Result<Option<Vec<NodeId>>, Error> {
    match present(obj, name) {
        None => Ok(None),
        Some(v) => {
            let a = v.as_array().ok_or_else(|| wrong(name, "[]ids.NodeID"))?;
            let mut out = Vec::with_capacity(a.len());
            for e in a {
                let s = e.as_str().ok_or_else(|| wrong(name, "ids.NodeID"))?;
                out.push(ids::node_id_from_str(s)?);
            }
            Ok(Some(out))
        }
    }
}

pub fn strings_of(obj: &Value, name: &str) -> Result<Option<Vec<String>>, Error> {
    match present(obj, name) {
        None => Ok(None),
        Some(v) => {
            let a = v.as_array().ok_or_else(|| wrong(name, "[]string"))?;
            let mut out = Vec::with_capacity(a.len());
            for e in a {
                out.push(e.as_str().ok_or_else(|| wrong(name, "string"))?.to_owned());
            }
            Ok(Some(out))
        }
    }
}

/// A Go `[]byte`, which travels as base64 and reads back as `None` when the
/// member is `null` — the nil slice, which writes as `null` again.
pub fn bytes_of(obj: &Value, name: &str) -> Result<Option<Vec<u8>>, Error> {
    match present(obj, name) {
        None => Ok(None),
        Some(v) => {
            let s = v.as_str().ok_or_else(|| wrong(name, "[]byte"))?;
            base64::engine::general_purpose::STANDARD
                .decode(s)
                .map(Some)
                .map_err(|e| format!("illegal base64 data: {e}"))
        }
    }
}

/// A Go `[N]byte`, which travels as a list of numbers. Go DISCARDS elements
/// past the array's length and leaves the ones it never reached at zero, so a
/// list of the wrong length is not an error.
pub fn byte_array_of(obj: &Value, name: &str) -> Result<[u8; 32], Error> {
    let mut out = [0u8; 32];
    let v = match present(obj, name) {
        None => return Ok(out),
        Some(v) => v,
    };
    let a = v.as_array().ok_or_else(|| wrong(name, "[32]uint8"))?;
    for (i, e) in a.iter().enumerate() {
        if i >= 32 {
            break;
        }
        let n = e.as_u64().ok_or_else(|| wrong(name, "uint8"))?;
        out[i] = u8::try_from(n).map_err(|_| wrong(name, "uint8"))?;
    }
    Ok(out)
}

pub fn time_of(obj: &Value, name: &str) -> Result<Time, Error> {
    match present(obj, name) {
        None => Ok(Time::zero()),
        Some(v) => {
            let s = v.as_str().ok_or_else(|| wrong(name, "time.Time"))?;
            Time::parse(s)
        }
    }
}

pub fn array_of<'a>(obj: &'a Value, name: &str) -> Result<Option<&'a Vec<Value>>, Error> {
    match present(obj, name) {
        None => Ok(None),
        Some(v) => v.as_array().map(Some).ok_or_else(|| wrong(name, "slice")),
    }
}

pub fn map_of(obj: &Value, name: &str) -> Result<Option<Vec<(String, String)>>, Error> {
    match present(obj, name) {
        None => Ok(None),
        Some(v) => {
            let m = v
                .as_object()
                .ok_or_else(|| wrong(name, "map[string]string"))?;
            let mut out: Vec<(String, String)> = Vec::with_capacity(m.len());
            for (k, val) in m {
                out.push((
                    k.clone(),
                    val.as_str().ok_or_else(|| wrong(name, "string"))?.to_owned(),
                ));
            }
            Ok(Some(out))
        }
    }
}

// ---------------------------------------------------------------------------
// Writing.
// ---------------------------------------------------------------------------

/// One JSON object, written member by member in declaration order.
pub struct Writer {
    out: String,
    first: bool,
}

impl Writer {
    pub fn object() -> Writer {
        Writer { out: "{".to_owned(), first: true }
    }

    fn key(&mut self, name: &str) {
        if !self.first {
            self.out.push(',');
        }
        self.first = false;
        quote(&mut self.out, name);
        self.out.push(':');
    }

    pub fn raw(&mut self, name: &str, body: &str) {
        self.key(name);
        self.out.push_str(body);
    }

    pub fn num(&mut self, name: &str, n: i64) {
        self.raw(name, &n.to_string());
    }

    pub fn unum(&mut self, name: &str, n: u64) {
        self.raw(name, &n.to_string());
    }

    pub fn text(&mut self, name: &str, s: &str) {
        self.key(name);
        quote(&mut self.out, s);
    }

    pub fn id(&mut self, name: &str, id: &Id) {
        self.text(name, &ids::id_string(id));
    }

    pub fn node_id(&mut self, name: &str, n: &NodeId) {
        self.text(name, &ids::node_id_string(n));
    }

    pub fn time(&mut self, name: &str, t: &Time) {
        self.text(name, &t.render());
    }

    /// A Go `[]byte`: base64, or `null` when the slice is nil.
    pub fn bytes(&mut self, name: &str, b: &Option<Vec<u8>>) {
        match b {
            None => self.raw(name, "null"),
            Some(v) => self.text(
                name,
                &base64::engine::general_purpose::STANDARD.encode(v),
            ),
        }
    }

    /// A Go `[N]byte`: a list of numbers, always written.
    pub fn byte_array(&mut self, name: &str, b: &[u8]) {
        let body: Vec<String> = b.iter().map(|x| x.to_string()).collect();
        self.raw(name, &format!("[{}]", body.join(",")));
    }

    pub fn list(&mut self, name: &str, bodies: &[String]) {
        self.raw(name, &format!("[{}]", bodies.join(",")));
    }

    pub fn strings(&mut self, name: &str, v: &Option<Vec<String>>) {
        match v {
            None => self.raw(name, "null"),
            Some(items) => {
                let bodies: Vec<String> = items
                    .iter()
                    .map(|s| {
                        let mut q = String::new();
                        quote(&mut q, s);
                        q
                    })
                    .collect();
                self.list(name, &bodies);
            }
        }
    }

    pub fn node_ids(&mut self, name: &str, v: &Option<Vec<NodeId>>) {
        match v {
            None => self.raw(name, "null"),
            Some(items) => {
                let bodies: Vec<String> = items
                    .iter()
                    .map(|n| {
                        let mut q = String::new();
                        quote(&mut q, &ids::node_id_string(n));
                        q
                    })
                    .collect();
                self.list(name, &bodies);
            }
        }
    }

    /// A Go map: `null` when nil, and otherwise its keys in sorted order,
    /// which is the order `encoding/json` writes them in and therefore the
    /// order the id is taken over.
    pub fn map(&mut self, name: &str, v: &Option<Vec<(String, String)>>) {
        match v {
            None => self.raw(name, "null"),
            Some(pairs) => {
                let mut sorted = pairs.clone();
                sorted.sort_by(|a, b| a.0.cmp(&b.0));
                let mut body = String::from("{");
                for (i, (k, val)) in sorted.iter().enumerate() {
                    if i > 0 {
                        body.push(',');
                    }
                    quote(&mut body, k);
                    body.push(':');
                    quote(&mut body, val);
                }
                body.push('}');
                self.raw(name, &body);
            }
        }
    }

    pub fn finish(mut self) -> String {
        self.out.push('}');
        self.out
    }
}

/// A Go JSON string. `<`, `>` and `&` are escaped because `Marshal` escapes
/// them, and a byte that starts no well-formed rune becomes U+FFFD.
pub fn quote(out: &mut String, s: &str) {
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

/// Read one value and refuse anything after it, which is what `Unmarshal`
/// does: a buffer with bytes past the value is not that value.
pub fn parse(bytes: &[u8]) -> Result<Value, Error> {
    let text = std::str::from_utf8(bytes).map_err(|_| "invalid character".to_owned())?;
    serde_json::from_str(text).map_err(|e| e.to_string())
}
