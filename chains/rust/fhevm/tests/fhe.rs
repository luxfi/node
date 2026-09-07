// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The operations F actually exposes — registering an encrypted value, granting
//! and withdrawing access to it, asking the committee to decrypt it, and rotating
//! the committee — and the threshold rules that make each of them safe.

mod common;

use common::*;

use lux_fhevm::clock::Time;
use lux_fhevm::error::Error;
use lux_fhevm::fhe;
use lux_fhevm::state::{
    derive_handle, derive_permit_id, tally, STATUS_ACTIVE, STATUS_REVOKED,
};
use lux_fhevm::transaction::DEFAULT_REQUEST_WINDOW;
use lux_fhevm::vm::height_key;

/// One encrypted value from registration to a completed threshold decryption —
/// the whole reason F exists, end to end.
#[test]
fn a_confidential_value_goes_from_registration_to_a_completed_decryption() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);

    // 1. The owner registers an encrypted value. F stores its digest, never it.
    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    let ct = vm.ciphertext(&handle).expect("the ciphertext is registered");
    assert_eq!(ct.meta.epoch, 0, "a ciphertext is bound to the epoch it was registered in");

    // 2. The grantee, holding the permit, asks the committee to decrypt.
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);

    let rec = vm.decrypt(&request_id).expect("the request is recorded");
    assert_eq!(rec.request.status, fhe::RequestStatus::Pending);
    assert_eq!(rec.request.ciphertext_handle, handle);
    assert_eq!(rec.permit_id, permit_id);
    assert_eq!(rec.request.epoch, 0);
    assert!(
        rec.request.expiry > rec.request.created_at,
        "an unbounded request would outlive the committee that can answer it"
    );
    assert_eq!(rec.request.expiry, rec.request.created_at + DEFAULT_REQUEST_WINDOW);

    // 3. Two of three members attest the same result handle. F never sees the
    //    plaintext — the committee combines its shares off-chain and agrees on the
    //    handle of what came out.
    let result = digest_of("decrypted-result-handle");
    accept_one(&vm, &fulfill_tx(&members[0], request_id, result, 1));
    let rec = vm.decrypt(&request_id).unwrap();
    assert_eq!(rec.request.status, fhe::RequestStatus::Pending, "one member is not a threshold");
    assert_eq!(rec.attestations().len(), 1);

    accept_one(&vm, &fulfill_tx(&members[1], request_id, result, 1));
    let rec = vm.decrypt(&request_id).unwrap();
    assert_eq!(rec.request.status, fhe::RequestStatus::Completed, "the threshold answers it");
    assert_eq!(rec.request.result_handle, result);
    assert_eq!(rec.attestations().len(), 2);
    assert_ne!(rec.request.completed_at, 0);

    // 4. The answer is final: a third attestation is refused.
    let err = vm.submit_tx(&fulfill_tx(&members[2], request_id, result, 1)).unwrap_err();
    assert!(matches!(err, Error::RequestClosed));
}

/// A lying member buys nothing but its own burnt fee. It attests first, and
/// wrong; the honest members still reach the threshold on the true result, because
/// votes are tallied per value rather than pinned by whoever spoke first.
#[test]
fn a_conflicting_attestation_cannot_stall_the_honest_majority() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);
    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);

    let (liar, truth) = (digest_of("forged"), digest_of("true-result"));

    accept_one(&vm, &fulfill_tx(&members[2], request_id, liar, 1));
    assert_eq!(vm.decrypt(&request_id).unwrap().request.status, fhe::RequestStatus::Pending);

    accept_one(&vm, &fulfill_tx(&members[0], request_id, truth, 1));
    accept_one(&vm, &fulfill_tx(&members[1], request_id, truth, 1));

    let rec = vm.decrypt(&request_id).unwrap();
    assert_eq!(rec.request.status, fhe::RequestStatus::Completed);
    assert_eq!(rec.request.result_handle, truth, "the honest majority decides the answer");
    assert_eq!(rec.attestations().len(), 3, "the false vote is counted against its own value");

    assert!(vm.balance(&members[2].addr).unwrap() < TEST_FUND, "the liar paid for the privilege");
}

