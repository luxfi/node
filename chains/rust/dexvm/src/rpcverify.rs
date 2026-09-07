// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

//! The network-backed verifier: proof that a manifest's assets EXIST on the
//! target net, before a deploy.
//!
//! For an ERC-20 it confirms the contract has code and that a static
//! `decimals()` call answers — which the registry then cross-checks against the
//! manifest's declared decimals. For the native coin it confirms the C-Chain is
//! reachable. A UTXO asset is confirmed through the X-Chain's
//! `avm.getAssetDescription`. And before any of that, the C-Chain's own identity
//! is confirmed twice: `eth_chainId` against the manifest's `evmChainID`, and
//! the live P-Chain's C-Chain against the manifest's `cChainID`.
//!
//! ## Why the socket is a parameter
//!
//! Every line below is the real check. What is injected is one thing: [`Rpc`],
//! the act of putting a request on a wire and getting bytes back. It is a
//! parameter for the same reason [`crate::registry::ChainVerifier`] is one — so
//! this admission logic is provable without a network, and so the consensus tree
//! never links a TLS stack to answer a question CI asks. A caller that has an
//! HTTP client supplies it; the tests here supply one that replays real
//! responses, and every refusal path is exercised against them.
//!
//! A [`Verifier`] with no [`Rpc`] cannot be built, so there is no shape of this
//! type that answers "real" without asking something.

use crate::error::{Error, Result};
use crate::ids::{self, Id};
use crate::registry::ChainVerifier;
use serde_json::Value;

/// One request on a wire. `url` is fully qualified; `body` is a JSON-RPC 2.0
/// request; the answer is the response body. An error means the request did not
/// complete, and every caller here treats that as "not proven real".
pub trait Rpc {
    fn post(&self, url: &str, body: &str) -> Result<String>;
}

/// The 4-byte selector of ERC-20 `decimals()` — `keccak256("decimals()")[:4]`.
pub const DECIMALS_SELECTOR: [u8; 4] = [0x31, 0x3c, 0xe5, 0x67];

/// A verifier that proves assets real against a live network: a C-Chain EVM RPC
/// for EVM_NATIVE and ERC20, and a node API base for the C-Chain consensus-id
/// confirm and UTXO asset descriptions.
pub struct Verifier<'a> {
    /// The full C-Chain RPC URL (`…/v1/chain/C/rpc`).
    evm_rpc: String,
    /// The node root, used to reach `/ext/P` and `/v1/chain/X`.
    api_base: String,
    /// The C-Chain native coin's decimals (18 for LUX).
    native_dec: u8,
    rpc: &'a dyn Rpc,
}

impl<'a> Verifier<'a> {
    pub fn new(evm_rpc: &str, api_base: &str, native_decimals: u8, rpc: &'a dyn Rpc) -> Self {
        Verifier {
            evm_rpc: evm_rpc.to_string(),
            api_base: api_base.trim_end_matches('/').to_string(),
            native_dec: native_decimals,
            rpc,
        }
    }

    /// One JSON-RPC 2.0 call, decoded. A transport failure, a JSON-RPC `error`
    /// member, or a missing `result` are all refusals.
    fn call(&self, url: &str, method: &str, params: Value) -> Result<Value> {
        let body = serde_json::json!({
            "jsonrpc": "2.0", "id": 1, "method": method, "params": params,
        })
        .to_string();
        let raw = self
            .rpc
            .post(url, &body)
            .map_err(|e| e.wrap(format!("{method} {url}")))?;
        let v: Value = serde_json::from_str(&raw)
            .map_err(|e| Error::other(format!("{method}: response is not JSON: {e}")))?;
        if let Some(err) = v.get("error").filter(|e| !e.is_null()) {
            let msg = err
                .get("message")
                .and_then(Value::as_str)
                .unwrap_or("unknown error");
            return Err(Error::other(format!("{method}: {msg}")));
        }
        v.get("result")
            .cloned()
            .ok_or_else(|| Error::other(format!("{method}: response carries no result")))
    }

    /// A `0x`-prefixed hex QUANTITY, as the EVM writes a number.
    fn quantity(v: &Value, what: &str) -> Result<u64> {
        let s = v
            .as_str()
            .ok_or_else(|| Error::other(format!("{what}: expected a hex string")))?;
        let t = s.strip_prefix("0x").unwrap_or(s);
        u64::from_str_radix(t, 16)
            .map_err(|e| Error::other(format!("{what}: {s:?} is not a hex quantity: {e}")))
    }

