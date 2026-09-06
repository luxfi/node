// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! ZAP — the arena message format the F-Chain is written in.
//!
//! One buffer, read without parsing: a 16-byte header names the root object's
//! offset, and every field is either inline at a fixed offset or a relative
//! pointer to somewhere else in the same buffer.
//!
//! ```text
//! ┌────────────────────────────────────────────┐
//! │ Header (16 bytes)                          │
//! │  ├─ Magic   4B "ZAP\0"                     │
//! │  ├─ Version 2B  1 (legacy) or 2 (current)  │
//! │  ├─ Flags   2B                             │
//! │  ├─ Root    4B  offset of the root object  │
//! │  └─ Size    4B  total message size         │
//! ├────────────────────────────────────────────┤
//! │ Data segment: objects, lists, byte tails   │
//! └────────────────────────────────────────────┘
//! ```
//!
//! Every integer is little-endian. Every pointer is relative TO THE FIELD THAT
//! HOLDS IT, so a message can be copied anywhere without rewriting it.
//!
//! This is the format Go writes in `luxfi/zap` (`builder.go`, `zap.go`) — not
//! the length-prefixed transport frame that shares the name. A transaction
//! written with the other one is a transaction no Go peer can read.
//!
//! WHY THE READER NEVER PANICS. Every accessor is bounds-checked and answers
//! the zero value on a short or crooked buffer, because these bytes arrive from
//! whoever wants to send them. Two rules beyond plain bounds carry weight: a
//! byte tail's pointer is UNSIGNED, so a crafted offset cannot alias back into
//! the fixed section, and no pointer of any kind may land inside the 16-byte
//! header, whose contents are not a legitimate payload.
//!
//! A reader that answered zero where Go raised would be the quieter of two
//! forks, so the zeros are the SAME zeros: this port follows Go accessor for
//! accessor, including which malformed shapes read as absent rather than as an
//! error.

/// The header, in bytes.
pub const HEADER_SIZE: usize = 16;

/// The four bytes every message starts with.
pub const MAGIC: [u8; 4] = [b'Z', b'A', b'P', 0];

/// The legacy schema. Accepted on read; never emitted.
pub const VERSION1: u16 = 1;
/// The current schema, and what [`Builder`] writes.
pub const VERSION2: u16 = 2;

/// Objects and lists start on this boundary.
pub const ALIGNMENT: usize = 8;

/// What a buffer can be refused for. The words are Go's, because they travel
/// into a conformance note beside the verdict.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    InvalidMagic,
    InvalidVersion,
    BufferTooSmall,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::InvalidMagic => write!(f, "zap: invalid magic bytes"),
            Error::InvalidVersion => write!(f, "zap: unsupported version"),
            Error::BufferTooSmall => write!(f, "zap: buffer too small"),
        }
    }
}

impl std::error::Error for Error {}

fn le16(b: &[u8], at: usize) -> u16 {
    u16::from_le_bytes([b[at], b[at + 1]])
}

fn le32(b: &[u8], at: usize) -> u32 {
    u32::from_le_bytes([b[at], b[at + 1], b[at + 2], b[at + 3]])
}

// ---------------------------------------------------------------- writing --

/// Writes a message. Offsets handed back by [`Builder::start_object`] and
/// [`Builder::start_list`] are absolute positions in the buffer under
/// construction; a pointer field turns one into a relative offset when it is
/// set.
#[derive(Debug)]
pub struct Builder {
    buf: Vec<u8>,
    pos: usize,
    root: usize,
}

impl Builder {
    /// A builder over a buffer of at least `capacity` bytes, header written.
    pub fn new(capacity: usize) -> Self {
        let cap = if capacity < HEADER_SIZE { 256 } else { capacity };
        let mut buf = vec![0u8; cap];
        buf[0..4].copy_from_slice(&MAGIC);
        buf[4..6].copy_from_slice(&VERSION2.to_le_bytes());
        Builder { buf, pos: HEADER_SIZE, root: 0 }
    }

    fn grow(&mut self, n: usize) {
        if self.pos + n <= self.buf.len() {
            return;
        }
        let mut cap = self.buf.len() * 2;
        if cap < self.pos + n {
            cap = self.pos + n;
        }
        self.buf.resize(cap, 0);
    }

