// SPDX-License-Identifier: BSD-3-Clause-Eco

//! The names a chain uses.
//!
//! `Id` is the host seam's own id — `lux_consensus::finality::Id`, a
//! `[u8; 32]` — so a block this chain builds is named the same way the node
//! that certifies it names it, with nothing in between to translate.
//!
//! A node and an address are both twenty bytes: a node id is the hash of a
//! staking certificate and an address is the hash of a public key. They are
//! different things and are different types here, because a validator set
//! keyed by an address, or an owner keyed by a node, would both compile.

pub use lux_consensus::finality::Id;

/// The all-zero id.
pub const EMPTY: Id = [0u8; 32];

/// The primary network. It has no creating transaction, so it has no id, and
/// the empty id is what stands in for it everywhere the P-Chain asks which
/// network something validates.
pub const PRIMARY_NETWORK_ID: Id = EMPTY;

/// Bytes in a node id.
pub const NODE_ID_LEN: usize = 20;
/// Bytes in an address.
pub const SHORT_ID_LEN: usize = 20;

/// A validator.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct NodeId(pub [u8; NODE_ID_LEN]);

impl NodeId {
    pub const EMPTY: NodeId = NodeId([0u8; NODE_ID_LEN]);

    pub fn as_bytes(&self) -> &[u8; NODE_ID_LEN] {
        &self.0
    }

    pub fn is_empty(&self) -> bool {
        self.0 == [0u8; NODE_ID_LEN]
    }
}

/// An address a UTXO can be owned by.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct ShortId(pub [u8; SHORT_ID_LEN]);

impl ShortId {
    pub fn as_bytes(&self) -> &[u8; SHORT_ID_LEN] {
        &self.0
    }
}

/// The 32 bytes at `field`, as an id.
///
/// Short reads answer zeros, for the same reason every ZAP accessor does: a
/// truncated buffer has to decode to a value that fails verification, not to a
/// panic — a hostile transaction that could crash the reader would never reach
/// the check that refuses it.
pub fn id_at(o: lux_zap::zap::Object<'_>, field: usize) -> Id {
    let mut id = EMPTY;
    let src = o.bytes_fixed(field, 32);
    id[..src.len()].copy_from_slice(src);
    id
}

/// The twenty bytes at `field`, as an address.
pub fn short_at(o: lux_zap::zap::Object<'_>, field: usize) -> ShortId {
    let mut a = [0u8; SHORT_ID_LEN];
    let src = o.bytes_fixed(field, SHORT_ID_LEN);
    a[..src.len()].copy_from_slice(src);
    ShortId(a)
}

/// `sha256` — what names a transaction and what names a block.
pub fn hash256(bytes: &[u8]) -> Id {
    lux_gpu::sha256(bytes)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_primary_network_has_no_id() {
        // Go states this as `constants.PrimaryNetworkID = ids.Empty`. Every
        // "is this the primary network" test in the executor is this equality.
        assert_eq!(PRIMARY_NETWORK_ID, EMPTY);
    }

    #[test]
    fn a_transaction_is_named_by_sha256_of_its_bytes() {
        // The empty-input digest, so the algorithm itself is pinned rather
        // than just its self-consistency.
        let want: [u8; 32] = [
            0xe3, 0xb0, 0xc4, 0x42, 0x98, 0xfc, 0x1c, 0x14, 0x9a, 0xfb, 0xf4, 0xc8, 0x99, 0x6f,
            0xb9, 0x24, 0x27, 0xae, 0x41, 0xe4, 0x64, 0x9b, 0x93, 0x4c, 0xa4, 0x95, 0x99, 0x1b,
            0x78, 0x52, 0xb8, 0x55,
        ];
        assert_eq!(hash256(&[]), want);
    }

    #[test]
    fn ids_order_by_their_bytes() {
        // The staker set breaks ties on tx id with a byte comparison, so the
        // ordering has to be the byte ordering and not, say, numeric.
        let mut a = [0u8; 32];
        let mut b = [0u8; 32];
        a[0] = 1;
        b[1] = 1;
        assert!(a > b);
    }
}
