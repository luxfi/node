// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The differential: what the Go F-Chain answered, put to this one.
//!
//! Every value here was produced by `github.com/luxfi/chains/fhevm` itself —
//! `vectors/run.sh` copies that package into a scratch directory, adds the
//! generator, and runs it — so nothing in this file is this port's own opinion.
//! Where the two disagree, GO IS RIGHT: a cross-language divergence on a chain is
//! a fork, not a preference.
//!
//! The last test is the one that subsumes most of the others. Go builds a chain
//! through the whole lifecycle and hands over the blocks as they went out on the
//! wire; this replays exactly those bytes and is required to leave behind a
//! byte-identical database — every record, every nonce, every balance, the burned
//! counter, the height index and the block store. F commits no state root, so
//! this is what stands in place of one.

mod common;

use common::*;
use serde_json::Value;

use lux_fhevm::clock::Time;
use lux_fhevm::fhe;
use lux_fhevm::gas;
use lux_fhevm::ids;
use lux_fhevm::mldsa;
use lux_fhevm::state::{
    self, committee_digest, derive_handle, derive_permit_id, derive_request_id, Attestation,
    CiphertextRecord, DecryptRecord, EpochRecord, PermitRecord, STATUS_ACTIVE,
};
use lux_fhevm::transaction::Transaction;
use lux_fhevm::vm::{Genesis, Init, Vm};
use lux_fhevm::wire;

fn vectors() -> Value {
    let raw = include_str!("../vectors/vectors.json");
    serde_json::from_str(raw).expect("the vectors parse")
}

fn unhex(v: &Value) -> Vec<u8> {
    hex::decode(v.as_str().expect("a hex string")).expect("hex")
}

fn arr32(v: &Value) -> [u8; 32] {
    let b = unhex(v);
    let mut out = [0u8; 32];
    out.copy_from_slice(&b);
    out
}

fn arr20(v: &Value) -> [u8; 20] {
    let b = unhex(v);
    let mut out = [0u8; 20];
    out.copy_from_slice(&b);
    out
}

fn vec_chain_id() -> ids::Id {
    // The generator binds every signature and every block id to this one chain.
    let mut id = ids::EMPTY;
    id[..11].copy_from_slice(b"fchain-test");
    id
}

// ---- ML-DSA-65 -------------------------------------------------------------

/// The port verifies a signature the Go chain's own signer produced, under a
/// different implementation of FIPS 204. Two implementations agreeing is
/// evidence; one implementation agreeing with itself is not.
#[test]
fn a_signature_the_go_chain_made_verifies_here() {
    let v = vectors();
    let m = &v["mldsa"];
    let pk = unhex(&m["publicKeyHex"]);
    let msg = unhex(&m["messageHex"]);
    let sig = unhex(&m["signatureHex"]);

    assert_eq!(pk.len(), m["publicKeySize"].as_u64().unwrap() as usize);
    assert_eq!(sig.len(), m["signatureSize"].as_u64().unwrap() as usize);
    assert_eq!(mldsa::PUBLIC_KEY_SIZE, pk.len());
    assert_eq!(mldsa::SIGNATURE_SIZE, sig.len());

    assert!(mldsa::verify(&pk, &msg, &sig), "the Go chain's signature must verify here");

    // And a single bit anywhere in it does not.
    let mut tampered = sig.clone();
    tampered[0] ^= 0xff;
    assert!(!mldsa::verify(&pk, &msg, &tampered));
    let mut other_msg = msg.clone();
    other_msg.push(b'x');
    assert!(!mldsa::verify(&pk, &other_msg, &sig));
}

/// A payer's address is the same one-way derivation on both sides, written the
/// same way.
#[test]
fn an_address_is_derived_and_written_the_way_go_derives_and_writes_it() {
    let v = vectors();
    let m = &v["mldsa"];
    let pk = unhex(&m["publicKeyHex"]);
    let want = arr20(&m["addressHex"]);
    assert_eq!(state::address_of(&pk), want);
    assert_eq!(ids::short_id_string(&want), m["addressCB58"].as_str().unwrap());
}

