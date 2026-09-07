// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Each test here pins a defence an adversarial review found missing. They are
//! written against the attack, not against the implementation: if a future change
//! reopens the hole, the test that named it fails.

mod common;

use std::sync::atomic::{AtomicBool, Ordering};

use common::*;

use lux_fhevm::block::{Block, MAX_FUTURE_SKEW_SECS};
use lux_fhevm::clock::Time;
use lux_fhevm::error::Error;
use lux_fhevm::fee;
use lux_fhevm::fhe;
use lux_fhevm::gas::{self, GAS_PER_BYTE};
use lux_fhevm::ids;
use lux_fhevm::state::{committee_digest, derive_handle, tally};
use lux_fhevm::transaction::{
    RegisterPayload, RevokePayload, Transaction, MAX_COMMITTEE, MAX_PAYLOAD, MAX_SCHEME,
    TX_REGISTER_CIPHERTEXT, TX_REVOKE_PERMIT,
};
use lux_fhevm::wire::MAX_BLOCK_SIZE;

/// C1: the epoch-advance effect was qualified by the PROPOSAL, but the decision a
/// member votes on is the epoch, of which exactly one is ever open. Two votes from
/// one member for two different committees therefore had two different effects,
/// survived admission and verification, and collided in acceptance — and a block
/// that passes verification on every validator and then fails to apply on every
/// validator halts the chain at that height by design.
#[test]
fn c1_one_member_cannot_double_vote_on_one_epoch() {
    let (vm, _, members) = new_decrypt_vm(3, 2, &[]);
    let (proposal_a, _) = new_committee(3);
    let (proposal_b, _) = new_committee(3);

    let vote_a = advance_tx(&members[0], 1, &proposal_a, 2, b"k", 1);
    let vote_b = advance_tx(&members[0], 1, &proposal_b, 2, b"k", 2);

    assert_eq!(
        vote_a.effect(),
        vote_b.effect(),
        "one member, one open decision, one effect — whatever it votes for"
    );

    vm.submit_tx(&vote_a).expect("the first vote");
    assert!(matches!(vm.submit_tx(&vote_b).unwrap_err(), Error::DuplicateEffect));

    // A peer can still propose the pair. Consensus must refuse the block rather
    // than certify it and then fail to apply it.
    assert!(matches!(
        force_block(&vm, vec![vote_a, vote_b]).verify(&vm).unwrap_err(),
        Error::DuplicateEffect
    ));
}

/// H1: a payer could queue a nonce gap. Admission took it, verification refused
/// the block for it, and the engine discards a failed block WITHOUT rejecting it —
/// so the transaction vanished and its effect claim did not. A registration's
/// effect is payer-independent, so any funded account could permanently block any
/// handle on that node, for free, and take the rest of the queue with it every
/// round.
#[test]
fn h1_a_gap_never_enters_the_queue_so_it_can_never_poison_a_block() {
    let attacker = TestKey::new();
    let victim = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&attacker, &victim]), &committee, 1);

    let target = digest_of("a-handle-someone-else-wants");

    assert!(matches!(
        vm.submit_tx(&register_tx(&attacker, TEST_SCHEME, target, 3)).unwrap_err(),
        Error::BadNonce
    ));
    assert!(vm.mempool.lock().unwrap().txs.is_empty());
    assert!(vm.mempool.lock().unwrap().claims.is_empty(), "a refused transaction claims nothing");

    // The victim's honest registration goes through untouched.
    let honest = register_tx(&victim, TEST_SCHEME, target, 1);
    vm.submit_tx(&honest).expect("admitted");
    accept_queued(&vm);
    assert_eq!(vm.ciphertext(&honest.subject).unwrap().meta.owner, victim.addr);
}

/// H1: even a well-nonced transaction that is selected into a block and then
/// discarded must leave its claim intact and its place in the queue, because the
/// engine is free to drop a proposal it never accepts or rejects.
#[test]
fn h1_a_claim_tracks_the_queue_exactly() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    let tx = register_tx(&k, TEST_SCHEME, digest_of("claimed"), 1);
    vm.submit_tx(&tx).expect("admitted");
    assert_eq!(vm.mempool.lock().unwrap().claims.len(), 1);
    assert_eq!(vm.mempool.lock().unwrap().queued[&k.addr], 1);

    // Build and then walk away from the proposal entirely — no acceptance, no
    // rejection.
    vm.build_block().expect("a block");
    assert_eq!(vm.mempool.lock().unwrap().txs.len(), 1, "the transaction is still queued");
    assert_eq!(vm.mempool.lock().unwrap().claims.len(), 1, "and still holds its own claim");

    accept_queued(&vm);
    assert!(vm.mempool.lock().unwrap().txs.is_empty());
    assert!(vm.mempool.lock().unwrap().claims.is_empty());
    assert!(vm.mempool.lock().unwrap().queued.is_empty());
    assert!(vm.ciphertext(&tx.subject).is_some());
}

/// H1: one unfit transaction must not take a block's worth of honest ones with it.
/// Selection runs the same admission the receive path runs and simply leaves out
/// what does not fit.
#[test]
fn h1_one_unfit_transaction_does_not_destroy_the_block() {
    let rich = TestKey::new();
    let poor = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(
        vec![(rich.hex_addr(), TEST_FUND), (poor.hex_addr(), 1_000)],
        &committee,
        1,
    );

    let good = register_tx(&rich, TEST_SCHEME, digest_of("good"), 1);
    vm.submit_tx(&good).expect("admitted");

    // The poor payer's transaction is refused at admission, but a peer could still
    // gossip it into our queue shape; put it there directly to prove selection
    // copes.
    vm.mempool.lock().unwrap().txs.push(register_tx(&poor, TEST_SCHEME, digest_of("broke"), 1));

    let blk = vm.build_block().expect("a block");
    assert_eq!(blk.transactions.len(), 1, "the unaffordable one is left out");
    blk.verify(&vm).expect("a proposer cannot build a block its own verification would reject");
    blk.accept(&vm).expect("accepted");

    assert!(vm.ciphertext(&good.subject).is_some(), "the honest transaction is not collateral");
}

