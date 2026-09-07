// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Durability, proven the only way it can be: by killing the process.
//!
//! [`store`](lux_platformvm::store) already has a torn-write test, and that
//! test writes the torn bytes itself. It proves the replay logic reads a
//! crafted file correctly. It cannot prove the thing the file exists for,
//! because three of the four ways a store loses a commit leave a file that
//! looks perfectly well-formed:
//!
//!   - the commit returned before the bytes reached the device;
//!   - the bytes were sitting in a userspace buffer that a destructor flushes;
//!   - the state came back parseable but not identical.
//!
//! Only ending the process where a clean shutdown would have been can tell. So
//! the write happens in a CHILD that never returns: it commits one height,
//! commits a second and cuts that one in half — the bytes are the store's own,
//! and what is left of them is what a kill inside `write` leaves behind — and
//! then aborts. No destructor runs, nothing is flushed on the way out, and the
//! operating system is the only thing that closes the file.
//!
//! The parent then opens the same path and asserts three things the P-chain
//! cannot restart without:
//!
//!   - the state is what the last finished height held, and hashes to the same
//!     root, because a state that comes back looking right and hashing
//!     differently is a node voting against its own network;
//!   - the chain knows which block it last accepted, because one that forgot
//!     would accept a second block at the same height;
//!   - the validator set at every past height is still reachable, because a
//!     signature made at height H is checked against the set at H, and a node
//!     that lost those sets can check nothing older than its own last block.

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::process::Command;

use lux_platformvm::block;
use lux_platformvm::components::{Owners, Output, Utxo, UtxoId};
use lux_platformvm::ids::{Id, NodeId, ShortId, PRIMARY_NETWORK_ID};
use lux_platformvm::persist;
use lux_platformvm::state::{Staker, State};
use lux_platformvm::store::File;
use lux_platformvm::txs::Priority;
use lux_platformvm::validators::{self, History, Validator};
use lux_platformvm::vm::state_root;

/// Set by the parent on the child it spawns; its value is where to write.
const CHILD: &str = "LUX_PVM_CRASH_CHILD";

const OTHER_NETWORK: Id = [9u8; 32];

fn owners(n: u8) -> Owners {
    Owners {
        locktime: u64::from(n),
        threshold: 1,
        addrs: vec![ShortId([n; 20]), ShortId([n.wrapping_add(1); 20])],
    }
}

fn utxo(tx: u8, index: u32, amount: u64) -> Utxo {
    Utxo {
        id: UtxoId {
            tx_id: [tx; 32],
            output_index: index,
        },
        output: Output {
            asset: [0xAA; 32],
            stake_lock: u64::from(tx),
            amount,
            owners: owners(tx),
        },
    }
}

/// `node` is separate from `tx` because a delegator backs SOMEONE ELSE'S node.
fn staker(tx: u8, node: u8, next_time: u64, priority: Priority) -> Staker {
    Staker {
        tx_id: [tx; 32],
        node_id: NodeId([node; 20]),
        public_key: if tx % 2 == 0 { Some([tx; 48]) } else { None },
        chain: PRIMARY_NETWORK_ID,
        weight: 1_000 * u64::from(tx),
        start_time: 10,
        end_time: next_time,
        potential_reward: 7 * u64::from(tx),
        next_time,
        priority,
    }
}

/// The state the last finished height left behind.
fn committed() -> State {
    let mut s = State::new();
    s.set_timestamp(1_712_345_678);

    s.put_current_validator(staker(2, 2, 200, Priority::PrimaryNetworkValidatorCurrent))
        .expect("validator");
    s.put_current_validator(staker(4, 4, 400, Priority::ChainPermissionedValidatorCurrent))
        .expect("validator");
    // A delegator behind the validator at node 2: many may share a node, and
    // which staker was seated there is not recoverable from the staker alone.
    s.put_current_delegator(staker(5, 2, 250, Priority::PrimaryNetworkDelegatorCurrent));
    s.put_pending_validator(staker(6, 6, 600, Priority::PrimaryNetworkValidatorPending))
        .expect("pending validator");

    for i in 1u8..=4 {
        s.add_utxo(utxo(i, u32::from(i), 100 * u64::from(i)));
    }
    // A spend, so the disk has to have recorded a removal and not just adds.
    s.delete_utxo(&utxo(3, 3, 0).id.input_id());
    s.set_current_supply(PRIMARY_NETWORK_ID, 720_000_000);
    s.set_current_supply(OTHER_NETWORK, 42);
    s.add_chain(OTHER_NETWORK, owners(9));
    s.add_blockchain([3; 32], "the-first-chain");
    s.set_delegatee_reward(PRIMARY_NETWORK_ID, NodeId([2; 20]), 55);
    s.add_reward_utxo([2; 32], utxo(8, 0, 800));
    s
}

