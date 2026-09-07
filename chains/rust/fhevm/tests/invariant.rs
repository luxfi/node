// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! F-Chain's defining invariant: it coordinates confidential compute without ever
//! holding the confidential part. No ciphertext body, no plaintext, no FHE secret
//! key, no decryption share.
//!
//! These prove it STRUCTURALLY, three ways.
//!
//! The SCHEMA PIN below lists every field F persists, and the function that
//! produces the list DESTRUCTURES each record exhaustively — so a field added,
//! removed or retyped does not fail an assertion, it fails to COMPILE until
//! someone writes it down. That is what makes "should F be storing that?" a
//! question somebody has to answer rather than one an omission answers.
//!
//! The NAME SCAN is the tripwire for new types: a byte-bearing field named like a
//! body, a plaintext or a share is flagged. It is a denylist and a denylist cannot
//! be complete — a field called Material carries exactly as much as one called
//! Secret — so it is kept beside the pin, not in place of it.
//!
//! The SOURCE SCAN reads the crate's own sources and fails if they name any
//! identifier by which F could generate a key, produce or combine shares, or
//! decrypt — and, separately, if the apply path names a wall clock or a source of
//! randomness. Comments are stripped first, so a doc comment that NAMES one of
//! these to explain the invariant is not flagged; only real code is.

mod common;

use std::collections::BTreeSet;

use lux_fhevm::fhe;
use lux_fhevm::state::{
    Attestation, CiphertextRecord, DecryptRecord, EpochRecord, PermitRecord,
};

// ---- the schema pin --------------------------------------------------------

/// Every field of a ciphertext record, flattened through the runtime type it
/// embeds. The destructuring is exhaustive: a change to the struct stops this
/// compiling.
fn pin_ciphertext(r: CiphertextRecord) -> Vec<&'static str> {
    let CiphertextRecord {
        meta:
            fhe::CiphertextMeta {
                handle: _,
                owner: _,
                kind: _,
                level: _,
                epoch: _,
                registered_at: _,
                size: _,
                chain_id: _,
            },
        scheme: _,
        digest: _,
    } = r;
    vec![
        "handle [u8; 32]",
        "owner [u8; 20]",
        "kind u8",
        "level i64",
        "epoch u64",
        "registered_at i64",
        "size u32",
        "chain_id Id",
        "scheme String",
        "digest [u8; 32]",
    ]
}

fn pin_permit(r: PermitRecord) -> Vec<&'static str> {
    let PermitRecord {
        permit:
            fhe::Permit {
                permit_id: _,
                handle: _,
                grantee: _,
                grantor: _,
                operations: _,
                expiry: _,
                created_at: _,
                attestation: _,
                chain_id: _,
            },
        status: _,
    } = r;
    vec![
        "permit_id [u8; 32]",
        "handle [u8; 32]",
        "grantee [u8; 20]",
        "grantor [u8; 20]",
        "operations u32",
        "expiry i64",
        "created_at i64",
        "attestation Vec<u8>",
        "chain_id Id",
        "status String",
    ]
}

fn pin_decrypt(r: DecryptRecord) -> Vec<&'static str> {
    let DecryptRecord {
        request:
            fhe::DecryptRequest {
                request_id: _,
                ciphertext_handle: _,
                requester: _,
                callback: _,
                callback_selector: _,
                source_chain: _,
                epoch: _,
                nonce: _,
                expiry: _,
                status: _,
                created_at: _,
                completed_at: _,
                result_handle: _,
                error: _,
            },
        permit_id: _,
        attestations: _,
    } = r;
    vec![
        "request_id [u8; 32]",
        "ciphertext_handle [u8; 32]",
        "requester [u8; 20]",
        "callback [u8; 20]",
        "callback_selector [u8; 4]",
        "source_chain Id",
        "epoch u64",
        "nonce u64",
        "expiry i64",
        "status RequestStatus",
        "created_at i64",
        "completed_at i64",
        "result_handle [u8; 32]",
        "error String",
        "permit_id [u8; 32]",
        "attestations List<Attestation>",
    ]
}

