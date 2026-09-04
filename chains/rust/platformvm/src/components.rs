// SPDX-License-Identifier: BSD-3-Clause-Eco

//! What a transaction spends and what it makes.
//!
//! The P-Chain moves value in unspent outputs. An output says how much of
//! which asset, and who may spend it; an input names an output that already
//! exists and asserts the right to spend it. A stakeable lock is a time before
//! which an output may only become another locked output of the same owner —
//! it is not a separate kind of output, it is a number on one, which is
//! exactly how the wire carries it.
//!
//! Go reaches the same shape through two nested interfaces
//! (`stakeable.LockOut` wrapping `secp256k1fx.TransferOutput`), and its ZAP
//! encoder flattens them back to one record with a `stake_lock` field on the
//! way out. Flattening once, here, means there is one representation instead
//! of a representation and an encoding of it.

use crate::ids::{Id, ShortId};
use crate::zap;

/// Who may spend an output.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Owners {
    /// Before this time nobody may spend it.
    pub locktime: u64,
    /// How many of the addresses must sign.
    pub threshold: u32,
    /// The addresses, in the order they are indexed by a credential.
    pub addrs: Vec<ShortId>,
}

/// Why an owner is not well formed.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum OwnerError {
    /// More signatures required than there are addresses to give them — an
    /// output nobody can ever spend.
    ThresholdExceedsAddresses,
    /// A threshold of zero with addresses named: the addresses would be
    /// decoration on an output anyone could take.
    ThresholdIsZero,
    /// The addresses are not sorted and unique, so one address could be
    /// counted twice toward the threshold.
    AddressesNotSortedUnique,
}

impl std::fmt::Display for OwnerError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            OwnerError::ThresholdExceedsAddresses => {
                write!(f, "threshold is greater than the number of addresses")
            }
            OwnerError::ThresholdIsZero => write!(f, "threshold is zero with addresses"),
            OwnerError::AddressesNotSortedUnique => {
                write!(f, "addresses are not sorted and unique")
            }
        }
    }
}

impl Owners {
    /// The checks Go's `secp256k1fx.OutputOwners.Verify` makes.
    pub fn verify(&self) -> Result<(), OwnerError> {
        if self.threshold as usize > self.addrs.len() {
            return Err(OwnerError::ThresholdExceedsAddresses);
        }
        if self.threshold == 0 && !self.addrs.is_empty() {
            return Err(OwnerError::ThresholdIsZero);
        }
        if !is_sorted_unique(&self.addrs) {
            return Err(OwnerError::AddressesNotSortedUnique);
        }
        Ok(())
    }

    /// The one canonical encoding of an owner, standalone.
    ///
    /// Go calls this `txs.MarshalOwner`. It is the key a locked balance is
    /// tallied under during a flow check, which is why it has to be one
    /// encoding and not "whatever the caller had": two spellings of the same
    /// owner would let locked value move between owners unnoticed.
    pub fn marshal(&self) -> Vec<u8> {
        const THRESHOLD: usize = 0;
        const LOCKTIME: usize = 4;
        const ADDRS: usize = 12;
        const SIZE: usize = 20;

        let mut b = zap::Builder::new(zap::HEADER_SIZE + 128);
        let (addr_off, addr_count) = write_addrs(&mut b, &self.addrs);
        let ob = b.start_object(SIZE);
        b.set_u32(&ob, THRESHOLD, self.threshold);
        b.set_u64(&ob, LOCKTIME, self.locktime);
        b.set_list(&ob, ADDRS, addr_off, addr_count);
        b.finish_as_root(&ob);
        b.finish()
    }

    /// Read back what [`Owners::marshal`] wrote.
    pub fn unmarshal(bytes: &[u8]) -> Result<Owners, zap::Error> {
        let msg = zap::Message::parse(bytes)?;
        let o = msg.root();
        Ok(Owners {
            threshold: o.u32(0),
            locktime: o.u64(4),
            addrs: read_addrs(o.list(12, crate::ids::SHORT_ID_LEN)),
        })
    }

    /// The identity a locked balance is tallied under.
    pub fn id(&self) -> Id {
        crate::ids::hash256(&self.marshal())
    }
}

/// An output: an amount of one asset, owned, possibly locked until a time.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Output {
    pub asset: Id,
    /// Zero means unlocked. Non-zero is the stakeable lock's expiry.
    pub stake_lock: u64,
    pub amount: u64,
    pub owners: Owners,
}

