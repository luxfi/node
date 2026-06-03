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

// TestAuditGate_ValidatorsListEmbeddersCallMustVerify mirrors the
// validatorslist-mustverify-gate workflow job. Every tx type that
// EMBEDS a ValidatorsList (returns it from an accessor and exposes it
// through a per-tx Verify() body) MUST call .MustVerify() inside
// that Verify() to enforce the 5 sub-field structural floor
// invariants (cap, weight, BLS-non-zero, expiry-non-zero).
//
// Parallel of TestAuditGate_ChainsListEmbeddersCallMustVerify; LP-023
// batch 5 v3.7.
//
// Local repro: `cd vms/platformvm/txs/zap_native && go test -run
// TestAuditGate_ValidatorsListEmbeddersCallMustVerify -v`
func TestAuditGate_ValidatorsListEmbeddersCallMustVerify(t *testing.T) {
	// Step 1: enumerate tx types that EMBED ValidatorsList. A tx type T
	// is an embedder if it has an accessor like
	//   func (t T) Validators() ValidatorsList
	// defined in a production file.
	out, err := exec.Command("sh", "-c",
		`grep -rEoh 'func \(t [A-Z][A-Za-z0-9_]+\) Validators\(\) ValidatorsList' `+
			`--include='*.go' . `+
			`| grep -vE '(_test\.go)' `+
			`| sed -E 's/^func \(t ([A-Za-z0-9_]+)\) Validators.*/\1/' `+
			`| sort -u || true`,
	).Output()
	if err != nil {
		t.Fatalf("embedder-type grep exec failed: %v", err)
	}
	embedderTypes := strings.Fields(strings.TrimSpace(string(out)))
	if len(embedderTypes) == 0 {
		t.Log("Audit clean: zero ValidatorsList embedder tx types")
		return
	}

	// Step 2: for each embedder T, confirm the Verify body in
	// tx_verify.go calls .MustVerify() on the validators view. The
	// search is body-bounded by brace nesting (the same heuristic as
	// the Owner-bearing audit).
	verifyBytes, err := exec.Command("cat", "tx_verify.go").Output()
	if err != nil {
		t.Fatalf("read tx_verify.go failed: %v", err)
	}
	verifySrc := string(verifyBytes)

	var offenders []string
	for _, T := range embedderTypes {
		methodOpen := "func (t " + T + ") Verify() error {"
		startIdx := strings.Index(verifySrc, methodOpen)
		if startIdx < 0 {
			// No Verify() at all — the per-tx audit covers this
			// elsewhere; nothing to enforce on the MustVerify side.
			continue
		}
		braceDepth := 0
		bodyStart := startIdx + len(methodOpen)
		bodyEnd := -1
		for i := bodyStart; i < len(verifySrc); i++ {
			switch verifySrc[i] {
			case '{':
				braceDepth++
			case '}':
				if braceDepth == 0 {
					bodyEnd = i
					i = len(verifySrc)
				} else {
					braceDepth--
				}
			}
		}
		if bodyEnd < 0 {
			offenders = append(offenders,
				T+" (Verify() body has unmatched braces — parse aborted)")
			continue
		}
		body := verifySrc[bodyStart:bodyEnd]
		// vals.MustVerify() / t.Validators().MustVerify() / etc.
		if !strings.Contains(body, ".MustVerify(") {
			offenders = append(offenders,
				T+" (Verify() body does not call validators.MustVerify())")
		}
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"ValidatorsList embedder tx type(s) missing .MustVerify() call.\n"+
				"Every tx type T with a Validators() accessor AND a Verify()\n"+
				"method MUST call list.MustVerify() inside that Verify()\n"+
				"to enforce the 5 structural floor invariants (cap, weight,\n"+
				"BLS-non-zero, expiry-non-zero).\n"+
				"\n"+
				"Offenders:\n%s",
			strings.Join(offenders, "\n"),
		)
	}
}

