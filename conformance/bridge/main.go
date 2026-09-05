// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.

// Command bridgediff asks every running Lux node runtime the same bridge
// questions, in the same order, and reports where their answers differ.
//
// WHAT A BRIDGE ACTUALLY DECIDES. A cross-chain transfer is locked on a source
// chain and released on a destination chain against a proof. Three things must
// hold, and they are the only three worth testing:
//
//	the value ARRIVES        — a valid proof releases exactly the locked amount
//	it arrives ONCE          — a second presentation of the same transfer is refused
//	a FORGED or REPLAYED     — a proof over any other transfer, or a proof
//	proof is REFUSED           re-encoded to look new, releases nothing
//
// WHERE EACH IS DECIDED. The first two are ledger questions and live in the
// B-Chain VM (`luxfi/chains/bridgevm`): `spend.isSettled` keys a settled marker
// by transfer id, and a block that would settle a transfer twice does not
// commit. The third is a CRYPTOGRAPHIC question and bottoms out in the EVM's
// `ecrecover` precompile at 0x01, because the attestation M threshold-signs
// (`chains/internal/bridgeattest`) is a plain secp256k1 signature over a
// domain-bound sha256 digest, and every destination gateway verifies it by
// recovering the signer and comparing to the group key it already trusts.
//
// So `ecrecover` over the REAL bridge digest is the one bridge decision all
// three node runtimes can be asked to make today, live, over JSON-RPC, without
// a funded key and without writing a block. That is what the P-probes below
// do. The S-probes ask, separately, whether a bridge is deployed at all.
//
// THE MALLEABILITY PROBE SAYS WHERE THE ONCE-ONLY GUARD MUST LIVE. Probe P4
// presents (r, n-s) with the recovery bit flipped. That pair verifies against
// the SAME digest and the SAME key as the valid proof, but it is a different 65
// bytes. The precompile is specified to accept both: geth's ecrecover calls
// ValidateSignatureValues with homestead=false, because the low-s rule binds
// transaction signatures, not this precompile. So P4 expects the attester from
// every runtime — and that expectation is the finding. Two distinct proofs
// authorise one transfer, which is why a bridge that keys its once-only guard
// on the proof bytes double-claims, and why bridgevm keys `settledKey` on the
// transfer digest instead. A runtime that refused P4 would be the divergent
// one.
//
// THE R-PROBES ASK EACH RUNTIME TO ACTUALLY MOVE THE VALUE. A release is a
// transaction on the destination chain, so R1 hands every runtime the bridged
// amount, to the bridged recipient, signed for the chain being asked, from an
// account holding nothing. Every runtime must refuse it, and refuse it for the
// same reason. R2 offers one chain's signed release to all of them, which is
// the cross-chain replay: a chain that is not the one it was signed for must
// refuse it as not theirs rather than weigh it on its merits.
//
// A difference on a P- or R-probe is a difference in what a release means, not
// a preference. S-probes and L-probes are expected to differ — the runtimes
// serve different chains, at different heights, and refuse a missing bridge
// chain in their own words — so those are printed and not counted.
//
// Usage:
//
//	go run . -endpoints go=http://127.0.0.1:19600/,cpp=http://127.0.0.1:19730/
//
// Exit code is 1 if any two runtimes disagreed on any probe.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	luxcrypto "github.com/luxfi/crypto"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
)

// ---------------------------------------------------------------------------
// The bridge attestation, as the tree freezes it.
//
// Transcribed from luxfi/chains/internal/bridgeattest/attest.go, which is an
// internal package and so cannot be imported from here. The layout, the field
// order and the domain tag are declared FROZEN there; goldenDigest below pins
// the bytes this file computes, so a drift on either side fails loudly rather
// than quietly testing a digest no chain uses.
// ---------------------------------------------------------------------------

