// SPDX-License-Identifier: BSD-3-Clause-Eco

//! ZAP — the wire every P-Chain object is written in.
//!
//! A ZAP message is a 16-byte header followed by a data segment. Inside the
//! segment, an object is a fixed-width payload at an aligned offset; anything
//! variable (bytes, text, a list, a nested object) lives elsewhere in the
//! segment and is reached by a pointer stored inside the fixed payload,
//! relative to the pointer's own position. Nothing is length-prefixed and
//! nothing is parsed: a reader indexes.
//!
//! ```text
//! ┌──────────────────────────────────────┐
//! │ magic   "ZAP\0"        4 bytes @ 0   │
//! │ version u16 = 2        2 bytes @ 4   │
//! │ flags   u16            2 bytes @ 6   │
//! │ root    u32 offset     4 bytes @ 8   │
//! │ size    u32 total      4 bytes @ 12  │
//! ├──────────────────────────────────────┤
//! │ data segment                         │
//! └──────────────────────────────────────┘
//! ```
//!
//! Everything is little-endian. The size field is what makes a message
//! self-delimiting, and that is load-bearing here: a signed transaction is the
//! unsigned message's bytes followed by the credential message's bytes, so the
//! unsigned bytes a signature covers are a genuine byte-prefix of the signed
//! bytes, found by reading one u32.
//!
//! This is the same format the Go P-Chain writes (`github.com/luxfi/zap`), down
//! to the padding: the builder aligns objects and lists to 8 and zero-fills
//! what it skips, so the same fields written in the same order produce the same
//! bytes in both languages. `wire_vectors` proves it against bytes captured
//! from the Go builder.
//!
//! Reads are bounds-checked and never panic. Out-of-range reads answer with the
//! zero value, exactly as Go's do — a malformed buffer must not be able to make
//! a validator crash, and it must make both languages disagree about nothing.

use std::convert::TryInto;

/// Bytes before the data segment.
pub const HEADER_SIZE: usize = 16;

/// What a ZAP message begins with.
pub const MAGIC: &[u8; 4] = b"ZAP\0";

/// The legacy wire version. Accepted on read, never written.
pub const VERSION1: u16 = 1;
/// The current wire version — what a builder writes.
pub const VERSION2: u16 = 2;

/// Objects and lists start on this boundary.
pub const ALIGNMENT: usize = 8;

/// Why a buffer is not a message.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Error {
    /// Fewer bytes than a header, or a declared size the buffer cannot hold.
    TooSmall,
    /// The first four bytes are not `ZAP\0`.
    BadMagic,
    /// A version neither 1 nor 2.
    BadVersion,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::TooSmall => write!(f, "zap: buffer too small"),
            Error::BadMagic => write!(f, "zap: invalid magic bytes"),
            Error::BadVersion => write!(f, "zap: unsupported version"),
        }
    }
}

impl std::error::Error for Error {}

fn u16_at(b: &[u8], i: usize) -> u16 {
    u16::from_le_bytes(b[i..i + 2].try_into().unwrap())
}

fn u32_at(b: &[u8], i: usize) -> u32 {
    u32::from_le_bytes(b[i..i + 4].try_into().unwrap())
}

/// The total length of the leading self-delimiting message in `b`.
///
/// This is the split point between a signed transaction's unsigned prefix and
/// its credential suffix.
pub fn message_len(b: &[u8]) -> Result<usize, Error> {
    if b.len() < HEADER_SIZE {
        return Err(Error::TooSmall);
    }
    let n = u32_at(b, 12) as usize;
    if n < HEADER_SIZE || n > b.len() {
        return Err(Error::TooSmall);
    }
    Ok(n)
}

/// A validated message. Reads borrow from the buffer; nothing is copied.
#[derive(Clone, Copy, Debug)]
pub struct Message<'a> {
    data: &'a [u8],
}

