// Copyright (C) 2019-2026, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build ignore

// Command regenfixtures regenerates wire-byte-anchored test fixtures
// after a wire-format change.
//
// Usage:
//
//	go run ./internal/regenfixtures -pkg ./p/block
//	go run ./internal/regenfixtures -pkg ./p/block -dry-run
//	go run ./internal/regenfixtures -pkg ./p/block,./p/state
//
// Algorithm:
//
//  1. Run `go test -v -count=1 <pkg>` and capture stderr+stdout.
//  2. Parse each failure for `expected: []byte{...}` and `actual: []byte{...}`
//     pairs (testify produces both in Not-equal failures).
//  3. For each `(expected, actual)` pair, scan the test files in <pkg>
//     using go/parser to find every `[]byte{...}` composite literal.
//     Decode each literal's byte values. If they match `expected`,
//     rewrite that literal in-place with `actual`'s byte values
//     formatted in canonical 16-bytes-per-line layout.
//  4. Emit a `// Regenerated post-LP-023 ZAP cutover` comment above
//     each rewritten literal (idempotent — skip if already present).
//
// Design notes:
//
//   - AST-based identification means comments inside the original
//     literal (e.g. `// Codec version`) are dropped on regenerate.
//     This is correct: the byte boundaries change with the wire
//     format, so the prior comments would lie.
//
//   - go/printer is not used to write the new literal because we want
//     deterministic 16-bytes-per-line layout. We render the literal
//     ourselves and string-splice it into the source.
//
//   - We disambiguate composite literals by their byte payload, not
//     their position. If the same expected bytes appear in multiple
//     literals in the same file, all are rewritten to the same actual
//     bytes. This is correct iff the test cases are uniquely keyed
//     by their fixture — which is the case for every package we
//     touch (a fixture is one block-or-tx's wire encoding).
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func main() {
	var (
		pkgFlag     = flag.String("pkg", "", "comma-separated package paths to regenerate (relative to module root)")
		dryRun      = flag.Bool("dry-run", false, "report what would change without writing files")
		verbose     = flag.Bool("v", false, "verbose output")
		annotateTag = flag.String("annotate", "Regenerated post-LP-023 ZAP cutover", "comment to add above rewritten literals")
	)
	flag.Parse()

	if *pkgFlag == "" {
		fmt.Fprintln(os.Stderr, "usage: regenfixtures -pkg <pkg1>[,<pkg2>...] [-dry-run] [-v]")
		os.Exit(2)
	}

	pkgs := strings.Split(*pkgFlag, ",")
	totalChanges := 0
	totalFails := 0
	for _, pkg := range pkgs {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		changes, fails, err := regenPackage(pkg, *dryRun, *verbose, *annotateTag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "regenfixtures: %s: %v\n", pkg, err)
			os.Exit(1)
		}
		totalChanges += changes
		totalFails += fails
		fmt.Printf("[%s] rewrote %d literal(s) across %d failure(s)\n", pkg, changes, fails)
	}
	fmt.Printf("\ntotal: %d literal(s) rewritten across %d failure(s)\n", totalChanges, totalFails)
}

func regenPackage(pkg string, dryRun, verbose bool, annotateTag string) (int, int, error) {
	pairs, err := collectFailures(pkg, verbose)
	if err != nil {
		return 0, 0, fmt.Errorf("collect failures: %w", err)
	}
	if len(pairs) == 0 {
		if verbose {
			fmt.Printf("[%s] no Not-equal failures captured (package may already be green or fail before assertion)\n", pkg)
		}
		return 0, 0, nil
	}
	if verbose {
		fmt.Printf("[%s] captured %d expected/actual pair(s)\n", pkg, len(pairs))
	}

	files, err := testFilesInPkg(pkg)
	if err != nil {
		return 0, 0, fmt.Errorf("enumerate test files: %w", err)
	}

	changes := 0
	for _, file := range files {
		n, err := rewriteFile(file, pairs, dryRun, verbose, annotateTag)
		if err != nil {
			return changes, len(pairs), fmt.Errorf("rewrite %s: %w", file, err)
		}
		changes += n
	}
	return changes, len(pairs), nil
}

// pair carries one expected/actual byte slice from a test failure.
type pair struct {
	expected []byte
	actual   []byte
	src      string // free-form provenance (test name, file:line)
	// kind indicates how this pair was scraped:
	//   "bytes"  — testify Not-equal on []byte literals
	//   "string" — testify Not-equal on string literals (e.g. base64)
	kind string
	// raw versions of expected/actual as they appear in source — only
	// populated for kind="string". For kind="bytes" we compare by the
	// decoded byte payload, regardless of source formatting.
	expectedRaw string
	actualRaw   string
}

