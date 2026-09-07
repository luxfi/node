// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! What the Go reference actually produced.
//!
//! Every number in `testdata/golden.json` was written BY
//! `~/work/lux/chains/zkvm`, running the emitter kept beside it in
//! `testdata/gen/`. Nothing here is a value someone read off the Go source and
//! retyped: a retyped constant proves that two people agree about what the
//! code says, which is not the question. The question is whether these two
//! implementations put the same bytes on a wire, and only the reference can
//! answer it.
//!
//! To regenerate, see `testdata/README.md`.

use std::collections::HashMap;

use lux_zkvm::block::Block;
use lux_zkvm::ids::{prefixed, Id, EMPTY};
use lux_zkvm::root::Root;
use lux_zkvm::tx::{
    ShieldedOutput, Transaction, TransactionType, TransparentInput, TransparentOutput, ZkProof,
};
use lux_zkvm::utxo::Utxo;
use lux_zkvm::vertex::Vertex;
use lux_zkvm::wire;

fn golden() -> HashMap<String, Vec<u8>> {
    let raw = std::fs::read_to_string(concat!(env!("CARGO_MANIFEST_DIR"), "/testdata/golden.json"))
        .expect("testdata/golden.json");
    let doc: serde_json::Value = serde_json::from_str(&raw).expect("golden.json parses");
    doc["vectors"]
        .as_array()
        .expect("vectors")
        .iter()
        .map(|v| {
            (
                v["name"].as_str().unwrap().to_string(),
                hex::decode(v["hex"].as_str().unwrap()).unwrap(),
            )
        })
        .collect()
}

const NETWORK_ID: u32 = 96369;

fn chain_id() -> Id {
    prefixed(&[0xAA, 0xBB, 0xCC])
}

fn bind() -> [u8; 32] {
    lux_zkvm::vm::bind(&chain_id(), NETWORK_ID)
}

// ---- the four transactions the emitter marshalled -------------------------

fn tx_full() -> Transaction {
    Transaction {
        kind: TransactionType::Shield as u8,
        version: 1,
        fee: 100,
        expiry: 999,
        transparent_inputs: vec![TransparentInput {
            tx_id: prefixed(&[4]),
            output_idx: 2,
            amount: 50,
            address: b"addr-in".to_vec(),
        }],
        transparent_outputs: vec![TransparentOutput {
            amount: 30,
            asset_id: prefixed(&[5]),
            address: b"addr-out".to_vec(),
        }],
        nullifiers: vec![b"null1".to_vec(), b"null2".to_vec()],
        outputs: vec![ShieldedOutput {
            commitment: b"c".to_vec(),
            encrypted_note: b"n".to_vec(),
            ephemeral_pub_key: b"e".to_vec(),
            output_proof: b"p".to_vec(),
        }],
        proof: Some(ZkProof {
            proof_type: "groth16".into(),
            proof_data: b"pd".to_vec(),
            public_inputs: vec![b"pi1".to_vec(), b"pi2".to_vec()],
        }),
        memo: b"memo".to_vec(),
        ..Default::default()
    }
}

fn tx_min() -> Transaction {
    Transaction {
        kind: TransactionType::Transfer as u8,
        fee: 1,
        nullifiers: vec![b"x".to_vec()],
        ..Default::default()
    }
}

fn tx_no_proof() -> Transaction {
    Transaction {
        kind: TransactionType::Unshield as u8,
        version: 3,
        fee: 7,
        expiry: 1 << 40,
        nullifiers: vec![vec![0x00], vec![0xff, 0xee]],
        transparent_outputs: vec![TransparentOutput {
            amount: 1 << 63,
            asset_id: prefixed(&[9]),
            address: Vec::new(),
        }],
        ..Default::default()
    }
}

fn tx_empty() -> Transaction {
    Transaction {
        kind: TransactionType::Mint as u8,
        ..Default::default()
    }
}

fn cases() -> Vec<(&'static str, Transaction)> {
    vec![
        ("tx_full", tx_full()),
        ("tx_min", tx_min()),
        ("tx_no_proof", tx_no_proof()),
        ("tx_empty", tx_empty()),
    ]
}

#[test]
fn the_chain_binding_is_the_go_binding() {
    assert_eq!(&bind()[..], &golden()["bind"][..]);
}

#[test]
fn every_transaction_marshals_to_the_go_bytes() {
    let g = golden();
    for (name, tx) in cases() {
        assert_eq!(
            wire::marshal_transaction(&tx),
            g[&format!("{name}.wire")],
            "{name}: the wire"
        );
    }
}

