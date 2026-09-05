// SPDX-License-Identifier: BSD-3-Clause-Eco

//! What a warp message *says*.
//!
//! Ported from Go `vms/platformvm/warp/payload` (the envelope) and
//! `vms/platformvm/warp/message` (the four things an L1 says to the P-Chain).
//!
//! Two layers, because they answer different questions. The envelope says
//! **who** sent this and from what address — a hash, or an addressed call. The
//! message says **what**: register this validator, this is its new weight, this
//! network has converted, this validation id is (or is not) registered.
//!
//! The validation id is the **hash of the registration message**. That is the
//! whole identity scheme: a validator's name on the P-Chain is the message that
//! registered it, so two registrations differing in any field are two different
//! validators and a replay of one cannot become the other.

use crate::ids::{hash256, Id, NodeId, ShortId, PRIMARY_NETWORK_ID, SHORT_ID_LEN};
use crate::signer::PUBLIC_KEY_LEN;
use crate::txs::PChainOwner;
use crate::zap;

// ── the envelope (Go: warp/payload)

const KIND_HASH: u8 = 0;
const KIND_ADDRESSED_CALL: u8 = 1;

const OFF_PKIND: usize = 0;
const HASH_OFF_HASH: usize = 1;
const HASH_SIZE: usize = 33;
const AC_OFF_SOURCE: usize = 1;
const AC_OFF_PAYLOAD: usize = 9;
const AC_SIZE: usize = 17;

// ── the messages (Go: warp/message)

const KIND_CONVERSION: u8 = 0;
const KIND_REGISTER: u8 = 1;
const KIND_REGISTRATION: u8 = 2;
const KIND_WEIGHT: u8 = 3;

const OFF_MKIND: usize = 0;

const RV_OFF_CHAIN_ID: usize = 1;
const RV_OFF_BLS_KEY: usize = 33;
const RV_OFF_EXPIRY: usize = 81;
const RV_OFF_WEIGHT: usize = 89;
const RV_OFF_NODE_ID: usize = 97;
const RV_OFF_REM_THRESHOLD: usize = 105;
const RV_OFF_REM_ADDRS: usize = 109;
const RV_OFF_DIS_THRESHOLD: usize = 117;
const RV_OFF_DIS_ADDRS: usize = 121;
const RV_SIZE: usize = 129;
const ADDR_STRIDE: usize = SHORT_ID_LEN;

const CONV_OFF_ID: usize = 1;
const CONV_SIZE: usize = 33;
const REG_OFF_VALIDATION_ID: usize = 1;
const REG_OFF_REGISTERED: usize = 33;
const REG_SIZE: usize = 34;
const VW_OFF_VALIDATION_ID: usize = 1;
const VW_OFF_NONCE: usize = 33;
const VW_OFF_WEIGHT: usize = 41;
const VW_SIZE: usize = 49;

// The conversion preimage: no kind byte, because it is never dispatched — it is
// only ever hashed.
const CD_OFF_CHAIN_ID: usize = 0;
const CD_OFF_MANAGER_ID: usize = 32;
const CD_OFF_MANAGER_ADDR: usize = 64;
const CD_OFF_VALIDATORS: usize = 72;
const CD_OFF_NODE_ID_POOL: usize = 80;
const CD_SIZE: usize = 88;

const CV_NODE_ID_START: usize = 0;
const CV_NODE_ID_LEN: usize = 4;
const CV_BLS_KEY: usize = 8;
const CV_WEIGHT: usize = 56;
const CV_STRIDE: usize = 64;

/// Why a payload is not one.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    Malformed(&'static str),
    /// A kind byte naming nothing this port knows.
    UnknownKind(u8),
    /// The payload parsed, and it is not the kind the caller needs.
    WrongKind,
    /// The primary network is not an L1 and registers no validators this way.
    NotAnL1,
    ZeroWeight,
    BadNodeId,
    BadOwner(crate::components::OwnerError),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Malformed(why) => write!(f, "warp payload: {why}"),
            Error::UnknownKind(k) => write!(f, "warp payload: kind {k} names nothing"),
            Error::WrongKind => write!(f, "warp payload: not the kind this needs"),
            Error::NotAnL1 => write!(f, "warp payload: the primary network is not an L1"),
            Error::ZeroWeight => write!(f, "warp payload: a validator of no weight"),
            Error::BadNodeId => write!(f, "warp payload: not a node id"),
            Error::BadOwner(e) => write!(f, "warp payload: {e}"),
        }
    }
}