/// A stranger cannot answer a decryption request, and a member cannot vote twice.
#[test]
fn only_the_committee_answers_and_a_member_holds_one_vote() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let stranger = TestKey::new();
    let (committee, members) = new_committee(3);
    let mut all: Vec<&TestKey> = members.iter().collect();
    all.extend_from_slice(&[&owner, &grantee, &stranger]);
    let vm = new_test_vm(fund_all(&all), &committee, 2);

    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);
    let result = digest_of("r");

    assert!(matches!(
        vm.submit_tx(&fulfill_tx(&stranger, request_id, result, 1)).unwrap_err(),
        Error::NotCommittee
    ));

    // Even the requester, who has every right to the answer, is not a member.
    assert!(matches!(
        vm.submit_tx(&fulfill_tx(&grantee, request_id, result, 2)).unwrap_err(),
        Error::NotCommittee
    ));

    accept_one(&vm, &fulfill_tx(&members[0], request_id, result, 1));

    // One member, one vote — but the vote is not spent by being cast. A second
    // attestation REPLACES the first rather than adding to the tally, so a member
    // can correct itself without ever counting twice.
    accept_one(&vm, &fulfill_tx(&members[0], request_id, digest_of("other"), 2));
    let rec = vm.decrypt(&request_id).unwrap();
    assert_eq!(rec.attestations().len(), 1, "one entry per member, however often it votes");
    assert_eq!(rec.attestations()[0].value, digest_of("other"));
    assert_eq!(tally(rec.attestations(), &result), 0, "the withdrawn vote counts for nothing");
    assert_eq!(rec.request.status, fhe::RequestStatus::Pending);
}

#[test]
fn a_request_that_was_never_made_cannot_be_answered() {
    let (vm, _, members) = new_decrypt_vm(3, 2, &[]);
    let err = vm
        .submit_tx(&fulfill_tx(&members[0], digest_of("never-asked"), digest_of("r"), 1))
        .unwrap_err();
    assert!(matches!(err, Error::RequestNotFound));
}

/// A request outlives nothing: once its window closes, no attestation is accepted
/// and it can never complete.
#[test]
fn an_expired_request_can_never_be_answered_and_reads_as_expired() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);

    let now = vm.clock.time();
    vm.clock.set(now);
    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);

    let expiry = now.unix() + 60;
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, expiry, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);

    // Still answerable inside the window.
    {
        let st = vm.state.read().unwrap();
        fulfill_tx(&members[0], request_id, digest_of("r"), 1)
            .check_auth(&st, expiry)
            .expect("inside the window");
    }

    // One second past it, closed.
    vm.clock.set(Time::from_unix(expiry + 1));
    let err = vm.submit_tx(&fulfill_tx(&members[0], request_id, digest_of("r"), 1)).unwrap_err();
    assert!(matches!(err, Error::RequestExpired));

    // And the read surface says so, rather than reporting it as still pending.
    let reply = lux_fhevm::service::call(
        &vm,
        "fchain.getDecrypt",
        &serde_json::json!({ "requestId": hex_of(request_id) }),
    )
    .expect("the request reads");
    assert_eq!(reply["request"]["status"], "expired");
}

/// The permit is the whole authority for a decryption request: it must exist, be
/// unrevoked, be unexpired, name THIS handle, name THIS grantee, and confer
/// decrypt specifically.
#[test]
fn a_permit_gates_a_decryption_on_every_one_of_its_terms() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let stranger = TestKey::new();
    let (committee, members) = new_committee(1);
    let mut all: Vec<&TestKey> = members.iter().collect();
    all.extend_from_slice(&[&owner, &grantee, &stranger]);
    let vm = new_test_vm(fund_all(&all), &committee, 1);

    let now = vm.clock.time();
    vm.clock.set(now);
    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);

    // No such permit.
    assert!(matches!(
        vm.submit_tx(&request_tx(&grantee, TEST_SCHEME, handle, digest_of("nope"), 0, 1))
            .unwrap_err(),
        Error::PermitNotFound
    ));

    // The permit belongs to someone else.
    assert!(matches!(
        vm.submit_tx(&request_tx(&stranger, TEST_SCHEME, handle, permit_id, 0, 1)).unwrap_err(),
        Error::Unauthorized
    ));

    // The permit is for another handle.
    let other = register_tx(&owner, TEST_SCHEME, digest_of("other-value"), 3);
    accept_one(&vm, &other);
    assert!(matches!(
        vm.submit_tx(&request_tx(&grantee, TEST_SCHEME, other.subject, permit_id, 0, 1))
            .unwrap_err(),
        Error::PermitInvalid(_)
    ));

    // The ciphertext was never registered.
    assert!(matches!(
        vm.submit_tx(&request_tx(&grantee, TEST_SCHEME, digest_of("unregistered"), permit_id, 0, 1))
            .unwrap_err(),
        Error::CiphertextNotFound
    ));

    // The permit confers compute but not decrypt.
    accept_one(&vm, &grant_tx(&owner, handle, grantee.addr, fhe::PERMIT_OP_COMPUTE, 0, 4));
    let compute_only =
        derive_permit_id(&handle, &owner.addr, &grantee.addr, fhe::PERMIT_OP_COMPUTE, 0, 4);
    assert!(matches!(
        vm.submit_tx(&request_tx(&grantee, TEST_SCHEME, handle, compute_only, 0, 1)).unwrap_err(),
        Error::PermitInvalid(_)
    ));

    // The permit has expired.
    let expiry = now.unix() + 30;
    accept_one(&vm, &grant_tx(&owner, handle, grantee.addr, fhe::PERMIT_OP_DECRYPT, expiry, 5));
    let short =
        derive_permit_id(&handle, &owner.addr, &grantee.addr, fhe::PERMIT_OP_DECRYPT, expiry, 5);
    vm.clock.set(Time::from_unix(expiry + 1));
    assert!(matches!(
        vm.submit_tx(&request_tx(&grantee, TEST_SCHEME, handle, short, 0, 1)).unwrap_err(),
        Error::PermitExpired
    ));
    vm.clock.set(now);

    // The permit was revoked.
    accept_one(&vm, &revoke_tx(&owner, permit_id, 6));
    assert_eq!(vm.permit(&permit_id).unwrap().status, STATUS_REVOKED);
    assert!(matches!(
        vm.submit_tx(&request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1)).unwrap_err(),
        Error::PermitRevoked
    ));
}