/// H2: nothing bounded a block's timestamp or height against its parent, and chain
/// time drives every expiry F enforces. A proposer could rewind time to revive an
/// expired permit, jump to year 36812 to expire everything at once, or skip heights
/// entirely.
#[test]
fn h2_chain_time_and_height_are_bounded_by_the_parent() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    let base = vm.clock.time();
    vm.clock.set(base);
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("tip"), 1));
    let parent = vm.last_block().expect("a tip");

    let next = register_tx(&k, TEST_SCHEME, digest_of("next"), 2);
    vm.submit_tx(&next).expect("admitted");

    let at = |ts: Time, height: u64| -> Block {
        let mut blk = Block {
            id: ids::EMPTY,
            parent_id: vm.last_accepted(),
            height,
            timestamp: ts,
            transactions: vec![next.clone()],
        };
        blk.id = blk.compute_id(&vm.chain_id);
        blk
    };

    // Time may not run backwards past the parent.
    assert!(matches!(
        at(parent.timestamp.add_secs(-1), parent.height + 1).verify(&vm).unwrap_err(),
        Error::InvalidBlock(_)
    ));

    // Nor leap beyond the skew allowance.
    assert!(matches!(
        at(vm.clock.time().add_secs(MAX_FUTURE_SKEW_SECS + 1), parent.height + 1)
            .verify(&vm)
            .unwrap_err(),
        Error::InvalidBlock(_)
    ));
    assert!(matches!(
        at(Time::from_unix(1 << 40), parent.height + 1).verify(&vm).unwrap_err(),
        Error::InvalidBlock(_)
    ));

    // Heights are consecutive.
    assert!(matches!(
        at(vm.clock.time(), parent.height + 2).verify(&vm).unwrap_err(),
        Error::InvalidBlock(_)
    ));
    assert!(matches!(
        at(vm.clock.time(), parent.height).verify(&vm).unwrap_err(),
        Error::InvalidBlock(_)
    ));

    // A block that sits correctly on its parent is fine, including one exactly at
    // the parent's timestamp and one at the edge of the allowance.
    at(parent.timestamp, parent.height + 1).verify(&vm).expect("at the parent's own time");
    at(vm.clock.time().add_secs(MAX_FUTURE_SKEW_SECS), parent.height + 1)
        .verify(&vm)
        .expect("at the edge of the allowance");
}

/// H2: a proposer must never produce a block its own verification would reject.
/// Two blocks inside one clock tick is the ordinary case — chain time steps
/// forward anyway — and a clock so far behind its own tip that it cannot step
/// forward legally declines to propose.
#[test]
fn h2_a_proposer_never_builds_a_block_it_would_reject() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    let base = vm.clock.time();
    vm.clock.set(base);
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("first"), 1));
    let parent = vm.last_block().expect("a tip");

    // The clock has not moved: the ordinary two-blocks-in-one-tick case.
    vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("second"), 2)).expect("admitted");
    let blk = vm.build_block().expect("a block");
    assert!(
        blk.timestamp.after(&parent.timestamp),
        "chain time advances even when the proposer's clock does not"
    );
    blk.verify(&vm).expect("what it built, it can verify");
    blk.accept(&vm).expect("accepted");

    // A clock an hour behind its own tip cannot step forward legally, so it
    // proposes nothing rather than a block it would itself reject.
    vm.clock.set(base.add_secs(-3600));
    vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("third"), 3)).expect("admitted");
    assert!(matches!(vm.build_block().unwrap_err(), Error::ClockBehind));
}

/// H3: the package claimed it holds no ciphertext body "structurally, not as a
/// matter of discipline". It was discipline. Gas had no length term, nothing
/// bounded the payload or the scheme, the decoder ignored members it did not know,
/// and acceptance persists the transaction verbatim — so a megabyte of ciphertext
/// rode onto the chain inside a register payload, for the same fee a hundred bytes
/// cost, and came back out of the block store.
#[test]
fn h3_bytes_are_bounded_before_they_are_decoded_and_priced_once_they_are() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    let digest = digest_of("smuggled");
    let handle = derive_handle(&digest, TEST_SCHEME.as_bytes());
    let sound = RegisterPayload { digest, kind: 4, level: 3, size: 4096 }.to_json();

    // A payload that is a superset of the schema is refused outright: there is no
    // member to hide anything in.
    let mut fat = String::from_utf8(sound.clone()).expect("the sound payload is text");
    fat.pop();
    fat.push_str(&format!(",\"body\":\"{}\"}}", "X".repeat(1024)));
    let mut smuggle = Transaction {
        tx_type: TX_REGISTER_CIPHERTEXT,
        scheme: TEST_SCHEME.as_bytes().to_vec(),
        payer: k.addr,
        subject: handle,
        gas_limit: TEST_GAS,
        nonce: 1,
        payload: fat.into_bytes(),
        ..Default::default()
    };
    k.sign(&mut smuggle);
    assert!(
        matches!(smuggle.syntactic_verify().unwrap_err(), Error::InvalidPayload(_)),
        "a payload the schema does not describe is not a payload"
    );
    assert!(matches!(vm.submit_tx(&smuggle).unwrap_err(), Error::InvalidPayload(_)));

    // Trailing bytes after a well-formed payload are refused too.
    let mut trailing_payload = sound.clone();
    trailing_payload.extend_from_slice(br#"{"body":"more"}"#);
    let mut trailing = Transaction { payload: trailing_payload, ..smuggle.clone() };
    k.sign(&mut trailing);
    assert!(matches!(trailing.syntactic_verify().unwrap_err(), Error::InvalidPayload(_)));

    // Bulk is bounded outright, whatever shape it claims. The payload here is VALID
    // and decodes to exactly the schema — whitespace is not content — so the only
    // thing that can refuse it is the length bound itself. Filling it with bytes
    // that are not JSON would prove nothing: the decoder would refuse those with or
    // without a bound, and the test would pass over a chain that had none.
    let mut padded = sound.clone();
    padded.extend(std::iter::repeat_n(b' ', MAX_PAYLOAD));
    assert!(padded.len() > MAX_PAYLOAD);
    let mut huge = Transaction { payload: padded, ..smuggle.clone() };
    k.sign(&mut huge);
    assert!(matches!(huge.syntactic_verify().unwrap_err(), Error::InvalidPayload(_)));

    // The control: the same value, unpadded, is accepted — so what was refused was
    // the size and nothing else.
    let mut control = Transaction { payload: sound, ..smuggle.clone() };
    k.sign(&mut control);
    control.syntactic_verify().expect("the control");

    // And the scheme is not a second unpriced channel on the operations that ignore
    // it: it is bounded on every operation.
    let (_, permit_id) = seed_permit(&vm, &k, &k, fhe::PERMIT_OP_DECRYPT, 0);
    let mut wide = Transaction {
        tx_type: TX_REVOKE_PERMIT,
        payer: k.addr,
        subject: permit_id,
        scheme: vec![b'A'; MAX_SCHEME + 1],
        gas_limit: TEST_GAS,
        nonce: 3,
        payload: RevokePayload::default().to_json(),
        ..Default::default()
    };
    k.sign(&mut wide);
    assert!(matches!(wide.syntactic_verify().unwrap_err(), Error::InvalidPayload(_)));
}