impl std::error::Error for Error {}

/// Go: `payload.Hash`. What was sent is named, and nothing more.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Hash {
    pub hash: Id,
    pub bytes: Vec<u8>,
}

impl Hash {
    pub fn build(hash: Id) -> Hash {
        let mut b = zap::Builder::new(zap::HEADER_SIZE + HASH_SIZE);
        let ob = b.start_object(HASH_SIZE);
        b.set_u8(&ob, OFF_PKIND, KIND_HASH);
        b.set_bytes_fixed(&ob, HASH_OFF_HASH, &hash);
        b.finish_as_root(&ob);
        Hash {
            hash,
            bytes: b.finish(),
        }
    }
}

/// Go: `payload.AddressedCall`. It says where the call came from. A
/// destination, if one is expected, is encoded *in* the payload — the envelope
/// only says the source.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Call {
    pub source_address: Vec<u8>,
    pub payload: Vec<u8>,
    pub bytes: Vec<u8>,
}

impl Call {
    pub fn build(source_address: &[u8], payload: &[u8]) -> Call {
        let mut b = zap::Builder::new(
            zap::HEADER_SIZE + AC_SIZE + source_address.len() + payload.len() + 64,
        );
        let ob = b.start_object(AC_SIZE);
        b.set_u8(&ob, OFF_PKIND, KIND_ADDRESSED_CALL);
        b.set_bytes(&ob, AC_OFF_SOURCE, source_address);
        b.set_bytes(&ob, AC_OFF_PAYLOAD, payload);
        b.finish_as_root(&ob);
        Call {
            source_address: source_address.to_vec(),
            payload: payload.to_vec(),
            bytes: b.finish(),
        }
    }
}

/// The two things a warp payload can be.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Envelope {
    Hash(Hash),
    Call(Call),
}

pub fn parse_envelope(raw: &[u8]) -> Result<Envelope, Error> {
    let msg = zap::Message::parse(raw)
        .map_err(|_| Error::Malformed("the envelope is not a zap message"))?;
    let root = msg.root();
    match root.u8(OFF_PKIND) {
        KIND_HASH => Ok(Envelope::Hash(Hash {
            hash: root.id(HASH_OFF_HASH),
            bytes: raw.to_vec(),
        })),
        KIND_ADDRESSED_CALL => Ok(Envelope::Call(Call {
            source_address: root.bytes(AC_OFF_SOURCE).to_vec(),
            payload: root.bytes(AC_OFF_PAYLOAD).to_vec(),
            bytes: raw.to_vec(),
        })),
        other => Err(Error::UnknownKind(other)),
    }
}

/// Go: `message.RegisterL1Validator`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Register {
    pub chain_id: Id,
    /// The raw bytes a warp payload names a node by; a length that is not a
    /// node id is refused by [`Register::verify`] rather than padded.
    pub node_id: Vec<u8>,
    pub bls_public_key: [u8; PUBLIC_KEY_LEN],
    pub expiry: u64,
    pub remaining_balance_owner: PChainOwner,
    pub disable_owner: PChainOwner,
    pub weight: u64,
    pub bytes: Vec<u8>,
}

