// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! How a transaction reaches the rest of the network, and what a peer's
//! transaction has to satisfy to be kept.
//!
//! Ported from `vms/xvm/network` — `gossip.go`, `config.go` and the parts of
//! `network.go` that decide what is admitted rather than which socket it came
//! from. The socket is the node's business: this crate holds the SET, and the
//! node's p2p layer asks it three questions — do you have this, here is one,
//! and what do you already know.
//!
//! ## The three refusals, before verification
//!
//! A transaction pushed by a peer is checked against what this node already
//! decided before it is executed:
//!
//! 1. already held — nothing to do, and re-gossiping it would be a loop;
//! 2. already refused, for a reason this node still remembers — the same
//!    answer, without paying for it again;
//! 3. it does not verify against the preferred state — refused, and the reason
//!    is remembered so the next peer to offer it is answered from memory.
//!
//! Only then does it reach the mempool, where [`crate::mempool`]'s own four
//! structural checks and the security gate run. That ordering is the whole
//! anti-flood argument: the expensive step is last and is reached only by a
//! transaction nothing cheaper could refuse.
//!
//! ## What the filter is for
//!
//! Pull gossip asks a peer for what this node is missing. Sending the ids of
//! everything held would cost more than the transactions do, so a node sends a
//! Bloom filter of what it has and the peer answers with what falls outside
//! it. False positives cost a transaction this node has to ask for again; false
//! negatives would cost correctness and cannot happen.
//!
//! [`Filter`] is Go's `p2p/gossip/bloom.go`, function for function: the hash is
//! the leading eight bytes of `sha256(id ‖ salt)` read big-endian, each of the
//! `k` probes rotates it left by seventeen and exclusive-ors a seed, and the
//! marshalled form is one byte of `k`, then the seeds big-endian, then the bit
//! array. The seeds travel with the filter, so a peer reconstructs it without
//! agreeing on anything first — and the salt does not, which is what stops a
//! stranger from computing ids that all land in one node's bits.
//!
//! THE SALT COMES FROM THE CALLER. Go reads `crypto/rand`. A chain crate has no
//! business choosing an entropy source for the node that runs it — and a test
//! that could not fix the salt could not check this against Go at all — so
//! [`Entropy`] is supplied, once, at construction.

use std::sync::Arc;

use crate::error::{Error, Result};
use crate::hash::sha256;
use crate::ids::Id;
use crate::mempool::Mempool;
use crate::txs::Tx;

// ------------------------------------------------------------- the numbers --

/// Go's `DefaultConfig`, in `vms/xvm/network/config.go`. These are network
/// behaviour: two nodes that disagree about them gossip at different rates and
/// waste each other's bandwidth, so they are values here rather than choices.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Config {
    /// How old a validator set may be before it is refreshed, in seconds.
    pub max_validator_set_staleness: u64,
    /// Bytes attempted per push, and per answer to a pull.
    pub target_gossip_size: usize,
    /// Fraction of total stake reached in the first round of push gossip.
    pub push_percent_stake: f64,
    pub push_validators: usize,
    pub push_peers: usize,
    pub repush_validators: usize,
    pub repush_peers: usize,
    /// How many recently-discarded ids are remembered so they are not pushed.
    pub push_discarded_remembered: usize,
    /// The shortest interval at which one transaction is pushed again, in
    /// milliseconds.
    pub push_max_regossip_ms: u64,
    pub push_every_ms: u64,
    pub pull_poll_size: usize,
    pub pull_every_ms: u64,
    pub pull_throttle_window_ms: u64,
    pub pull_throttle_limit: usize,
    /// What the filter is sized for.
    pub filter_elements: usize,
    pub filter_false_positive: f64,
    /// The false-positive rate at which the filter is thrown away and rebuilt.
    pub filter_reset_false_positive: f64,
}

impl Default for Config {
    fn default() -> Config {
        Config {
            max_validator_set_staleness: 60,
            target_gossip_size: 20 * 1024,
            push_percent_stake: 0.9,
            push_validators: 100,
            push_peers: 0,
            repush_validators: 10,
            repush_peers: 0,
            push_discarded_remembered: 16384,
            push_max_regossip_ms: 30_000,
            push_every_ms: 500,
            pull_poll_size: 1,
            pull_every_ms: 1500,
            pull_throttle_window_ms: 10_000,
            pull_throttle_limit: 2,
            filter_elements: 8 * 1024,
            filter_false_positive: 0.01,
            filter_reset_false_positive: 0.05,
        }
    }
}