/// The height the child was part-way through. It must NOT come back.
fn uncommitted() -> State {
    let mut s = committed();
    s.add_utxo(utxo(9, 9, 9_000));
    s.set_timestamp(1_799_999_999);
    s
}

fn validator(n: u8, weight: u64, key: Option<u8>) -> Validator {
    Validator {
        node_id: NodeId([n; 20]),
        weight,
        public_key: key.map(|k| vec![k; 48]),
        tx_id: [n; 32],
    }
}

fn set(vs: &[Validator]) -> BTreeMap<NodeId, Validator> {
    vs.iter().map(|v| (v.node_id, v.clone())).collect()
}

/// The validator set at each height, from one to four; height zero is the empty
/// set the chain started from. Recorded as changes, which is what a history
/// holds.
fn history() -> (History, Vec<BTreeMap<NodeId, Validator>>) {
    let sets = vec![
        set(&[]),
        set(&[validator(1, 100, Some(1))]),
        set(&[validator(1, 100, Some(1)), validator(2, 200, None)]),
        set(&[validator(1, 250, Some(7)), validator(2, 200, None)]),
        set(&[validator(2, 60, None), validator(3, 30, Some(3))]),
    ];
    let mut h = History::default();
    for height in 1..sets.len() {
        h.record(height as u64, &changes(&sets[height - 1], &sets[height]))
            .expect("record");
    }
    (h, sets)
}

/// What one height did to a set, in the shape the history records.
fn changes(
    before: &BTreeMap<NodeId, Validator>,
    after: &BTreeMap<NodeId, Validator>,
) -> BTreeMap<validators::Where, validators::Change> {
    let mut out = BTreeMap::new();
    let mut nodes: Vec<NodeId> = before.keys().chain(after.keys()).copied().collect();
    nodes.sort_unstable();
    nodes.dedup();
    for node in nodes {
        let was = before.get(&node);
        let now = after.get(&node);
        let old = was.map_or(0, |v| v.weight);
        let new = now.map_or(0, |v| v.weight);
        let c = validators::Change {
            weight: validators::WeightDiff {
                decrease: new < old,
                amount: new.abs_diff(old),
            },
            validation: now.or(was).map(|v| v.tx_id).unwrap_or_default(),
            renamed: was.is_some() && now.is_none(),
            key_before: was.and_then(|v| v.public_key.clone()).unwrap_or_default(),
            key_after: now.and_then(|v| v.public_key.clone()).unwrap_or_default(),
        };
        out.insert(
            validators::Where {
                chain: PRIMARY_NETWORK_ID,
                node,
            },
            c,
        );
    }
    out
}

/// A block at each height, so the chain has a tip to remember.
fn blocks() -> Vec<(Id, u64, block::Block, Id)> {
    let mut out = Vec::new();
    let mut parent: Id = [0u8; 32];
    for h in 1u64..=4 {
        let blk = block::Block::standard(parent, h, 1_000 + h, Vec::new());
        parent = blk.id();
        out.push((blk.id(), h, blk, [h as u8; 32]));
    }
    out
}