/// Why an output is not well formed.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum OutputError {
    /// An output of nothing.
    NoValue,
    /// A stakeable lock whose time is zero is a lock that locks nothing; Go
    /// refuses it rather than treating it as absent.
    InvalidLocktime,
    Owner(OwnerError),
}

impl std::fmt::Display for OutputError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            OutputError::NoValue => write!(f, "output has no value"),
            OutputError::InvalidLocktime => write!(f, "invalid locktime"),
            OutputError::Owner(e) => write!(f, "{e}"),
        }
    }
}

impl Output {
    pub fn verify(&self) -> Result<(), OutputError> {
        if self.amount == 0 {
            return Err(OutputError::NoValue);
        }
        self.owners.verify().map_err(OutputError::Owner)?;
        Ok(())
    }

    /// The bytes an output is ordered by.
    ///
    /// Go orders a transaction's outputs by asset id, then by the inner fx
    /// envelope's bytes. That envelope is a two-byte discriminator followed by
    /// a ZAP message, and it is reproduced here exactly, because the ordering
    /// is consensus: a differently-ordered output list is a different
    /// transaction and every peer must agree which one is canonical.
    pub fn order_key(&self) -> Vec<u8> {
        const TYPE_SECP256K1: u8 = 0x01;
        const TYPE_RESERVED: u8 = 0x00;
        const SHAPE_TRANSFER_OUTPUT: u8 = 0x01;
        const SHAPE_LOCKED_OUTPUT: u8 = 0x0F;

        // The transfer output's own envelope.
        let mut b = zap::Builder::new(zap::HEADER_SIZE + 128);
        let (addr_off, addr_count) = write_addrs(&mut b, &self.owners.addrs);
        let ob = b.start_object(28);
        b.set_u64(&ob, 0, self.amount);
        b.set_u64(&ob, 8, self.owners.locktime);
        b.set_u32(&ob, 16, self.owners.threshold);
        b.set_list(&ob, 20, addr_off, addr_count);
        b.finish_as_root(&ob);
        let mut inner = vec![TYPE_SECP256K1, SHAPE_TRANSFER_OUTPUT];
        inner.extend_from_slice(&b.finish());

        if self.stake_lock == 0 {
            return inner;
        }

        // Wrapped in a stakeable lock.
        let mut b = zap::Builder::new(zap::HEADER_SIZE + 256);
        let ob = b.start_object(16);
        b.set_u64(&ob, 0, self.stake_lock);
        b.set_bytes(&ob, 8, &inner);
        b.finish_as_root(&ob);
        let mut outer = vec![TYPE_RESERVED, SHAPE_LOCKED_OUTPUT];
        outer.extend_from_slice(&b.finish());
        outer
    }
}

/// Names an output made by an earlier transaction.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct UtxoId {
    pub tx_id: Id,
    pub output_index: u32,
}

impl UtxoId {
    /// The key an unspent output is stored under.
    ///
    /// Go spells it `TxID.Prefix(uint64(OutputIndex))`: the index big-endian
    /// in front of the transaction id, hashed. The order and the endianness
    /// are the wire — a different arrangement names different UTXOs and the
    /// chain would spend the wrong ones.
    pub fn input_id(&self) -> Id {
        let mut buf = Vec::with_capacity(40);
        buf.extend_from_slice(&(self.output_index as u64).to_be_bytes());
        buf.extend_from_slice(&self.tx_id);
        crate::ids::hash256(&buf)
    }
}

/// An input: the right to spend a named output.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Input {
    pub utxo: UtxoId,
    pub asset: Id,
    /// Must equal the consumed output's lock, and be zero when it has none.
    pub stake_lock: u64,
    pub amount: u64,
    /// Which of the owner's addresses the credential's signatures are for.
    pub sig_indices: Vec<u32>,
}

/// Why an input is not well formed.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum InputError {
    /// An input of nothing.
    NoValue,
    /// Repeated or unordered signature indices would let one signature be
    /// counted more than once toward a threshold.
    IndicesNotSortedUnique,
    InvalidLocktime,
}

impl std::fmt::Display for InputError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            InputError::NoValue => write!(f, "input has no value"),
            InputError::IndicesNotSortedUnique => {
                write!(f, "signature indices are not sorted and unique")
            }
            InputError::InvalidLocktime => write!(f, "invalid locktime"),
        }
    }
}

impl Input {
    pub fn verify(&self) -> Result<(), InputError> {
        if self.amount == 0 {
            return Err(InputError::NoValue);
        }
        if !is_sorted_unique(&self.sig_indices) {
            return Err(InputError::IndicesNotSortedUnique);
        }
        Ok(())
    }
}