impl<'a> Message<'a> {
    /// Validate `data` as a message and truncate it to its declared size.
    pub fn parse(data: &'a [u8]) -> Result<Self, Error> {
        if data.len() < HEADER_SIZE {
            return Err(Error::TooSmall);
        }
        if &data[0..4] != MAGIC {
            return Err(Error::BadMagic);
        }
        let version = u16_at(data, 4);
        if version != VERSION1 && version != VERSION2 {
            return Err(Error::BadVersion);
        }
        let size = u32_at(data, 12) as usize;
        if size < HEADER_SIZE || size > data.len() {
            return Err(Error::TooSmall);
        }
        Ok(Message {
            data: &data[..size],
        })
    }

    pub fn version(&self) -> u16 {
        u16_at(self.data, 4)
    }

    pub fn flags(&self) -> u16 {
        u16_at(self.data, 6)
    }

    pub fn size(&self) -> usize {
        self.data.len()
    }

    pub fn bytes(&self) -> &'a [u8] {
        self.data
    }

    /// The object the message is about.
    pub fn root(&self) -> Object<'a> {
        Object {
            data: self.data,
            offset: u32_at(self.data, 8) as usize,
        }
    }
}

/// A view onto one object's fixed payload.
///
/// A null object — one reached through a zero pointer, or through a pointer
/// that would leave the buffer — reads as zeros everywhere. That is the same
/// answer Go gives, and it is the reason a hostile buffer produces wrong data
/// rather than a panic.
#[derive(Clone, Copy, Debug)]
pub struct Object<'a> {
    data: &'a [u8],
    offset: usize,
}

impl<'a> Object<'a> {
    /// The empty object: every read answers zero.
    pub fn null() -> Self {
        Object {
            data: &[],
            offset: 0,
        }
    }

    pub fn is_null(&self) -> bool {
        self.offset == 0
    }

    pub fn offset(&self) -> usize {
        self.offset
    }

    pub fn u8(&self, field: usize) -> u8 {
        let pos = self.offset + field;
        if pos >= self.data.len() {
            return 0;
        }
        self.data[pos]
    }

    pub fn bool(&self, field: usize) -> bool {
        self.u8(field) != 0
    }

    pub fn u16(&self, field: usize) -> u16 {
        let pos = self.offset + field;
        if pos + 2 > self.data.len() {
            return 0;
        }
        u16_at(self.data, pos)
    }

    pub fn u32(&self, field: usize) -> u32 {
        let pos = self.offset + field;
        if pos + 4 > self.data.len() {
            return 0;
        }
        u32_at(self.data, pos)
    }

    pub fn u64(&self, field: usize) -> u64 {
        let pos = self.offset + field;
        if pos + 8 > self.data.len() {
            return 0;
        }
        u64::from_le_bytes(self.data[pos..pos + 8].try_into().unwrap())
    }

    /// `n` bytes stored in place inside the fixed payload.
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

    /// A 32-byte id stored in place. Missing bytes read as zero.
    pub fn id(&self, field: usize) -> [u8; 32] {
        let mut out = [0u8; 32];
        let src = self.bytes_fixed(field, 32);
        out[..src.len()].copy_from_slice(src);
        out
    }

    /// A 20-byte id stored in place.
    pub fn short_id(&self, field: usize) -> [u8; 20] {
        let mut out = [0u8; 20];
        let src = self.bytes_fixed(field, 20);
        out[..src.len()].copy_from_slice(src);
        out
    }

