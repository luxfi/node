// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! An F-Chain operation is paid for by BURNING real on-chain balance inside a
//! consensus block — not by an unbacked integer a caller writes into a request.

mod common;

use common::*;

use lux_fhevm::error::Error;
use lux_fhevm::fee;
use lux_fhevm::fhe;
use lux_fhevm::gas::{self, GAS_PER_BYTE, GAS_PRICE};
use lux_fhevm::state::{derive_request_id, STATUS_REVOKED};

/// The headline: the fee comes from the schedule, the payer is debited exactly
/// it, the same amount is burned, and the operation takes effect through
/// consensus.
#[test]
fn a_fee_is_metered_from_the_schedule_burned_from_the_payer_and_settled_in_a_block() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    assert_eq!(vm.balance(&k.addr).unwrap(), TEST_FUND, "genesis must fund the payer");
    let burned0 = vm.burned().unwrap();

    let tx = register_tx(&k, TEST_SCHEME, digest_of("treasury"), 1);

    // The fee is computed from the schedule, NOT supplied by the caller: register
    // plus ckks-n14 is (21000 + 60000) gas, plus 16 gas for every byte the
    // transaction puts on the chain, all at 1000 nLUX a unit.
    let expected = gas::fee_for(&tx).unwrap();
    let stored = (tx.payload.len() + tx.scheme.len()) as u64;
    assert_eq!(expected, (21_000 + 60_000 + stored * GAS_PER_BYTE) * GAS_PRICE);
    assert!(expected > 81_000_000, "stored bytes are not free");

    assert_eq!(vm.submit_tx(&tx).unwrap(), tx.id());

    let blk = vm.build_block().expect("a block");
    blk.verify(&vm).expect("it verifies");

    // Verification must NOT move funds: it is a read-only affordability check.
    assert_eq!(vm.balance(&k.addr).unwrap(), TEST_FUND, "verification must not debit");

    blk.accept(&vm).expect("it is accepted");

    assert_eq!(vm.balance(&k.addr).unwrap(), TEST_FUND - expected, "debited the metered fee");
    assert_eq!(vm.burned().unwrap(), burned0 + expected, "burned, not credited anywhere");

    let rec = vm.ciphertext(&tx.subject).expect("the effect is applied in acceptance");
    assert_eq!(rec.meta.owner, k.addr);
    assert_eq!(rec.scheme, TEST_SCHEME);
    assert_eq!(rec.digest, digest_of("treasury"));
    assert_eq!(
        rec.meta.registered_at,
        blk.timestamp.unix(),
        "a record's timestamp comes from the accepting block, never a validator's clock"
    );

    assert_eq!(vm.last_accepted(), blk.id);
    assert!(vm.mempool.lock().unwrap().txs.is_empty());
}

/// The fee is balance-backed: a payer without funds cannot get an operation
/// accepted. Admission refuses it, and even forced into a block, verification
/// fails closed.
#[test]
fn an_unfunded_payer_cannot_settle_at_either_gate() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    // Far less than one operation's fee.
    let vm = new_test_vm(vec![(k.hex_addr(), 1_000)], &committee, 1);

    let tx = register_tx(&k, TEST_SCHEME, digest_of("x"), 1);

    assert!(matches!(
        vm.submit_tx(&tx).unwrap_err(),
        Error::Fee(fee::Error::InsufficientFunds)
    ));

    let blk = force_block(&vm, vec![tx.clone()]);
    assert!(matches!(
        blk.verify(&vm).unwrap_err(),
        Error::Fee(fee::Error::InsufficientFunds)
    ));

    assert!(vm.ciphertext(&tx.subject).is_none());
    assert_eq!(vm.burned().unwrap(), 0);
}

/// Affordability is checked against the payer's RUNNING debit inside the block,
/// not its opening balance: two operations that each fit alone but do not fit
/// together never share a block, and a peer proposing both is refused.
#[test]
fn affordability_is_checked_against_the_running_debit_inside_the_block() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);

    let a = register_tx(&k, TEST_SCHEME, digest_of("a"), 1);
    let b = register_tx(&k, TEST_SCHEME, digest_of("b"), 2);
    let one = gas::fee_for(&a).unwrap();
    // Enough for one operation and one nLUX, never for two.
    let vm = new_test_vm(vec![(k.hex_addr(), one + 1)], &committee, 1);
    vm.submit_tx(&a).expect("affordable on its own");
    vm.submit_tx(&b).expect("affordable on its own");

    // The proposer takes what fits and leaves the rest queued, rather than
    // proposing a block its own verification would reject.
    let blk = vm.build_block().expect("a block");
    assert_eq!(blk.transactions.len(), 1, "only the affordable one is taken");
    blk.verify(&vm).expect("what it built, it can verify");
    assert_eq!(vm.mempool.lock().unwrap().txs.len(), 2, "selection does not drain the queue");

    // A peer proposing both is refused.
    assert!(matches!(
        force_block(&vm, vec![a, b]).verify(&vm).unwrap_err(),
        Error::Fee(fee::Error::InsufficientFunds)
    ));
}