    fn align(&mut self) {
        let pad = (ALIGNMENT - (self.pos % ALIGNMENT)) % ALIGNMENT;
        self.grow(pad);
        for _ in 0..pad {
            self.buf[self.pos] = 0;
            self.pos += 1;
        }
    }

    /// Start an object, RESERVING its whole fixed section up front. Reserving
    /// eagerly is what lets a variable field append its tail immediately after
    /// the fixed section and patch its own pointer on the spot, with no
    /// deferred offset list to replay.
    pub fn start_object(&mut self, data_size: usize) -> Object {
        self.align();
        let start = self.pos;
        let ob = Object { start };
        ob.reserve(self, data_size);
        ob
    }

    /// Start a list. Elements are appended by the `add_*` methods; the stride
    /// is implicit in which one the caller invokes.
    pub fn start_list(&mut self) -> List {
        self.align();
        List { start: self.pos, count: 0 }
    }

    /// Finish the message: stamp the root offset and the total size.
    pub fn finish(mut self, root: usize) -> Vec<u8> {
        self.root = root;
        self.buf[8..12].copy_from_slice(&(self.root as u32).to_le_bytes());
        self.buf[12..16].copy_from_slice(&(self.pos as u32).to_le_bytes());
        self.buf.truncate(self.pos);
        self.buf
    }
}

/// An object under construction: a fixed section at `start`, with variable
/// fields appended after it.
#[derive(Clone, Copy, Debug)]
pub struct Object {
    start: usize,
}

impl Object {
    /// Where this object begins. Handed to [`Builder::finish`] as the root, or
    /// to a parent's pointer field.
    pub fn offset(self) -> usize {
        self.start
    }

    fn reserve(self, b: &mut Builder, end: usize) {
        let need = self.start + end;
        if need > b.pos {
            b.grow(need - b.pos);
            for i in b.pos..need {
                b.buf[i] = 0;
            }
            b.pos = need;
        }
    }

    pub fn set_u8(self, b: &mut Builder, at: usize, v: u8) {
        self.reserve(b, at + 1);
        b.buf[self.start + at] = v;
    }

    pub fn set_u32(self, b: &mut Builder, at: usize, v: u32) {
        self.reserve(b, at + 4);
        b.buf[self.start + at..self.start + at + 4].copy_from_slice(&v.to_le_bytes());
    }

    pub fn set_u64(self, b: &mut Builder, at: usize, v: u64) {
        self.reserve(b, at + 8);
        b.buf[self.start + at..self.start + at + 8].copy_from_slice(&v.to_le_bytes());
    }

    pub fn set_i64(self, b: &mut Builder, at: usize, v: i64) {
        self.set_u64(b, at, v as u64);
    }

    /// Copy `v` inline at `at`. The counterpart of [`Reader::bytes_fixed`].
    /// An empty `v` is a no-op, exactly as Go's `SetBytesFixed` is: the slot is
    /// already the zero value.
    pub fn set_bytes_fixed(self, b: &mut Builder, at: usize, v: &[u8]) {
        if v.is_empty() {
            return;
        }
        self.reserve(b, at + v.len());
        b.buf[self.start + at..self.start + at + v.len()].copy_from_slice(v);
    }

    /// Append `v` after the fixed section and write this field's
    /// (relative offset, length) pair. An empty `v` writes the null pair.
    pub fn set_bytes(self, b: &mut Builder, at: usize, v: &[u8]) {
        let field = self.start + at;
        if v.is_empty() {
            b.buf[field..field + 4].copy_from_slice(&0u32.to_le_bytes());
            b.buf[field + 4..field + 8].copy_from_slice(&0u32.to_le_bytes());
            return;
        }
        let data = b.pos;
        b.grow(v.len());
        b.buf[b.pos..b.pos + v.len()].copy_from_slice(v);
        b.pos += v.len();

        let rel = (data - field) as u32;
        b.buf[field..field + 4].copy_from_slice(&rel.to_le_bytes());
        b.buf[field + 4..field + 8].copy_from_slice(&(v.len() as u32).to_le_bytes());
    }

