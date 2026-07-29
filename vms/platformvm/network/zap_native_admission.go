// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"fmt"

	"github.com/luxfi/node/vms/platformvm/txs"
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
// The gate names kinds by their concrete txs.UnsignedTx type. It cannot name
// them by the 1-byte wire discriminator: txs.kind and zap_native.TxKind are
// separate namespaces that both live at object byte 0 and assign the same byte
// values to different types, so a byte comparison cannot tell a P-chain
// CreateChainTx (txs.kind 7) from a zap_native RegisterL1ValidatorTx (TxKind 7).
//
// REMOVAL CHECKLIST (when a stub executor body lands):
//
//  1. Implement the executor body in
//     vms/platformvm/txs/executor/standard_tx_executor.go.
//  2. Remove the matching type from isLegacyTxKindNotYetExecutable below.
//  3. Drop the matching test from zap_native_admission_test.go.
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

// isLegacyTxKindNotYetExecutable reports whether tx is a concrete
// txs.UnsignedTx type whose executor body is not implemented yet. It
// discriminates on the Go type, which is the only unambiguous way to name a
// tx kind here: the 1-byte wire discriminator is namespace-relative, and
// txs.kind and zap_native.TxKind number the same byte values differently.
//
// TODAY: none. CreateSovereignL1Tx and ConvertNetworkToL1Tx were folded into
// CreateNetworkTx, whose executor is implemented, so every kind that reaches
// admission is executable. New stub types are listed here by type.
//
// DO NOT add map lookups here in the hot path — IssueTxFromRPC is on
// the critical path of every incoming RPC tx. Use a type switch.
func isLegacyTxKindNotYetExecutable(_ *txs.Tx) bool {
	return false
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
	if isLegacyTxKindNotYetExecutable(tx) {
		return fmt.Errorf(
			"legacy tx type %T: %w",
			tx.Unsigned, ErrZapNativeNotYetExecutable,
		)
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
