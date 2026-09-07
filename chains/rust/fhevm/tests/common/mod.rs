// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What every F-Chain test needs: an identity that can sign, a committee, a
//! chain, and one builder per operation.
//!
//! The private keys live ONLY here, which is the point: the chain under test
//! parses a public key and verifies a signature, and never holds either half of
//! an identity.

#![allow(dead_code)]

use std::sync::Arc;

use fips204::ml_dsa_65;
use fips204::traits::{SerDes, Signer};
use sha2::{Digest, Sha256};

use lux_fhevm::block::Block;
use lux_fhevm::clock::Time;
use lux_fhevm::db::{Kv, Mem};
use lux_fhevm::fee::Account;
use lux_fhevm::fhe;
use lux_fhevm::ids::{self, Id};
use lux_fhevm::state::{
    address_of, committee_digest, derive_handle, derive_permit_id, derive_request_id,
};
use lux_fhevm::transaction::{
    AdvancePayload, FulfillPayload, GrantPayload, RegisterPayload, RequestPayload, RevokePayload,
    Transaction, TX_ADVANCE_EPOCH, TX_FULFILL_DECRYPT, TX_GRANT_PERMIT, TX_REGISTER_CIPHERTEXT,
    TX_REQUEST_DECRYPT, TX_REVOKE_PERMIT,
};
use lux_fhevm::vm::{Genesis, Init, Vm};

/// A gas ceiling above every scheduled operation, so a test that is not about
/// metering never trips the limit.
pub const TEST_GAS: u64 = 500_000;

/// Enough for many operations at the scheduled prices.
pub const TEST_FUND: u64 = 10_000_000_000;

/// The scheme most tests price against.
pub const TEST_SCHEME: &str = "ckks-n14";

/// THE chain id.
///
/// Production runs ONE F-Chain per network, and every validator of it — proposer
/// and follower alike — holds the same id: it is what a payer signs over and what
/// a block id commits to. So the harness holds it constant too. Giving each node
/// its own would make a proposer and a follower two different chains, which is
/// not a shape production ever has, and would quietly excuse the very binding
/// these tests exist to check. A test that is genuinely ABOUT two chains names
/// its second one itself.
pub fn test_chain_id() -> Id {
    let mut id = ids::EMPTY;
    id[..11].copy_from_slice(b"fchain-test");
    id
}

/// Genesis is fixed for the same reason: every validator of one chain reads one
/// genesis, so its id is one value. A wall clock here would give two nodes built
/// a second apart two different genesis blocks.
pub const TEST_GENESIS_TIME: i64 = 1_700_000_000;

/// An external identity: a payer, a grantee, or a committee member.
pub struct TestKey {
    secret: ml_dsa_65::PrivateKey,
    pub public: Vec<u8>,
    pub addr: Account,
}

impl TestKey {
    pub fn new() -> Self {
        let (pk, sk) = ml_dsa_65::try_keygen().expect("a key pair");
        let public = pk.into_bytes().to_vec();
        let addr = address_of(&public);
        TestKey { secret: sk, public, addr }
    }

    pub fn hex_addr(&self) -> String {
        hex::encode(self.addr)
    }

    /// Attaches the payer's public key and a valid signature over the signing
    /// bytes for THE chain under test.
    pub fn sign(&self, tx: &mut Transaction) {
        self.sign_for(&test_chain_id(), tx);
    }

    /// Signs for a named chain. Only a test that is about more than one chain
    /// needs it.
    pub fn sign_for(&self, chain: &Id, tx: &mut Transaction) {
        tx.auth = self.public.clone();
        let sig = self
            .secret
            .try_sign(&tx.signing_bytes(chain), &[])
            .expect("a signature");
        tx.sig = sig.to_vec();
    }
}

impl Default for TestKey {
    fn default() -> Self {
        TestKey::new()
    }
}