/// A capability over an encrypted value comes from its owner and from nobody else
/// — including the grantee, who cannot pass its access on, and cannot keep it
/// after the owner withdraws it.
#[test]
fn only_the_owner_grants_and_only_the_owner_revokes() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let stranger = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&owner, &grantee, &stranger]), &committee, 1);

    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);

    // A stranger cannot grant over someone else's ciphertext...
    assert!(matches!(
        vm.submit_tx(&grant_tx(&stranger, handle, stranger.addr, fhe::PERMIT_OP_DECRYPT, 0, 1))
            .unwrap_err(),
        Error::Unauthorized
    ));

    // ...nor can the grantee pass its own access along.
    assert!(matches!(
        vm.submit_tx(&grant_tx(&grantee, handle, stranger.addr, fhe::PERMIT_OP_DECRYPT, 0, 1))
            .unwrap_err(),
        Error::Unauthorized
    ));

    // Granting over a ciphertext nobody registered is refused.
    assert!(matches!(
        vm.submit_tx(&grant_tx(
            &owner,
            digest_of("ghost"),
            grantee.addr,
            fhe::PERMIT_OP_DECRYPT,
            0,
            3
        ))
        .unwrap_err(),
        Error::CiphertextNotFound
    ));

    // A stranger cannot withdraw someone else's grant.
    assert!(matches!(
        vm.submit_tx(&revoke_tx(&stranger, permit_id, 1)).unwrap_err(),
        Error::Unauthorized
    ));

    // Revoking something that was never granted is refused.
    assert!(matches!(
        vm.submit_tx(&revoke_tx(&owner, digest_of("no-permit"), 3)).unwrap_err(),
        Error::PermitNotFound
    ));

    // The owner can, and once withdrawn it stays withdrawn.
    accept_one(&vm, &revoke_tx(&owner, permit_id, 3));
    assert!(matches!(
        vm.submit_tx(&revoke_tx(&owner, permit_id, 4)).unwrap_err(),
        Error::PermitRevoked
    ));
}

/// A handle names its content: the same body under the same scheme is the same
/// handle, so it cannot be registered twice, and a handle cannot be claimed by
/// someone who does not have the body to hash.
#[test]
fn a_handle_names_its_content_so_it_cannot_be_registered_twice_or_squatted() {
    let a = TestKey::new();
    let b = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&a, &b]), &committee, 1);

    let body = digest_of("the-encrypted-body");
    accept_one(&vm, &register_tx(&a, TEST_SCHEME, body, 1));

    // A different account cannot re-register the same content.
    assert!(matches!(
        vm.submit_tx(&register_tx(&b, TEST_SCHEME, body, 1)).unwrap_err(),
        Error::CiphertextExists
    ));

    // The same content under a DIFFERENT scheme is a different value, and is
    // registrable — a CKKS ciphertext and a BFV one are not the same object.
    accept_one(&vm, &register_tx(&b, "bfv-n13", body, 1));
    assert_eq!(vm.ciphertexts().len(), 2);

    assert_eq!(
        vm.ciphertext(&derive_handle(&body, TEST_SCHEME.as_bytes())).unwrap().scheme,
        TEST_SCHEME
    );
    assert_eq!(vm.ciphertext(&derive_handle(&body, b"bfv-n13")).unwrap().scheme, "bfv-n13");
}