    /// Variable-length bytes reached by a forward pointer.
    ///
    /// The offset is read unsigned deliberately: a negative bit pattern becomes
    /// a huge positive one and fails the bound, which is what stops a crafted
    /// pointer from aliasing back into the fixed section and making one buffer
    /// mean two things.
    pub fn bytes(&self, field: usize) -> &'a [u8] {
        let pos = self.offset + field;
        if pos + 4 > self.data.len() {
            return &[];
        }
        let rel = u32_at(self.data, pos) as usize;
        if rel == 0 {
            return &[];
        }
        if pos + 8 > self.data.len() {
            return &[];
        }
        let len = u32_at(self.data, pos + 4) as usize;
        let abs = pos + rel;
        if abs < HEADER_SIZE || abs + len > self.data.len() {
            return &[];
        }
        &self.data[abs..abs + len]
    }

    /// Text is bytes. Invalid UTF-8 reads as empty rather than as a panic.
    pub fn text(&self, field: usize) -> &'a str {
        std::str::from_utf8(self.bytes(field)).unwrap_or("")
    }

    /// A nested object reached by a signed pointer — a builder may finalize a
    /// child before its parent, in which case the child lies earlier.
    pub fn object(&self, field: usize) -> Object<'a> {
        let pos = self.offset + field;
        if pos + 4 > self.data.len() {
            return Object::null();
        }
        let rel = u32_at(self.data, pos) as i32;
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
        }
    }

    /// A list whose element width the caller knows.
    ///
    /// The width is what lets the count be checked here rather than at every
    /// element: a declared count that could not fit in the remaining buffer is
    /// refused outright, so a four-billion-element claim costs one comparison
    /// instead of four billion.
    pub fn list(&self, field: usize, stride: usize) -> List<'a> {
        let pos = self.offset + field;
        if pos + 8 > self.data.len() {
            return List::empty();
        }
        let rel = u32_at(self.data, pos) as i32;
        if rel == 0 {
            return List::empty();
        }
        let len = u32_at(self.data, pos + 4) as usize;
        let abs = pos as i64 + rel as i64;
        if abs < HEADER_SIZE as i64 || abs >= self.data.len() as i64 {
            return List::empty();
        }
        let abs = abs as usize;
        let remaining = self.data.len() - abs;
        if stride > 0 {
            if len.saturating_mul(stride) > remaining {
                return List::empty();
            }
        } else if len > self.data.len() {
            return List::empty();
        }
        List {
            data: self.data,
            offset: abs,
            len,
        }
    }
}

/// A view onto a run of same-width elements.
#[derive(Clone, Copy, Debug)]
pub struct List<'a> {
    data: &'a [u8],
    offset: usize,
    len: usize,
}

impl<'a> List<'a> {
    pub fn empty() -> Self {
        List {
            data: &[],
            offset: 0,
            len: 0,
        }
    }

    pub fn len(&self) -> usize {
        self.len
    }

    pub fn is_empty(&self) -> bool {
        self.len == 0
    }

    pub fn u8(&self, i: usize) -> u8 {
        if i >= self.len {
            return 0;
        }
        let pos = self.offset + i;
        if pos >= self.data.len() {
            return 0;
        }
        self.data[pos]
    }

    pub fn u32(&self, i: usize) -> u32 {
        if i >= self.len {
            return 0;
        }
        let pos = self.offset + i * 4;
        if pos + 4 > self.data.len() {
            return 0;
        }
        u32_at(self.data, pos)
    }

    pub fn u64(&self, i: usize) -> u64 {
        if i >= self.len {
            return 0;
        }
        let pos = self.offset + i * 8;
        if pos + 8 > self.data.len() {
            return 0;
        }
        u64::from_le_bytes(self.data[pos..pos + 8].try_into().unwrap())
    }

    /// Element `i` of a list whose elements are inline objects of `stride`.
    pub fn object(&self, i: usize, stride: usize) -> Object<'a> {
        if i >= self.len {
            return Object::null();
        }
        Object {
            data: self.data,
            offset: self.offset + i * stride,
        }
    }
}

/// Writes messages.
///
/// The write order is the wire: a caller emits every variable part first, then
/// starts the object and sets its fixed fields, so a field's pointer always
/// points backward to something already placed. `StartObject` reserves the
/// whole fixed payload up front, which is what lets `set_bytes` append its tail
/// immediately and patch its own pointer instead of replaying a deferred list.
pub struct Builder {
    buf: Vec<u8>,
    pos: usize,
    root: usize,
}

