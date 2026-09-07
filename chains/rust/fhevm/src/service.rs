// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The read surface, and the one way to change anything.
//!
//! Mutating operations are submitted as CLIENT-SIGNED transactions and take
//! effect only through fee-settled consensus blocks. Everything else here is a
//! read-only query of PUBLIC state. There is no endpoint that decrypts, none that
//! returns a ciphertext body, and no "fee" integer in any request — a fee is never
//! an unbacked number a caller writes down, it is gas metered and burned from the
//! payer's on-chain balance inside consensus.
//!
//! Two of the methods report the FHE RUNTIME's configuration rather than F's
//! ledger — the public parameters and the seated committee — and answer in the
//! runtime's vocabulary, so a client that understands the runtime understands F.
//! The rest describe F's ledger and answer in F's own views. That is the whole
//! rule: runtime types for runtime facts, F's views for F's records.

use serde_json::{json, Value};

use crate::block::Block;
use crate::error::{Error, Result};
use crate::fhe;
use crate::gas;
use crate::ids;
use crate::state::{CiphertextRecord, DecryptRecord, PermitRecord};
use crate::vm::{account_from_hex, Vm};
use crate::wire;

/// The PUBLIC view of a registered encrypted value. It carries the digest of the
/// ciphertext body so a client can check a body it fetched from off-chain
/// storage; it does not carry the body, because F never has it.
pub fn ciphertext_view(r: &CiphertextRecord) -> Value {
    json!({
        "handle": hex::encode(r.meta.handle),
        "owner": ids::short_id_string(&r.meta.owner),
        "scheme": r.scheme,
        "digest": hex::encode(r.digest),
        "type": r.meta.kind,
        "level": r.meta.level,
        "epoch": r.meta.epoch,
        "size": r.meta.size,
        "registeredAt": r.meta.registered_at,
        "chainId": ids::id_string(&r.meta.chain_id),
    })
}

/// The PUBLIC view of a capability grant.
pub fn permit_view(r: &PermitRecord) -> Value {
    json!({
        "permitId": hex::encode(r.permit.permit_id),
        "handle": hex::encode(r.permit.handle),
        "grantor": ids::short_id_string(&r.permit.grantor),
        "grantee": ids::short_id_string(&r.permit.grantee),
        "operations": r.permit.operations,
        "expiry": r.permit.expiry,
        "status": r.status,
        "createdAt": r.permit.created_at,
        "chainId": ids::id_string(&r.permit.chain_id),
    })
}

/// The PUBLIC view of a threshold-decryption request. It has no plaintext field:
/// the plaintext goes to the requester's callback off-chain, and F records only
/// the handle the committee agreed the decryption produced.
pub fn decrypt_view(vm: &Vm, r: &DecryptRecord) -> Value {
    // A pending request past its expiry can no longer be answered — every
    // attestation to it is refused — so it is reported expired. The stored status
    // stays pending because nothing ever answered it; expiry is a fact about the
    // clock, and this is the one place that reads the clock to say so.
    let mut status = r.request.status;
    if status == fhe::RequestStatus::Pending
        && r.request.expiry != 0
        && vm.clock.time().unix() > r.request.expiry
    {
        status = fhe::RequestStatus::Expired;
    }
    let attestations: Vec<Value> = r
        .attestations()
        .iter()
        .map(|a| {
            json!({
                "member": ids::short_id_string(&a.member),
                "value": hex::encode(a.value),
            })
        })
        .collect();
    let threshold = vm.epoch(r.request.epoch).map(|e| e.info.threshold).unwrap_or(0);
    let mut view = json!({
        "requestId": hex::encode(r.request.request_id),
        "handle": hex::encode(r.request.ciphertext_handle),
        "requester": ids::short_id_string(&r.request.requester),
        "permitId": hex::encode(r.permit_id),
        "callback": hex::encode(r.request.callback),
        "selector": hex::encode(r.request.callback_selector),
        "epoch": r.request.epoch,
        "status": status.as_str(),
        "attestations": attestations,
        "threshold": threshold,
        "expiry": r.request.expiry,
        "createdAt": r.request.created_at,
        "sourceChain": ids::id_string(&r.request.source_chain),
    });
    if r.request.status == fhe::RequestStatus::Completed {
        view["resultHandle"] = json!(hex::encode(r.request.result_handle));
        view["completedAt"] = json!(r.request.completed_at);
    }
    view
}