    /// Point at a list already written elsewhere in the buffer. A zero offset
    /// or a zero length writes the null pair.
    pub fn set_list(self, b: &mut Builder, at: usize, list: usize, len: usize) {
        self.reserve(b, at + 8);
        let field = self.start + at;
        if list == 0 || len == 0 {
            b.buf[field..field + 4].copy_from_slice(&0u32.to_le_bytes());
            b.buf[field + 4..field + 8].copy_from_slice(&0u32.to_le_bytes());
            return;
        }
        let rel = (list as i64 - field as i64) as i32 as u32;
        b.buf[field..field + 4].copy_from_slice(&rel.to_le_bytes());
        b.buf[field + 4..field + 8].copy_from_slice(&(len as u32).to_le_bytes());
    }
}

/// A list under construction.
#[derive(Clone, Copy, Debug)]
pub struct List {
    start: usize,
    count: usize,
}

impl List {
    pub fn add_u32(&mut self, b: &mut Builder, v: u32) {
        b.grow(4);
        b.buf[b.pos..b.pos + 4].copy_from_slice(&v.to_le_bytes());
        b.pos += 4;
        self.count += 1;
    }

    /// Where the list begins, and how many elements it holds.
    pub fn finish(self) -> (usize, usize) {
        (self.start, self.count)
    }
}

// ---------------------------------------------------------------- reading --

/// A parsed message. Holds the buffer TRUNCATED to the size its header
/// declares, which is what every bound below is measured against.
#[derive(Clone, Copy, Debug)]
pub struct Message<'a> {
    data: &'a [u8],
}

/// Parse a message. Accepts both wire versions, as Go does; refuses a declared
/// size below the header or past the buffer.
pub fn parse(data: &[u8]) -> Result<Message<'_>, Error> {
    if data.len() < HEADER_SIZE {
        return Err(Error::BufferTooSmall);
    }
    if data[0..4] != MAGIC {
        return Err(Error::InvalidMagic);
    }
    let version = le16(data, 4);
    if version != VERSION1 && version != VERSION2 {
        return Err(Error::InvalidVersion);
    }
    let size = le32(data, 12) as usize;
    if size < HEADER_SIZE || size > data.len() {
        return Err(Error::BufferTooSmall);
    }
    Ok(Message { data: &data[..size] })
}

impl<'a> Message<'a> {
    /// The declared size — what this message occupies, which may be less than
    /// the buffer it was parsed from.
    pub fn size(&self) -> usize {
        self.data.len()
    }

    pub fn version(&self) -> u16 {
        le16(self.data, 4)
    }

    /// The root object.
    pub fn root(&self) -> Reader<'a> {
        Reader { data: self.data, offset: le32(self.data, 8) as usize }
    }
}

/// A view onto one object. Every accessor answers the zero value rather than
/// raising, because these bytes arrive from whoever wants to send them.
#[derive(Clone, Copy, Debug)]
pub struct Reader<'a> {
    data: &'a [u8],
    offset: usize,
}

impl<'a> Reader<'a> {
    pub fn u8(&self, at: usize) -> u8 {
        let pos = self.offset + at;
        if pos >= self.data.len() {
            return 0;
        }
        self.data[pos]
    }

    pub fn u32(&self, at: usize) -> u32 {
        let pos = self.offset + at;
        if pos + 4 > self.data.len() {
            return 0;
        }
        le32(self.data, pos)
    }

    pub fn u64(&self, at: usize) -> u64 {
        let pos = self.offset + at;
        if pos + 8 > self.data.len() {
            return 0;
        }
        u64::from_le_bytes(self.data[pos..pos + 8].try_into().unwrap())
    }

    pub fn i64(&self, at: usize) -> i64 {
        self.u64(at) as i64
    }

