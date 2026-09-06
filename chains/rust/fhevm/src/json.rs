// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Reading an operation payload, by exactly the rules Go reads one by.
//!
//! An F payload is JSON, and the chain persists the transaction verbatim, so a
//! decoder that keeps what it is given and ignores what it does not recognise
//! is a byte channel: a megabyte of ciphertext body in a `body` member used to
//! decode fine, cost nothing extra, and come back out of the block store.
//! Go closes that with `Decoder.DisallowUnknownFields` plus a check for content
//! after the value, and this closes it the same way — [`Fields`] hands out the
//! members a schema asks for and refuses whatever is left over.
//!
//! The four shapes Go's `encoding/json` gives these types, and why each is what
//! it is:
//!
//! - a `[N]byte` is a JSON ARRAY OF NUMBERS. Not hex, not base64 — Go treats a
//!   byte ARRAY as an array of integers and only a byte SLICE as base64. A hex
//!   string in a digest member is therefore a type error, not a value error.
//! - a `[]byte` is standard base64.
//! - an `ids.ShortID` is a bare CB58 word and an `ids.NodeID` a prefixed one,
//!   because each has its own `MarshalJSON`.
//! - `null` is not a value: Go leaves the field at its zero and reports nothing.
//!
//! Three more of Go's rules are here because leaving them out would be a rule
//! this chain has and Go does not. A member is matched by its exact tag first
//! and by Unicode simple case folding second, and two members naming one field
//! are both resolved and the later one written. An array longer than the Go
//! array it decodes into is not an error — the extra elements are discarded,
//! and a shorter one leaves the rest zero. And a base64 word broken across a
//! line is the same word: `encoding/base64` ignores CR and LF.

use serde_json::{Map, Value};

use crate::error::{Code, Error, Result};
use crate::id::{self, Account, NodeId};

/// Read the whole payload as one JSON value, refusing anything after it.
///
/// Go reads a payload through a `Decoder` and then asks `dec.More()`, and More
/// answers "is there another ELEMENT": it peeks the next byte that is not space
/// and reports false for `]` and `}`. So a stray closing bracket after a
/// complete value is NOT trailing content, while a comma or a second document
/// is. A reader that refused everything after the value would refuse two
/// payloads the reference admits.
pub fn value(payload: &[u8], op: &str) -> Result<Value> {
    let mut stream = serde_json::Deserializer::from_slice(payload).into_iter::<Value>();
    let v = match stream.next() {
        Some(Ok(v)) => v,
        Some(Err(e)) => return Err(bad(op, e.to_string())),
        None => return Err(bad(op, "unexpected end of JSON input")),
    };
    let rest = &payload[stream.byte_offset()..];
    match rest.iter().find(|b| !matches!(b, b' ' | b'\t' | b'\r' | b'\n')) {
        None | Some(b']') | Some(b'}') => Ok(v),
        Some(_) => Err(bad(op, "trailing content after the payload")),
    }
}

/// Go's `foldName`: a name folded so that two names equal under Unicode simple
/// case folding fold alike.
///
/// ASCII is uppercased and every other rune goes to the SMALLEST code point in
/// its fold class. It is not ASCII case folding and it is not `to_lowercase`:
/// U+017F (long s) folds onto `S` and U+212A (Kelvin sign) onto `K`, so `ſize`
/// names Size and `publicKey` spelled with a Kelvin sign names PublicKey.
fn fold(name: &str) -> String {
    name.chars().map(fold_char).collect()
}

fn fold_char(c: char) -> char {
    if c.is_ascii() {
        return c.to_ascii_uppercase();
    }
    // The class is the closure of the rune under the single-character simple
    // case mappings; Go reaches the same set by walking `unicode.SimpleFold`'s
    // orbit until it comes back round. A mapping that yields more than one
    // character is not a simple one and takes nothing into the class — which is
    // why ß, whose uppercase is SS, folds to itself in both.
    let mut class = vec![c];
    let mut i = 0;
    while i < class.len() {
        let r = class[i];
        for m in [one(r.to_uppercase()), one(r.to_lowercase())].into_iter().flatten() {
            if !class.contains(&m) {
                class.push(m);
            }
        }
        i += 1;
    }
    class.into_iter().min().unwrap_or(c)
}

fn one(mut mapping: impl Iterator<Item = char>) -> Option<char> {
    let first = mapping.next()?;
    mapping.next().is_none().then_some(first)
}

fn bad(op: &str, why: impl std::fmt::Display) -> Error {
    Error::detail(Code::InvalidPayload, format!("{op}: {why}"))
}

