// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The chain's own life: what it reports, what it builds, what it refuses to
//! build, and what it keeps track of while a block is in flight.

mod common;

use common::*;

use lux_fhevm::block::{Block, STATUS_ACCEPTED, STATUS_PROCESSING};
use lux_fhevm::db::Mem;
use lux_fhevm::error::Error;
use lux_fhevm::fee;
use lux_fhevm::fhe;
use lux_fhevm::ids;
use lux_fhevm::transaction::{RevokePayload, Transaction, TX_REVOKE_PERMIT};
use lux_fhevm::vm::{Genesis, Init, Vm, VERSION};

fn detail(details: &[(String, String)], key: &str) -> String {
    details
        .iter()
        .find(|(k, _)| k == key)
        .map(|(_, v)| v.clone())
        .unwrap_or_else(|| panic!("no detail named {key}"))
}

#[test]
fn a_started_chain_reports_its_version_its_committee_and_its_epoch() {
    let (committee, _) = new_committee(3);
    let vm = new_test_vm(Vec::new(), &committee, 2);

    assert_eq!(vm.version(), VERSION);

    let (healthy, details) = vm.health_check().expect("health reads");
    assert!(healthy);
    assert_eq!(detail(&details, "committee"), "3");
    assert_eq!(detail(&details, "threshold"), "2");
    assert_eq!(detail(&details, "epoch"), "0");

    let ep = vm.epoch(0).expect("genesis must seat epoch 0");
    assert_eq!(ep.committee().len(), 3);
    assert_eq!(vm.current_epoch(), 0);
}

/// A chain with nobody seated reports itself degraded rather than healthy:
/// decryptions could be requested but never answered.
#[test]
fn a_chain_with_no_committee_reports_itself_degraded() {
    let vm = new_test_vm(Vec::new(), &[], 0);
    let (healthy, details) = vm.health_check().expect("health reads");
    assert!(!healthy);
    assert_eq!(detail(&details, "committee"), "0");
}

/// Genesis goes through the same committee check an epoch advance does, so a
/// chain cannot be born holding a committee consensus would refuse to install.
#[test]
fn genesis_refuses_a_committee_consensus_would_refuse() {
    let mut node_id = [0u8; 20];
    node_id[0] = 1;
    let bad = vec![fhe::CommitteeMember {
        node_id,
        public_key: b"not-an-mldsa-key".to_vec(),
        weight: 1,
        index: 0,
    }];
    let g = Genesis {
        version: 1,
        message: String::new(),
        timestamp: TEST_GENESIS_TIME,
        alloc: Vec::new(),
        committee: Some(bad),
        threshold: 1,
        public_key: b"pk".to_vec(),
    };
    let err = Vm::initialize(Init {
        db: Mem::new(),
        chain_id: test_chain_id(),
        network_id: 96369,
        genesis: g.to_json(),
        config: Vec::new(),
    })
    .err()
    .expect("a committee that cannot speak is refused");
    assert!(matches!(err, Error::InvalidCommittee(_)));
}

/// The height index answers from an entry written in the same commit as the
/// block, and refuses a height the chain never reached.
#[test]
fn the_height_index_names_the_block_at_each_height_and_nothing_beyond() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    assert_eq!(vm.block_id_at(0).unwrap(), vm.last_accepted(), "height 0 names genesis");

    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("a"), 1));
    let id1 = vm.block_id_at(1).unwrap();
    assert_eq!(id1, vm.last_accepted());

    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("b"), 2));
    let id2 = vm.block_id_at(2).unwrap();
    assert_eq!(id2, vm.last_accepted());
    assert_ne!(id1, id2);

    assert!(vm.block_id_at(99).is_err(), "a height the chain never reached is an error");
}