/// How much larger than the mempool the filter is sized when it is rebuilt.
const CHURN: usize = 3;

/// The fewest and most probes a filter may use. Go's `minHashes`/`maxHashes`.
const MIN_PROBES: usize = 1;
const MAX_PROBES: usize = 16;
/// The fewest bytes a filter may be.
const MIN_BYTES: usize = 1;
/// How far the hash is rotated between probes.
const ROTATION: u32 = 17;

/// A malformed filter, in the crate's one refusal type.
fn bad(why: &str) -> Error {
    Error::Storage(why.to_string())
}

// ------------------------------------------------------------- the filter --

/// Where a fresh salt and fresh seeds come from.
///
/// The node has an entropy source; a chain does not get to pick one for it.
pub trait Entropy: Send + Sync {
    fn fill(&self, buf: &mut [u8]);
}

/// A fixed source, for a test that has to say what the salt is — and for the
/// vector that checks this against the bytes Go produces.
pub struct Fixed(pub Vec<u8>);

impl Entropy for Fixed {
    fn fill(&self, buf: &mut [u8]) {
        for (i, b) in buf.iter_mut().enumerate() {
            *b = *self.0.get(i % self.0.len().max(1)).unwrap_or(&0);
        }
    }
}

/// What this node has, probabilistically.
pub struct Filter {
    salt: Id,
    seeds: Vec<u64>,
    bits: Vec<u8>,
    /// How many have been added since the last rebuild.
    count: usize,
    /// How many may be added before the false-positive rate passes the reset
    /// bound.
    max_count: usize,
    min_elements: usize,
    false_positive: f64,
    reset_false_positive: f64,
}

impl Filter {
    /// A filter sized for `min_elements` at `false_positive`, salted and seeded
    /// from `entropy`.
    pub fn new(
        min_elements: usize,
        false_positive: f64,
        reset_false_positive: f64,
        entropy: &dyn Entropy,
    ) -> Result<Filter> {
        let mut f = Filter {
            salt: [0u8; 32],
            seeds: Vec::new(),
            bits: Vec::new(),
            count: 0,
            max_count: 0,
            min_elements,
            false_positive,
            reset_false_positive,
        };
        f.rebuild(min_elements, entropy)?;
        Ok(f)
    }

    /// A filter with the salt and seeds already chosen — how a Go-produced
    /// filter is reconstructed here, and the only way a vector can compare.
    pub fn with(salt: Id, seeds: Vec<u64>, bytes: usize) -> Result<Filter> {
        if seeds.len() < MIN_PROBES {
            return Err(bad("bloom: too few hashes"));
        }
        if seeds.len() > MAX_PROBES {
            return Err(bad("bloom: too many hashes"));
        }
        if bytes < MIN_BYTES {
            return Err(bad("bloom: too few entries"));
        }
        Ok(Filter {
            salt,
            seeds,
            bits: vec![0u8; bytes],
            count: 0,
            max_count: usize::MAX,
            min_elements: 0,
            false_positive: 0.0,
            reset_false_positive: 0.0,
        })
    }

    fn rebuild(&mut self, elements: usize, entropy: &dyn Entropy) -> Result<()> {
        let (probes, bytes) = optimal(elements, self.false_positive);
        if probes < MIN_PROBES {
            return Err(bad("bloom: too few hashes"));
        }
        if probes > MAX_PROBES {
            return Err(bad("bloom: too many hashes"));
        }
        if bytes < MIN_BYTES {
            return Err(bad("bloom: too few entries"));
        }
        let mut raw = vec![0u8; 32 + probes * 8];
        entropy.fill(&mut raw);
        let mut salt = [0u8; 32];
        salt.copy_from_slice(&raw[..32]);
        let seeds = (0..probes)
            .map(|i| {
                let at = 32 + i * 8;
                u64::from_be_bytes(raw[at..at + 8].try_into().expect("eight bytes"))
            })
            .collect();
        self.salt = salt;
        self.seeds = seeds;
        self.bits = vec![0u8; bytes];
        self.count = 0;
        self.max_count = estimate_count(probes, bytes, self.reset_false_positive);
        Ok(())
    }