/// The members of one JSON object, handed out by name and counted.
///
/// Every schema below reads its members through this and then calls
/// [`Fields::done`], which is what refuses a member the schema does not
/// describe. A schema that forgot to call it would be the byte channel again,
/// so nothing here returns a payload without it.
#[derive(Debug)]
pub struct Fields<'a> {
    op: &'a str,
    map: Map<String, Value>,
}

impl<'a> Fields<'a> {
    /// The object at the top of a payload. A payload that is not an object at
    /// all is refused here, naming the shape it was.
    pub fn open(v: Value, op: &'a str) -> Result<Fields<'a>> {
        match v {
            Value::Object(map) => Ok(Fields { op, map }),
            // `null` is not a value. Go leaves the struct at its zero and
            // reports nothing, so what refuses the transaction is whatever rule
            // the zero payload then breaks — which is a different rule per
            // operation, and naming the wrong one is what refusing null here
            // would do.
            Value::Null => Ok(Fields { op, map: Map::new() }),
            other => Err(bad(op, format!("cannot unmarshal {} into an object", kind(&other)))),
        }
    }

    /// Take one member.
    ///
    /// Go resolves each JSON key on its own — by the exact tag first and by
    /// [`fold`] second — and then writes what it found, so when two members
    /// name one field the LATER one lands whichever way each of them matched.
    /// Preferring the exact match would read a different value out of the same
    /// bytes.
    fn take(&mut self, name: &str) -> Option<Value> {
        let folded = fold(name);
        let naming: Vec<String> = self
            .map
            .keys()
            .filter(|k| k.as_str() == name || fold(k) == folded)
            .cloned()
            .collect();
        let last = naming.last()?.clone();
        let mut taken = None;
        for k in &naming {
            // A shift keeps what is left in the order the document wrote it,
            // which is the order this rule is about.
            let v = self.map.shift_remove(k);
            if *k == last {
                taken = v;
            }
        }
        taken
    }

    /// Every member the schema asked for has been taken; anything still here is
    /// a member the schema does not describe.
    pub fn done(self) -> Result<()> {
        match self.map.keys().next() {
            None => Ok(()),
            Some(k) => Err(bad(self.op, format!("unknown field {k:?}"))),
        }
    }

    /// Take what the schema asked for and IGNORE the rest — which is what the
    /// reference does for a genesis and never for an operation payload. The
    /// difference is deliberate and is stated at the one call site.
    pub fn ignore_rest(self) {}

    /// A map of hex address to amount: the shape a genesis allocation takes.
    ///
    /// Hex here, not CB58. A funding entry is a map KEY, and the reference
    /// parses one with its own hex reader rather than through the address
    /// type's text form, so the two shapes are not interchangeable.
    pub fn hex_amounts(&mut self, name: &str) -> Result<Vec<(Account, u64)>> {
        let Some(v) = self.take(name) else { return Ok(Vec::new()) };
        if v.is_null() {
            return Ok(Vec::new());
        }
        let map = match v {
            Value::Object(map) => map,
            other => return Err(self.wrong(name, &other, "map[string]uint64")),
        };
        let mut out = Vec::with_capacity(map.len());
        for (k, val) in map {
            let bytes = id::unhex(k.strip_prefix("0x").unwrap_or(&k))
                .ok_or_else(|| bad(self.op, format!("alloc {k:?}: not hex")))?;
            let acct: Account = bytes.as_slice().try_into().map_err(|_| {
                bad(
                    self.op,
                    format!("alloc {k:?}: address must be 20 bytes, got {}", bytes.len()),
                )
            })?;
            let amount = val
                .as_u64()
                .ok_or_else(|| bad(self.op, format!("alloc {k:?}: not an amount")))?;
            out.push((acct, amount));
        }
        Ok(out)
    }

    fn wrong(&self, name: &str, v: &Value, want: &str) -> Error {
        bad(self.op, format!("cannot unmarshal {} into field {name} of type {want}", kind(v)))
    }

    /// A fixed-width byte array, written as an array of numbers.
    pub fn bytes<const N: usize>(&mut self, name: &str) -> Result<[u8; N]> {
        let mut out = [0u8; N];
        let Some(v) = self.take(name) else { return Ok(out) };
        if v.is_null() {
            return Ok(out);
        }
        let Value::Array(xs) = &v else {
            return Err(self.wrong(name, &v, &format!("[{N}]uint8")));
        };
        // Go discards elements past the end of the array it is filling and
        // leaves a short one's tail at zero. Neither is an error there.
        for (i, x) in xs.iter().take(N).enumerate() {
            out[i] = match x.as_u64() {
                Some(n) if n <= u8::MAX as u64 => n as u8,
                _ => return Err(self.wrong(name, x, "uint8")),
            };
        }
        Ok(out)
    }

    /// A variable-length byte string, written as standard base64.
    pub fn base64(&mut self, name: &str) -> Result<Vec<u8>> {
        let Some(v) = self.take(name) else { return Ok(Vec::new()) };
        if v.is_null() {
            return Ok(Vec::new());
        }
        let Value::String(s) = &v else {
            return Err(self.wrong(name, &v, "[]uint8"));
        };
        use base64::Engine;
        // `encoding/base64` ignores CR and LF wherever they fall, so a word
        // broken across a line is the same word and decodes to the same bytes.
        // A reader that refused it would read a whole proposal as one with no
        // key in it.
        let unwrapped: String = s.chars().filter(|c| *c != '\r' && *c != '\n').collect();
        base64::engine::general_purpose::STANDARD
            .decode(&unwrapped)
            .map_err(|e| bad(self.op, format!("{name}: {e}")))
    }

    /// An account, written as a bare CB58 word.
    pub fn account(&mut self, name: &str) -> Result<Account> {
        let Some(v) = self.take(name) else { return Ok([0u8; 20]) };
        if v.is_null() {
            return Ok([0u8; 20]);
        }
        let Value::String(s) = &v else {
            return Err(self.wrong(name, &v, "ids.ShortID"));
        };
        id::account_from_str(s).map_err(|e| bad(self.op, format!("{name}: {e}")))
    }

    /// A committee seat, written as a CB58 word behind `NodeID-`.
    pub fn node_id(&mut self, name: &str) -> Result<NodeId> {
        let Some(v) = self.take(name) else { return Ok([0u8; 20]) };
        if v.is_null() {
            return Ok([0u8; 20]);
        }
        let Value::String(s) = &v else {
            return Err(self.wrong(name, &v, "ids.NodeID"));
        };
        id::node_id_from_str(s).map_err(|e| bad(self.op, format!("{name}: {e}")))
    }

    pub fn string(&mut self, name: &str) -> Result<String> {
        let Some(v) = self.take(name) else { return Ok(String::new()) };
        if v.is_null() {
            return Ok(String::new());
        }
        match v {
            Value::String(s) => Ok(s),
            other => Err(self.wrong(name, &other, "string")),
        }
    }

    pub fn u64(&mut self, name: &str) -> Result<u64> {
        self.unsigned(name, u64::MAX, "uint64")
    }

    pub fn u32(&mut self, name: &str) -> Result<u32> {
        self.unsigned(name, u32::MAX as u64, "uint32").map(|n| n as u32)
    }

    pub fn u8(&mut self, name: &str) -> Result<u8> {
        self.unsigned(name, u8::MAX as u64, "uint8").map(|n| n as u8)
    }

    fn unsigned(&mut self, name: &str, max: u64, want: &str) -> Result<u64> {
        let Some(v) = self.take(name) else { return Ok(0) };
        if v.is_null() {
            return Ok(0);
        }
        match v.as_u64() {
            Some(n) if n <= max => Ok(n),
            _ => Err(self.wrong(name, &v, want)),
        }
    }

    pub fn i64(&mut self, name: &str) -> Result<i64> {
        self.signed(name, i64::MIN, i64::MAX, "int64")
    }

    /// Go's `int`, which is 64-bit everywhere this chain runs.
    pub fn int(&mut self, name: &str) -> Result<i64> {
        self.signed(name, i64::MIN, i64::MAX, "int")
    }

    fn signed(&mut self, name: &str, lo: i64, hi: i64, want: &str) -> Result<i64> {
        let Some(v) = self.take(name) else { return Ok(0) };
        if v.is_null() {
            return Ok(0);
        }
        match v.as_i64() {
            Some(n) if n >= lo && n <= hi => Ok(n),
            _ => Err(self.wrong(name, &v, want)),
        }
    }

    /// A list of objects, each read by `each` through its own [`Fields`].
    pub fn list<T>(
        &mut self,
        name: &str,
        want: &str,
        mut each: impl FnMut(Fields<'_>) -> Result<T>,
    ) -> Result<Vec<T>> {
        let Some(v) = self.take(name) else { return Ok(Vec::new()) };
        if v.is_null() {
            return Ok(Vec::new());
        }
        let xs = match v {
            Value::Array(xs) => xs,
            other => return Err(self.wrong(name, &other, want)),
        };
        let mut out = Vec::with_capacity(xs.len());
        for x in xs {
            out.push(each(Fields::open(x, self.op)?)?);
        }
        Ok(out)
    }
}

/// What Go calls each JSON shape in a type error.
fn kind(v: &Value) -> &'static str {
    match v {
        Value::Null => "null",
        Value::Bool(_) => "bool",
        Value::Number(_) => "number",
        Value::String(_) => "string",
        Value::Array(_) => "array",
        Value::Object(_) => "object",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn open(text: &str) -> Fields<'static> {
        Fields::open(value(text.as_bytes(), "t").expect("json"), "t").expect("object")
    }

    #[test]
    fn a_byte_array_is_an_array_of_numbers_and_not_a_string() {
        let mut f = open(r#"{"digest":[1,2,3]}"#);
        let d: [u8; 4] = f.bytes("digest").unwrap();
        assert_eq!(d, [1, 2, 3, 0]);
        f.done().unwrap();

        // The shape F_TX_UNKNOWN_PAYLOAD_FIELD carries: hex in a digest member.
        let mut f = open(r#"{"digest":"2121"}"#);
        let e = f.bytes::<32>("digest").unwrap_err();
        assert_eq!(e.code, Code::InvalidPayload);
        assert!(e.detail.contains("cannot unmarshal string"), "{}", e.detail);
    }

    #[test]
    fn a_longer_array_is_trimmed_and_a_shorter_one_left_zero() {
        // Go discards past the end and zero-fills the tail; neither is an error
        // there, so neither is one here.
        let mut f = open(r#"{"a":[1,2,3,4,5]}"#);
        assert_eq!(f.bytes::<3>("a").unwrap(), [1, 2, 3]);
        let mut f = open(r#"{"a":[1]}"#);
        assert_eq!(f.bytes::<3>("a").unwrap(), [1, 0, 0]);
    }

    #[test]
    fn a_member_the_schema_does_not_describe_is_refused() {
        let mut f = open(r#"{"reason":"x","body":"AAAA"}"#);
        assert_eq!(f.string("reason").unwrap(), "x");
        let e = f.done().unwrap_err();
        assert!(e.detail.contains("unknown field"), "{}", e.detail);
    }

    #[test]
    fn content_after_the_value_is_refused() {
        let e = value(br#"{"a":1} {"b":2}"#, "t").unwrap_err();
        assert!(e.detail.contains("trailing content"), "{}", e.detail);
        assert!(value(br#"{"a":1} ,"#, "t").is_err(), "a comma is another element");
    }

    /// `More` answers "is there another ELEMENT", so a stray closing bracket
    /// after a complete value is not content and the payload stands.
    #[test]
    fn a_stray_closing_bracket_is_not_trailing_content() {
        assert_eq!(value(br#"{"a":1}}"#, "t").unwrap(), value(br#"{"a":1}"#, "t").unwrap());
        assert_eq!(value(br#"{"a":1}]"#, "t").unwrap(), value(br#"{"a":1}"#, "t").unwrap());
    }

    #[test]
    fn a_member_matches_its_exact_name_first_and_its_case_second() {
        // Go's decoder does both, in that order.
        let mut f = open(r#"{"Reason":"loud"}"#);
        assert_eq!(f.string("reason").unwrap(), "loud");
        f.done().unwrap();
    }

    /// The fold is Unicode's, not ASCII's. Both of these name a field whose tag
    /// is spelled in ASCII, and an ASCII-only fold reads neither.
    #[test]
    fn a_member_folds_by_unicode_and_not_by_ascii_case() {
        let mut f = open("{\"\u{17F}ize\":4096}");
        assert_eq!(f.u64("size").unwrap(), 4096);
        f.done().unwrap();

        let mut f = open("{\"public\u{212A}ey\":\"d3d3\"}");
        assert_eq!(f.base64("publicKey").unwrap(), vec![0x77, 0x77, 0x77]);
        f.done().unwrap();

        assert_eq!(fold("\u{17F}ize"), fold("Size"));
        assert_eq!(fold("public\u{212A}ey"), fold("publicKey"));
        // A mapping that is not a single character takes nothing into the
        // class, so ß folds to itself rather than to SS.
        assert_eq!(fold("ß"), "ß");
        // U+212B ANGSTROM SIGN reaches U+00C5 only through its lowercase, which
        // is why the class is a closure and not one mapping.
        assert_eq!(fold("\u{212B}"), "\u{C5}");
    }

    /// Two members naming one field: both are resolved and the later one is
    /// written, whichever way each of them matched.
    #[test]
    fn the_later_of_two_members_naming_one_field_wins() {
        let mut f = open(r#"{"size":4096,"SIZE":8192}"#);
        assert_eq!(f.u64("size").unwrap(), 8192);
        f.done().unwrap();

        let mut f = open(r#"{"SIZE":8192,"size":4096}"#);
        assert_eq!(f.u64("size").unwrap(), 4096);
        f.done().unwrap();
    }

    #[test]
    fn a_base64_word_broken_across_a_line_is_the_same_word() {
        let mut f = open("{\"k\":\"d3d3\"}");
        let whole = f.base64("k").unwrap();
        let mut f = open("{\"k\":\"d3\\r\\nd3\"}");
        assert_eq!(f.base64("k").unwrap(), whole);
    }

    /// A literal null where a struct belongs is the ZERO struct and not a
    /// refusal, so what refuses the transaction is the rule the zero payload
    /// breaks — a different rule per operation.
    #[test]
    fn a_null_payload_is_the_zero_struct() {
        let mut f = Fields::open(value(b"null", "t").unwrap(), "t").expect("null is a struct");
        assert_eq!(f.u64("size").unwrap(), 0);
        assert_eq!(f.bytes::<32>("digest").unwrap(), [0u8; 32]);
        f.done().unwrap();
    }

    #[test]
    fn null_is_the_zero_value_and_not_a_refusal() {
        let mut f = open(r#"{"n":null,"s":null,"b":null,"a":null}"#);
        assert_eq!(f.u64("n").unwrap(), 0);
        assert_eq!(f.string("s").unwrap(), "");
        assert_eq!(f.base64("b").unwrap(), Vec::<u8>::new());
        assert_eq!(f.bytes::<4>("a").unwrap(), [0u8; 4]);
        f.done().unwrap();
    }

    #[test]
    fn an_absent_member_is_the_zero_value() {
        let mut f = open("{}");
        assert_eq!(f.u32("operations").unwrap(), 0);
        assert_eq!(f.i64("expiry").unwrap(), 0);
        assert_eq!(f.bytes::<32>("digest").unwrap(), [0u8; 32]);
        f.done().unwrap();
    }

    #[test]
    fn a_number_out_of_its_width_is_refused() {
        let mut f = open(r#"{"t":256}"#);
        assert!(f.u8("t").is_err());
        let mut f = open(r#"{"t":1.5}"#);
        assert!(f.u8("t").is_err());
        let mut f = open(r#"{"t":-1}"#);
        assert!(f.u32("t").is_err());
    }

    #[test]
    fn a_negative_level_is_a_value_and_not_a_type_error() {
        // `Level int` takes -1 happily; refusing it is the CHAIN's rule, not
        // the decoder's, and the two must not be confused.
        let mut f = open(r#"{"level":-1}"#);
        assert_eq!(f.int("level").unwrap(), -1);
    }

    #[test]
    fn base64_and_cb58_are_read_the_way_go_writes_them() {
        let mut f = open(r#"{"k":"d3d3","g":"NG42sa8bZqRUjcaoq9igh5toYw8mXmw7H"}"#);
        assert_eq!(f.base64("k").unwrap(), vec![0x77, 0x77, 0x77]);
        assert_eq!(id::hex(&f.account("g").unwrap()), "e933697f7a3d671b8c294452465230d4d433d337");
        f.done().unwrap();
    }

    #[test]
    fn a_genesis_allocation_is_keyed_by_hex_and_not_by_cb58() {
        let mut f = open(r#"{"alloc":{"e933697f7a3d671b8c294452465230d4d433d337":1099511627776}}"#);
        let a = f.hex_amounts("alloc").unwrap();
        assert_eq!(a.len(), 1);
        assert_eq!(id::hex(&a[0].0), "e933697f7a3d671b8c294452465230d4d433d337");
        assert_eq!(a[0].1, 1_099_511_627_776);
        f.done().unwrap();

        let mut f = open(r#"{"alloc":{"NG42sa8bZqRUjcaoq9igh5toYw8mXmw7H":1}}"#);
        assert!(f.hex_amounts("alloc").is_err(), "a CB58 word is not a hex key");

        let mut f = open(r#"{"alloc":{"0xe933697f7a3d671b8c294452465230d4d433d337":7}}"#);
        assert_eq!(f.hex_amounts("alloc").unwrap()[0].1, 7);

        let mut f = open(r#"{"alloc":{"e933":1}}"#);
        assert!(f.hex_amounts("alloc").unwrap_err().detail.contains("must be 20 bytes"));
    }

    #[test]
    fn a_payload_that_is_not_an_object_is_refused() {
        let e = Fields::open(value(b"[1,2]", "t").unwrap(), "t").unwrap_err();
        assert!(e.detail.contains("cannot unmarshal array"), "{}", e.detail);
    }
}