/// H3: what a transaction stores it pays for. Two identical operations differing
/// only in payload length must not cost the same.
#[test]
fn h3_a_longer_payload_costs_more_and_so_does_a_longer_scheme() {
    let base = gas::gas_for(&Transaction {
        tx_type: TX_REVOKE_PERMIT,
        payload: RevokePayload::default().to_json(),
        ..Default::default()
    })
    .unwrap();

    let long = gas::gas_for(&Transaction {
        tx_type: TX_REVOKE_PERMIT,
        payload: RevokePayload { reason: "r".repeat(1000) }.to_json(),
        ..Default::default()
    })
    .unwrap();

    assert!(long > base, "a longer payload must cost more");
    assert!(long - base >= 1000 * GAS_PER_BYTE, "every stored byte is priced");

    let wide = gas::gas_for(&Transaction {
        tx_type: TX_REVOKE_PERMIT,
        scheme: b"0123456789abcdef".to_vec(),
        payload: RevokePayload::default().to_json(),
        ..Default::default()
    })
    .unwrap();
    assert_eq!(wide, base + 16 * GAS_PER_BYTE);
}

/// H3: a committee is bounded, so the unauthenticated work a transaction can
/// demand before its signature is checked is bounded with it.
#[test]
fn h3_a_committee_is_bounded_in_count() {
    let (big, _) = new_committee(MAX_COMMITTEE + 1);
    assert!(matches!(
        lux_fhevm::transaction::validate_committee(&big, 2, b"k").unwrap_err(),
        Error::InvalidCommittee(_)
    ));

    let (ok, _) = new_committee(MAX_COMMITTEE);
    lux_fhevm::transaction::validate_committee(&ok, 2, b"k").expect("the control");
}

/// H5: the committee check deduplicated node ids, but a member's VOTING IDENTITY
/// is the address of its public key. n seats sharing one key passed every check and
/// seated a threshold only one account could ever vote toward — so no decryption
/// could complete and the committee could never rotate itself out, while the chain
/// reported itself fully seated and healthy.
#[test]
fn h5_a_committee_is_deduplicated_by_voting_identity_not_by_seat() {
    let one = TestKey::new();
    let mut shared: Vec<fhe::CommitteeMember> = (0u8..3)
        .map(|i| fhe::CommitteeMember {
            node_id: [i + 1; 20],   // distinct
            public_key: one.public.clone(), // identical
            weight: 1,
            index: 0,
        })
        .collect();
    shared.sort_by_key(|m| m.node_id);
    for (i, m) in shared.iter_mut().enumerate() {
        m.index = i as i64;
    }

    assert!(
        matches!(
            lux_fhevm::transaction::validate_committee(&shared, 3, b"pk").unwrap_err(),
            Error::InvalidCommittee(_)
        ),
        "three seats and one voter is not a three-of-three committee"
    );

    // Genesis refuses it too, so a chain cannot be born unable to answer.
    let g = test_genesis(Vec::new(), &shared, 3);
    let err = lux_fhevm::Vm::initialize(lux_fhevm::Init {
        db: lux_fhevm::db::Mem::new(),
        chain_id: test_chain_id(),
        network_id: 96369,
        genesis: g.to_json(),
        config: Vec::new(),
    })
    .err()
    .expect("genesis refuses it");
    assert!(matches!(err, Error::InvalidCommittee(_)));

    // And an epoch proposal carrying one is refused before anyone votes on it.
    let advance = Transaction {
        tx_type: lux_fhevm::transaction::TX_ADVANCE_EPOCH,
        nonce: 1,
        subject: committee_digest(1, 3, b"pk", &shared),
        payload: lux_fhevm::transaction::AdvancePayload {
            epoch: 1,
            committee: Some(shared),
            threshold: 3,
            public_key: b"pk".to_vec(),
        }
        .to_json(),
        ..Default::default()
    };
    assert!(matches!(advance.syntactic_verify().unwrap_err(), Error::InvalidCommittee(_)));
}