    /// The leading eight bytes of `sha256(id ‖ salt)`, big-endian.
    fn seed_hash(&self, id: &Id) -> u64 {
        let mut buf = Vec::with_capacity(64);
        buf.extend_from_slice(id);
        buf.extend_from_slice(&self.salt);
        let h = sha256(&buf);
        u64::from_be_bytes(h[..8].try_into().expect("eight bytes"))
    }

    pub fn add(&mut self, id: &Id) {
        let num_bits = (self.bits.len() * 8) as u64;
        let mut h = self.seed_hash(id);
        for seed in &self.seeds {
            h = h.rotate_left(ROTATION) ^ seed;
            let index = h % num_bits;
            self.bits[(index / 8) as usize] |= 1 << (index % 8);
        }
        self.count += 1;
    }

    /// Whether this id MAY have been added. Never a false negative.
    pub fn has(&self, id: &Id) -> bool {
        let num_bits = (self.bits.len() * 8) as u64;
        let mut h = self.seed_hash(id);
        let mut hit: u8 = 1;
        for seed in &self.seeds {
            if hit == 0 {
                break;
            }
            h = h.rotate_left(ROTATION) ^ seed;
            let index = h % num_bits;
            hit &= self.bits[(index / 8) as usize] >> (index % 8);
        }
        hit != 0
    }

    /// The filter as it crosses the wire, and the salt beside it: one byte of
    /// probe count, the seeds big-endian, then the bits.
    pub fn marshal(&self) -> (Vec<u8>, Id) {
        let mut out = Vec::with_capacity(1 + self.seeds.len() * 8 + self.bits.len());
        out.push(self.seeds.len() as u8);
        for seed in &self.seeds {
            out.extend_from_slice(&seed.to_be_bytes());
        }
        out.extend_from_slice(&self.bits);
        (out, self.salt)
    }

    /// A peer's filter, from the bytes it sent and the salt it sent beside
    /// them. The inverse of [`Filter::marshal`], and Go's `bloom.Parse`.
    ///
    /// The bounds are checked here rather than trusted, because these bytes
    /// come from a stranger: a probe count of zero would read no bits and
    /// answer "yes" to everything, and one of two hundred would make this node
    /// hash two hundred times per lookup on request. A filter this node cannot
    /// read is a refusal, not an empty filter — an empty one answers "no" to
    /// everything, which would tell this node the peer holds nothing and send
    /// it the whole pool.
    ///
    /// What comes back is read-only in the sense that matters: it answers
    /// [`Filter::has`], and adding to it would be describing a peer's set with
    /// this node's transactions.
    pub fn parse(raw: &[u8], salt: Id) -> Result<Filter> {
        let probes = *raw.first().ok_or_else(|| bad("bloom: empty filter"))? as usize;
        if probes < MIN_PROBES {
            return Err(bad("bloom: too few hashes"));
        }
        if probes > MAX_PROBES {
            return Err(bad("bloom: too many hashes"));
        }
        let at = 1 + probes * 8;
        if raw.len() < at + MIN_BYTES {
            return Err(bad("bloom: too few entries"));
        }
        let seeds = (0..probes)
            .map(|i| {
                let s = 1 + i * 8;
                u64::from_be_bytes(raw[s..s + 8].try_into().expect("eight bytes"))
            })
            .collect();
        Ok(Filter {
            salt,
            seeds,
            bits: raw[at..].to_vec(),
            count: 0,
            max_count: usize::MAX,
            min_elements: 0,
            false_positive: 0.0,
            reset_false_positive: 0.0,
        })
    }

    pub fn salt(&self) -> Id {
        self.salt
    }

    pub fn count(&self) -> usize {
        self.count
    }

    /// Throw the filter away and rebuild it if it has taken more than it was
    /// sized for. `elements` lets it grow with the pool it describes.
    pub fn reset_if_needed(&mut self, elements: usize, entropy: &dyn Entropy) -> Result<bool> {
        if self.count <= self.max_count {
            return Ok(false);
        }
        let elements = elements.max(self.min_elements);
        self.rebuild(elements, entropy)?;
        Ok(true)
    }
}