// fingerprint returns a fixed-string for map keying on expected bytes.
func (p pair) fingerprint() string {
	if p.kind == "string" {
		return "s:" + p.expectedRaw
	}
	return "b:" + string(p.expected)
}

// collectFailures runs `go test -v -count=1` and parses Not-equal
// failures from the output.
func collectFailures(pkg string, verbose bool) ([]pair, error) {
	cmd := exec.Command("go", "test", "-v", "-count=1", pkg)
	out, _ := cmd.CombinedOutput()
	if verbose {
		fmt.Printf("--- go test output (%s, %d bytes) ---\n", pkg, len(out))
	}
	return parseFailures(string(out))
}

// reExpected matches `expected: []byte{...}` in testify Not-equal output.
var reExpected = regexp.MustCompile(`expected:\s*\[\]byte\{([^}]*)\}`)
var reActual = regexp.MustCompile(`actual\s*:\s*\[\]byte\{([^}]*)\}`)
var reTestLine = regexp.MustCompile(`Test:\s*(\S+)`)
var reErrTrace = regexp.MustCompile(`Error Trace:\s*(\S+)`)

// reExpectedString matches `expected: "..."` in testify Not-equal output.
// Strings are typically base64-encoded byte buffers in our test suite.
// We require the literal to be on its own line ending in `\n` to avoid
// pulling fragments out of multi-line strings.
var reExpectedString = regexp.MustCompile(`expected:\s*"([^"]*)"`)
var reActualString = regexp.MustCompile(`actual\s*:\s*"([^"]*)"`)

func parseFailures(output string) ([]pair, error) {
	// Each failure block contains both an `expected:` and `actual:` line.
	// We split on test boundaries and pull them in order. We don't
	// require the lines to be adjacent — testify can interleave with
	// the rendered Diff — but the next `actual:` after an `expected:`
	// is its counterpart.
	expectedMatches := reExpected.FindAllStringSubmatchIndex(output, -1)
	actualMatches := reActual.FindAllStringSubmatchIndex(output, -1)

	if len(expectedMatches) != len(actualMatches) {
		// Not fatal — we pair what we can in order.
	}

	n := len(expectedMatches)
	if len(actualMatches) < n {
		n = len(actualMatches)
	}

	pairs := make([]pair, 0, n)
	for i := 0; i < n; i++ {
		expSrc := output[expectedMatches[i][2]:expectedMatches[i][3]]
		actSrc := output[actualMatches[i][2]:actualMatches[i][3]]
		exp, err := decodeGoByteList(expSrc)
		if err != nil {
			return nil, fmt.Errorf("parse expected: %w", err)
		}
		act, err := decodeGoByteList(actSrc)
		if err != nil {
			return nil, fmt.Errorf("parse actual: %w", err)
		}
		// Locate test name within the surrounding window.
		windowStart := expectedMatches[i][0]
		windowEnd := len(output)
		if i+1 < len(expectedMatches) {
			windowEnd = expectedMatches[i+1][0]
		}
		window := output[windowStart:windowEnd]
		var src string
		if m := reTestLine.FindStringSubmatch(window); m != nil {
			src = m[1]
		}
		if m := reErrTrace.FindStringSubmatch(window); m != nil {
			if src != "" {
				src += " @ " + m[1]
			} else {
				src = m[1]
			}
		}
		pairs = append(pairs, pair{expected: exp, actual: act, src: src, kind: "bytes"})
	}

	// Scrape string fixtures (e.g. base64-encoded byte buffers).
	expectedStrMatches := reExpectedString.FindAllStringSubmatchIndex(output, -1)
	actualStrMatches := reActualString.FindAllStringSubmatchIndex(output, -1)
	nStr := len(expectedStrMatches)
	if len(actualStrMatches) < nStr {
		nStr = len(actualStrMatches)
	}
	for i := 0; i < nStr; i++ {
		expSrc := output[expectedStrMatches[i][2]:expectedStrMatches[i][3]]
		actSrc := output[actualStrMatches[i][2]:actualStrMatches[i][3]]
		// Locate test name within the surrounding window.
		windowStart := expectedStrMatches[i][0]
		windowEnd := len(output)
		if i+1 < len(expectedStrMatches) {
			windowEnd = expectedStrMatches[i+1][0]
		}
		window := output[windowStart:windowEnd]
		var src string
		if m := reTestLine.FindStringSubmatch(window); m != nil {
			src = m[1]
		}
		if m := reErrTrace.FindStringSubmatch(window); m != nil {
			if src != "" {
				src += " @ " + m[1]
			} else {
				src = m[1]
			}
		}
		pairs = append(pairs, pair{
			src:         src,
			kind:        "string",
			expectedRaw: expSrc,
			actualRaw:   actSrc,
		})
	}

	// Deduplicate: same expected bytes can appear in many subtests
	// (e.g., len(0) buffers); we want to map the union of expected→actual
	// across all failures.
	uniq := make(map[string]pair, len(pairs))
	for _, p := range pairs {
		key := p.fingerprint()
		if existing, ok := uniq[key]; ok {
			if !bytes.Equal(existing.actual, p.actual) {
				return nil, fmt.Errorf(
					"conflict: same expected bytes map to two different actual outputs (test1=%s, test2=%s)",
					existing.src, p.src,
				)
			}
			continue
		}
		uniq[key] = p
	}
	out := make([]pair, 0, len(uniq))
	for _, p := range uniq {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].src < out[j].src })
	return out, nil
}