/// An output that exists and has not been spent.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Utxo {
    pub id: UtxoId,
    pub output: Output,
}

/// One credential: the signatures for one input.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Credential {
    pub sigs: Vec<[u8; 65]>,
}

/// True when `xs` ascends with no repeats.
pub fn is_sorted_unique<T: Ord>(xs: &[T]) -> bool {
    xs.windows(2).all(|w| w[0] < w[1])
}

/// Outputs are canonical when they ascend by asset id then by envelope bytes.
///
/// Note this is sorted, not sorted-and-unique: two identical outputs in one
/// transaction are legal, and Go's `sort.IsSorted` accepts them.
pub fn is_sorted_outputs(outs: &[Output]) -> bool {
    outs.windows(2).all(|w| {
        let (a, b) = (&w[0], &w[1]);
        match a.asset.cmp(&b.asset) {
            std::cmp::Ordering::Less => true,
            std::cmp::Ordering::Greater => false,
            std::cmp::Ordering::Equal => a.order_key() <= b.order_key(),
        }
    })
}

/// Put outputs in canonical order.
pub fn sort_outputs(outs: &mut [Output]) {
    outs.sort_by(|a, b| {
        a.asset
            .cmp(&b.asset)
            .then_with(|| a.order_key().cmp(&b.order_key()))
    });
}

/// Inputs are canonical when they ascend by the output they name, with no
/// repeats — a transaction spending one output twice would otherwise pass the
/// flow check by counting it twice.
pub fn is_sorted_unique_inputs(ins: &[Input]) -> bool {
    ins.windows(2).all(|w| w[0].utxo < w[1].utxo)
}

/// Put inputs in canonical order.
pub fn sort_inputs(ins: &mut [Input]) {
    ins.sort_by_key(|a| a.utxo);
}

// ---- the address array shared by every owner on a transaction ----

pub(crate) fn write_addrs(b: &mut zap::Builder, addrs: &[ShortId]) -> (usize, usize) {
    if addrs.is_empty() {
        return (0, 0);
    }
    let mut lb = b.start_list();
    for a in addrs {
        b.list_bytes(&mut lb, &a.0);
    }
    (lb.offset(), addrs.len())
}

pub(crate) fn read_addrs(list: zap::List<'_>) -> Vec<ShortId> {
    slice_addrs(list, 0, list.len() as u32)
}

/// A run of addresses out of the transaction-wide array, refused outright when
/// the claimed range is not inside it.
pub(crate) fn slice_addrs(list: zap::List<'_>, start: u32, count: u32) -> Vec<ShortId> {
    let total = list.len() as u32;
    if count == 0 || start > total || count > total - start {
        return Vec::new();
    }
    (0..count)
        .map(|i| {
            let o = list.object((start + i) as usize, crate::ids::SHORT_ID_LEN);
            ShortId(o.short_id(0))
        })
        .collect()
}

pub(crate) fn slice_sigs(list: zap::List<'_>, start: u32, count: u32) -> Vec<u32> {
    let total = list.len() as u32;
    if count == 0 || start > total || count > total - start {
        return Vec::new();
    }
    (0..count).map(|i| list.u32((start + i) as usize)).collect()
}

/// Memo must be empty.
///
/// Go's `VerifyMemoFieldLength` with the current rules refuses any memo at
/// all: the field stays on the wire so old bytes still parse, and carries
/// nothing.
pub fn verify_memo(memo: &[u8]) -> Result<(), MemoTooLarge> {
    if memo.is_empty() {
        Ok(())
    } else {
        Err(MemoTooLarge(memo.len()))
    }
}

/// A memo was carried where none is allowed.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct MemoTooLarge(pub usize);

impl std::fmt::Display for MemoTooLarge {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "memo length {} > 0", self.0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn addr(n: u8) -> ShortId {
        ShortId([n; 20])
    }

    fn out(asset: u8, amount: u64, lock: u64) -> Output {
        Output {
            asset: [asset; 32],
            stake_lock: lock,
            amount,
            owners: Owners {
                locktime: 0,
                threshold: 1,
                addrs: vec![addr(1)],
            },
        }
    }