#[test]
fn every_transaction_has_the_go_identity() {
    let g = golden();
    for (name, tx) in cases() {
        assert_eq!(
            &tx.compute_id()[..],
            &g[&format!("{name}.id")][..],
            "{name}: the id"
        );
    }
}

#[test]
fn the_go_bytes_parse_back_to_the_same_transaction() {
    // The other direction: Go's bytes, read by this parser, are the value Go
    // held — including the id, which is derived rather than carried.
    let g = golden();
    for (name, tx) in cases() {
        let parsed = wire::parse_transaction(&g[&format!("{name}.wire")]).expect(name);
        assert_eq!(parsed, tx.clone().with_id(), "{name}");
    }
}

#[test]
fn a_utxo_marshals_to_the_go_bytes() {
    let u = Utxo {
        tx_id: prefixed(&[1, 2, 3]),
        output_index: 7,
        height: 42,
        commitment: b"commit".to_vec(),
        ciphertext: b"cipher".to_vec(),
        ephemeral_pk: b"epk".to_vec(),
    };
    let g = golden();
    assert_eq!(u.marshal(), g["utxo.wire"]);
    assert_eq!(wire::parse_utxo(&g["utxo.wire"]).unwrap(), u);
}

#[test]
fn the_state_root_folds_to_the_go_root() {
    let g = golden();
    let dir = tempdir("golden-root");
    let view = lux_zkvm::db::View::new(std::sync::Arc::new(
        lux_zkvm::db::Db::open(dir.join("log")).unwrap(),
    ));

    let zero = Root::open(&view).unwrap();
    assert_eq!(zero.get().unwrap(), vec![0u8; 32]);
    assert_eq!(zero.after(&[tx_full()]).unwrap(), g["root.after_zero_full"]);

    // Then over a non-zero committed root, which is the case that proves the
    // committed value is folded in first rather than assumed empty.
    zero.finalize(&view, &g["root.committed"]).unwrap();
    assert_eq!(
        zero.after(&[tx_full(), tx_min()]).unwrap(),
        g["root.after_full_min"]
    );
    assert_eq!(zero.after(&[]).unwrap(), g["root.after_none"]);
}

#[test]
fn a_block_marshals_and_names_itself_as_go_does() {
    let g = golden();
    for (name, parent, height, ts, txs, state_root) in [
        (
            "block",
            prefixed(&[2]),
            5u64,
            1_700_000_000i64,
            vec![tx_full().with_id(), tx_min().with_id()],
            b"root".to_vec(),
        ),
        ("block_empty", EMPTY, 0, 0, vec![], Vec::new()),
        ("block_neg", prefixed(&[7]), 1, -3, vec![], Vec::new()),
    ] {
        let block = Block::new(parent, height, ts, txs, state_root);
        assert_eq!(
            block.marshal(),
            g[&format!("{name}.wire")],
            "{name}: the wire"
        );
        assert_eq!(
            &block.compute_id(&bind())[..],
            &g[&format!("{name}.id")][..],
            "{name}: the id"
        );
        // And Go's bytes read back to the same block.
        let back = Block::parse(&g[&format!("{name}.wire")]).expect(name);
        assert_eq!(back.marshal(), block.marshal(), "{name}: round trip");
    }
}

#[test]
fn a_vertex_serializes_and_names_itself_as_go_does() {
    let g = golden();
    let v = Vertex::new(
        9,
        2,
        vec![prefixed(&[1]), prefixed(&[2])],
        vec![tx_full().with_id(), tx_min().with_id()],
    );
    assert_eq!(v.serialize(), g["vertex.wire"]);
    assert_eq!(&v.compute_id(&bind())[..], &g["vertex.id"][..]);

    let back = Vertex::parse(&g["vertex.wire"]).unwrap();
    assert_eq!(back.serialize(), v.serialize());
    assert_eq!(back.height(), 9);
    assert_eq!(back.epoch(), 2);
    assert_eq!(back.parents().len(), 2);
}

#[test]
fn gnarks_points_read_back_as_the_points_gnark_wrote() {
    // The encoding is the reason this test exists: X before Y, big-endian,
    // and an extension coordinate A1 BEFORE A0. Reading the halves the other
    // way round produces a point that is still on the curve often enough to
    // pass a shallow test, and never verifies a real proof.
    let g = golden();
    let (g1, used) = lux_zkvm::groth16::read_g1(&g["g1.gen"]).unwrap();
    assert_eq!(used, 64);
    let (g2, used) = lux_zkvm::groth16::read_g2(&g["g2.gen"]).unwrap();
    assert_eq!(used, 128);
    // Doubling and tripling the generators lands where Go says they land.
    assert_eq!(lux_zkvm::groth16::read_g1(&g["g1.two"]).unwrap().0, g1 + g1);
    assert_eq!(
        lux_zkvm::groth16::read_g2(&g["g2.three"]).unwrap().0,
        g2 + g2 + g2
    );
}

