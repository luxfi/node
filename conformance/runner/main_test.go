// SPDX-License-Identifier: BSD-3-Clause-Eco

// What the runner must do to be worth running.
//
// Every claim of cross-language parity in this repository is this program's
// exit code. So the questions here are the ones a reader would ask of any
// measurement: can it come out wrong, and does it notice the three ways an
// implementation can fail to answer — a wrong answer, a declined one, and
// silence. A green that cannot go red measures nothing.
package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The column after the id is the label the corpus files a vector under — the
// chain, for the chain corpus. The runner reads it so a report can group.
const vectors = `# V	<id>	<label>	<payload>
V	A	T	00
V	B	T	01
`

// The reference's own answers, as `gen emit` records them.
const recorded = `R	A	ok	Base	aa	OK	LEDGER	fine
R	B	ok	Base	bb	OK	LEDGER	fine
`

// evaluatorSaying writes a program that ignores the corpus and prints rows.
func evaluatorSaying(t *testing.T, dir, name, rows string) string {
	t.Helper()
	body := filepath.Join(dir, name+".rows")
	if err := os.WriteFile(body, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, name)
	script := "#!/bin/sh\ncat " + body + "\n"
	if err := os.WriteFile(prog, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return prog
}

type run struct {
	code   int
	output string
}

func differential(t *testing.T, runner, dir string, evals ...string) run {
	t.Helper()
	args := []string{
		"-subject", "test",
		"-fields", "parse,kind,id,syntactic,exec",
		"-vectors", filepath.Join(dir, "vectors.tsv"),
		"-expected", filepath.Join(dir, "expected.tsv"),
	}
	for _, e := range evals {
		args = append(args, "-eval", e)
	}
	cmd := exec.Command(runner, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return run{code, string(out)}
}

func setup(t *testing.T) (runner, dir string) {
	t.Helper()
	dir = t.TempDir()
	runner = filepath.Join(dir, "runner")
	build := exec.Command("go", "build", "-o", runner, ".")
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the runner: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "vectors.tsv"), []byte(vectors), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "expected.tsv"), []byte(recorded), 0o644); err != nil {
		t.Fatal(err)
	}
	return runner, dir
}

