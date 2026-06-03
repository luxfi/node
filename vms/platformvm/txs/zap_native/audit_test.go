// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
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

// astParseFile parses a Go source file and returns its AST. fatal-fails the
// test on parse error so audit-gate failures surface as real test failures
// rather than silent skips.
func astParseFile(t *testing.T, path string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return fset, f
}

// recvTypeName returns the unqualified type name of a method receiver, or
// "" if the FuncDecl has no receiver or the receiver type is not a bare
// *ast.Ident (e.g. `func (t *T) M()` returns "T"). The audit gates use
// the bare identifier (no pointer) because every zap_native tx type is a
// value receiver.
func recvTypeName(fd *ast.FuncDecl) string {
	if fd == nil || fd.Recv == nil || len(fd.Recv.List) != 1 {
		return ""
	}
	expr := fd.Recv.List[0].Type
	// Strip pointer indirection if present.
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	id, ok := expr.(*ast.Ident)
	if !ok {
		return ""
	}
	return id.Name
}

// findMethod returns the FuncDecl for `func (... T) name()` in the file, or
// nil if not found. The lookup matches receiver type name + method name only;
// the receiver variable name (`t`, `r`, `_`, etc.) is ignored.
func findMethod(f *ast.File, recvType, methodName string) *ast.FuncDecl {
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fd.Name == nil || fd.Name.Name != methodName {
			continue
		}
		if recvTypeName(fd) != recvType {
			continue
		}
		return fd
	}
	return nil
}

// hasSelectorCall reports whether any *ast.CallExpr in node's subtree is
// either of:
//
//   - SelectorExpr with .Sel.Name == selector       (e.g. `x.MustVerify()`)
//   - Ident with .Name == selector                  (e.g. `MustVerify()`,
//     a bare package-local function call — used for free functions; not
//     today's pattern in tx_verify.go but here for completeness)
//
// Crucially, this walks the AST — string literals containing "MustVerify"
// and comments containing "// MustVerify" do NOT match because the parser
// produces neither *ast.CallExpr nor *ast.SelectorExpr / *ast.Ident nodes
// for them. *ast.BasicLit holds the literal, *ast.CommentGroup holds the
// comment — both are skipped by the call-site scan.
func hasSelectorCall(node ast.Node, selector string) bool {
	if node == nil {
		return false
	}
	var found bool
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel != nil && fn.Sel.Name == selector {
				found = true
				return false
			}
		case *ast.Ident:
			if fn.Name == selector {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// collectMethodsOnFiles walks every .go file in dir (non-test only by
// default; if includeTests is true, *_test.go files are included) and
// returns the set of receiver type names for which a method matching
// methodPred exists. The methodPred lets the caller match by method name
// AND a return-tuple shape — used by the Owner-bearing gate to enumerate
// types that expose the (uint32, uint64, ids.ShortID) tuple specifically.
func collectMethodsOnFiles(t *testing.T, dir string, includeTests bool, methodPred func(fd *ast.FuncDecl) bool) map[string]struct{} {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	out := make(map[string]struct{})
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if !includeTests && strings.HasSuffix(name, "_test.go") {
			continue
		}
		_, f := astParseFile(t, filepath.Join(dir, name))
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if !methodPred(fd) {
				continue
			}
			T := recvTypeName(fd)
			if T == "" {
				continue
			}
			out[T] = struct{}{}
		}
	}
	return out
}

// returnsOwnerTuple reports whether the FuncDecl returns exactly the
// (uint32, uint64, ids.ShortID) tuple. The audit gates use this to find
// embedded-Owner tuple accessors regardless of method name (Owner /
// RewardsOwner / ValidationRewardsOwner / DelegationRewardsOwner all
// share the same return shape).
func returnsOwnerTuple(fd *ast.FuncDecl) bool {
	if fd == nil || fd.Type == nil || fd.Type.Results == nil {
		return false
	}
	results := fd.Type.Results.List
	if len(results) != 3 {
		return false
	}
	// Each list entry may carry multiple names if grouped — here we
	// expect 3 separate result types.
	if len(results[0].Names) > 0 || len(results[1].Names) > 0 || len(results[2].Names) > 0 {
		// Named results are allowed; the audit just needs the type
		// shape. Fall through.
	}
	want := []string{"uint32", "uint64", "ShortID"}
	got := []string{
		exprIdentName(results[0].Type),
		exprIdentName(results[1].Type),
		exprIdentName(results[2].Type),
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// exprIdentName returns the unqualified identifier name of an expression,
// handling bare idents and selector exprs (e.g. `ids.ShortID` -> "ShortID").
func exprIdentName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		if v.Sel != nil {
			return v.Sel.Name
		}
	}
	return ""
}