/// n committee members in canonical node-id order, each carrying a real public
/// key, alongside the keys that can sign for them (index-aligned).
pub fn new_committee(n: usize) -> (Vec<fhe::CommitteeMember>, Vec<TestKey>) {
    let mut pairs: Vec<(fhe::CommitteeMember, TestKey)> = (0..n)
        .map(|_| {
            let k = TestKey::new();
            let mut node_id = [0u8; 20];
            // A node id is 20 bytes of nothing in particular; deriving it from the
            // key keeps it distinct without a second source of randomness.
            node_id.copy_from_slice(&Sha256::digest(&k.public)[8..28]);
            (
                fhe::CommitteeMember { node_id, public_key: k.public.clone(), weight: 1, index: 0 },
                k,
            )
        })
        .collect();
    pairs.sort_by_key(|(m, _)| m.node_id);
    let mut members = Vec::with_capacity(n);
    let mut keys = Vec::with_capacity(n);
    for (i, (mut m, k)) in pairs.into_iter().enumerate() {
        m.index = i as i64;
        members.push(m);
        keys.push(k);
    }
    (members, keys)
}

/// A genesis allocation funding every given key.
pub fn fund_all(keys: &[&TestKey]) -> Vec<(String, u64)> {
    keys.iter().map(|k| (k.hex_addr(), TEST_FUND)).collect()
}

/// An in-memory F-Chain seeded with the given allocation and epoch-0 committee.
pub fn new_test_vm(
    alloc: Vec<(String, u64)>,
    committee: &[fhe::CommitteeMember],
    threshold: i64,
) -> Vm {
    new_vm_on_chain(test_chain_id(), alloc, committee, threshold)
}

/// A chain with an explicitly named id. Only a test that is about more than one
/// chain needs it; every other one uses [`test_chain_id`], because production
/// runs one chain with one id.
pub fn new_vm_on_chain(
    chain: Id,
    alloc: Vec<(String, u64)>,
    committee: &[fhe::CommitteeMember],
    threshold: i64,
) -> Vm {
    let g = test_genesis(alloc, committee, threshold);
    Vm::initialize(Init {
        db: Mem::new(),
        chain_id: chain,
        network_id: 96369,
        genesis: g.to_json(),
        config: Vec::new(),
    })
    .expect("the chain starts")
}

pub fn test_genesis(
    alloc: Vec<(String, u64)>,
    committee: &[fhe::CommitteeMember],
    threshold: i64,
) -> Genesis {
    Genesis {
        version: 1,
        message: String::new(),
        timestamp: TEST_GENESIS_TIME,
        alloc,
        committee: if committee.is_empty() { None } else { Some(committee.to_vec()) },
        threshold,
        public_key: if committee.is_empty() {
            Vec::new()
        } else {
            b"network-fhe-public-key".to_vec()
        },
    }
}

/// A chain over a store the caller supplies, so a test can fail the disk.
pub fn boot_on(db: Arc<dyn Kv>, genesis: &[u8]) -> lux_fhevm::Result<Vm> {
    Vm::initialize(Init {
        db,
        chain_id: test_chain_id(),
        network_id: 96369,
        genesis: genesis.to_vec(),
        config: Vec::new(),
    })
}

// ---- builders --------------------------------------------------------------

pub fn digest_of(s: &str) -> [u8; 32] {
    Sha256::digest(s.as_bytes()).into()
}

/// A 32-byte identifier the way the RPC surface takes it.
pub fn hex_of(v: [u8; 32]) -> String {
    hex::encode(v)
}

pub fn register_tx(k: &TestKey, scheme: &str, digest: [u8; 32], nonce: u64) -> Transaction {
    let mut tx = Transaction {
        tx_type: TX_REGISTER_CIPHERTEXT,
        scheme: scheme.as_bytes().to_vec(),
        payer: k.addr,
        subject: derive_handle(&digest, scheme.as_bytes()),
        gas_limit: TEST_GAS,
        nonce,
        payload: RegisterPayload { digest, kind: 4, level: 3, size: 4096 }.to_json(),
        auth: Vec::new(),
        sig: Vec::new(),
    };
    k.sign(&mut tx);
    tx
}

pub fn grant_tx(
    owner: &TestKey,
    handle: [u8; 32],
    grantee: Account,
    ops: u32,
    expiry: i64,
    nonce: u64,
) -> Transaction {
    let mut tx = Transaction {
        tx_type: TX_GRANT_PERMIT,
        payer: owner.addr,
        subject: handle,
        gas_limit: TEST_GAS,
        nonce,
        payload: GrantPayload { grantee, operations: ops, expiry }.to_json(),
        ..Transaction::default()
    };
    owner.sign(&mut tx);
    tx
}