/// Decodes a hex-encoded 32-byte identifier — a handle, a permit id, a request id.
/// One decoder for all three, so none of them can drift into accepting a length
/// the others reject.
pub fn hash32(s: &str) -> Result<[u8; 32]> {
    let body = s.strip_prefix("0x").unwrap_or(s);
    let b = hex::decode(body).map_err(|e| Error::InvalidPayload(e.to_string()))?;
    if b.len() != 32 {
        return Err(Error::InvalidPayload(format!("identifier is {} bytes, not 32", b.len())));
    }
    let mut out = [0u8; 32];
    out.copy_from_slice(&b);
    Ok(out)
}

fn param<'a>(params: &'a Value, name: &str) -> Option<&'a Value> {
    params.get(name)
}

fn string_param(params: &Value, name: &str) -> String {
    param(params, name).and_then(|v| v.as_str()).unwrap_or_default().to_string()
}

/// Answers one call. The seam speaks JSON-RPC, so this is where a caller's request
/// becomes a query and a record becomes a view.
pub fn call(vm: &Vm, method: &str, params: &Value) -> Result<Value> {
    match method {
        // ---- the one mutating call: submit a client-signed transaction ----
        "fchain.submitTransaction" => {
            let raw_hex = string_param(params, "tx");
            let body = raw_hex.strip_prefix("0x").unwrap_or(&raw_hex);
            let raw = hex::decode(body).map_err(|e| Error::InvalidPayload(e.to_string()))?;
            let tx = wire::parse_transaction(&raw)?;
            let id = vm.submit_tx(&tx)?;
            Ok(json!({ "txId": ids::id_string(&id) }))
        }

        // ---- FHE runtime facts ----
        "fchain.getPublicParams" => {
            let epoch = vm.current_epoch();
            let p = fhe::DEFAULT_THRESHOLD_PARAMS;
            let mut out = json!({
                "epoch": epoch,
                "logN": p.log_n,
                "logQP": p.log_qp,
                "logScale": p.log_scale,
                "chainId": ids::id_string(&vm.chain_id),
                "threshold": 0,
                "publicKey": "",
            });
            if let Some(rec) = vm.epoch(epoch) {
                out["threshold"] = json!(rec.info.threshold);
                out["publicKey"] = json!(hex::encode(&rec.info.public_key));
            }
            Ok(out)
        }

        "fchain.getCommittee" => {
            let n = match param(params, "epoch").and_then(|v| v.as_u64()) {
                Some(n) => n,
                None => vm.current_epoch(),
            };
            let rec = vm.epoch(n).ok_or(Error::EpochNotFound)?;
            let members: Vec<Value> = rec
                .committee()
                .iter()
                .map(|m| {
                    json!({
                        "nodeId": ids::node_id_string(&m.node_id),
                        "publicKey": hex::encode(&m.public_key),
                        "weight": m.weight,
                        "index": m.index,
                    })
                })
                .collect();
            Ok(json!({
                "epoch": rec.info.epoch,
                "threshold": rec.info.threshold,
                "members": members,
            }))
        }

        // ---- F's ledger ----
        "fchain.getCiphertext" => {
            let h = hash32(&string_param(params, "handle"))?;
            let rec = vm.ciphertext(&h).ok_or(Error::CiphertextNotFound)?;
            Ok(json!({ "ciphertext": ciphertext_view(&rec) }))
        }

        "fchain.listCiphertexts" => {
            let owner_hex = string_param(params, "owner");
            let scheme = string_param(params, "scheme");
            // A filter whose address does not decode is an error, not an empty
            // listing: an empty listing reads as "this owner has nothing".
            let owner = if owner_hex.is_empty() {
                None
            } else {
                Some(
                    account_from_hex(&owner_hex)
                        .map_err(Error::InvalidPayload)?,
                )
            };
            let mut out: Vec<Value> = Vec::new();
            let mut records = vm.ciphertexts();
            // A listing is a read of a map, and a map has no order. Sorting by
            // handle gives one, so two nodes answering the same query answer the
            // same way.
            records.sort_by_key(|r| r.meta.handle);
            for rec in records {
                if let Some(o) = owner {
                    if rec.meta.owner != o {
                        continue;
                    }
                }
                if !scheme.is_empty() && rec.scheme != scheme {
                    continue;
                }
                out.push(ciphertext_view(&rec));
            }
            Ok(json!({ "total": out.len(), "ciphertexts": out }))
        }

        "fchain.getPermit" => {
            let id = hash32(&string_param(params, "permitId"))?;
            let rec = vm.permit(&id).ok_or(Error::PermitNotFound)?;
            Ok(json!({ "permit": permit_view(&rec) }))
        }

        "fchain.getDecrypt" => {
            let id = hash32(&string_param(params, "requestId"))?;
            let rec = vm.decrypt(&id).ok_or(Error::RequestNotFound)?;
            Ok(json!({ "request": decrypt_view(vm, &rec) }))
        }

        // ---- accounts ----
        "fchain.balance" => {
            let acct = account_from_hex(&string_param(params, "address"))
                .map_err(Error::InvalidPayload)?;
            // The two numbers are read independently, and a failure on either is
            // reported. Burned supply coming back zero on a failed read is
            // indistinguishable from a chain that has never settled a fee.
            let bal = vm.balance(&acct)?;
            let burned = vm.burned()?;
            Ok(json!({ "balanceNLux": bal, "burnedNLux": burned }))
        }

        // ---- diagnostics ----
        "fchain.health" => {
            let (healthy, details) = vm.health_check()?;
            let mut map = serde_json::Map::new();
            for (k, v) in details {
                map.insert(k, json!(v));
            }
            Ok(json!({ "healthy": healthy, "details": Value::Object(map) }))
        }

        "fchain.feeSchedule" => {
            let mut entries: Vec<Value> = Vec::new();
            for (op, name) in gas::OP_NAMES {
                if gas::uses_scheme(op) {
                    for (scheme, _) in gas::SCHEME_GAS {
                        let tx = crate::transaction::Transaction {
                            tx_type: op,
                            scheme: scheme.as_bytes().to_vec(),
                            ..crate::transaction::Transaction::default()
                        };
                        let g = gas::gas_for(&tx)?;
                        entries.push(json!({
                            "operation": name,
                            "scheme": scheme,
                            "gas": g,
                            "feeNLux": crate::fee::cost(g, gas::GAS_PRICE)?,
                        }));
                    }
                    continue;
                }
                let tx = crate::transaction::Transaction {
                    tx_type: op,
                    ..crate::transaction::Transaction::default()
                };
                let g = gas::gas_for(&tx)?;
                entries.push(json!({
                    "operation": name,
                    "scheme": "",
                    "gas": g,
                    "feeNLux": crate::fee::cost(g, gas::GAS_PRICE)?,
                }));
            }
            Ok(json!({ "gasPriceNLuxPerGas": gas::GAS_PRICE, "entries": entries }))
        }

        other => Err(Error::InvalidPayload(format!("the method {other} does not exist"))),
    }
}

/// The block the seam hands back, described the way the seam describes one.
pub fn block_summary(b: &Block) -> Value {
    json!({
        "id": ids::id_string(&b.id),
        "parentId": ids::id_string(&b.parent_id),
        "height": b.height,
        "timestamp": b.timestamp.unix(),
        "txs": b.transactions.len(),
    })
}