impl Register {
    #[allow(clippy::too_many_arguments)]
    pub fn build(
        chain_id: Id,
        node_id: &NodeId,
        key: &[u8; PUBLIC_KEY_LEN],
        expiry: u64,
        remaining_balance_owner: &PChainOwner,
        disable_owner: &PChainOwner,
        weight: u64,
    ) -> Register {
        let addrs = remaining_balance_owner.addresses.len() + disable_owner.addresses.len();
        let mut b = zap::Builder::new(zap::HEADER_SIZE + RV_SIZE + addrs * ADDR_STRIDE + 64);
        let (rem_off, rem_count) = write_addrs(&mut b, &remaining_balance_owner.addresses);
        let (dis_off, dis_count) = write_addrs(&mut b, &disable_owner.addresses);
        let ob = b.start_object(RV_SIZE);
        b.set_u8(&ob, OFF_MKIND, KIND_REGISTER);
        b.set_bytes_fixed(&ob, RV_OFF_CHAIN_ID, &chain_id);
        b.set_bytes_fixed(&ob, RV_OFF_BLS_KEY, key);
        b.set_u64(&ob, RV_OFF_EXPIRY, expiry);
        b.set_u64(&ob, RV_OFF_WEIGHT, weight);
        b.set_bytes(&ob, RV_OFF_NODE_ID, node_id.as_bytes());
        b.set_u32(&ob, RV_OFF_REM_THRESHOLD, remaining_balance_owner.threshold);
        b.set_list(&ob, RV_OFF_REM_ADDRS, rem_off, rem_count);
        b.set_u32(&ob, RV_OFF_DIS_THRESHOLD, disable_owner.threshold);
        b.set_list(&ob, RV_OFF_DIS_ADDRS, dis_off, dis_count);
        b.finish_as_root(&ob);

        Register {
            chain_id,
            node_id: node_id.as_bytes().to_vec(),
            bls_public_key: *key,
            expiry,
            remaining_balance_owner: remaining_balance_owner.clone(),
            disable_owner: disable_owner.clone(),
            weight,
            bytes: b.finish(),
        }
    }

    /// Go: `RegisterL1Validator.Verify`.
    pub fn verify(&self) -> Result<(), Error> {
        if self.chain_id == PRIMARY_NETWORK_ID {
            return Err(Error::NotAnL1);
        }
        if self.weight == 0 {
            return Err(Error::ZeroWeight);
        }
        if self.node_id.len() != crate::ids::NODE_ID_LEN || self.node_id.iter().all(|b| *b == 0) {
            return Err(Error::BadNodeId);
        }
        self.remaining_balance_owner
            .as_owners()
            .verify()
            .map_err(Error::BadOwner)?;
        self.disable_owner
            .as_owners()
            .verify()
            .map_err(Error::BadOwner)
    }

    /// The validator's name on the P-Chain: the hash of the message that
    /// registered it.
    pub fn validation_id(&self) -> Id {
        hash256(&self.bytes)
    }
}

/// Go: `message.L1ValidatorRegistration`. `registered: false` means this
/// validation id is not and can never become a validator — which is what makes
/// an expiry final rather than a retry.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Registration {
    pub validation_id: Id,
    pub registered: bool,
    pub bytes: Vec<u8>,
}

impl Registration {
    pub fn build(validation_id: Id, registered: bool) -> Registration {
        let mut b = zap::Builder::new(zap::HEADER_SIZE + REG_SIZE);
        let ob = b.start_object(REG_SIZE);
        b.set_u8(&ob, OFF_MKIND, KIND_REGISTRATION);
        b.set_bytes_fixed(&ob, REG_OFF_VALIDATION_ID, &validation_id);
        b.set_u8(&ob, REG_OFF_REGISTERED, u8::from(registered));
        b.finish_as_root(&ob);
        Registration {
            validation_id,
            registered,
            bytes: b.finish(),
        }
    }
}

/// Go: `message.L1ValidatorWeight`. The nonce is what stops an old weight from
/// being replayed over a newer one.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Weight {
    pub validation_id: Id,
    pub nonce: u64,
    pub weight: u64,
    pub bytes: Vec<u8>,
}

impl Weight {
    pub fn build(validation_id: Id, nonce: u64, weight: u64) -> Weight {
        let mut b = zap::Builder::new(zap::HEADER_SIZE + VW_SIZE);
        let ob = b.start_object(VW_SIZE);
        b.set_u8(&ob, OFF_MKIND, KIND_WEIGHT);
        b.set_bytes_fixed(&ob, VW_OFF_VALIDATION_ID, &validation_id);
        b.set_u64(&ob, VW_OFF_NONCE, nonce);
        b.set_u64(&ob, VW_OFF_WEIGHT, weight);
        b.finish_as_root(&ob);
        Weight {
            validation_id,
            nonce,
            weight,
            bytes: b.finish(),
        }
    }
}

/// Go: `message.ChainToL1Conversion`. It carries only the *id* of the
/// conversion — the data itself is hashed to that id and lives on the chain
/// that converted.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Conversion {
    pub id: Id,
    pub bytes: Vec<u8>,
}