    /// `n` inline bytes at `at`. Empty if the span falls outside the buffer.
    pub fn bytes_fixed(&self, at: usize, n: usize) -> &'a [u8] {
        if n == 0 {
            return &[];
        }
        let pos = self.offset + at;
        if pos + n > self.data.len() {
            return &[];
        }
        &self.data[pos..pos + n]
    }

    /// A variable-length tail.
    ///
    /// The relative offset is an UNSIGNED forward pointer. A signed one would
    /// let a crafted offset alias back into the fixed section, and a tail that
    /// landed inside the wire header would be reading Magic/Version/Root/Size
    /// as a payload — both are refused as absent.
    pub fn bytes(&self, at: usize) -> &'a [u8] {
        let pos = self.offset + at;
        if pos + 4 > self.data.len() {
            return &[];
        }
        let rel = le32(self.data, pos) as usize;
        if rel == 0 {
            return &[];
        }
        if pos + 8 > self.data.len() {
            return &[];
        }
        let len = le32(self.data, pos + 4) as usize;
        let abs = pos + rel;
        if abs < HEADER_SIZE || abs + len > self.data.len() {
            return &[];
        }
        &self.data[abs..abs + len]
    }

    /// A list, with the caller's per-element stride applied as a bound up
    /// front: a declared length that could not fit in the remaining buffer is
    /// refused here rather than at every element.
    pub fn list(&self, at: usize, stride: usize) -> Slots<'a> {
        let empty = Slots { data: self.data, offset: 0, len: 0 };
        let pos = self.offset + at;
        if pos + 8 > self.data.len() {
            return empty;
        }
        let rel = le32(self.data, pos) as i32;
        if rel == 0 {
            return empty;
        }
        let len = le32(self.data, pos + 4) as u64;
        let abs = (pos as i64 + rel as i64) as usize;
        if abs < HEADER_SIZE || abs >= self.data.len() {
            return empty;
        }
        let room = (self.data.len() - abs) as u64;
        if stride > 0 {
            if len * stride as u64 > room {
                return empty;
            }
        } else if len > self.data.len() as u64 {
            return empty;
        }
        Slots { data: self.data, offset: abs, len: len as usize }
    }
}

/// A view onto one list.
#[derive(Clone, Copy, Debug)]
pub struct Slots<'a> {
    data: &'a [u8],
    offset: usize,
    len: usize,
}

impl Slots<'_> {
    /// The element count as encoded on the wire. It is bounded by the buffer
    /// and by the stride the reader was given, and by nothing else — never
    /// pre-allocate against it.
    pub fn len(&self) -> usize {
        self.len
    }

    pub fn is_empty(&self) -> bool {
        self.len == 0
    }

    pub fn u32(&self, i: usize) -> u32 {
        if i >= self.len {
            return 0;
        }
        let pos = self.offset + i * 4;
        if pos + 4 > self.data.len() {
            return 0;
        }
        le32(self.data, pos)
    }
}