/// The payer's declared ceiling is real: an operation costing more gas than the
/// payer allowed is never included and never charged.
#[test]
fn a_gas_limit_below_the_scheduled_cost_is_never_included_and_never_charged() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    let mut tx = register_tx(&k, TEST_SCHEME, digest_of("tight"), 1);
    tx.gas_limit = 1; // far below the scheduled 81,000
    k.sign(&mut tx);

    vm.submit_tx(&tx).expect("admission prices the operation but does not meter it");

    // Nothing else is queued, so the proposer has nothing it can build.
    assert!(matches!(vm.build_block().unwrap_err(), Error::NoPendingTxs));

    // And a peer proposing it is refused.
    assert!(matches!(
        force_block(&vm, vec![tx]).verify(&vm).unwrap_err(),
        Error::Fee(fee::Error::OutOfGas)
    ));

    assert_eq!(vm.burned().unwrap(), 0, "an unmetered operation is never charged");
}

/// Authorization integrity at the boundary: F authenticates the payer by
/// PUBLIC-key signature, so a tampered, unsigned or impersonating transaction
/// cannot spend or act.
#[test]
fn a_tampered_an_unsigned_and_an_impersonating_transaction_all_fail_at_admission() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    let mut tampered = register_tx(&k, TEST_SCHEME, digest_of("x"), 1);
    tampered.nonce = 2;
    assert!(matches!(vm.submit_tx(&tampered).unwrap_err(), Error::BadSignature));

    let mut unsigned = register_tx(&k, TEST_SCHEME, digest_of("y"), 1);
    unsigned.auth.clear();
    unsigned.sig.clear();
    assert!(matches!(vm.submit_tx(&unsigned).unwrap_err(), Error::UnsignedTx));

    let other = TestKey::new();
    let mut impersonating = register_tx(&k, TEST_SCHEME, digest_of("z"), 1);
    other.sign(&mut impersonating);
    assert!(matches!(vm.submit_tx(&impersonating).unwrap_err(), Error::PayerMismatch));

    assert_eq!(vm.burned().unwrap(), 0);
}

/// A captured signed transaction cannot be resubmitted to drain the payer through
/// repeated fee burns: the nonce refuses it at admission and again in consensus,
/// and no second burn occurs.
#[test]
fn a_replayed_transaction_is_refused_at_both_gates_and_burns_nothing_twice() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    let tx = register_tx(&k, TEST_SCHEME, digest_of("replay"), 1);
    accept_one(&vm, &tx);
    let after = vm.balance(&k.addr).unwrap();

    assert!(matches!(vm.submit_tx(&tx).unwrap_err(), Error::BadNonce));
    assert!(matches!(force_block(&vm, vec![tx]).verify(&vm).unwrap_err(), Error::BadNonce));

    assert_eq!(vm.balance(&k.addr).unwrap(), after, "a replay must not burn the payer again");
}

/// Admission enforces the SAME nonce rule consensus does, counted over what is
/// already queued. It used to accept any nonce above the committed one, so a payer
/// could queue a gap that made every block containing it unverifiable — and a
/// block that fails verification is discarded without being rejected, so the queue
/// behind it went too.
#[test]
fn admission_demands_the_consecutive_nonces_consensus_demands() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    // A gap is refused outright.
    assert!(matches!(
        vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("skip"), 2)).unwrap_err(),
        Error::BadNonce
    ));
    assert!(vm.mempool.lock().unwrap().txs.is_empty());

    // Consecutive nonces queue, including ahead of the committed one.
    vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("one"), 1)).expect("the first");
    vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("two"), 2))
        .expect("the second follows the first that is already queued");
    assert!(
        matches!(
            vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("four"), 4)).unwrap_err(),
            Error::BadNonce
        ),
        "a gap after them is still a gap"
    );

    let blk = accept_queued(&vm);
    assert_eq!(blk.transactions.len(), 2);
    assert!(vm.mempool.lock().unwrap().txs.is_empty(), "acceptance is what clears the queue");

    // After acceptance the payer continues from the committed nonce.
    vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("three"), 3)).expect("the next");
}

