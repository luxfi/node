// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The shapes two chains both speak are declared the same way in both.
//!
//! An unspent output is made by the X-chain and spent by the P-chain, so both
//! schemas state it — the same fields at the same offsets. That is two
//! statements of one format, and two statements of one format are how two
//! chains come to disagree about what a UTXO is while each stays consistent
//! with itself.
//!
//! It is two because `xchain.zap` names `TransferableOut` from its own
//! envelope, so lifting the shared primitives into a third schema needs an
//! import across schemas that zapgen does not have. Until it does, this test
//! is the join: the day the two drift is the day it fails, which is the day
//! before a P node and an X node stop agreeing about a UTXO.
//!
//! It reads the schemas as text on purpose. Comparing the EMITTED constants
//! would need one crate to depend on both chains, and the schema is the thing
//! that is supposed to be one — so the schema is what is compared.

use std::path::PathBuf;

/// The lines of one struct's body, whitespace-normalized.
fn body(schema: &str, name: &str) -> Vec<String> {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../schema")
        .join(schema);
    let text = std::fs::read_to_string(&path)
        .unwrap_or_else(|e| panic!("{}: {e}", path.display()));
    let open = format!("struct {name} {{");
    let start = text
        .find(&open)
        .unwrap_or_else(|| panic!("{schema} does not declare {name}"))
        + open.len();
    let end = start
        + text[start..]
            .find("\n}")
            .unwrap_or_else(|| panic!("{schema}: {name} is not terminated"));
    text[start..end]
        .lines()
        .map(|l| l.split('#').next().unwrap_or("").split_whitespace().collect::<Vec<_>>().join(" "))
        .filter(|l| !l.is_empty())
        .collect()
}

#[test]
fn the_shapes_both_chains_speak_are_one_shape() {
    // Every struct declared in more than one schema, and what it is.
    for (name, what) in [
        ("TransferOutput", "how much, and whose"),
        ("Utxo", "an output that has not been spent"),
    ] {
        let x = body("xchain.zap", name);
        let p = body("pchain.zap", name);
        assert!(!x.is_empty(), "{name} has no fields in xchain.zap");
        assert_eq!(
            x, p,
            "{name} — {what} — is declared differently in the two schemas. \
             A P node and an X node would read the same bytes as different values."
        );
    }
}

#[test]
fn a_struct_only_one_chain_declares_is_not_compared() {
    // The guard on the test above: it would pass vacuously if `body` answered
    // an empty list for a name neither schema declares, so `body` panics
    // instead, and this says so.
    let caught = std::panic::catch_unwind(|| body("pchain.zap", "NotAStructAnybodyDeclares"));
    assert!(caught.is_err(), "a name no schema declares was not refused");
}