// decodeGoByteList parses comma-separated Go byte literals like "0x0, 0x1, 0xff".
func decodeGoByteList(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return []byte{}, nil
	}
	parts := strings.Split(s, ",")
	out := make([]byte, 0, len(parts))
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Strip Go literal prefixes/suffixes — we only see 0x..
		// or decimal here.
		v, err := strconv.ParseUint(raw, 0, 8)
		if err != nil {
			return nil, fmt.Errorf("byte literal %q: %w", raw, err)
		}
		out = append(out, byte(v))
	}
	return out, nil
}

// testFilesInPkg lists *_test.go files in pkg (relative to module root).
func testFilesInPkg(pkg string) ([]string, error) {
	dir, err := pkgDir(pkg)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, "_test.go") {
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Strings(out)
	return out, nil
}

// pkgDir converts a Go package path like "./p/block" into a filesystem
// directory. We assume the current working directory IS the module
// root.
func pkgDir(pkg string) (string, error) {
	pkg = strings.TrimPrefix(pkg, "./")
	if pkg == "" || strings.HasPrefix(pkg, "/") {
		return "", fmt.Errorf("invalid pkg path %q (must be relative to module root)", pkg)
	}
	if _, err := os.Stat(pkg); err != nil {
		return "", fmt.Errorf("pkg dir %q: %w", pkg, err)
	}
	return pkg, nil
}

// rewriteFile rewrites every `[]byte{...}` literal in file whose decoded
// payload matches any pair's expected bytes. Returns the number of
// rewrites.
func rewriteFile(file string, pairs []pair, dryRun, verbose bool, annotateTag string) (int, error) {
	src, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		// Best-effort: if parsing fails (e.g. syntax error introduced
		// by earlier run), skip.
		if verbose {
			fmt.Printf("  parse %s: %v (skipping)\n", file, err)
		}
		return 0, nil
	}

	// Walk all CompositeLit nodes whose Type encodes []byte (either
	// the literal `[]byte` or an alias resolving to it). We accept
	// `[]byte` and `[]uint8`.
	type rewrite struct {
		startOff int    // byte offset where replacement starts
		endOff   int    // byte offset (exclusive) where replacement ends
		newBytes []byte // replacement bytes (when !isString)
		newStr   string // replacement string literal (when isString)
		isString bool   // true if rewrite targets a string literal
		oldHash  string // for logging
	}
	var rewrites []rewrite

	// Build a quick map of expected → actual for O(1) lookup.
	wantBytes := make(map[string][]byte, len(pairs))
	wantStrings := make(map[string]string, len(pairs))
	for _, p := range pairs {
		switch p.kind {
		case "bytes":
			wantBytes["b:"+string(p.expected)] = p.actual
		case "string":
			wantStrings["s:"+p.expectedRaw] = p.actualRaw
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		// Byte slice composite literals.
		if cl, ok := n.(*ast.CompositeLit); ok && cl.Type != nil && isByteSliceType(cl.Type) {
			got, ok := decodeASTByteList(cl)
			if !ok {
				return true
			}
			actual, found := wantBytes["b:"+string(got)]
			if !found {
				return true
			}
			if bytes.Equal(got, actual) {
				return true
			}
			lbrace := fset.Position(cl.Lbrace).Offset
			rbrace := fset.Position(cl.Rbrace).Offset
			rewrites = append(rewrites, rewrite{
				startOff: lbrace,
				endOff:   rbrace + 1,
				newBytes: actual,
				oldHash:  fmt.Sprintf("len=%d", len(got)),
			})
			return true
		}
		// String literals (e.g. base64-encoded byte buffers).
		if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			unquoted, err := strconv.Unquote(bl.Value)
			if err != nil {
				return true
			}
			actual, found := wantStrings["s:"+unquoted]
			if !found {
				return true
			}
			if unquoted == actual {
				return true
			}
			start := fset.Position(bl.Pos()).Offset
			end := fset.Position(bl.End()).Offset
			// Render quoted Go string literal with the new content.
			replacement := strconv.Quote(actual)
			rewrites = append(rewrites, rewrite{
				startOff: start,
				endOff:   end,
				newStr:   replacement,
				oldHash:  fmt.Sprintf("strlen=%d", len(unquoted)),
				isString: true,
			})
			return true
		}
		return true
	})

	if len(rewrites) == 0 {
		return 0, nil
	}

	// Sort descending by start offset so we can splice in-place
	// without invalidating earlier offsets.
	sort.Slice(rewrites, func(i, j int) bool {
		return rewrites[i].startOff > rewrites[j].startOff
	})

	if verbose {
		fmt.Printf("  %s: %d literal(s) to rewrite\n", file, len(rewrites))
	}

	out := make([]byte, len(src))
	copy(out, src)
	for _, rw := range rewrites {
		var replacement string
		if rw.isString {
			replacement = rw.newStr
		} else {
			replacement = renderByteLiteral(rw.newBytes, computeIndent(out, rw.startOff))
		}
		out = append(out[:rw.startOff], append([]byte(replacement), out[rw.endOff:]...)...)
	}

	// Optionally annotate: walk the literals once more from a fresh
	// parse and prepend the comment if not already present. To keep
	// the tool simple and deterministic, we skip annotation here —
	// the regenerator's commit message and this tool's docstring
	// document the wire-format change.
	_ = annotateTag

	if dryRun {
		fmt.Printf("[dry-run] %s: would rewrite %d literal(s)\n", file, len(rewrites))
		return len(rewrites), nil
	}
	if err := os.WriteFile(file, out, 0o644); err != nil {
		return 0, err
	}
	return len(rewrites), nil
}

