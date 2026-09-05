// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The chain inside the node.
//!
//! Everything in `src/` proves the X-Chain is right about itself. This proves
//! it is REACHABLE: that `lux-rs/node` — the real one, not a stand-in — holds
//! this chain behind its own `Vm` trait, names it, serves it over its own
//! JSON-RPC, drives it over its own sealed plugin link, and reads out of it the
//! two roots a certificate is made of.
//!
//! Nothing here is a mock. `lux_node::Rpc`, `lux_node::plugin::{Client,
//! Server}`, `lux_node::Engine` and `lux_node::pq::Identity` are the node's own
//! types, and the only thing this file supplies is the chain.

use std::io::{Read, Write};
use std::net::TcpStream;
use std::sync::Arc;

use lux_node::vm::{Error as HostError, Vm as _};

use lux_xvm::fx::secp256k1::{address_of, MintOutput, TransferInput, TransferOutput};
use lux_xvm::fx::{self, FxIn, Input, Owners, State};
use lux_xvm::ids::{self, Id, ShortId};
use lux_xvm::txs::executor::{AtomicRequests, Config, Net, SharedMemory};
use lux_xvm::txs::{BaseTx, CreateAssetTx, InitialState, Tx, Unsigned};
use lux_xvm::utxo::{Asset, BaseTxFields, TransferableInput, TransferableOutput, UtxoId};
use lux_xvm::vm::{FixedClock, Genesis, Xvm};

const NETWORK_ID: u32 = 10;
const AT: u64 = 1_700_000_000;

fn chain_id() -> Id {
    ids::prefixed(&[5])
}

fn key(n: u8) -> [u8; 32] {
    let mut k = [0u8; 32];
    k[31] = n;
    k
}

fn addr(n: u8) -> ShortId {
    address_of(&key(n)).expect("an address for a key")
}

struct OneNet;
impl Net for OneNet {
    fn network_of(&self, _: &Id) -> lux_xvm::Result<Id> {
        Ok(ids::prefixed(&[0xAB]))
    }
}

struct NoMemory;
impl SharedMemory for NoMemory {
    fn get(&self, _: &Id, _: &[Vec<u8>]) -> lux_xvm::Result<Vec<Vec<u8>>> {
        Ok(Vec::new())
    }
    fn apply(&self, _: &[(Id, AtomicRequests)]) -> lux_xvm::Result<()> {
        Ok(())
    }
}

/// The one genesis transaction: it creates the asset, and its own id becomes
/// the asset's id.
fn genesis_tx() -> Tx {
    Tx::new(Unsigned::CreateAsset(CreateAssetTx {
        base: BaseTxFields {
            network_id: NETWORK_ID,
            blockchain_id: chain_id(),
            outs: vec![],
            ins: vec![],
            memo: vec![],
        },
        name: "Asset".into(),
        symbol: "AST".into(),
        denomination: 0,
        states: vec![InitialState {
            fx_index: 0,
            outs: vec![
                State::Mint(MintOutput {
                    owners: Owners::new(1, vec![addr(1)]),
                }),
                State::Transfer(TransferOutput {
                    amt: 1_000,
                    owners: Owners::new(1, vec![addr(1)]),
                }),
            ],
        }],
    }))
}

fn a_chain() -> (Arc<Xvm>, Tx) {
    let g = genesis_tx();
    let vm = Xvm::new(
        Genesis {
            network_id: NETWORK_ID,
            chain_id: chain_id(),
            net_id: ids::prefixed(&[0xAB]),
            fee_asset_id: g.id(),
            config: Config {
                tx_fee: 0,
                create_asset_tx_fee: 0,
            },
            txs: vec![g.clone()],
            timestamp: AT,
        },
        Arc::new(FixedClock::new(AT)),
        Arc::new(lux_xvm::gossip::Fixed(vec![0x5A, 0xA5, 0x11, 0x22])),
        Some(Arc::new(OneNet)),
        Some(Arc::new(NoMemory)),
    )
    .expect("a chain from its genesis");
    vm.set_bootstrapped(true);
    (Arc::new(vm), g)
}

/// Spend the genesis transfer output into one output owned by `n`.
fn spend_genesis(g: &Tx, n: u8) -> Tx {
    let mut tx = Tx::new(Unsigned::Base(BaseTx {
        base: BaseTxFields {
            network_id: NETWORK_ID,
            blockchain_id: chain_id(),
            outs: vec![TransferableOutput {
                asset: Asset { id: g.id() },
                out: State::Transfer(TransferOutput {
                    amt: 1_000,
                    owners: Owners::new(1, vec![addr(n)]),
                }),
            }],
            ins: vec![TransferableInput {
                utxo_id: UtxoId::new(g.id(), 1),
                asset: Asset { id: g.id() },
                input: FxIn::Transfer(TransferInput {
                    amt: 1_000,
                    input: Input {
                        sig_indices: vec![0],
                    },
                }),
            }],
            memo: vec![],
        },
    }));
    tx.sign(fx::Family::Secp256k1, &[vec![key(1)]])
        .expect("sign with the owner's key");
    tx
}

