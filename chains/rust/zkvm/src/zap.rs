// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! ZAP — the arena message format the Z-Chain is written in.
//!
//! One buffer, read without parsing: a 16-byte header names the root object's
//! offset, and every field is either inline at a fixed offset or a relative
//! pointer to somewhere else in the same buffer. That is the whole format.
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
//! This is the same format Go writes in `luxfi/zap` (`builder.go`, `zap.go`) —
//! not the length-prefixed transport frame that shares the name. A block
//! written with the other one is a block no Go peer can read.
//!
//! WHY THE READER NEVER PANICS. Every accessor is bounds-checked and answers
//! the zero value on a short or crooked buffer, because these bytes arrive from
//! whoever wants to send them. Two rules beyond plain bounds carry weight:
//! a byte tail's pointer is UNSIGNED (a signed one would let a crafted offset
//! alias back into the fixed section), and no pointer of any kind may land
//! inside the 16-byte header, whose contents are not a legitimate payload.

use std::convert::TryInto;

/// The header, in bytes.
pub const HEADER_SIZE: usize = 16;

/// The four bytes every message starts with.
pub const MAGIC: [u8; 4] = [b'Z', b'A', b'P', 0];

/// The legacy schema. Accepted on read; never emitted.
pub const VERSION1: u16 = 1;
/// The current schema, and what [`Builder`] writes.
pub const VERSION2: u16 = 2;
/// What a new message is stamped with.
pub const VERSION: u16 = VERSION2;

/// Objects and lists start on this boundary.
pub const ALIGNMENT: usize = 8;

/// What a buffer can be refused for.
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

// ---------------------------------------------------------------- writing --

/// Writes a message. Positions handed back by [`Builder::start_object`] and
/// [`Builder::start_list`] are absolute offsets into the buffer under
/// construction; a pointer field turns one into a relative offset when it is
/// set.
#[derive(Debug)]
pub struct Builder {
    buf: Vec<u8>,
    pos: usize,
    root_offset: usize,
}

impl Default for Builder {
    fn default() -> Self {
        Builder::new(256)
    }
}

impl Builder {
    /// A builder over a buffer of at least `capacity` bytes, header written.
    pub fn new(capacity: usize) -> Self {
        let cap = if capacity < HEADER_SIZE {
            256
        } else {
            capacity
        };
        let mut buf = vec![0u8; cap];
        buf[0..4].copy_from_slice(&MAGIC);
        buf[4..6].copy_from_slice(&VERSION2.to_le_bytes());
        Builder {
            buf,
            pos: HEADER_SIZE,
            root_offset: 0,
        }
    }

    fn grow(&mut self, n: usize) {
        if self.pos + n <= self.buf.len() {
            return;
        }
        let mut new_cap = self.buf.len() * 2;
        if new_cap < self.pos + n {
            new_cap = self.pos + n;
        }
        self.buf.resize(new_cap, 0);
    }

    fn align(&mut self, alignment: usize) {
        let padding = (alignment - (self.pos % alignment)) % alignment;
        self.grow(padding);
        for _ in 0..padding {
            self.buf[self.pos] = 0;
            self.pos += 1;
        }
    }

    /// Extend the cursor to `end`, zero-filling. This is what makes a reused or
    /// grown buffer emit the same bytes as a fresh one.
    fn ensure(&mut self, end: usize) {
        if end > self.pos {
            self.grow(end - self.pos);
            for i in self.pos..end {
                self.buf[i] = 0;
            }
            self.pos = end;
        }
    }

    /// Open an object whose fixed section is `data_size` bytes. The whole fixed
    /// section is reserved now, so a variable field can append its tail
    /// immediately after it and patch its own pointer on the spot.
    pub fn start_object(&mut self, data_size: usize) -> ObjectBuilder {
        self.align(ALIGNMENT);
        let start = self.pos;
        self.ensure(start + data_size);
        ObjectBuilder { start }
    }

    /// Open a list. The stride is implicit in which `add_*` the caller uses.
    pub fn start_list(&mut self) -> ListBuilder {
        self.align(ALIGNMENT);
        ListBuilder {
            start: self.pos,
            count: 0,
        }
    }