/// How many probes and how many bytes a filter of `count` elements wants at
/// `false_positive`. Go's `optimalParameters`.
pub fn optimal(count: usize, false_positive: f64) -> (usize, usize) {
    let bytes = optimal_bytes(count, false_positive);
    (optimal_probes(bytes, count), bytes)
}

fn optimal_probes(bytes: usize, count: usize) -> usize {
    if bytes < MIN_BYTES {
        return MIN_PROBES;
    }
    if count == 0 {
        return MAX_PROBES;
    }
    let probes = (bytes as f64 * 8.0 * std::f64::consts::LN_2 / count as f64).ceil();
    if probes >= MAX_PROBES as f64 {
        return MAX_PROBES;
    }
    (probes as usize).max(MIN_PROBES)
}

fn optimal_bytes(count: usize, false_positive: f64) -> usize {
    if count == 0 {
        return MIN_BYTES;
    }
    if false_positive >= 1.0 {
        return MIN_BYTES;
    }
    if false_positive <= 0.0 {
        return usize::MAX;
    }
    let ln2_squared = std::f64::consts::LN_2 * std::f64::consts::LN_2;
    let in_bits = -(count as f64) * false_positive.ln() / ln2_squared;
    let bytes = (in_bits + 8.0 - 1.0) / 8.0;
    if bytes >= usize::MAX as f64 {
        return usize::MAX;
    }
    (bytes as usize).max(MIN_BYTES)
}

/// How many may be added before the false-positive rate passes `bound`.
pub fn estimate_count(probes: usize, bytes: usize, bound: f64) -> usize {
    if probes < MIN_PROBES || bytes < MIN_BYTES || bound <= 0.0 {
        return 0;
    }
    if bound >= 1.0 {
        return usize::MAX;
    }
    let inv = 1.0 / probes as f64;
    let num_bits = (bytes * 8) as f64;
    let exp = 1.0 - bound.powf(inv);
    let count = (-exp.ln() * num_bits * inv).ceil();
    if count >= usize::MAX as f64 {
        return usize::MAX;
    }
    count as usize
}

// -------------------------------------------------------------- the set --

/// The mempool as the gossip layer sees it: a set with a filter over it.
pub struct Gossip {
    pool: Mempool,
    filter: Filter,
    entropy: Arc<dyn Entropy>,
    /// Transactions accepted here and not yet handed to the network.
    outbound: Vec<Id>,
}

impl Gossip {
    pub fn new(pool: Mempool, config: &Config, entropy: Arc<dyn Entropy>) -> Result<Gossip> {
        let filter = Filter::new(
            config.filter_elements,
            config.filter_false_positive,
            config.filter_reset_false_positive,
            entropy.as_ref(),
        )?;
        Ok(Gossip {
            pool,
            filter,
            entropy,
            outbound: Vec::new(),
        })
    }

    pub fn pool(&self) -> &Mempool {
        &self.pool
    }

    pub fn pool_mut(&mut self) -> &mut Mempool {
        &mut self.pool
    }

    pub fn filter(&self) -> &Filter {
        &self.filter
    }

    /// A transaction a peer pushed, one a pull answered with, or one a wallet
    /// handed this node directly. All three are the same offer.
    ///
    /// Returning `Ok(())` here is what tells the p2p layer to pass it on, so
    /// every refusal below is also a decision not to spend the network on it.
    ///
    /// `verify` is the chain's own verification against the state this node
    /// prefers, and it is a parameter rather than something this set holds. The
    /// chain owns the set; a set that held a way to call back into the chain
    /// would be the chain holding itself, and the answer to "is this
    /// transaction good" would have two places to live. It is passed in, and
    /// runs exactly where Go runs it — after the two cheap refusals above it
    /// and before the pool's own admission below.
    pub fn add(&mut self, tx: Tx, verify: impl FnOnce(&Tx) -> Result<()>) -> Result<()> {
        let id = tx.id();
        if self.pool.has(&id) {
            return Err(Error::DuplicateTx);
        }
        if let Some(why) = self.pool.drop_reason(&id) {
            // Judged already. The same answer, without paying for it again.
            return Err(why.clone());
        }
        if let Err(why) = verify(&tx) {
            self.pool.mark_dropped(id, why.clone());
            return Err(why);
        }
        self.add_unverified(tx)
    }