/// H4: one vote per member, no revote, no timeout, no reset — and the tally only
/// cleared when the advance that could not happen happened. A committee that split
/// its vote could never rotate again, which at unanimity one member could do alone,
/// and which an honest DKG race could do with no adversary at all. A member's
/// LATEST vote is now its vote.
#[test]
fn h4_a_split_vote_can_be_resolved_by_a_member_changing_its_mind() {
    let (vm, _, members) = new_decrypt_vm(3, 3, &[]); // unanimity: the worst case
    let (agreed, _) = new_committee(3);
    let (stale, _) = new_committee(3);
    let pk = b"k";

    accept_one(&vm, &advance_tx(&members[0], 1, &stale, 3, pk, 1));
    accept_one(&vm, &advance_tx(&members[1], 1, &agreed, 3, pk, 1));
    accept_one(&vm, &advance_tx(&members[2], 1, &agreed, 3, pk, 1));
    assert_eq!(vm.current_epoch(), 0, "no proposal has unanimity yet");
    assert_eq!(vm.epoch(0).unwrap().attestations().len(), 3, "one entry per member");

    // The dissenter changes its mind. Its new vote REPLACES the old one.
    accept_one(&vm, &advance_tx(&members[0], 1, &agreed, 3, pk, 2));

    assert_eq!(vm.current_epoch(), 1, "the committee converged and rotated");
    let installed = vm.epoch(1).expect("the successor");
    assert_eq!(installed.committee().len(), 3);
    assert!(installed.attestations().is_empty(), "a fresh epoch starts with no votes cast");

    let old = vm.epoch(0).unwrap();
    assert_eq!(old.attestations().len(), 3, "a member's second vote replaced its first");
    assert_eq!(tally(old.attestations(), &committee_digest(1, 3, pk, &agreed)), 3);
}

/// H4: the same applies to a decryption. A member that attested a result nobody
/// else saw can withdraw it in favour of the one the committee agreed on.
#[test]
fn h4_a_member_can_correct_its_attestation() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 3, &[&owner, &grantee]);

    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);

    let (wrong, right) = (digest_of("wrong"), digest_of("right"));
    accept_one(&vm, &fulfill_tx(&members[0], request_id, wrong, 1));
    accept_one(&vm, &fulfill_tx(&members[1], request_id, right, 1));
    accept_one(&vm, &fulfill_tx(&members[2], request_id, right, 1));

    assert_eq!(
        vm.decrypt(&request_id).unwrap().request.status,
        fhe::RequestStatus::Pending,
        "two of three is not unanimity"
    );

    accept_one(&vm, &fulfill_tx(&members[0], request_id, right, 2));
    let rec = vm.decrypt(&request_id).unwrap();
    assert_eq!(rec.request.status, fhe::RequestStatus::Completed);
    assert_eq!(rec.request.result_handle, right);
    assert_eq!(rec.attestations().len(), 3, "one entry per member");
    assert_eq!(tally(rec.attestations(), &wrong), 0, "the withdrawn vote counts for nothing");
}

/// H6: the block bookkeeping was written under one lock and read under another.
/// Two locks over one map is a data race. It has one owner now: the state lock,
/// with the rest of the chain state.
#[test]
fn h6_the_block_bookkeeping_has_one_owner() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("seed"), 1));

    let stop = AtomicBool::new(false);
    std::thread::scope(|s| {
        // A proposer: writes the in-flight set.
        s.spawn(|| {
            let mut nonce = 2u64;
            while !stop.load(Ordering::SeqCst) {
                if vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of(&nonce.to_string()), nonce)).is_ok()
                {
                    nonce += 1;
                }
                let _ = vm.build_block();
            }
        });
        // A cache reload: the abort path, which reads it.
        s.spawn(|| {
            while !stop.load(Ordering::SeqCst) {
                let _ = vm.load_state();
            }
        });
        // Readers on the consensus surface.
        s.spawn(|| {
            while !stop.load(Ordering::SeqCst) {
                let tip = vm.last_accepted();
                let _ = vm.get_block(&tip);
                let _ = vm.health_check();
                let _ = vm.block_id_at(1);
            }
        });
        std::thread::sleep(std::time::Duration::from_millis(300));
        stop.store(true, Ordering::SeqCst);
    });
}

/// M1: the request's permit was written and read by nothing but the view, so an
/// owner who withdrew a capability watched the committee answer the request anyway
/// and deliver the plaintext to the callback. Revocation now reaches an in-flight
/// request.
#[test]
fn m1_revocation_stops_an_in_flight_decryption() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);

    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);

    // One member has already answered when the owner withdraws consent.
    let result = digest_of("plaintext-handle");
    accept_one(&vm, &fulfill_tx(&members[0], request_id, result, 1));
    accept_one(&vm, &revoke_tx(&owner, permit_id, 3));

    // The committee can no longer complete it.
    assert!(matches!(
        vm.submit_tx(&fulfill_tx(&members[1], request_id, result, 1)).unwrap_err(),
        Error::PermitRevoked
    ));

    // And a peer forcing it into a block gets a revert, not an answer.
    let forced = fulfill_tx(&members[1], request_id, result, 1);
    let blk = force_block(&vm, vec![forced]);
    blk.verify(&vm).expect("it is well-formed, ordered and paid for");
    blk.accept(&vm).expect("the block applies; the transaction reverts");

    let rec = vm.decrypt(&request_id).unwrap();
    assert_eq!(rec.request.status, fhe::RequestStatus::Pending, "a revoked permit answers nothing");
    assert_eq!(rec.attestations().len(), 1, "the second attestation never landed");
}

