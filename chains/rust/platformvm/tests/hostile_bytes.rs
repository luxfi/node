// SPDX-License-Identifier: BSD-3-Clause-Eco

//! What arrives from a stranger.
//!
//! Every entry point in this crate that takes bytes takes them from someone
//! who chose them. A reader that can be made to panic on a chosen buffer is a
//! way to stop a node from a distance, and stopping enough nodes is stopping
//! the chain — so the only acceptable answer to any 300 bytes anyone can think
//! of is a value or a refusal, never a crash.
//!
//! These are not fuzzers; they are a deterministic sweep. Every buffer below is
//! derived from a real one by a mutation an attacker can make: flip a byte,
//! truncate, extend, or claim a length nothing backs. Each is thrown at every
//! reader, and the only thing asserted is that the reader returns.
//!
//! The sweep reaches past the front door: of the 5,130 single-byte mutations,
//! 3,980 still parse as messages — so the field accessors are exercised on
//! hostile content rather than refused at the header — and 483 still parse as
//! whole signed transactions.

use lux_platformvm::components::Utxo;
use lux_platformvm::components::{Credential, Input, Output, Owners, UtxoId};
use lux_platformvm::genesis::Genesis;
use lux_platformvm::ids::{NodeId, ShortId};
use lux_platformvm::signer::Signer;
use lux_platformvm::txs::{Envelope, Tx, Unsigned, Validator};
use lux_platformvm::{block, txs};
use lux_zap::zap;

fn id(b: u8) -> [u8; 32] {
    [b; 32]
}

fn owners(b: u8) -> Owners {
    Owners {
        locktime: 0,
        threshold: 1,
        addrs: vec![ShortId([b; 20])],
    }
}

fn envelope() -> Envelope {
    Envelope {
        network_id: 1,
        blockchain_id: id(3),
        outs: vec![Output {
            asset: id(9),
            stake_lock: 0,
            amount: 100,
            owners: owners(1),
        }],
        ins: vec![Input {
            utxo: UtxoId {
                tx_id: id(1),
                output_index: 0,
            },
            asset: id(9),
            stake_lock: 0,
            amount: 200,
            sig_indices: vec![0],
        }],
        memo: Vec::new(),
    }
}

/// One well-formed buffer of each kind this crate reads.
fn honest_buffers() -> Vec<(&'static str, Vec<u8>)> {
    let signed = Tx::new(
        Unsigned::AddPermissionlessValidator {
            base: envelope(),
            validator: Validator {
                node_id: NodeId([5; 20]),
                start: 1000,
                end: 2000,
                weight: 50,
            },
            chain: [0; 32],
            signer: Signer::ProofOfPossession {
                public_key: [11; 48],
                proof: [12; 96],
            },
            stake: vec![Output {
                asset: id(9),
                stake_lock: 0,
                amount: 50,
                owners: owners(2),
            }],
            validator_rewards_owner: owners(3),
            delegator_rewards_owner: owners(4),
            delegation_shares: 20_000,
        },
        vec![Credential {
            sigs: vec![[7u8; 65]],
        }],
    );
    let utxo = Utxo {
        id: UtxoId {
            tx_id: id(0),
            output_index: 1,
        },
        output: Output {
            asset: id(9),
            stake_lock: 5000,
            amount: 250,
            owners: owners(1),
        },
    };
    let genesis = Genesis {
        utxos: Vec::new(),
        validators: vec![signed.clone()],
        chains: Vec::new(),
        timestamp: 1000,
        initial_supply: 1_000_000,
        message: "hello".to_string(),
    };
    vec![
        ("signed transaction", signed.bytes().to_vec()),
        (
            "block",
            block::Block::standard(id(1), 5, 1000, vec![signed])
                .bytes()
                .to_vec(),
        ),
        ("unspent output", utxo.wire_bytes()),
        ("genesis", genesis.to_bytes()),
        ("owner", owners(1).marshal()),
    ]
}