// ---- the derivations -------------------------------------------------------

#[test]
fn every_name_the_chain_derives_is_the_name_go_derives() {
    let v = vectors();
    let d = &v["derivations"];
    let scheme = d["schemeName"].as_str().unwrap();
    let digest = arr32(&d["digestHex"]);
    let handle = arr32(&d["handleHex"]);
    assert_eq!(derive_handle(&digest, scheme.as_bytes()), handle);

    let owner = arr20(&d["ownerHex"]);
    let grantee = arr20(&d["granteeHex"]);
    let ops = d["permitOps"].as_u64().unwrap() as u32;
    let permit_nonce = d["permitNonce"].as_u64().unwrap();
    assert_eq!(
        derive_permit_id(&handle, &owner, &grantee, ops, 0, permit_nonce),
        arr32(&d["permitIDHex"])
    );

    let request_nonce = d["requestNonce"].as_u64().unwrap();
    assert_eq!(derive_request_id(&handle, &grantee, request_nonce), arr32(&d["requestIDHex"]));

    let committee = committee_from(&d["committee"]);
    assert_eq!(
        committee_digest(
            d["committeeEpoch"].as_u64().unwrap(),
            d["committeeThr"].as_i64().unwrap(),
            &unhex(&d["committeePKHex"]),
            &committee,
        ),
        arr32(&d["committeeDigestHex"])
    );
}

fn committee_from(v: &Value) -> Vec<fhe::CommitteeMember> {
    v.as_array()
        .unwrap()
        .iter()
        .map(|m| fhe::CommitteeMember {
            node_id: arr20(&m["nodeIDHex"]),
            public_key: unhex(&m["publicKeyHex"]),
            weight: m["weight"].as_u64().unwrap(),
            index: m["index"].as_i64().unwrap(),
        })
        .collect()
}

/// The vmID is an immutable one-way door: it is baked into the genesis
/// CreateChainTx and stored by the P-Chain forever. Both languages must write the
/// same word for it, and for the chain id every record carries.
#[test]
fn the_chain_and_the_vm_are_named_the_same_way_in_both_languages() {
    let v = vectors();
    let d = &v["derivations"];
    assert_eq!(ids::id_string(&state::vm_id()), d["vmIDCB58"].as_str().unwrap());
    assert_eq!(ids::id_string(&vec_chain_id()), d["chainIDCB58"].as_str().unwrap());
}

// ---- the wire --------------------------------------------------------------

fn tx_from(v: &Value) -> Transaction {
    Transaction {
        tx_type: v["type"].as_u64().unwrap() as u8,
        scheme: v["scheme"].as_str().unwrap().as_bytes().to_vec(),
        payer: arr20(&v["payerHex"]),
        subject: arr32(&v["subjectHex"]),
        gas_limit: v["gasLimit"].as_u64().unwrap(),
        nonce: v["nonce"].as_u64().unwrap(),
        payload: unhex(&v["payloadHex"]),
        auth: unhex(&v["authHex"]),
        sig: unhex(&v["sigHex"]),
    }
}