pub fn revoke_tx(k: &TestKey, permit_id: [u8; 32], nonce: u64) -> Transaction {
    let mut tx = Transaction {
        tx_type: TX_REVOKE_PERMIT,
        payer: k.addr,
        subject: permit_id,
        gas_limit: TEST_GAS,
        nonce,
        payload: RevokePayload { reason: "no longer sanctioned".into() }.to_json(),
        ..Transaction::default()
    };
    k.sign(&mut tx);
    tx
}

pub fn request_tx(
    k: &TestKey,
    scheme: &str,
    handle: [u8; 32],
    permit_id: [u8; 32],
    expiry: i64,
    nonce: u64,
) -> Transaction {
    let mut tx = Transaction {
        tx_type: TX_REQUEST_DECRYPT,
        scheme: scheme.as_bytes().to_vec(),
        payer: k.addr,
        subject: handle,
        gas_limit: TEST_GAS,
        nonce,
        payload: RequestPayload {
            permit_id,
            callback: {
                let mut c = [0u8; 20];
                c[0] = 0xca;
                c[1] = 0x11;
                c
            },
            selector: [1, 2, 3, 4],
            expiry,
        }
        .to_json(),
        ..Transaction::default()
    };
    k.sign(&mut tx);
    tx
}

pub fn fulfill_tx(k: &TestKey, request_id: [u8; 32], result: [u8; 32], nonce: u64) -> Transaction {
    let mut tx = Transaction {
        tx_type: TX_FULFILL_DECRYPT,
        payer: k.addr,
        subject: request_id,
        gas_limit: TEST_GAS,
        nonce,
        payload: FulfillPayload { result }.to_json(),
        ..Transaction::default()
    };
    k.sign(&mut tx);
    tx
}

pub fn advance_tx(
    k: &TestKey,
    epoch: u64,
    committee: &[fhe::CommitteeMember],
    threshold: i64,
    pk: &[u8],
    nonce: u64,
) -> Transaction {
    let mut tx = Transaction {
        tx_type: TX_ADVANCE_EPOCH,
        payer: k.addr,
        subject: committee_digest(epoch, threshold, pk, committee),
        gas_limit: TEST_GAS,
        nonce,
        payload: AdvancePayload {
            epoch,
            committee: Some(committee.to_vec()),
            threshold,
            public_key: pk.to_vec(),
        }
        .to_json(),
        ..Transaction::default()
    };
    k.sign(&mut tx);
    tx
}

// ---- driving the chain -----------------------------------------------------

/// Submits, builds, verifies and accepts a single-transaction block.
pub fn accept_one(vm: &Vm, tx: &Transaction) {
    vm.submit_tx(tx).expect("the transaction is admitted");
    accept_queued(vm);
}

/// Builds, verifies and accepts whatever is queued.
pub fn accept_queued(vm: &Vm) -> Block {
    let blk = vm.build_block().expect("a block is built");
    blk.verify(vm).expect("the block verifies");
    blk.accept(vm).expect("the block is accepted");
    blk
}

/// Builds a block directly from the given transactions, bypassing the queue — the
/// way a peer's proposal arrives.
pub fn force_block(vm: &Vm, txs: Vec<Transaction>) -> Block {
    let (parent_id, height) = {
        let st = vm.state.read().unwrap();
        (st.last_accepted, st.height)
    };
    let mut blk = Block {
        id: ids::EMPTY,
        parent_id,
        height: height + 1,
        timestamp: vm.clock.time(),
        transactions: txs,
    };
    blk.id = blk.compute_id(&vm.chain_id);
    blk
}

/// Registers a ciphertext owned by `owner` and grants `grantee` a permit over it,
/// returning the handle and the permit id. It is the starting state every
/// decryption test needs. It spends the owner's nonces 1 and 2, so the owner's
/// next transaction carries nonce 3.
pub fn seed_permit(
    vm: &Vm,
    owner: &TestKey,
    grantee: &TestKey,
    ops: u32,
    expiry: i64,
) -> ([u8; 32], [u8; 32]) {
    let reg = register_tx(owner, TEST_SCHEME, digest_of(&format!("seed-{}", owner.hex_addr())), 1);
    accept_one(vm, &reg);
    let handle = reg.subject;

    let grant = grant_tx(owner, handle, grantee.addr, ops, expiry, 2);
    accept_one(vm, &grant);
    let permit_id = derive_permit_id(&handle, &owner.addr, &grantee.addr, ops, expiry, 2);
    assert!(vm.permit(&permit_id).is_some(), "the grant must create the permit its inputs derive");
    (handle, permit_id)
}