// ------------------------------------------------------- the node holds it --

#[test]
fn the_node_holds_this_chain_behind_its_own_trait() {
    let (vm, _) = a_chain();
    // The cast is the whole claim: `Arc<dyn lux_node::vm::Vm>` is what the
    // node's chain map, its engine and its plugin server all take.
    let held: Arc<dyn lux_node::Vm> = vm;
    assert_eq!(held.name(), "X");
    assert_eq!(held.version(), env!("CARGO_PKG_VERSION"));
    held.health().expect("a fresh chain is healthy");
}

#[test]
fn the_nodes_registry_finds_the_chain_under_its_own_name() {
    let (vm, _) = a_chain();
    let rpc = lux_node::Rpc::new(NETWORK_ID as u64, "lux-xvm-test").with(vm.clone());
    let found = rpc
        .chain("X")
        .expect("registered under the name it reports");
    assert_eq!(found.name(), "X");
    assert_eq!(found.last_accepted(), vm.last_accepted());
    // The node lower-cases what a chain calls itself, and answers either way.
    assert!(rpc.chain("x").is_some());
    assert!(rpc.chain("P").is_none(), "only what was registered");
}

// ------------------------------------------------- the node serves it, live --

/// One JSON-RPC request against a running node, over a real socket.
fn post(addr: std::net::SocketAddr, path: &str, body: &str) -> serde_json::Value {
    let mut s = TcpStream::connect(addr).expect("connect to the node");
    let req = format!(
        "POST {path} HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\n\
         Content-Length: {}\r\nConnection: close\r\n\r\n{body}",
        body.len()
    );
    s.write_all(req.as_bytes()).expect("write the request");
    let mut raw = String::new();
    s.read_to_string(&mut raw).expect("read the answer");
    let (_head, json) = raw
        .split_once("\r\n\r\n")
        .expect("a body after the headers");
    serde_json::from_str(json).expect("the node answers JSON")
}

#[test]
fn the_nodes_own_rpc_server_routes_a_call_to_this_chain() {
    let (vm, g) = a_chain();
    let serving = lux_node::Rpc::new(NETWORK_ID as u64, "lux-xvm-test")
        .with(vm.clone())
        .listen("127.0.0.1:0".parse().expect("a bind address"))
        .expect("the node binds");
    let addr = serving.addr();

    // A method only this chain answers, reached through the node's X route.
    let got = post(
        addr,
        "/v1/chain/x",
        r#"{"jsonrpc":"2.0","id":1,"method":"xvm.getBlockchainID","params":{}}"#,
    );
    assert_eq!(got["result"], serde_json::json!(ids::hex(&chain_id())));

    // And a real transaction, offered over that same socket.
    let tx = spend_genesis(&g, 2);
    let body = format!(
        r#"{{"jsonrpc":"2.0","id":2,"method":"xvm.issueTx","params":{{"tx":"{}"}}}}"#,
        ids::hex(tx.bytes())
    );
    let issued = post(addr, "/v1/chain/x", &body);
    assert_eq!(
        issued["result"]["txID"],
        serde_json::json!(ids::hex(&tx.id()))
    );
    assert_eq!(vm.pending(), 1, "the node's socket reached the mempool");

    // The node reports this chain among the ones it runs.
    let info = post(
        addr,
        "/v1/info",
        r#"{"jsonrpc":"2.0","id":3,"method":"info.getBlockchainID","params":[]}"#,
    );
    assert_eq!(info["result"]["blockchains"], serde_json::json!(["X"]));

    // A method the chain does not answer is the node's own JSON-RPC error, not
    // a panic and not a 500 with a chain string in it.
    let nope = post(
        addr,
        "/v1/chain/x",
        r#"{"jsonrpc":"2.0","id":4,"method":"xvm.nonsense","params":{}}"#,
    );
    assert_eq!(nope["error"]["code"], serde_json::json!(-32601));
}

// ------------------------------------------- the node drives it, over a link --