/// Every operation, encoded here, must be the bytes Go encoded — content,
/// preimage, wire form and id alike. The preimage is the one that decides whether
/// a payer's signature means anything at all.
#[test]
fn every_transaction_encodes_to_the_bytes_go_encoded() {
    let v = vectors();
    let chain = vec_chain_id();
    for tv in v["transactions"].as_array().unwrap() {
        let name = tv["name"].as_str().unwrap();
        let tx = tx_from(tv);
        assert_eq!(hex::encode(tx.content()), tv["contentHex"].as_str().unwrap(), "{name}: content");
        assert_eq!(
            hex::encode(tx.signing_bytes(&chain)),
            tv["signingHex"].as_str().unwrap(),
            "{name}: the signed preimage"
        );
        assert_eq!(hex::encode(tx.bytes()), tv["bytesHex"].as_str().unwrap(), "{name}: wire");
        assert_eq!(ids::id_string(&tx.id()), tv["idHex"].as_str().unwrap(), "{name}: id");
        assert_eq!(hex::encode(tx.effect()), tv["effectHex"].as_str().unwrap(), "{name}: effect");

        // And what Go wrote, this parses back to the same transaction.
        let parsed = wire::parse_transaction(&unhex(&tv["bytesHex"])).expect(name);
        assert_eq!(parsed, tx, "{name}: round trip");
    }
}

/// The price of every operation is the same number on both sides — and a payer's
/// signature is checked against a transaction whose signature Go actually made.
#[test]
fn every_transaction_is_priced_and_authenticated_the_way_go_prices_and_authenticates_it() {
    let v = vectors();
    let chain = vec_chain_id();
    for tv in v["transactions"].as_array().unwrap() {
        let name = tv["name"].as_str().unwrap();
        let tx = tx_from(tv);
        assert_eq!(gas::gas_for(&tx).unwrap(), tv["gas"].as_u64().unwrap(), "{name}: gas");
        assert_eq!(gas::fee_for(&tx).unwrap(), tv["feeNLux"].as_u64().unwrap(), "{name}: fee");

        let go_said = tv["syntacticErr"].as_str().unwrap();
        let we_say = tx.syntactic_verify();
        assert_eq!(
            go_said.is_empty(),
            we_say.is_ok(),
            "{name}: Go said {go_said:?}, this said {we_say:?}"
        );

        // The sample transaction carries a made-up key, so only the six real ones
        // are authenticated here.
        if name != "sample" {
            tx.authenticate(&chain).unwrap_or_else(|e| panic!("{name}: {e}"));
        }
    }
}

#[test]
fn a_block_encodes_and_names_itself_the_way_go_does() {
    let v = vectors();
    let b = &v["block"];
    let chain = vec_chain_id();

    let raw = unhex(&b["bytesHex"]);
    let blk = wire::parse_block(&chain, &raw).expect("the block parses");
    assert_eq!(hex::encode(blk.bytes()), b["bytesHex"].as_str().unwrap());
    assert_eq!(hex::encode(blk.compute_id(&chain)), b["idHex"].as_str().unwrap());
    assert_eq!(blk.parent_id, arr32(&b["parentIDHex"]));
    assert_eq!(blk.height, b["height"].as_u64().unwrap());
    assert_eq!(blk.timestamp.unix(), b["timestamp"].as_i64().unwrap());
    assert_eq!(blk.transactions.len(), 2);

    assert_eq!(wire::empty_block_size(), b["emptyBlockSize"].as_u64().unwrap() as usize);
    assert_eq!(wire::TX_ENTRY, b["txEntry"].as_u64().unwrap() as usize);
}

// ---- the records -----------------------------------------------------------