/// The committee installs its own successor by threshold vote, and the change
/// takes effect only when the threshold is reached.
#[test]
fn the_committee_installs_its_successor_only_at_the_threshold() {
    let (vm, _, members) = new_decrypt_vm(3, 2, &[]);
    let (next, next_keys) = new_committee(3);
    let next_pk = b"epoch-1-network-key";

    // Fund the incoming committee so it can answer once seated.
    {
        let st = vm.state.read().unwrap();
        for k in &next_keys {
            lux_fhevm::fee::credit(st.db.as_ref(), &k.addr, TEST_FUND).expect("credited");
        }
        st.versdb.commit().expect("committed");
    }

    // One vote is not enough.
    accept_one(&vm, &advance_tx(&members[0], 1, &next, 2, next_pk, 1));
    assert_eq!(vm.current_epoch(), 0, "one member is not a threshold");
    let cur = vm.epoch(0).unwrap();
    assert_eq!(cur.attestations().len(), 1);
    assert_eq!(cur.info.status, fhe::EpochStatus::Active);

    // The second vote installs it, atomically with the block.
    vm.submit_tx(&advance_tx(&members[1], 1, &next, 2, next_pk, 1)).expect("admitted");
    let blk = accept_queued(&vm);
    assert_eq!(vm.current_epoch(), 1);

    let old = vm.epoch(0).unwrap();
    assert_eq!(old.info.status, fhe::EpochStatus::Ended);
    assert_eq!(old.info.end_time, blk.timestamp.unix());

    let installed = vm.epoch(1).expect("the successor is seated");
    assert_eq!(installed.info.status, fhe::EpochStatus::Active);
    assert_eq!(installed.info.threshold, 2);
    assert_eq!(installed.info.public_key, next_pk);
    assert_eq!(installed.committee().len(), 3);
    assert!(installed.attestations().is_empty(), "a fresh epoch starts with no votes cast");

    // The outgoing committee no longer decides anything.
    let (further, _) = new_committee(3);
    assert!(matches!(
        vm.submit_tx(&advance_tx(&members[2], 2, &further, 2, b"k", 1)).unwrap_err(),
        Error::NotCommittee
    ));

    // The incoming one does.
    let st = vm.state.read().unwrap();
    advance_tx(&next_keys[0], 2, &further, 2, b"k", 1)
        .check_auth(&st, vm.clock.time().unix())
        .expect("the seated committee decides");
}

/// Who may rotate the committee, and to what.
#[test]
fn an_epoch_advance_refuses_a_stranger_a_skip_and_a_second_vote() {
    let stranger = TestKey::new();
    let (committee, members) = new_committee(3);
    let mut all: Vec<&TestKey> = members.iter().collect();
    all.push(&stranger);
    let vm = new_test_vm(fund_all(&all), &committee, 2);
    let (next, _) = new_committee(3);
    let pk = b"k";

    // A stranger cannot propose a successor.
    assert!(matches!(
        vm.submit_tx(&advance_tx(&stranger, 1, &next, 2, pk, 1)).unwrap_err(),
        Error::NotCommittee
    ));

    // Nor can a member skip an epoch or re-elect the current one.
    assert!(matches!(
        vm.submit_tx(&advance_tx(&members[0], 2, &next, 2, pk, 1)).unwrap_err(),
        Error::EpochMismatch(_)
    ));
    assert!(matches!(
        vm.submit_tx(&advance_tx(&members[0], 0, &next, 2, pk, 1)).unwrap_err(),
        Error::EpochMismatch(_)
    ));

    // One member, one vote — a member that moves to a rival proposal moves its
    // single vote rather than splitting the tally in its own favour.
    accept_one(&vm, &advance_tx(&members[0], 1, &next, 2, pk, 1));
    let (rival, _) = new_committee(3);
    accept_one(&vm, &advance_tx(&members[0], 1, &rival, 2, pk, 2));

    let ep = vm.epoch(0).unwrap();
    assert_eq!(ep.attestations().len(), 1, "one entry per member, however often it votes");
    assert_eq!(
        tally(ep.attestations(), &lux_fhevm::state::committee_digest(1, 2, pk, &next)),
        0,
        "the abandoned proposal keeps none of its support"
    );
    assert_eq!(
        tally(ep.attestations(), &lux_fhevm::state::committee_digest(1, 2, pk, &rival)),
        1
    );
    assert_eq!(vm.current_epoch(), 0, "and one member is still not a threshold");
}