    /// A transaction this node already trusts — one it was handed directly, or
    /// one it is replaying. It still has to fit and not conflict.
    pub fn add_unverified(&mut self, tx: Tx) -> Result<()> {
        let id = tx.id();
        if let Err(why) = self.pool.add(tx) {
            self.pool.mark_dropped(id, why.clone());
            return Err(why);
        }
        self.filter.add(&id);
        self.outbound.push(id);
        if self
            .filter
            .reset_if_needed(self.pool.len() * CHURN, self.entropy.as_ref())?
        {
            // A rebuilt filter knows nothing. What is still held goes back in,
            // or this node would ask peers for what it already has.
            let held: Vec<Id> = self.pool.candidates().iter().map(|t| t.id()).collect();
            for held_id in held {
                self.filter.add(&held_id);
            }
        }
        Ok(())
    }

    /// Whether this node holds it — exactly, not probabilistically. What a
    /// peer's `Has` asks.
    pub fn has(&self, id: &Id) -> bool {
        self.pool.has(id)
    }

    /// What to tell a peer this node already knows.
    pub fn marshal_filter(&self) -> (Vec<u8>, Id) {
        self.filter.marshal()
    }

    /// Transactions to push, oldest first, up to `target` bytes.
    ///
    /// Bounded by bytes rather than by count because that is what the far end
    /// has to read: one large transaction and a hundred small ones cost the
    /// same link the same.
    pub fn take_outbound(&mut self, target: usize) -> Vec<Vec<u8>> {
        let mut out = Vec::new();
        let mut sent = 0usize;
        let mut kept = Vec::new();
        for id in std::mem::take(&mut self.outbound) {
            let Some(tx) = self.pool.get(&id) else {
                // Gone from the pool between being accepted and being pushed.
                continue;
            };
            if !out.is_empty() && sent + tx.size() > target {
                kept.push(id);
                continue;
            }
            sent += tx.size();
            out.push(tx.bytes().to_vec());
        }
        self.outbound = kept;
        out
    }

    /// How many are waiting to be pushed.
    pub fn outbound(&self) -> usize {
        self.outbound.len()
    }
}

/// A transaction as it crosses a gossip message: its own canonical bytes and
/// nothing around them. Go's `MarshalGossip`.
pub fn marshal(tx: &Tx) -> Vec<u8> {
    tx.bytes().to_vec()
}