/// M1: expiry bounds the ASK, not the ANSWER. A permit that ran out after a request
/// was properly made does not strand it — the grantee asked in time, and the
/// committee answering later is not the grantee acting.
#[test]
fn m1_a_lapsed_permit_does_not_strand_a_request_made_in_time() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);

    let base = vm.clock.time();
    vm.clock.set(base);
    let permit_expiry = base.unix() + 60;
    let (handle, permit_id) =
        seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, permit_expiry);

    // The request is made in time, with a window that outlasts the permit.
    accept_one(
        &vm,
        &request_tx(&grantee, TEST_SCHEME, handle, permit_id, base.unix() + 10_000, 1),
    );
    let request_id = request_id_for(handle, &grantee.addr, 1);

    // The permit lapses; the committee answers afterwards.
    vm.clock.set(Time::from_unix(permit_expiry + 1));
    let result = digest_of("answer");
    accept_one(&vm, &fulfill_tx(&members[0], request_id, result, 1));
    accept_one(&vm, &fulfill_tx(&members[1], request_id, result, 1));

    let rec = vm.decrypt(&request_id).unwrap();
    assert_eq!(
        rec.request.status,
        fhe::RequestStatus::Completed,
        "the grantee asked while it could; a lapsed permit is not a withdrawn one"
    );
    assert_eq!(rec.request.result_handle, result);
}

/// L1: the parser walked the transaction blob by the declared lengths and never
/// checked it had consumed it, so a block had many valid encodings and one id.
#[test]
fn l1_bytes_inside_the_blob_that_no_length_covers_are_refused() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("canon"), 1));
    let blk = vm.last_block().expect("a tip");

    // Its own encoding round-trips.
    assert_eq!(vm.parse_block(&blk.bytes()).unwrap().id, blk.id);

    // Bytes inside the blob that no length covers are refused.
    let mut lens = Vec::new();
    let mut blob = Vec::new();
    for tx in &blk.transactions {
        let b = tx.bytes();
        lens.push(b.len() as u32);
        blob.extend_from_slice(&b);
    }
    blob.extend(std::iter::repeat_n(0xAAu8, 4096));
    let padded = blk.bytes_with_tx_lens(&lens, &blob);
    assert_ne!(padded, blk.bytes());
    assert!(matches!(vm.parse_block(&padded).unwrap_err(), Error::InvalidPayload(_)));

    // So are bytes after the message.
    let mut trailing = blk.bytes();
    trailing.push(0xff);
    assert!(matches!(vm.parse_block(&trailing).unwrap_err(), Error::InvalidPayload(_)));
}

/// M3: a follower could not verify a block whose parent it had only parsed. The
/// in-flight set was written in exactly one place — building — so a block arriving
/// from a peer was never findable by id, and its child failed with "parent not
/// found" no matter what it contained.
#[test]
fn m3_a_follower_can_verify_against_a_parent_it_only_parsed() {
    let first = TestKey::new();
    let second = TestKey::new();
    let (committee, _) = new_committee(1);
    let proposer = new_test_vm(fund_all(&[&first, &second]), &committee, 1);
    let follower = new_test_vm(fund_all(&[&first, &second]), &committee, 1);

    let build = |k: &TestKey, handle: &str| -> Block {
        proposer.submit_tx(&register_tx(k, TEST_SCHEME, digest_of(handle), 1)).expect("admitted");
        proposer.build_block().expect("a block")
    };

    let b1 = build(&first, "one");
    b1.accept(&proposer).expect("accepted");
    let b2 = build(&second, "two"); // built on b1, still in flight

    // The follower accepts neither. b2 must still verify: its parent is a block the
    // follower has only parsed.
    let p1 = follower.parse_block(&b1.bytes()).expect("it parses");
    p1.verify(&follower).expect("the parent verifies");

    let p2 = follower.parse_block(&b2.bytes()).expect("it parses");
    p2.verify(&follower).expect("a verified parent must be findable, whoever built it");
}

/// M3: the tracker must not become the leak an unreleased claim was. The engine may
/// drop a block it never accepts and never rejects, so nothing else removes it.
#[test]
fn m3_the_in_flight_set_is_bounded_by_the_accepted_height() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    for nonce in 1..=4u64 {
        accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of(&nonce.to_string()), nonce));
    }

    let st = vm.state.read().unwrap();
    for (id, blk) in &st.pending_blocks {
        assert!(
            blk.height > st.height,
            "block {} at height {} is at or below the accepted height {}",
            ids::id_string(id),
            blk.height,
            st.height
        );
    }
}

/// C2: verification required only that a block's height was its parent's plus one,
/// and never that the parent was the TIP. A block at height 2 whose parent is the
/// long-since-accepted block at height 1 satisfies that perfectly, so a chain at
/// height 3 verified it, accepted it, and rewound.
#[test]
fn c2_an_orphan_cannot_rewind_the_chain() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("one"), 1));
    let fork_point = vm.last_accepted();
    let fork_time = vm.last_block().unwrap().timestamp;
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("two"), 2));
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("three"), 3));
    let (tip, height) = (vm.last_accepted(), vm.height());
    assert_eq!(height, 3);

    // Everything else about this block is impeccable: the height follows its
    // parent, the time follows its parent and is inside the skew allowance, and its
    // transaction carries the payer's next nonce against committed state.
    let mut orphan = Block {
        id: ids::EMPTY,
        parent_id: fork_point,
        height: 2,
        timestamp: fork_time.add_secs(1),
        transactions: vec![register_tx(&k, TEST_SCHEME, digest_of("rewind"), 4)],
    };
    orphan.id = orphan.compute_id(&vm.chain_id);

    assert!(matches!(orphan.verify(&vm).unwrap_err(), Error::NotOnTip(_)));
    assert!(
        matches!(orphan.accept(&vm).unwrap_err(), Error::NotOnTip(_)),
        "acceptance decides this too: verification judged an earlier tip"
    );

    assert_eq!(vm.height(), height, "the chain did not rewind");
    assert_eq!(vm.last_accepted(), tip);
    assert_ne!(vm.block_id_at(2).unwrap(), orphan.id, "the index still names the accepted chain");
    assert!(vm.ciphertext(&orphan.transactions[0].subject).is_none(), "and applied nothing");

    // The control: the same transaction in a block that DOES extend the tip is
    // accepted. What was refused is the parent, not the contents.
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("rewind"), 4));
    assert_eq!(vm.height(), 4);
}