/// What F persists is JSON, and two validators replaying one block must write the
/// same bytes. These are the bytes Go wrote.
#[test]
fn every_record_is_stored_as_the_bytes_go_stores() {
    let v = vectors();
    let d = &v["derivations"];
    let r = &v["records"];
    let chain = vec_chain_id();
    let handle = arr32(&d["handleHex"]);
    let digest = arr32(&d["digestHex"]);
    let owner = arr20(&d["ownerHex"]);
    let grantee = arr20(&d["granteeHex"]);
    let permit_id = arr32(&d["permitIDHex"]);
    let request_id = arr32(&d["requestIDHex"]);
    let committee = committee_from(&d["committee"]);

    let ct = CiphertextRecord {
        meta: fhe::CiphertextMeta {
            handle,
            owner,
            kind: 4,
            level: 3,
            epoch: 7,
            registered_at: 1_700_000_000,
            size: 4096,
            chain_id: chain,
        },
        scheme: d["schemeName"].as_str().unwrap().into(),
        digest,
    };
    assert_eq!(ct.to_json(), r["ciphertext"].as_str().unwrap());
    assert_eq!(CiphertextRecord::from_json(ct.to_json().as_bytes()).unwrap(), ct);

    let pm = PermitRecord {
        permit: fhe::Permit {
            permit_id,
            handle,
            grantee,
            grantor: owner,
            operations: fhe::PERMIT_OP_DECRYPT,
            expiry: 99,
            created_at: 10,
            attestation: Vec::new(),
            chain_id: chain,
        },
        status: STATUS_ACTIVE.into(),
    };
    assert_eq!(pm.to_json(), r["permit"].as_str().unwrap());

    let dr = DecryptRecord {
        request: fhe::DecryptRequest {
            request_id,
            ciphertext_handle: handle,
            requester: grantee,
            callback: {
                let mut c = [0u8; 20];
                c[0] = 0xca;
                c[1] = 0x11;
                c
            },
            callback_selector: [1, 2, 3, 4],
            source_chain: chain,
            epoch: 1,
            nonce: 2,
            expiry: 3,
            status: fhe::RequestStatus::Completed,
            created_at: 4,
            completed_at: 5,
            result_handle: digest_of("result"),
            error: String::new(),
        },
        permit_id,
        attestations: Some(vec![Attestation { member: owner, value: digest_of("v") }]),
    };
    assert_eq!(dr.to_json(), r["decrypt"].as_str().unwrap());

    let ep = EpochRecord {
        info: fhe::EpochInfo {
            epoch: 1,
            start_time: 2,
            end_time: 3,
            committee: Some(committee),
            threshold: 2,
            public_key: b"network-fhe-public-key".to_vec(),
            status: fhe::EpochStatus::Ended,
        },
        attestations: None,
    };
    assert_eq!(ep.to_json(), r["epoch"].as_str().unwrap());

    // An epoch with nothing in it is where `omitempty` and a nil slice differ from
    // an empty one, and where a port that guessed would guess wrong.
    let empty = EpochRecord {
        info: fhe::EpochInfo { epoch: 9, status: fhe::EpochStatus::Active, ..Default::default() },
        attestations: None,
    };
    assert_eq!(empty.to_json(), r["epochEmpty"].as_str().unwrap());
}

// ---- the schedule ----------------------------------------------------------

#[test]
fn the_gas_schedule_prices_and_refuses_exactly_what_gos_does() {
    let v = vectors();
    for entry in v["gas"].as_array().unwrap() {
        let tx = Transaction {
            tx_type: entry["type"].as_u64().unwrap() as u8,
            scheme: entry["scheme"].as_str().unwrap().as_bytes().to_vec(),
            payload: unhex(&entry["payloadHex"]),
            ..Transaction::default()
        };
        let label = format!("op {} scheme {:?}", tx.tx_type, entry["scheme"]);
        match entry.get("err") {
            Some(_) => assert!(gas::gas_for(&tx).is_err(), "{label}: Go refused it"),
            None => {
                assert_eq!(gas::gas_for(&tx).unwrap(), entry["gas"].as_u64().unwrap(), "{label}");
                assert_eq!(
                    gas::fee_for(&tx).unwrap(),
                    entry["feeNLux"].as_u64().unwrap(),
                    "{label}"
                );
            }
        }
    }
}

#[test]
fn the_runtime_parameters_are_the_ones_the_runtime_reports() {
    let v = vectors();
    let p = &v["params"];
    let ours = fhe::DEFAULT_THRESHOLD_PARAMS;
    assert_eq!(ours.log_n, p["logN"].as_i64().unwrap());
    assert_eq!(ours.log_qp, p["logQP"].as_i64().unwrap());
    assert_eq!(ours.log_scale, p["logScale"].as_i64().unwrap());
}