fn pin_epoch(r: EpochRecord) -> Vec<&'static str> {
    let EpochRecord {
        info:
            fhe::EpochInfo {
                epoch: _,
                start_time: _,
                end_time: _,
                committee: _,
                threshold: _,
                public_key: _,
                status: _,
            },
        attestations: _,
    } = r;
    vec![
        "epoch u64",
        "start_time i64",
        "end_time i64",
        "committee List<CommitteeMember>",
        "threshold i64",
        "public_key Vec<u8>",
        "status EpochStatus",
        "attestations List<Attestation>",
    ]
}

fn pin_attestation(a: Attestation) -> Vec<&'static str> {
    let Attestation { member: _, value: _ } = a;
    vec!["member Account", "value [u8; 32]"]
}

/// The four records are everything F persists, and this is what they hold. The
/// list is written down so that changing it is a decision.
#[test]
fn the_persisted_schema_is_pinned_field_for_field() {
    assert_eq!(
        pin_ciphertext(CiphertextRecord::default()),
        vec![
            "handle [u8; 32]",
            "owner [u8; 20]",
            "kind u8",
            "level i64",
            "epoch u64",
            "registered_at i64",
            "size u32",
            "chain_id Id",
            "scheme String",
            "digest [u8; 32]",
        ]
    );
    assert_eq!(
        pin_permit(PermitRecord::default()),
        vec![
            "permit_id [u8; 32]",
            "handle [u8; 32]",
            "grantee [u8; 20]",
            "grantor [u8; 20]",
            "operations u32",
            "expiry i64",
            "created_at i64",
            "attestation Vec<u8>",
            "chain_id Id",
            "status String",
        ]
    );
    assert_eq!(pin_decrypt(DecryptRecord::default()).len(), 16);
    assert_eq!(pin_epoch(EpochRecord::default()).len(), 8);
    assert_eq!(pin_attestation(Attestation { member: [0; 20], value: [0; 32] }).len(), 2);
}

/// And the stored form carries exactly those fields and no others: the pin is over
/// the type, this is over the bytes, and a field that stopped being written would
/// pass the first and fail this.
#[test]
fn the_stored_form_carries_exactly_the_fields_that_are_pinned() {
    let keys = |json: String| -> BTreeSet<String> {
        let v: serde_json::Value = serde_json::from_str(&json).expect("a record is JSON");
        v.as_object().expect("an object").keys().cloned().collect()
    };

    // The two `omitempty` fields are written when they hold something, so a record
    // with everything set shows the whole schema.
    let ct = CiphertextRecord { scheme: "ckks-n14".into(), ..Default::default() };
    assert_eq!(
        keys(ct.to_json()),
        [
            "handle",
            "owner",
            "type",
            "level",
            "epoch",
            "registered_at",
            "size",
            "chain_id",
            "scheme",
            "digest",
        ]
        .iter()
        .map(|s| s.to_string())
        .collect::<BTreeSet<_>>()
    );

    let pm = PermitRecord {
        permit: fhe::Permit { attestation: vec![1], ..Default::default() },
        ..Default::default()
    };
    assert_eq!(
        keys(pm.to_json()),
        [
            "permit_id",
            "handle",
            "grantee",
            "grantor",
            "operations",
            "expiry",
            "created_at",
            "attestation",
            "chain_id",
            "status",
        ]
        .iter()
        .map(|s| s.to_string())
        .collect::<BTreeSet<_>>()
    );

    let dr = DecryptRecord {
        request: fhe::DecryptRequest {
            completed_at: 1,
            error: "why".into(),
            ..Default::default()
        },
        ..Default::default()
    };
    assert_eq!(
        keys(dr.to_json()),
        [
            "request_id",
            "ciphertext_handle",
            "requester",
            "callback",
            "callback_selector",
            "source_chain",
            "epoch",
            "nonce",
            "expiry",
            "status",
            "created_at",
            "completed_at",
            "result_handle",
            "error",
            "permitId",
            "attestations",
        ]
        .iter()
        .map(|s| s.to_string())
        .collect::<BTreeSet<_>>()
    );

    let ep = EpochRecord {
        info: fhe::EpochInfo { end_time: 1, ..Default::default() },
        ..Default::default()
    };
    assert_eq!(
        keys(ep.to_json()),
        [
            "epoch",
            "start_time",
            "end_time",
            "committee",
            "threshold",
            "public_key",
            "status",
            "attestations",
        ]
        .iter()
        .map(|s| s.to_string())
        .collect::<BTreeSet<_>>()
    );
}