/// A proposal that never lands costs nothing. Building SELECTS from the queue
/// rather than draining it, so a block that is rejected — or that the engine
/// simply discards, which it may do without ever rejecting it — cannot take the
/// queue with it.
#[test]
fn a_discarded_proposal_leaves_the_queue_and_the_state_alone() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    let tx = register_tx(&k, TEST_SCHEME, digest_of("discarded"), 1);
    vm.submit_tx(&tx).expect("admitted");

    let blk = vm.build_block().expect("a block");
    assert_eq!(vm.mempool.lock().unwrap().txs.len(), 1, "building selects, it does not drain");
    assert_eq!(vm.mempool.lock().unwrap().claims.len(), 1);

    blk.reject(&vm).expect("rejected");
    assert_eq!(vm.mempool.lock().unwrap().txs.len(), 1, "rejection leaves the queue alone");

    assert!(vm.ciphertext(&tx.subject).is_none(), "a rejected block must apply nothing");
    assert_eq!(vm.burned().unwrap(), 0, "a rejected block must burn nothing");

    // The transaction is still good and the next block carries it.
    accept_queued(&vm);
    assert!(vm.ciphertext(&tx.subject).is_some());
    assert!(vm.mempool.lock().unwrap().txs.is_empty(), "acceptance is the only thing that clears");
    assert!(vm.mempool.lock().unwrap().claims.is_empty(), "and the only thing that releases");
}

#[test]
fn a_block_reports_processing_until_it_is_accepted() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("s"), 1)).expect("admitted");
    let blk = vm.build_block().expect("a block");
    assert_eq!(blk.status(&vm), STATUS_PROCESSING);
    blk.verify(&vm).expect("verifies");
    blk.accept(&vm).expect("accepted");
    assert_eq!(blk.status(&vm), STATUS_ACCEPTED);
}

#[test]
fn an_accepted_block_re_parses_from_its_stored_bytes() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("rt"), 1));
    let blk = vm.last_block().expect("a tip");

    let parsed = vm.parse_block(&blk.bytes()).expect("it re-parses");
    assert_eq!(parsed.id, blk.id);
    assert_eq!(parsed.height, blk.height);
    assert_eq!(parsed.parent_id, blk.parent_id);

    assert_eq!(vm.get_block(&blk.id).unwrap().id, blk.id);
}

/// The structural refusals: a block claiming to be genesis, one whose parent
/// nobody has, an empty one, and one carrying more transactions than may ever be
/// verified. None of these needs state, so each is refused before any signature is
/// checked.
#[test]
fn verification_refuses_a_block_that_is_not_one() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("tip"), 1));

    let build = |mutate: &dyn Fn(&mut Block)| -> Block {
        let mut b = Block {
            id: ids::EMPTY,
            parent_id: vm.last_accepted(),
            height: vm.height() + 1,
            timestamp: vm.clock.time(),
            transactions: vec![register_tx(&k, TEST_SCHEME, digest_of("payload"), 2)],
        };
        mutate(&mut b);
        b.id = b.compute_id(&vm.chain_id);
        b
    };

    assert!(
        matches!(build(&|b| b.height = 0).verify(&vm).unwrap_err(), Error::InvalidBlock(_)),
        "genesis is not a proposed block"
    );
    assert!(
        build(&|b| b.parent_id = [0xaa; 32]).verify(&vm).is_err(),
        "a parent nobody has"
    );
    assert!(
        matches!(
            build(&|b| b.transactions.clear()).verify(&vm).unwrap_err(),
            Error::InvalidBlock(_)
        ),
        "an empty block buys block space for nothing"
    );

    let tiny = Transaction {
        tx_type: TX_REVOKE_PERMIT,
        nonce: 1,
        payload: RevokePayload::default().to_json(),
        ..Default::default()
    };
    let crowd = build(&|b| {
        b.transactions = vec![tiny.clone(); lux_fhevm::batch::MAX_BLOCK_TXS + 1];
    });
    assert!(matches!(crowd.verify(&vm).unwrap_err(), Error::InvalidBlock(_)));

    // The control: without the mutation it verifies.
    build(&|_| {}).verify(&vm).expect("the control");
}

/// The header a block answers with is the header it was built from — including an
/// id computed on demand when it was never stamped.
#[test]
fn a_block_names_itself_the_same_way_stamped_or_not() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("hdr"), 1));
    let blk = vm.last_block().expect("a tip");

    let unstamped = Block { id: ids::EMPTY, ..blk.clone() };
    assert_eq!(unstamped.id_or_compute(&vm.chain_id), blk.id);

    // An accepted block reports accepted even when it is no longer the tip,
    // because the store holds it.
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("hdr2"), 2));
    assert_ne!(blk.id, vm.last_accepted());
    assert_eq!(blk.status(&vm), STATUS_ACCEPTED);
}

