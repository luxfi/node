// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What F does when its database does not answer, which is a consensus question
//! rather than an operational one.
//!
//! The failure that matters most is the quiet one: a read that FAILED reported as
//! a read that found NOTHING. The last-accepted pointer coming back empty made a
//! chain at height 2 look like a chain that had never run, and the node went on to
//! build height 1 over it — durably, over the height index, with startup returning
//! success and nothing in any log to say so. The same conflation on the epoch
//! pointer seats a committee nobody elected, and on a nonce it reopens the replay
//! window a nonce exists to close.
//!
//! So these tests fail the database deliberately and require F to stop. Each one
//! carries its own control — the SAME read, absent rather than failing — because a
//! test that only sees the failure cannot tell "refuses everything" from
//! "distinguishes the two".

mod common;

use std::sync::atomic::Ordering;
use std::sync::Arc;

use common::*;

use lux_fhevm::db::{Kv, Mem};
use lux_fhevm::error::Error;
use lux_fhevm::fee;
use lux_fhevm::fhe;
use lux_fhevm::ids;
use lux_fhevm::state::{derive_handle, CiphertextRecord, DecryptRecord, EpochRecord, PermitRecord};
use lux_fhevm::vm::{
    CIPHERTEXT_PREFIX, CURRENT_EPOCH_KEY, DECRYPT_PREFIX, EPOCH_PREFIX, GENESIS_MARKER,
    HEIGHT_PREFIX, LAST_ACCEPTED_KEY, NONCE_PREFIX, PERMIT_PREFIX,
};

/// A read that fails is not a read that found nothing.
#[test]
fn a_boot_read_that_fails_is_not_a_fresh_chain() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let genesis = test_genesis(fund_all(&[&k]), &committee, 1).to_json();

    // A chain that has actually run.
    let store = Mem::new();
    let vm = boot_on(store.clone(), &genesis).expect("the chain starts");
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("live-1"), 1));
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("live-2"), 2));
    let (live_tip, live_height) = (vm.last_accepted(), vm.height());
    assert_eq!(live_height, 2);

    // THE CONTROL: rebooting on the same store finds the chain where it left it.
    // Without this the checks below would pass just as well against a VM that
    // refused every boot.
    let back = boot_on(store.clone(), &genesis).expect("the reboot succeeds");
    assert_eq!(back.height(), live_height);
    assert_eq!(back.last_accepted(), live_tip);

    // Each pointer, unreadable in turn: the boot fails rather than inventing an
    // answer. A node that cannot read its own tip must not serve as one.
    for key in [LAST_ACCEPTED_KEY, CURRENT_EPOCH_KEY] {
        let db: Arc<dyn Kv> = Arc::new(Faults::over(store.clone()).reading(key));
        let err = boot_on(db, &genesis).err().expect("an unreadable pointer stops the boot");
        assert!(is_disk(&err), "{key:?}: {err}");
    }

    // A corrupt record index likewise stops the boot rather than presenting as an
    // empty one — for each of the four record kinds, so none can be the one that
    // silently loads nothing.
    for prefix in [CIPHERTEXT_PREFIX, PERMIT_PREFIX, DECRYPT_PREFIX, EPOCH_PREFIX] {
        let db: Arc<dyn Kv> = Arc::new(Faults::over(store.clone()).scanning(prefix));
        let err = boot_on(db, &genesis).err().expect("an unreadable index stops the boot");
        assert!(is_disk(&err), "{prefix:?}: {err}");
    }

    // And a nonce that cannot be read is an error, not a zero. A zero here says
    // "this payer has spent nothing", which lets every transaction it ever signed
    // through again.
    let nonced = boot_on(store.clone(), &genesis).expect("the chain starts");
    assert_eq!(
        nonced.state.read().unwrap().nonce_of(&k.addr).unwrap(),
        2,
        "the control: the nonce is readable and correct"
    );

    let sound = fail_with(&nonced, Faults::over(store.clone()).reading(NONCE_PREFIX));
    let err = nonced.state.read().unwrap().nonce_of(&k.addr).unwrap_err();
    assert!(is_disk(&err), "an unreadable nonce is an error, never a fresh account");

    // It reaches the paths that decide, too: admission refuses rather than
    // treating the payer as new.
    let err = nonced
        .submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("replayed"), 1))
        .unwrap_err();
    assert!(is_disk(&err), "{err}");
    restore(&nonced, sound);
}