/// The length of the leading self-delimiting message in `b` — its header's own
/// size field. This is the split point between a transaction's signing prefix
/// and the appended auth object.
pub fn message_len(b: &[u8]) -> Option<usize> {
    if b.len() < HEADER_SIZE {
        return None;
    }
    let n = le32(b, 12) as usize;
    if n < HEADER_SIZE || n > b.len() {
        return None;
    }
    Some(n)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_header_is_sixteen_bytes_of_the_shape_go_writes() {
        let mut b = Builder::new(64);
        let o = b.start_object(8);
        o.set_u64(&mut b, 0, 0x0102030405060708);
        let root = o.offset();
        let out = b.finish(root);
        assert_eq!(&out[0..4], b"ZAP\0");
        assert_eq!(le16(&out, 4), VERSION2);
        assert_eq!(le32(&out, 8) as usize, HEADER_SIZE);
        assert_eq!(le32(&out, 12) as usize, out.len());
        assert_eq!(out.len(), HEADER_SIZE + 8);
    }

    #[test]
    fn a_tail_is_appended_after_the_fixed_section() {
        let mut b = Builder::new(64);
        let o = b.start_object(16);
        o.set_bytes(&mut b, 0, b"hello");
        o.set_bytes(&mut b, 8, b"world!!");
        let root = o.offset();
        let out = b.finish(root);

        let m = parse(&out).expect("parses");
        assert_eq!(m.size(), out.len());
        assert_eq!(m.root().bytes(0), b"hello");
        assert_eq!(m.root().bytes(8), b"world!!");
        // Fixed section first, then the two tails in the order they were set.
        assert_eq!(out.len(), HEADER_SIZE + 16 + 5 + 7);
    }

    #[test]
    fn an_empty_tail_is_a_null_pair_and_reads_back_empty() {
        let mut b = Builder::new(64);
        let o = b.start_object(8);
        o.set_bytes(&mut b, 0, b"");
        let root = o.offset();
        let out = b.finish(root);
        assert_eq!(out.len(), HEADER_SIZE + 8);
        assert!(parse(&out).unwrap().root().bytes(0).is_empty());
    }

    #[test]
    fn a_list_is_written_before_the_object_that_points_at_it() {
        let mut b = Builder::new(64);
        let mut l = b.start_list();
        l.add_u32(&mut b, 7);
        l.add_u32(&mut b, 9);
        let (off, n) = l.finish();
        let o = b.start_object(16);
        o.set_list(&mut b, 0, off, n);
        let root = o.offset();
        let out = b.finish(root);

        let m = parse(&out).unwrap();
        let slots = m.root().list(0, 4);
        assert_eq!(slots.len(), 2);
        assert_eq!(slots.u32(0), 7);
        assert_eq!(slots.u32(1), 9);
        // The pointer is backwards, and resolves.
        assert!(off < root);
    }

    #[test]
    fn a_short_buffer_is_refused_rather_than_read() {
        assert_eq!(parse(&[]).unwrap_err(), Error::BufferTooSmall);
        assert_eq!(parse(&[0x5a]).unwrap_err(), Error::BufferTooSmall);
        let mut hdr = [0u8; HEADER_SIZE];
        hdr[0..4].copy_from_slice(&MAGIC);
        hdr[4..6].copy_from_slice(&VERSION2.to_le_bytes());
        hdr[12..16].copy_from_slice(&(64u32).to_le_bytes());
        assert_eq!(parse(&hdr).unwrap_err(), Error::BufferTooSmall);
    }

    #[test]
    fn a_foreign_magic_or_version_is_refused() {
        let mut b = Builder::new(64);
        let o = b.start_object(8);
        let root = o.offset();
        let mut out = b.finish(root);
        out[0] = b'X';
        assert_eq!(parse(&out).unwrap_err(), Error::InvalidMagic);
        out[0] = b'Z';
        out[4..6].copy_from_slice(&9u16.to_le_bytes());
        assert_eq!(parse(&out).unwrap_err(), Error::InvalidVersion);
    }

    #[test]
    fn a_tail_pointing_into_the_header_reads_as_absent() {
        let mut b = Builder::new(64);
        let o = b.start_object(8);
        o.set_bytes(&mut b, 0, b"abcd");
        let root = o.offset();
        let mut out = b.finish(root);
        // Aim the pointer backwards at offset 0 — the header.
        let field = root;
        let rel = (0i64 - field as i64) as i32 as u32;
        out[field..field + 4].copy_from_slice(&rel.to_le_bytes());
        assert!(parse(&out).unwrap().root().bytes(0).is_empty());
    }

    #[test]
    fn a_declared_size_shorter_than_the_buffer_bounds_every_read() {
        let mut b = Builder::new(64);
        let o = b.start_object(8);
        o.set_u64(&mut b, 0, 0xdead_beef);
        let root = o.offset();
        let mut out = b.finish(root);
        let real = out.len();
        out.extend_from_slice(&[0xff; 8]);
        let m = parse(&out).unwrap();
        assert_eq!(m.size(), real);
        assert_eq!(message_len(&out), Some(real));
    }

    #[test]
    fn a_list_length_that_cannot_fit_is_refused_by_its_stride() {
        let mut b = Builder::new(64);
        let mut l = b.start_list();
        l.add_u32(&mut b, 1);
        let (off, _) = l.finish();
        let o = b.start_object(16);
        o.set_list(&mut b, 0, off, 1);
        let root = o.offset();
        let mut out = b.finish(root);
        // Claim four billion elements.
        out[root + 4..root + 8].copy_from_slice(&u32::MAX.to_le_bytes());
        assert_eq!(parse(&out).unwrap().root().list(0, 4).len(), 0);
    }
}