    /// Name `offset` as the message root.
    pub fn set_root(&mut self, offset: usize) {
        self.root_offset = offset;
    }

    /// Seal the message: write the root offset and the total size.
    pub fn finish(mut self) -> Vec<u8> {
        let root = self.root_offset as u32;
        let size = self.pos as u32;
        self.buf[8..12].copy_from_slice(&root.to_le_bytes());
        self.buf[12..16].copy_from_slice(&size.to_le_bytes());
        self.buf.truncate(self.pos);
        self.buf
    }

    fn put_u8(&mut self, at: usize, v: u8) {
        self.ensure(at + 1);
        self.buf[at] = v;
    }

    fn put_u32(&mut self, at: usize, v: u32) {
        self.ensure(at + 4);
        self.buf[at..at + 4].copy_from_slice(&v.to_le_bytes());
    }

    fn put_u64(&mut self, at: usize, v: u64) {
        self.ensure(at + 8);
        self.buf[at..at + 8].copy_from_slice(&v.to_le_bytes());
    }
}

/// A handle on one open object. Field offsets are relative to the object.
#[derive(Clone, Copy, Debug)]
pub struct ObjectBuilder {
    start: usize,
}

impl ObjectBuilder {
    /// The object's absolute offset — what a pointer field or list slot names.
    pub fn offset(&self) -> usize {
        self.start
    }

    pub fn set_u8(&self, b: &mut Builder, field: usize, v: u8) {
        b.put_u8(self.start + field, v);
    }

    pub fn set_u32(&self, b: &mut Builder, field: usize, v: u32) {
        b.put_u32(self.start + field, v);
    }

    pub fn set_u64(&self, b: &mut Builder, field: usize, v: u64) {
        b.put_u64(self.start + field, v);
    }

    /// Copy `v` inline at `field`. For a schema's fixed-width byte slot — a
    /// 32-byte id, a 20-byte address. Zero length is a no-op, leaving zeros.
    pub fn set_bytes_fixed(&self, b: &mut Builder, field: usize, v: &[u8]) {
        if v.is_empty() {
            return;
        }
        let at = self.start + field;
        b.ensure(at + v.len());
        b.buf[at..at + v.len()].copy_from_slice(v);
    }

    /// Write `v` as a variable-length tail and point `field` at it. The field is
    /// the pair (relative offset u32, length u32); empty writes the null pair.
    pub fn set_bytes(&self, b: &mut Builder, field: usize, v: &[u8]) {
        let field_abs = self.start + field;
        if v.is_empty() {
            b.put_u32(field_abs, 0);
            b.put_u32(field_abs + 4, 0);
            return;
        }
        let data_pos = b.pos;
        b.grow(v.len());
        b.buf[data_pos..data_pos + v.len()].copy_from_slice(v);
        b.pos = data_pos + v.len();

        let rel = (data_pos as i64 - field_abs as i64) as i32;
        b.put_u32(field_abs, rel as u32);
        b.put_u32(field_abs + 4, v.len() as u32);
    }

    /// Point `field` at an object already written at `obj_offset`. Zero writes
    /// the null pointer.
    pub fn set_object(&self, b: &mut Builder, field: usize, obj_offset: usize) {
        let field_abs = self.start + field;
        if obj_offset == 0 {
            b.put_u32(field_abs, 0);
            return;
        }
        let rel = (obj_offset as i64 - field_abs as i64) as i32;
        b.put_u32(field_abs, rel as u32);
    }

    /// Point `field` at a list already written at `list_offset` with `length`
    /// elements. Either being zero writes the null pair.
    pub fn set_list(&self, b: &mut Builder, field: usize, list_offset: usize, length: usize) {
        let field_abs = self.start + field;
        if list_offset == 0 || length == 0 {
            b.put_u32(field_abs, 0);
            b.put_u32(field_abs + 4, 0);
            return;
        }
        let rel = (list_offset as i64 - field_abs as i64) as i32;
        b.put_u32(field_abs, rel as u32);
        b.put_u32(field_abs + 4, length as u32);
    }

