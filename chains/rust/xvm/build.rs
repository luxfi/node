// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Re-emit the loader path `libluxcrypto` was resolved at.
//!
//! This crate links that library only through the node: `lux-node` holds an
//! ML-DSA identity and an ML-KEM session, and both come from `libluxcrypto`
//! by way of `lux-pq`. `cargo:rustc-link-arg` reaches only the binaries of the
//! crate that emitted it, so the rpath `lux-node` set for its own binaries does
//! not reach this crate's test binaries — `tests/node.rs` runs the node, and
//! without this it links and then cannot load.
//!
//! `links` metadata does reach here. `DEP_LUXPQ_LIB_DIR` is `lux-pq` passing on
//! what the ABI crate resolved, so the directory is found exactly once, down
//! there, and repeated here rather than searched for a second time. Nothing is
//! emitted when the variable is absent: the library may already be on the
//! system loader path, and inventing a directory would only be a second place
//! to look.

fn main() {
    if let Some(dir) = std::env::var_os("DEP_LUXPQ_LIB_DIR") {
        println!("cargo:rustc-link-arg=-Wl,-rpath,{}", dir.to_string_lossy());
    }
    println!("cargo:rerun-if-env-changed=DEP_LUXPQ_LIB_DIR");
}