/// C2: acceptance re-decides it because the tip moves between the two calls. The
/// engine may verify a block, accept a sibling, and only then accept the first — at
/// which point it no longer extends anything.
#[test]
fn c2_a_tip_that_moves_after_verification_is_caught_by_acceptance() {
    let first = TestKey::new();
    let second = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&first, &second]), &committee, 1);

    let a = force_block(&vm, vec![register_tx(&first, TEST_SCHEME, digest_of("a"), 1)]);
    let b = force_block(&vm, vec![register_tx(&second, TEST_SCHEME, digest_of("b"), 1)]);
    assert_ne!(a.id, b.id, "two distinct siblings on one parent");

    a.verify(&vm).expect("a verifies");
    b.verify(&vm).expect("both verify against the same tip");

    a.accept(&vm).expect("a is accepted");
    assert!(
        matches!(b.accept(&vm).unwrap_err(), Error::NotOnTip(_)),
        "the sibling no longer extends the tip, whatever verification said earlier"
    );
    assert_eq!(vm.height(), 1);
}

/// H7: the transaction-count bound was applied by verification, and verification
/// runs after the parse — so nothing bounded what a peer could make this node
/// parse, hash and allocate before a single check had run. Both bounds belong at
/// the first byte, and neither implies the other.
#[test]
fn h7_a_block_is_bounded_in_bytes_and_in_count_at_the_parse() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    // Bytes: a block over the size bound is refused before it is decoded.
    let mut fat = register_tx(&k, TEST_SCHEME, digest_of("fat"), 1);
    fat.payload = vec![b'A'; MAX_PAYLOAD];
    k.sign(&mut fat);
    let huge = Block {
        id: ids::EMPTY,
        parent_id: vm.last_accepted(),
        height: 1,
        timestamp: vm.clock.time(),
        transactions: vec![fat; 32],
    };
    let raw = huge.bytes();
    assert!(raw.len() > MAX_BLOCK_SIZE);
    assert!(matches!(vm.parse_block(&raw).unwrap_err(), Error::InvalidPayload(_)));

    // Count: a block UNDER the size bound may still declare more transactions than
    // may ever be verified, so the count is bounded at the parse too.
    let tiny = Transaction {
        tx_type: TX_REVOKE_PERMIT,
        nonce: 1,
        payload: RevokePayload::default().to_json(),
        ..Default::default()
    };
    let crowd = Block {
        id: ids::EMPTY,
        parent_id: vm.last_accepted(),
        height: 1,
        timestamp: vm.clock.time(),
        transactions: vec![tiny; lux_fhevm::batch::MAX_BLOCK_TXS + 1],
    };
    let crowded = crowd.bytes();
    assert!(crowded.len() <= MAX_BLOCK_SIZE, "this one is small; only the count is out of bounds");
    assert!(matches!(vm.parse_block(&crowded).unwrap_err(), Error::InvalidPayload(_)));

    // A block this node builds is held to the byte bound too, so a proposer cannot
    // produce one its peers refuse to parse.
    assert!(matches!(huge.verify(&vm).unwrap_err(), Error::InvalidBlock(_)));

    // The control: one transaction under both bounds round-trips.
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("ordinary"), 1));
    vm.parse_block(&vm.last_block().unwrap().bytes()).expect("the control");
}

/// H7: selection stops at the byte bound as well as the count, so the proposer and
/// the parser agree. Stopping only on the count let 1024 ordinary transactions —
/// each carrying an ML-DSA-65 public key and signature — build a 5 MB block this
/// node's own parser refuses.
#[test]
fn h7_selection_stops_at_the_byte_bound_well_before_the_count() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), 1u64 << 50)], &committee, 1);

    // Ordinary registrations — nothing oversized about any of them. What makes them
    // heavy is the fixed part: a public key and a signature, about 5 KB each, which
    // no payload bound touches.
    const QUEUED: u64 = 450;
    for nonce in 1..=QUEUED {
        vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of(&nonce.to_string()), nonce))
            .expect("admitted");
    }
    assert_eq!(vm.mempool.lock().unwrap().txs.len(), QUEUED as usize);

    let blk = vm.build_block().expect("a block");
    assert!(
        blk.bytes().len() <= MAX_BLOCK_SIZE,
        "the proposer stopped at the size its own parser enforces"
    );
    assert!(blk.transactions.len() < QUEUED as usize, "so it could not take them all");
    assert!(
        blk.transactions.len() < lux_fhevm::batch::MAX_BLOCK_TXS,
        "and the BYTE bound is what stopped it, well before the count bound"
    );

    blk.verify(&vm).expect("what it built, it can verify");
    vm.parse_block(&blk.bytes()).expect("what it built, it can parse");

    // The rest is still queued and goes out in the next block.
    blk.accept(&vm).expect("accepted");
    assert_eq!(vm.mempool.lock().unwrap().txs.len(), QUEUED as usize - blk.transactions.len());
    assert!(!accept_queued(&vm).transactions.is_empty());
}