    /// Seal the object and name it the root.
    pub fn finish_as_root(&self, b: &mut Builder) -> usize {
        b.set_root(self.start);
        self.start
    }
}

/// A handle on one open list.
#[derive(Clone, Copy, Debug)]
pub struct ListBuilder {
    start: usize,
    count: usize,
}

impl ListBuilder {
    pub fn add_u8(&mut self, b: &mut Builder, v: u8) {
        b.grow(1);
        b.buf[b.pos] = v;
        b.pos += 1;
        self.count += 1;
    }

    pub fn add_u32(&mut self, b: &mut Builder, v: u32) {
        b.grow(4);
        let p = b.pos;
        b.buf[p..p + 4].copy_from_slice(&v.to_le_bytes());
        b.pos = p + 4;
        self.count += 1;
    }

    pub fn add_u64(&mut self, b: &mut Builder, v: u64) {
        b.grow(8);
        let p = b.pos;
        b.buf[p..p + 8].copy_from_slice(&v.to_le_bytes());
        b.pos = p + 8;
        self.count += 1;
    }

    /// Append raw bytes. The count advances by the BYTE length — this is the
    /// element kind for a byte run or a fixed-stride record written whole.
    pub fn add_bytes(&mut self, b: &mut Builder, data: &[u8]) {
        b.grow(data.len());
        let p = b.pos;
        b.buf[p..p + data.len()].copy_from_slice(data);
        b.pos = p + data.len();
        self.count += data.len();
    }

    /// Append a 4-byte SIGNED pointer to an object at `target`. This is the
    /// element kind of a repeated-message field: the objects are written first
    /// and the pointer array after, so the offsets are usually negative.
    pub fn add_object_ptr(&mut self, b: &mut Builder, target: usize) {
        b.grow(4);
        let p = b.pos;
        let v: u32 = if target == 0 {
            0
        } else {
            ((target as i64 - p as i64) as i32) as u32
        };
        b.buf[p..p + 4].copy_from_slice(&v.to_le_bytes());
        b.pos = p + 4;
        self.count += 1;
    }

    /// (offset, length) — what a list field is set from.
    pub fn finish(&self) -> (usize, usize) {
        (self.start, self.count)
    }
}

// ---------------------------------------------------------------- reading --

/// A validated message, read in place.
#[derive(Clone, Copy, Debug)]
pub struct Message<'a> {
    data: &'a [u8],
}

impl<'a> Message<'a> {
    /// Validate a buffer's header and take the message it declares.
    ///
    /// The declared size must be at least the header and at most the input, so
    /// a size of zero cannot pass here and blow up an accessor later.
    pub fn parse(data: &'a [u8]) -> Result<Self, Error> {
        if data.len() < HEADER_SIZE {
            return Err(Error::BufferTooSmall);
        }
        if data[0..4] != MAGIC {
            return Err(Error::InvalidMagic);
        }
        let version = u16::from_le_bytes(data[4..6].try_into().unwrap());
        if version != VERSION1 && version != VERSION2 {
            return Err(Error::InvalidVersion);
        }
        let size = u32::from_le_bytes(data[12..16].try_into().unwrap()) as usize;
        if size < HEADER_SIZE || size > data.len() {
            return Err(Error::BufferTooSmall);
        }
        Ok(Message {
            data: &data[..size],
        })
    }

    pub fn bytes(&self) -> &'a [u8] {
        self.data
    }

    pub fn size(&self) -> usize {
        self.data.len()
    }

    pub fn version(&self) -> u16 {
        u16::from_le_bytes(self.data[4..6].try_into().unwrap())
    }

    pub fn flags(&self) -> u16 {
        u16::from_le_bytes(self.data[6..8].try_into().unwrap())
    }

    /// The root object.
    pub fn root(&self) -> Object<'a> {
        let offset = u32::from_le_bytes(self.data[8..12].try_into().unwrap()) as usize;
        Object {
            data: self.data,
            offset,
            null: false,
        }
    }
}