#[test]
fn a_groth16_instance_go_verifies_verifies_here_too() {
    // The instance in the golden file is one the Go reference itself accepted
    // — `TestCheckGoldenInstance` in `testdata/gen/` ran gnark's own pairing
    // over these exact bytes — and the mutated one is one it refused. Two
    // independent pairing implementations, one answer.
    let g = golden();
    let vk = lux_zkvm::groth16::read_verifying_key(&g["groth16.vk1"]).unwrap();
    lux_zkvm::groth16::validate_verifying_key(&vk).unwrap();
    let proof = lux_zkvm::groth16::read_proof(&g["groth16.proof1"]).unwrap();
    let witness = [lux_zkvm::groth16::witness_from_bytes(&g["bind"])];
    assert!(
        lux_zkvm::groth16::verify(&proof, &vk, &witness).is_ok(),
        "an instance Go verifies must verify here"
    );

    let moved = lux_zkvm::groth16::read_proof(&g["groth16.proof1_bad"]).unwrap();
    assert!(
        lux_zkvm::groth16::verify(&moved, &vk, &witness).is_err(),
        "an instance Go refuses must be refused here"
    );

    // And the witness count the key describes is the count it takes.
    assert!(lux_zkvm::groth16::verify(&proof, &vk, &[]).is_err());
    assert!(lux_zkvm::groth16::verify(&proof, &vk, &[witness[0], witness[0]]).is_err());
}

/// The reference's four steps, in the reference's order: the length bound, the
/// key, the proof, the pairing. `judge` in `testdata/gen/emit_test.go` is this
/// function in Go, and the whole of the test below is that the two answer the
/// same thing about the same bytes.
fn judge(vk_bytes: &[u8], proof_bytes: &[u8], witness: &[u8]) -> std::result::Result<(), String> {
    if proof_bytes.len() < lux_zkvm::groth16::PROOF_LEN {
        return Err("proof data too short".into());
    }
    let vk = lux_zkvm::groth16::read_verifying_key(vk_bytes).map_err(|e| format!("vk: {e}"))?;
    lux_zkvm::groth16::validate_verifying_key(&vk).map_err(|e| format!("vk validate: {e}"))?;
    let proof = lux_zkvm::groth16::read_proof(proof_bytes).map_err(|e| format!("proof: {e}"))?;
    let w = [lux_zkvm::groth16::witness_from_bytes(witness)];
    lux_zkvm::groth16::verify(&proof, &vk, &w).map_err(|e| format!("pairing: {e}"))
}

/// Sixteen instances the Go reference read, and the verdict it reached on each.
///
/// A verifier that accepts everything passes any test that only feeds it good
/// proofs. So fifteen of these are bad in one specific way — a proof element
/// moved, a point at infinity where a pairing would then DROP the term, a
/// point on the twist but outside the group, a coordinate that is not a field
/// element, a proof one byte short, a key whose point count overruns its own
/// bytes, a key for a different circuit — and exactly one is good. Go's answer
/// to each was computed by Go, over these exact bytes, and written into
/// `adv.<case>.verdict`.
///
/// Both halves matter. Refusing the good instance would be a chain that can
/// never accept a shielded transfer; accepting any of the other fifteen is a
/// forgery this node would put in a block.
#[test]
fn every_adversarial_instance_gets_the_verdict_go_gave_it() {
    let g = golden();
    let bind = g["bind"].clone();

    let mut cases: Vec<String> = g
        .keys()
        .filter_map(|k| k.strip_prefix("adv.")?.strip_suffix(".verdict"))
        .map(str::to_string)
        .collect();
    cases.sort();
    assert!(
        cases.len() >= 16,
        "the corpus carries {} adversarial cases; the emitter writes sixteen",
        cases.len()
    );

    let mut accepted = 0;
    let mut disagreements = Vec::new();
    for name in &cases {
        let vk = &g[&format!("adv.{name}.vk")];
        let proof = &g[&format!("adv.{name}.proof")];
        let go_accepted = g[&format!("adv.{name}.verdict")] == [1u8];
        let here = judge(vk, proof, &bind);
        if here.is_ok() {
            accepted += 1;
        }
        if here.is_ok() != go_accepted {
            disagreements.push(format!(
                "{name}: Go {}, this {} ({})",
                if go_accepted { "accepted" } else { "refused" },
                if here.is_ok() { "accepted" } else { "refused" },
                here.err().unwrap_or_else(|| "verified".into())
            ));
        }
    }

    assert!(
        disagreements.is_empty(),
        "the classical verifier disagrees with the reference on {} instance(s):\n  {}",
        disagreements.len(),
        disagreements.join("\n  ")
    );
    // A verdict table where nothing verifies would agree with a verifier that
    // is simply broken, so the control has to be named separately.
    assert_eq!(
        accepted, 1,
        "exactly one of these instances is a satisfying assignment; {accepted} were accepted"
    );
    assert!(judge(&g["adv.good.vk"], &g["adv.good.proof"], &bind).is_ok());
}

