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
