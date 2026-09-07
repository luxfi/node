// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The read surface, and the one way to change anything.

mod common;

use common::*;
use serde_json::json;

use lux_fhevm::error::Error;
use lux_fhevm::fhe;
use lux_fhevm::gas::{self, GAS_PRICE};
use lux_fhevm::ids;
use lux_fhevm::service::call;
use lux_fhevm::state::{derive_handle, STATUS_ACTIVE, STATUS_REVOKED};

/// The parameters F reports come from the FHE runtime's own configuration — so a
/// client encrypting for F and a node evaluating for F agree by construction —
/// while the threshold and network key come from the seated epoch on chain.
#[test]
fn the_public_parameters_are_the_runtimes_and_the_committee_is_the_chains() {
    let (committee, _) = new_committee(3);
    let vm = new_test_vm(Vec::new(), &committee, 2);

    let reply = call(&vm, "fchain.getPublicParams", &json!({})).expect("the params read");
    let p = fhe::DEFAULT_THRESHOLD_PARAMS;
    assert_eq!(reply["logN"], p.log_n);
    assert_eq!(reply["logQP"], p.log_qp);
    assert_eq!(reply["logScale"], p.log_scale);
    assert_eq!(reply["epoch"], 0);
    assert_eq!(reply["threshold"], 2);
    assert_eq!(reply["publicKey"], hex::encode(b"network-fhe-public-key"));
    assert_eq!(reply["chainId"], ids::id_string(&vm.chain_id));
}

/// The committee is readable per epoch, and an epoch nobody ever seated is an
/// error rather than an empty answer.
#[test]
fn the_committee_reads_per_epoch_and_an_unseated_one_is_an_error() {
    let (committee, _) = new_committee(3);
    let vm = new_test_vm(Vec::new(), &committee, 2);

    let reply = call(&vm, "fchain.getCommittee", &json!({})).expect("the committee reads");
    assert_eq!(reply["epoch"], 0);
    assert_eq!(reply["threshold"], 2);
    let members = reply["members"].as_array().expect("a list of members");
    assert_eq!(members.len(), 3);
    for (i, m) in members.iter().enumerate() {
        assert_eq!(m["nodeId"], ids::node_id_string(&committee[i].node_id));
        assert_eq!(m["publicKey"], hex::encode(&committee[i].public_key));
        assert_eq!(m["index"], i as i64);
    }

    let err = call(&vm, "fchain.getCommittee", &json!({ "epoch": 9 })).unwrap_err();
    assert!(matches!(err, Error::EpochNotFound));
}

/// A registered value reads back with its digest — so a client can check a body it
/// fetched from off-chain storage — and the listing filters work.
#[test]
fn a_ciphertext_reads_back_with_its_digest_and_the_filters_work() {
    let a = TestKey::new();
    let b = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&a, &b]), &committee, 1);

    let digest = digest_of("value-one");
    accept_one(&vm, &register_tx(&a, TEST_SCHEME, digest, 1));
    accept_one(&vm, &register_tx(&b, "bfv-n13", digest_of("value-two"), 1));

    let handle = derive_handle(&digest, TEST_SCHEME.as_bytes());
    let got = call(&vm, "fchain.getCiphertext", &json!({ "handle": hex_of(handle) }))
        .expect("the ciphertext reads");
    let view = &got["ciphertext"];
    assert_eq!(view["handle"], hex_of(handle));
    assert_eq!(view["digest"], hex_of(digest));
    assert_eq!(view["owner"], ids::short_id_string(&a.addr));
    assert_eq!(view["scheme"], TEST_SCHEME);
    assert_eq!(view["size"], 4096);
    assert_eq!(view["chainId"], ids::id_string(&vm.chain_id));

    let all = call(&vm, "fchain.listCiphertexts", &json!({})).expect("the listing reads");
    assert_eq!(all["total"], 2);

    let by_scheme = call(&vm, "fchain.listCiphertexts", &json!({ "scheme": "bfv-n13" })).unwrap();
    assert_eq!(by_scheme["total"], 1);
    assert_eq!(by_scheme["ciphertexts"][0]["scheme"], "bfv-n13");

    let by_owner =
        call(&vm, "fchain.listCiphertexts", &json!({ "owner": a.hex_addr() })).unwrap();
    assert_eq!(by_owner["total"], 1);
    assert_eq!(by_owner["ciphertexts"][0]["owner"], ids::short_id_string(&a.addr));

    let err = call(&vm, "fchain.getCiphertext", &json!({ "handle": hex_of(digest_of("absent")) }))
        .unwrap_err();
    assert!(matches!(err, Error::CiphertextNotFound));
}