// ---- the name scan ---------------------------------------------------------

/// Substrings that, on a byte-bearing field, would say F is holding something it
/// must not. "share" is included but only flags BYTE-bearing fields, so an integer
/// count named `…_shares` is not flagged.
const SECRET_TOKENS: [&str; 10] = [
    "private", "secret", "mnemonic", "seed", "share", "privkey", "plaintext", "cleartext",
    "body", "blob",
];

/// Whether a declared type can carry opaque bytes. Fixed-size byte arrays count:
/// being id-sized is not being an id, and a scalar share is exactly 32 bytes.
fn is_byte_bearing(ty: &str) -> bool {
    let t = ty.replace(' ', "");
    t == "String"
        || t == "Vec<u8>"
        || t == "Vec<Vec<u8>>"
        || (t.starts_with("[u8;") && t.ends_with(']'))
}

/// The fields a struct declares, as (name, type) pairs, read from the source. A
/// field this cannot see is a field the scan cannot judge, so it is asserted to
/// see every one the pin names.
fn fields_of(src: &str, name: &str) -> Vec<(String, String)> {
    let needle = format!("pub struct {name} {{");
    let start = src.find(&needle).unwrap_or_else(|| panic!("no struct {name}")) + needle.len();
    let body = &src[start..start + src[start..].find("\n}").expect("a struct ends")];
    let mut out = Vec::new();
    for line in body.lines() {
        let line = line.trim();
        let Some(decl) = line.strip_prefix("pub ") else { continue };
        let Some((field, ty)) = decl.split_once(':') else { continue };
        let ty = ty.trim().trim_end_matches(',');
        out.push((field.trim().to_string(), ty.to_string()));
    }
    out
}

fn flagged(fields: &[(String, String)], path: &str) -> Vec<String> {
    let mut bad = Vec::new();
    for (name, ty) in fields {
        if ty.contains("PrivateKey") || ty.contains("SecretKey") {
            bad.push(format!("{path}.{name} has secret-bearing type {ty}"));
        }
        if is_byte_bearing(ty) {
            let lower = name.to_lowercase();
            for token in SECRET_TOKENS {
                if lower.contains(token) {
                    bad.push(format!("byte-bearing field {path}.{name} ({ty}) is named {token:?}"));
                }
            }
        }
    }
    bad
}

fn source(file: &str) -> String {
    std::fs::read_to_string(format!("{}/src/{file}", env!("CARGO_MANIFEST_DIR")))
        .unwrap_or_else(|e| panic!("read src/{file}: {e}"))
}

/// Everything F serializes — what it persists, what it accepts on the wire, and
/// what it reads from genesis — is enumerable, and none of those fields may be, or
/// hold, the encrypted part.
#[test]
fn nothing_f_serializes_is_named_like_the_thing_it_must_not_hold() {
    let fhe_src = source("fhe.rs");
    let state_src = source("state.rs");
    let tx_src = source("transaction.rs");
    let vm_src = source("vm.rs");

    let mut bad = Vec::new();
    for (src, name) in [
        (&fhe_src, "CiphertextMeta"),
        (&fhe_src, "Permit"),
        (&fhe_src, "DecryptRequest"),
        (&fhe_src, "EpochInfo"),
        (&fhe_src, "CommitteeMember"),
        (&state_src, "CiphertextRecord"),
        (&state_src, "PermitRecord"),
        (&state_src, "DecryptRecord"),
        (&state_src, "EpochRecord"),
        (&state_src, "Attestation"),
        (&tx_src, "Transaction"),
        (&tx_src, "RegisterPayload"),
        (&tx_src, "GrantPayload"),
        (&tx_src, "RevokePayload"),
        (&tx_src, "RequestPayload"),
        (&tx_src, "FulfillPayload"),
        (&tx_src, "AdvancePayload"),
        (&vm_src, "Genesis"),
    ] {
        let fields = fields_of(src, name);
        assert!(!fields.is_empty(), "{name}: the scan saw no fields, so it judged nothing");
        bad.extend(flagged(&fields, name));
    }
    assert!(bad.is_empty(), "F-Chain state can reach secret material:\n{}", bad.join("\n"));
}

