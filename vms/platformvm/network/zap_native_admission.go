// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"fmt"

	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/proto/zap_native"
)

// LP-023 Red round 7 R7V5 — mempool admission gate for zap_native tx
// kinds whose executor body is not-yet-implemented.
//
// Threat model (Neo's ZAP-activation=0 rollout):
//
// As of v1.28.19, ZAP-native wire format is mandatory from genesis
// (ZAPActivationUnix=0, codec_select.go). The mempool starts accepting
// ZAP-shaped tx bytes the moment the image lands. If a tx kind whose
// executor body is still a stub (returns "not yet implemented") enters
// the mempool, it CANNOT execute — but it CAN sit there forever,
// gossiped between nodes, consuming RAM, and (worst case) re-tried by
// validators on every block-build cycle until it's manually evicted.
//
// The cleanest defense is to refuse at the admission boundary. The
// wire-decode path stays canonical (parses confirm the buffer is
// well-formed); only the EXECUTION path is gated. Once a stub executor
// body lands (batch 6 or later), the corresponding entry can be removed
// from the gate.
//
// Gate covers BOTH today's path (legacy txs.* struct Visit dispatch
// hits the standard_tx_executor stub at line 636) AND tomorrow's path
// (zap_native wire bytes parse-and-dispatch through a future wire-aware
// admission flow). The double coverage is intentional: we don't need
// to track which path is "live" — we refuse on either.
//
// REMOVAL CHECKLIST (when a stub executor body lands):
//
//  1. Implement the executor body in
//     vms/platformvm/txs/executor/standard_tx_executor.go (current
//     CreateSovereignL1Tx stub at line 636).
//  2. Remove the matching case from IsZapNativeNotYetExecutable below.
//  3. Drop the matching test from zap_native_admission_test.go.
//  4. Document the executor-body PR in LP-023 batch-6 (or later) so the
//     reviewer can cross-check the gate removal against the body landing.
//
// Until those steps are complete, every tx of the gated kinds is
// rejected at admission. Validators NEVER accept them; gossip NEVER
// propagates them; the mempool NEVER holds them.

// ErrZapNativeNotYetExecutable is returned at mempool admission when a tx
// is of a zap_native kind whose executor body is still a stub. Wire-decode
// path is canonical (the buffer is well-formed); execution is gated. See
// the package comment for the removal checklist.
//
// Wrapped per-kind with the tx-kind name for context; consumers match
// the bare error via errors.Is.
var ErrZapNativeNotYetExecutable = errors.New(
	"zap_native: tx kind executor not yet implemented; rejected at mempool admission " +
		"to prevent zombie txs during Neo ZAP-activation=0 rollout (LP-023 R7V5)",
)

// notYetExecutableLegacyKinds returns the set of legacy txs.UnsignedTx
// concrete types whose corresponding zap_native tx kind has no executor
// body yet. The legacy struct is what enters Visit-dispatch today; the
// zap_native wire kind is what tomorrow's parser would route through.
//
// TODAY: only txs.CreateSovereignL1Tx has a not-yet-implemented stub.
// The legacy ConvertNetworkToL1Tx (line 639) and RegisterL1ValidatorTx
// (line 761) executors are wired and working. They're listed in the
// brief because the zap_native wire variants of those types may still
// route through paths that hit the stubbed code — defense-in-depth: the
// gate covers all three even though only one has a live stub. When the
// legacy executors definitively cover the zap_native code path, the
// non-CreateSovereignL1 entries can be dropped.
//
// DO NOT add map lookups here in the hot path — IssueTxFromRPC is on
// the critical path of every incoming RPC tx. Use a type switch.
func isLegacyTxKindNotYetExecutable(_ *txs.Tx) bool {
	// The former CreateSovereignL1Tx / ConvertNetworkToL1Tx legacy struct
	// kinds were folded into CreateNetworkTx, whose executor is
	// implemented. No legacy struct kind is not-yet-executable today; the
	// zap_native wire gate below still covers future stub kinds.
	return false
}