/// A grant reads back with the capability bits and expiry it was created with, and
/// revocation is visible.
#[test]
fn a_permit_reads_back_with_its_capabilities_and_its_withdrawal() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&owner, &grantee]), &committee, 1);

    let ops = fhe::PERMIT_OP_DECRYPT | fhe::PERMIT_OP_COMPUTE;
    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, ops, 0);

    let got = call(&vm, "fchain.getPermit", &json!({ "permitId": hex_of(permit_id) })).unwrap();
    let view = &got["permit"];
    assert_eq!(view["permitId"], hex_of(permit_id));
    assert_eq!(view["handle"], hex_of(handle));
    assert_eq!(view["grantor"], ids::short_id_string(&owner.addr));
    assert_eq!(view["grantee"], ids::short_id_string(&grantee.addr));
    assert_eq!(view["operations"], ops);
    assert_eq!(view["status"], STATUS_ACTIVE);

    accept_one(&vm, &revoke_tx(&owner, permit_id, 3));
    let got = call(&vm, "fchain.getPermit", &json!({ "permitId": hex_of(permit_id) })).unwrap();
    assert_eq!(got["permit"]["status"], STATUS_REVOKED);

    let err = call(&vm, "fchain.getPermit", &json!({ "permitId": hex_of(digest_of("absent")) }))
        .unwrap_err();
    assert!(matches!(err, Error::PermitNotFound));
}

/// The request surface shows how far the committee has got — who attested what,
/// against what threshold — and the final result handle once it completes.
#[test]
fn a_decryption_request_shows_the_committees_progress_and_its_answer() {
    let owner = TestKey::new();
    let grantee = TestKey::new();
    let (vm, _, members) = new_decrypt_vm(3, 2, &[&owner, &grantee]);

    let (handle, permit_id) = seed_permit(&vm, &owner, &grantee, fhe::PERMIT_OP_DECRYPT, 0);
    accept_one(&vm, &request_tx(&grantee, TEST_SCHEME, handle, permit_id, 0, 1));
    let request_id = request_id_for(handle, &grantee.addr, 1);

    let got = call(&vm, "fchain.getDecrypt", &json!({ "requestId": hex_of(request_id) })).unwrap();
    let view = &got["request"];
    assert_eq!(view["requestId"], hex_of(request_id));
    assert_eq!(view["handle"], hex_of(handle));
    assert_eq!(view["permitId"], hex_of(permit_id));
    assert_eq!(view["requester"], ids::short_id_string(&grantee.addr));
    assert_eq!(view["status"], "pending");
    assert_eq!(view["threshold"], 2);
    assert!(view["attestations"].as_array().unwrap().is_empty());
    assert!(view.get("resultHandle").is_none(), "a pending request has no result");
    assert_eq!(view["callback"], "ca11000000000000000000000000000000000000");
    assert_eq!(view["selector"], "01020304");

    let result = digest_of("answer");
    accept_one(&vm, &fulfill_tx(&members[0], request_id, result, 1));
    let got = call(&vm, "fchain.getDecrypt", &json!({ "requestId": hex_of(request_id) })).unwrap();
    let view = &got["request"];
    assert_eq!(view["attestations"].as_array().unwrap().len(), 1);
    assert_eq!(view["attestations"][0]["member"], ids::short_id_string(&members[0].addr));
    assert_eq!(view["attestations"][0]["value"], hex_of(result));
    assert_eq!(view["status"], "pending");

    accept_one(&vm, &fulfill_tx(&members[1], request_id, result, 1));
    let got = call(&vm, "fchain.getDecrypt", &json!({ "requestId": hex_of(request_id) })).unwrap();
    assert_eq!(got["request"]["status"], "completed");
    assert_eq!(got["request"]["resultHandle"], hex_of(result));
    assert_ne!(got["request"]["completedAt"], 0);

    let err = call(&vm, "fchain.getDecrypt", &json!({ "requestId": hex_of(digest_of("absent")) }))
        .unwrap_err();
    assert!(matches!(err, Error::RequestNotFound));
}

/// The one mutating call takes a client-signed transaction as hex, parses it
/// canonically, and enqueues it — and a transaction the chain would refuse is
/// refused here too.
#[test]
fn the_one_mutating_call_takes_a_signed_transaction_and_nothing_else() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    let tx = register_tx(&k, TEST_SCHEME, digest_of("submitted"), 1);
    let reply =
        call(&vm, "fchain.submitTransaction", &json!({ "tx": hex::encode(tx.bytes()) })).unwrap();
    assert_eq!(reply["txId"], ids::id_string(&tx.id()));

    // The 0x prefix is accepted too.
    let next = register_tx(&k, TEST_SCHEME, digest_of("prefixed"), 2);
    call(&vm, "fchain.submitTransaction", &json!({ "tx": format!("0x{}", hex::encode(next.bytes())) }))
        .expect("the prefixed form");

    accept_queued(&vm);
    assert!(vm.ciphertext(&tx.subject).is_some());

    assert!(call(&vm, "fchain.submitTransaction", &json!({ "tx": "zzzz" })).is_err(), "not hex");
    assert!(
        call(&vm, "fchain.submitTransaction", &json!({ "tx": "deadbeef" })).is_err(),
        "hex, but not a transaction"
    );
    assert!(matches!(
        call(&vm, "fchain.submitTransaction", &json!({ "tx": hex::encode(tx.bytes()) }))
            .unwrap_err(),
        Error::BadNonce
    ));
}

