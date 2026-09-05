// SPDX-License-Identifier: BSD-3-Clause-Eco

// The cross-language differential runner.
//
// It hands ONE corpus to every evaluator it is given, collects their verdict
// lines, and compares them field by field. A field two implementations answer
// differently is a disagreement: the run fails, and the row names the pair and
// prints what each of them said.
//
// It knows nothing about any chain. It parses two tab-separated formats and
// compares strings — which is the point. A runner that understood the rules
// would be a fourth implementation, and the day it was wrong it would hide the
// disagreement instead of reporting it.
//
// Nothing here reaches luxfi/node: the reference lives behind the Go evaluator,
// in its own module.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// The fields that are compared, in the order they appear on a result line.
// Which fields those are is not knowledge this program has: the caller names
// them with -fields, because the caller is what chose the evaluators, and a
// runner that knew the chain's five field names would have to learn the next
// differential's as well.
var compared []string

// notEvaluated marks a field an implementation declined to answer. It is never
// a pass: it is counted, reported, and excluded from comparison, because
// comparing an answer against a non-answer would let a chain that evaluates
// nothing agree with everyone.
const notEvaluated = "SKIPPED"

const absent = "" // the implementation printed no row for this vector at all

type result struct {
	fields []string
	note   string
}

type evaluator struct {
	name string
	cmd  []string
	rows map[string]result
}

func main() {
	var vectors string
	var evalFlags stringList
	flag.StringVar(&vectors, "vectors", "conformance/corpus/vectors.tsv", "the corpus")
	fields := flag.String("fields", "", "comma-separated names of the compared fields, in result-line order")
	subject := flag.String("subject", "", "what is being compared, for the banner")
	flag.Var(&evalFlags, "eval", "name=command to run (repeatable); the corpus path is appended")
	expected := flag.String("expected", "", "the recorded reference answers, compared as one more implementation")
	verbose := flag.Bool("v", false, "print every row, not only the disagreements")
	flag.Parse()

	for _, f := range strings.Split(*fields, ",") {
		if f = strings.TrimSpace(f); f != "" {
			compared = append(compared, f)
		}
	}
	if len(compared) == 0 {
		fmt.Fprintln(os.Stderr, "-fields names the columns to compare; without it nothing is compared")
		os.Exit(2)
	}

	if len(evalFlags) < 2 {
		fmt.Fprintln(os.Stderr, "a differential needs at least two implementations to differ")
		os.Exit(2)
	}

	ids, err := readVectorIDs(vectors)
	if err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		os.Exit(1)
	}

	var evals []*evaluator
	byName := map[string]*evaluator{}
	for _, spec := range evalFlags {
		name, command, ok := strings.Cut(spec, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "runner: -eval wants name=command, got %q\n", spec)
			os.Exit(2)
		}
		// One implementation may answer through more than one binary — a chain
		// per program is how two of the three are built. Repeating a name
		// merges the rows, and a vector answered twice under one name is an
		// error rather than a silent last-one-wins.
		e := byName[name]
		if e == nil {
			e = &evaluator{name: name, rows: map[string]result{}}
			byName[name] = e
			evals = append(evals, e)
		}
		e.cmd = append(strings.Fields(command), vectors)
		if err := e.run(); err != nil {
			// An evaluator that cannot run is reported and the run fails. A
			// harness that quietly dropped an implementation would report
			// agreement among whoever was left.
			fmt.Fprintf(os.Stderr, "runner: %s did not run: %v\n", name, err)
			os.Exit(1)
		}
	}

	// The corpus's recorded answers join the comparison as one more voice, so
	// that a reference that changed its mind since the corpus was generated is
	// a disagreement rather than a silent new normal.
	if *expected != "" {
		e := &evaluator{name: "corpus", rows: map[string]result{}}
		if err := e.load(*expected); err != nil {
			fmt.Fprintln(os.Stderr, "runner:", err)
			os.Exit(1)
		}
		evals = append(evals, e)
	}

	report(*subject, ids, evals, *verbose)
}

func (e *evaluator) run() error {
	cmd := exec.Command(e.cmd[0], e.cmd[1:]...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.Join(e.cmd, " "), err)
	}
	s := bufio.NewScanner(strings.NewReader(string(out)))
	s.Buffer(make([]byte, 1<<20), 1<<26)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != resultWidth() || f[0] != "R" {
			return fmt.Errorf("not a result line, want %d columns for %s, got %d: %q",
				resultWidth(), strings.Join(compared, "/"), len(f), line)
		}
		if _, dup := e.rows[f[1]]; dup {
			return fmt.Errorf("%s answered %s twice", e.name, f[1])
		}
		e.rows[f[1]] = result{
			fields: append([]string(nil), f[2:2+len(compared)]...),
			note:   f[len(f)-1],
		}
	}
	return s.Err()
}

// load reads recorded answers from a file instead of running a program.
func (e *evaluator) load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 1<<26)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fs := strings.Split(line, "\t")
		if len(fs) != resultWidth() || fs[0] != "R" {
			return fmt.Errorf("%s: not a result line, want %d columns, got %d: %q",
				path, resultWidth(), len(fs), line)
		}
		e.rows[fs[1]] = result{
			fields: append([]string(nil), fs[2:2+len(compared)]...),
			note:   fs[len(fs)-1],
		}
	}
	return s.Err()
}

