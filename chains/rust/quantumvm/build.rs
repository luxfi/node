// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! Re-emit the loader path `libluxcrypto` was resolved at.
//!
//! `cargo:rustc-link-arg` reaches only the binaries of the crate that emitted
//! it, so the rpath `lux-pq` set for its own tests does not reach this chain's
//! test binaries. `links` metadata does reach here — `DEP_LUXPQ_LIB_DIR` is
//! `lux-pq` passing on what the ABI crate resolved — so the path is found once,
//! down there, and repeated here rather than searched for a second time.
//!
//! Nothing is emitted when the variable is absent: the library may be on the
//! system loader path already, and inventing a directory would only be a second
//! place to look.

fn main() {
    if let Some(dir) = std::env::var_os("DEP_LUXPQ_LIB_DIR") {
        println!("cargo:rustc-link-arg=-Wl,-rpath,{}", dir.to_string_lossy());
    }
    println!("cargo:rerun-if-env-changed=DEP_LUXPQ_LIB_DIR");
}