/// A boot whose genesis marker cannot be read must stop before it decides whether
/// to seed: seeding twice writes genesis over a live tip.
#[test]
fn a_seeder_that_cannot_tell_whether_it_ran_stops() {
    let (committee, _) = new_committee(1);
    let genesis = test_genesis(Vec::new(), &committee, 1).to_json();
    let db: Arc<dyn Kv> = Arc::new(Faults::over(Mem::new()).reading(GENESIS_MARKER));
    let err = boot_on(db, &genesis).err().expect("the boot stops");
    assert!(is_disk(&err), "{err}");
}

/// The whole block is rolled back when the one commit fails: no operation applied,
/// no fee burned, no nonce consumed, and the caches reloaded from the store that
/// never changed.
#[test]
fn a_failed_commit_rolls_the_whole_block_back_and_the_chain_can_continue() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let genesis = test_genesis(fund_all(&[&k]), &committee, 1).to_json();

    let broken = Arc::new(Faults::over(Mem::new()));
    let vm = boot_on(broken.clone(), &genesis).expect("the chain starts");

    let tx = register_tx(&k, TEST_SCHEME, digest_of("uncommittable"), 1);
    vm.submit_tx(&tx).expect("admitted");
    let blk = vm.build_block().expect("a block");
    blk.verify(&vm).expect("it verifies");

    broken.write_fails.store(true, Ordering::SeqCst);
    let err = blk.accept(&vm).unwrap_err();
    assert!(is_disk(&err), "{err}");

    assert!(vm.ciphertext(&tx.subject).is_none(), "an uncommitted block applied nothing");
    assert_eq!(vm.burned().unwrap(), 0, "and burned nothing");
    assert_eq!(vm.balance(&k.addr).unwrap(), TEST_FUND);
    assert_eq!(vm.height(), 0);
    assert_eq!(vm.state.read().unwrap().nonce_of(&k.addr).unwrap(), 0, "nor consumed the nonce");

    // The failure was transient: with the disk answering again the same block
    // applies, so a rollback leaves the chain able to continue rather than stuck.
    broken.write_fails.store(false, Ordering::SeqCst);
    blk.accept(&vm).expect("the same block applies");
    assert!(vm.ciphertext(&tx.subject).is_some());
}

/// Settlement reports a read failure rather than proceeding on the answer it would
/// have invented — here the payer's committed nonce, whose absence and whose
/// failure mean opposite things.
#[test]
fn settlement_stops_when_the_state_cannot_be_read() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    let tx = register_tx(&k, TEST_SCHEME, digest_of("unreadable"), 1);
    vm.submit_tx(&tx).expect("admitted");
    let blk = vm.build_block().expect("a block");

    let sound = fail_with(&vm, Faults::over(version_layer(&vm)).reading(NONCE_PREFIX));
    let err = blk.accept(&vm).unwrap_err();
    assert!(is_disk(&err), "{err}");
    restore(&vm, sound);

    assert!(vm.ciphertext(&tx.subject).is_none(), "a block that could not be settled applied nothing");
}

/// Every record writer reports a failed write instead of returning success and
/// leaving the cache holding a record the store does not have. A closed database is
/// the real shape of this: a block applying while the VM shuts down.
#[test]
fn every_record_writer_reports_a_write_it_cannot_make() {
    let owner = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&owner]), &committee, 1);
    version_layer(&vm).close().expect("the layer closes");

    let mut st = vm.state.write().unwrap();
    assert!(st.put_ciphertext(CiphertextRecord::default()).is_err(), "ciphertext");
    assert!(st.put_permit(PermitRecord::default()).is_err(), "permit");
    assert!(st.put_decrypt(DecryptRecord::default()).is_err(), "decrypt");
    assert!(st.put_epoch(EpochRecord::default()).is_err(), "epoch");
    assert!(st.set_current_epoch(7).is_err(), "the epoch pointer");
    assert!(st.set_nonce(&owner.addr, 1).is_err(), "a nonce");
}

/// The funding table is held to the same address format everything else is, rather
/// than seeding a chain that is short by however much the bad row was worth.
#[test]
fn genesis_refuses_an_allocation_it_cannot_apply() {
    let (committee, _) = new_committee(1);
    for addr in ["zzzz", "0011223344", "00112233445566778899aabbccddeeff0011223344556677"] {
        let genesis = test_genesis(vec![(addr.to_string(), 1)], &committee, 1).to_json();
        assert!(boot_on(Mem::new(), &genesis).is_err(), "address {addr:?}");
    }
}

