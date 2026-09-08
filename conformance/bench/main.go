// SPDX-License-Identifier: BSD-3-Clause-Eco

// The cross-language benchmark.
//
// It hands ONE corpus to every evaluator the differential already runs, asks
// each to do its parse-and-verify work over that corpus a fixed number of
// times, and reports how long that work took. The workload is the differential's
// workload: the same vectors, and the same answers, computed the same way.
// Nothing here asks an implementation to do less.
//
// It knows nothing about any chain, and it does not time anything itself. Each
// evaluator runs its own clock around its own work and prints one line to
// stderr:
//
//	B <impl> <vectors> <repeats> <seconds>
//
// which is the only thing this program reads. The alternative — timing the
// child process from out here — would measure five process starts, five corpus
// reads and five output writes as if they were chain work, and the Go binary
// carries a 27 MB linked reference whose start alone would swamp the C++ one's
// entire run.
//
// A SINGLE TIMING IS NOT A MEASUREMENT. Every evaluator is run -runs times and
// the spread of those runs is reported beside the median. The runs are
// interleaved — every evaluator once, then every evaluator again — so that a
// machine that slows down halfway through slows all of them down rather than
// one.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

// An evaluator binary: one program, one chain's worth of the corpus.
type binary struct {
	impl    string    // the implementation it speaks for: go, rust, cpp
	cmd     []string  // how to run it
	name    string    // what it called itself on its B line
	vectors int       // how many vectors it answered
	secs    []float64 // one elapsed time per run
}

func main() {
	var evalFlags stringList
	vectors := flag.String("vectors", "conformance/corpus/vectors.tsv", "the corpus")
	repeats := flag.Int("repeats", 100, "how many times each evaluator walks the whole corpus per run")
	runs := flag.Int("runs", 5, "how many times each evaluator is run, to show the spread")
	flag.Var(&evalFlags, "eval", "name=command to run (repeatable); the corpus path and the repeat count are appended")
	flag.Parse()

	if *repeats < 1 || *runs < 1 {
		fmt.Fprintln(os.Stderr, "bench: -repeats and -runs are counts, and a count is at least 1")
		os.Exit(2)
	}
	if len(evalFlags) == 0 {
		fmt.Fprintln(os.Stderr, "bench: nothing to time")
		os.Exit(2)
	}

	var bins []*binary
	for _, spec := range evalFlags {
		impl, command, ok := strings.Cut(spec, "=")
		if !ok {
			fmt.Fprintf(os.Stderr, "bench: -eval wants name=command, got %q\n", spec)
			os.Exit(2)
		}
		bins = append(bins, &binary{impl: impl, cmd: strings.Fields(command)})
	}

	fmt.Printf("chain differential benchmark — evaluators: %d · repeats of the corpus per run: %d · runs: %d\n\n",
		len(bins), *repeats, *runs)

	for run := 0; run < *runs; run++ {
		for _, b := range bins {
			if err := b.time(*vectors, *repeats); err != nil {
				fmt.Fprintf(os.Stderr, "bench: %s: %v\n", strings.Join(b.cmd, " "), err)
				os.Exit(1)
			}
		}
	}

	report(bins, *repeats, *runs)
}

// time runs one evaluator once and records what its own clock said.
//
// Its verdicts are thrown away here — the differential is what reads them, and
// it is a separate target. They are still computed and still printed, because
// an evaluator asked to print less would be an evaluator doing less.
func (b *binary) time(vectors string, repeats int) error {
	args := append(append([]string(nil), b.cmd[1:]...), vectors, strconv.Itoa(repeats))
	cmd := exec.Command(b.cmd[0], args...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	// The B line, among whatever else the evaluator wrote to stderr — the Go
	// one logs a node starting up before it answers anything.
	var line []string
	s := bufio.NewScanner(&stderr)
	s.Buffer(make([]byte, 1<<20), 1<<26)
	for s.Scan() {
		if f := strings.Split(s.Text(), "\t"); len(f) == 5 && f[0] == "B" {
			line = f
		}
	}
	if line == nil {
		return fmt.Errorf("printed no B line: it does not take a repeat count")
	}

	n, err := strconv.Atoi(line[2])
	if err != nil || n < 1 {
		return fmt.Errorf("B line names %q vectors", line[2])
	}
	// An evaluator that ran a different number of rounds than it was asked for
	// would be timing a different amount of work than the table says.
	if got, err := strconv.Atoi(line[3]); err != nil || got != repeats {
		return fmt.Errorf("asked for %d repeats, B line says %q", repeats, line[3])
	}
	secs, err := strconv.ParseFloat(line[4], 64)
	if err != nil {
		return fmt.Errorf("B line names %q seconds", line[4])
	}
	if b.name != "" && b.vectors != n {
		return fmt.Errorf("answered %d vectors, and %d on an earlier run", n, b.vectors)
	}
	b.name, b.vectors = line[1], n
	b.secs = append(b.secs, secs)
	return nil
}

func report(bins []*binary, repeats, runs int) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "evaluator\tvectors\trepeats\truns\tfastest s\tmedian s\tslowest s\tspread\tµs/vector")

	// Every binary, in the order it was given, and — where an implementation
	// answers through more than one — a row for the implementation, whose time
	// for a run is the sum of its binaries' times for THAT run. Summing per run
	// rather than summing the medians is what keeps the total's own spread
	// honest.
	printed := map[string]bool{}
	for _, b := range bins {
		if printed[b.impl] {
			continue
		}
		printed[b.impl] = true

		var group []*binary
		for _, o := range bins {
			if o.impl == b.impl {
				group = append(group, o)
			}
		}
		for _, o := range group {
			row(w, o.name, o.vectors, repeats, o.secs)
		}
		if len(group) < 2 {
			continue
		}
		total := make([]float64, runs)
		vectors := 0
		for _, o := range group {
			vectors += o.vectors
			for i, s := range o.secs {
				total[i] += s
			}
		}
		row(w, b.impl+" (all)", vectors, repeats, total)
	}
	w.Flush()

	fmt.Println("\nµs/vector is per vector per repeat, from the FASTEST run. Everything else")
	fmt.Println("sharing the machine can only ever add time, never subtract it, so the fastest")
	fmt.Println("run is the least contaminated one. The spread beside it says how contaminated")
	fmt.Println("the others were: a large spread means the machine was busy, not that one")
	fmt.Println("implementation is erratic, and the run is worth repeating on a quiet one.")
}

func row(w io.Writer, name string, vectors, repeats int, secs []float64) {
	lo, mid, hi := spread(secs)
	fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%.3f\t%.3f\t%.3f\t%.0f%%\t%.2f\n",
		name, vectors, repeats, len(secs), lo, mid, hi,
		100*(hi-lo)/lo, 1e6*lo/float64(vectors*repeats))
}

// spread reports the fastest, middle and slowest of a set of runs. The median
// rather than the mean, because what goes wrong on a shared machine is one run
// being interrupted, and a mean carries that into every number beside it while
// a median does not.
func spread(secs []float64) (lo, mid, hi float64) {
	s := append([]float64(nil), secs...)
	sort.Float64s(s)
	mid = s[len(s)/2]
	if len(s)%2 == 0 {
		mid = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return s[0], mid, s[len(s)-1]
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}
