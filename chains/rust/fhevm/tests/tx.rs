// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The three gates a transaction passes: structure, the payer's signature, and
//! authorization — and what each of them refuses.

mod common;

use common::*;

use lux_fhevm::error::Error;
use lux_fhevm::fhe;
use lux_fhevm::state::{
    self, committee_digest, derive_handle, derive_permit_id, derive_request_id,
};
use lux_fhevm::transaction::{
    validate_committee, AdvancePayload, FulfillPayload, GrantPayload, RegisterPayload, RequestPayload, RevokePayload,
    Transaction, MAX_CIPHERTEXT_SIZE, TX_ADVANCE_EPOCH, TX_FULFILL_DECRYPT, TX_GRANT_PERMIT,
    TX_REGISTER_CIPHERTEXT, TX_REQUEST_DECRYPT, TX_REVOKE_PERMIT,
};

#[test]
fn a_well_formed_transaction_is_the_baseline_every_refusal_is_measured_against() {
    let k = TestKey::new();
    register_tx(&k, TEST_SCHEME, digest_of("ok"), 1)
        .syntactic_verify()
        .expect("a well-formed registration");
}

/// Each structural rule actually refuses. Every case is a well-formed transaction
/// with exactly one thing wrong, so a rule that stopped being enforced fails here
/// instead of shipping.
#[test]
fn each_structural_rule_refuses_what_it_names() {
    let k = TestKey::new();
    let digest = digest_of("subject");
    let handle = derive_handle(&digest, TEST_SCHEME.as_bytes());
    let scheme = TEST_SCHEME.as_bytes().to_vec();

    // One case: a name, the transaction with exactly one thing wrong, and the
    // refusal it must draw.
    type Case = (&'static str, Transaction, fn(&Error) -> bool);
    let cases: Vec<Case> = vec![
        (
            "unknown transaction type",
            Transaction { tx_type: 99, nonce: 1, ..Default::default() },
            |e| matches!(e, Error::InvalidTxType),
        ),
        (
            "unknown scheme",
            Transaction {
                tx_type: TX_REGISTER_CIPHERTEXT,
                scheme: b"paillier".to_vec(),
                nonce: 1,
                payload: RegisterPayload { digest, size: 1, ..Default::default() }.to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::UnknownScheme(_)),
        ),
        (
            "nonce zero",
            Transaction {
                tx_type: TX_REGISTER_CIPHERTEXT,
                scheme: scheme.clone(),
                nonce: 0,
                subject: handle,
                payload: RegisterPayload { digest, size: 1, ..Default::default() }.to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::BadNonce),
        ),
        (
            "undecodable payload",
            Transaction {
                tx_type: TX_REGISTER_CIPHERTEXT,
                scheme: scheme.clone(),
                nonce: 1,
                subject: handle,
                payload: b"not json".to_vec(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "register with no digest",
            Transaction {
                tx_type: TX_REGISTER_CIPHERTEXT,
                scheme: scheme.clone(),
                nonce: 1,
                subject: derive_handle(&[0u8; 32], TEST_SCHEME.as_bytes()),
                payload: RegisterPayload { size: 1, ..Default::default() }.to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "register with zero size",
            Transaction {
                tx_type: TX_REGISTER_CIPHERTEXT,
                scheme: scheme.clone(),
                nonce: 1,
                subject: handle,
                payload: RegisterPayload { digest, ..Default::default() }.to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "register with a size beyond anything servable",
            Transaction {
                tx_type: TX_REGISTER_CIPHERTEXT,
                scheme: scheme.clone(),
                nonce: 1,
                subject: handle,
                payload: RegisterPayload {
                    digest,
                    kind: 4,
                    level: 3,
                    size: MAX_CIPHERTEXT_SIZE + 1,
                }
                .to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "register with a negative level",
            Transaction {
                tx_type: TX_REGISTER_CIPHERTEXT,
                scheme: scheme.clone(),
                nonce: 1,
                subject: handle,
                payload: RegisterPayload { digest, size: 1, level: -1, ..Default::default() }
                    .to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "register whose subject is not the derived handle",
            Transaction {
                tx_type: TX_REGISTER_CIPHERTEXT,
                scheme: scheme.clone(),
                nonce: 1,
                subject: {
                    let mut s = [0u8; 32];
                    s[0] = 0xde;
                    s[1] = 0xad;
                    s
                },
                payload: RegisterPayload { digest, size: 1, ..Default::default() }.to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::HandleMismatch(_)),
        ),
        (
            "register under a different scheme than the handle names",
            Transaction {
                tx_type: TX_REGISTER_CIPHERTEXT,
                scheme: b"bfv-n13".to_vec(),
                nonce: 1,
                subject: handle,
                payload: RegisterPayload { digest, size: 1, ..Default::default() }.to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::HandleMismatch(_)),
        ),
        (
            "grant conferring nothing",
            Transaction {
                tx_type: TX_GRANT_PERMIT,
                nonce: 1,
                subject: handle,
                payload: GrantPayload { grantee: k.addr, operations: 0, expiry: 0 }.to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "grant with unknown operation bits",
            Transaction {
                tx_type: TX_GRANT_PERMIT,
                nonce: 1,
                subject: handle,
                payload: GrantPayload { grantee: k.addr, operations: 1 << 20, expiry: 0 }.to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "grant with a negative expiry",
            Transaction {
                tx_type: TX_GRANT_PERMIT,
                nonce: 1,
                subject: handle,
                payload: GrantPayload {
                    grantee: k.addr,
                    operations: fhe::PERMIT_OP_DECRYPT,
                    expiry: -1,
                }
                .to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "request naming no permit",
            Transaction {
                tx_type: TX_REQUEST_DECRYPT,
                scheme: scheme.clone(),
                nonce: 1,
                subject: handle,
                payload: RequestPayload::default().to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "request with a negative expiry",
            Transaction {
                tx_type: TX_REQUEST_DECRYPT,
                scheme: scheme.clone(),
                nonce: 1,
                subject: handle,
                payload: RequestPayload {
                    permit_id: [1u8; 32],
                    expiry: -1,
                    ..Default::default()
                }
                .to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "fulfil carrying no result",
            Transaction {
                tx_type: TX_FULFILL_DECRYPT,
                nonce: 1,
                subject: handle,
                payload: FulfillPayload::default().to_json(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "revoke whose payload is not an object",
            Transaction {
                tx_type: TX_REVOKE_PERMIT,
                nonce: 1,
                payload: b"[]".to_vec(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
        (
            "advance whose payload does not decode",
            Transaction {
                tx_type: TX_ADVANCE_EPOCH,
                nonce: 1,
                payload: b"{".to_vec(),
                ..Default::default()
            },
            |e| matches!(e, Error::InvalidPayload(_)),
        ),
    ];

    for (name, tx, want) in cases {
        let got = tx.syntactic_verify().expect_err(name);
        assert!(want(&got), "{name}: refused with {got:?}");
    }

    // The control: the well-formed shapes all pass.
    Transaction {
        tx_type: TX_REGISTER_CIPHERTEXT,
        scheme: scheme.clone(),
        nonce: 1,
        subject: handle,
        payload: RegisterPayload { digest, kind: 4, level: 3, size: 4096 }.to_json(),
        ..Default::default()
    }
    .syntactic_verify()
    .expect("a sound registration");
    Transaction {
        tx_type: TX_REVOKE_PERMIT,
        nonce: 1,
        payload: RevokePayload::default().to_json(),
        ..Default::default()
    }
    .syntactic_verify()
    .expect("a sound revocation");
}

/// An epoch proposal carries the whole committee check with it, so a committee
/// that could never sign cannot be voted in.
#[test]
fn an_epoch_proposal_carries_the_whole_committee_check() {
    let (good, _) = new_committee(3);
    let pk = b"next-network-key".to_vec();

    let advance = |c: &[fhe::CommitteeMember], threshold: i64, key: &[u8]| Transaction {
        tx_type: TX_ADVANCE_EPOCH,
        nonce: 1,
        subject: committee_digest(1, threshold, key, c),
        payload: AdvancePayload {
            epoch: 1,
            committee: if c.is_empty() { None } else { Some(c.to_vec()) },
            threshold,
            public_key: key.to_vec(),
        }
        .to_json(),
        ..Default::default()
    };

    advance(&good, 2, &pk).syntactic_verify().expect("a sound proposal");

    assert!(matches!(
        advance(&[], 1, &pk).syntactic_verify().unwrap_err(),
        Error::InvalidCommittee(_)
    ));
    assert!(matches!(
        advance(&good, 0, &pk).syntactic_verify().unwrap_err(),
        Error::InvalidThreshold(_)
    ));
    assert!(matches!(
        advance(&good, 4, &pk).syntactic_verify().unwrap_err(),
        Error::InvalidThreshold(_)
    ));
    assert!(matches!(
        advance(&good, 2, b"").syntactic_verify().unwrap_err(),
        Error::InvalidCommittee(_)
    ));

    // Out of canonical order: two members proposing the same set would hash it
    // differently and never agree, so the order is part of the value.
    let mut shuffled = good.clone();
    shuffled.swap(0, 2);
    assert!(matches!(
        advance(&shuffled, 2, &pk).syntactic_verify().unwrap_err(),
        Error::InvalidCommittee(_)
    ));

    // A member whose key cannot be parsed could never attest — installing it would
    // wedge the chain, because a committee that cannot speak also cannot be
    // replaced.
    let mut unusable = good.clone();
    unusable[1].public_key = b"not-an-mldsa-65-key".to_vec();
    assert!(matches!(
        advance(&unusable, 2, &pk).syntactic_verify().unwrap_err(),
        Error::InvalidCommittee(_)
    ));

    // A proposal whose subject does not hash its own contents is refused, so the
    // signature always covers the committee actually being voted for.
    let mut mismatched = advance(&good, 2, &pk);
    mismatched.subject = [0xff; 32];
    assert!(matches!(mismatched.syntactic_verify().unwrap_err(), Error::HandleMismatch(_)));
}

/// A committee's members must arrive in canonical order and without repeats.
/// Members hash the SET to vote on it, so two orderings of the same members would
/// be two different proposals and the committee could never agree with itself.
#[test]
fn a_committee_is_ordered_and_free_of_repeats() {
    let (c, _) = new_committee(3);

    let shuffled = vec![c[2].clone(), c[0].clone(), c[1].clone()];
    assert!(matches!(
        validate_committee(&shuffled, 2, b"pk").unwrap_err(),
        Error::InvalidCommittee(_)
    ));

    let repeated = vec![c[0].clone(), c[0].clone(), c[1].clone()];
    assert!(
        matches!(
            validate_committee(&repeated, 2, b"pk").unwrap_err(),
            Error::InvalidCommittee(_)
        ),
        "one seat twice is not two seats"
    );

    validate_committee(&c, 2, b"pk").expect("the control");
}

/// The payer authentication path: an unsigned, a tampered and an impersonating
/// transaction are each refused, and only a genuine signature by the claimed
/// identity passes.
#[test]
fn only_a_genuine_signature_by_the_claimed_identity_authenticates() {
    let k = TestKey::new();
    let other = TestKey::new();
    let chain = test_chain_id();

    register_tx(&k, TEST_SCHEME, digest_of("auth"), 1)
        .authenticate(&chain)
        .expect("a genuine signature");

    let mut unsigned = register_tx(&k, TEST_SCHEME, digest_of("auth"), 1);
    unsigned.auth.clear();
    unsigned.sig.clear();
    assert!(matches!(unsigned.authenticate(&chain).unwrap_err(), Error::UnsignedTx));

    let mut tampered = register_tx(&k, TEST_SCHEME, digest_of("auth"), 1);
    tampered.nonce = 7;
    assert!(matches!(tampered.authenticate(&chain).unwrap_err(), Error::BadSignature));

    // Signed by `other` but claiming k's address: the address is derived from the
    // attached public key, so the two cannot be made to agree.
    let mut impersonating = register_tx(&k, TEST_SCHEME, digest_of("auth"), 1);
    other.sign(&mut impersonating);
    assert!(matches!(impersonating.authenticate(&chain).unwrap_err(), Error::PayerMismatch));

    // Auth bytes that are not a public key at all.
    let mut garbage = register_tx(&k, TEST_SCHEME, digest_of("auth"), 1);
    garbage.auth = b"garbage".to_vec();
    garbage.payer = state::address_of(&garbage.auth);
    assert!(garbage.authenticate(&chain).is_err());
}

/// A payer's address and a committee member's address come from the same one-way
/// derivation, so membership and payer identity cannot disagree.
#[test]
fn a_member_is_recognised_by_the_address_that_authenticates_it() {
    let k = TestKey::new();
    assert_eq!(k.addr, state::address_of(&k.public));

    let (members, keys) = new_committee(3);
    let rec = lux_fhevm::EpochRecord {
        info: fhe::EpochInfo { committee: Some(members), ..Default::default() },
        attestations: None,
    };
    for (i, key) in keys.iter().enumerate() {
        assert!(rec.member_of(&key.addr), "member {i} must be recognised by its payer address");
    }
    assert!(!rec.member_of(&TestKey::new().addr), "a stranger is not a member");
}

/// Every id F derives comes from signed transaction fields alone, so two
/// validators applying one block agree — and changing any input changes the id.
#[test]
fn every_identifier_is_deterministic_in_the_fields_that_produce_it() {
    let d = digest_of("determinism");
    let a = TestKey::new().addr;
    let b = TestKey::new().addr;
    let scheme = TEST_SCHEME.as_bytes();

    assert_eq!(derive_handle(&d, scheme), derive_handle(&d, scheme));
    assert_ne!(derive_handle(&d, scheme), derive_handle(&d, b"bfv-n13"));
    assert_ne!(derive_handle(&d, scheme), derive_handle(&digest_of("other"), scheme));

    let h = derive_handle(&d, scheme);
    assert_eq!(derive_permit_id(&h, &a, &b, 1, 0, 1), derive_permit_id(&h, &a, &b, 1, 0, 1));
    assert_ne!(
        derive_permit_id(&h, &a, &b, 1, 0, 1),
        derive_permit_id(&h, &a, &b, 1, 0, 2),
        "a grantor may re-grant the same capability without colliding"
    );
    assert_ne!(derive_permit_id(&h, &a, &b, 1, 0, 1), derive_permit_id(&h, &b, &a, 1, 0, 1));

    assert_eq!(derive_request_id(&h, &a, 3), derive_request_id(&h, &a, 3));
    assert_ne!(derive_request_id(&h, &a, 3), derive_request_id(&h, &a, 4));
    assert_ne!(derive_request_id(&h, &a, 3), derive_request_id(&h, &b, 3));
}

/// The value members attest describes the PROPOSAL, not the bytes one client
/// happened to encode: it changes with every meaningful field and with the members
/// themselves.
#[test]
fn a_committee_digest_is_the_proposal_and_not_one_clients_encoding_of_it() {
    let (c, _) = new_committee(3);
    let pk = b"network-key";
    let base = committee_digest(1, 2, pk, &c);

    assert_eq!(base, committee_digest(1, 2, pk, &c));
    assert_ne!(base, committee_digest(2, 2, pk, &c), "epoch must bind");
    assert_ne!(base, committee_digest(1, 3, pk, &c), "threshold must bind");
    assert_ne!(base, committee_digest(1, 2, b"other", &c), "network key must bind");
    assert_ne!(base, committee_digest(1, 2, pk, &c[..2]), "membership must bind");

    // Length-prefixing means a member's fields cannot be re-split across the
    // boundary to forge a colliding digest.
    let mut shifted = c.clone();
    let mut joined = c[0].public_key.clone();
    joined.extend_from_slice(&c[1].public_key);
    shifted[0].public_key = joined;
    assert_ne!(base, committee_digest(1, 2, pk, &shifted));
}

/// The in-flight uniqueness key names the right thing: two votes by one member on
/// one decision collide, votes by different members do not, and two grants by one
/// grantor do not.
#[test]
fn the_effect_key_names_the_entry_and_where_it_matters_who_writes_it() {
    let k = TestKey::new();
    let other = TestKey::new();
    let req = [7u8; 32];

    // One member cannot have two attestations to one request in flight, even
    // naming different results — the effect is the vote, not the value.
    let v1 = fulfill_tx(&k, req, [1u8; 32], 1);
    let v2 = fulfill_tx(&k, req, [2u8; 32], 2);
    assert_eq!(v1.effect(), v2.effect());

    // A different member voting on the same request is a different effect.
    let v3 = fulfill_tx(&other, req, [1u8; 32], 1);
    assert_ne!(v1.effect(), v3.effect());

    // Registering two different ciphertexts are different effects; registering the
    // same one twice is the same effect.
    let r1 = register_tx(&k, TEST_SCHEME, digest_of("one"), 1);
    let r2 = register_tx(&k, TEST_SCHEME, digest_of("two"), 2);
    let r1_again = register_tx(&other, TEST_SCHEME, digest_of("one"), 1);
    assert_ne!(r1.effect(), r2.effect());
    assert_eq!(r1.effect(), r1_again.effect(), "the handle is the effect, whoever claims it");

    // Two grants over the same handle by the same grantor are distinct — the nonce
    // distinguishes the permits they create.
    let handle = derive_handle(&digest_of("g"), TEST_SCHEME.as_bytes());
    let g1 = grant_tx(&k, handle, other.addr, fhe::PERMIT_OP_DECRYPT, 0, 1);
    let g2 = grant_tx(&k, handle, other.addr, fhe::PERMIT_OP_DECRYPT, 0, 2);
    assert_ne!(g1.effect(), g2.effect());

    // An epoch advance is a vote on the DECISION, and exactly one epoch is ever
    // open, so one member's two votes are one effect however they differ. The
    // proposal must not enter the effect: if it did, a member could vote twice,
    // both votes would pass verification against committed state, and acceptance
    // would apply one and refuse the other — a block every validator certifies and
    // no validator can apply.
    let (c_a, _) = new_committee(3);
    let (c_b, _) = new_committee(3);
    let a1 = advance_tx(&k, 1, &c_a, 2, b"k", 1);
    let a2 = advance_tx(&k, 1, &c_b, 2, b"k", 2);
    assert_ne!(a1.subject, a2.subject, "two genuinely different proposals");
    assert_eq!(a1.effect(), a2.effect(), "but one member, one vote");

    let a3 = advance_tx(&other, 1, &c_a, 2, b"k", 1);
    assert_ne!(a1.effect(), a3.effect());

    // A vote and an attestation never collide across operation kinds.
    assert_ne!(a1.effect(), v1.effect());
}

#[test]
fn a_transaction_id_is_the_hash_of_its_content_and_cannot_be_chosen() {
    let k = TestKey::new();
    let tx = register_tx(&k, TEST_SCHEME, digest_of("id"), 1);
    let first = tx.id();
    assert_ne!(first, lux_fhevm::ids::EMPTY);
    assert_eq!(first, tx.id(), "an id is stable");
    assert_ne!(first, register_tx(&k, TEST_SCHEME, digest_of("id"), 2).id());
}

/// Every effect reports a failed write instead of returning success. These are the
/// errors that abort a block whole: a validator that cannot write cannot proceed,
/// and a transaction that silently did not apply would leave two validators
/// holding different state.
#[test]
fn every_effect_reports_a_write_it_cannot_make() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);
    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);
    let (next, _) = new_committee(3);
    let now = vm.clock.time().unix();

    version_layer(&vm).close().expect("the layer closes");
    let mut st = vm.state.write().unwrap();

    assert!(register_tx(&owner, TEST_SCHEME, digest_of("w"), 9)
        .apply_register(&mut st, now)
        .is_err());
    assert!(grant_tx(&owner, handle, grantee.addr, fhe::PERMIT_OP_DECRYPT, 0, 9)
        .apply_grant(&mut st, now)
        .is_err());
    assert!(revoke_tx(&owner, permit_id, 9).apply_revoke(&mut st).is_err());
    assert!(request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 9)
        .apply_request(&mut st, now)
        .is_err());
    assert!(fulfill_tx(&members[0], request_id, digest_of("r"), 9)
        .apply_fulfill(&mut st, now)
        .is_err());
    assert!(advance_tx(&members[0], 1, &next, 2, b"pk", 9)
        .apply_advance(&mut st, now)
        .is_err());
}

/// Each effect re-derives its arguments from the payload and fails closed rather
/// than applying a zero value. The structural gate has already accepted the
/// payload by the time a block carries it, so this is the check behind the check —
/// but a check that only ever runs behind another is one nobody can show still
/// works, so it is shown here directly.
#[test]
fn every_effect_refuses_a_payload_it_cannot_decode_and_a_record_that_is_not_there() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);
    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);
    let now = vm.clock.time().unix();

    let junk = |tx_type: u8, subject: [u8; 32]| Transaction {
        tx_type,
        scheme: TEST_SCHEME.as_bytes().to_vec(),
        payer: owner.addr,
        subject,
        nonce: 9,
        payload: b"{not json".to_vec(),
        ..Default::default()
    };

    {
        let mut st = vm.state.write().unwrap();
        assert!(junk(TX_REGISTER_CIPHERTEXT, handle).apply_register(&mut st, now).is_err());
        assert!(junk(TX_GRANT_PERMIT, handle).apply_grant(&mut st, now).is_err());
        assert!(junk(TX_REQUEST_DECRYPT, handle).apply_request(&mut st, now).is_err());
        assert!(junk(TX_FULFILL_DECRYPT, request_id).apply_fulfill(&mut st, now).is_err());
        assert!(junk(TX_ADVANCE_EPOCH, [0u8; 32]).apply_advance(&mut st, now).is_err());

        // And each effect that names a record refuses when the record is not there.
        let missing = [0xaa; 32];
        assert!(matches!(
            revoke_tx(&owner, missing, 9).apply_revoke(&mut st).unwrap_err(),
            Error::PermitNotFound
        ));
        assert!(matches!(
            fulfill_tx(&members[0], missing, digest_of("r"), 9)
                .apply_fulfill(&mut st, now)
                .unwrap_err(),
            Error::RequestNotFound
        ));

        // A fulfilment whose epoch has been forgotten is refused rather than
        // tallied against a threshold nobody can name.
        st.decrypts.get_mut(&request_id).expect("the request").request.epoch = 99;
        assert!(matches!(
            fulfill_tx(&members[0], request_id, digest_of("r"), 9)
                .apply_fulfill(&mut st, now)
                .unwrap_err(),
            Error::EpochNotFound
        ));
    }
}

/// Authorization fails closed on an operation it does not know, rather than
/// falling through to an effect. Application has the same default and it is
/// UNREACHABLE — authorization runs first and refuses every type the switch does
/// not name — but without it an unknown type would report the transaction APPLIED,
/// which is the one answer that must never be given by accident.
#[test]
fn an_unknown_operation_is_refused_and_reverts_rather_than_halting_the_block() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    let alien = Transaction { tx_type: 99, payer: k.addr, nonce: 1, ..Default::default() };
    let mut st = vm.state.write().unwrap();
    assert!(matches!(alien.check_auth(&st, 0).unwrap_err(), Error::InvalidTxType));
    assert!(!alien.apply(&mut st, 0).expect("an unknown operation reverts, it does not halt"));
}

/// The authorization decisions that turn on a record being there. Each is a
/// refusal rather than a default: authorization that fell through when it could
/// not find what it was asked about would grant on ignorance.
#[test]
fn authorization_refuses_what_it_cannot_resolve() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);
    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);
    let now = vm.clock.time().unix();

    let mut st = vm.state.write().unwrap();

    // A request whose payload does not decode cannot be authorized against a
    // permit it does not name.
    let malformed = Transaction {
        tx_type: TX_REQUEST_DECRYPT,
        scheme: TEST_SCHEME.as_bytes().to_vec(),
        payer: grantee.addr,
        subject: handle,
        nonce: 2,
        payload: b"{not json".to_vec(),
        ..Default::default()
    };
    assert!(matches!(malformed.check_auth(&st, now).unwrap_err(), Error::InvalidPayload(_)));

    // So does an epoch proposal.
    let bad_advance = Transaction {
        tx_type: TX_ADVANCE_EPOCH,
        payer: members[0].addr,
        nonce: 2,
        payload: b"{not json".to_vec(),
        ..Default::default()
    };
    assert!(matches!(bad_advance.check_auth(&st, now).unwrap_err(), Error::InvalidPayload(_)));

    // A fulfilment for an epoch the chain has forgotten names no committee, so
    // there is nobody it could be authorized as.
    st.decrypts.get_mut(&request_id).expect("the request").request.epoch = 99;
    assert!(matches!(
        fulfill_tx(&members[0], request_id, digest_of("r"), 1)
            .check_auth(&st, now)
            .unwrap_err(),
        Error::EpochNotFound
    ));
}

/// A committee cannot answer a request whose authorizing permit is no longer
/// there. The permit is what makes the ask legitimate, so a request that outlived
/// it authorizes nothing — otherwise a deleted grant would still deliver a
/// plaintext to a callback.
#[test]
fn a_fulfilment_needs_the_permit_that_asked_for_it() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);
    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);
    let now = vm.clock.time().unix();
    let fulfil = fulfill_tx(&members[0], request_id, digest_of("r"), 1);

    {
        let st = vm.state.read().unwrap();
        fulfil.check_auth(&st, now).expect("the control: with the permit there, it is authorized");
    }

    // Point the request at a permit nothing granted.
    {
        let mut st = vm.state.write().unwrap();
        st.decrypts.get_mut(&request_id).expect("the request").permit_id = [0xab; 32];
    }

    let st = vm.state.read().unwrap();
    assert!(matches!(fulfil.check_auth(&st, now).unwrap_err(), Error::PermitNotFound));
}