/// The funding table is applied or refused whole. Two spellings of one address are
/// one account, so a table can ask for more than an account can hold.
#[test]
fn genesis_refuses_an_allocation_it_cannot_credit() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let genesis = test_genesis(
        vec![(k.hex_addr(), u64::MAX), (format!("0x{}", k.hex_addr()), 1)],
        &committee,
        1,
    )
    .to_json();
    assert!(
        boot_on(Mem::new(), &genesis).is_err(),
        "a balance that cannot be credited is not a balance that was"
    );
}

/// One unreadable row does not stop a node booting — the rest of the chain's
/// records still load. This is the other side of the boot rules: an index that
/// ERRORS is fatal, a single record that does not decode is skipped, because the
/// first says nothing can be trusted and the second says one row cannot.
#[test]
fn one_record_that_does_not_decode_is_skipped_and_the_rest_still_load() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("good"), 1));
    let good = derive_handle(&digest_of("good"), TEST_SCHEME.as_bytes());

    let mut junk = [0u8; 32];
    junk[0] = 0xff;
    {
        let st = vm.state.read().unwrap();
        let mut key = CIPHERTEXT_PREFIX.to_vec();
        key.extend_from_slice(&junk);
        st.db.put(&key, b"{not json").expect("written");
    }
    vm.load_state().expect("the boot still succeeds");

    assert!(vm.ciphertext(&good).is_some(), "a readable record still loads");
    assert!(vm.ciphertext(&junk).is_none(), "an undecodable one is skipped, not guessed at");
}

/// A last-accepted pointer to a block the store does not hold stops the boot.
/// Continuing would put the chain at genesis while its pointer says otherwise.
#[test]
fn a_tip_that_names_no_block_stops_the_boot() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let genesis = test_genesis(fund_all(&[&k]), &committee, 1).to_json();

    let store = Mem::new();
    let vm = boot_on(store.clone(), &genesis).expect("the chain starts");
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("t"), 1));

    // Point the tip at a block nobody wrote.
    let mut missing = ids::EMPTY;
    missing[0] = 0xde;
    missing[1] = 0xad;
    store.put(LAST_ACCEPTED_KEY, &missing).expect("written");
    assert!(boot_on(store.clone(), &genesis).is_err());

    // A pointer of the wrong width is refused for the same reason.
    store.put(LAST_ACCEPTED_KEY, &[1, 2, 3]).expect("written");
    let err = boot_on(store.clone(), &genesis).err().expect("refused");
    assert!(matches!(err, Error::InvalidPayload(_)), "{err}");
}

/// Each write acceptance makes, failed one at a time. A block is settled and
/// applied through a layer committed ONCE, so any of these failing must leave the
/// chain where it was — not half-applied, and not applied-but-unindexed.
#[test]
fn acceptance_is_all_or_nothing_at_every_write_it_makes() {
    for (name, prefix) in [
        ("the block itself", lux_fhevm::vm::BLOCK_PREFIX),
        ("the height index", HEIGHT_PREFIX),
        ("the last-accepted pointer", LAST_ACCEPTED_KEY),
        ("the payer's nonce", NONCE_PREFIX),
        ("the record it applies", CIPHERTEXT_PREFIX),
    ] {
        let k = TestKey::new();
        let (committee, _) = new_committee(1);
        let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

        let tx = register_tx(&k, TEST_SCHEME, digest_of("atomic"), 1);
        vm.submit_tx(&tx).expect("admitted");
        let blk = vm.build_block().expect("a block");
        blk.verify(&vm).expect("it verifies");

        let sound = fail_with(&vm, Faults::over(version_layer(&vm)).writing(prefix));
        let err = blk.accept(&vm).unwrap_err();
        assert!(is_disk(&err), "{name}: {err}");
        restore(&vm, sound);

        assert_eq!(vm.height(), 0, "{name}: the chain did not move");
        assert!(vm.ciphertext(&tx.subject).is_none(), "{name}: and applied nothing");
        assert_eq!(vm.burned().unwrap(), 0, "{name}: and burned nothing");

        // The control: with the disk sound the same block applies.
        blk.accept(&vm).unwrap_or_else(|e| panic!("{name}: the control: {e}"));
        assert_eq!(vm.height(), 1);
    }
}

/// A rollback whose cache reload ALSO fails still returns the original failure,
/// rather than panicking or reporting the second problem in place of the first.
#[test]
fn a_rollback_whose_reload_also_fails_still_reports_the_first_failure() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    let tx = register_tx(&k, TEST_SCHEME, digest_of("doomed"), 1);
    vm.submit_tx(&tx).expect("admitted");
    let blk = vm.build_block().expect("a block");

    let sound = fail_with(
        &vm,
        Faults::over(version_layer(&vm))
            .writing(lux_fhevm::vm::BLOCK_PREFIX)
            .reading(CURRENT_EPOCH_KEY),
    );
    let err = blk.accept(&vm).unwrap_err();
    assert!(is_disk(&err), "the write failure is what the caller is told about: {err}");
    restore(&vm, sound);
    assert_eq!(vm.height(), 0);
}