/// Everything the child does. It does not return.
fn write_then_die(path: &Path) -> ! {
    let mut disk = File::open(path).expect("open");
    let (h, _) = history();
    let bs = blocks();
    let tip = (bs[bs.len() - 1].0, bs.len() as u64);

    persist::flush(
        &mut disk,
        &State::new(),
        &committed(),
        &History::default(),
        &h,
        &bs,
        tip,
    )
    .expect("the height that finishes");
    let finished = std::fs::metadata(path).expect("stat").len();

    // A second height, written by the STORE and then cut in half. The bytes on
    // the disk are the store's own — not a record this test built to its own
    // idea of the format — and what is left of them is what a kill inside
    // `write` leaves behind.
    persist::flush(
        &mut disk,
        &committed(),
        &uncommitted(),
        &h,
        &h,
        &[],
        tip,
    )
    .expect("the height that will be torn");
    let whole = std::fs::metadata(path).expect("stat").len();
    std::fs::OpenOptions::new()
        .write(true)
        .open(path)
        .expect("truncate")
        .set_len(finished + (whole - finished) / 2)
        .expect("tear the record in half");

    // Where a clean shutdown would have been. No unwinding, no destructors, no
    // flush: the process simply stops existing.
    std::process::abort()
}

#[test]
fn a_state_survives_the_process_that_wrote_it_being_killed() {
    let path = match std::env::var(CHILD) {
        Ok(p) => write_then_die(Path::new(&p)),
        Err(_) => scratch(),
    };

    let child = Command::new(std::env::current_exe().expect("test binary"))
        .arg("--exact")
        .arg("a_state_survives_the_process_that_wrote_it_being_killed")
        .env(CHILD, &path)
        .output()
        .expect("spawn the writer");

    // It must have DIED, not exited. A child that returned cleanly would mean
    // this test proved nothing at all.
    assert!(
        !child.status.success(),
        "the writer exited instead of dying: {:?}\n{}",
        child.status,
        String::from_utf8_lossy(&child.stderr)
    );
    #[cfg(unix)]
    {
        use std::os::unix::process::ExitStatusExt;
        assert_eq!(
            child.status.signal(),
            Some(6),
            "the writer did not die by signal: {:?}",
            child.status
        );
    }

    let disk = File::open(&path).expect("reopen after the kill");

    // ---- the state, and the commitment over it ----
    let back = persist::restore(&disk).expect("restore");
    assert_eq!(
        persist::records(&back),
        persist::records(&committed()),
        "the state that came back is not the state that finished"
    );
    assert_eq!(
        state_root(&back),
        state_root(&committed()),
        "the state came back with a different root"
    );
    assert_ne!(
        state_root(&back),
        state_root(&uncommitted()),
        "the height that was never finished came back"
    );
    // The distinction a naive round trip loses.
    assert_eq!(
        back.current_validator(&PRIMARY_NETWORK_ID, &NodeId([2; 20]))
            .expect("the seated validator")
            .tx_id,
        [2u8; 32],
        "the delegator took the validator's place"
    );

    // ---- where the chain got to ----
    let (blocks_back, by_height, _roots, tip) =
        persist::restore_blocks(&disk).expect("restore blocks");
    let bs = blocks();
    assert_eq!(blocks_back.len(), bs.len());
    assert_eq!(by_height.len(), bs.len());
    assert_eq!(
        tip,
        Some((bs[bs.len() - 1].0, bs.len() as u64)),
        "the chain came back not knowing which block it last accepted"
    );

    // ---- the validator set at every past height ----
    let past = persist::restore_history(&disk).expect("restore history");
    let (_, sets) = history();
    let now = (sets.len() - 1) as u64;
    for (h, want) in sets.iter().enumerate() {
        let mut got = sets[sets.len() - 1].clone();
        past.rewind(&mut got, &PRIMARY_NETWORK_ID, now, h as u64)
            .expect("a past set");
        assert_eq!(
            &got, want,
            "the validator set at height {h} did not survive the kill"
        );
    }

    // ---- and the store carries on ----
    let mut disk = disk;
    persist::flush(
        &mut disk,
        &committed(),
        &uncommitted(),
        &past,
        &past,
        &[],
        (bs[bs.len() - 1].0, bs.len() as u64),
    )
    .expect("a height after the kill");
    drop(disk);
    let reread = File::open(&path).expect("third open");
    assert_eq!(
        persist::records(&persist::restore(&reread).expect("restore")),
        persist::records(&uncommitted()),
        "the height written after the kill did not land"
    );

    let _ = std::fs::remove_dir_all(path.parent().expect("scratch dir"));
}

fn scratch() -> PathBuf {
    let dir = std::env::temp_dir().join(format!("lux-pvm-crash-{}", std::process::id()));
    std::fs::create_dir_all(&dir).expect("scratch");
    dir.join("state")
}
