// SPDX-License-Identifier: BSD-3-Clause-Eco

//! How the chain is born.
//!
//! A Lux network has exactly one P-Chain, and the P-Chain's genesis IS the
//! network's genesis: who is staking at the first instant, what the chains
//! are, what exists to be spent, and how much of the asset there is. Anyone
//! can compute the whole initial state from these bytes and nobody has to be
//! trusted to publish it, which is what makes "the network started here" a
//! fact a reader can check rather than an assertion.
//!
//! Go states this as `vms/platformvm/genesis`. The blob is one ZAP object: a
//! few scalars, and three lists of self-delimiting things — the unspent
//! outputs, the transactions that admit the first validators, and the
//! transactions that make the first chains. Each embedded transaction is
//! stored as its own signed bytes and read back through [`Tx::parse`], so its
//! id — the hash of those bytes — survives the round trip exactly. Nothing is
//! re-encoded on the way in or out.

use crate::components::{Utxo, WireError};
use crate::ids::{hash256, Id, PRIMARY_NETWORK_ID};
use crate::reward;
use crate::state::{Staker, State};
use crate::txs::{Tx, Unsigned};
use crate::zap;
use std::time::Duration;

/// One unspent output at genesis, and the note that came with it.
///
/// The note is opaque here — it is whatever the allocation carried, kept so
/// the bytes round-trip — and nothing in this chain reads it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Allocation {
    pub utxo: Utxo,
    pub message: Vec<u8>,
}

/// The state a network starts from.
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Genesis {
    pub utxos: Vec<Allocation>,
    /// The transactions that admit the first validators. Each is an
    /// `AddValidator` or an `AddPermissionlessValidator`.
    pub validators: Vec<Tx>,
    /// The transactions that make the first chains.
    pub chains: Vec<Tx>,
    pub timestamp: u64,
    pub initial_supply: u64,
    pub message: String,
}

/// Why a genesis blob is not one.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    Wire(zap::Error),
    Envelope(WireError),
    /// A declared element length that runs past the blob it slices.
    LengthOverrunsBlob {
        index: usize,
        length: usize,
        blob: usize,
    },
    Tx {
        index: usize,
        why: crate::txs::Error,
    },
    Utxo {
        index: usize,
        why: WireError,
    },
    /// A genesis validator transaction that admits no validator.
    NotAValidator(usize),
    /// A genesis chain transaction that makes no chain.
    NotAChain(usize),
    /// The supply plus what genesis promises to mint does not fit.
    SupplyOverflow,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Wire(e) => write!(f, "{e}"),
            Error::Envelope(e) => write!(f, "{e}"),
            Error::LengthOverrunsBlob {
                index,
                length,
                blob,
            } => write!(f, "element {index} length {length} overruns blob ({blob})"),
            Error::Tx { index, why } => write!(f, "transaction {index}: {why}"),
            Error::Utxo { index, why } => write!(f, "unspent output {index}: {why}"),
            Error::NotAValidator(i) => {
                write!(f, "genesis validator {i} admits no validator")
            }
            Error::NotAChain(i) => write!(f, "genesis chain {i} makes no chain"),
            Error::SupplyOverflow => write!(f, "the supply genesis promises does not fit"),
        }
    }
}

impl std::error::Error for Error {}

impl From<zap::Error> for Error {
    fn from(e: zap::Error) -> Self {
        Error::Wire(e)
    }
}

// The genesis object.
const TIMESTAMP: usize = 0;
const INITIAL_SUPPLY: usize = 8;
const MESSAGE: usize = 16;
const UTXO_LENS: usize = 24;
const UTXO_BLOB: usize = 32;
const VDR_LENS: usize = 40;
const VDR_BLOB: usize = 48;
const CHAIN_LENS: usize = 56;
const CHAIN_BLOB: usize = 64;
const SIZE: usize = 72;
/// One `u32` per element, saying how long it is.
const LEN_STRIDE: usize = 4;

// One allocation: the unspent output's envelope, and the note.
const ALLOC_UTXO: usize = 0;
const ALLOC_MESSAGE: usize = 8;
const ALLOC_SIZE: usize = 16;

impl Allocation {
    fn to_bytes(&self) -> Vec<u8> {
        let wire = self.utxo.wire_bytes();
        let mut b =
            zap::Builder::new(zap::HEADER_SIZE + ALLOC_SIZE + wire.len() + self.message.len());
        let ob = b.start_object(ALLOC_SIZE);
        b.set_bytes(&ob, ALLOC_UTXO, &wire);
        b.set_bytes(&ob, ALLOC_MESSAGE, &self.message);
        b.finish_as_root(&ob);
        b.finish()
    }