    #[test]
    fn an_owner_needs_enough_addresses_to_meet_its_threshold() {
        // Go: secp256k1fx.OutputOwners.Verify.
        let mut o = Owners {
            locktime: 0,
            threshold: 2,
            addrs: vec![addr(1)],
        };
        assert_eq!(o.verify(), Err(OwnerError::ThresholdExceedsAddresses));

        o.addrs.push(addr(2));
        assert_eq!(o.verify(), Ok(()));

        o.threshold = 0;
        assert_eq!(o.verify(), Err(OwnerError::ThresholdIsZero));

        // An owner with no addresses and no threshold is well formed — it is
        // how a change-less output is spelled.
        let none = Owners::default();
        assert_eq!(none.verify(), Ok(()));
    }

    #[test]
    fn an_owners_addresses_must_be_sorted_and_unique() {
        let o = Owners {
            locktime: 0,
            threshold: 1,
            addrs: vec![addr(2), addr(1)],
        };
        assert_eq!(o.verify(), Err(OwnerError::AddressesNotSortedUnique));

        let dup = Owners {
            locktime: 0,
            threshold: 1,
            addrs: vec![addr(1), addr(1)],
        };
        assert_eq!(dup.verify(), Err(OwnerError::AddressesNotSortedUnique));
    }

    #[test]
    fn an_owner_round_trips_through_its_canonical_encoding() {
        let o = Owners {
            locktime: 77,
            threshold: 2,
            addrs: vec![addr(1), addr(2), addr(3)],
        };
        assert_eq!(Owners::unmarshal(&o.marshal()).unwrap(), o);
    }

    #[test]
    fn two_spellings_of_one_owner_share_an_identity() {
        // The flow check tallies locked value under this id. Same owner, same
        // id — and a different owner, a different id.
        let a = Owners {
            locktime: 1,
            threshold: 1,
            addrs: vec![addr(9)],
        };
        let b = a.clone();
        assert_eq!(a.id(), b.id());

        let c = Owners {
            locktime: 1,
            threshold: 1,
            addrs: vec![addr(8)],
        };
        assert_ne!(a.id(), c.id());
    }

    #[test]
    fn an_output_must_carry_value() {
        assert_eq!(out(0, 0, 0).verify(), Err(OutputError::NoValue));
        assert_eq!(out(0, 1, 0).verify(), Ok(()));
    }

    #[test]
    fn an_input_must_carry_value_and_ordered_indices() {
        let mut i = Input {
            utxo: UtxoId::default(),
            asset: [0; 32],
            stake_lock: 0,
            amount: 0,
            sig_indices: vec![0],
        };
        assert_eq!(i.verify(), Err(InputError::NoValue));
        i.amount = 5;
        assert_eq!(i.verify(), Ok(()));
        i.sig_indices = vec![1, 0];
        assert_eq!(i.verify(), Err(InputError::IndicesNotSortedUnique));
        i.sig_indices = vec![0, 0];
        assert_eq!(i.verify(), Err(InputError::IndicesNotSortedUnique));
    }

    #[test]
    fn outputs_order_by_asset_then_by_their_wire_bytes() {
        let mut outs = vec![out(2, 1, 0), out(1, 1, 0)];
        assert!(!is_sorted_outputs(&outs));
        sort_outputs(&mut outs);
        assert!(is_sorted_outputs(&outs));
        assert_eq!(outs[0].asset, [1; 32]);

        // Same asset: the envelope bytes decide, and a locked output's
        // envelope is a different shape from an unlocked one's.
        let mut same = vec![out(1, 5, 0), out(1, 3, 0)];
        sort_outputs(&mut same);
        assert!(is_sorted_outputs(&same));
    }

    #[test]
    fn identical_outputs_are_sorted() {
        // Go's IsSorted is not IsSortedAndUnique for outputs — a transaction
        // may legally make two identical ones.
        assert!(is_sorted_outputs(&[out(1, 1, 0), out(1, 1, 0)]));
    }

    #[test]
    fn inputs_must_be_sorted_and_unique() {
        let mk = |tx: u8, idx: u32| Input {
            utxo: UtxoId {
                tx_id: [tx; 32],
                output_index: idx,
            },
            asset: [0; 32],
            stake_lock: 0,
            amount: 1,
            sig_indices: vec![0],
        };
        assert!(is_sorted_unique_inputs(&[mk(1, 0), mk(1, 1), mk(2, 0)]));
        assert!(!is_sorted_unique_inputs(&[mk(2, 0), mk(1, 0)]));
        // Spending one output twice: the whole point of "unique".
        assert!(!is_sorted_unique_inputs(&[mk(1, 0), mk(1, 0)]));
    }