// isZapNativeWireKindNotYetExecutable inspects the signed tx bytes. If
// they parse as a ZAP-native wire buffer (4-byte "ZAP\x00" magic) AND
// the kind discriminator is one of the not-yet-implemented set, the gate
// fires. This is the FUTURE-PROOFING half of the defense — today no
// production path routes ZAP-shaped bytes through the
// vms/platformvm/network admission flow, but the moment that path lands
// (batch 6 zap_native parser integration), this check covers it without
// a second edit.
//
// Gate set per brief: CreateSovereignL1 (kind 23), RegisterL1Validator
// (kind 7), ConvertNetworkToL1 (kind 22). The brief is explicit that
// the executor coverage gap exists for all three at the zap_native wire
// level — even though the legacy struct executors for kinds 7 and 22
// work, the zap_native wire bytes don't necessarily route through them.
//
// Returns the zap_native kind that triggered the gate (for the wrapped
// error message). Returns TxKindReserved (0) when no gate fires —
// callers compare against 0 to short-circuit.
func zapNativeWireKindNotYetExecutable(signedBytes []byte) zap_native.TxKind {
	if !zap_native.IsZAPBytes(signedBytes) {
		return zap_native.TxKindReserved
	}
	// Try to wrap as each gated kind. Each Wrap*Tx confirms the kind
	// discriminator matches — wrong kind returns ErrWrongTxKind and we
	// move to the next. Cheap because parseAndCheckKind does a single
	// header parse + byte compare.
	if _, err := zap_native.WrapCreateSovereignL1Tx(signedBytes); err == nil {
		return zap_native.TxKindCreateSovereignL1
	}
	if _, err := zap_native.WrapRegisterL1ValidatorTx(signedBytes); err == nil {
		return zap_native.TxKindRegisterL1Validator
	}
	if _, err := zap_native.WrapConvertNetworkToL1Tx(signedBytes); err == nil {
		return zap_native.TxKindConvertNetworkToL1
	}
	return zap_native.TxKindReserved
}

// zapNativeKindName returns a human-readable name for a TxKind. Only
// the gated kinds are listed — every other kind returns "unknown".
// Used only in the gate's error message; not on any hot path.
func zapNativeKindName(k zap_native.TxKind) string {
	switch k {
	case zap_native.TxKindCreateSovereignL1:
		return "CreateSovereignL1Tx"
	case zap_native.TxKindRegisterL1Validator:
		return "RegisterL1ValidatorTx"
	case zap_native.TxKindConvertNetworkToL1:
		return "ConvertNetworkToL1Tx"
	default:
		return "unknown"
	}
}

// zapNativeAdmissionGate wraps a TxVerifier and refuses admission for
// tx kinds whose zap_native executor body is not yet implemented. The
// wrapper preserves the underlying verifier's behavior for every other
// kind — single-purpose gate, composable, no side effects on the
// pass-through path.
//
// Construction: NewZapNativeAdmissionGate. The constructor is the only
// public surface; the wrapped verifier is hidden by implementation
// type to keep callers from accidentally bypassing the gate.
type zapNativeAdmissionGate struct {
	inner TxVerifier
}

// VerifyTx fires the R7V5 gate FIRST, then delegates to the inner
// verifier when the tx is admissible. Order matters: the gate must
// fire BEFORE any state-machine execution so the executor never sees
// a not-yet-implemented kind.
func (g *zapNativeAdmissionGate) VerifyTx(tx *txs.Tx) error {
	// Path 1 — legacy struct dispatch (today's hot path).
	if isLegacyTxKindNotYetExecutable(tx) {
		return fmt.Errorf(
			"legacy tx type %T: %w",
			tx.Unsigned, ErrZapNativeNotYetExecutable,
		)
	}
	// Path 2 — zap_native wire bytes (future-proof). Only inspect the
	// signed bytes when they exist; some test paths construct a Tx
	// without calling Initialize.
	if tx != nil {
		if signedBytes := tx.Bytes(); len(signedBytes) >= 4 {
			if kind := zapNativeWireKindNotYetExecutable(signedBytes); kind != zap_native.TxKindReserved {
				return fmt.Errorf(
					"zap_native wire kind %s (%d): %w",
					zapNativeKindName(kind), kind, ErrZapNativeNotYetExecutable,
				)
			}
		}
	}
	return g.inner.VerifyTx(tx)
}

// NewZapNativeAdmissionGate wraps a TxVerifier with the R7V5 gate. The
// wrapped verifier is consulted only for txs the gate admits.
//
// USAGE: pass the gate-wrapped TxVerifier into network.New so every
// inbound tx routes through the gate first. The bare TxVerifier should
// never be installed into a production Network — the gate must be
// the outermost layer at every admission boundary.
func NewZapNativeAdmissionGate(inner TxVerifier) TxVerifier {
	return &zapNativeAdmissionGate{inner: inner}
}