/// A view on one object. Reading past the end answers the zero value.
#[derive(Clone, Copy, Debug)]
pub struct Object<'a> {
    data: &'a [u8],
    offset: usize,
    null: bool,
}

impl<'a> Object<'a> {
    /// The object that is not there.
    pub fn null() -> Self {
        Object {
            data: &[],
            offset: 0,
            null: true,
        }
    }

    pub fn is_null(&self) -> bool {
        self.null || self.offset == 0
    }

    pub fn offset(&self) -> usize {
        self.offset
    }

    /// The message this view is into — for reaching a sibling object.
    pub fn message(&self) -> Message<'a> {
        Message { data: self.data }
    }

    pub fn u8(&self, field: usize) -> u8 {
        let pos = self.offset + field;
        if pos >= self.data.len() {
            return 0;
        }
        self.data[pos]
    }

    pub fn u16(&self, field: usize) -> u16 {
        let pos = self.offset + field;
        if pos + 2 > self.data.len() {
            return 0;
        }
        u16::from_le_bytes(self.data[pos..pos + 2].try_into().unwrap())
    }

    pub fn u32(&self, field: usize) -> u32 {
        let pos = self.offset + field;
        if pos + 4 > self.data.len() {
            return 0;
        }
        u32::from_le_bytes(self.data[pos..pos + 4].try_into().unwrap())
    }

    pub fn u64(&self, field: usize) -> u64 {
        let pos = self.offset + field;
        if pos + 8 > self.data.len() {
            return 0;
        }
        u64::from_le_bytes(self.data[pos..pos + 8].try_into().unwrap())
    }

    /// An inline byte slot of `n` bytes. Empty when the span leaves the buffer.
    pub fn bytes_fixed(&self, field: usize, n: usize) -> &'a [u8] {
        if n == 0 {
            return &[];
        }
        let pos = self.offset + field;
        if pos + n > self.data.len() {
            return &[];
        }
        &self.data[pos..pos + n]
    }

    /// A variable-length byte tail.
    ///
    /// The relative offset is UNSIGNED — a forward pointer only. A negative bit
    /// pattern flows through as a huge positive and is refused by the bound
    /// below, which is what stops a crafted tail from aliasing the fixed
    /// section it was written beside.
    pub fn bytes(&self, field: usize) -> &'a [u8] {
        let pos = self.offset + field;
        if pos + 4 > self.data.len() {
            return &[];
        }
        let rel = u32::from_le_bytes(self.data[pos..pos + 4].try_into().unwrap()) as usize;
        if rel == 0 {
            return &[];
        }
        let len_pos = pos + 4;
        if len_pos + 4 > self.data.len() {
            return &[];
        }
        let length =
            u32::from_le_bytes(self.data[len_pos..len_pos + 4].try_into().unwrap()) as usize;
        let abs = match pos.checked_add(rel) {
            Some(v) => v,
            None => return &[],
        };
        if abs < HEADER_SIZE {
            return &[];
        }
        match abs.checked_add(length) {
            Some(end) if end <= self.data.len() => &self.data[abs..end],
            _ => &[],
        }
    }

    /// A nested object.
    ///
    /// This pointer is SIGNED: a builder may finish a child before its parent,
    /// leaving the child earlier in the buffer. It may still never land inside
    /// the header.
    pub fn object(&self, field: usize) -> Object<'a> {
        let pos = self.offset + field;
        if pos + 4 > self.data.len() {
            return Object::null();
        }
        let rel = i32::from_le_bytes(self.data[pos..pos + 4].try_into().unwrap());
        if rel == 0 {
            return Object::null();
        }
        let abs = pos as i64 + rel as i64;
        if abs < HEADER_SIZE as i64 || abs >= self.data.len() as i64 {
            return Object::null();
        }
        Object {
            data: self.data,
            offset: abs as usize,
            null: false,
        }
    }

    /// A list, with no statement about element width.
    ///
    /// The length is clamped to the message size: without that, a peer's
    /// `0xFFFFFFFF` would make every `for i in 0..len` loop run four billion
    /// times even though each element read answers zero.
    pub fn list(&self, field: usize) -> List<'a> {
        self.list_stride(field, 0)
    }

    /// A list whose element width the caller knows.
    ///
    /// `min_stride` buys the tighter test `length * stride <= what is left`,
    /// which refuses an announced length no buffer of this size could hold —
    /// instead of pushing the refusal down to every element access.
    pub fn list_stride(&self, field: usize, min_stride: u32) -> List<'a> {
        let pos = self.offset + field;
        if pos + 8 > self.data.len() {
            return List::null();
        }
        let rel = i32::from_le_bytes(self.data[pos..pos + 4].try_into().unwrap());
        if rel == 0 {
            return List::null();
        }
        let length = u32::from_le_bytes(self.data[pos + 4..pos + 8].try_into().unwrap());
        let abs = pos as i64 + rel as i64;
        if abs < HEADER_SIZE as i64 || abs >= self.data.len() as i64 {
            return List::null();
        }
        let abs = abs as usize;
        let remaining = (self.data.len() - abs) as u64;
        if min_stride > 0 {
            if length as u64 * min_stride as u64 > remaining {
                return List::null();
            }
        } else if length as u64 > self.data.len() as u64 {
            return List::null();
        }
        List {
            data: self.data,
            offset: abs,
            length: length as usize,
            null: false,
        }
    }
}