// ---- and the whole chain ---------------------------------------------------

/// THE gate. Go ran the whole lifecycle — register, grant, request, two
/// attestations, two epoch votes, revoke — and handed over the blocks as they went
/// out on the wire. This replays exactly those bytes onto a fresh node whose clock
/// is set to a different hour, and requires the database it leaves behind to be
/// byte-identical to Go's: every record, every nonce, every balance, the burned
/// counter, the height index and the block store.
///
/// F commits no state root, so nothing in consensus would catch a divergence here.
/// This is what stands in its place, and it stands across the language boundary.
#[test]
fn replaying_gos_chain_leaves_gos_database_byte_for_byte() {
    let v = vectors();
    let r = &v["replay"];

    let vm = Vm::initialize(Init {
        db: lux_fhevm::db::Mem::new(),
        chain_id: vec_chain_id(),
        network_id: 96369,
        genesis: unhex(&r["genesisHex"]),
        config: Vec::new(),
    })
    .expect("the chain starts on Go's genesis");

    // A different hour entirely: chain time must come from the block, so nothing
    // stored may depend on this.
    vm.clock.set(Time::from_unix(2_100_000_000));

    for (i, raw_hex) in r["blocksHex"].as_array().unwrap().iter().enumerate() {
        let raw = unhex(raw_hex);
        let blk = vm.parse_block(&raw).unwrap_or_else(|e| panic!("block {i}: {e}"));
        blk.verify(&vm).unwrap_or_else(|e| panic!("block {i}: verify: {e}"));
        blk.accept(&vm).unwrap_or_else(|e| panic!("block {i}: accept: {e}"));
    }

    assert_eq!(vm.height(), r["height"].as_u64().unwrap(), "the chain reached Go's height");
    assert_eq!(vm.current_epoch(), r["epoch"].as_u64().unwrap(), "and Go's epoch");

    // The replayed state is not merely equal, it is the right state.
    let handle = arr32(&r["handleHex"]);
    let permit_id = arr32(&r["permitIDHex"]);
    let request_id = arr32(&r["requestIDHex"]);
    assert!(vm.ciphertext(&handle).is_some());
    assert_eq!(vm.permit(&permit_id).unwrap().status, "revoked");
    let req = vm.decrypt(&request_id).expect("the request is there");
    assert_eq!(req.request.status, fhe::RequestStatus::Completed);
    assert_eq!(req.request.result_handle, arr32(&r["resultHex"]));

    let (sum, listing) = dump(&vm);
    if listing != r["dumpListing"].as_str().unwrap() {
        // Name the first row that differs rather than printing two databases.
        let want: Vec<&str> = r["dumpListing"].as_str().unwrap().lines().collect();
        let got: Vec<&str> = listing.lines().collect();
        for (i, (w, g)) in want.iter().zip(got.iter()).enumerate() {
            assert_eq!(w, g, "row {i} differs from Go's database");
        }
        assert_eq!(want.len(), got.len(), "the databases hold different numbers of rows");
    }
    assert_eq!(sum, r["dumpSumHex"].as_str().unwrap(), "the database is not Go's, byte for byte");
}

/// The genesis Go wrote is the genesis this reads, and the genesis this writes is
/// the one Go would read: the same bytes both ways.
#[test]
fn the_genesis_round_trips_through_both_languages() {
    let v = vectors();
    let raw = unhex(&v["replay"]["genesisHex"]);
    let g = Genesis::decode(&raw).expect("Go's genesis decodes");
    assert_eq!(g.version, 1);
    assert_eq!(g.timestamp, 1_700_000_000);
    assert_eq!(g.members().len(), 3);
    assert_eq!(g.threshold, 2);
    assert_eq!(g.public_key, b"network-fhe-public-key");
    assert_eq!(g.to_json(), raw, "and re-encodes to the bytes it arrived as");
}