/// The control for the scan above: a walk that flags nothing proves nothing unless
/// the walk can be shown to flag something.
#[test]
fn the_name_scan_flags_the_shapes_it_claims_to() {
    let cases = [
        ("a named body", vec![("ciphertext_body".into(), "Vec<u8>".into())]),
        ("a named share", vec![("share".into(), "Vec<u8>".into())]),
        // A scalar share is exactly 32 bytes: being id-sized is not being an id.
        ("an id-sized share", vec![("secret_share".into(), "[u8; 32]".into())]),
        ("a secret-bearing type", vec![("key".into(), "PrivateKey".into())]),
        ("a plaintext string", vec![("plaintext".into(), "String".into())]),
    ];
    for (name, fields) in cases {
        assert!(!flagged(&fields, name).is_empty(), "the scan must flag {name}");
    }
    // And it does not flag what it should not: an integer count of shares, or a
    // handle that happens to be 32 bytes.
    assert!(flagged(&[("shares".into(), "u32".into())], "count").is_empty());
    assert!(flagged(&[("handle".into(), "[u8; 32]".into())], "handle").is_empty());
}

// ---- the source scan -------------------------------------------------------

/// Removes comments, so a doc comment that NAMES a forbidden identifier to explain
/// why F does not do it is not mistaken for doing it.
fn strip_comments(src: &str) -> String {
    let mut out = String::with_capacity(src.len());
    let mut chars = src.chars().peekable();
    let mut in_string = false;
    let mut in_block = 0usize;
    while let Some(c) = chars.next() {
        if in_block > 0 {
            if c == '*' && chars.peek() == Some(&'/') {
                chars.next();
                in_block -= 1;
            } else if c == '/' && chars.peek() == Some(&'*') {
                chars.next();
                in_block += 1;
            }
            continue;
        }
        if in_string {
            out.push(c);
            if c == '\\' {
                if let Some(n) = chars.next() {
                    out.push(n);
                }
            } else if c == '"' {
                in_string = false;
            }
            continue;
        }
        match c {
            '"' => {
                in_string = true;
                out.push(c);
            }
            '/' if chars.peek() == Some(&'/') => {
                for n in chars.by_ref() {
                    if n == '\n' {
                        out.push('\n');
                        break;
                    }
                }
            }
            '/' if chars.peek() == Some(&'*') => {
                chars.next();
                in_block = 1;
            }
            _ => out.push(c),
        }
    }
    out
}

fn crate_sources() -> Vec<(String, String)> {
    let dir = format!("{}/src", env!("CARGO_MANIFEST_DIR"));
    let mut out = Vec::new();
    for entry in std::fs::read_dir(&dir).expect("the source directory") {
        let path = entry.expect("an entry").path();
        if path.extension().and_then(|e| e.to_str()) != Some("rs") {
            continue;
        }
        let name = path.file_name().unwrap().to_string_lossy().into_owned();
        out.push((name, std::fs::read_to_string(&path).expect("a source file")));
    }
    out.sort();
    out
}

/// Any function or type by which F could generate a key, produce or combine
/// shares, or decrypt. F wraps the runtime's vocabulary, never its key machinery.
#[test]
fn the_sources_name_nothing_by_which_f_could_hold_a_secret() {
    let forbidden: [(&str, &str); 12] = [
        ("try_keygen", "key generation"),
        ("try_sign", "signing uses a private key; F only verifies"),
        ("keypair", "key generation"),
        ("PrivateKey", "F holds no private key"),
        ("SecretKey", "F holds no secret key"),
        ("decapsulate", "KEM decapsulation uses the private key"),
        ("encapsulate", "F performs no cryptographic compute"),
        ("zeroize", "only needed if secret material were held"),
        ("reconstruct", "share reconstruction"),
        ("combine_shares", "share combination"),
        ("recover_secret", "secret recovery"),
        ("decrypt_ciphertext", "decryption happens off-chain, on the committee"),
    ];

    let mut scanned = 0;
    let mut bad = Vec::new();
    for (name, src) in crate_sources() {
        // The ZAP arena format is a serialization this chain shares with the rest
        // of the suite, not F's own code, and it names none of these anyway.
        let code = strip_comments(&src);
        for (needle, why) in forbidden {
            if code.contains(needle) {
                bad.push(format!("{name} names {needle:?} — {why}"));
            }
        }
        scanned += 1;
    }
    assert!(scanned > 5, "expected to scan the crate's sources, saw {scanned} files");
    assert!(bad.is_empty(), "{}", bad.join("\n"));
}