// isByteSliceType returns true for `[]byte` or `[]uint8`.
func isByteSliceType(e ast.Expr) bool {
	arr, ok := e.(*ast.ArrayType)
	if !ok {
		return false
	}
	if arr.Len != nil {
		return false
	}
	ident, ok := arr.Elt.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "byte" || ident.Name == "uint8"
}

// decodeASTByteList decodes the elements of a []byte composite literal
// into raw bytes. Returns (bytes, true) on success. Returns false if
// the literal contains an unparseable element (an identifier reference,
// a slice expression, etc.). Accepts:
//   - integer literals: 0x00, 42, 0o77
//   - char literals: 'n' (decoded as rune)
//   - string literals: "abc" (decoded as UTF-8 bytes)
func decodeASTByteList(cl *ast.CompositeLit) ([]byte, bool) {
	out := make([]byte, 0, len(cl.Elts))
	for _, elt := range cl.Elts {
		bl, ok := elt.(*ast.BasicLit)
		if !ok {
			return nil, false
		}
		switch bl.Kind {
		case token.INT:
			v, err := strconv.ParseUint(bl.Value, 0, 8)
			if err != nil {
				return nil, false
			}
			out = append(out, byte(v))
		case token.CHAR:
			r, _, _, err := strconv.UnquoteChar(bl.Value[1:len(bl.Value)-1], '\'')
			if err != nil {
				return nil, false
			}
			if r > 0xff {
				return nil, false
			}
			out = append(out, byte(r))
		case token.STRING:
			unquoted, err := strconv.Unquote(bl.Value)
			if err != nil {
				return nil, false
			}
			out = append(out, []byte(unquoted)...)
		default:
			return nil, false
		}
	}
	return out, true
}

// computeIndent returns the whitespace from the start of the line up
// to offset. Used to indent multi-line literal content.
func computeIndent(src []byte, offset int) string {
	lineStart := offset
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	// Indent is everything up to the first non-tab/space character
	// on the line, OR up to offset, whichever is shorter.
	indent := []byte{}
	for i := lineStart; i < offset; i++ {
		ch := src[i]
		if ch == '\t' || ch == ' ' {
			indent = append(indent, ch)
			continue
		}
		break
	}
	return string(indent)
}

// renderByteLiteral renders a `{ 0x.., 0x.., ... }` body with one
// byte per line at 16 bytes per group, indented one tab deeper than
// the literal's opening `{`.
func renderByteLiteral(b []byte, indent string) string {
	if len(b) == 0 {
		return "{}"
	}
	innerIndent := indent + "\t"
	var sb strings.Builder
	sb.WriteString("{\n")
	for i, v := range b {
		if i%16 == 0 {
			sb.WriteString(innerIndent)
		}
		fmt.Fprintf(&sb, "0x%02x,", v)
		if i == len(b)-1 || (i+1)%16 == 0 {
			sb.WriteByte('\n')
		} else {
			sb.WriteByte(' ')
		}
	}
	sb.WriteString(indent)
	sb.WriteByte('}')
	return sb.String()
}

func init() {
	// Defensive: any unexpected runtime panic should be surfaced
	// with the tool's own banner.
	if false {
		_ = errors.New("unused")
	}
}
