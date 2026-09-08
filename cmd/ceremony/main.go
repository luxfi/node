package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		cmdInit(os.Args[2:])
	case "contribute":
		cmdContribute(os.Args[2:])
	case "verify":
		cmdVerify(os.Args[2:])
	case "export":
		cmdExport(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: ceremony <command> [flags]

Commands:
  init        Initialize a new ceremony
  contribute  Add a contribution to an existing ceremony
  verify      Verify ceremony consistency
  export      Export binary SRS from completed ceremony

Run 'ceremony <command> -h' for command-specific flags.`)
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	circuit := fs.String("circuit", "", "Circuit name (required)")
	participants := fs.Int("participants", 3, "Expected number of participants")
	power := fs.Int("power", 20, "Power of 2 for constraint count (2^power)")
	output := fs.String("output", "", "Output file path (required)")
	fs.Parse(args)

	if *circuit == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "error: --circuit and --output are required")
		fs.Usage()
		os.Exit(1)
	}

	if *power < 1 || *power > 28 {
		fmt.Fprintln(os.Stderr, "error: --power must be in [1, 28]")
		os.Exit(1)
	}

	if *participants < 1 {
		fmt.Fprintln(os.Stderr, "error: --participants must be >= 1")
		os.Exit(1)
	}

	numConstraints := 1 << *power
	fmt.Fprintf(os.Stderr, "Initializing ceremony: circuit=%s constraints=%d participants=%d\n",
		*circuit, numConstraints, *participants)

	state, err := initCeremony(*circuit, numConstraints, *participants)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := writeState(state, *output); err != nil {
		fmt.Fprintf(os.Stderr, "error writing state: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Ceremony initialized: %s (%d powers)\n", *output, state.PowersNeeded)
}

func cmdContribute(args []string) {
	fs := flag.NewFlagSet("contribute", flag.ExitOnError)
	input := fs.String("input", "", "Input ceremony file (required)")
	output := fs.String("output", "", "Output ceremony file (required)")
	participant := fs.String("participant", "", "Participant name (required)")
	fs.Parse(args)

	if *input == "" || *output == "" || *participant == "" {
		fmt.Fprintln(os.Stderr, "error: --input, --output, and --participant are required")
		fs.Usage()
		os.Exit(1)
	}

	state, err := readState(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading state: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Contributing as %q (contribution %d/%d)...\n",
		*participant, len(state.Contributions)+1, state.Participants)

	contrib, err := contribute(state, *participant)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	state.Contributions = append(state.Contributions, contrib)

	if err := writeState(state, *output); err != nil {
		fmt.Fprintf(os.Stderr, "error writing state: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Contribution recorded: hash=%s\n", contrib.Hash)
}

func cmdVerify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	input := fs.String("input", "", "Ceremony file to verify (required)")
	fs.Parse(args)

	if *input == "" {
		fmt.Fprintln(os.Stderr, "error: --input is required")
		fs.Usage()
		os.Exit(1)
	}

	state, err := readState(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading state: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Verifying ceremony: %d contributions, %d powers...\n",
		len(state.Contributions), state.PowersNeeded)

	if err := verifyCeremony(state); err != nil {
		fmt.Fprintf(os.Stderr, "VERIFICATION FAILED: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "VERIFICATION PASSED")
	for i, c := range state.Contributions {
		fmt.Fprintf(os.Stderr, "  [%d] %s at %s (hash: %s)\n", i+1, c.Participant, c.Timestamp, c.Hash[:16])
	}
}

func cmdExport(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	input := fs.String("input", "", "Ceremony file to export (required)")
	output := fs.String("output", "", "Output SRS binary file (required)")
	fs.Parse(args)

	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "error: --input and --output are required")
		fs.Usage()
		os.Exit(1)
	}

	state, err := readState(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading state: %v\n", err)
		os.Exit(1)
	}

	// Verify before export
	if err := verifyCeremony(state); err != nil {
		fmt.Fprintf(os.Stderr, "VERIFICATION FAILED — refusing to export: %v\n", err)
		os.Exit(1)
	}

	srs := exportSRS(state)

	if err := os.WriteFile(*output, srs, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "error writing SRS: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "SRS exported: %s (%d bytes)\n", *output, len(srs))
}

// stateEnvelope wraps CeremonyState with a SHA-256 integrity hash.
type stateEnvelope struct {
	State     jsontext.Value `json:"state"`
	Integrity string          `json:"integrity"` // hex(SHA-256(state bytes))
}

func readState(path string) (*CeremonyState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var env stateEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse ceremony envelope: %w", err)
	}

	// Verify integrity
	if env.Integrity == "" {
		return nil, fmt.Errorf("ceremony file missing integrity hash")
	}
	h := sha256.Sum256(env.State)
	expected := hex.EncodeToString(h[:])
	if env.Integrity != expected {
		return nil, fmt.Errorf("integrity check failed: expected %s, got %s", expected, env.Integrity)
	}

	var state CeremonyState
	if err := json.Unmarshal(env.State, &state); err != nil {
		return nil, fmt.Errorf("parse ceremony state: %w", err)
	}
	return &state, nil
}

func writeState(state *CeremonyState, path string) error {
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return err
	}

	h := sha256.Sum256(stateBytes)
	env := stateEnvelope{
		State:     stateBytes,
		Integrity: hex.EncodeToString(h[:]),
	}

	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