/// The failures that abort a whole block rather than reverting one transaction.
/// Each is reached by accepting a block WITHOUT verifying it first — which is
/// exactly the case these checks exist for, since a peer's block reaches
/// acceptance only after verification passed, and a check that only ever runs
/// behind another is not a check.
#[test]
fn settlement_reports_what_no_validator_can_proceed_past() {
    let (committee, _) = new_committee(1);

    // An unpriceable operation.
    {
        let k = TestKey::new();
        let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
        let mut tx = lux_fhevm::Transaction {
            tx_type: 99,
            payer: k.addr,
            gas_limit: TEST_GAS,
            nonce: 1,
            ..Default::default()
        };
        k.sign(&mut tx);
        assert!(force_block(&vm, vec![tx]).accept(&vm).is_err());
    }

    // Gas beyond the payer's own limit.
    {
        let k = TestKey::new();
        let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
        let mut tx = register_tx(&k, TEST_SCHEME, digest_of("starved"), 1);
        tx.gas_limit = 1;
        k.sign(&mut tx);
        assert!(matches!(
            force_block(&vm, vec![tx]).accept(&vm).unwrap_err(),
            Error::Fee(fee::Error::OutOfGas)
        ));
    }

    // A fee the payer cannot pay.
    {
        let k = TestKey::new();
        let vm = new_test_vm(vec![(k.hex_addr(), 1)], &committee, 1);
        let tx = register_tx(&k, TEST_SCHEME, digest_of("broke"), 1);
        assert!(matches!(
            force_block(&vm, vec![tx]).accept(&vm).unwrap_err(),
            Error::Fee(fee::Error::InsufficientFunds)
        ));
    }

    // A nonce verification would have caught.
    {
        let k = TestKey::new();
        let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
        let tx = register_tx(&k, TEST_SCHEME, digest_of("gap"), 7);
        assert!(matches!(force_block(&vm, vec![tx]).accept(&vm).unwrap_err(), Error::BadNonce));
    }
}

/// Admission's own refusals over a peer's block: the shape checks it makes before
/// it reads any state, and the state reads that can themselves fail.
#[test]
fn admission_reports_what_it_cannot_decide() {
    let (committee, _) = new_committee(1);

    // Malformed.
    {
        let k = TestKey::new();
        let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
        let mut tx = lux_fhevm::Transaction {
            tx_type: 99,
            payer: k.addr,
            gas_limit: TEST_GAS,
            nonce: 1,
            ..Default::default()
        };
        k.sign(&mut tx);
        assert!(matches!(
            force_block(&vm, vec![tx]).verify(&vm).unwrap_err(),
            Error::InvalidTxType
        ));
    }

    // Gas over the declared limit.
    {
        let k = TestKey::new();
        let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
        let mut tx = register_tx(&k, TEST_SCHEME, digest_of("overgas"), 1);
        tx.gas_limit = 1;
        k.sign(&mut tx);
        assert!(matches!(
            force_block(&vm, vec![tx]).verify(&vm).unwrap_err(),
            Error::Fee(fee::Error::OutOfGas)
        ));
    }

    // Unaffordable.
    {
        let k = TestKey::new();
        let vm = new_test_vm(vec![(k.hex_addr(), 1)], &committee, 1);
        let tx = register_tx(&k, TEST_SCHEME, digest_of("poor"), 1);
        assert!(matches!(
            force_block(&vm, vec![tx]).verify(&vm).unwrap_err(),
            Error::Fee(fee::Error::InsufficientFunds)
        ));
    }

    // A nonce that cannot be read.
    {
        let k = TestKey::new();
        let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
        let blk = force_block(&vm, vec![register_tx(&k, TEST_SCHEME, digest_of("unreadable"), 1)]);
        let sound = fail_with(&vm, Faults::over(version_layer(&vm)).reading(NONCE_PREFIX));
        let err = blk.verify(&vm).unwrap_err();
        assert!(is_disk(&err), "{err}");
        restore(&vm, sound);
    }

    // A balance that cannot be read.
    {
        let k = TestKey::new();
        let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
        let blk = force_block(&vm, vec![register_tx(&k, TEST_SCHEME, digest_of("unbalanced"), 1)]);
        let sound = fail_with(&vm, Faults::over(version_layer(&vm)).reading(b"fee/"));
        let err = blk.verify(&vm).unwrap_err();
        assert!(is_disk(&err), "{err}");
        restore(&vm, sound);
    }
}