    #[test]
    fn one_output_of_a_transaction_is_named_apart_from_the_others() {
        let a = UtxoId {
            tx_id: [7; 32],
            output_index: 0,
        };
        let b = UtxoId {
            tx_id: [7; 32],
            output_index: 1,
        };
        assert_ne!(a.input_id(), b.input_id());
    }

    #[test]
    fn a_memo_is_not_carried() {
        assert_eq!(verify_memo(&[]), Ok(()));
        assert_eq!(verify_memo(b"hi"), Err(MemoTooLarge(2)));
    }
}

// ---- the envelope a value travels in ----
//
// Every value that crosses between chains — an unspent output, and the output
// inside it — is written as two bytes naming what it is, then a ZAP message
// saying what it holds. Go states this as `luxfi/utxo/wire`: the first byte is
// the signature family that owns the shape and the second is the shape. The
// pair is what stops one buffer from being read as a different value: a
// transfer output read as an owner set would answer every field, plausibly and
// wrongly.

/// The signature family a shape belongs to. Zero is reserved and names none,
/// so a zeroed buffer is not a shape.
pub const TYPE_RESERVED: u8 = 0x00;
/// secp256k1 — the family every P-Chain output belongs to.
pub const TYPE_SECP256K1: u8 = 0x01;

/// An amount and who may spend it.
pub const SHAPE_TRANSFER_OUTPUT: u8 = 0x01;
/// An unspent output.
pub const SHAPE_UTXO: u8 = 0x0A;
/// An output that cannot be spent until a time.
pub const SHAPE_LOCKED_OUTPUT: u8 = 0x0F;

/// The two bytes in front of every envelope.
const ENVELOPE_PREFIX: usize = 2;

/// Why an envelope is not the value it was read as.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum WireError {
    /// Fewer than the two bytes that name the shape.
    ShortEnvelope,
    /// The prefix names a different shape than the reader expected.
    WrongShape {
        want: u8,
        got: u8,
    },
    /// The prefix names no family, where one is required.
    WrongType(u8),
    /// Bytes past the end of the self-delimiting message. The message would
    /// read the same and the buffer would hash differently, so the two would
    /// disagree about the value's name.
    TrailingBytes,
    Wire(zap::Error),
}

impl std::fmt::Display for WireError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            WireError::ShortEnvelope => write!(f, "envelope shorter than its two-byte prefix"),
            WireError::WrongShape { want, got } => {
                write!(f, "envelope names shape {got:#04x}, not {want:#04x}")
            }
            WireError::WrongType(t) => write!(f, "envelope names family {t:#04x}"),
            WireError::TrailingBytes => write!(f, "trailing bytes after the message"),
            WireError::Wire(e) => write!(f, "{e}"),
        }
    }
}

impl std::error::Error for WireError {}

impl From<zap::Error> for WireError {
    fn from(e: zap::Error) -> Self {
        WireError::Wire(e)
    }
}

/// Split the prefix off an envelope and check the shape it names.
fn open(b: &[u8], want_shape: u8) -> Result<(u8, &[u8]), WireError> {
    if b.len() < ENVELOPE_PREFIX {
        return Err(WireError::ShortEnvelope);
    }
    if b[1] != want_shape {
        return Err(WireError::WrongShape {
            want: want_shape,
            got: b[1],
        });
    }
    Ok((b[0], &b[ENVELOPE_PREFIX..]))
}

/// Put the prefix in front of a message.
fn seal(type_kind: u8, shape: u8, message: Vec<u8>) -> Vec<u8> {
    let mut out = Vec::with_capacity(ENVELOPE_PREFIX + message.len());
    out.push(type_kind);
    out.push(shape);
    out.extend_from_slice(&message);
    out
}

// A transfer output's message: amount, then the owners, inline.
const TO_AMOUNT: usize = 0;
const TO_LOCKTIME: usize = 8;
const TO_THRESHOLD: usize = 16;
const TO_ADDRS: usize = 20;
const TO_SIZE: usize = 28;

// A locked output's message: the time it unlocks at, and the output it holds.
const LO_LOCKTIME: usize = 0;
const LO_INNER: usize = 8;
const LO_SIZE: usize = 16;

// An unspent output's message.
const UTXO_TX_ID: usize = 0;
const UTXO_OUTPUT_INDEX: usize = 32;
const UTXO_ASSET: usize = 36;
const UTXO_OUTPUT: usize = 68;
const UTXO_SIZE: usize = 76;