/// A view on one list.
#[derive(Clone, Copy, Debug)]
pub struct List<'a> {
    data: &'a [u8],
    offset: usize,
    length: usize,
    null: bool,
}

impl<'a> List<'a> {
    pub fn null() -> Self {
        List {
            data: &[],
            offset: 0,
            length: 0,
            null: true,
        }
    }

    /// The element count as encoded.
    ///
    /// Never size an allocation from this alone: the wire only bounds it by the
    /// buffer length, so a small message can announce a large count.
    pub fn len(&self) -> usize {
        self.length
    }

    pub fn is_empty(&self) -> bool {
        self.length == 0
    }

    pub fn is_null(&self) -> bool {
        self.null
    }

    pub fn u8(&self, i: usize) -> u8 {
        if i >= self.length {
            return 0;
        }
        let pos = self.offset + i;
        if pos >= self.data.len() {
            return 0;
        }
        self.data[pos]
    }

    pub fn u32(&self, i: usize) -> u32 {
        if i >= self.length {
            return 0;
        }
        let pos = self.offset + i * 4;
        if pos + 4 > self.data.len() {
            return 0;
        }
        u32::from_le_bytes(self.data[pos..pos + 4].try_into().unwrap())
    }

    pub fn u64(&self, i: usize) -> u64 {
        if i >= self.length {
            return 0;
        }
        let pos = self.offset + i * 8;
        if pos + 8 > self.data.len() {
            return 0;
        }
        u64::from_le_bytes(self.data[pos..pos + 8].try_into().unwrap())
    }