/// The seeder stops on a failed write rather than committing a partly-seeded chain
/// — one with an allocation but no committee, or a committee but no height index.
#[test]
fn the_seeder_reports_a_write_it_cannot_make() {
    let (committee, _) = new_committee(1);
    let g = test_genesis(Vec::new(), &committee, 1);
    for (name, prefix) in [
        ("the epoch record", EPOCH_PREFIX.to_vec()),
        ("the epoch pointer", CURRENT_EPOCH_KEY.to_vec()),
        ("the height index", HEIGHT_PREFIX.to_vec()),
    ] {
        let vm = new_test_vm(Vec::new(), &committee, 1);
        // Clear the marker so the seeder runs again: what is under test is the
        // seeding, not the once-only guard, which has its own test.
        version_layer(&vm).delete(GENESIS_MARKER).expect("the marker is cleared");

        let sound = fail_with(&vm, Faults::over(version_layer(&vm)).writing(&prefix));
        let err = {
            let mut st = vm.state.write().unwrap();
            let mut id = ids::EMPTY;
            id[0] = 1;
            st.seed_genesis(&g, &id).unwrap_err()
        };
        restore(&vm, sound);
        assert!(is_disk(&err), "{name}: {err}");
    }
}

/// The two numbers a balance reply carries are read independently, and a failure
/// on either is reported. Burned supply coming back zero on a failed read is
/// indistinguishable from a chain that has never settled a fee.
#[test]
fn a_balance_reply_reports_a_supply_it_cannot_read() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("burned"), 1));

    // The account's own balance still reads; only the supply counter does not.
    let sound = fail_with(&vm, Faults::over(version_layer(&vm)).reading(b"fee/burned"));
    let err = lux_fhevm::service::call(
        &vm,
        "fchain.balance",
        &serde_json::json!({ "address": k.hex_addr() }),
    )
    .unwrap_err();
    assert!(is_disk(&err), "{err}");
    let err = lux_fhevm::service::call(&vm, "fchain.health", &serde_json::json!({})).unwrap_err();
    assert!(is_disk(&err), "{err}");
    restore(&vm, sound);

    let reply = lux_fhevm::service::call(
        &vm,
        "fchain.balance",
        &serde_json::json!({ "address": k.hex_addr() }),
    )
    .expect("the control: the supply reads");
    assert!(reply["burnedNLux"].as_u64().unwrap() > 0);
}

/// The rotation reports a failure on the epoch it OPENS as readily as on the one
/// it closes. A rotation that closed the sitting committee and failed to seat its
/// successor would leave the chain with nobody able to answer a decryption or to
/// rotate again.
#[test]
fn an_epoch_rotation_is_both_writes_or_neither() {
    let (vm, _, members) = new_decrypt_vm(3, 1, &[]); // one vote decides
    let (next, _) = new_committee(3);

    let mut key = EPOCH_PREFIX.to_vec();
    key.extend_from_slice(&1u64.to_be_bytes());
    let sound = fail_with(&vm, Faults::over(version_layer(&vm)).writing(&key));
    let now = vm.clock.time().unix();
    let err = {
        let mut st = vm.state.write().unwrap();
        advance_tx(&members[0], 1, &next, 2, b"pk", 1).apply_advance(&mut st, now).unwrap_err()
    };
    restore(&vm, sound);

    assert!(is_disk(&err), "the successor could not be seated, so the rotation failed: {err}");
    assert_eq!(vm.current_epoch(), 0, "and the sitting epoch is still the sitting epoch");
}

/// A chain with no committee has an epoch 0 with no members, which authorizes
/// nobody. It is not an error and it is not a licence: every threshold decision is
/// refused rather than accepted from anyone.
#[test]
fn a_chain_with_no_epoch_record_still_reports_a_sitting_epoch_that_authorizes_nobody() {
    let vm = new_test_vm(Vec::new(), &[], 0);
    let st = vm.state.read().unwrap();
    let cur = st.current_epoch();
    assert_eq!(cur.info.epoch, 0);
    assert_eq!(cur.info.status, fhe::EpochStatus::Active);
    assert!(cur.committee().is_empty());
    assert!(!cur.member_of(&TestKey::new().addr), "an empty committee recognises nobody");
    assert!(vm.epoch(0).is_none(), "and there is no record of one");
}