/// Seats an n-member committee at threshold t and funds the members plus the
/// given users.
pub fn new_decrypt_vm(
    n: usize,
    threshold: i64,
    users: &[&TestKey],
) -> (Vm, Vec<fhe::CommitteeMember>, Vec<TestKey>) {
    let (committee, keys) = new_committee(n);
    let mut all: Vec<&TestKey> = keys.iter().collect();
    all.extend_from_slice(users);
    let vm = new_test_vm(fund_all(&all), &committee, threshold);
    (vm, committee, keys)
}

/// A fixed block timestamp, so a test that serializes one gets the same bytes on
/// every run.
pub fn time_at(secs: i64) -> Time {
    Time::from_unix(secs)
}

/// Every key the chain has written, in a fixed order, so two databases can be
/// compared exactly. The prefixes are walked in a fixed sequence and each walk
/// returns its keys sorted, so the result depends on the DATA and nothing else.
pub fn dump(vm: &Vm) -> (String, String) {
    use lux_fhevm::vm::{
        BLOCK_PREFIX, CIPHERTEXT_PREFIX, DECRYPT_PREFIX, EPOCH_PREFIX, HEIGHT_PREFIX, NONCE_PREFIX,
        PERMIT_PREFIX,
    };
    let st = vm.state.read().unwrap();
    let mut h = Sha256::new();
    let mut listing = String::new();
    for prefix in [
        CIPHERTEXT_PREFIX,
        PERMIT_PREFIX,
        DECRYPT_PREFIX,
        EPOCH_PREFIX,
        BLOCK_PREFIX,
        NONCE_PREFIX,
        HEIGHT_PREFIX,
        b"fee/",
    ] {
        for (k, v) in st.db.scan(prefix).expect("the store answers") {
            listing.push_str(&hex::encode(&k));
            listing.push('=');
            listing.push_str(&hex::encode(&v));
            listing.push('\n');
            h.update(&k);
            h.update(&v);
        }
    }
    (hex::encode(h.finalize()), listing)
}

/// The sample transaction the wire vectors are taken over: every field non-empty,
/// so a canonical round-trip exercises every offset.
pub fn sample_tx() -> Transaction {
    let mut payer: Account = [0u8; 20];
    payer.copy_from_slice(b"payer-address-20byte");
    Transaction {
        tx_type: TX_REGISTER_CIPHERTEXT,
        scheme: TEST_SCHEME.as_bytes().to_vec(),
        payer,
        subject: {
            let mut s = [0u8; 32];
            s[..9].copy_from_slice(&[9, 8, 7, 6, 5, 4, 3, 2, 1]);
            s
        },
        gas_limit: 81000,
        nonce: 42,
        payload: b"register-ciphertext-payload".to_vec(),
        auth: b"payer-public-key-bytes".to_vec(),
        sig: b"payer-signature-bytes".to_vec(),
    }
}

pub fn request_id_for(handle: [u8; 32], requester: &Account, nonce: u64) -> [u8; 32] {
    derive_request_id(&handle, requester, nonce)
}

// ---- a store that fails on purpose -----------------------------------------

/// The one thing a chain's database can do that no amount of correct logic
/// covers: not answer.
///
/// The failure that matters most is the quiet one — a read that FAILED reported
/// as a read that found NOTHING. The last-accepted pointer coming back empty made
/// a chain at height 2 look like a chain that had never run, and the node went on
/// to build height 1 over it, durably, with nothing in any log to say so. So this
/// fails one chosen operation and passes the rest through, and every test that
/// uses it carries its own control: the SAME read, absent rather than failing.
pub struct Faults {
    pub inner: Arc<dyn Kv>,
    /// Reads of keys under this prefix fail.
    pub read_fails: Option<Vec<u8>>,
    /// Writes of keys under this prefix fail.
    pub put_fails: Option<Vec<u8>>,
    /// The commit batch fails.
    pub write_fails: std::sync::atomic::AtomicBool,
    /// Walks over this prefix report an error instead of rows.
    pub scan_fails: Option<Vec<u8>>,
}