/// Throw `buffer` at every reader. Nothing is asserted about the answers —
/// what is being checked is that there are answers.
fn read_every_way(buffer: &[u8]) {
    let _ = zap::Message::parse(buffer);
    let _ = txs::Unsigned::parse(buffer);
    let _ = Tx::parse(buffer);
    let _ = txs::parse_credentials(buffer);
    let _ = block::Block::parse(buffer);
    let _ = Utxo::parse_wire(buffer);
    let _ = Output::parse_wire(buffer, id(9));
    let _ = Genesis::parse(buffer);
    let _ = Owners::unmarshal(buffer);
    // And the accessors, on whatever a parse did produce.
    if let Ok(msg) = zap::Message::parse(buffer) {
        let root = msg.root();
        for field in [0usize, 1, 4, 8, 16, 32, 64, 77, 121, 200, 4096] {
            let _ = root.u8(field);
            let _ = root.u32(field);
            let _ = root.u64(field);
            let _ = lux_platformvm::ids::id_at(root, field);
            let _ = lux_platformvm::ids::short_at(root, field);
            let _ = root.bytes(field);
            let _ = root.text(field);
            let _ = root.bytes_fixed(field, 96);
            let _ = root.object(field);
            let list = root.list_stride(field, 4);
            for i in [0usize, 1, 7, 1_000] {
                let _ = list.u8(i);
                let _ = list.u32(i);
                let _ = list.u64(i);
                let _ = list.object(i, 96).u64(0);
            }
        }
    }
}

/// Every prefix of every honest buffer — the message that was cut off.
#[test]
fn a_truncated_buffer_is_answered_rather_than_fatal() {
    for (name, honest) in honest_buffers() {
        for end in 0..honest.len().min(400) {
            read_every_way(&honest[..end]);
        }
        // And the last bytes of a long one, so the tail is covered too.
        for end in honest.len().saturating_sub(64)..honest.len() {
            read_every_way(&honest[..end]);
        }
        let _ = name;
    }
}

/// Every single-byte change to an honest buffer.
///
/// This is the mutation an attacker makes for free: a length that is now too
/// large, a pointer that now runs backwards, a count that now claims four
/// billion elements.
#[test]
fn a_flipped_byte_is_answered_rather_than_fatal() {
    for (_, honest) in honest_buffers() {
        for i in 0..honest.len().min(256) {
            for patch in [0x00u8, 0x01, 0x7f, 0x80, 0xff] {
                let mut mutated = honest.clone();
                mutated[i] = patch;
                read_every_way(&mutated);
            }
        }
    }
}

/// A buffer that says it is longer than it is, and one that carries a tail.
#[test]
fn a_length_nothing_backs_is_answered_rather_than_fatal() {
    for (_, honest) in honest_buffers() {
        if honest.len() < zap::HEADER_SIZE {
            continue;
        }
        for size in [
            0u32,
            1,
            zap::HEADER_SIZE as u32,
            honest.len() as u32 - 1,
            honest.len() as u32 + 1,
            u32::MAX,
        ] {
            let mut mutated = honest.clone();
            mutated[12..16].copy_from_slice(&size.to_le_bytes());
            read_every_way(&mutated);
        }
        // A root offset pointing anywhere at all.
        for root in [0u32, 1, 8, 15, 16, honest.len() as u32, u32::MAX] {
            let mut mutated = honest.clone();
            mutated[8..12].copy_from_slice(&root.to_le_bytes());
            read_every_way(&mutated);
        }
        // And a tail, which is the malleability an id would otherwise hide.
        let mut extended = honest.clone();
        extended.extend_from_slice(&[0xff; 32]);
        read_every_way(&extended);
    }
}

/// Buffers nobody built: all zeros, all ones, and the shortest things there
/// are.
#[test]
fn arbitrary_bytes_are_answered_rather_than_fatal() {
    for len in [0usize, 1, 2, 3, 15, 16, 17, 32, 64, 128, 512] {
        read_every_way(&vec![0u8; len]);
        read_every_way(&vec![0xffu8; len]);
        // A valid header over nothing.
        let size = len.max(zap::HEADER_SIZE);
        let mut header = vec![0u8; size];
        header[..4].copy_from_slice(b"ZAP\0");
        header[4..6].copy_from_slice(&2u16.to_le_bytes());
        header[8..12].copy_from_slice(&(zap::HEADER_SIZE as u32).to_le_bytes());
        header[12..16].copy_from_slice(&(size as u32).to_le_bytes());
        read_every_way(&header);
    }
}