    /// Element `i` of an INLINE object list: a fixed-stride record in place.
    pub fn object(&self, i: usize, elem_size: usize) -> Object<'a> {
        if i >= self.length {
            return Object::null();
        }
        Object {
            data: self.data,
            offset: self.offset + i * elem_size,
            null: false,
        }
    }

    /// Element `i` of an OUT-OF-LINE object list: a 4-byte signed pointer,
    /// dereferenced exactly as [`Object::object`] does.
    pub fn object_ptr(&self, i: usize) -> Object<'a> {
        if i >= self.length {
            return Object::null();
        }
        let pos = self.offset + i * 4;
        if pos + 4 > self.data.len() {
            return Object::null();
        }
        let rel = i32::from_le_bytes(self.data[pos..pos + 4].try_into().unwrap());
        if rel == 0 {
            return Object::null();
        }
        let abs = pos as i64 + rel as i64;
        if abs < HEADER_SIZE as i64 || abs >= self.data.len() as i64 {
            return Object::null();
        }
        Object {
            data: self.data,
            offset: abs as usize,
            null: false,
        }
    }

    /// The whole run, for a byte list.
    pub fn bytes(&self) -> &'a [u8] {
        if self.null || self.offset + self.length > self.data.len() {
            return &[];
        }
        &self.data[self.offset..self.offset + self.length]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_message_starts_with_the_magic_and_the_current_version() {
        let mut b = Builder::new(64);
        let ob = b.start_object(8);
        ob.set_u64(&mut b, 0, 0x0102_0304_0506_0708);
        ob.finish_as_root(&mut b);
        let raw = b.finish();

        assert_eq!(&raw[0..4], &MAGIC);
        assert_eq!(u16::from_le_bytes([raw[4], raw[5]]), VERSION2);
        let msg = Message::parse(&raw).unwrap();
        assert_eq!(msg.size(), raw.len());
        assert_eq!(msg.root().u64(0), 0x0102_0304_0506_0708);
    }

    #[test]
    fn every_scalar_width_round_trips() {
        let mut b = Builder::new(64);
        let ob = b.start_object(16);
        ob.set_u8(&mut b, 0, 0xAB);
        ob.set_u32(&mut b, 4, 0xDEAD_BEEF);
        ob.set_u64(&mut b, 8, u64::MAX);
        ob.finish_as_root(&mut b);
        let raw = b.finish();
        let root = Message::parse(&raw).unwrap().root();
        assert_eq!(root.u8(0), 0xAB);
        assert_eq!(root.u32(4), 0xDEAD_BEEF);
        assert_eq!(root.u64(8), u64::MAX);
    }

    #[test]
    fn a_byte_tail_round_trips_and_an_empty_one_is_null() {
        let mut b = Builder::new(64);
        let ob = b.start_object(16);
        ob.set_bytes(&mut b, 0, &[1, 2, 3, 4, 5]);
        ob.set_bytes(&mut b, 8, &[]);
        ob.finish_as_root(&mut b);
        let raw = b.finish();
        let root = Message::parse(&raw).unwrap().root();
        assert_eq!(root.bytes(0), &[1, 2, 3, 4, 5]);
        assert_eq!(root.bytes(8), &[] as &[u8]);
    }

    #[test]
    fn an_inline_byte_slot_stays_in_the_fixed_section() {
        let id = [7u8; 32];
        let mut b = Builder::new(64);
        let ob = b.start_object(32);
        ob.set_bytes_fixed(&mut b, 0, &id);
        ob.finish_as_root(&mut b);
        let raw = b.finish();
        let root = Message::parse(&raw).unwrap().root();
        assert_eq!(root.bytes_fixed(0, 32), &id);
    }

    #[test]
    fn a_u32_list_round_trips() {
        let mut b = Builder::new(64);
        let mut lb = b.start_list();
        for v in [1u32, 2, 3, 4] {
            lb.add_u32(&mut b, v);
        }
        let (off, n) = lb.finish();
        let ob = b.start_object(8);
        ob.set_list(&mut b, 0, off, n);
        ob.finish_as_root(&mut b);
        let raw = b.finish();
        let root = Message::parse(&raw).unwrap().root();
        let l = root.list_stride(0, 4);
        assert_eq!(l.len(), 4);
        assert_eq!(
            (0..4).map(|i| l.u32(i)).collect::<Vec<_>>(),
            vec![1, 2, 3, 4]
        );
    }

    #[test]
    fn an_object_pointer_list_reaches_objects_written_before_it() {
        let mut b = Builder::new(128);
        let mut offs = Vec::new();
        for v in [10u64, 20, 30] {
            let ob = b.start_object(8);
            ob.set_u64(&mut b, 0, v);
            offs.push(ob.offset());
        }
        let mut lb = b.start_list();
        for off in &offs {
            lb.add_object_ptr(&mut b, *off);
        }
        let (loff, n) = lb.finish();
        let ob = b.start_object(8);
        ob.set_list(&mut b, 0, loff, n);
        ob.finish_as_root(&mut b);
        let raw = b.finish();

        let root = Message::parse(&raw).unwrap().root();
        let l = root.list_stride(0, 4);
        assert_eq!(l.len(), 3);
        assert_eq!(
            (0..3).map(|i| l.object_ptr(i).u64(0)).collect::<Vec<_>>(),
            vec![10, 20, 30]
        );
    }

    #[test]
    fn parse_refuses_a_short_buffer_a_bad_magic_and_a_bad_version() {
        assert_eq!(
            Message::parse(&[0u8; 4]).unwrap_err(),
            Error::BufferTooSmall
        );

        let mut raw = {
            let mut b = Builder::new(64);
            let ob = b.start_object(8);
            ob.finish_as_root(&mut b);
            b.finish()
        };
        let good = raw.clone();

        raw[0] = b'X';
        assert_eq!(Message::parse(&raw).unwrap_err(), Error::InvalidMagic);

        raw = good.clone();
        raw[4] = 9;
        assert_eq!(Message::parse(&raw).unwrap_err(), Error::InvalidVersion);

        // A declared size below the header is refused at the boundary rather
        // than left to panic in an accessor.
        raw = good.clone();
        raw[12..16].copy_from_slice(&0u32.to_le_bytes());
        assert_eq!(Message::parse(&raw).unwrap_err(), Error::BufferTooSmall);

        // A declared size past the input is refused too.
        raw = good;
        raw[12..16].copy_from_slice(&9999u32.to_le_bytes());
        assert_eq!(Message::parse(&raw).unwrap_err(), Error::BufferTooSmall);
    }

    #[test]
    fn a_pointer_into_the_header_is_refused() {
        // Hand-build a message whose object pointer aims backwards at the
        // header. Nothing there is a legitimate payload.
        let mut b = Builder::new(64);
        let ob = b.start_object(16);
        ob.finish_as_root(&mut b);
        let mut raw = b.finish();
        let root_off = u32::from_le_bytes(raw[8..12].try_into().unwrap()) as usize;
        // Point field 0 at offset 4 (inside the header).
        let rel = 4i32 - root_off as i32;
        raw[root_off..root_off + 4].copy_from_slice(&rel.to_le_bytes());
        raw[root_off + 4..root_off + 8].copy_from_slice(&4u32.to_le_bytes());

        let root = Message::parse(&raw).unwrap().root();
        assert!(root.object(0).is_null());
        assert_eq!(root.bytes(0), &[] as &[u8]);
        assert!(root.list(0).is_null());
    }

    #[test]
    fn an_announced_length_no_buffer_could_hold_is_refused() {
        let mut b = Builder::new(64);
        let mut lb = b.start_list();
        lb.add_u32(&mut b, 1);
        let (off, _) = lb.finish();
        let ob = b.start_object(8);
        ob.set_list(&mut b, 0, off, 1);
        ob.finish_as_root(&mut b);
        let mut raw = b.finish();
        let root_off = u32::from_le_bytes(raw[8..12].try_into().unwrap()) as usize;
        raw[root_off + 4..root_off + 8].copy_from_slice(&u32::MAX.to_le_bytes());

        let root = Message::parse(&raw).unwrap().root();
        assert!(root.list_stride(0, 4).is_null());
        // Even with no stride stated, the length is clamped to the message.
        assert!(root.list(0).is_null());
    }

    #[test]
    fn a_reader_never_panics_on_a_truncated_field() {
        let mut b = Builder::new(64);
        let ob = b.start_object(8);
        ob.set_u64(&mut b, 0, 1);
        ob.finish_as_root(&mut b);
        let raw = b.finish();
        let root = Message::parse(&raw).unwrap().root();
        // Read well past the end of everything.
        assert_eq!(root.u64(4096), 0);
        assert_eq!(root.u32(4096), 0);
        assert_eq!(root.u8(4096), 0);
        assert_eq!(root.bytes(4096), &[] as &[u8]);
        assert_eq!(root.bytes_fixed(4096, 32), &[] as &[u8]);
        assert!(root.object(4096).is_null());
        assert!(root.list(4096).is_null());
    }
}