#[test]
fn a_shielded_transaction_verifies_end_to_end_against_the_go_instance() {
    // The whole classical path, on a permissive chain: Go's transaction bytes,
    // Go's verifying key, Go's proof — parsed here, bound to this chain, and
    // verified.
    use std::collections::HashMap;
    let g = golden();
    let tx = wire::parse_transaction(&g["groth16.tx_wire"]).unwrap();
    assert_eq!(&tx.id[..], &g["groth16.tx_id"][..]);

    let mut keys = HashMap::new();
    keys.insert(
        TransactionType::Transfer.circuit_key(),
        g["groth16.vk_tx"].clone(),
    );
    let pv = lux_zkvm::verifier::ProofVerifier::new(
        false,
        bind(),
        &keys,
        8,
        lux_zkvm::starkfri::Verifier::unbound(),
    )
    .unwrap();
    pv.verify(&tx).expect("Go's proof over Go's transaction");

    // Move one nullifier: the public inputs no longer describe what is spent,
    // and the refusal comes BEFORE any pairing.
    let mut stolen = tx.clone();
    stolen.nullifiers[0] = b"someone-elses-note".to_vec();
    assert!(pv.verify(&stolen).is_err());

    // And a strict-PQ chain refuses the same transaction outright.
    let strict = lux_zkvm::verifier::ProofVerifier::new(
        true,
        bind(),
        &HashMap::new(),
        8,
        lux_zkvm::starkfri::Verifier::unbound(),
    )
    .unwrap();
    assert_eq!(
        strict.verify(&tx),
        Err(lux_zkvm::Error::StrictPqClassicalForbidden)
    );
}

