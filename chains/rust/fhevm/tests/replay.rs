// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! F COMMITS NO STATE ROOT, and this is what stands in its place.
//!
//! The design turns on determinism: F owns its persistence rather than writing
//! through the FHE runtime's registry, because that registry stamps the wall clock
//! and two validators replaying one block would then store different bytes. That
//! argument is only worth making if the property it protects is actually checked —
//! and a chain with no state root has nothing in consensus that checks it. Two
//! validators could diverge and neither would learn.
//!
//! So the property is checked here instead, the only way it can be from outside
//! consensus: replay the same blocks on two independently-built nodes and require
//! the resulting databases to be byte-identical. That catches a wall clock, a
//! random value, an unordered map walk and a float, all at once, and it catches
//! them by the effect they have rather than by the names they are spelled with.
//!
//! Committing a root IN consensus needs something this VM does not have: a state
//! layer per in-flight block. Verification reads committed state, so a proposer
//! cannot know its post-state until acceptance, and a root checked only at
//! acceptance would be a halt an adversary could trigger by proposing a wrong one
//! — strictly worse than no root.

mod common;

use common::*;

use lux_fhevm::clock::Time;
use lux_fhevm::db::Mem;
use lux_fhevm::fhe;
use lux_fhevm::state::{derive_handle, STATUS_REVOKED};
use lux_fhevm::vm::{Init, Vm};

/// A node built from bytes identical to every other node's, so that any difference
/// in the result comes from applying the block and nothing else. That includes the
/// chain id: these nodes are validators of ONE chain, as production's are, and the
/// id is not a parameter because there is nothing for a caller to vary.
fn replay_node(genesis: &[u8]) -> Vm {
    Vm::initialize(Init {
        db: Mem::new(),
        chain_id: test_chain_id(),
        network_id: 96369,
        genesis: genesis.to_vec(),
        config: Vec::new(),
    })
    .expect("the node starts")
}

/// The property the whole persistence decision rests on: two validators handed the
/// same blocks write the same database.
#[test]
fn two_validators_handed_the_same_blocks_write_the_same_database() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (committee, members) = new_committee(3);
    let mut all: Vec<&TestKey> = members.iter().collect();
    all.extend_from_slice(&[&owner, &grantee]);
    let genesis = test_genesis(fund_all(&all), &committee, 2).to_json();

    // The producer runs the whole lifecycle, so the blocks exercise every
    // operation and every record type.
    let producer = replay_node(&genesis);
    producer.clock.set(Time::from_unix(1_700_000_100));

    let (handle, permit_id) = seed_permit(&producer, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&producer, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);
    let result = digest_of("agreed-result");
    accept_one(&producer, &fulfill_tx(&members[0], request_id, result, 1));
    accept_one(&producer, &fulfill_tx(&members[1], request_id, result, 1));
    let (next, _) = new_committee(3);
    accept_one(&producer, &advance_tx(&members[0], 1, &next, 2, b"epoch-1-key", 2));
    accept_one(&producer, &advance_tx(&members[1], 1, &next, 2, b"epoch-1-key", 2));
    accept_one(&producer, &revoke_tx(&owner, permit_id, 3));

    assert_eq!(producer.height(), 8, "one block per operation, every kind exercised");
    assert_eq!(producer.current_epoch(), 1, "the committee rotated");

    // Collect the chain as it went out on the wire.
    let mut wire: Vec<Vec<u8>> = Vec::new();
    for h in 1..=producer.height() {
        let id = producer.block_id_at(h).expect("the height index names it");
        wire.push(producer.get_block(&id).expect("the block is stored").bytes());
    }

    // Two fresh nodes replay it. Their clocks differ from the producer's and from
    // each other's, because chain time must come from the block.
    let a = replay_node(&genesis);
    a.clock.set(Time::from_unix(1_900_000_000));
    let b = replay_node(&genesis);
    b.clock.set(Time::from_unix(2_100_000_000));

    for node in [&a, &b] {
        for raw in &wire {
            let blk = node.parse_block(raw).expect("it parses");
            blk.verify(node).expect("it verifies");
            blk.accept(node).expect("it is accepted");
        }
    }

    assert_eq!(dump(&producer), dump(&a), "a replaying node must write what the producer wrote");
    assert_eq!(dump(&a), dump(&b), "and two replaying nodes must agree with each other");

    // The replayed state is not merely equal, it is the right state.
    assert_eq!(a.current_epoch(), 1);
    let rec = a.decrypt(&request_id).expect("the request");
    assert_eq!(rec.request.status, fhe::RequestStatus::Completed);
    assert_eq!(rec.request.result_handle, result);
    assert_eq!(a.permit(&permit_id).unwrap().status, STATUS_REVOKED);
}

/// The specific hazard the persistence decision was made to avoid: a record whose
/// timestamp came from the validator rather than the block. Both nodes replay with
/// wildly different clocks; if any stored field read a clock, the databases
/// diverge.
#[test]
fn a_stored_timestamp_comes_from_the_block_and_never_from_the_node() {
    let k = TestKey::new();
    let (committee, keys) = new_committee(1);
    let mut all: Vec<&TestKey> = keys.iter().collect();
    all.push(&k);
    let genesis = test_genesis(fund_all(&all), &committee, 1).to_json();

    let producer = replay_node(&genesis);
    producer.clock.set(Time::from_unix(1_700_000_500));
    accept_one(&producer, &register_tx(&k, TEST_SCHEME, digest_of("stamped"), 1));
    let raw = producer.last_block().expect("a tip").bytes();

    let follower = replay_node(&genesis);
    follower.clock.set(Time::from_unix(1_700_090_000)); // a day later
    let blk = follower.parse_block(&raw).expect("it parses");
    blk.verify(&follower).expect("it verifies");
    blk.accept(&follower).expect("it is accepted");

    assert_eq!(dump(&producer), dump(&follower));

    let rec = follower
        .ciphertext(&derive_handle(&digest_of("stamped"), TEST_SCHEME.as_bytes()))
        .expect("the record");
    assert_eq!(
        rec.meta.registered_at, 1_700_000_500,
        "the record carries the block's time, not the node's"
    );
}