impl Conversion {
    pub fn build(id: Id) -> Conversion {
        let mut b = zap::Builder::new(zap::HEADER_SIZE + CONV_SIZE);
        let ob = b.start_object(CONV_SIZE);
        b.set_u8(&ob, OFF_MKIND, KIND_CONVERSION);
        b.set_bytes_fixed(&ob, CONV_OFF_ID, &id);
        b.finish_as_root(&ob);
        Conversion {
            id,
            bytes: b.finish(),
        }
    }
}

/// The four things an L1 says.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Message {
    Conversion(Conversion),
    Register(Register),
    Registration(Registration),
    Weight(Weight),
}

pub fn parse_message(raw: &[u8]) -> Result<Message, Error> {
    let msg =
        zap::Message::parse(raw).map_err(|_| Error::Malformed("the message is not a zap message"))?;
    let root = msg.root();
    match root.u8(OFF_MKIND) {
        KIND_CONVERSION => Ok(Message::Conversion(Conversion {
            id: root.id(CONV_OFF_ID),
            bytes: raw.to_vec(),
        })),
        KIND_REGISTER => {
            let mut bls_public_key = [0u8; PUBLIC_KEY_LEN];
            let key = root.bytes_fixed(RV_OFF_BLS_KEY, PUBLIC_KEY_LEN);
            bls_public_key[..key.len()].copy_from_slice(key);
            Ok(Message::Register(Register {
                chain_id: root.id(RV_OFF_CHAIN_ID),
                node_id: root.bytes(RV_OFF_NODE_ID).to_vec(),
                bls_public_key,
                expiry: root.u64(RV_OFF_EXPIRY),
                weight: root.u64(RV_OFF_WEIGHT),
                remaining_balance_owner: read_owner(
                    &root,
                    RV_OFF_REM_THRESHOLD,
                    RV_OFF_REM_ADDRS,
                ),
                disable_owner: read_owner(&root, RV_OFF_DIS_THRESHOLD, RV_OFF_DIS_ADDRS),
                bytes: raw.to_vec(),
            }))
        }
        KIND_REGISTRATION => Ok(Message::Registration(Registration {
            validation_id: root.id(REG_OFF_VALIDATION_ID),
            registered: root.bool(REG_OFF_REGISTERED),
            bytes: raw.to_vec(),
        })),
        KIND_WEIGHT => Ok(Message::Weight(Weight {
            validation_id: root.id(VW_OFF_VALIDATION_ID),
            nonce: root.u64(VW_OFF_NONCE),
            weight: root.u64(VW_OFF_WEIGHT),
            bytes: raw.to_vec(),
        })),
        other => Err(Error::UnknownKind(other)),
    }
}

/// One validator, as the conversion preimage names it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ConversionValidator {
    pub node_id: Vec<u8>,
    pub bls_public_key: [u8; PUBLIC_KEY_LEN],
    pub weight: u64,
}

/// What a network became when it went sovereign: a standalone hash preimage
/// with no kind byte, because it is never dispatched.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct ConversionData {
    pub chain_id: Id,
    pub manager_chain_id: Id,
    pub manager_address: Vec<u8>,
    pub validators: Vec<ConversionValidator>,
}

impl ConversionData {
    /// The one canonical encoding, which is also the preimage of the id — so
    /// the id and the bytes can never diverge.
    pub fn encode(&self) -> Vec<u8> {
        let mut b = zap::Builder::new(
            zap::HEADER_SIZE
                + CD_SIZE
                + self.validators.len() * CV_STRIDE
                + self.manager_address.len()
                + 256,
        );

        let mut node_id_pool: Vec<u8> = Vec::new();
        let (vdr_off, vdr_count) = if self.validators.is_empty() {
            (0, 0)
        } else {
            let mut lb = b.start_list();
            for v in &self.validators {
                let mut e = [0u8; CV_STRIDE];
                e[CV_NODE_ID_START..CV_NODE_ID_START + 4]
                    .copy_from_slice(&(node_id_pool.len() as u32).to_le_bytes());
                e[CV_NODE_ID_LEN..CV_NODE_ID_LEN + 4]
                    .copy_from_slice(&(v.node_id.len() as u32).to_le_bytes());
                node_id_pool.extend_from_slice(&v.node_id);
                e[CV_BLS_KEY..CV_BLS_KEY + PUBLIC_KEY_LEN].copy_from_slice(&v.bls_public_key);
                e[CV_WEIGHT..CV_WEIGHT + 8].copy_from_slice(&v.weight.to_le_bytes());
                b.list_bytes(&mut lb, &e);
            }
            (lb.offset(), self.validators.len())
        };

        let ob = b.start_object(CD_SIZE);
        b.set_bytes_fixed(&ob, CD_OFF_CHAIN_ID, &self.chain_id);
        b.set_bytes_fixed(&ob, CD_OFF_MANAGER_ID, &self.manager_chain_id);
        b.set_bytes(&ob, CD_OFF_MANAGER_ADDR, &self.manager_address);
        b.set_list(&ob, CD_OFF_VALIDATORS, vdr_off, vdr_count);
        b.set_bytes(&ob, CD_OFF_NODE_ID_POOL, &node_id_pool);
        b.finish_as_root(&ob);
        b.finish()
    }