impl Builder {
    pub fn new(capacity: usize) -> Self {
        let capacity = if capacity < HEADER_SIZE {
            256
        } else {
            capacity
        };
        let mut buf = vec![0u8; capacity];
        buf[0..4].copy_from_slice(MAGIC);
        buf[4..6].copy_from_slice(&VERSION2.to_le_bytes());
        Builder {
            buf,
            pos: HEADER_SIZE,
            root: 0,
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

    /// Extend the cursor so `end` bytes exist, zero-filling what it passes.
    fn ensure(&mut self, end: usize) {
        if end > self.pos {
            self.grow(end - self.pos);
            for i in self.pos..end {
                self.buf[i] = 0;
            }
            self.pos = end;
        }
    }

    /// Place raw bytes in the data segment and answer where they landed.
    pub fn write_bytes(&mut self, data: &[u8]) -> usize {
        if data.is_empty() {
            return 0;
        }
        self.align(ALIGNMENT);
        let offset = self.pos;
        self.grow(data.len());
        self.buf[self.pos..self.pos + data.len()].copy_from_slice(data);
        self.pos += data.len();
        offset
    }

    /// Begin an object of `size` bytes, reserving all of it.
    pub fn start_object(&mut self, size: usize) -> ObjectBuilder {
        self.align(ALIGNMENT);
        let start = self.pos;
        self.ensure(start + size);
        ObjectBuilder { start }
    }

    /// Begin a list. Elements are appended by the `list_*` methods.
    pub fn start_list(&mut self) -> ListBuilder {
        self.align(ALIGNMENT);
        ListBuilder {
            start: self.pos,
            count: 0,
        }
    }

    /// Append raw bytes to a list. The count advances by the byte length —
    /// callers that build fixed-stride element lists this way supply the real
    /// element count to `set_list` themselves, exactly as Go does.
    pub fn list_bytes(&mut self, lb: &mut ListBuilder, data: &[u8]) {
        self.grow(data.len());
        self.buf[self.pos..self.pos + data.len()].copy_from_slice(data);
        self.pos += data.len();
        lb.count += data.len();
    }

    /// Append one u32 element.
    pub fn list_u32(&mut self, lb: &mut ListBuilder, v: u32) {
        self.grow(4);
        self.buf[self.pos..self.pos + 4].copy_from_slice(&v.to_le_bytes());
        self.pos += 4;
        lb.count += 1;
    }

    pub fn set_u8(&mut self, ob: &ObjectBuilder, field: usize, v: u8) {
        self.ensure(ob.start + field + 1);
        self.buf[ob.start + field] = v;
    }

    pub fn set_u32(&mut self, ob: &ObjectBuilder, field: usize, v: u32) {
        self.ensure(ob.start + field + 4);
        let at = ob.start + field;
        self.buf[at..at + 4].copy_from_slice(&v.to_le_bytes());
    }

    pub fn set_u64(&mut self, ob: &ObjectBuilder, field: usize, v: u64) {
        self.ensure(ob.start + field + 8);
        let at = ob.start + field;
        self.buf[at..at + 8].copy_from_slice(&v.to_le_bytes());
    }

    /// Store bytes in place inside the fixed payload.
    pub fn set_bytes_fixed(&mut self, ob: &ObjectBuilder, field: usize, v: &[u8]) {
        if v.is_empty() {
            return;
        }
        self.ensure(ob.start + field + v.len());
        let at = ob.start + field;
        self.buf[at..at + v.len()].copy_from_slice(v);
    }

    /// Store variable bytes after the fixed payload and point at them.
    pub fn set_bytes(&mut self, ob: &ObjectBuilder, field: usize, v: &[u8]) {
        let at = ob.start + field;
        self.ensure(at + 8);
        if v.is_empty() {
            self.buf[at..at + 4].copy_from_slice(&0u32.to_le_bytes());
            self.buf[at + 4..at + 8].copy_from_slice(&0u32.to_le_bytes());
            return;
        }
        let data_pos = self.pos;
        self.grow(v.len());
        self.buf[self.pos..self.pos + v.len()].copy_from_slice(v);
        self.pos += v.len();
        let rel = (data_pos - at) as u32;
        self.buf[at..at + 4].copy_from_slice(&rel.to_le_bytes());
        self.buf[at + 4..at + 8].copy_from_slice(&(v.len() as u32).to_le_bytes());
    }

    /// Point a field at a list already placed in the data segment.
    pub fn set_list(&mut self, ob: &ObjectBuilder, field: usize, list_offset: usize, len: usize) {
        let at = ob.start + field;
        self.ensure(at + 8);
        if list_offset == 0 || len == 0 {
            self.buf[at..at + 4].copy_from_slice(&0u32.to_le_bytes());
            self.buf[at + 4..at + 8].copy_from_slice(&0u32.to_le_bytes());
            return;
        }
        let rel = (list_offset as i64 - at as i64) as i32 as u32;
        self.buf[at..at + 4].copy_from_slice(&rel.to_le_bytes());
        self.buf[at + 4..at + 8].copy_from_slice(&(len as u32).to_le_bytes());
    }

    /// Name this object as what the message is about.
    pub fn finish_as_root(&mut self, ob: &ObjectBuilder) {
        self.root = ob.start;
    }

    /// The finished message.
    pub fn finish(mut self) -> Vec<u8> {
        let root = self.root as u32;
        let size = self.pos as u32;
        self.buf[8..12].copy_from_slice(&root.to_le_bytes());
        self.buf[12..16].copy_from_slice(&size.to_le_bytes());
        self.buf.truncate(self.pos);
        self.buf
    }
}

/// Where an object started. Fields are set through the builder that owns it.
#[derive(Clone, Copy, Debug)]
pub struct ObjectBuilder {
    start: usize,
}

impl ObjectBuilder {
    pub fn offset(&self) -> usize {
        self.start
    }
}

/// Where a list started, and how many elements it has taken.
#[derive(Clone, Copy, Debug)]
pub struct ListBuilder {
    start: usize,
    count: usize,
}

impl ListBuilder {
    pub fn offset(&self) -> usize {
        self.start
    }

    /// What the appenders counted. `list_u32` counts elements; `list_bytes`
    /// counts bytes, so a fixed-stride byte list reports its own element count.
    pub fn count(&self) -> usize {
        self.count
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_message_carries_its_own_length() {
        let mut b = Builder::new(64);
        let ob = b.start_object(8);
        b.set_u64(&ob, 0, 0x0102_0304_0506_0708);
        b.finish_as_root(&ob);
        let bytes = b.finish();

        assert_eq!(&bytes[0..4], MAGIC);
        assert_eq!(u16_at(&bytes, 4), VERSION2);
        assert_eq!(message_len(&bytes).unwrap(), bytes.len());

        // The length is what splits a signed tx; trailing bytes do not move it.
        let mut with_tail = bytes.clone();
        with_tail.extend_from_slice(b"tail");
        assert_eq!(message_len(&with_tail).unwrap(), bytes.len());

        let m = Message::parse(&bytes).unwrap();
        assert_eq!(m.root().u64(0), 0x0102_0304_0506_0708);
    }

    #[test]
    fn a_buffer_that_is_not_a_message_is_refused() {
        assert_eq!(Message::parse(&[]).unwrap_err(), Error::TooSmall);
        assert_eq!(Message::parse(&[0u8; 16]).unwrap_err(), Error::BadMagic);

        let mut b = Builder::new(64);
        let ob = b.start_object(8);
        b.finish_as_root(&ob);
        let mut bytes = b.finish();
        bytes[4] = 9; // a version nobody writes
        assert_eq!(Message::parse(&bytes).unwrap_err(), Error::BadVersion);

        // A size larger than the buffer is refused rather than trusted.
        let mut b = Builder::new(64);
        let ob = b.start_object(8);
        b.finish_as_root(&ob);
        let mut bytes = b.finish();
        bytes[12..16].copy_from_slice(&9999u32.to_le_bytes());
        assert_eq!(Message::parse(&bytes).unwrap_err(), Error::TooSmall);
    }

    #[test]
    fn a_read_past_the_end_answers_zero() {
        let mut b = Builder::new(64);
        let ob = b.start_object(8);
        b.set_u64(&ob, 0, 7);
        b.finish_as_root(&ob);
        let bytes = b.finish();
        let root = Message::parse(&bytes).unwrap().root();

        assert_eq!(root.u64(0), 7);
        assert_eq!(root.u64(1_000_000), 0);
        assert_eq!(root.u32(1_000_000), 0);
        assert_eq!(root.u8(1_000_000), 0);
        assert!(root.bytes(1_000_000).is_empty());
        assert!(root.object(1_000_000).is_null());
        assert_eq!(root.list(1_000_000, 4).len(), 0);
    }

    #[test]
    fn bytes_and_lists_round_trip() {
        let mut b = Builder::new(64);
        let mut lb = b.start_list();
        for i in 0u32..5 {
            b.list_u32(&mut lb, i * 11);
        }
        let (loff, lcount) = (lb.offset(), lb.count());

        let ob = b.start_object(24);
        b.set_u32(&ob, 0, 42);
        b.set_list(&ob, 8, loff, lcount);
        b.set_bytes(&ob, 16, b"hello wire");
        b.finish_as_root(&ob);
        let bytes = b.finish();

        let root = Message::parse(&bytes).unwrap().root();
        assert_eq!(root.u32(0), 42);
        let l = root.list(8, 4);
        assert_eq!(l.len(), 5);
        for i in 0..5 {
            assert_eq!(l.u32(i), i as u32 * 11);
        }
        assert_eq!(root.bytes(16), b"hello wire");
        assert_eq!(root.text(16), "hello wire");
    }

    #[test]
    fn an_empty_slot_reads_as_nothing() {
        let mut b = Builder::new(64);
        let ob = b.start_object(16);
        b.set_bytes(&ob, 0, b"");
        b.set_list(&ob, 8, 0, 0);
        b.finish_as_root(&ob);
        let bytes = b.finish();
        let root = Message::parse(&bytes).unwrap().root();
        assert!(root.bytes(0).is_empty());
        assert_eq!(root.list(8, 4).len(), 0);
    }

    #[test]
    fn a_declared_count_that_cannot_fit_is_refused() {
        // The four-billion-element claim. Go rejects it at the accessor with
        // the stride; so does this, and for the same reason.
        let mut b = Builder::new(64);
        let mut lb = b.start_list();
        b.list_u32(&mut lb, 1);
        let (loff, _) = (lb.offset(), lb.count());
        let ob = b.start_object(8);
        b.set_list(&ob, 0, loff, 1);
        b.finish_as_root(&ob);
        let mut bytes = b.finish();

        let root_off = u32_at(&bytes, 8) as usize;
        bytes[root_off + 4..root_off + 8].copy_from_slice(&u32::MAX.to_le_bytes());
        let root = Message::parse(&bytes).unwrap().root();
        assert_eq!(root.list(0, 4).len(), 0);
    }

    #[test]
    fn a_pointer_into_the_header_is_refused() {
        // A backward pointer that lands on the magic bytes would let one buffer
        // read as two different objects.
        let mut b = Builder::new(64);
        let ob = b.start_object(8);
        b.set_bytes(&ob, 0, b"abcd");
        b.finish_as_root(&ob);
        let mut bytes = b.finish();
        let root_off = u32_at(&bytes, 8) as usize;
        let back = -(root_off as i64) as i32 as u32; // point at offset 0
        bytes[root_off..root_off + 4].copy_from_slice(&back.to_le_bytes());
        let root = Message::parse(&bytes).unwrap().root();
        assert!(root.bytes(0).is_empty());
    }

    #[test]
    fn objects_are_eight_aligned() {
        let mut b = Builder::new(64);
        b.write_bytes(b"x"); // leave the cursor at 17
        let ob = b.start_object(8);
        b.finish_as_root(&ob);
        let bytes = b.finish();
        assert_eq!(u32_at(&bytes, 8) % ALIGNMENT as u32, 0);
    }
}
