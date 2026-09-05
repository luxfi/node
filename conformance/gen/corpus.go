// SPDX-License-Identifier: BSD-3-Clause-Eco

// The corpus format, and the result format the three evaluators speak.
//
// Both are line-oriented and tab-separated, for one reason: a C++ evaluator
// must read the same file the Go and Rust ones read, without a JSON library
// entering the C++ chain's dependency graph. One format, three readers, no
// translation between them.
package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// A Vector is one thing to evaluate: bytes, and what to do with them.
//
//	V <id> <chain> <op> <wire-hex>
//
// chain is P or X. op is one of:
//
//	tx      — parse the bytes as a signed transaction, verify, execute
//	block   — parse the bytes as a block
//	seam    — no bytes; ask the implementation about a decision-seam method
type Vector struct {
	ID    string
	Chain string
	Op    string
	Wire  string
}

// A Result is what one implementation says about one vector.
//
//	R <id> <parse> <kind> <hash> <syntactic> <exec> <note>
//
// The first five fields after the id are COMPARED across implementations. The
// note is not: it carries the implementation's own words for what happened,
// so a disagreement can be read without opening three debuggers.
type Result struct {
	ID        string
	Parse     string // ok | MALFORMED
	Kind      string // the tx or block kind's canonical name, or -
	Hash      string // tx id / block id, lowercase hex, or -
	Syntactic string // verdict class
	Exec      string // verdict class
	Note      string
}

// The verdict vocabulary. Every implementation maps its own error type into
// exactly one of these before printing, because "failed to fetch UTXO",
// `MissingUtxo` and `kUtxoNotFound` are three spellings of one answer and
// comparing the spellings would report a disagreement that is not one.
//
// LEDGER is deliberately coarse: on the empty chain every vector meets, a
// missing UTXO and a missing validator are both just "the chain does not hold
// this", and splitting them would report which lookup each implementation
// happened to reach first as if it were a difference of opinion. What the
// empty chain DOES separate is UNSUPPORTED — a refusal that never looked at
// the chain at all — from everything that tried.
//
// The mapping is per-implementation and deliberately small; the raw words
// survive in the note beside it, so a mapping that flattened a real difference
// is visible to anyone reading the row.
const (
	VOK          = "OK"          // accepted
	VMalformed   = "MALFORMED"   // the bytes are not a transaction
	VSyntactic   = "SYNTACTIC"   // well-formedness: ordering, zero amounts, field limits
	VOverflow    = "OVERFLOW"    // an arithmetic bound was crossed
	VLedger      = "LEDGER"      // the chain does not hold what the tx needs
	VAuth        = "AUTH"        // a credential did not authorise the spend
	VWarp        = "WARP"        // a warp message did not verify
	VUnsupported = "UNSUPPORTED" // the implementation refuses this kind by name
	VSkipped     = "SKIPPED"     // not evaluated at this layer (never a pass)
	VInternal    = "INTERNAL"    // the evaluator itself failed
)

const none = "-"

func (v Vector) String() string {
	return fmt.Sprintf("V\t%s\t%s\t%s\t%s", v.ID, v.Chain, v.Op, v.Wire)
}

func (r Result) String() string {
	note := strings.ReplaceAll(r.Note, "\t", " ")
	note = strings.ReplaceAll(note, "\n", " ")
	if note == "" {
		note = none
	}
	return fmt.Sprintf("R\t%s\t%s\t%s\t%s\t%s\t%s\t%s",
		r.ID, r.Parse, r.Kind, r.Hash, r.Syntactic, r.Exec, note)
}

func readVectors(r io.Reader) ([]Vector, error) {
	var out []Vector
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1<<20), 1<<26)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 || f[0] != "V" {
			return nil, fmt.Errorf("corpus line is not a vector: %q", line)
		}
		out = append(out, Vector{ID: f[1], Chain: f[2], Op: f[3], Wire: f[4]})
	}
	return out, s.Err()
}