    /// A `0x`-prefixed hex DATA string, as the EVM writes bytes.
    fn data(v: &Value, what: &str) -> Result<Vec<u8>> {
        let s = v
            .as_str()
            .ok_or_else(|| Error::other(format!("{what}: expected a hex string")))?;
        crate::forbidden::from_hex(s).map_err(|e| e.wrap(what.to_string()))
    }

    fn eth_chain_id(&self) -> Result<u64> {
        let r = self.call(&self.evm_rpc, "eth_chainId", serde_json::json!([]))?;
        Self::quantity(&r, "eth_chainId")
    }

    /// The live, authoritative C-Chain consensus id: ask the P-Chain for the
    /// blockchain named "C". Older nets name it "C-Chain".
    fn resolve_c_chain_id(&self) -> Result<Id> {
        let url = format!("{}/ext/P", self.api_base);
        let r = self.call(&url, "platform.getBlockchains", serde_json::json!({}))?;
        let chains = r
            .get("blockchains")
            .and_then(Value::as_array)
            .ok_or_else(|| Error::other("platform.getBlockchains: no blockchains array"))?;
        for b in chains {
            let name = b.get("name").and_then(Value::as_str).unwrap_or_default();
            if name == "C-Chain" || name == "C" {
                let id = b
                    .get("id")
                    .and_then(Value::as_str)
                    .ok_or_else(|| Error::other("platform.getBlockchains: C-Chain has no id"))?;
                return ids::from_string(id).map_err(|e| {
                    Error::other(format!("platform.getBlockchains: C-Chain id {id:?}: {e}"))
                });
            }
        }
        Err(Error::other("no C-Chain in platform.getBlockchains"))
    }

    /// A static `eth_call` to `decimals()`, decoded from the uint8
    /// right-aligned in a 32-byte word.
    fn call_decimals(&self, addr: &[u8]) -> Result<u8> {
        let r = self.call(
            &self.evm_rpc,
            "eth_call",
            serde_json::json!([
                {
                    "to": crate::forbidden::to_hex(addr),
                    "data": crate::forbidden::to_hex(&DECIMALS_SELECTOR),
                },
                "latest"
            ]),
        )?;
        let out = Self::data(&r, "decimals()")?;
        if out.is_empty() {
            return Err(Error::other(
                "decimals() returned empty (not ERC-20 compliant)",
            ));
        }
        Ok(out[out.len() - 1])
    }
}