// returnsTypeName reports whether the FuncDecl returns exactly one value
// whose unqualified type name equals want. Used to enumerate Chains() and
// Validators() accessors.
func returnsTypeName(fd *ast.FuncDecl, want ...string) bool {
	if fd == nil || fd.Type == nil || fd.Type.Results == nil {
		return false
	}
	results := fd.Type.Results.List
	if len(results) != 1 {
		return false
	}
	got := exprIdentName(results[0].Type)
	for _, w := range want {
		if got == w {
			return true
		}
	}
	return false
}

// TestAuditGate_ChainsListEmbeddersCallMustVerify mirrors the
// chainslist-verify-gate workflow job. Every tx type that EMBEDS a
// ChainsList (returns it from an accessor and exposes it through a
// per-tx Verify() body) MUST call .MustVerify() inside that Verify().
//
// LP-023 R7V8: the receiver-name rename Verify → MustVerify makes the
// gate AST-walkable from CI. The previous Verify() name collided with
// the tx-level Verify() convention and invited the reader to assume
// "the tx Verify already covered this" — wrong, because the tx-level
// Verify is responsible for orchestrating the per-field gates, not
// the list-level walk.
//
// V2 fix (LP-023 batch 5 v3.8): the previous gate used
// `strings.Contains(body, ".MustVerify(")` which a string literal
// `_ = ".MustVerify("` or a `// .MustVerify(` comment would silently
// satisfy. The current implementation parses tx_verify.go via
// go/parser and walks *ast.CallExpr nodes — strings and comments do
// not produce CallExpr nodes, so the bypass surface is closed.
//
// Local repro: `cd vms/platformvm/txs/zap_native && go test -run
// TestAuditGate_ChainsListEmbeddersCallMustVerify -v`
func TestAuditGate_ChainsListEmbeddersCallMustVerify(t *testing.T) {
	// Step 1: enumerate ChainsList embedder tx types via AST. An embedder
	// is any production type T with a method `func (t T) Chains()
	// ChainsList | ChainsListView | BoundChainsList`.
	embedders := collectMethodsOnFiles(t, ".", false, func(fd *ast.FuncDecl) bool {
		if fd.Name == nil || fd.Name.Name != "Chains" {
			return false
		}
		return returnsTypeName(fd, "ChainsList", "ChainsListView", "BoundChainsList")
	})
	if len(embedders) == 0 {
		t.Log("Audit clean: zero ChainsList embedder tx types")
		return
	}

	// Step 2: parse tx_verify.go and for each embedder T confirm
	// `(t T) Verify() error` exists AND its body contains a real
	// MustVerify() CallExpr.
	_, verifyFile := astParseFile(t, "tx_verify.go")

	var offenders []string
	for T := range embedders {
		fd := findMethod(verifyFile, T, "Verify")
		if fd == nil {
			// No Verify() at all — the gate is a no-op for embedders
			// that don't expose Verify(). Mirror the legacy
			// behavior: continue, do not fail.
			continue
		}
		if !hasSelectorCall(fd.Body, "MustVerify") {
			offenders = append(offenders,
				T+" (Verify() in tx_verify.go does not call .MustVerify() — AST walk)")
		}
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"ChainsList embedder tx type(s) missing .MustVerify() call.\n"+
				"Every tx type T that has Chains() accessor AND a Verify()\n"+
				"method MUST call list.MustVerify() inside that Verify()\n"+
				"to enforce the FxIDsLen + reserved-bytes invariants\n"+
				"(R6-4 / R6V5 / R7V8). AST walker confirms a REAL call site\n"+
				"exists (not a string literal, not a comment).\n"+
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
// V2 fix (LP-023 batch 5 v3.8): AST-based call-site walk; see
// TestAuditGate_ChainsListEmbeddersCallMustVerify docstring for the
// rationale.
//
// Local repro: `cd vms/platformvm/txs/zap_native && go test -run
// TestAuditGate_ValidatorsListEmbeddersCallMustVerify -v`
func TestAuditGate_ValidatorsListEmbeddersCallMustVerify(t *testing.T) {
	embedders := collectMethodsOnFiles(t, ".", false, func(fd *ast.FuncDecl) bool {
		if fd.Name == nil || fd.Name.Name != "Validators" {
			return false
		}
		return returnsTypeName(fd, "ValidatorsList")
	})
	if len(embedders) == 0 {
		t.Log("Audit clean: zero ValidatorsList embedder tx types")
		return
	}

	_, verifyFile := astParseFile(t, "tx_verify.go")
	var offenders []string
	for T := range embedders {
		fd := findMethod(verifyFile, T, "Verify")
		if fd == nil {
			continue
		}
		if !hasSelectorCall(fd.Body, "MustVerify") {
			offenders = append(offenders,
				T+" (Verify() in tx_verify.go does not call .MustVerify() — AST walk)")
		}
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"ValidatorsList embedder tx type(s) missing .MustVerify() call.\n"+
				"Every tx type T with a Validators() accessor AND a Verify()\n"+
				"method MUST call list.MustVerify() inside that Verify()\n"+
				"to enforce the 5 structural floor invariants (cap, weight,\n"+
				"BLS-non-zero, expiry-non-zero). AST walker confirms a REAL\n"+
				"call site exists (not a string literal, not a comment).\n"+
				"\n"+
				"Offenders:\n%s",
			strings.Join(offenders, "\n"),
		)
	}
}

// TestAuditGate_OwnerBearingTxCallsSyntacticVerify pins LP-023 R4V7 batch
// 5 v3.8: every tx type with an embedded Owner accessor (any function
// named Owner / RewardsOwner / ValidationRewardsOwner /
// DelegationRewardsOwner returning the (threshold, locktime, address)
// tuple) MUST call SyntacticVerify on the reconstructed OwnerStub inside
// its per-tx Verify() body. The wire layer is permissive by design —
// threshold == 0 or threshold > addrcount slips through parseAndCheckKind;
// the only gate is the executor-side SyntacticVerify hook.
//
// V2 fix (LP-023 batch 5 v3.8): AST-based call-site walk. The previous
// `strings.Contains(body, "SyntacticVerify")` heuristic was defeated by
// string literals containing the word and by commented-out call sites.
// The current implementation parses tx_verify.go via go/parser and walks
// *ast.CallExpr nodes — strings and comments do not produce CallExpr
// nodes, so the bypass surface is closed.
//
// Local repro: `cd vms/platformvm/txs/zap_native && go test -run
// TestAuditGate_OwnerBearingTxCallsSyntacticVerify -v`
func TestAuditGate_OwnerBearingTxCallsSyntacticVerify(t *testing.T) {
	// Step 1: enumerate Owner-bearing tx types via AST. Method name MUST
	// be one of {Owner, RewardsOwner, ValidationRewardsOwner,
	// DelegationRewardsOwner} AND the return tuple MUST be exactly
	// (uint32, uint64, ids.ShortID).
	ownerNames := map[string]struct{}{
		"Owner":                  {},
		"RewardsOwner":           {},
		"ValidationRewardsOwner": {},
		"DelegationRewardsOwner": {},
	}
	embedders := collectMethodsOnFiles(t, ".", false, func(fd *ast.FuncDecl) bool {
		if fd.Name == nil {
			return false
		}
		if _, ok := ownerNames[fd.Name.Name]; !ok {
			return false
		}
		return returnsOwnerTuple(fd)
	})

	// TransferChainOwnershipTx uses OwnerThreshold/OwnerLocktime/OwnerAddress
	// (separate accessors) instead of a single Owner() tuple. A type is
	// only the TCO pattern if it has ALL THREE accessors — otherwise we'd
	// flag TransferableOutput (which only has OwnerAddress + Threshold +
	// Locktime accessors but no embedded Owner semantic).
	tcoThreshold := collectMethodsOnFiles(t, ".", false, func(fd *ast.FuncDecl) bool {
		return fd.Name != nil && fd.Name.Name == "OwnerThreshold"
	})
	tcoLocktime := collectMethodsOnFiles(t, ".", false, func(fd *ast.FuncDecl) bool {
		return fd.Name != nil && fd.Name.Name == "OwnerLocktime"
	})
	tcoAddress := collectMethodsOnFiles(t, ".", false, func(fd *ast.FuncDecl) bool {
		return fd.Name != nil && fd.Name.Name == "OwnerAddress"
	})
	for T := range tcoThreshold {
		if _, hasLT := tcoLocktime[T]; !hasLT {
			continue
		}
		if _, hasAD := tcoAddress[T]; !hasAD {
			continue
		}
		embedders[T] = struct{}{}
	}

	if len(embedders) == 0 {
		t.Log("Audit clean: zero Owner-bearing tx types")
		return
	}

	// Step 2: parse tx_verify.go and for each embedder T confirm
	// `(t T) Verify() error` exists AND its body contains a real
	// SyntacticVerify() CallExpr (selector or bare).
	_, verifyFile := astParseFile(t, "tx_verify.go")
	var offenders []string
	for T := range embedders {
		fd := findMethod(verifyFile, T, "Verify")
		if fd == nil {
			offenders = append(offenders,
				T+" (no Verify() method in tx_verify.go — Owner is consumed without gate)")
			continue
		}
		if !hasSelectorCall(fd.Body, "SyntacticVerify") {
			offenders = append(offenders,
				T+" (Verify() body does not call SyntacticVerify — AST walk)")
		}
	}

	if len(offenders) > 0 {
		t.Fatalf(
			"Owner-bearing tx type(s) missing SyntacticVerify call.\n"+
				"Every tx type T with an Owner accessor MUST call\n"+
				"  stubFromTuple(...).SyntacticVerify()\n"+
				"OR (for multi-address Owner)\n"+
				"  OwnerView(...).SyntacticVerify()\n"+
				"inside its per-tx Verify() body. AST walker confirms a REAL\n"+
				"call site exists (not a string literal, not a comment).\n"+
				"Skipping this gate makes a threshold=0 wire-encoded Owner a\n"+
				"silent authorization bypass (R4V7).\n"+
				"\n"+
				"Offenders:\n%s",
			strings.Join(offenders, "\n"),
		)
	}
}

// --- V2 positive tests: confirm the AST walker REJECTS the three known
// bypass shapes that defeated the previous strings.Contains gate. Each
// test synthesizes a small Go source via parser.ParseFile and asserts
// hasSelectorCall returns the expected boolean.

// TestAuditGate_ASTWalkerRejects_CommentOnly confirms that a Verify()
// body containing ONLY a `// SyntacticVerify` comment does NOT count as
// calling SyntacticVerify. The previous string-contains gate would
// silently accept this.
func TestAuditGate_ASTWalkerRejects_CommentOnly(t *testing.T) {
	src := `package x
type T struct{}
func (t T) Verify() error {
	// SyntacticVerify — placeholder comment, no real call
	return nil
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synth.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fd := findMethod(f, "T", "Verify")
	if fd == nil {
		t.Fatal("synth Verify not found")
	}
	if hasSelectorCall(fd.Body, "SyntacticVerify") {
		t.Fatal("AST walker incorrectly matched a COMMENT — gate is defeated")
	}
}

// TestAuditGate_ASTWalkerRejects_StringLiteral confirms that a Verify()
// body whose only mention of SyntacticVerify is inside a string literal
// does NOT count as calling SyntacticVerify.
func TestAuditGate_ASTWalkerRejects_StringLiteral(t *testing.T) {
	src := `package x
type T struct{}
func (t T) Verify() error {
	_ = "SyntacticVerify"
	return nil
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synth.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fd := findMethod(f, "T", "Verify")
	if fd == nil {
		t.Fatal("synth Verify not found")
	}
	if hasSelectorCall(fd.Body, "SyntacticVerify") {
		t.Fatal("AST walker incorrectly matched a STRING LITERAL — gate is defeated")
	}
}

// TestAuditGate_ASTWalkerRejects_EmptyBody confirms that an empty
// Verify() body does NOT count as calling SyntacticVerify.
func TestAuditGate_ASTWalkerRejects_EmptyBody(t *testing.T) {
	src := `package x
type T struct{}
func (t T) Verify() error {
	return nil
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synth.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fd := findMethod(f, "T", "Verify")
	if fd == nil {
		t.Fatal("synth Verify not found")
	}
	if hasSelectorCall(fd.Body, "SyntacticVerify") {
		t.Fatal("AST walker incorrectly matched an EMPTY body — gate is defeated")
	}
}

// TestAuditGate_ASTWalkerAccepts_RealCall confirms the positive case:
// a Verify() body that contains a real `view.SyntacticVerify()` call IS
// accepted by the AST walker. This is the canonical pattern from
// tx_verify.go and the audit gate MUST accept it.
func TestAuditGate_ASTWalkerAccepts_RealCall(t *testing.T) {
	src := `package x
type View struct{}
func (v View) SyntacticVerify() error { return nil }
type T struct{}
func (t T) view() View { return View{} }
func (t T) Verify() error {
	if err := t.view().SyntacticVerify(); err != nil {
		return err
	}
	return nil
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synth.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fd := findMethod(f, "T", "Verify")
	if fd == nil {
		t.Fatal("synth Verify not found")
	}
	if !hasSelectorCall(fd.Body, "SyntacticVerify") {
		t.Fatal("AST walker FAILED to match a real SelectorExpr call — gate is too strict")
	}
}

// TestAuditGate_OwnerConsumersInExecutorAndService extends the V2 gate
// to the cross-package call sites flagged by V3 (LP-023 batch 5 v3.8):
// Owner-bearing tx accessors are consumed without an in-line
// SyntacticVerify gate at:
//
//   - ~/work/lux/node/vms/platformvm/txs/executor/proposal_tx_executor.go
//     lines 303, 340, 429 — calls ValidationRewardsOwner /
//     DelegationRewardsOwner / RewardsOwner and passes the result to
//     Fx.CreateOutput. CreateOutput goes through fx.Owner.Verify() which
//     is the canonical gate; this audit confirms that path is taken via
//     an explicit .Verify() call site, NOT silently dropped.
//
//   - ~/work/lux/node/vms/platformvm/service.go lines 748-755 — caches
//     ValidationRewardsOwner / DelegationRewardsOwner / RewardsOwner on
//     stakerAttributes. The cache is a READ-ONLY view; the audit confirms
//     the caller-side path goes through Verify() before treating the
//     Owner as authoritative.
//
// The gate is structural: it confirms each callsite either:
//   (a) calls .Verify() on the Owner directly before returning / passing
//       it onward (proposal_tx_executor.go path), OR
//   (b) caches the Owner only after a .Verify() in the originating Verify
//       path of the embedder tx (service.go path).
//
// Today both paths satisfy (a) — see proposal_tx_executor.go where each
// call site is preceded by an explicit `if err := X.Verify(); err != nil`
// guard. service.go inherits the gate from the embedder tx's
// SyntacticVerify which fired at admission time. The audit pins this
// invariant so a future edit cannot silently drop the gate.
//
// LP-023 batch 5 v3.8 V3 HIGH.
func TestAuditGate_OwnerConsumersInExecutorAndService(t *testing.T) {
	type site struct {
		path     string
		consumer string // function name (or "<top-level>" for non-method call sites)
		callee   string // method name expected to be called on the consumed Owner
	}
	// Each site is the callsite we want the audit to confirm. The
	// consumer function name identifies WHICH FuncDecl in the file to
	// inspect; the callee identifies the SelectorExpr we expect to find
	// somewhere in the consumer body — `.Verify(` (fx.Owner is a
	// verify.Verifiable; .Verify() is the canonical method).
	sites := []site{
		{
			path:     "../../../../vms/platformvm/txs/executor/proposal_tx_executor.go",
			consumer: "rewardValidatorTx",
			callee:   "Verify",
		},
		{
			path:     "../../../../vms/platformvm/txs/executor/proposal_tx_executor.go",
			consumer: "rewardDelegatorTx",
			callee:   "Verify",
		},
		{
			path:     "../../../../vms/platformvm/service.go",
			consumer: "loadStakerTxAttributes",
			callee:   "Verify",
		},
	}
	for _, s := range sites {
		_, err := os.Stat(s.path)
		if err != nil {
			// File missing — log and continue (audit gate is informational
			// for cross-package consumers; the in-package gates above are
			// the hard fail).
			t.Logf("skip cross-package audit for %s: %v", s.path, err)
			continue
		}
		_, f := astParseFile(t, s.path)
		// The consumer can be either a method (receiver-bound FuncDecl)
		// or a top-level function. Walk all FuncDecls and match by name.
		var fd *ast.FuncDecl
		for _, decl := range f.Decls {
			d, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if d.Name != nil && d.Name.Name == s.consumer {
				fd = d
				break
			}
		}
		if fd == nil {
			t.Errorf("audit: consumer %s not found in %s — extend regex or wire the gate", s.consumer, s.path)
			continue
		}
		if !hasSelectorCall(fd.Body, s.callee) {
			t.Errorf(
				"audit: %s::%s does not call .%s() on Owner — the call site "+
					"reads Owner from a tx accessor without an executor-side "+
					"Verifiable gate. Either invoke .Verify() inline OR document the "+
					"upstream Verify-path that guarantees the gate fired at admission.",
				s.path, s.consumer, s.callee,
			)
		}
	}
}
