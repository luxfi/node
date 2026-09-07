// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Durability, proven the only way it can be: by killing the process.
//!
//! [`db`](lux_xvm::db) already has a torn-tail test, and that test writes the
//! torn bytes itself. It proves the replay logic reads a crafted file
//! correctly. It cannot prove the thing the file exists for, because three of
//! the four ways a store loses a commit leave a file that looks perfectly
//! well-formed:
//!
//!   - the commit returned before the bytes reached the device;
//!   - the bytes were sitting in a userspace buffer that a destructor flushes;
//!   - the state came back parseable but not identical.
//!
//! Only ending the process where a clean shutdown would have been can tell.
//! So the write happens in a CHILD that never returns: it commits one height,
//! commits a second and cuts that one in half — the bytes are the store's own,
//! and what is left of them is what a kill inside `write` leaves behind — and
//! then aborts. No destructor runs, nothing is flushed on the way out, and the
//! operating system is the only thing that closes the file.
//!
//! The parent then opens the same path and asserts the state is what the last
//! completed commit held AND that its execution root is the same id, because a
//! state that comes back looking right and hashing differently is a node voting
//! against its own network.

use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::Arc;

use lux_xvm::block::root::block_execution_root;
use lux_xvm::db;
use lux_xvm::fx;
use lux_xvm::ids::{self, Id, ShortId, EMPTY};
use lux_xvm::state::{Chain, ReadOnlyChain, Store as ChainState};
use lux_xvm::utxo::{Asset, Utxo, UtxoId};

/// Set by the parent on the child it spawns; its value is where to write.
const CHILD: &str = "LUX_XVM_CRASH_CHILD";

fn utxo(tx: u8, index: u32, amount: u64) -> Utxo {
    Utxo {
        utxo_id: UtxoId::new(ids::prefixed(&[tx]), index),
        asset: Asset {
            id: ids::prefixed(&[0xAA]),
        },
        out: fx::State::Transfer(fx::secp256k1::TransferOutput {
            amt: amount,
            owners: fx::secp256k1::Owners {
                locktime: 0,
                threshold: 1,
                addrs: vec![ShortId([tx; 20])],
            },
        }),
    }
}

/// What the child writes into the state before it commits.
fn fill(s: &mut ChainState) {
    for i in 1u8..=8 {
        s.add_utxo(utxo(i, u32::from(i), 1_000 * u64::from(i)));
    }
    // A spend, so the disk has to have recorded a removal and not just adds.
    s.delete_utxo(&utxo(4, 4, 4_000).input_id());
    s.set_last_accepted(ids::prefixed(&[0xC0]));
    s.set_timestamp(1_712_345_678);
}

/// What the child was part-way through when it died. It must NOT come back.
fn fill_further(s: &mut ChainState) {
    s.add_utxo(utxo(9, 9, 9_000));
    s.set_timestamp(1_799_999_999);
}

fn root(s: &ChainState) -> Id {
    block_execution_root(EMPTY, &[], s, 11).expect("root")
}

fn snapshot(s: &ChainState) -> (Vec<Utxo>, Id, u64) {
    (
        s.utxos(&EMPTY, 0).expect("utxos"),
        s.get_last_accepted(),
        s.get_timestamp(),
    )
}

/// The state the parent must find, built with no device under it.
fn committed() -> ChainState {
    let mut s = ChainState::new();
    fill(&mut s);
    s
}

fn uncommitted() -> ChainState {
    let mut s = committed();
    fill_further(&mut s);
    s
}

/// Everything the child does. It does not return.
fn write_then_die(path: &Path) -> ! {
    let log = Arc::new(db::Log::open(path).expect("open"));
    let mut state = ChainState::on(log.clone()).expect("read");
    fill(&mut state);
    state.commit().expect("commit");
    let finished = std::fs::metadata(path).expect("stat").len();

    // A second height, written by the STORE and then cut in half. The bytes on
    // the disk are the store's own — not a frame this test built to its own
    // idea of the format — and what is left of them is exactly what a kill
    // inside `write` leaves behind.
    fill_further(&mut state);
    state.commit().expect("commit the height that will be torn");
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

    let log = Arc::new(db::Log::open(&path).expect("reopen after the kill"));
    let back = ChainState::on(log.clone()).expect("read after the kill");

    assert_eq!(
        snapshot(&back),
        snapshot(&committed()),
        "the state that came back is not the state that was committed"
    );
    assert_eq!(
        root(&back),
        root(&committed()),
        "the state came back with a different execution root"
    );
    assert_ne!(
        root(&back),
        root(&uncommitted()),
        "the height that was never finished came back"
    );
    assert!(
        back.get_utxo(&utxo(9, 9, 9_000).input_id()).is_err(),
        "a row from the torn record survived"
    );
    // The file is back on a record boundary, so the next append is replayable.
    assert_eq!(
        log.size().expect("size"),
        std::fs::metadata(&path).expect("stat").len(),
        "the torn tail was left on the disk"
    );
    // And the store is USABLE, not merely readable: a chain that came back
    // read-only would still be a chain that cannot carry on.
    let mut next = ChainState::on(log).expect("read again");
    next.add_utxo(utxo(12, 12, 12_000));
    next.commit().expect("commit after the kill");

    let reread = ChainState::on(Arc::new(db::Log::open(&path).expect("third open")))
        .expect("read a third time");
    assert!(reread.get_utxo(&utxo(12, 12, 12_000).input_id()).is_ok());

    let _ = std::fs::remove_dir_all(path.parent().expect("scratch dir"));
}

fn scratch() -> PathBuf {
    let dir = std::env::temp_dir().join(format!("lux-xvm-crash-{}", std::process::id()));
    std::fs::create_dir_all(&dir).expect("scratch");
    dir.join("state")
}