/// H8: nothing bound a transaction or a block to the chain it was meant for. An
/// address is the hash of a public key, so the same payer exists on every F-Chain;
/// a transaction lifted from one authenticated verbatim on the others and burned a
/// balance there for an operation nobody asked for. Blocks were worse: every chain
/// whose genesis carried the same timestamp had the SAME genesis id.
#[test]
fn h8_a_signature_and_a_block_each_name_one_chain() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let mut other = ids::EMPTY;
    other[..7].copy_from_slice(b"another");

    let home = new_test_vm(fund_all(&[&k]), &committee, 1);
    let away = new_vm_on_chain(other, fund_all(&[&k]), &committee, 1);

    // Same genesis bytes, same timestamp, different chains: different ids.
    assert_ne!(
        home.last_accepted(),
        away.last_accepted(),
        "two chains must not share a genesis block"
    );

    // A transaction signed for home does not authenticate away.
    let tx = register_tx(&k, TEST_SCHEME, digest_of("mine"), 1);
    tx.authenticate(&home.chain_id).expect("it authenticates at home");
    assert!(matches!(tx.authenticate(&away.chain_id).unwrap_err(), Error::BadSignature));
    assert!(matches!(away.submit_tx(&tx).unwrap_err(), Error::BadSignature));

    // Nor inside a block: a peer forcing it in gets a refusal, not an effect.
    let mut forced = Block {
        id: ids::EMPTY,
        parent_id: away.last_accepted(),
        height: 1,
        timestamp: away.clock.time(),
        transactions: vec![tx.clone()],
    };
    forced.id = forced.compute_id(&away.chain_id);
    assert!(matches!(forced.verify(&away).unwrap_err(), Error::BadSignature));

    // And home's whole block does not land away: its parent is a genesis away has
    // never heard of.
    accept_one(&home, &tx);
    let parsed = away
        .parse_block(&home.last_block().unwrap().bytes())
        .expect("the bytes are well formed; it is the chain that differs");
    assert!(parsed.verify(&away).is_err());
    assert_eq!(away.height(), 0, "nothing from another chain moved this one");

    // The control: signed for away, it works away.
    let mut native = register_tx(&k, TEST_SCHEME, digest_of("theirs"), 1);
    k.sign_for(&other, &mut native);
    away.submit_tx(&native).expect("its own chain accepts it");
}

/// The receive path must decide exactly what the build path decides. A sibling
/// chain's parser discarded the transaction set, so a signature check gated on the
/// set being non-empty ran only on blocks that node had built itself: the same
/// block was refused in memory and accepted off the wire.
#[test]
fn the_wire_verdict_is_the_memory_verdict() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    let build = |txs: Vec<Transaction>| -> Block {
        let mut b = Block {
            id: ids::EMPTY,
            parent_id: vm.last_accepted(),
            height: vm.height() + 1,
            timestamp: vm.clock.time(),
            transactions: txs,
        };
        b.id = b.compute_id(&vm.chain_id);
        b
    };
    let both_ways = |blk: &Block| -> (lux_fhevm::Result<()>, lux_fhevm::Result<()>) {
        let in_memory = blk.verify(&vm);
        let off_wire = match vm.parse_block(&blk.bytes()) {
            Ok(parsed) => parsed.verify(&vm),
            Err(e) => Err(e),
        };
        (in_memory, off_wire)
    };

    // Sound: both accept.
    let sound = build(vec![register_tx(&k, TEST_SCHEME, digest_of("sound"), 1)]);
    let (mem, wire) = both_ways(&sound);
    mem.expect("in memory");
    wire.expect("off the wire");

    // Forged signature: both refuse, and for the same reason.
    let mut forged = register_tx(&k, TEST_SCHEME, digest_of("forged"), 1);
    forged.sig[0] ^= 0xff;
    let (mem, wire) = both_ways(&build(vec![forged]));
    assert!(matches!(mem.unwrap_err(), Error::BadSignature));
    assert!(matches!(wire.unwrap_err(), Error::BadSignature));

    // Unsigned: a transaction carrying no authorization at all must not become
    // authorized by travelling.
    let mut unsigned = register_tx(&k, TEST_SCHEME, digest_of("unsigned"), 1);
    unsigned.auth.clear();
    unsigned.sig.clear();
    let (mem, wire) = both_ways(&build(vec![unsigned]));
    assert!(matches!(mem.unwrap_err(), Error::UnsignedTx));
    assert!(matches!(wire.unwrap_err(), Error::UnsignedTx));

    // A nonce out of order: refused both ways.
    let (mem, wire) = both_ways(&build(vec![register_tx(&k, TEST_SCHEME, digest_of("gap"), 9)]));
    assert!(matches!(mem.unwrap_err(), Error::BadNonce));
    assert!(matches!(wire.unwrap_err(), Error::BadNonce));

    // And an empty block does not become non-empty by being parsed.
    let (mem, _) = both_ways(&build(Vec::new()));
    assert!(matches!(mem.unwrap_err(), Error::InvalidBlock(_)));
}