/// What a disk that did not answer says.
pub const DISK: &str = "test: the disk did not answer";

pub fn disk_error() -> lux_fhevm::db::Error {
    lux_fhevm::db::Error::Other(DISK.into())
}

/// True when the error is the deliberate disk failure and not something else.
pub fn is_disk(e: &lux_fhevm::Error) -> bool {
    matches!(e, lux_fhevm::Error::Db(lux_fhevm::db::Error::Other(why)) if why == DISK)
        || matches!(e, lux_fhevm::Error::Fee(lux_fhevm::fee::Error::Db(lux_fhevm::db::Error::Other(why))) if why == DISK)
}

impl Faults {
    pub fn over(inner: Arc<dyn Kv>) -> Self {
        Faults {
            inner,
            read_fails: None,
            put_fails: None,
            write_fails: std::sync::atomic::AtomicBool::new(false),
            scan_fails: None,
        }
    }

    pub fn reading(mut self, prefix: &[u8]) -> Self {
        self.read_fails = Some(prefix.to_vec());
        self
    }

    pub fn writing(mut self, prefix: &[u8]) -> Self {
        self.put_fails = Some(prefix.to_vec());
        self
    }

    pub fn scanning(mut self, prefix: &[u8]) -> Self {
        self.scan_fails = Some(prefix.to_vec());
        self
    }

    pub fn committing(self) -> Self {
        self.write_fails.store(true, std::sync::atomic::Ordering::SeqCst);
        self
    }

    fn hit(key: &[u8], prefix: &Option<Vec<u8>>) -> bool {
        matches!(prefix, Some(p) if key.starts_with(p))
    }
}

impl Kv for Faults {
    fn has(&self, key: &[u8]) -> Result<bool, lux_fhevm::db::Error> {
        if Self::hit(key, &self.read_fails) {
            return Err(disk_error());
        }
        self.inner.has(key)
    }

    fn get(&self, key: &[u8]) -> Result<Vec<u8>, lux_fhevm::db::Error> {
        if Self::hit(key, &self.read_fails) {
            return Err(disk_error());
        }
        self.inner.get(key)
    }

    fn put(&self, key: &[u8], value: &[u8]) -> Result<(), lux_fhevm::db::Error> {
        if Self::hit(key, &self.put_fails) {
            return Err(disk_error());
        }
        self.inner.put(key, value)
    }

    fn delete(&self, key: &[u8]) -> Result<(), lux_fhevm::db::Error> {
        self.inner.delete(key)
    }

    fn scan(&self, prefix: &[u8]) -> Result<Vec<(Vec<u8>, Vec<u8>)>, lux_fhevm::db::Error> {
        if let Some(p) = &self.scan_fails {
            if prefix.starts_with(p.as_slice()) {
                // A corrupt index presents as an error, not as an empty one.
                return Err(disk_error());
            }
        }
        self.inner.scan(prefix)
    }

    fn write_batch(&self, batch: &[lux_fhevm::db::Write]) -> Result<(), lux_fhevm::db::Error> {
        if self.write_fails.load(std::sync::atomic::Ordering::SeqCst) {
            return Err(disk_error());
        }
        self.inner.write_batch(batch)
    }
}

/// Puts a failing store in front of the chain's own, and hands back what was
/// there so a test can put it back.
pub fn fail_with(vm: &Vm, faults: Faults) -> Arc<dyn Kv> {
    let mut st = vm.state.write().unwrap();
    let sound = st.db.clone();
    st.db = Arc::new(faults);
    sound
}

pub fn restore(vm: &Vm, sound: Arc<dyn Kv>) {
    vm.state.write().unwrap().db = sound;
}

/// The version layer the chain commits through, for a test that needs to close it
/// or fail its commit.
pub fn version_layer(vm: &Vm) -> Arc<lux_fhevm::db::Version> {
    vm.state.read().unwrap().versdb.clone()
}