/// One payer cannot queue two transactions that claim the same effect. Without
/// this guard both pass every individual check and only collide in acceptance,
/// aborting the block that carried them.
#[test]
fn a_second_claim_on_one_effect_is_refused_at_admission_and_in_consensus() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    let first = register_tx(&k, TEST_SCHEME, digest_of("same"), 1);
    let second = register_tx(&k, TEST_SCHEME, digest_of("same"), 2); // same handle, next nonce

    vm.submit_tx(&first).expect("the first");
    assert!(matches!(vm.submit_tx(&second).unwrap_err(), Error::DuplicateEffect));
    assert_eq!(vm.mempool.lock().unwrap().txs.len(), 1);

    // A peer could still propose a block containing both. Consensus refuses it.
    assert!(matches!(
        force_block(&vm, vec![first.clone(), second]).verify(&vm).unwrap_err(),
        Error::DuplicateEffect
    ));

    // The honest first transaction is unaffected.
    accept_queued(&vm);
    assert!(vm.ciphertext(&first.subject).is_some());

    // And once it is committed, a later duplicate is refused by state.
    assert!(matches!(
        vm.submit_tx(&register_tx(&k, TEST_SCHEME, digest_of("same"), 2)).unwrap_err(),
        Error::CiphertextExists
    ));
}

/// An authorization failure costs the payer its fee and nothing else. The pair
/// here passes verification honestly — at that moment the permit is still active —
/// and only conflicts once the revocation lands, which is exactly the case no
/// per-transaction check can see coming. Aborting the block there would mean one
/// every validator certified and none could apply, which halts the chain;
/// reverting the one transaction does not.
#[test]
fn a_transaction_that_loses_its_authority_reverts_and_still_pays() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(
        vec![(owner.hex_addr(), TEST_FUND), (grantee.hex_addr(), TEST_FUND)],
        &committee,
        1,
    );

    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    let burned_before = vm.burned().unwrap();
    let grantee_before = vm.balance(&grantee.addr).unwrap();

    // Revocation first, then a request the revocation invalidates. The owner has
    // already spent nonces 1 and 2 seeding the permit.
    let revoke = revoke_tx(&owner, permit_id, 3);
    let request = request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1);

    let blk = force_block(&vm, vec![revoke.clone(), request.clone()]);
    blk.verify(&vm)
        .expect("both are well-formed, ordered and paid for — authorization is not this gate's question");
    blk.accept(&vm).expect("the block applies; only the transaction that lost its authority reverts");

    assert_eq!(vm.permit(&permit_id).unwrap().status, STATUS_REVOKED, "the revocation took effect");
    assert!(
        vm.decrypt(&derive_request_id(&handle, &grantee.addr, 1)).is_none(),
        "a reverted transaction leaves no record"
    );

    // But it paid: the fee is burned and the nonce consumed, so a revert is not
    // free block space.
    let request_fee = gas::fee_for(&request).unwrap();
    let revoke_fee = gas::fee_for(&revoke).unwrap();
    assert_eq!(vm.burned().unwrap(), burned_before + request_fee + revoke_fee);
    assert_eq!(vm.balance(&grantee.addr).unwrap(), grantee_before - request_fee);

    // The nonce advanced, so the reverted transaction cannot be retried as-is.
    assert!(matches!(vm.submit_tx(&request).unwrap_err(), Error::BadNonce));
}

/// The commit boundary: when settlement cannot proceed at all — here a nonce
/// verification would have caught, reached by accepting directly — the layer is
/// rolled back and the caches reloaded, so no part of the block survives.
#[test]
fn a_block_that_cannot_be_settled_leaves_no_part_of_itself_behind() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(vec![(k.hex_addr(), TEST_FUND)], &committee, 1);

    let good = register_tx(&k, TEST_SCHEME, digest_of("good"), 1);
    let gapped = register_tx(&k, TEST_SCHEME, digest_of("gapped"), 3); // 2 is missing

    let blk = force_block(&vm, vec![good.clone(), gapped.clone()]);
    assert!(matches!(blk.verify(&vm).unwrap_err(), Error::BadNonce), "consensus would refuse it");
    assert!(matches!(blk.accept(&vm).unwrap_err(), Error::BadNonce));

    // The first transaction had already been applied and burned in memory when the
    // second failed. Neither survives.
    assert!(vm.ciphertext(&good.subject).is_none());
    assert!(vm.ciphertext(&gapped.subject).is_none());
    assert_eq!(vm.burned().unwrap(), 0);
    assert_eq!(vm.balance(&k.addr).unwrap(), TEST_FUND);
    assert_eq!(vm.state.read().unwrap().nonce_of(&k.addr).unwrap(), 0);

    // And the chain still works.
    accept_one(&vm, &good);
    assert!(vm.ciphertext(&good.subject).is_some());
}