/// H2, residual: chain time is monotone and bounded ahead, and that is ALL a chain
/// can promise about it. Within [parent timestamp, local clock + skew] the proposer
/// still chooses, so a permit that lapsed during a gap in block production can
/// still authorize one request in the block that closes the gap.
///
/// This measures that freedom rather than leaving it unexamined: the window is
/// exactly the time since the last block. On a chain producing blocks it is
/// seconds; on an idle chain it is the length of the idle period. Removing it
/// entirely means expiries counted in block heights, which no proposer can rewind
/// — a change to the grant surface, not a patch.
#[test]
fn h2_the_proposers_remaining_freedom_is_exactly_the_gap_since_the_last_block() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, _) = new_decrypt_vm(3, 2, &[&owner, &grantee]);

    let base = vm.clock.time();
    vm.clock.set(base);
    let expiry = base.unix() + 60;
    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, expiry);
    let parent = vm.last_block().expect("a tip");

    // Long past the permit's expiry, admission refuses the request.
    vm.clock.set(Time::from_unix(expiry + 100_000));
    let req = request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1);
    assert!(matches!(vm.submit_tx(&req).unwrap_err(), Error::PermitExpired));

    let at = |ts: Time| -> Block {
        let mut blk = Block {
            id: ids::EMPTY,
            parent_id: vm.last_accepted(),
            height: parent.height + 1,
            timestamp: ts,
            transactions: vec![req.clone()],
        };
        blk.id = blk.compute_id(&vm.chain_id);
        blk
    };

    // The proposer cannot go below the parent, which is the bound that exists.
    assert!(matches!(
        at(parent.timestamp.add_secs(-1)).verify(&vm).unwrap_err(),
        Error::InvalidBlock(_)
    ));

    // Inside the window it can, and the request is authorized — by a permit that
    // has, in wall-clock terms, expired. The block is valid; the transaction
    // applies. This is the residual, and it is exactly this large.
    let inside = at(Time::from_unix(expiry - 1));
    inside.verify(&vm).expect("inside the window");
    inside.accept(&vm).expect("and it applies");
    assert!(vm.decrypt(&request_id_for(handle, &grantee.addr, 1)).is_some());

    // And it closes behind itself: chain time has now passed the expiry, so no later
    // block can reach back. A second request is refused by consensus, not merely by
    // admission.
    vm.clock.set(Time::from_unix(expiry + 100_000));
    let later = request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 2);
    let mut next = Block {
        id: ids::EMPTY,
        parent_id: vm.last_accepted(),
        height: vm.height() + 1,
        timestamp: vm.last_block().unwrap().timestamp.add_secs(2),
        transactions: vec![later],
    };
    next.id = next.compute_id(&vm.chain_id);
    next.verify(&vm).expect("the block is valid");
    next.accept(&vm).expect("and applies");
    assert!(
        vm.decrypt(&request_id_for(handle, &grantee.addr, 2)).is_none(),
        "the expired permit authorizes nothing once chain time passes it"
    );
}

/// L3: what an UNAUTHENTICATED transaction can make this node do before its
/// signature is checked. The answer must be bounded by its own declared size, and
/// it is: bytes and count are bounded at the parse, the payload and scheme are
/// bounded before either is decoded, and the one expensive decode — parsing a
/// committee's public keys — is bounded by the committee count.
#[test]
fn l3_the_work_an_unauthenticated_transaction_can_demand_is_bounded_by_its_size() {
    let k = TestKey::new();

    // The costly branch: an epoch proposal parses a public key per member, and that
    // runs before authentication. The count bounds it.
    let (over, _) = new_committee(MAX_COMMITTEE + 1);
    let huge = Transaction {
        tx_type: lux_fhevm::transaction::TX_ADVANCE_EPOCH,
        nonce: 1,
        subject: committee_digest(1, 2, b"pk", &over),
        payload: lux_fhevm::transaction::AdvancePayload {
            epoch: 1,
            committee: Some(over),
            threshold: 2,
            public_key: b"pk".to_vec(),
        }
        .to_json(),
        ..Default::default()
    };
    assert!(
        matches!(huge.syntactic_verify().unwrap_err(), Error::InvalidCommittee(_)),
        "the committee is refused by COUNT, before any of its keys is parsed"
    );

    // Every other byte channel is bounded before it is decoded at all.
    let cases: Vec<(&str, Transaction)> = vec![
        (
            "payload",
            Transaction {
                tx_type: TX_REVOKE_PERMIT,
                nonce: 1,
                payload: vec![b'A'; MAX_PAYLOAD + 1],
                ..Default::default()
            },
        ),
        (
            "scheme",
            Transaction {
                tx_type: TX_REVOKE_PERMIT,
                nonce: 1,
                scheme: vec![b'A'; MAX_SCHEME + 1],
                payload: RevokePayload::default().to_json(),
                ..Default::default()
            },
        ),
        (
            "auth",
            Transaction {
                tx_type: TX_REVOKE_PERMIT,
                nonce: 1,
                payload: RevokePayload::default().to_json(),
                auth: vec![b'A'; 9],
                ..Default::default()
            },
        ),
        (
            "sig",
            Transaction {
                tx_type: TX_REVOKE_PERMIT,
                nonce: 1,
                payload: RevokePayload::default().to_json(),
                sig: vec![b'A'; 9],
                ..Default::default()
            },
        ),
    ];
    for (name, tx) in cases {
        assert!(
            matches!(tx.syntactic_verify().unwrap_err(), Error::InvalidPayload(_)),
            "{name} is not bounded"
        );
    }

    // And the order holds where it matters: a transaction whose signature is wrong
    // is refused, so nothing beyond the bounded checks above is ever done on an
    // unauthenticated one.
    let mut forged = register_tx(&k, TEST_SCHEME, digest_of("forged"), 1);
    forged.sig[0] ^= 0xff;
    assert!(matches!(forged.authenticate(&test_chain_id()).unwrap_err(), Error::BadSignature));
}

/// The fee surface fails closed on the same inputs the gas schedule does. A fee
/// that defaulted to zero on an unknown operation would be an operation that
/// settles nothing.
#[test]
fn the_fee_surface_refuses_what_it_cannot_price() {
    assert!(gas::fee_for(&Transaction { tx_type: 99, ..Default::default() }).is_err());
    assert!(gas::fee_for(&Transaction {
        tx_type: TX_REGISTER_CIPHERTEXT,
        scheme: b"rot13-n1".to_vec(),
        ..Default::default()
    })
    .is_err());
    let priced = gas::fee_for(&Transaction {
        tx_type: TX_REGISTER_CIPHERTEXT,
        scheme: TEST_SCHEME.as_bytes().to_vec(),
        ..Default::default()
    })
    .expect("the control");
    assert!(priced > 0, "a priced operation settles something");
    assert!(priced >= fee::MIN_TX_FEE_FLOOR);
}