/// And back. Byte-preserving, so the id a peer named is the id read here.
pub fn unmarshal(raw: &[u8]) -> Result<Tx> {
    Tx::parse(raw)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::fx::secp256k1::{TransferInput, TransferOutput};
    use crate::fx::{FxIn, Input, Owners, State};
    use crate::ids::{self, ShortId};
    use crate::txs::{BaseTx, Unsigned};
    use crate::utxo::{Asset, BaseTxFields, TransferableInput, TransferableOutput, UtxoId};

    fn a_tx(src: u8, idx: u32, memo: &[u8]) -> Tx {
        Tx::new(Unsigned::Base(BaseTx {
            base: BaseTxFields {
                network_id: 10,
                blockchain_id: ids::prefixed(&[5]),
                outs: vec![TransferableOutput {
                    asset: Asset {
                        id: ids::prefixed(&[1]),
                    },
                    out: State::Transfer(TransferOutput {
                        amt: 1,
                        owners: Owners::new(1, vec![ShortId::prefixed_bytes(&[1])]),
                    }),
                }],
                ins: vec![TransferableInput {
                    utxo_id: UtxoId::new(ids::prefixed(&[src]), idx),
                    asset: Asset {
                        id: ids::prefixed(&[1]),
                    },
                    input: FxIn::Transfer(TransferInput {
                        amt: 1,
                        input: Input {
                            sig_indices: vec![0],
                        },
                    }),
                }],
                memo: memo.to_vec(),
            },
        }))
    }

    /// A chain that finds every transaction good.
    fn anything(_: &Tx) -> Result<()> {
        Ok(())
    }

    /// A chain that finds none good.
    fn nothing(_: &Tx) -> Result<()> {
        Err(Error::WrongSig)
    }

    fn entropy() -> Arc<dyn Entropy> {
        Arc::new(Fixed(vec![0xA5, 0x5A, 0x3C, 0xC3, 0x11, 0x22, 0x33, 0x44]))
    }

    fn a_set() -> Gossip {
        Gossip::new(Mempool::new(), &Config::default(), entropy()).unwrap()
    }

    // ---- the filter ----

    #[test]
    fn what_was_added_is_always_found() {
        let mut f = Filter::new(1024, 0.01, 0.05, entropy().as_ref()).unwrap();
        let held: Vec<Id> = (0u8..64).map(|n| ids::prefixed(&[n, 7])).collect();
        for id in &held {
            f.add(id);
        }
        for id in &held {
            assert!(f.has(id), "a bloom filter has no false negatives");
        }
        assert_eq!(f.count(), 64);
    }

    #[test]
    fn what_was_not_added_is_almost_never_found() {
        let mut f = Filter::new(1024, 0.01, 0.05, entropy().as_ref()).unwrap();
        for n in 0u8..64 {
            f.add(&ids::prefixed(&[n, 7]));
        }
        let wrong = (0u16..1000)
            .filter(|n| f.has(&ids::prefixed(&[(n >> 8) as u8, *n as u8, 9])))
            .count();
        assert!(wrong < 50, "one percent of a thousand, with room: {wrong}");
    }

    #[test]
    fn the_marshalled_filter_is_the_probe_count_then_the_seeds_then_the_bits() {
        let f = Filter::with([3u8; 32], vec![0x0102030405060708, 0x1112131415161718], 4).unwrap();
        let (raw, salt) = f.marshal();
        assert_eq!(salt, [3u8; 32]);
        assert_eq!(raw[0], 2, "two probes");
        assert_eq!(&raw[1..9], &0x0102030405060708u64.to_be_bytes());
        assert_eq!(&raw[9..17], &0x1112131415161718u64.to_be_bytes());
        assert_eq!(&raw[17..], &[0u8; 4], "and four empty bytes of filter");
    }

    #[test]
    fn a_peers_filter_reads_back_as_the_filter_that_was_sent() {
        let mut mine = Filter::new(64, 0.01, 0.05, entropy().as_ref()).unwrap();
        let held: Vec<Id> = (0u8..32).map(|n| ids::prefixed(&[n, 9])).collect();
        for id in &held {
            mine.add(id);
        }
        let (raw, salt) = mine.marshal();
        let theirs = Filter::parse(&raw, salt).expect("a peer reads what was sent");
        for id in &held {
            assert!(theirs.has(id), "no false negatives across the wire either");
        }
        // And it marshals back to the same bytes: nothing was lost in reading.
        let (again, again_salt) = theirs.marshal();
        assert_eq!(again, raw);
        assert_eq!(again_salt, salt);
    }

    #[test]
    fn a_filter_a_stranger_sent_is_refused_rather_than_read_as_empty() {
        // A filter this node cannot read must be a refusal. Reading it as an
        // empty filter would say the peer holds nothing, and this node would
        // answer by sending it everything.
        assert!(Filter::parse(&[], [0u8; 32]).is_err(), "no probe count");
        assert!(Filter::parse(&[0, 1], [0u8; 32]).is_err(), "zero probes");
        let mut too_many = vec![17u8];
        too_many.extend_from_slice(&[0u8; 17 * 8 + 1]);
        assert!(Filter::parse(&too_many, [0u8; 32]).is_err(), "17 probes");
        // A probe count the body cannot back: the seeds run off the end.
        assert!(Filter::parse(&[2, 0, 0, 0], [0u8; 32]).is_err());
        // Seeds exactly, and not one byte of filter to look in.
        assert!(Filter::parse(&[1, 0, 0, 0, 0, 0, 0, 0, 0], [0u8; 32]).is_err());
        // One byte of filter is enough to be a filter.
        assert!(Filter::parse(&[1, 0, 0, 0, 0, 0, 0, 0, 0, 0], [0u8; 32]).is_ok());
    }

    #[test]
    fn a_filter_must_have_between_one_and_sixteen_probes_and_a_byte_to_write_in() {
        assert!(Filter::with([0u8; 32], vec![], 4).is_err());
        assert!(Filter::with([0u8; 32], vec![1; 17], 4).is_err());
        assert!(Filter::with([0u8; 32], vec![1], 0).is_err());
        assert!(Filter::with([0u8; 32], vec![1], 1).is_ok());
    }

    #[test]
    fn the_sizing_is_the_arithmetic_go_does() {
        // 8192 elements at one percent: the textbook m = -n ln p / (ln 2)^2,
        // rounded up to whole bytes, and k = m ln 2 / n. The two numbers are
        // what `optimalEntries`/`optimalHashes` in `p2p/gossip/bloom.go` answer
        // for the same inputs — run, not remembered, because the rounding here
        // (a truncating cast over a ceil-in-bits) is exactly where an
        // independently written formula drifts by one byte.
        let (probes, bytes) = optimal(8 * 1024, 0.01);
        assert_eq!(bytes, 9815);
        assert_eq!(probes, 7);
        // Degenerate inputs answer with the bounds rather than dividing by zero.
        assert_eq!(optimal(0, 0.01), (MAX_PROBES, MIN_BYTES));
        assert_eq!(optimal(10, 1.0), (MIN_PROBES, MIN_BYTES));
        assert_eq!(estimate_count(0, 10, 0.05), 0);
        assert_eq!(estimate_count(3, 0, 0.05), 0);
        assert_eq!(estimate_count(3, 10, 0.0), 0);
    }

    #[test]
    fn a_full_filter_is_thrown_away_and_what_is_held_goes_back_in() {
        // Sized for one element, so it fills at once.
        let mut set = Gossip::new(
            Mempool::new(),
            &Config {
                filter_elements: 1,
                ..Config::default()
            },
            entropy(),
        )
        .unwrap();
        let mut ids_seen = Vec::new();
        for n in 1u8..40 {
            let tx = a_tx(n, 0, &[n]);
            ids_seen.push(tx.id());
            set.add(tx, anything).unwrap();
        }
        // However often it was rebuilt, every held transaction is still in it.
        for id in &ids_seen {
            assert!(set.filter().has(id), "a rebuilt filter is refilled");
        }
    }

    // ---- the set ----

    #[test]
    fn a_transaction_a_peer_pushed_is_verified_before_it_is_held() {
        let mut set = a_set();
        let tx = a_tx(1, 0, b"a");
        assert_eq!(set.add(tx.clone(), nothing).unwrap_err(), Error::WrongSig);
        assert!(!set.has(&tx.id()));
        // And the reason is remembered, so the next peer costs nothing.
        assert_eq!(set.pool().drop_reason(&tx.id()), Some(&Error::WrongSig));
    }

    #[test]
    fn a_transaction_already_judged_is_answered_from_memory() {
        let asked = std::cell::Cell::new(0usize);
        let count = |_: &Tx| {
            asked.set(asked.get() + 1);
            Err(Error::WrongSig)
        };
        let mut set = a_set();
        let tx = a_tx(1, 0, b"a");
        assert!(set.add(tx.clone(), count).is_err());
        assert!(set.add(tx, count).is_err());
        assert_eq!(asked.get(), 1, "the second offer never reached the chain");
    }

    #[test]
    fn a_transaction_already_held_is_a_duplicate_and_not_re_verified() {
        let mut set = a_set();
        let tx = a_tx(1, 0, b"a");
        set.add(tx.clone(), anything).unwrap();
        assert_eq!(
            set.add(tx.clone(), anything).unwrap_err(),
            Error::DuplicateTx
        );
        assert!(set.has(&tx.id()));
        assert!(set.filter().has(&tx.id()));
    }

    #[test]
    fn a_transaction_the_pool_refuses_is_remembered_as_refused() {
        let mut set = a_set();
        set.add(a_tx(1, 0, b"first"), anything).unwrap();
        let conflicting = a_tx(1, 0, b"second");
        assert_eq!(
            set.add(conflicting.clone(), anything).unwrap_err(),
            Error::ConflictsWithOtherTx
        );
        assert_eq!(
            set.pool().drop_reason(&conflicting.id()),
            Some(&Error::ConflictsWithOtherTx)
        );
    }

    #[test]
    fn a_strict_chain_refuses_a_classical_transaction_at_the_gossip_door() {
        let mut pool = Mempool::new();
        pool.hold_to(crate::security::strict_pq(), None);
        let mut set = Gossip::new(pool, &Config::default(), entropy()).unwrap();
        let mut tx = a_tx(1, 0, b"a");
        tx.sign(crate::fx::Family::Secp256k1, &[vec![[7u8; 32]]])
            .unwrap();
        assert_eq!(
            set.add(tx.clone(), anything).unwrap_err(),
            Error::ClassicalCredentialRefused
        );
        assert!(!set.has(&tx.id()), "and it is not gossiped on");
    }

    #[test]
    fn what_is_accepted_is_queued_to_be_told_to_the_network() {
        let mut set = a_set();
        let a = a_tx(1, 0, b"a");
        let b = a_tx(2, 0, b"b");
        set.add(a.clone(), anything).unwrap();
        set.add(b.clone(), anything).unwrap();
        assert_eq!(set.outbound(), 2);
        let pushed = set.take_outbound(Config::default().target_gossip_size);
        assert_eq!(pushed, vec![a.bytes().to_vec(), b.bytes().to_vec()]);
        assert_eq!(set.outbound(), 0, "and only once");
    }

    #[test]
    fn a_push_is_bounded_by_bytes_and_the_rest_waits() {
        let mut set = a_set();
        let a = a_tx(1, 0, b"a");
        let b = a_tx(2, 0, b"b");
        set.add(a.clone(), anything).unwrap();
        set.add(b.clone(), anything).unwrap();
        // Room for the first only; the second is kept rather than dropped.
        let pushed = set.take_outbound(a.size());
        assert_eq!(pushed, vec![a.bytes().to_vec()]);
        assert_eq!(set.outbound(), 1);
        assert_eq!(
            set.take_outbound(usize::MAX),
            vec![b.bytes().to_vec()],
            "and it goes on the next round"
        );
    }

    #[test]
    fn one_transaction_larger_than_a_whole_push_still_goes() {
        // Otherwise it would wait forever, and a transaction nobody can gossip
        // is a transaction nobody can mine.
        let mut set = a_set();
        let a = a_tx(1, 0, b"a");
        set.add(a.clone(), anything).unwrap();
        assert_eq!(set.take_outbound(1), vec![a.bytes().to_vec()]);
    }

    #[test]
    fn a_transaction_crosses_a_gossip_message_as_its_own_bytes() {
        let tx = a_tx(1, 0, b"over the wire");
        let raw = marshal(&tx);
        assert_eq!(raw, tx.bytes());
        let back = unmarshal(&raw).unwrap();
        assert_eq!(back.id(), tx.id(), "and comes back with the same name");
        assert_eq!(back.bytes(), tx.bytes());
        assert!(unmarshal(b"not a transaction").is_err());
    }

    #[test]
    fn the_gossip_numbers_are_the_ones_go_defaults_to() {
        let c = Config::default();
        assert_eq!(c.max_validator_set_staleness, 60);
        assert_eq!(c.target_gossip_size, 20 * 1024);
        assert_eq!(c.push_percent_stake, 0.9);
        assert_eq!(c.push_validators, 100);
        assert_eq!(c.push_peers, 0);
        assert_eq!(c.repush_validators, 10);
        assert_eq!(c.repush_peers, 0);
        assert_eq!(c.push_discarded_remembered, 16384);
        assert_eq!(c.push_max_regossip_ms, 30_000);
        assert_eq!(c.push_every_ms, 500);
        assert_eq!(c.pull_poll_size, 1);
        assert_eq!(c.pull_every_ms, 1500);
        assert_eq!(c.pull_throttle_window_ms, 10_000);
        assert_eq!(c.pull_throttle_limit, 2);
        assert_eq!(c.filter_elements, 8 * 1024);
        assert_eq!(c.filter_false_positive, 0.01);
        assert_eq!(c.filter_reset_false_positive, 0.05);
        assert_eq!(CHURN, 3);
    }
}