    /// The id every later message about this L1 refers to.
    pub fn conversion_id(&self) -> Id {
        hash256(&self.encode())
    }
}

fn write_addrs(b: &mut zap::Builder, addrs: &[ShortId]) -> (usize, usize) {
    if addrs.is_empty() {
        return (0, 0);
    }
    let mut lb = b.start_list();
    for a in addrs {
        b.list_bytes(&mut lb, a.as_bytes());
    }
    // `list_bytes` counts bytes, so the element count is the caller's to give.
    (lb.offset(), addrs.len())
}

fn read_owner(root: &zap::Object<'_>, threshold_off: usize, addrs_off: usize) -> PChainOwner {
    let list = root.list(addrs_off, ADDR_STRIDE);
    let mut addresses = Vec::with_capacity(list.len());
    for i in 0..list.len() {
        let mut a = [0u8; SHORT_ID_LEN];
        let raw = list.object(i, ADDR_STRIDE).bytes_fixed(0, ADDR_STRIDE);
        a[..raw.len()].copy_from_slice(raw);
        addresses.push(ShortId(a));
    }
    PChainOwner {
        threshold: root.u32(threshold_off),
        addresses,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn owner(threshold: u32, n: u8) -> PChainOwner {
        PChainOwner {
            threshold,
            addresses: (0..n).map(|i| ShortId([i + 1; SHORT_ID_LEN])).collect(),
        }
    }

    fn register() -> Register {
        Register::build(
            [4; 32],
            &NodeId([5; 20]),
            &[6; PUBLIC_KEY_LEN],
            1_700_000_000,
            &owner(1, 2),
            &owner(1, 1),
            100,
        )
    }

    #[test]
    fn an_envelope_survives_the_trip_to_the_wire_and_back() {
        let h = Hash::build([9; 32]);
        assert_eq!(parse_envelope(&h.bytes), Ok(Envelope::Hash(h)));

        let c = Call::build(&[1, 2, 3], b"a payload");
        assert_eq!(parse_envelope(&c.bytes), Ok(Envelope::Call(c)));
    }

    #[test]
    fn an_envelope_kind_naming_nothing_is_refused() {
        let mut b = zap::Builder::new(zap::HEADER_SIZE + HASH_SIZE);
        let ob = b.start_object(HASH_SIZE);
        b.set_u8(&ob, OFF_PKIND, 7);
        b.finish_as_root(&ob);
        assert_eq!(parse_envelope(&b.finish()), Err(Error::UnknownKind(7)));
    }

    #[test]
    fn every_message_survives_the_trip_to_the_wire_and_back() {
        let r = register();
        assert_eq!(parse_message(&r.bytes), Ok(Message::Register(r)));

        let c = Conversion::build([2; 32]);
        assert_eq!(parse_message(&c.bytes), Ok(Message::Conversion(c)));

        for registered in [true, false] {
            let g = Registration::build([3; 32], registered);
            assert_eq!(parse_message(&g.bytes), Ok(Message::Registration(g)));
        }

        let w = Weight::build([4; 32], 7, 0);
        assert_eq!(parse_message(&w.bytes), Ok(Message::Weight(w)));
    }

    #[test]
    fn a_message_kind_naming_nothing_is_refused() {
        let mut b = zap::Builder::new(zap::HEADER_SIZE + VW_SIZE);
        let ob = b.start_object(VW_SIZE);
        b.set_u8(&ob, OFF_MKIND, 9);
        b.finish_as_root(&ob);
        assert_eq!(parse_message(&b.finish()), Err(Error::UnknownKind(9)));
    }

    #[test]
    fn a_validators_name_is_the_hash_of_the_message_that_registered_it() {
        // Two registrations differing anywhere are two validators, so a replay
        // of one can never become the other.
        let a = register();
        let mut b = Register::build(
            [4; 32],
            &NodeId([5; 20]),
            &[6; PUBLIC_KEY_LEN],
            1_700_000_000,
            &owner(1, 2),
            &owner(1, 1),
            101, // one unit of weight apart
        );
        assert_ne!(a.validation_id(), b.validation_id());
        assert_eq!(a.validation_id(), hash256(&a.bytes));
        b.weight = 100;
        assert_ne!(
            a.validation_id(),
            b.validation_id(),
            "the id is over the BYTES, not over the fields as later edited"
        );
    }

    #[test]
    fn the_primary_network_registers_no_validator_this_way() {
        let mut r = register();
        r.chain_id = PRIMARY_NETWORK_ID;
        assert_eq!(r.verify(), Err(Error::NotAnL1));
    }

    #[test]
    fn a_registration_of_no_weight_or_no_node_is_refused() {
        let mut r = register();
        r.weight = 0;
        assert_eq!(r.verify(), Err(Error::ZeroWeight));

        let mut r = register();
        r.node_id = vec![0; 20];
        assert_eq!(r.verify(), Err(Error::BadNodeId), "the empty node id is nobody");

        let mut r = register();
        r.node_id = vec![1; 19];
        assert_eq!(r.verify(), Err(Error::BadNodeId));
    }

    #[test]
    fn a_registration_naming_an_owner_nobody_could_satisfy_is_refused() {
        let mut r = register();
        r.remaining_balance_owner = owner(3, 1);
        assert!(matches!(r.verify(), Err(Error::BadOwner(_))));

        let mut r = register();
        r.disable_owner = owner(3, 1);
        assert!(matches!(r.verify(), Err(Error::BadOwner(_))));
    }

    #[test]
    fn a_well_formed_registration_verifies() {
        assert_eq!(register().verify(), Ok(()));
    }

    #[test]
    fn the_conversion_id_is_over_the_whole_set_and_the_manager() {
        let base = ConversionData {
            chain_id: [1; 32],
            manager_chain_id: [2; 32],
            manager_address: vec![3; 20],
            validators: vec![
                ConversionValidator {
                    node_id: vec![4; 20],
                    bls_public_key: [5; PUBLIC_KEY_LEN],
                    weight: 10,
                },
                ConversionValidator {
                    node_id: vec![6; 20],
                    bls_public_key: [7; PUBLIC_KEY_LEN],
                    weight: 20,
                },
            ],
        };
        assert_eq!(base.conversion_id(), hash256(&base.encode()));

        // Every field moves the id: a conversion nobody can restate is what
        // makes a later message about this L1 name this L1.
        let mut weight = base.clone();
        weight.validators[1].weight = 21;
        assert_ne!(base.conversion_id(), weight.conversion_id());

        let mut manager = base.clone();
        manager.manager_address = vec![3; 19];
        assert_ne!(base.conversion_id(), manager.conversion_id());

        let mut order = base.clone();
        order.validators.swap(0, 1);
        assert_ne!(
            base.conversion_id(),
            order.conversion_id(),
            "the set is a sequence, and its order is part of what was agreed"
        );

        // An empty set still encodes, and to something of its own.
        let empty = ConversionData {
            validators: vec![],
            ..base.clone()
        };
        assert_ne!(base.conversion_id(), empty.conversion_id());
    }

    #[test]
    fn an_owner_with_no_addresses_writes_an_empty_list_that_reads_back_empty() {
        let r = Register::build(
            [4; 32],
            &NodeId([5; 20]),
            &[6; PUBLIC_KEY_LEN],
            1,
            &PChainOwner::default(),
            &PChainOwner::default(),
            1,
        );
        let Ok(Message::Register(read)) = parse_message(&r.bytes) else {
            panic!("a registration reads back as one")
        };
        assert_eq!(read.remaining_balance_owner, PChainOwner::default());
        assert_eq!(read.disable_owner, PChainOwner::default());
    }
}