    fn parse(b: &[u8]) -> Result<Allocation, WireError> {
        let msg = zap::Message::parse(b)?;
        let o = msg.root();
        Ok(Allocation {
            utxo: Utxo::parse_wire(o.bytes(ALLOC_UTXO))?,
            message: o.bytes(ALLOC_MESSAGE).to_vec(),
        })
    }
}

/// Write a list of self-delimiting things as their lengths plus their bytes,
/// end to end. It is the framing a block uses for its transactions, for the
/// same reason: each element already says how long it is, and one length list
/// lets a reader cut them apart without parsing any of them.
fn write_blobs(b: &mut zap::Builder, blobs: &[Vec<u8>]) -> ((usize, usize), Vec<u8>) {
    if blobs.is_empty() {
        return ((0, 0), Vec::new());
    }
    let mut blob = Vec::new();
    let mut lb = b.start_list();
    for raw in blobs {
        b.list_u32(&mut lb, raw.len() as u32);
        blob.extend_from_slice(raw);
    }
    ((lb.offset(), lb.count()), blob)
}

fn read_blobs<'a>(
    o: zap::Object<'a>,
    len_off: usize,
    blob_off: usize,
) -> Result<Vec<&'a [u8]>, Error> {
    let lengths = o.list(len_off, LEN_STRIDE);
    let n = lengths.len();
    if n == 0 {
        return Ok(Vec::new());
    }
    let blob = o.bytes(blob_off);
    let mut out = Vec::with_capacity(n);
    let mut cursor = 0usize;
    for i in 0..n {
        let size = lengths.u32(i) as usize;
        if cursor + size > blob.len() {
            return Err(Error::LengthOverrunsBlob {
                index: i,
                length: size,
                blob: blob.len(),
            });
        }
        out.push(&blob[cursor..cursor + size]);
        cursor += size;
    }
    Ok(out)
}

impl Genesis {
    /// The bytes a network is published as.
    pub fn to_bytes(&self) -> Vec<u8> {
        let utxo_blobs: Vec<Vec<u8>> = self.utxos.iter().map(|u| u.to_bytes()).collect();
        let vdr_blobs: Vec<Vec<u8>> = self.validators.iter().map(|t| t.bytes().to_vec()).collect();
        let chain_blobs: Vec<Vec<u8>> = self.chains.iter().map(|t| t.bytes().to_vec()).collect();

        let mut b = zap::Builder::new(zap::HEADER_SIZE + SIZE + 1024);
        let (utxo_lens, utxo_blob) = write_blobs(&mut b, &utxo_blobs);
        let (vdr_lens, vdr_blob) = write_blobs(&mut b, &vdr_blobs);
        let (chain_lens, chain_blob) = write_blobs(&mut b, &chain_blobs);

        let ob = b.start_object(SIZE);
        b.set_u64(&ob, TIMESTAMP, self.timestamp);
        b.set_u64(&ob, INITIAL_SUPPLY, self.initial_supply);
        b.set_bytes(&ob, MESSAGE, self.message.as_bytes());
        b.set_list(&ob, UTXO_LENS, utxo_lens.0, utxo_lens.1);
        b.set_bytes(&ob, UTXO_BLOB, &utxo_blob);
        b.set_list(&ob, VDR_LENS, vdr_lens.0, vdr_lens.1);
        b.set_bytes(&ob, VDR_BLOB, &vdr_blob);
        b.set_list(&ob, CHAIN_LENS, chain_lens.0, chain_lens.1);
        b.set_bytes(&ob, CHAIN_BLOB, &chain_blob);
        b.finish_as_root(&ob);
        b.finish()
    }

    /// Read a network's genesis.
    pub fn parse(bytes: &[u8]) -> Result<Genesis, Error> {
        let msg = zap::Message::parse(bytes)?;
        let o = msg.root();

        let mut utxos = Vec::new();
        for (i, raw) in read_blobs(o, UTXO_LENS, UTXO_BLOB)?.into_iter().enumerate() {
            utxos.push(Allocation::parse(raw).map_err(|why| Error::Utxo { index: i, why })?);
        }
        let parse_txs = |blobs: Vec<&[u8]>| -> Result<Vec<Tx>, Error> {
            blobs
                .into_iter()
                .enumerate()
                .map(|(i, raw)| Tx::parse(raw).map_err(|why| Error::Tx { index: i, why }))
                .collect()
        };
        Ok(Genesis {
            utxos,
            validators: parse_txs(read_blobs(o, VDR_LENS, VDR_BLOB)?)?,
            chains: parse_txs(read_blobs(o, CHAIN_LENS, CHAIN_BLOB)?)?,
            timestamp: o.u64(TIMESTAMP),
            initial_supply: o.u64(INITIAL_SUPPLY),
            message: o.text(MESSAGE).to_string(),
        })
    }