func TestTwoImplementationsThatAgreePass(t *testing.T) {
	runner, dir := setup(t)
	a := evaluatorSaying(t, dir, "go", recorded)
	b := evaluatorSaying(t, dir, "cpp", recorded)
	got := differential(t, runner, dir, "go="+a, "cpp="+b)
	if got.code != 0 {
		t.Fatalf("exit %d, want 0\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, "AGREED") {
		t.Fatalf("no AGREED line:\n%s", got.output)
	}
}

func TestOneWrongAnswerFails(t *testing.T) {
	runner, dir := setup(t)
	a := evaluatorSaying(t, dir, "go", recorded)
	b := evaluatorSaying(t, dir, "cpp", strings.Replace(recorded, "\taa\t", "\tff\t", 1))
	got := differential(t, runner, dir, "go="+a, "cpp="+b)
	if got.code != 1 {
		t.Fatalf("a wrong answer exited %d, want 1\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, "DISAGREEMENTS") || !strings.Contains(got.output, "A . id") {
		t.Fatalf("the wrong field is not named:\n%s", got.output)
	}
}

func TestSilenceFails(t *testing.T) {
	runner, dir := setup(t)
	a := evaluatorSaying(t, dir, "go", recorded)
	b := evaluatorSaying(t, dir, "cpp", "")
	got := differential(t, runner, dir, "go="+a, "cpp="+b)
	if got.code != 1 {
		t.Fatalf("silence exited %d, want 1\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, "NOT ANSWERED") {
		t.Fatalf("silence is not reported:\n%s", got.output)
	}
}

// The one that used to pass. Both ports print SKIPPED for every field, leaving
// the reference and the reference's own recording as the only two answers.
// They agree because they are the same bytes.
func TestEveryPortDecliningFails(t *testing.T) {
	runner, dir := setup(t)
	skipped := "R	A	SKIPPED	SKIPPED	SKIPPED	SKIPPED	SKIPPED	no\n" +
		"R	B	SKIPPED	SKIPPED	SKIPPED	SKIPPED	SKIPPED	no\n"
	a := evaluatorSaying(t, dir, "go", recorded)
	b := evaluatorSaying(t, dir, "rust", skipped)
	c := evaluatorSaying(t, dir, "cpp", skipped)
	got := differential(t, runner, dir, "go="+a, "rust="+b, "cpp="+c)
	if got.code != 1 {
		t.Fatalf("two declining ports exited %d, want 1\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, "NOT COMPARED") {
		t.Fatalf("nothing was compared and it did not say so:\n%s", got.output)
	}
}

// The recording is the reference's own output. It may disagree with anyone,
// and it may not stand in for the second implementation a field needs.
func TestRecordingIsNotASecondImplementation(t *testing.T) {
	runner, dir := setup(t)
	skipped := "R	A	SKIPPED	SKIPPED	SKIPPED	SKIPPED	SKIPPED	no\n" +
		"R	B	SKIPPED	SKIPPED	SKIPPED	SKIPPED	SKIPPED	no\n"
	a := evaluatorSaying(t, dir, "go", recorded)
	b := evaluatorSaying(t, dir, "cpp", skipped)
	got := differential(t, runner, dir, "go="+a, "cpp="+b)
	if got.code != 1 {
		t.Fatalf("one implementation plus its own recording exited %d, want 1\n%s", got.code, got.output)
	}
}

// A reference that has changed its mind since the corpus was written is a
// disagreement, not a new normal.
func TestRecordingStillCatchesDrift(t *testing.T) {
	runner, dir := setup(t)
	drifted := strings.Replace(recorded, "\taa\t", "\tff\t", 1)
	a := evaluatorSaying(t, dir, "go", drifted)
	b := evaluatorSaying(t, dir, "cpp", drifted)
	got := differential(t, runner, dir, "go="+a, "cpp="+b)
	if got.code != 1 {
		t.Fatalf("drift from the recorded corpus exited %d, want 1\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, "corpus") {
		t.Fatalf("the recording is not named as the dissenter:\n%s", got.output)
	}
}

// A port that declines a field two others still answer does not fail the run,
// but the run says how much it declined.
func TestDecliningIsCounted(t *testing.T) {
	runner, dir := setup(t)
	partial := "R	A	ok	Base	aa	OK	SKIPPED	no\n" +
		"R	B	ok	Base	bb	OK	LEDGER	fine\n"
	a := evaluatorSaying(t, dir, "go", recorded)
	b := evaluatorSaying(t, dir, "rust", recorded)
	c := evaluatorSaying(t, dir, "cpp", partial)
	got := differential(t, runner, dir, "go="+a, "rust="+b, "cpp="+c)
	if got.code != 0 {
		t.Fatalf("exit %d, want 0\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, "DECLINED") || !strings.Contains(got.output, "cpp    1 of 10 fields") {
		t.Fatalf("the declined field is not counted:\n%s", got.output)
	}
}

// A count is not something anyone can go and fix. The vector, the label it is
// filed under, the field and the reason are.
func TestDecliningIsNamed(t *testing.T) {
	runner, dir := setup(t)
	partial := "R	A	ok	Base	aa	OK	SKIPPED	needs a chain with a tip\n" +
		"R	B	ok	Base	bb	OK	LEDGER	fine\n"
	a := evaluatorSaying(t, dir, "go", recorded)
	b := evaluatorSaying(t, dir, "rust", recorded)
	c := evaluatorSaying(t, dir, "cpp", partial)
	got := differential(t, runner, dir, "go="+a, "rust="+b, "cpp="+c)
	if got.code != 0 {
		t.Fatalf("exit %d, want 0\n%s", got.code, got.output)
	}
	for _, want := range []string{"cpp . T . exec — 1", "needs a chain with a tip", "\n      A\n"} {
		if !strings.Contains(got.output, want) {
			t.Fatalf("the declined field is not named (%q missing):\n%s", want, got.output)
		}
	}
	// B was answered. Naming that would be naming a gap that is not there.
	if strings.Contains(got.output, "\n      B\n") {
		t.Fatalf("an answered vector is named as declined:\n%s", got.output)
	}
}

// Two ports declining the same field for different reasons are two gaps, and
// the report keeps them apart. One heading over both would read as one.
func TestDeclinesAreGroupedByReason(t *testing.T) {
	runner, dir := setup(t)
	rust := "R	A	ok	Base	aa	OK	SKIPPED	no tip\n" +
		"R	B	ok	Base	bb	OK	SKIPPED	no funds\n"
	a := evaluatorSaying(t, dir, "go", recorded)
	b := evaluatorSaying(t, dir, "cpp", recorded)
	c := evaluatorSaying(t, dir, "rust", rust)
	got := differential(t, runner, dir, "go="+a, "cpp="+b, "rust="+c)
	if got.code != 0 {
		t.Fatalf("exit %d, want 0\n%s", got.code, got.output)
	}
	if strings.Count(got.output, "rust . T . exec — 1") != 2 {
		t.Fatalf("two reasons did not make two groups:\n%s", got.output)
	}
}

// A vector line that carries no label is a corpus this runner cannot group, and
// it says so instead of reporting every gap under the empty string.
func TestVectorWithoutALabelIsRejected(t *testing.T) {
	runner, dir := setup(t)
	if err := os.WriteFile(filepath.Join(dir, "vectors.tsv"), []byte("V\tA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := evaluatorSaying(t, dir, "go", recorded)
	b := evaluatorSaying(t, dir, "cpp", recorded)
	got := differential(t, runner, dir, "go="+a, "cpp="+b)
	if got.code != 1 {
		t.Fatalf("an unlabelled vector exited %d, want 1\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, "not a vector line") {
		t.Fatalf("the bad line is not named:\n%s", got.output)
	}
}

// An empty column is a row an evaluator got wrong. Reading it as a polite
// non-answer is how a broken evaluator disappears from the comparison.
func TestEmptyFieldIsRejected(t *testing.T) {
	runner, dir := setup(t)
	a := evaluatorSaying(t, dir, "go", recorded)
	b := evaluatorSaying(t, dir, "cpp", "R	A	ok	Base		OK	LEDGER	fine\n")
	got := differential(t, runner, dir, "go="+a, "cpp="+b)
	if got.code != 1 {
		t.Fatalf("an empty field exited %d, want 1\n%s", got.code, got.output)
	}
	if !strings.Contains(got.output, "left id of A empty") {
		t.Fatalf("the empty field is not named:\n%s", got.output)
	}
}