func readVectorIDs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var ids []string
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 1<<26)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 || f[0] != "V" {
			return nil, fmt.Errorf("not a vector line: %q", line)
		}
		// Everything after the id is the evaluators' business, not this
		// program's: it hands the file over whole and never reads the payload.
		ids = append(ids, f[1])
	}
	return ids, s.Err()
}

type disagreement struct {
	vector string
	field  string
	a, b   string // evaluator names
	av, bv string // their answers
	an, bn string // their notes
}

// A result line is R, the vector id, one value per compared field, and a note.
func resultWidth() int { return len(compared) + 3 }

func report(subject string, ids []string, evals []*evaluator, verbose bool) {
	names := make([]string, len(evals))
	for i, e := range evals {
		names[i] = e.name
	}
	fmt.Printf("%s differential: %d vectors, %d implementations (%s)\n\n",
		subject, len(ids), len(evals), strings.Join(names, ", "))

	var bad []disagreement
	uncovered := map[string][]string{} // vector -> fields nobody could compare
	silent := map[string][]string{}    // implementation -> vectors it never answered
	answered := map[string]int{}

	// An implementation that prints no row for a vector has not passed it. The
	// field comparison cannot see that on its own: three of four answering is
	// still a comparison, and it agrees, and the fourth's silence reads as
	// success. Every implementation is asked for every vector, and one that
	// skips a chain it does not serve is expected to be paired with the
	// evaluator that does, under the same name.
	for _, e := range evals {
		for _, id := range ids {
			if _, ok := e.rows[id]; !ok {
				silent[e.name] = append(silent[e.name], id)
			}
		}
	}

	for _, id := range ids {
		rowDisagrees := false
		for fi, field := range compared {
			// Collect the implementations that actually answered this field.
			type said struct{ name, value, note string }
			var says []said
			for _, e := range evals {
				r, ok := e.rows[id]
				if !ok {
					continue // this implementation printed no row: absent
				}
				v := r.fields[fi]
				if v == notEvaluated || v == absent {
					continue
				}
				says = append(says, said{e.name, v, r.note})
			}
			if len(says) < 2 {
				uncovered[id] = append(uncovered[id], field)
				continue
			}
			for i := 0; i < len(says); i++ {
				for j := i + 1; j < len(says); j++ {
					if says[i].value != says[j].value {
						rowDisagrees = true
						bad = append(bad, disagreement{
							vector: id, field: field,
							a: says[i].name, av: says[i].value, an: says[i].note,
							b: says[j].name, bv: says[j].value, bn: says[j].note,
						})
					}
				}
			}
		}
		if !rowDisagrees {
			answered[id]++
		}
		if verbose {
			printRow(id, evals)
		}
	}

	// What nobody could compare. Printed loudly, never counted as a pass.
	if len(uncovered) > 0 {
		fmt.Println("NOT COMPARED — fewer than two implementations answered:")
		keys := make([]string, 0, len(uncovered))
		for k := range uncovered {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %-34s %s\n", k, strings.Join(uncovered[k], " "))
		}
		fmt.Println()
	}

	if len(silent) > 0 {
		fmt.Println("NOT ANSWERED — an implementation printed no row at all:")
		keys := make([]string, 0, len(silent))
		for k := range silent {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %-6s %d of %d: %s\n", k, len(silent[k]), len(ids), strings.Join(clip(silent[k], 6), " "))
		}
		fmt.Println()
	}

	if len(bad) == 0 && len(silent) == 0 {
		fmt.Printf("AGREED on every compared field of %d vectors.\n", len(ids))
		fmt.Println()
		fmt.Println("Read that with the NOT COMPARED list above: a field no two")
		fmt.Println("implementations answered was not checked by anything here.")
		return
	}

	if len(bad) == 0 {
		fmt.Printf("Every compared field of %d vectors agreed, but the run fails:\n", len(ids))
		fmt.Println("silence is not agreement.")
		os.Exit(1)
	}

	fmt.Printf("DISAGREEMENTS: %d\n\n", len(bad))
	for _, d := range bad {
		fmt.Printf("  %s . %s\n", d.vector, d.field)
		fmt.Printf("      %-5s %-12s %s\n", d.a, d.av, trim(d.an))
		fmt.Printf("      %-5s %-12s %s\n", d.b, d.bv, trim(d.bn))
		fmt.Println()
	}

	// The pairs, so the summary line names who disagrees with whom.
	pairs := map[string]int{}
	for _, d := range bad {
		pairs[d.a+" vs "+d.b]++
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println("by pair:")
	for _, k := range keys {
		fmt.Printf("  %-16s %d\n", k, pairs[k])
	}
	os.Exit(1)
}

func printRow(id string, evals []*evaluator) {
	fmt.Printf("  %-34s", id)
	for _, e := range evals {
		r, ok := e.rows[id]
		if !ok {
			fmt.Printf(" %s=%-12s", e.name, "-absent-")
			continue
		}
		fmt.Printf(" %s=%-12s", e.name, r.fields[len(r.fields)-1])
	}
	fmt.Println()
}

// clip shortens a list for printing, saying how much it left out.
func clip(v []string, n int) []string {
	if len(v) <= n {
		return v
	}
	return append(append([]string{}, v[:n]...), fmt.Sprintf("... and %d more", len(v)-n))
}

func trim(s string) string {
	if len(s) > 96 {
		return s[:96]
	}
	return s
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}