impl Output {
    /// The envelope this output travels in.
    ///
    /// The asset is not in it: an output says how much and whose, and which
    /// asset is the unspent output's business. A lock becomes a second
    /// envelope around the first rather than a field, so a reader that does
    /// not know about locks cannot read a locked output as an unlocked one.
    pub fn wire_bytes(&self) -> Vec<u8> {
        let mut b = zap::Builder::new(zap::HEADER_SIZE + 128);
        let (addr_off, addr_count) = write_addrs(&mut b, &self.owners.addrs);
        let ob = b.start_object(TO_SIZE);
        b.set_u64(&ob, TO_AMOUNT, self.amount);
        b.set_u64(&ob, TO_LOCKTIME, self.owners.locktime);
        b.set_u32(&ob, TO_THRESHOLD, self.owners.threshold);
        b.set_list(&ob, TO_ADDRS, addr_off, addr_count);
        b.finish_as_root(&ob);
        let transfer = seal(TYPE_SECP256K1, SHAPE_TRANSFER_OUTPUT, b.finish());
        if self.stake_lock == 0 {
            return transfer;
        }
        let mut b = zap::Builder::new(zap::HEADER_SIZE + 64 + transfer.len());
        let ob = b.start_object(LO_SIZE);
        b.set_u64(&ob, LO_LOCKTIME, self.stake_lock);
        b.set_bytes(&ob, LO_INNER, &transfer);
        b.finish_as_root(&ob);
        seal(TYPE_RESERVED, SHAPE_LOCKED_OUTPUT, b.finish())
    }

    /// Read an output back out of its envelope.
    ///
    /// `asset` comes from outside because the envelope does not carry it —
    /// the unspent output the envelope sits inside is what names the asset.
    pub fn parse_wire(b: &[u8], asset: Id) -> Result<Output, WireError> {
        if b.len() >= ENVELOPE_PREFIX && b[1] == SHAPE_LOCKED_OUTPUT {
            let (_, message) = open(b, SHAPE_LOCKED_OUTPUT)?;
            let msg = zap::Message::parse(message)?;
            if msg.size() != message.len() {
                return Err(WireError::TrailingBytes);
            }
            let o = msg.root();
            let stake_lock = o.u64(LO_LOCKTIME);
            let mut inner = Output::parse_wire(o.bytes(LO_INNER), asset)?;
            inner.stake_lock = stake_lock;
            return Ok(inner);
        }
        let (type_kind, message) = open(b, SHAPE_TRANSFER_OUTPUT)?;
        if type_kind == TYPE_RESERVED {
            return Err(WireError::WrongType(type_kind));
        }
        let msg = zap::Message::parse(message)?;
        if msg.size() != message.len() {
            return Err(WireError::TrailingBytes);
        }
        let o = msg.root();
        Ok(Output {
            asset,
            stake_lock: 0,
            amount: o.u64(TO_AMOUNT),
            owners: Owners {
                locktime: o.u64(TO_LOCKTIME),
                threshold: o.u32(TO_THRESHOLD),
                addrs: read_addrs(o.list(TO_ADDRS, crate::ids::SHORT_ID_LEN)),
            },
        })
    }
}

impl Utxo {
    /// The envelope an unspent output travels in.
    pub fn wire_bytes(&self) -> Vec<u8> {
        let output = self.output.wire_bytes();
        let mut b = zap::Builder::new(zap::HEADER_SIZE + UTXO_SIZE + output.len() + 32);
        let ob = b.start_object(UTXO_SIZE);
        b.set_bytes_fixed(&ob, UTXO_TX_ID, &self.id.tx_id);
        b.set_u32(&ob, UTXO_OUTPUT_INDEX, self.id.output_index);
        b.set_bytes_fixed(&ob, UTXO_ASSET, &self.output.asset);
        b.set_bytes(&ob, UTXO_OUTPUT, &output);
        b.finish_as_root(&ob);
        seal(TYPE_RESERVED, SHAPE_UTXO, b.finish())
    }

    /// Read an unspent output back out of its envelope.
    pub fn parse_wire(b: &[u8]) -> Result<Utxo, WireError> {
        let (_, message) = open(b, SHAPE_UTXO)?;
        let msg = zap::Message::parse(message)?;
        if msg.size() != message.len() {
            return Err(WireError::TrailingBytes);
        }
        let o = msg.root();
        let asset = o.id(UTXO_ASSET);
        Ok(Utxo {
            id: UtxoId {
                tx_id: o.id(UTXO_TX_ID),
                output_index: o.u32(UTXO_OUTPUT_INDEX),
            },
            output: Output::parse_wire(o.bytes(UTXO_OUTPUT), asset)?,
        })
    }
}