/// The surface F implements without holding anything: the work latch, the fee
/// policy it declares, and the read paths a host reaches for.
#[test]
fn the_chain_answers_the_calls_a_host_makes_of_it() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    assert_eq!(vm.fee_policy.min_tx_fee(), fee::MIN_TX_FEE_FLOOR);
    assert_eq!(vm.fee_policy.fee_asset(), lux_fhevm::vm::utxo_asset_id(96369));

    // The latch wakes when a transaction arrives.
    vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("wake"), 1)).expect("admitted");
    vm.wait_for_event().expect("the latch was signalled");

    // And a stopping chain stops waiting.
    vm.shutdown().expect("the chain stops");
    assert!(matches!(vm.wait_for_event().unwrap_err(), Error::VmShutdown));

    // Releasing nothing does nothing.
    vm.release(&[]);
    assert_eq!(vm.mempool.lock().unwrap().txs.len(), 1);
}

/// Admission stops at the bound. It is open to anyone who can pay, so without one
/// the queue is whatever an adversary chooses to make it.
#[test]
fn the_queue_is_bounded() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    vm.mempool.lock().unwrap().txs = vec![Transaction::default(); lux_fhevm::vm::MAX_MEMPOOL];

    assert!(matches!(
        vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("overflow"), 1)).unwrap_err(),
        Error::MempoolFull
    ));
}

/// The in-flight set's contract, driven directly: after the tip check nothing can
/// reach it with a decided block, and a rule enforced only by its callers is one
/// no test can show still works.
#[test]
fn the_in_flight_set_holds_only_what_is_still_in_flight() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("a"), 1));
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("b"), 2));
    assert_eq!(vm.height(), 2);

    let mut st = vm.state.write().unwrap();

    // A block at or below the accepted height is decided or orphaned. Tracking one
    // would make it resolvable as a parent, which is how an orphan gets built on.
    for h in [1u64, 2] {
        let mut decided = Block {
            id: ids::EMPTY,
            parent_id: st.last_accepted,
            height: h,
            timestamp: vm.clock.time(),
            transactions: Vec::new(),
        };
        decided.id = decided.compute_id(&vm.chain_id);
        st.track_verified(decided.clone());
        assert!(!st.pending_blocks.contains_key(&decided.id), "a decided height is not in flight");
    }

    // One above it is tracked...
    let mut live = Block {
        id: ids::EMPTY,
        parent_id: st.last_accepted,
        height: 3,
        timestamp: vm.clock.time(),
        transactions: Vec::new(),
    };
    live.id = live.compute_id(&vm.chain_id);
    st.track_verified(live.clone());
    assert!(st.pending_blocks.contains_key(&live.id));

    // ...and dropped once the chain passes it, because nothing else will: the
    // engine may drop a block it never accepts and never rejects.
    st.height = 3;
    let mut later = Block {
        id: ids::EMPTY,
        parent_id: live.id,
        height: 4,
        timestamp: vm.clock.time(),
        transactions: Vec::new(),
    };
    later.id = later.compute_id(&vm.chain_id);
    st.track_verified(later.clone());
    assert!(!st.pending_blocks.contains_key(&live.id), "the set is pruned, not merely appended to");
    assert!(st.pending_blocks.contains_key(&later.id));
}

/// A chain with no identity binds nothing: every signature it accepts and every
/// block id it computes would be shared with every other chain that also had
/// none. And what it cannot parse stops the boot rather than starting a chain on
/// defaults nobody chose.
#[test]
fn a_chain_refuses_to_start_without_an_identity_or_on_bytes_it_cannot_parse() {
    let boot = |config: &[u8], genesis: &[u8], chain: ids::Id| {
        Vm::initialize(Init {
            db: Mem::new(),
            chain_id: chain,
            network_id: 96369,
            genesis: genesis.to_vec(),
            config: config.to_vec(),
        })
    };

    assert!(boot(b"{not json", b"", test_chain_id()).is_err(), "a config that does not parse");
    assert!(boot(b"", b"{not json", test_chain_id()).is_err(), "a genesis that does not parse");
    assert!(matches!(
        boot(b"", b"", ids::EMPTY).err().expect("a chain with no identity is refused"),
        Error::InvalidBlock(_)
    ));

    // The control: the same call with all three supplied works, and the config's
    // network id is the one that decides the fee asset.
    let vm = boot(br#"{"networkId":96369}"#, b"", test_chain_id()).expect("the control");
    assert_eq!(vm.network_id, 96369);
}