// DomainTag is written raw, with no length prefix, as the first bytes of the
// preimage. It is what stops a bridge attestation being replayed as any other
// message the signer produces.
const DomainTag = "LUX_BRIDGE_TRANSFER_v1"

// Transfer is the message a lock commits to and an attestation authorises. One
// signature over one Transfer authorises exactly one release — of that amount,
// of that asset, to that recipient, on that route, at that nonce.
type Transfer struct {
	SrcChainID uint32
	DstChainID uint32
	Asset      [32]byte
	Amount     uint64
	Recipient  [20]byte
	Nonce      uint64
}

// Digest is the domain-separated signing preimage:
//
//	sha256(tag || u32be(src) || u32be(dst) || asset[32] ||
//	       u64be(amount) || recipient[20] || u64be(nonce))
func (t Transfer) Digest() [32]byte {
	h := sha256.New()
	h.Write([]byte(DomainTag))
	var b [8]byte
	binary.BigEndian.PutUint32(b[:4], t.SrcChainID)
	h.Write(b[:4])
	binary.BigEndian.PutUint32(b[:4], t.DstChainID)
	h.Write(b[:4])
	h.Write(t.Asset[:])
	binary.BigEndian.PutUint64(b[:], t.Amount)
	h.Write(b[:])
	h.Write(t.Recipient[:])
	binary.BigEndian.PutUint64(b[:], t.Nonce)
	h.Write(b[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// goldenDigest pins the digest of the transfer this harness bridges. It was not
// transcribed: it was printed by luxfi/chains/internal/bridgeattest itself, from
// the same field values buildProbes uses, and this file's own Digest is
// re-derived on every run and compared to it. If the two ever disagree the
// harness refuses to run rather than compare runtimes on a message no chain
// would accept.
const goldenDigest = "e6ad636227308db518f35d691a4d81e73c77d85fb84e12b027ec1499f639c611"

// goldenTamperedDigest pins the digest of the same transfer with its amount
// moved from 1000 to 1000000 — the message probe P2 presents the valid
// signature against. Also printed by bridgeattest, for the same reason.
const goldenTamperedDigest = "41a06496101441eefba6869bd432fd8a21bb5b6511b03d47ebf2edc6bda18708"

// The bridge registrar and gateway precompiles, from luxfi/precompile/bridge.
// The registrar keeps the one list of chains this bridge serves; the gateway
// holds the rows. Neither is a place a transfer passes through unless the chain
// turned it on, which is exactly what the S-probes measure.
const (
	registrarAddr = "0x0400000000000000000000000000000000000046"
	gatewayAddr   = "0x0400000000000000000000000000000000000040"

	// selGetCount is the registrar's read of how many chains are registered.
	// Its wire form is one leading selector byte, not a 4-byte ABI hash.
	selGetCount = "0x04"

	ecrecoverAddr = "0x0000000000000000000000000000000000000001"
)

// secp256k1N is the group order. n-s is the other valid s for the same
// signature, and the reason a bridge must pin one of them.
var secp256k1N, _ = new(big.Int).SetString(
	"fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141", 16,
)

// ---------------------------------------------------------------------------
// Probes
// ---------------------------------------------------------------------------

// probe is one question, asked identically of every runtime.
type probe struct {
	name string
	// onBridgeChain sends the probe to the bridge chain's own RPC rather than
	// the EVM's. The ledger questions — did it arrive, did it arrive twice —
	// are not EVM questions; only a running B-Chain can answer them.
	onBridgeChain bool
	// why states what a divergence on this probe would mean. It is printed
	// beside a divergence so the report says what broke, not just that
	// something did.
	why    string
	method string
	params []any
	// expect, when set, is the answer the bridge's own rules require. A
	// runtime that agrees with the others but disagrees with expect is a
	// uniform bug, which is worth more than a disagreement.
	expect string
	// perEndpoint builds the params from the endpoint, for the one question
	// whose bytes are legitimately different per chain: a transaction carries
	// the id of the chain it is for, and that is the replay protection.
	perEndpoint func(e endpoint) []any
	// wantRefusal, when set, is the class every runtime's refusal must fall
	// into. It is how a probe asserts "all three said no, for the same
	// reason" without pinning one runtime's exact wording.
	wantRefusal string
	// splits marks a probe whose answers are SUPPOSED to differ by runtime,
	// with the reason written down. Everything else that differs is counted.
	splits string
}

// answer is one runtime's reply to one probe.
type answer struct {
	result string
	errMsg string
}

func (a answer) String() string {
	if a.errMsg != "" {
		return "error: " + a.errMsg
	}
	return a.result
}

// ecrecoverInput lays out the precompile's 128-byte argument:
// hash(32) || v(32, big-endian 27 or 28) || r(32) || s(32).
func ecrecoverInput(hash [32]byte, v byte, r, s []byte) string {
	buf := make([]byte, 0, 128)
	buf = append(buf, hash[:]...)
	var vw [32]byte
	vw[31] = v
	buf = append(buf, vw[:]...)
	buf = append(buf, leftPad(r, 32)...)
	buf = append(buf, leftPad(s, 32)...)
	return "0x" + hex.EncodeToString(buf)
}

func leftPad(b []byte, n int) []byte {
	if len(b) >= n {
		return b[len(b)-n:]
	}
	out := make([]byte, n)
	copy(out[n-len(b):], b)
	return out
}

// word20 renders a 20-byte address as ecrecover returns it: left-padded to a
// 32-byte word.
func word20(addr []byte) string {
	return "0x" + hex.EncodeToString(leftPad(addr, 32))
}

// buildProbes derives the whole question set from one signed transfer.
func buildProbes() ([]probe, string, error) {
	// The attester. A fixed key, so the run is reproducible and the recovered
	// address in the report can be checked by hand.
	sec := make([]byte, 32)
	sec[31] = 0x2a
	priv, err := secp256k1.ToPrivateKey(sec)
	if err != nil {
		return nil, "", fmt.Errorf("attester key: %w", err)
	}
	pub := secp256k1.PubkeyToAddress(priv.ToECDSA().PublicKey)

	// The transfer being bridged: 1000 units of one asset, from chain 96368
	// (the Lux testnet the RLP import stood up) to chain 200201 (Zoo), at
	// nonce 7.
	var asset [32]byte
	copy(asset[:], []byte("LUX"))
	var recipient [20]byte
	copy(recipient[:], mustHex("9011E888251AB053B7bD1cdB598Db4f9DEd94714"))
	t := Transfer{
		SrcChainID: 96368,
		DstChainID: 200201,
		Asset:      asset,
		Amount:     1000,
		Recipient:  recipient,
		Nonce:      7,
	}
	d := t.Digest()
	if got := hex.EncodeToString(d[:]); got != goldenDigest {
		return nil, "", fmt.Errorf(
			"bridge digest drifted: computed %s, pinned %s", got, goldenDigest,
		)
	}

	sig, err := secp256k1.Sign(d[:], sec)
	if err != nil {
		return nil, "", fmt.Errorf("attest: %w", err)
	}
	r, s, v := sig[:32], sig[32:64], sig[64]+27

	// The same transfer with one field moved: a proof over THIS is a proof of
	// a different release, and must not authorise the first one.
	tampered := t
	tampered.Amount = 1_000_000
	td := tampered.Digest()
	if got := hex.EncodeToString(td[:]); got != goldenTamperedDigest {
		return nil, "", fmt.Errorf(
			"tampered digest drifted: computed %s, pinned %s", got, goldenTamperedDigest,
		)
	}

	// The malleable twin: (r, n-s) with the recovery bit flipped verifies
	// against the same digest and the same key, but is different bytes.
	sBig := new(big.Int).SetBytes(s)
	sFlipped := new(big.Int).Sub(secp256k1N, sBig)
	vFlipped := byte(27)
	if v == 27 {
		vFlipped = 28
	}

	forgedR := append([]byte(nil), r...)
	forgedR[0] ^= 0x01

	attester := word20(pub.Bytes())
	empty := "0x"

	ps := []probe{
		{
			name:   "S1.chainId",
			why:    "which chain each runtime believes it is serving",
			method: "eth_chainId",
			params: []any{},
		},
		{
			name:   "S2.blockNumber",
			why:    "the height a release would land at",
			method: "eth_blockNumber",
			params: []any{},
		},
		{
			name:   "S3.registrar.code",
			why:    "whether the bridge registrar precompile is activated on this chain",
			method: "eth_getCode",
			params: []any{registrarAddr, "latest"},
		},
		{
			name:   "S4.gateway.code",
			why:    "whether the bridge gateway precompile is activated on this chain",
			method: "eth_getCode",
			params: []any{gatewayAddr, "latest"},
		},
		{
			name:   "S5.registrar.getCount",
			why:    "how many chains this bridge says it serves",
			method: "eth_call",
			params: []any{
				map[string]any{"to": registrarAddr, "data": selGetCount},
				"latest",
			},
		},
		{
			name: "P1.valid.arrives",
			why: "a valid attestation must recover the attester — this is the " +
				"check a destination gateway makes before it releases",
			method: "eth_call",
			params: []any{
				map[string]any{
					"to":   ecrecoverAddr,
					"data": ecrecoverInput(d, v, r, s),
					"gas":  "0x100000",
				},
				"latest",
			},
			expect: attester,
		},
		{
			name: "P2.forged.wrongTransfer",
			why: "a proof of a 1000-unit release must not authorise a 1000000-unit " +
				"release; the recovered signer must not be the attester",
			method: "eth_call",
			params: []any{
				map[string]any{
					"to":   ecrecoverAddr,
					"data": ecrecoverInput(td, v, r, s),
					"gas":  "0x100000",
				},
				"latest",
			},
			// Deliberately no expect: recovery over a foreign digest yields
			// SOME address, and which one is what must match across runtimes.
			// The rule it must satisfy — that it is not the attester — is
			// checked in report().
		},
		{
			name:   "P3.forged.signature",
			why:    "a bit-flipped r must not recover the attester",
			method: "eth_call",
			params: []any{
				map[string]any{
					"to":   ecrecoverAddr,
					"data": ecrecoverInput(d, v, forgedR, s),
					"gas":  "0x100000",
				},
				"latest",
			},
		},
		{
			name: "P4.malleable.twin",
			why: "(r, n-s) with the recovery bit flipped verifies against the SAME " +
				"digest and the SAME key, but is different bytes. The precompile " +
				"is specified to accept it — the homestead low-s rule binds " +
				"transaction signatures, not ecrecover — so all three must " +
				"recover the attester here. That is precisely why a bridge " +
				"cannot key its once-only guard on the proof bytes: two " +
				"signatures authorise one transfer",
			method: "eth_call",
			params: []any{
				map[string]any{
					"to":   ecrecoverAddr,
					"data": ecrecoverInput(d, vFlipped, r, sFlipped.Bytes()),
					"gas":  "0x100000",
				},
				"latest",
			},
			expect: attester,
		},
		{
			name:   "P5.malformed.recoveryId",
			why:    "a recovery id outside {27,28} must be refused, not coerced",
			method: "eth_call",
			params: []any{
				map[string]any{
					"to":   ecrecoverAddr,
					"data": ecrecoverInput(d, 29, r, s),
					"gas":  "0x100000",
				},
				"latest",
			},
			expect: empty,
		},
		{
			name:   "P6.malformed.zeroSignature",
			why:    "an all-zero signature must recover nobody",
			method: "eth_call",
			params: []any{
				map[string]any{
					"to":   ecrecoverAddr,
					"data": ecrecoverInput(d, 27, make([]byte, 32), make([]byte, 32)),
					"gas":  "0x100000",
				},
				"latest",
			},
			expect: empty,
		},
		{
			name: "P7.replay.identical",
			why: "ecrecover is stateless, so presenting the same valid proof twice " +
				"validates twice; the once-only guard is the settled ledger, " +
				"not this precompile, and a runtime with no ledger has no guard",
			method: "eth_call",
			params: []any{
				map[string]any{
					"to":   ecrecoverAddr,
					"data": ecrecoverInput(d, v, r, s),
					"gas":  "0x100000",
				},
				"latest",
			},
			expect: attester,
		},

		// The ledger half. Arrival and once-only are not decisions the EVM
		// makes; they are decisions the B-Chain's settled ledger makes, keyed
		// by transfer digest. These probes ask each runtime for that chain. A
		// runtime that cannot answer has no once-only guard to compare — which
		// is the answer, not a gap in the harness.
		// The release half, attempted for real. A bridge release lands as a
		// transaction that moves value on the destination chain, so these ask
		// each runtime to move the bridged amount to the bridged recipient and
		// compare the refusals. The attester holds nothing on any of these
		// chains, so every runtime must refuse — and refuse for the same
		// reason. R2 is the cross-chain replay: one chain's signed transfer,
		// offered to every chain.
		{
			name: "R1.release.unfunded",
			why: "the bridged amount, to the bridged recipient, signed for the " +
				"chain being asked: an unbacked release must be refused by " +
				"every runtime, for the same reason",
			method: "eth_sendRawTransaction",
			perEndpoint: func(e endpoint) []any {
				raw, err := signTransfer(sec, e.chainID, recipient, 1000)
				if err != nil {
					return []any{"0x"}
				}
				return []any{raw}
			},
			wantRefusal: "funds",
		},
		{
			name: "R2.release.replayedAcrossChains",
			why: "one chain's signed release, offered to all of them: a chain " +
				"that is not the one it was signed for must refuse it as such, " +
				"not weigh it on its merits",
			splits: "the chain it was signed for judges it on its merits; every " +
				"other chain refuses it as not theirs. That split is the " +
				"replay protection working",
			method: "eth_sendRawTransaction",
			perEndpoint: func(e endpoint) []any {
				raw, err := signTransfer(sec, big.NewInt(96368), recipient, 1000)
				if err != nil {
					return []any{"0x"}
				}
				return []any{raw}
			},
		},

		{
			name:          "L1.bridgeChain.info",
			why:           "whether this runtime serves a bridge chain at all",
			onBridgeChain: true,
			method:        "bridge_getBridgeInfo",
			params:        []any{map[string]any{}},
		},
		{
			name: "L2.settled.status",
			why: "whether the transfer this harness signed is already settled — " +
				"the once-only guard, keyed by transfer digest, not by proof bytes",
			onBridgeChain: true,
			method:        "bridge_getStatus",
			params: []any{map[string]any{
				"requestID": "0x" + hex.EncodeToString(d[:]),
			}},
		},
	}
	return ps, attester, nil
}

// signTransfer builds the transaction a release would land as: a plain value
// transfer of amount to recipient, signed for chainID by the attester's key.
// RLP appears here and only here, because that is the C-Chain's external
// Ethereum wire format and a raw transaction has no other encoding.
func signTransfer(sec []byte, chainID *big.Int, recipient [20]byte, amount uint64) (string, error) {
	key, err := luxcrypto.ToECDSA(sec)
	if err != nil {
		return "", err
	}
	to := common.BytesToAddress(recipient[:])
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(30_000_000_000),
		Gas:      21_000,
		To:       &to,
		Value:    new(big.Int).SetUint64(amount),
	})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), key)
	if err != nil {
		return "", err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(raw), nil
}