    /// The name of the block a chain starting from these bytes begins at.
    ///
    /// Go derives it the same way — the hash of the genesis bytes — so two
    /// implementations reading the same publication agree about which chain
    /// they are on before they have agreed about anything else.
    pub fn id(&self) -> Id {
        hash256(&self.to_bytes())
    }

    /// The state the chain holds at its first instant.
    ///
    /// Every genesis validator enters the current set immediately, with the
    /// reward it will be owed already computed and already added to the
    /// supply — the same order Go's `syncGenesis` uses, and the reason a
    /// validator admitted at genesis is paid on exactly the terms a validator
    /// admitted a block later is.
    pub fn state(&self, rewards: &reward::Calculator) -> Result<State, Error> {
        let mut state = State::new();
        state.set_timestamp(self.timestamp);
        state.set_current_supply(PRIMARY_NETWORK_ID, self.initial_supply);

        for allocation in &self.utxos {
            state.add_utxo(allocation.utxo.clone());
        }

        for (i, tx) in self.validators.iter().enumerate() {
            let staker = tx.unsigned.staker().ok_or(Error::NotAValidator(i))?;
            let supply = state
                .current_supply(&PRIMARY_NETWORK_ID)
                .unwrap_or(self.initial_supply);
            // Genesis stakers start now, so the term they are paid for is the
            // whole of what they promised.
            let potential_reward = rewards.calculate(
                Duration::from_secs(staker.validator.end.saturating_sub(self.timestamp)),
                staker.validator.weight,
                supply,
            );
            let new_supply = supply
                .checked_add(potential_reward)
                .ok_or(Error::SupplyOverflow)?;

            state
                .put_current_validator(Staker {
                    tx_id: tx.id(),
                    node_id: staker.validator.node_id,
                    public_key: staker.signer,
                    chain: staker.chain,
                    weight: staker.validator.weight,
                    start_time: self.timestamp,
                    end_time: staker.validator.end,
                    potential_reward,
                    next_time: staker.validator.end,
                    priority: staker.priority_current,
                })
                .map_err(|_| Error::NotAValidator(i))?;
            state.add_tx(tx.clone());
            state.set_current_supply(PRIMARY_NETWORK_ID, new_supply);
        }

        for (i, tx) in self.chains.iter().enumerate() {
            let Unsigned::CreateChain { name, .. } = &tx.unsigned else {
                return Err(Error::NotAChain(i));
            };
            state.add_blockchain(tx.id(), name);
            state.add_tx(tx.clone());
        }

        Ok(state)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::components::{Output, Owners, UtxoId};
    use crate::ids::{NodeId, ShortId};
    use crate::signer::Signer;
    use crate::txs::{Envelope, Validator};

    fn owners(n: u8) -> Owners {
        Owners {
            locktime: 0,
            threshold: 1,
            addrs: vec![ShortId([n; 20])],
        }
    }

    fn output(amount: u64, stake_lock: u64) -> Output {
        Output {
            asset: [9; 32],
            stake_lock,
            amount,
            owners: owners(1),
        }
    }

    fn allocation(index: u32, amount: u64, stake_lock: u64) -> Allocation {
        Allocation {
            utxo: Utxo {
                id: UtxoId {
                    tx_id: [0; 32],
                    output_index: index,
                },
                output: output(amount, stake_lock),
            },
            message: b"hello".to_vec(),
        }
    }

    fn pop() -> Signer {
        let sk = blst::min_pk::SecretKey::key_gen(&[7u8; 32], &[]).unwrap();
        Signer::prove(&sk)
    }

    fn validator_tx(node: u8, weight: u64, end: u64) -> Tx {
        Tx::new(
            Unsigned::AddPermissionlessValidator {
                base: Envelope {
                    network_id: 1,
                    blockchain_id: [0; 32],
                    outs: Vec::new(),
                    ins: Vec::new(),
                    memo: Vec::new(),
                },
                validator: Validator {
                    node_id: NodeId([node; 20]),
                    start: 0,
                    end,
                    weight,
                },
                chain: PRIMARY_NETWORK_ID,
                signer: pop(),
                stake: vec![Output {
                    asset: [9; 32],
                    stake_lock: 0,
                    amount: weight,
                    owners: owners(2),
                }],
                validator_rewards_owner: owners(3),
                delegator_rewards_owner: owners(3),
                delegation_shares: 20_000,
            },
            Vec::new(),
        )
    }

    fn chain_tx(name: &str) -> Tx {
        Tx::new(
            Unsigned::CreateChain {
                base: Envelope {
                    network_id: 1,
                    blockchain_id: [0; 32],
                    outs: Vec::new(),
                    ins: Vec::new(),
                    memo: Vec::new(),
                },
                chain: [6; 32],
                vm_id: [8; 32],
                name: name.to_string(),
                fx_ids: Vec::new(),
                genesis: b"chain genesis".to_vec(),
                chain_auth: Vec::new(),
            },
            Vec::new(),
        )
    }

    fn genesis() -> Genesis {
        Genesis {
            utxos: vec![allocation(0, 100, 0), allocation(1, 250, 5_000)],
            validators: vec![validator_tx(5, 2_000_000, 1_000_000)],
            chains: vec![chain_tx("a chain")],
            timestamp: 1000,
            initial_supply: 360_000_000_000_000,
            message: "let there be light".to_string(),
        }
    }

    #[test]
    fn a_genesis_round_trips_through_its_bytes() {
        let g = genesis();
        let bytes = g.to_bytes();
        let back = Genesis::parse(&bytes).expect("parse");
        assert_eq!(back, g);
        // And writing it again gives the same bytes, which is what makes the
        // name of the first block a fact rather than a choice.
        assert_eq!(back.to_bytes(), bytes);
        assert_eq!(back.id(), g.id());
    }

    #[test]
    fn an_embedded_transaction_keeps_its_name() {
        // The point of storing signed bytes rather than re-encoding: a
        // transaction's id is the hash of the bytes it was published as, and
        // genesis must not change it.
        let g = genesis();
        let back = Genesis::parse(&g.to_bytes()).unwrap();
        assert_eq!(back.validators[0].id(), g.validators[0].id());
        assert_eq!(back.validators[0].bytes(), g.validators[0].bytes());
        assert_eq!(back.chains[0].id(), g.chains[0].id());
    }

    #[test]
    fn an_unspent_output_round_trips_through_its_envelope() {
        for stake_lock in [0u64, 9_999] {
            let u = Utxo {
                id: UtxoId {
                    tx_id: [4; 32],
                    output_index: 7,
                },
                output: output(1234, stake_lock),
            };
            let wire = u.wire_bytes();
            assert_eq!(wire[0], crate::components::TYPE_RESERVED);
            assert_eq!(wire[1], crate::components::SHAPE_UTXO);
            assert_eq!(Utxo::parse_wire(&wire).unwrap(), u);
        }
    }

    #[test]
    fn a_locked_output_is_a_second_envelope_not_a_field() {
        let locked = output(50, 4242).wire_bytes();
        assert_eq!(locked[1], crate::components::SHAPE_LOCKED_OUTPUT);
        let plain = output(50, 0).wire_bytes();
        assert_eq!(plain[1], crate::components::SHAPE_TRANSFER_OUTPUT);
        assert_eq!(plain[0], crate::components::TYPE_SECP256K1);
        // A reader that asks for the wrong shape is told so rather than being
        // handed a plausible answer.
        assert!(matches!(
            Output::parse_wire(&plain[..1], [9; 32]),
            Err(WireError::ShortEnvelope)
        ));
        let mut wrong = plain.clone();
        wrong[1] = crate::components::SHAPE_UTXO;
        assert!(matches!(
            Output::parse_wire(&wrong, [9; 32]),
            Err(WireError::WrongShape { .. })
        ));
    }

    #[test]
    fn an_envelope_with_a_tail_is_refused() {
        // The message says how long it is, so a tail would read the same and
        // hash differently — two names for one value.
        let mut wire = allocation(0, 100, 0).utxo.wire_bytes();
        wire.push(0);
        assert!(matches!(
            Utxo::parse_wire(&wire),
            Err(WireError::TrailingBytes)
        ));
    }

    #[test]
    fn a_length_that_runs_past_the_blob_is_refused() {
        let g = genesis();
        let mut bytes = g.to_bytes();
        // Find the u32 length of the first validator blob and make it huge.
        let msg = zap::Message::parse(&bytes).unwrap();
        let root = msg.root();
        let lens = root.list(VDR_LENS, LEN_STRIDE);
        assert_eq!(lens.len(), 1);
        let real = lens.u32(0);
        let at = bytes
            .windows(4)
            .position(|w| w == real.to_le_bytes())
            .expect("the length is in the buffer");
        bytes[at..at + 4].copy_from_slice(&u32::MAX.to_le_bytes());
        assert!(matches!(
            Genesis::parse(&bytes),
            Err(Error::LengthOverrunsBlob { .. })
        ));
    }

    #[test]
    fn the_first_state_is_what_genesis_says() {
        let g = genesis();
        let rewards = reward::Calculator::new(reward::Config {
            max_consumption_rate: 120_000,
            min_consumption_rate: 100_000,
            minting_period: Duration::from_secs(365 * 24 * 60 * 60),
            supply_cap: 720_000_000_000_000_000,
        });
        let state = g.state(&rewards).expect("a genesis state");

        assert_eq!(state.timestamp(), 1000);
        // Both allocations are there to be spent.
        assert_eq!(
            state
                .utxo(&g.utxos[0].utxo.id.input_id())
                .unwrap()
                .output
                .amount,
            100
        );
        assert_eq!(
            state
                .utxo(&g.utxos[1].utxo.id.input_id())
                .unwrap()
                .output
                .stake_lock,
            5_000
        );

        // The validator is in the CURRENT set at the first instant — nobody
        // waits, and nobody admits it.
        let v = state
            .current_validator(&PRIMARY_NETWORK_ID, &NodeId([5; 20]))
            .expect("the genesis validator validates from the start");
        assert_eq!(v.weight, 2_000_000);
        assert_eq!(v.start_time, 1000);
        assert_eq!(v.end_time, 1_000_000);
        assert!(v.public_key.is_some(), "a primary-network validator signs");
        assert_eq!(v.tx_id, g.validators[0].id());

        // What it will be owed is already promised, and the supply already
        // counts it.
        assert!(v.potential_reward > 0);
        assert_eq!(
            state.current_supply(&PRIMARY_NETWORK_ID).unwrap(),
            g.initial_supply + v.potential_reward
        );

        // And the chain genesis names is taken.
        assert!(state.is_chain_name_taken("a chain"));
    }

    #[test]
    fn a_genesis_naming_something_that_is_not_a_validator_is_refused() {
        let rewards = reward::Calculator::new(reward::Config {
            max_consumption_rate: 120_000,
            min_consumption_rate: 100_000,
            minting_period: Duration::from_secs(365 * 24 * 60 * 60),
            supply_cap: 720_000_000_000_000_000,
        });
        let mut g = genesis();
        g.validators = vec![chain_tx("not a validator")];
        assert_eq!(g.state(&rewards).err(), Some(Error::NotAValidator(0)));

        let mut g = genesis();
        g.chains = vec![validator_tx(6, 10, 100)];
        assert_eq!(g.state(&rewards).err(), Some(Error::NotAChain(0)));
    }

    #[test]
    fn an_empty_genesis_is_still_a_genesis() {
        let g = Genesis {
            timestamp: 42,
            initial_supply: 7,
            ..Default::default()
        };
        let back = Genesis::parse(&g.to_bytes()).unwrap();
        assert_eq!(back, g);
        assert!(back.utxos.is_empty());
        assert!(back.validators.is_empty());
        assert!(back.chains.is_empty());
        assert_eq!(back.message, "");
    }

    /// A lock naming time zero is not a lock.
    ///
    /// Go can hold the same thing — a `LockOut` whose `Locktime` is zero — and
    /// every comparison it makes against it (`now < locktime`) is false, so it
    /// spends as ordinary value. Reading it back as no lock is that same
    /// value, said once.
    #[test]
    fn a_lock_at_time_zero_is_no_lock() {
        let locked_at_zero = {
            let plain = output(50, 0).wire_bytes();
            let mut b = zap::Builder::new(zap::HEADER_SIZE + 64 + plain.len());
            let ob = b.start_object(16);
            b.set_u64(&ob, 0, 0);
            b.set_bytes(&ob, 8, &plain);
            b.finish_as_root(&ob);
            let mut out = vec![
                crate::components::TYPE_RESERVED,
                crate::components::SHAPE_LOCKED_OUTPUT,
            ];
            out.extend_from_slice(&b.finish());
            out
        };
        let back = Output::parse_wire(&locked_at_zero, [9; 32]).expect("it reads");
        assert_eq!(back.stake_lock, 0);
        assert_eq!(back, output(50, 0));
    }
}
