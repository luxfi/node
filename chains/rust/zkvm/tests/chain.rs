// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The chain, run.
//!
//! Everything here goes through the real store on a real file, because the
//! properties being checked are exactly the ones an in-memory chain cannot
//! have: that a node which stops and starts again remembers what it spent.

use std::sync::Arc;

use lux_zkvm::error::Error;
use lux_zkvm::ids::{prefixed, EMPTY};
use lux_zkvm::starkfri;
use lux_zkvm::store::Decision as _;
use lux_zkvm::tx::{ShieldedOutput, Transaction, TransactionType, ZkProof};
use lux_zkvm::vm::{Applied, Open, ZkVm};
use lux_zkvm::{fee, wire};

const NETWORK: u32 = 96369;

/// A STARK/FRI backend that accepts. It stands where the audited verifier
/// stands, so the rest of the chain can be exercised on the profile it
/// actually runs — strict-PQ — rather than on the classical path a real
/// Z-Chain refuses.
struct Accepts;
impl starkfri::Backend for Accepts {
    fn verify(&self, _: u8, _: &[u8], _: &[u8]) -> lux_zkvm::Result<bool> {
        Ok(true)
    }
}

struct Dir(std::path::PathBuf);

impl Dir {
    fn new(tag: &str) -> Dir {
        let mut p = std::env::temp_dir();
        p.push(format!(
            "lux-zkvm-chain-{tag}-{}-{:?}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        std::fs::create_dir_all(&p).unwrap();
        Dir(p)
    }
    fn log(&self) -> std::path::PathBuf {
        self.0.join("chain")
    }
}

impl Drop for Dir {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

fn open(dir: &Dir, genesis: &[u8], bound: bool) -> ZkVm {
    ZkVm::open(Open {
        chain_id: prefixed(&[0xAA, 0xBB, 0xCC]),
        network_id: NETWORK,
        path: dir.log(),
        genesis: genesis.to_vec(),
        config: Vec::new(), // no config: the strict-PQ default
        stark: if bound {
            starkfri::Verifier::bound(Arc::new(Accepts))
        } else {
            starkfri::Verifier::unbound()
        },
    })
    .expect("the chain opens")
}

/// A shielded transfer paying the floor, whose proof is a STARK envelope.
fn transfer(nullifier: &[u8], commitment: &[u8], expiry: u64) -> Transaction {
    Transaction {
        kind: TransactionType::Transfer as u8,
        version: 1,
        fee: fee::MIN_TX_FEE_FLOOR,
        expiry,
        nullifiers: vec![nullifier.to_vec()],
        outputs: vec![ShieldedOutput {
            commitment: commitment.to_vec(),
            encrypted_note: b"note".to_vec(),
            ephemeral_pub_key: b"epk".to_vec(),
            output_proof: b"range".to_vec(),
        }],
        proof: Some(ZkProof {
            proof_type: "stark".into(),
            proof_data: b"P3Q1 a proof the bound verifier accepts".to_vec(),
            public_inputs: Vec::new(),
        }),
        ..Default::default()
    }
    .with_id()
}

#[test]
fn a_chain_defaults_to_strict_pq_and_fails_closed_without_a_verifier() {
    let d = Dir::new("strict");
    let vm = open(&d, b"", false);
    assert!(
        vm.strict_pq(),
        "the Z-Chain is the strict-PQ chain by default"
    );

    // Nothing is admitted: no classical fallback, and no STARK binding.
    let tx = transfer(b"n1", b"c1", 100);
    assert_eq!(vm.admit(&tx, 1), Err(Error::VerifierNotRegistered));

    // A classical proof is refused by name, before anything else is looked at.
    let mut classical = tx.clone();
    classical.proof.as_mut().unwrap().proof_type = "groth16".into();
    assert_eq!(
        vm.admit(&classical, 1),
        Err(Error::StrictPqClassicalForbidden)
    );

    // And the classical precompiles are not there to be called.
    assert!(vm
        .precompiles()
        .get(lux_zkvm::precompiles::GROTH16)
        .is_err());
    assert!(vm.precompiles().get(lux_zkvm::precompiles::STARK).is_ok());
}

#[test]
fn the_fee_floor_is_checked_before_the_pool() {
    let d = Dir::new("fee");
    let vm = open(&d, b"", true);
    let mut cheap = transfer(b"n1", b"c1", 100);
    cheap.fee = fee::MIN_TX_FEE_FLOOR - 1;
    assert!(matches!(
        vm.submit(&cheap),
        Err(Error::InsufficientFee { .. })
    ));
    assert_eq!(vm.mempool().len(), 0, "a refused transaction takes no slot");

    assert!(vm.submit(&transfer(b"n1", b"c1", 100)).is_ok());
    assert_eq!(vm.mempool().len(), 1);
}

#[test]
fn a_transaction_becomes_a_block_and_the_block_becomes_state() {
    let d = Dir::new("lifecycle");
    let vm = open(&d, b"", true);
    let genesis_tip = vm.last_accepted();

    vm.submit(&transfer(b"n1", b"c1", 100)).unwrap();
    let built = ZkVm::build(&vm).expect("a block is built");
    assert_eq!(built.height(), 1);
    assert_eq!(built.parent(), genesis_tip);

    // Verify is idempotent — the engine calls it more than once.
    vm.verify(&built).unwrap();
    vm.verify(&built).unwrap();

    vm.accept(&built.id()).unwrap();
    assert_eq!(vm.last_accepted(), built.id());
    assert_eq!(vm.id_at_height(1).unwrap(), built.id());
    assert_eq!(
        vm.mempool().len(),
        0,
        "an accepted transaction leaves the pool"
    );

    // The state moved: the note is spent, the commitment exists.
    let health = vm.health();
    assert_eq!(health["details"]["nullifierCount"], "1");
    assert_eq!(health["details"]["utxoCount"], "1");
    assert_eq!(health["details"]["lastBlockHeight"], "1");
}

#[test]
fn a_node_that_stops_remembers_what_it_spent() {
    // The whole reason the store is a file. A node that forgets comes back
    // willing to spend a note it already spent.
    let d = Dir::new("restart");
    let block_id;
    {
        let vm = open(&d, b"", true);
        vm.submit(&transfer(b"n1", b"c1", 100)).unwrap();
        let built = ZkVm::build(&vm).unwrap();
        vm.verify(&built).unwrap();
        vm.accept(&built.id()).unwrap();
        block_id = built.id();
    }

    let vm = open(&d, b"", true);
    assert_eq!(vm.last_accepted(), block_id, "the tip comes back");
    assert_eq!(vm.id_at_height(1).unwrap(), block_id);
    assert_eq!(vm.health()["details"]["nullifierCount"], "1");
    assert_eq!(vm.health()["details"]["utxoCount"], "1");

    // And the spend still stands.
    assert_eq!(
        vm.admit(&transfer(b"n1", b"c2", 100), 2),
        Err(Error::NullifierSpent)
    );

    // The block itself reads back, byte for byte.
    let back = ZkVm::get(&vm, &block_id).unwrap();
    assert_eq!(back.id(), block_id);
    assert_eq!(back.height(), 1);
}

#[test]
fn one_note_cannot_be_spent_twice_across_blocks() {
    let d = Dir::new("double");
    let vm = open(&d, b"", true);
    vm.submit(&transfer(b"n1", b"c1", 100)).unwrap();
    let one = ZkVm::build(&vm).unwrap();
    vm.verify(&one).unwrap();
    vm.accept(&one.id()).unwrap();

    // A second transaction spending the same note: refused by the predicate,
    // so it never reaches a block.
    let again = transfer(b"n1", b"c2", 100);
    assert_eq!(vm.admit(&again, 2), Err(Error::NullifierSpent));
    vm.submit(&again).unwrap(); // the pool does not know the chain's spent set
    assert_eq!(
        ZkVm::build(&vm).err(),
        Some(Error::NoTransactions),
        "assembly drops what verify would refuse"
    );
}

#[test]
fn one_note_cannot_be_spent_twice_inside_one_block() {
    let d = Dir::new("dup-in-block");
    let vm = open(&d, b"", true);
    // Two different transactions naming the same nullifier. The pool refuses
    // the second, so a block built here cannot carry both...
    vm.submit(&transfer(b"n1", b"c1", 100)).unwrap();
    assert!(vm.submit(&transfer(b"n1", b"c2", 100)).is_err());

    // ...and one that ARRIVED carrying both is refused on its shape.
    let a = transfer(b"n1", b"c1", 100);
    let b = transfer(b"n1", b"c2", 100);
    let bad = lux_zkvm::Block::new(vm.last_accepted(), 1, 0, vec![a, b], vec![]);
    let bad = Arc::new(Applied::Block(bad));
    assert_eq!(vm.verify(&bad), Err(Error::DuplicateNullifier));
}

#[test]
fn a_block_that_does_not_extend_the_tip_is_refused() {
    let d = Dir::new("fork");
    let vm = open(&d, b"", true);
    let genesis = vm.last_accepted();

    vm.submit(&transfer(b"n1", b"c1", 100)).unwrap();
    let one = ZkVm::build(&vm).unwrap();
    vm.verify(&one).unwrap();
    vm.accept(&one.id()).unwrap();

    // A block whose parent is the OLD tip. Its height is right and its parent
    // exists; accepting it would rewind the chain.
    let sibling = lux_zkvm::Block::new(genesis, 1, 0, vec![], vm.state_root(&[]).unwrap());
    let sibling = Arc::new(Applied::Block(sibling));
    assert!(matches!(vm.verify(&sibling), Err(Error::NotOnTip(_))));
}

#[test]
fn a_block_claiming_the_wrong_state_root_is_refused() {
    let d = Dir::new("root");
    let vm = open(&d, b"", true);
    let tx = transfer(b"n1", b"c1", 100);
    let honest = vm.state_root(std::slice::from_ref(&tx)).unwrap();

    let good = Arc::new(Applied::Block(lux_zkvm::Block::new(
        vm.last_accepted(),
        1,
        0,
        vec![tx.clone()],
        honest.clone(),
    )));
    assert!(vm.verify(&good).is_ok());

    let mut lying = honest;
    lying[0] ^= 0xFF;
    let bad = Arc::new(Applied::Block(lux_zkvm::Block::new(
        vm.last_accepted(),
        1,
        0,
        vec![tx],
        lying,
    )));
    assert_eq!(vm.verify(&bad), Err(Error::InvalidStateRoot));
}

#[test]
fn a_rejected_block_returns_its_transactions_and_leaves_no_state() {
    let d = Dir::new("reject");
    let vm = open(&d, b"", true);
    vm.submit(&transfer(b"n1", b"c1", 100)).unwrap();
    let built = ZkVm::build(&vm).unwrap();
    vm.verify(&built).unwrap();

    vm.reject(&built.id()).unwrap();
    assert_eq!(vm.mempool().len(), 1, "the transaction is waiting again");
    assert_eq!(vm.health()["details"]["nullifierCount"], "0");
    assert_eq!(vm.last_accepted(), vm.last_accepted());

    // And it can be built and accepted afterwards.
    let again = ZkVm::build(&vm).unwrap();
    vm.verify(&again).unwrap();
    vm.accept(&again.id()).unwrap();
    assert_eq!(vm.health()["details"]["nullifierCount"], "1");
}

#[test]
fn a_genesis_allocation_happens_once_and_survives() {
    // The trap this closes: a seed that recorded no tip left the chain
    // reporting itself fresh on every boot, allocating again, and failing on
    // the duplicate its own state refuses.
    //
    // The document is the reference's: a transaction is an OBJECT, its bytes
    // are base64, and it names the id its outputs are found under.
    let genesis = br#"{
        "timestamp": 1700000000,
        "initialTransactions": [
            {
                "id": "11111111111111111111111111111111Z",
                "type": 1,
                "outputs": [{"commitment": "Z2VuZXNpcy1ub3Rl"}]
            }
        ]
    }"#;

    let d = Dir::new("genesis");
    let first = open(&d, genesis, true);
    assert_eq!(first.health()["details"]["utxoCount"], "1");
    let tip = first.last_accepted();
    drop(first);

    // The seeded record carries the id the FILE named, not one recomputed
    // from the transaction's content. A wallet that goes looking for its
    // genesis note by that id finds it here and on a reference node alike.
    {
        let view = lux_zkvm::db::View::new(Arc::new(lux_zkvm::db::Db::open(d.log()).unwrap()));
        let utxos = lux_zkvm::utxo::Utxos::open(&view).unwrap();
        let seeded = utxos.get(&view, b"genesis-note").unwrap();
        let mut named = EMPTY;
        named[31] = b'Z';
        assert_eq!(seeded.tx_id, named, "the id the genesis document named");
        assert_ne!(
            seeded.tx_id,
            Transaction {
                kind: TransactionType::Mint as u8,
                outputs: vec![ShieldedOutput {
                    commitment: b"genesis-note".to_vec(),
                    ..Default::default()
                }],
                ..Default::default()
            }
            .compute_id(),
            "and not the one its content derives — the two are different here"
        );
    }

    // The second boot must not allocate again — and must not fail.
    let second = open(&d, genesis, true);
    assert_eq!(second.health()["details"]["utxoCount"], "1");
    assert_eq!(second.last_accepted(), tip, "the genesis id is stable");

    // A genesis that names no time is stamped 0, not "now": the id is the
    // same on every boot, which is what makes it the same chain.
    let e = Dir::new("genesis-timeless");
    let a = open(&e, b"{}", true).last_accepted();
    let f = Dir::new("genesis-timeless-2");
    let b = open(&f, b"{}", true).last_accepted();
    assert_eq!(a, b);
}

#[test]
fn the_chain_binding_makes_another_chains_block_a_stranger() {
    let d = Dir::new("bind-a");
    let a = open(&d, b"", true);
    let e = Dir::new("bind-b");
    let b = ZkVm::open(Open {
        chain_id: prefixed(&[0xDD]), // a different chain
        network_id: NETWORK,
        path: e.log(),
        genesis: Vec::new(),
        config: Vec::new(),
        stark: starkfri::Verifier::bound(Arc::new(Accepts)),
    })
    .unwrap();

    assert_ne!(a.chain_binding(), b.chain_binding());
    assert_ne!(
        a.last_accepted(),
        b.last_accepted(),
        "an identical genesis on two chains must not be one block"
    );
}

#[test]
fn a_vertex_is_held_to_what_a_block_is_held_to() {
    let d = Dir::new("vertex");
    let vm = open(&d, b"", true);
    vm.submit(&transfer(b"n1", b"c1", 100)).unwrap();

    let v = vm.build_vertex().expect("a vertex is built");
    assert_eq!(v.height(), 1);
    vm.verify(&v).unwrap();
    vm.accept(&v.id()).unwrap();
    assert_eq!(vm.health()["details"]["nullifierCount"], "1");

    // A vertex naming no parent at an absurd height is refused — accepting one
    // set the store's height to 2^40 and left the chain unable to propose a
    // child ever again.
    let absurd = Arc::new(Applied::Vertex(lux_zkvm::Vertex::new(
        1 << 40,
        0,
        vec![],
        vec![],
    )));
    assert!(matches!(vm.verify(&absurd), Err(Error::NotOnTip(_))));

    // And asking for a vertex by BLOCK id is a miss rather than something the
    // caller cannot use.
    assert!(matches!(vm.block(&v.id()), Err(Error::NoBlock(_))));
}

#[test]
fn the_node_seam_answers() {
    let d = Dir::new("seam");
    let vm = open(&d, b"", true);
    let seam: &dyn lux_zkvm::host::Vm = &vm;

    assert_eq!(seam.name(), "Z");
    assert_eq!(seam.version(), lux_zkvm::vm::VERSION);
    assert!(seam.health().is_ok());
    assert_eq!(seam.last_accepted(), vm.last_accepted());

    // Nothing to build is Empty, not an invalid block.
    assert!(matches!(seam.build(), Err(lux_zkvm::host::Error::Empty)));

    vm.submit(&transfer(b"n1", b"c1", 100)).unwrap();
    let built = seam.build().unwrap();
    assert_eq!(built.height(), 1);
    assert_ne!(
        built.state_root(),
        EMPTY,
        "a certificate over an uncomputed root certifies nothing"
    );
    assert_ne!(built.payload_root(), EMPTY);

    // The bytes go out and come back the same block.
    let raw = built.bytes();
    let parsed = seam.parse(&raw).unwrap();
    assert_eq!(parsed.id(), built.id());
    assert_eq!(parsed.bytes(), raw);

    seam.verify(&built.id()).unwrap();
    seam.set_preference(&built.id()).unwrap();
    seam.accept(&built.id()).unwrap();
    assert_eq!(seam.last_accepted(), built.id());
    assert_eq!(seam.block_id_at(1).unwrap(), built.id());

    // A block nobody has is NotFound, which is what makes the engine fetch it
    // rather than drop the peer.
    assert!(matches!(
        seam.get(&prefixed(&[0xAB])),
        Err(lux_zkvm::host::Error::NotFound)
    ));
    // And bytes that are not a block are Malformed, which is what makes it
    // drop the peer rather than fetch again.
    assert!(matches!(
        seam.parse(b"not a frame"),
        Err(lux_zkvm::host::Error::Malformed(_))
    ));
}

#[test]
fn the_json_surface_answers_the_same_routes_the_go_node_mounts() {
    let d = Dir::new("json");
    let vm = open(&d, b"", true);
    let seam: &dyn lux_zkvm::host::Vm = &vm;

    let tx = transfer(b"n1", b"c1", 100);
    let raw = lux_zkvm::hex::encode(&wire::marshal_transaction(&tx));
    let sent = seam
        .call("sendTransaction", &serde_json::json!({ "tx": raw }))
        .unwrap();
    assert_eq!(sent["success"], true);
    assert_eq!(sent["txID"], lux_zkvm::ids::hex(&tx.id));

    let pending = seam
        .call(
            "getTransaction",
            &serde_json::json!({ "txID": sent["txID"] }),
        )
        .unwrap();
    assert_eq!(pending["status"], "pending");

    // Not spent, and the answer says so with a height of zero.
    let spent = seam
        .call(
            "isNullifierSpent",
            &serde_json::json!({ "nullifier": "6e31" }),
        )
        .unwrap();
    assert_eq!(spent["isSpent"], false);

    let built = seam.build().unwrap();
    seam.verify(&built.id()).unwrap();
    seam.accept(&built.id()).unwrap();

    let spent = seam
        .call(
            "isNullifierSpent",
            &serde_json::json!({ "nullifier": "6e31" }),
        )
        .unwrap();
    assert_eq!(spent["isSpent"], true);
    assert_eq!(spent["height"], 1);

    let latest = seam.call("getLatestBlock", &serde_json::json!({})).unwrap();
    assert_eq!(latest["height"], 1);
    assert_eq!(latest["txCount"], 1);
    assert_eq!(latest["id"], lux_zkvm::ids::hex(&built.id()));

    let count = seam.call("getUTXOCount", &serde_json::json!({})).unwrap();
    assert_eq!(count["count"], 1);

    let utxo = seam
        .call("getUTXO", &serde_json::json!({ "commitment": "6331" }))
        .unwrap();
    assert_eq!(utxo["height"], 1);
    assert_eq!(utxo["commitment"], "6331");

    assert!(seam.call("getStatus", &serde_json::json!({})).is_ok());
    assert!(seam.call("getProofStats", &serde_json::json!({})).is_ok());

    // A method that does not exist says so.
    assert!(matches!(
        seam.call("mintMeSomething", &serde_json::json!({})),
        Err(lux_zkvm::host::Error::NoMethod(_))
    ));
}

#[test]
fn a_transaction_over_the_cap_never_reaches_the_parser() {
    let d = Dir::new("cap");
    let vm = open(&d, b"", true);
    let seam: &dyn lux_zkvm::host::Vm = &vm;
    let huge = "00".repeat(lux_zkvm::tx::MAX_TX_SIZE + 1);
    assert!(seam
        .call("sendTransaction", &serde_json::json!({ "tx": huge }))
        .is_err());
}

#[test]
fn an_expired_transaction_is_dropped_rather_than_held_forever() {
    let d = Dir::new("expiry");
    let vm = open(&d, b"", true);

    // One that expires at height 1, one that lives longer.
    vm.submit(&transfer(b"n-old", b"c-old", 1)).unwrap();
    vm.submit(&transfer(b"n-new", b"c-new", 1000)).unwrap();
    assert_eq!(vm.mempool().len(), 2);

    // Both are still admissible at height 1.
    let built = ZkVm::build(&vm).unwrap();
    assert_eq!(built.txs().len(), 2);
    vm.verify(&built).unwrap();
    vm.accept(&built.id()).unwrap();

    // At height 2 the first can never be built, so nothing is left holding a
    // slot for it.
    vm.submit(&transfer(b"n-old-again", b"c2", 1)).unwrap();
    vm.mempool().prune_expired(2);
    assert_eq!(vm.mempool().len(), 0);
}