// TestAuditGate_OwnerBearingTxCallsSyntacticVerify pins LP-023 R4V7 batch
// 5 v3.7: every tx type with an embedded Owner accessor (any function
// named Owner / RewardsOwner / ValidationRewardsOwner /
// DelegationRewardsOwner returning the (threshold, locktime, address)
// tuple) MUST call SyntacticVerify on the reconstructed OwnerStub inside
// its per-tx Verify() body. The wire layer is permissive by design —
// threshold == 0 or threshold > addrcount slips through parseAndCheckKind;
// the only gate is the executor-side SyntacticVerify hook. Without this
// audit, a future tx type could expose an Owner accessor, ship a
// half-finished Verify() that reads Threshold() directly, and silently
// admit a threshold=0 authorization bypass.
//
// Heuristic: enumerate Owner-bearing tx types via grep on production files
// for `func (t T) (Owner|RewardsOwner|ValidationRewardsOwner|
// DelegationRewardsOwner)() (uint32, uint64, ids.ShortID)`. For each such
// type T:
//
//   - Confirm tx_verify.go has `func (t T) Verify() error` (every
//     Owner-bearing type MUST surface Verify; skip is documented inline
//     with the reason).
//   - Confirm the Verify body invokes SyntacticVerify on the reconstructed
//     OwnerStub. The reconstruction goes through stubFromTuple in
//     tx_verify.go; the call site signature is `stubFromTuple(...)` and
//     then `.SyntacticVerify()` on the result. A heuristic scan for the
//     bare token `SyntacticVerify` inside the Verify function body
//     suffices because the helper exists for one and only one purpose.
//
// Local repro: `cd vms/platformvm/txs/zap_native && go test -run
// TestAuditGate_OwnerBearingTxCallsSyntacticVerify -v`
func TestAuditGate_OwnerBearingTxCallsSyntacticVerify(t *testing.T) {
	// Step 1: enumerate Owner-bearing tx types. Match `func (t T) Owner()`
	// OR any of the named-rewards-owner accessors that return the
	// (uint32, uint64, ids.ShortID) tuple. The grep is whole-package; the
	// receiver-type name is extracted via sed.
	out, err := exec.Command("sh", "-c",
		`grep -rEoh 'func \(t [A-Z][A-Za-z0-9_]+\) (Owner|RewardsOwner|ValidationRewardsOwner|DelegationRewardsOwner)\(\) \(uint32, uint64, ids\.ShortID\)' `+
			`--include='*.go' . `+
			`| grep -vE '(_test\.go)' `+
			`| sed -E 's/^func \(t ([A-Za-z0-9_]+)\).*/\1/' `+
			`| sort -u || true`,
	).Output()
	if err != nil {
		t.Fatalf("owner-embedder grep failed: %v", err)
	}
	// TransferChainOwnershipTx uses OwnerThreshold/OwnerLocktime/OwnerAddress
	// (separate accessors) instead of a single Owner() tuple. Add it
	// explicitly so the audit covers it.
	embedderTypes := strings.Fields(strings.TrimSpace(string(out)))
	tcoSeen := false
	for _, T := range embedderTypes {
		if T == "TransferChainOwnershipTx" {
			tcoSeen = true
			break
		}
	}
	if !tcoSeen {
		// Confirm the type actually exposes OwnerThreshold/Locktime/Address.
		hits, _ := exec.Command("sh", "-c",
			`grep -El 'func \(t TransferChainOwnershipTx\) (OwnerThreshold|OwnerLocktime|OwnerAddress)\(\)' `+
				`--include='*.go' . || true`,
		).Output()
		if strings.TrimSpace(string(hits)) != "" {
			embedderTypes = append(embedderTypes, "TransferChainOwnershipTx")
		}
	}
	if len(embedderTypes) == 0 {
		t.Log("Audit clean: zero Owner-bearing tx types")
		return
	}

	// Step 2: for each embedder T, confirm tx_verify.go has a Verify()
	// method AND its body invokes SyntacticVerify. Because tx_verify.go
	// is the centralized Verify file, a single grep over its content
	// suffices to locate both the method header and the SyntacticVerify
	// call. We extract the bounded function body and search inside it.
	verifyBytes, err := exec.Command("cat", "tx_verify.go").Output()
	if err != nil {
		t.Fatalf("read tx_verify.go failed: %v", err)
	}
	verifySrc := string(verifyBytes)

	var offenders []string
	for _, T := range embedderTypes {
		methodOpen := "func (t " + T + ") Verify() error {"
		startIdx := strings.Index(verifySrc, methodOpen)
		if startIdx < 0 {
			offenders = append(offenders,
				T+" (no Verify() method in tx_verify.go — Owner is consumed without gate)")
			continue
		}
		// Locate the matching close brace by tracking nesting depth from
		// the opening brace of the function. tx_verify.go is hand-written
		// Go without raw-string literals containing '{' or '}', so a
		// nesting counter suffices.
		braceDepth := 0
		bodyStart := startIdx + len(methodOpen)
		bodyEnd := -1
		for i := bodyStart; i < len(verifySrc); i++ {
			switch verifySrc[i] {
			case '{':
				braceDepth++
			case '}':
				if braceDepth == 0 {
					bodyEnd = i
					i = len(verifySrc)
				} else {
					braceDepth--
				}
			}
		}
		if bodyEnd < 0 {
			offenders = append(offenders,
				T+" (Verify() body has unmatched braces — parse aborted)")
			continue
		}
		body := verifySrc[bodyStart:bodyEnd]
		if !strings.Contains(body, "SyntacticVerify") {
			offenders = append(offenders,
				T+" (Verify() body does not call SyntacticVerify)")
		}
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"Owner-bearing tx type(s) missing SyntacticVerify call.\n"+
				"Every tx type T with an Owner accessor MUST call\n"+
				"  stubFromTuple(...).SyntacticVerify()\n"+
				"OR (for multi-address Owner)\n"+
				"  OwnerView(...).SyntacticVerify()\n"+
				"inside its per-tx Verify() body. Skipping this gate\n"+
				"makes a threshold=0 wire-encoded Owner a silent\n"+
				"authorization bypass (R4V7).\n"+
				"\n"+
				"Offenders:\n%s",
			strings.Join(offenders, "\n"),
		)
	}
}