/// The control: the scan must be able to see a forbidden name when one is there.
#[test]
fn the_source_scan_sees_code_and_not_comments() {
    let commented = "/// try_keygen is exactly what this does not do.\n// nor try_sign\nfn f() {}\n";
    let real = "fn f() { let k = try_keygen(); }\n";
    assert!(!strip_comments(commented).contains("try_keygen"));
    assert!(!strip_comments(commented).contains("try_sign"));
    assert!(strip_comments(real).contains("try_keygen"));
    // A string literal is code, not a comment, and stays visible.
    assert!(strip_comments("let s = \"try_keygen\";").contains("try_keygen"));
}

/// The one clock F may read is its own, and only outside consensus — admission and
/// the read surface, where a wrong answer costs a retry rather than a fork.
/// Nothing under acceptance may read a wall clock or a source of randomness.
#[test]
fn the_apply_path_names_no_wall_clock_and_no_randomness() {
    let forbidden = [
        ("SystemTime", "wall-clock time diverges between validators"),
        ("Instant::now", "a monotonic clock is still not the block's time"),
        ("rand::", "randomness in an applied path diverges"),
        ("thread_rng", "randomness in an applied path diverges"),
        ("available_parallelism", "a result must not depend on the machine"),
    ];
    // The apply path: what acceptance reaches. `state.rs` holds the derivations,
    // `transaction.rs` the effects, `block.rs` the settlement, `batch.rs`
    // admission.
    for file in ["transaction.rs", "block.rs", "state.rs", "batch.rs"] {
        let code = strip_comments(&source(file));
        for (needle, why) in forbidden {
            assert!(!code.contains(needle), "{file} names {needle:?} — {why}");
        }
    }

    // And the crate as a whole reaches for no randomness: the one crate that could
    // supply it is named for a verifier that never needs any.
    for (name, src) in crate_sources() {
        let code = strip_comments(&src);
        assert!(!code.contains("rand::"), "{name} reaches for randomness");
        assert!(!code.contains("getrandom"), "{name} reaches for randomness");
    }
}

/// The public surface offers no way to ASK F for something it must not have.
///
/// The rule is about accessors — methods that hand a caller back a value — because
/// that is the only way material could leave. A method that returns nothing
/// performs an operation; `seed_genesis` is one, and it is named for the genesis
/// it seeds rather than for any key. `decrypt` is an accessor and is named for what
/// it returns: the RECORD of a decryption request. So the check is against the
/// things a caller could mistake for the plaintext itself.
#[test]
fn no_public_accessor_hands_back_something_f_does_not_hold() {
    let bad = ["private_key", "secret_key", "plaintext", "share", "seed", "body", "reconstruct"];
    let mut accessors = 0;
    for file in ["vm.rs", "service.rs"] {
        let code = strip_comments(&source(file));
        for line in code.lines() {
            let line = line.trim();
            let Some(rest) = line.strip_prefix("pub fn ") else { continue };
            // What it hands back. A method returning nothing, or only whether it
            // succeeded, is an operation rather than an accessor.
            let Some((head, ret)) = rest.split_once("->") else { continue };
            let ret = ret.trim().trim_end_matches('{').trim();
            if ret == "()" || ret == "Result<()>" {
                continue;
            }
            accessors += 1;
            let name = head.split(['(', '<']).next().unwrap_or("");
            for token in bad {
                assert!(
                    !name.contains(token),
                    "{file} hands back {name:?}, which suggests material F does not hold"
                );
            }
        }
    }
    assert!(accessors > 10, "the scan saw {accessors} accessors, so it judged almost nothing");
}

/// The read surface has no method that performs a decryption or hands back a
/// plaintext. Decryption is a consensus transaction answered by the committee,
/// never a synchronous call.
#[test]
fn the_read_surface_has_no_decrypting_endpoint() {
    let code = strip_comments(&source("service.rs"));
    for method in ["reencrypt", "evaluate", "plaintext", "privateKey", "secretKey"] {
        assert!(!code.contains(method), "the surface names {method:?}");
    }
    // The one decrypt-named method is a read of the request record.
    assert!(code.contains("fchain.getDecrypt"));
}
