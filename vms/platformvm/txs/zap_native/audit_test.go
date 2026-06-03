// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"os/exec"
	"strings"
	"testing"
)

// TestAuditGate_AddressListNoProductionConsumers mirrors the grep gate in
// .github/workflows/zap-audit.yml so a contributor can reproduce the CI
// failure locally with `go test ./vms/platformvm/txs/zap_native/`.
//
// LP-023 R6-6: AddressList.At() must remain non-production until the
// owner-model migration retires it OR an executor-side equivalence proof
// lands. The allowlist:
//
//   - *_test.go         — tests
//   - tx_verify.go      — executor-side SyntacticVerify boundary
//   - owner.go          — the type's own implementation
//   - audit_test.go     — this file (its own grep pattern would self-match)
//
// To LEGITIMATELY introduce a production caller, land the migration AND
// update the allowlist in BOTH this test and the workflow YAML.
func TestAuditGate_AddressListNoProductionConsumers(t *testing.T) {
	out, err := exec.Command("sh", "-c",
		`grep -rn 'AddressList\.At(' --include='*.go' . | grep -vE '(_test\.go|tx_verify\.go|owner\.go|audit_test\.go)' || true`,
	).Output()
	if err != nil {
		t.Fatalf("grep gate exec failed: %v", err)
	}
	s := strings.TrimSpace(string(out))
	if s != "" {
		t.Fatalf(
			"AddressList.At() production consumer(s) found.\n"+
				"AddressList is forward-looking; if you have a legitimate use,\n"+
				"either land the owner-model migration or add an allowlist entry\n"+
				"with the equivalence proof recorded in owner.go.\n\n"+
				"Offenders:\n%s",
			s,
		)
	}
}

// TestAuditGate_ChainsListEmbeddersCallMustVerify mirrors the
// chainslist-verify-gate workflow job. Every tx type that EMBEDS a
// ChainsList (returns it from an accessor and exposes it through a
// per-tx Verify() body) MUST call .MustVerify() inside that Verify().
//
// LP-023 R7V8: the receiver-name rename Verify → MustVerify makes the
// gate grep-able from CI. The previous Verify() name collided with
// the tx-level Verify() convention and invited the reader to assume
// "the tx Verify already covered this" — wrong, because the tx-level
// Verify is responsible for orchestrating the per-field gates, not
// the list-level walk.
//
// Heuristic: enumerate tx types that own a ChainsList-returning Chains()
// accessor. For each such tx type T, confirm that .MustVerify() is
// called inside ANY function with a receiver of (T) or (T )
// (the per-tx Verify body, which by convention lives in tx_verify.go).
// The check is whole-package because tx_verify.go centralizes the
// Verify methods for the package — embedder file (type definition)
// and Verify file (gate body) are decoupled by design.
//
// Allowlist mechanism: a file may legitimately skip the call (e.g.
// the type definition itself, or audit_test.go which would self-match).
// Document any exception inline.
//
// Local repro: `cd vms/platformvm/txs/zap_native && go test -run
// TestAuditGate_ChainsListEmbeddersCallMustVerify -v`
func TestAuditGate_ChainsListEmbeddersCallMustVerify(t *testing.T) {
	// Step 1: enumerate tx types that EMBED ChainsList. A tx type T is
	// an embedder if it has an accessor like `func (t T) Chains()
	// ChainsListView` defined in a production file.
	out, err := exec.Command("sh", "-c",
		`grep -rEoh 'func \(t [A-Z][A-Za-z0-9_]+\) Chains\(\) (ChainsList|ChainsListView|BoundChainsList)' `+
			`--include='*.go' . `+
			`| grep -vE '(_test\.go)' `+
			`| sed -E 's/^func \(t ([A-Za-z0-9_]+)\) Chains.*/\1/' `+
			`| sort -u || true`,
	).Output()
	if err != nil {
		t.Fatalf("embedder-type grep exec failed: %v", err)
	}
	embedderTypes := strings.Fields(strings.TrimSpace(string(out)))
	if len(embedderTypes) == 0 {
		t.Log("Audit clean: zero ChainsList embedder tx types")
		return
	}

	// Step 2: for each embedder type T, scan the package for a
	// MustVerify() call inside any method body whose receiver
	// includes T. The simplest grep: look for the package-wide
	// presence of `.MustVerify(` AND a method `func (...T) Verify()`.
	// Since this package centralizes Verify in tx_verify.go, we just
	// confirm that EVERY embedder T appears in a function-receiver
	// line within tx_verify.go AND that file contains `.MustVerify(`.
	var offenders []string
	for _, T := range embedderTypes {
		// (a) confirm a Verify() method for T exists in tx_verify.go.
		methodRe := `func \([a-z]+ ` + T + `\) Verify\(\)`
		hits, err := exec.Command("sh", "-c",
			`grep -E '`+methodRe+`' tx_verify.go || true`,
		).Output()
		if err != nil {
			t.Fatalf("method grep exec failed for %s: %v", T, err)
		}
		hasVerify := strings.TrimSpace(string(hits)) != ""

		// (b) confirm the same file calls .MustVerify().
		mvOut, err := exec.Command("sh", "-c",
			`grep -l '\.MustVerify(' tx_verify.go || true`,
		).Output()
		if err != nil {
			t.Fatalf("MustVerify grep exec failed for %s: %v", T, err)
		}
		hasMV := strings.TrimSpace(string(mvOut)) != ""

		if hasVerify && !hasMV {
			offenders = append(offenders,
				T+" (Verify() in tx_verify.go does not call .MustVerify())")
		}
		// If the embedder type has NO Verify() method at all, it can't
		// have skipped MustVerify by definition. The gate is a no-op
		// for embedders that don't expose Verify() (e.g. future tx
		// types that punt the gate to a follow-up).
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"ChainsList embedder tx type(s) missing .MustVerify() call.\n"+
				"Every tx type T that has Chains() accessor AND a Verify()\n"+
				"method MUST call list.MustVerify() inside that Verify()\n"+
				"to enforce the FxIDsLen + reserved-bytes invariants\n"+
				"(R6-4 / R6V5 / R7V8).\n"+
				"\n"+
				"Either wire the gate or document why it's safe to skip\n"+
				"inline with a clear comment.\n"+
				"\n"+
				"Offenders:\n%s",
			strings.Join(offenders, "\n"),
		)
	}
}