// verdict names why a runtime said no, so three runtimes phrasing the same
// refusal differently still compare equal. A runtime that did not say no at all
// is "admitted", which is the answer that matters most. A refusal nobody can
// classify is reported as itself rather than folded into "other", because an
// unrecognised refusal is the interesting one.
func verdict(a answer) string {
	if a.errMsg == "" {
		return "admitted"
	}
	m := strings.ToLower(a.errMsg)
	switch {
	case strings.Contains(m, "insufficient funds"), strings.Contains(m, "insufficient balance"):
		return "funds"
	case strings.Contains(m, "chain id"), strings.Contains(m, "chainid"),
		strings.Contains(m, "invalid sender"), strings.Contains(m, "wrong chain"),
		strings.Contains(m, "signed for chain"):
		return "chainid"
	case strings.Contains(m, "nonce"):
		return "nonce"
	case strings.Contains(m, "gas price"), strings.Contains(m, "underpriced"),
		strings.Contains(m, "fee cap"):
		return "price"
	case strings.Contains(m, "not supported"), strings.Contains(m, "method"),
		strings.Contains(m, "unreachable"), strings.Contains(m, "no such"),
		strings.Contains(m, "not json-rpc"):
		return "unsupported"
	default:
		return "unclassified: " + a.errMsg
	}
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// ---------------------------------------------------------------------------
// Asking
// ---------------------------------------------------------------------------

type endpoint struct {
	lang string
	// eth is where this runtime serves the C-Chain's JSON-RPC.
	eth string
	// bridge is where a B-Chain would be served on the same node. It is
	// derived, not configured: /ext/bc/B/rpc is the one path a bridge chain
	// answers on, and a runtime serving it elsewhere is itself a divergence.
	bridge string
	// chainID is read from the runtime itself, not configured, because a
	// transaction is signed for the chain that will execute it.
	chainID *big.Int
}

func newEndpoint(lang, ethURL string) (endpoint, error) {
	u, err := url.Parse(ethURL)
	if err != nil {
		return endpoint{}, fmt.Errorf("endpoint %s: %w", lang, err)
	}
	b := *u
	b.Path = "/ext/bc/B/rpc"
	b.RawQuery = ""
	return endpoint{lang: lang, eth: ethURL, bridge: b.String()}, nil
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcReply struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func ask(client *http.Client, url, method string, params []any) answer {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return answer{errMsg: err.Error()}
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return answer{errMsg: "unreachable: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return answer{errMsg: err.Error()}
	}
	var reply rpcReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return answer{errMsg: fmt.Sprintf("not json-rpc: %.80s", strings.TrimSpace(string(raw)))}
	}
	if reply.Error != nil {
		return answer{errMsg: reply.Error.Message}
	}
	var out string
	if err := json.Unmarshal(reply.Result, &out); err != nil {
		return answer{result: strings.TrimSpace(string(reply.Result))}
	}
	return answer{result: out}
}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

func report(ps []probe, eps []endpoint, table map[string]map[string]answer, attester string) int {
	divergences := 0
	violations := 0

	fmt.Printf("attester (the key a destination gateway would trust): %s\n\n", attester)

	for _, p := range ps {
		fmt.Printf("%-26s %s\n", p.name, p.why)
		seen := map[string][]string{}
		for _, e := range eps {
			a := table[p.name][e.lang]
			shown := a.String()
			// An R-probe compares WHY each runtime refused, not the wording.
			key := shown
			if strings.HasPrefix(p.name, "R") {
				key = verdict(a)
				shown = fmt.Sprintf("%-12s (%s)", key, a)
			}
			fmt.Printf("    %-11s %s\n", e.lang, shown)
			seen[key] = append(seen[key], e.lang)
		}
		if len(seen) > 1 {
			switch {
			case p.splits != "":
				fmt.Printf("    (expected split: %s)\n", p.splits)
			case strings.HasPrefix(p.name, "P"), strings.HasPrefix(p.name, "R"):
				// A P-probe is a pure function of bytes every runtime was
				// handed identically; an R-probe asks every runtime to do the
				// same thing to its own chain. Disagreement in either is a
				// difference in what a release means, not a preference.
				divergences++
				fmt.Printf("    !! DIVERGENCE — the same request, judged differently\n")
			case strings.HasPrefix(p.name, "L"):
				fmt.Printf("    (each refuses in its own words; no runtime " +
					"serves a bridge chain to disagree with)\n")
			default:
				fmt.Printf("    (chain-specific; not a bridge decision)\n")
			}
		}
		if p.wantRefusal != "" {
			for _, e := range eps {
				got := verdict(table[p.name][e.lang])
				if got != p.wantRefusal {
					violations++
					fmt.Printf("    !! %s answered %q, the bridge's rule requires %q\n",
						e.lang, got, p.wantRefusal)
				}
			}
		}
		if strings.HasPrefix(p.name, "R") {
			for _, e := range eps {
				if table[p.name][e.lang].errMsg == "" {
					violations++
					fmt.Printf("    !! %s ADMITTED an unbacked release and "+
						"answered with a hash (%s) — a relayer reads that as accepted\n",
						e.lang, table[p.name][e.lang])
				}
			}
		}
		if p.expect != "" {
			for _, e := range eps {
				got := table[p.name][e.lang].String()
				if got != p.expect {
					violations++
					fmt.Printf("    !! %s answered %s, the bridge's rule requires %s\n",
						e.lang, got, p.expect)
				}
			}
		}
		if p.name == "P2.forged.wrongTransfer" || p.name == "P3.forged.signature" {
			for _, e := range eps {
				if table[p.name][e.lang].String() == attester {
					violations++
					fmt.Printf("    !! %s recovered the attester from a forged proof\n", e.lang)
				}
			}
		}
		fmt.Println()
	}

	fmt.Printf("%d divergence(s), %d rule violation(s)\n", divergences, violations)
	if divergences+violations > 0 {
		return 1
	}
	return 0
}

func main() {
	var (
		list    = flag.String("endpoints", "", "comma-separated lang=url")
		timeout = flag.Duration("timeout", 10*time.Second, "per-request timeout")
	)
	flag.Parse()

	if *list == "" {
		fmt.Fprintln(os.Stderr, "-endpoints is required, e.g. go=http://127.0.0.1:19600/")
		os.Exit(2)
	}
	var eps []endpoint
	for _, part := range strings.Split(*list, ",") {
		lang, raw, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "bad endpoint %q, want lang=url\n", part)
			os.Exit(2)
		}
		e, err := newEndpoint(lang, raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		eps = append(eps, e)
	}
	sort.SliceStable(eps, func(i, j int) bool { return eps[i].lang < eps[j].lang })

	ps, attester, err := buildProbes()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	client := &http.Client{Timeout: *timeout}

	// A transaction is signed for the chain that will execute it, so read each
	// runtime's own id before asking it to move anything.
	for i := range eps {
		a := ask(client, eps[i].eth, "eth_chainId", []any{})
		id, ok := new(big.Int).SetString(strings.TrimPrefix(a.result, "0x"), 16)
		if !ok {
			fmt.Fprintf(os.Stderr, "%s: no chain id (%s)\n", eps[i].lang, a)
			os.Exit(2)
		}
		eps[i].chainID = id
	}

	table := map[string]map[string]answer{}
	for _, p := range ps {
		table[p.name] = map[string]answer{}
		for _, e := range eps {
			target := e.eth
			if p.onBridgeChain {
				target = e.bridge
			}
			params := p.params
			if p.perEndpoint != nil {
				params = p.perEndpoint(e)
			}
			table[p.name][e.lang] = ask(client, target, p.method, params)
		}
	}
	os.Exit(report(ps, eps, table, attester))
}