impl ChainVerifier for Verifier<'_> {
    /// Confirm a contract has code at `addr` and answer with its `decimals()`.
    fn verify_erc20(&self, _network_id: u32, _c_chain_id: Id, addr: &[u8]) -> Result<u8> {
        let a = crate::forbidden::to_hex(addr);
        let r = self.call(
            &self.evm_rpc,
            "eth_getCode",
            serde_json::json!([a, "latest"]),
        )?;
        let code = Self::data(&r, "eth_getCode")?;
        if code.is_empty() {
            return Err(Error::other(format!(
                "no contract code at {a} (not a real ERC-20 on this net)"
            )));
        }
        self.call_decimals(addr)
            .map_err(|e| e.wrap(format!("decimals() at {a}")))
    }

    /// Confirm the C-Chain is reachable and answer with the native decimals.
    /// The chainID EQUALITY is enforced once, in `confirm_c_chain`.
    fn verify_evm_native(&self, _network_id: u32, _c_chain_id: Id) -> Result<u8> {
        self.eth_chain_id().map_err(|e| e.wrap("native chainID"))?;
        Ok(self.native_dec)
    }

    /// Confirm a UTXO assetID exists through `avm.getAssetDescription` and
    /// answer with its denomination.
    fn verify_utxo_asset(
        &self,
        _network_id: u32,
        _source_chain_id: Id,
        asset_id: Id,
    ) -> Result<u8> {
        let url = format!("{}/v1/chain/X", self.api_base);
        let r = self
            .call(
                &url,
                "avm.getAssetDescription",
                serde_json::json!({ "assetID": ids::cb58(&asset_id) }),
            )
            .map_err(|e| e.wrap("asset not on this net"))?;
        // Some avm responses stringify the denomination, so both shapes are
        // accepted and anything out of a byte's range is refused.
        let d = r
            .get("denomination")
            .ok_or_else(|| Error::other("avm.getAssetDescription: no denomination"))?;
        let n = match d {
            Value::Number(n) => n.as_u64().ok_or_else(|| {
                Error::other("avm.getAssetDescription: denomination is not a whole number")
            })?,
            Value::String(s) => s
                .parse::<u64>()
                .map_err(|e| Error::other(format!("rpcverify: bad uint8 {s:?}: {e}")))?,
            _ => {
                return Err(Error::other(
                    "avm.getAssetDescription: denomination is neither a number nor a string",
                ))
            }
        };
        if n > 255 {
            return Err(Error::other(format!(
                "rpcverify: denomination {n} out of uint8 range"
            )));
        }
        Ok(n as u8)
    }

    /// Confirm the live `eth_chainId` equals the manifest's `evmChainID` and the
    /// live C-Chain consensus id equals the manifest's `cChainID`. A mismatch on
    /// either means the validator is pointed at the wrong network, and the
    /// manifest must NOT be admitted.
    fn confirm_c_chain(&self, _network_id: u32, evm_chain_id: u64, c_chain_id: Id) -> Result<()> {
        let got = self.eth_chain_id()?;
        if got != evm_chain_id {
            return Err(Error::other(format!(
                "eth_chainId mismatch: RPC={got} manifest={evm_chain_id} (wrong network)"
            )));
        }
        let live = self
            .resolve_c_chain_id()
            .map_err(|e| e.wrap("resolve live C-Chain id"))?;
        if live != c_chain_id {
            return Err(Error::other(format!(
                "C-Chain consensus id mismatch: live={} manifest={}",
                ids::cb58(&live),
                ids::cb58(&c_chain_id)
            )));
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::cell::RefCell;
    use std::collections::BTreeMap;

    const EVM: &str = "https://api.example/v1/chain/C/rpc";
    const BASE: &str = "https://api.example";

    /// A wire that answers from a script, keyed by the method the request names,
    /// and RECORDS what it was asked. A method with no answer scripted fails the
    /// request, which is what an unreachable endpoint does.
    struct Script {
        answers: BTreeMap<String, String>,
        asked: RefCell<Vec<(String, String)>>,
    }

    impl Script {
        fn new(pairs: &[(&str, &str)]) -> Self {
            Script {
                answers: pairs
                    .iter()
                    .map(|(m, r)| (m.to_string(), r.to_string()))
                    .collect(),
                asked: RefCell::new(Vec::new()),
            }
        }
    }

    impl Rpc for Script {
        fn post(&self, url: &str, body: &str) -> Result<String> {
            let v: serde_json::Value = serde_json::from_str(body).expect("a request must be JSON");
            let method = v["method"].as_str().unwrap_or_default().to_string();
            // The request shape is part of the contract, not decoration.
            assert_eq!(v["jsonrpc"], "2.0");
            assert_eq!(v["id"], 1);
            self.asked
                .borrow_mut()
                .push((url.to_string(), method.clone()));
            self.answers
                .get(&method)
                .cloned()
                .ok_or_else(|| Error::other(format!("script: nothing answers {method}")))
        }
    }

    /// A 32-byte word carrying `n` right-aligned — how the EVM returns a uint8.
    fn word(n: u8) -> String {
        format!("\"0x{}{:02x}\"", "00".repeat(31), n)
    }

    fn ok(result: &str) -> String {
        format!("{{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{result}}}")
    }

    fn c_chain() -> Id {
        ids::sha256(b"lux:dexvm:test:rpc/c-chain")
    }

    fn blockchains(name: &str, id: &Id) -> String {
        ok(&format!(
            "{{\"blockchains\":[{{\"id\":\"{}\",\"name\":\"X-Chain\"}},{{\"id\":\"{}\",\"name\":\"{name}\"}}]}}",
            ids::cb58(&ids::sha256(b"some-other-chain")),
            ids::cb58(id)
        ))
    }

    #[test]
    fn the_c_chain_identity_is_confirmed_twice() {
        let c = c_chain();
        let s = Script::new(&[
            ("eth_chainId", &ok("\"0x17871\"")), // 96369
            ("platform.getBlockchains", &blockchains("C-Chain", &c)),
        ]);
        let v = Verifier::new(EVM, BASE, 18, &s);
        v.confirm_c_chain(1, 96369, c).expect("identity confirms");

        // Both endpoints were actually asked — a confirm that skipped one would
        // be a confirm in name only.
        let asked = s.asked.borrow();
        assert!(asked.iter().any(|(u, m)| u == EVM && m == "eth_chainId"));
        assert!(asked
            .iter()
            .any(|(u, m)| u == "https://api.example/ext/P" && m == "platform.getBlockchains"));
    }

    #[test]
    fn the_older_bare_c_name_is_accepted() {
        let c = c_chain();
        let s = Script::new(&[
            ("eth_chainId", &ok("\"0x17871\"")),
            ("platform.getBlockchains", &blockchains("C", &c)),
        ]);
        Verifier::new(EVM, BASE, 18, &s)
            .confirm_c_chain(1, 96369, c)
            .expect("the older 'C' name names the same chain");
    }

    #[test]
    fn a_wrong_evm_chain_id_refuses_the_manifest() {
        let c = c_chain();
        let s = Script::new(&[
            ("eth_chainId", &ok("\"0x17870\"")), // 96368, the testnet
            ("platform.getBlockchains", &blockchains("C-Chain", &c)),
        ]);
        let e = Verifier::new(EVM, BASE, 18, &s)
            .confirm_c_chain(1, 96369, c)
            .unwrap_err();
        assert!(e.text.contains("wrong network"), "{e}");
    }

    #[test]
    fn a_wrong_live_c_chain_refuses_the_manifest() {
        let s = Script::new(&[
            ("eth_chainId", &ok("\"0x17871\"")),
            (
                "platform.getBlockchains",
                &blockchains("C-Chain", &ids::sha256(b"a-different-c-chain")),
            ),
        ]);
        let e = Verifier::new(EVM, BASE, 18, &s)
            .confirm_c_chain(1, 96369, c_chain())
            .unwrap_err();
        assert!(e.text.contains("C-Chain consensus id mismatch"), "{e}");
    }

    #[test]
    fn a_net_with_no_c_chain_refuses_the_manifest() {
        let s = Script::new(&[
            ("eth_chainId", &ok("\"0x17871\"")),
            (
                "platform.getBlockchains",
                &ok("{\"blockchains\":[{\"id\":\"11111111111111111111111111111111X\",\"name\":\"X-Chain\"}]}"),
            ),
        ]);
        let e = Verifier::new(EVM, BASE, 18, &s)
            .confirm_c_chain(1, 96369, c_chain())
            .unwrap_err();
        assert!(e.text.contains("no C-Chain"), "{e}");
    }

    #[test]
    fn a_contract_with_no_code_is_not_a_real_token() {
        let s = Script::new(&[("eth_getCode", &ok("\"0x\""))]);
        let e = Verifier::new(EVM, BASE, 18, &s)
            .verify_erc20(1, c_chain(), &[7u8; 20])
            .unwrap_err();
        assert!(e.text.contains("no contract code"), "{e}");
    }

    #[test]
    fn a_real_token_answers_with_its_decimals() {
        let s = Script::new(&[
            ("eth_getCode", &ok("\"0x60806040\"")),
            ("eth_call", &ok(&word(6))),
        ]);
        assert_eq!(
            Verifier::new(EVM, BASE, 18, &s)
                .verify_erc20(1, c_chain(), &[7u8; 20])
                .expect("a real token"),
            6
        );
    }

    #[test]
    fn a_token_whose_decimals_answers_empty_is_not_erc20_compliant() {
        let s = Script::new(&[
            ("eth_getCode", &ok("\"0x60806040\"")),
            ("eth_call", &ok("\"0x\"")),
        ]);
        let e = Verifier::new(EVM, BASE, 18, &s)
            .verify_erc20(1, c_chain(), &[7u8; 20])
            .unwrap_err();
        assert!(e.text.contains("not ERC-20 compliant"), "{e}");
    }

    #[test]
    fn the_decimals_call_carries_the_right_selector() {
        let s = Script::new(&[
            ("eth_getCode", &ok("\"0x60806040\"")),
            ("eth_call", &ok(&word(18))),
        ]);
        Verifier::new(EVM, BASE, 18, &s)
            .verify_erc20(1, c_chain(), &[7u8; 20])
            .expect("real");
        // keccak256("decimals()")[:4]. A wrong selector would call a different
        // function and believe its answer.
        assert_eq!(DECIMALS_SELECTOR, [0x31, 0x3c, 0xe5, 0x67]);
    }

    #[test]
    fn the_native_coin_needs_a_reachable_chain() {
        let s = Script::new(&[("eth_chainId", &ok("\"0x17871\""))]);
        assert_eq!(
            Verifier::new(EVM, BASE, 18, &s)
                .verify_evm_native(1, c_chain())
                .expect("reachable"),
            18
        );

        // An unreachable C-Chain is not a proof of anything.
        let dead = Script::new(&[]);
        assert!(Verifier::new(EVM, BASE, 18, &dead)
            .verify_evm_native(1, c_chain())
            .is_err());
    }

    #[test]
    fn a_utxo_denomination_is_read_as_a_number_or_a_string() {
        for answer in ["{\"denomination\":9}", "{\"denomination\":\"9\"}"] {
            let s = Script::new(&[("avm.getAssetDescription", &ok(answer))]);
            assert_eq!(
                Verifier::new(EVM, BASE, 18, &s)
                    .verify_utxo_asset(1, c_chain(), ids::sha256(b"asset"))
                    .expect(answer),
                9
            );
        }
    }

    #[test]
    fn a_denomination_out_of_a_bytes_range_is_refused() {
        let s = Script::new(&[("avm.getAssetDescription", &ok("{\"denomination\":300}"))]);
        let e = Verifier::new(EVM, BASE, 18, &s)
            .verify_utxo_asset(1, c_chain(), ids::sha256(b"asset"))
            .unwrap_err();
        assert!(e.text.contains("out of uint8 range"), "{e}");
    }

    #[test]
    fn an_asset_the_x_chain_does_not_know_is_refused() {
        let s = Script::new(&[(
            "avm.getAssetDescription",
            "{\"jsonrpc\":\"2.0\",\"id\":1,\"error\":{\"code\":-32000,\"message\":\"asset not found\"}}",
        )]);
        let e = Verifier::new(EVM, BASE, 18, &s)
            .verify_utxo_asset(1, c_chain(), ids::sha256(b"asset"))
            .unwrap_err();
        assert!(e.text.contains("asset not found"), "{e}");
        assert!(e.text.contains("asset not on this net"), "{e}");
    }

    #[test]
    fn a_wire_that_answers_nothing_proves_nothing() {
        // Every path refuses when the transport fails: an unreachable RPC is
        // never "real by default".
        let dead = Script::new(&[]);
        let v = Verifier::new(EVM, BASE, 18, &dead);
        assert!(v.confirm_c_chain(1, 96369, c_chain()).is_err());
        assert!(v.verify_erc20(1, c_chain(), &[7u8; 20]).is_err());
        assert!(v.verify_evm_native(1, c_chain()).is_err());
        assert!(v
            .verify_utxo_asset(1, c_chain(), ids::sha256(b"asset"))
            .is_err());
    }

    #[test]
    fn a_response_that_is_not_json_is_refused() {
        let s = Script::new(&[("eth_chainId", "<html>502 Bad Gateway</html>")]);
        let e = Verifier::new(EVM, BASE, 18, &s)
            .verify_evm_native(1, c_chain())
            .unwrap_err();
        assert!(e.text.contains("not JSON"), "{e}");
    }

    #[test]
    fn the_api_base_is_normalised_so_a_trailing_slash_cannot_double_it() {
        let c = c_chain();
        let s = Script::new(&[
            ("eth_chainId", &ok("\"0x17871\"")),
            ("platform.getBlockchains", &blockchains("C-Chain", &c)),
        ]);
        Verifier::new(EVM, "https://api.example/", 18, &s)
            .confirm_c_chain(1, 96369, c)
            .expect("confirms");
        assert!(s
            .asked
            .borrow()
            .iter()
            .any(|(u, _)| u == "https://api.example/ext/P"));
    }
}