#[test]
fn the_node_drives_this_chain_across_its_sealed_plugin_link() {
    let (vm, g) = a_chain();
    let genesis_id = vm.last_accepted();

    let plugin_id = Arc::new(lux_node::Identity::generate().expect("the chain's identity"));
    let node_id = lux_node::Identity::generate().expect("the node's identity");

    let server = lux_node::plugin::Server::new(
        vm.clone() as Arc<dyn lux_node::Vm>,
        plugin_id.clone(),
        genesis_id,
    );
    let addr = server
        .listen("127.0.0.1:0".parse().expect("a bind address"))
        .expect("the chain binds");

    let client =
        lux_node::plugin::Client::connect("X", addr, &node_id, Some(plugin_id.node()), genesis_id)
            .expect("the node reaches the chain");

    // Offer a transaction through the link, then build, verify and accept it —
    // every one of these crosses the socket as a message and comes back.
    let tx = spend_genesis(&g, 2);
    client
        .call(
            "xvm.issueTx",
            &serde_json::json!({ "tx": ids::hex(tx.bytes()) }),
        )
        .expect("a transaction crosses the link");

    let built = client.build().expect("the far end builds a block");
    assert_eq!(built.height(), 1);
    assert_eq!(built.parent(), genesis_id);

    let id = built.id();
    client.verify(&id).expect("the far end verifies it");
    client.accept(&id).expect("the far end accepts it");
    assert_eq!(client.last_accepted(), id);
    assert_eq!(
        vm.last_accepted(),
        id,
        "what the link accepted is what the chain accepted"
    );

    // The block reads back through the link as the same block.
    let again = client.get(&id).expect("the far end still has it");
    assert_eq!(again.bytes(), built.bytes());
    assert_eq!(again.state_root(), built.state_root());
    assert_eq!(client.block_id_at(1).expect("height index"), id);
}

#[test]
fn a_node_running_another_genesis_never_reaches_this_chain() {
    let (vm, _) = a_chain();
    let plugin_id = Arc::new(lux_node::Identity::generate().expect("the chain's identity"));
    let node_id = lux_node::Identity::generate().expect("the node's identity");
    let server = lux_node::plugin::Server::new(
        vm.clone() as Arc<dyn lux_node::Vm>,
        plugin_id.clone(),
        vm.last_accepted(),
    );
    let addr = server
        .listen("127.0.0.1:0".parse().expect("a bind address"))
        .expect("the chain binds");

    let refused = lux_node::plugin::Client::connect(
        "X",
        addr,
        &node_id,
        Some(plugin_id.node()),
        ids::prefixed(&[0xDE, 0xAD]),
    )
    .err()
    .expect("a genesis mismatch must not connect");
    assert!(
        matches!(&refused, HostError::Invalid(why) if why.contains("genesis")),
        "{refused:?}"
    );
}

// ------------------------------------------ what a certificate is made of --

#[test]
fn the_statement_a_validator_signs_is_built_from_this_chains_own_block() {
    use blst::min_pk::SecretKey;

    let (vm, g) = a_chain();
    vm.issue(spend_genesis(&g, 2)).expect("offer a transaction");
    let block = vm.build().expect("a block to certify");

    // A one-validator committee, published the way a genesis file publishes
    // one: an ML-DSA name, a BLS voting key, and a proof of possession.
    let identity = lux_node::Identity::generate().expect("identity");
    let sk = SecretKey::key_gen(&[7u8; 32], &[]).expect("a voting key");
    let pk = sk.sk_to_pk().compress();
    let proof = lux_consensus::pop::sign(&sk, &identity.node(), &pk);
    let line = lux_node::engine::Committee::line(identity.public().as_bytes(), &pk, &proof);
    let committee = lux_node::engine::Committee::read(&line).expect("a committee of one");

    let engine = lux_node::Engine::new(identity.node(), sk, &committee, 1, chain_id())
        .expect("an engine over this chain");
    assert_eq!(engine.quorum(), 1, "a chain of one certifies with its vote");

    let position = engine.position(block.as_ref());
    assert_eq!(position.chain_id, chain_id());
    assert_eq!(position.height, 1);
    assert_eq!(position.block_id, block.id());
    assert_eq!(position.parent_id, vm.last_accepted());

    // The two fields that make this a chain rather than a harness: the root of
    // the state the block leaves behind, and the root of what it carries. Both
    // are the chain's own, and neither is empty.
    assert_eq!(position.execution_state_root, block.state_root());
    assert_eq!(position.payload_root, block.payload_root());
    assert_ne!(position.execution_state_root, ids::EMPTY);
    assert_ne!(position.payload_root, ids::EMPTY);
    assert_ne!(
        position.execution_state_root, position.payload_root,
        "a state and the transactions that produced it are two commitments"
    );
}

#[test]
fn a_block_a_peer_pushed_reads_back_through_the_seam_as_the_same_block() {
    let (vm, g) = a_chain();
    vm.issue(spend_genesis(&g, 2)).expect("offer a transaction");
    let built = vm.build().expect("a block");

    // What `Engine::follow` does with bytes off the mesh: parse, then verify.
    let (fresh, _) = a_chain();
    let parsed = fresh.parse(&built.bytes()).expect("a peer's bytes parse");
    assert_eq!(parsed.id(), built.id());
    assert_eq!(parsed.state_root(), built.state_root());
    assert_eq!(parsed.payload_root(), built.payload_root());
    fresh
        .verify(&parsed.id())
        .expect("and execute to that root");
    fresh.accept(&parsed.id()).expect("and be accepted");
    assert_eq!(fresh.last_accepted(), built.id());
}