#[test]
fn the_account_and_diagnostic_surfaces_report_what_consensus_did() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);

    let tx = register_tx(&k, TEST_SCHEME, digest_of("charged"), 1);
    let spent = gas::fee_for(&tx).unwrap();
    accept_one(&vm, &tx);

    let bal = call(&vm, "fchain.balance", &json!({ "address": k.hex_addr() })).unwrap();
    assert_eq!(bal["balanceNLux"], TEST_FUND - spent);
    assert_eq!(bal["burnedNLux"], spent);

    assert!(call(&vm, "fchain.balance", &json!({ "address": "nothex" })).is_err());
    assert!(call(&vm, "fchain.balance", &json!({ "address": "0011" })).is_err());

    let h = call(&vm, "fchain.health", &json!({})).unwrap();
    assert_eq!(h["healthy"], true);
    assert_eq!(h["details"]["ciphertexts"], "1");
    assert_eq!(h["details"]["height"], "1");
}

/// Clients can compute the exact burn before submitting: every operation is
/// listed, scheme-priced ones once per scheme, and each entry's fee is gas times
/// the price.
#[test]
fn the_fee_schedule_lists_every_operation_at_the_price_it_will_settle() {
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(Vec::new(), &committee, 1);

    let reply = call(&vm, "fchain.feeSchedule", &json!({})).unwrap();
    assert_eq!(reply["gasPriceNLuxPerGas"], GAS_PRICE);

    let mut seen: std::collections::HashMap<String, usize> = std::collections::HashMap::new();
    for e in reply["entries"].as_array().unwrap() {
        *seen.entry(e["operation"].as_str().unwrap().to_string()).or_default() += 1;
        assert_eq!(
            e["feeNLux"].as_u64().unwrap(),
            e["gas"].as_u64().unwrap() * GAS_PRICE,
            "{} / {}",
            e["operation"],
            e["scheme"]
        );
        assert!(e["feeNLux"].as_u64().unwrap() >= gas::min_scheduled_fee());
    }
    for (op, name) in gas::OP_NAMES {
        let want = if gas::uses_scheme(op) { gas::SCHEME_GAS.len() } else { 1 };
        assert_eq!(seen.get(name).copied().unwrap_or(0), want, "operation {name}");
    }
}

/// The 32-byte identifier decoder fails closed on anything that is not exactly 32
/// bytes of hex, rather than padding or truncating into a valid-looking lookup.
#[test]
fn an_identifier_that_is_not_thirty_two_bytes_of_hex_is_refused() {
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(Vec::new(), &committee, 1);

    for bad in ["", "zz", "00", &hex::encode([0u8; 31]), &hex::encode([0u8; 33])] {
        assert!(call(&vm, "fchain.getCiphertext", &json!({ "handle": bad })).is_err(), "{bad:?}");
        assert!(call(&vm, "fchain.getPermit", &json!({ "permitId": bad })).is_err(), "{bad:?}");
        assert!(call(&vm, "fchain.getDecrypt", &json!({ "requestId": bad })).is_err(), "{bad:?}");
    }
}

/// A filter whose address does not decode is an error, not an empty listing: an
/// empty listing reads as "this owner has nothing". And a method the surface does
/// not answer is refused rather than answered with nothing.
#[test]
fn the_read_surface_refuses_what_it_cannot_answer() {
    let k = TestKey::new();
    let (committee, _) = new_committee(1);
    let vm = new_test_vm(fund_all(&[&k]), &committee, 1);
    accept_one(&vm, &register_tx(&k, TEST_SCHEME, digest_of("listed"), 1));

    assert!(call(&vm, "fchain.listCiphertexts", &json!({ "owner": "not-an-address" })).is_err());
    assert!(call(&vm, "fchain.balance", &json!({ "address": "zz" })).is_err());
    assert!(call(&vm, "fchain.thereIsNoSuchMethod", &json!({})).is_err());

    // The control: the same calls with sound arguments answer.
    let listed = call(&vm, "fchain.listCiphertexts", &json!({ "owner": k.hex_addr() })).unwrap();
    assert_eq!(listed["total"], 1);
    let bal = call(&vm, "fchain.balance", &json!({ "address": k.hex_addr() })).unwrap();
    assert!(bal["balanceNLux"].as_u64().unwrap() < TEST_FUND, "the fee was burned");
    assert!(bal["burnedNLux"].as_u64().unwrap() > 0);
}