/// An answer comes from the committee that was seated when the request was made. A
/// later committee holds different key shares and never saw the permit, so it must
/// not be able to answer.
#[test]
fn a_request_binds_to_the_epoch_that_was_seated_when_it_was_made() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);
    let (next, next_keys) = new_committee(3);
    {
        let st = vm.state.read().unwrap();
        for k in &next_keys {
            lux_fhevm::fee::credit(st.db.as_ref(), &k.addr, TEST_FUND).expect("credited");
        }
        st.versdb.commit().expect("committed");
    }

    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);

    // Rotate the committee out from under the pending request.
    accept_one(&vm, &advance_tx(&members[0], 1, &next, 2, b"k", 1));
    accept_one(&vm, &advance_tx(&members[1], 1, &next, 2, b"k", 1));
    assert_eq!(vm.current_epoch(), 1);

    // The NEW committee cannot answer the OLD request.
    assert!(matches!(
        vm.submit_tx(&fulfill_tx(&next_keys[0], request_id, digest_of("r"), 1)).unwrap_err(),
        Error::NotCommittee
    ));

    // The committee that was seated when it was asked still can.
    let result = digest_of("r");
    accept_one(&vm, &fulfill_tx(&members[0], request_id, result, 2));
    accept_one(&vm, &fulfill_tx(&members[1], request_id, result, 2));
    assert_eq!(vm.decrypt(&request_id).unwrap().request.status, fhe::RequestStatus::Completed);

    // A request made now belongs to the new epoch.
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 2));
    let fresh = vm.decrypt(&request_id_for(handle, &grantee.addr, 2)).unwrap();
    assert_eq!(fresh.request.epoch, 1);
}

/// A chain whose genesis seated nobody refuses every threshold decision rather
/// than accepting one from anybody.
#[test]
fn a_chain_with_nobody_seated_answers_nothing() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let vm = new_test_vm(fund_all(&[&owner, &grantee]), &[], 0);

    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);

    // There is no epoch record to name a committee, so nothing can answer.
    assert!(matches!(
        vm.submit_tx(&fulfill_tx(&owner, request_id, digest_of("r"), 3)).unwrap_err(),
        Error::EpochNotFound
    ));

    // And nobody can seat themselves.
    let (next, _) = new_committee(3);
    assert!(matches!(
        vm.submit_tx(&advance_tx(&owner, 1, &next, 2, b"k", 3)).unwrap_err(),
        Error::NotCommittee
    ));
}

#[test]
fn a_stopping_chain_proposes_nothing_and_an_idle_one_proposes_nothing() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
    assert!(matches!(vm.build_block().unwrap_err(), Error::NoPendingTxs));

    vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("x"), 1)).expect("admitted");
    vm.shutdown().expect("the chain stops");
    assert!(matches!(vm.build_block().unwrap_err(), Error::VmShutdown));
}

/// The height index is validated when it is read, so a damaged entry surfaces as
/// an error instead of a truncated id.
#[test]
fn a_corrupt_height_index_entry_is_an_error_not_a_truncated_id() {
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(Vec::new(), &committee, 1);

    {
        // A read lock is enough: the store is behind its own lock, so writing a row
        // does not need exclusive access to the chain's caches.
        let st = vm.state.read().unwrap();
        st.db.put(&height_key(5), &[1, 2, 3]).expect("written");
        st.versdb.commit().expect("committed");
    }

    assert!(matches!(vm.block_id_at(5).unwrap_err(), Error::InvalidPayload(_)));
}

/// The caches are a projection of the database and not the truth: rebuilding them
/// from disk yields the same chain.
#[test]
fn the_state_survives_a_reload_because_the_store_is_the_truth() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);

    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);
    accept_one(&vm, &fulfill_tx(&members[0], request_id, digest_of("r"), 1));

    let before = vm.decrypt(&request_id).unwrap();
    let (height, epoch) = (vm.height(), vm.current_epoch());

    vm.load_state().expect("the caches rebuild");

    assert_eq!(vm.height(), height);
    assert_eq!(vm.current_epoch(), epoch);
    let after = vm.decrypt(&request_id).expect("the request survives");
    assert_eq!(after.attestations(), before.attestations());
    assert_eq!(after.request.status, before.request.status);
    assert!(vm.ciphertext(&handle).is_some());
    assert_eq!(vm.permit(&permit_id).unwrap().status, STATUS_ACTIVE);
    assert_eq!(vm.epoch(0).unwrap().committee().len(), 3);
}