fn tempdir(tag: &str) -> std::path::PathBuf {
    let mut p = std::env::temp_dir();
    p.push(format!(
        "lux-zkvm-{tag}-{}-{:?}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&p).unwrap();
    p
}

// ---- the genesis document -------------------------------------------------

/// Every genesis document the emitter handed to the reference, judged the way
/// the reference judged it.
///
/// A genesis file is an operator's, and one file goes to every implementation
/// of this chain. What it produces — the genesis block's id, the initial state
/// root, the first shielded outputs — has to come out the same, and what it is
/// REFUSED for has to be the same too: a file one node boots and another does
/// not is a chain with one node on it.
#[test]
fn every_genesis_document_gets_the_answer_go_gave_it() {
    let g = golden();
    let mut checked = 0;
    let mut accepted = 0;
    for name in g.keys().filter(|k| k.ends_with(".verdict")) {
        let Some(case) = name
            .strip_prefix("genesis.")
            .and_then(|s| s.strip_suffix(".verdict"))
        else {
            continue;
        };
        checked += 1;
        let doc = &g[&format!("genesis.{case}.doc")];
        let parsed = lux_zkvm::Genesis::parse(doc);
        let go_accepted = g[name] == [1u8];
        assert_eq!(
            parsed.is_ok(),
            go_accepted,
            "genesis {case}: go {} this document, here it is {} — {:?}",
            if go_accepted { "accepted" } else { "refused" },
            if parsed.is_ok() {
                "accepted"
            } else {
                "refused"
            },
            String::from_utf8_lossy(doc)
        );
        let Ok(parsed) = parsed else { continue };
        accepted += 1;

        let stamp = i64::from_be_bytes(g[&format!("genesis.{case}.stamp")][..].try_into().unwrap());
        assert_eq!(parsed.timestamp, stamp, "genesis {case}: the timestamp");
        let ntx =
            u32::from_be_bytes(g[&format!("genesis.{case}.ntx")][..].try_into().unwrap()) as usize;
        assert_eq!(
            parsed.initial_txs.len(),
            ntx,
            "genesis {case}: how many transactions"
        );

        for (i, tx) in parsed.initial_txs.iter().enumerate() {
            let p = format!("genesis.{case}.tx{i}");
            // The wire is every decoded field at once: one byte wrong in any
            // of them, and these do not match.
            assert_eq!(
                wire::marshal_transaction(tx),
                g[&format!("{p}.wire")],
                "{p}: the fields"
            );
            assert_eq!(
                &tx.id[..],
                &g[&format!("{p}.id_carried")][..],
                "{p}: the id the file names"
            );
            assert_eq!(
                &tx.compute_id()[..],
                &g[&format!("{p}.id_computed")][..],
                "{p}: the id its content derives"
            );
            // The UTXO records genesis seeding writes — keyed on the CARRIED
            // id. A node that recomputed it would seed the shielded set under
            // keys no other node looks in.
            for (j, out) in tx.outputs.iter().enumerate() {
                let u = Utxo {
                    tx_id: tx.id,
                    output_index: j as u32,
                    commitment: out.commitment.clone(),
                    ciphertext: out.encrypted_note.clone(),
                    ephemeral_pk: out.ephemeral_pub_key.clone(),
                    height: 0,
                };
                assert_eq!(u.marshal(), g[&format!("{p}.utxo{j}")], "{p}: utxo {j}");
            }
        }

        // The initial state root, and the identity of block zero.
        let dir = tempdir(&format!("golden-genesis-{case}"));
        let view = lux_zkvm::db::View::new(std::sync::Arc::new(
            lux_zkvm::db::Db::open(dir.join("log")).unwrap(),
        ));
        let root = Root::open(&view).unwrap();
        assert_eq!(
            root.after(&parsed.initial_txs).unwrap(),
            g[&format!("genesis.{case}.root")],
            "genesis {case}: the initial state root"
        );
        let block = Block::new(
            EMPTY,
            0,
            parsed.timestamp,
            parsed
                .initial_txs
                .iter()
                .cloned()
                .map(|t| t.with_id())
                .collect(),
            Vec::new(),
        );
        assert_eq!(
            &block.compute_id(&bind())[..],
            &g[&format!("genesis.{case}.block_id")][..],
            "genesis {case}: the genesis block's id"
        );
        std::fs::remove_dir_all(&dir).ok();
    }
    assert!(
        checked >= 25 && accepted >= 14,
        "the genesis corpus went missing: {checked} documents, {accepted} of them accepted"
    );
}

// ---- the verifier precompiles ---------------------------------------------

/// Each precompile input the emitter ran through the reference, answered here.
///
/// `Run` returns a byte AND an error, and they are different facts: `0x00`
/// with no error is "that proof does not verify", and an error is "this call
/// could not be judged" — in an EVM, a `false` against a revert. Both are
/// compared, because a port that erred where the reference returned a verdict
/// would make the same call succeed on one node and revert on another.
#[test]
fn every_precompile_input_gets_the_answer_go_gave_it() {
    let g = golden();
    let mut registry = lux_zkvm::precompiles::Registry::new();
    lux_zkvm::precompiles::register(
        &mut registry,
        false,
        lux_zkvm::starkfri::Verifier::unbound(),
    );
    let mut checked = 0;
    for name in g.keys().filter(|k| k.ends_with(".out")) {
        let Some(case) = name
            .strip_prefix("pc.")
            .and_then(|s| s.strip_suffix(".out"))
        else {
            continue;
        };
        let addr = match case.split('_').next().unwrap() {
            "groth16" => lux_zkvm::precompiles::GROTH16,
            "plonk" => lux_zkvm::precompiles::PLONK,
            "stark" => lux_zkvm::precompiles::STARK,
            "halo2" => lux_zkvm::precompiles::HALO2,
            "nova" => lux_zkvm::precompiles::NOVA,
            other => panic!("no address for {other}"),
        };
        checked += 1;
        let c = registry.get(addr).unwrap();
        let input = &g[&format!("pc.{case}.input")];
        let (out, err) = c.run(input);
        assert_eq!(out, g[name], "{case}: the byte");
        let go_errored = g[&format!("pc.{case}.err")] == [1u8];
        assert_eq!(
            err.is_err(),
            go_errored,
            "{case}: go {} this call, here it {} — {err:?}",
            if go_errored {
                "could not judge"
            } else {
                "judged"
            },
            if err.is_err() { "errs" } else { "answers" }
        );
        assert_eq!(
            c.required_gas(input),
            u64::from_be_bytes(g[&format!("pc.{case}.gas")][..].try_into().unwrap()),
            "{case}: the gas"
        );
    }
    assert!(
        checked >= 18,
        "the precompile corpus went missing: {checked}"
    );
}
