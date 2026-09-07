// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The chain's id, which cannot change.
//!
//! A vmID is an immutable one-way door: it is baked into the genesis
//! CreateChainTx, it is the plugin binary's filename, and the P-Chain stores it
//! forever. So it is written out literally rather than computed — a change to it,
//! for any reason including an "obviously equivalent" refactor, fails here instead
//! of silently producing a chain no node can start.

mod common;

use lux_fhevm::ids;
use lux_fhevm::state::vm_id;
use lux_fhevm::vm::VM_NAME;

/// F-Chain's vmID in the encoding the node's plugin registry uses. The plugin
/// binary MUST be installed under exactly this filename; the registry resolves a
/// CreateChainTx's vmID to an implementation by looking for a file with this name
/// in the plugin directory.
const CANONICAL_VM_ID_CB58: &str = "n6sSsSfbpQBrU9sY4R29U6z8VrmnTo2CntW6da4rRS7qmnGdv";

#[test]
fn the_vm_id_is_canonical_and_stable() {
    assert_eq!(
        ids::id_string(&vm_id()),
        CANONICAL_VM_ID_CB58,
        "A vmID cannot change after a chain is created with it. If this is an\n\
         intentional pre-launch change, every declaration must move together:\n\
        \x20 luxfi/constants        vm_ids.go FHEVMID\n\
        \x20 node/node              vms.go OptionalVMs\n\
        \x20 chains/fhevm           factory.go, cmd/plugin/main.go, and its test\n\
        \x20 chains/rust/fhevm      state.rs vm_id, and this test"
    );
    assert_ne!(vm_id(), ids::EMPTY, "the vmID is not the empty id");
}

/// The bytes themselves, spelled out: an ASCII name left-padded into 32 bytes.
#[test]
fn the_vm_id_is_the_name_padded_into_thirty_two_bytes() {
    let mut want = ids::EMPTY;
    want[..5].copy_from_slice(b"fhevm");
    assert_eq!(vm_id(), want);
    assert_eq!(VM_NAME, "fhevm");
    assert_eq!(&vm_id()[..VM_NAME.len()], VM_NAME.as_bytes());
}

/// M-Chain and F-Chain were split out of the retired threshold VM. They are
/// separate chains and must never share an id.
#[test]
fn the_vm_id_does_not_collide_with_the_chain_it_was_split_from() {
    let mut mpc = ids::EMPTY;
    mpc[..5].copy_from_slice(b"mpcvm");
    assert_ne!(vm_id(), mpc);
}
