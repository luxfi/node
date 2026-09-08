// SPDX-License-Identifier: BSD-3-Clause-Eco

// The validator-set root, handed to every implementation that has one.
//
// The root is the commitment to the set a vote is cast under, and it sits
// INSIDE the signed vote message. Two nodes that compute it differently do not
// disagree about a block — they sign different bytes, so each drops the other's
// votes as unverifiable and neither can say why. That makes it the first thing
// a Go, a Rust and a C++ daemon have to agree on before they can be on one
// network at all, and it is written four times: chains.SetRoot here,
// validators::root and Committee::root in the Rust node, validator_set_root in
// the C++ node. Every test any of them has compares an implementation to a
// restatement of the spec in its own language. Until this corpus, no two of
// them had ever been compared byte for byte.
//
// This program is both halves of that comparison, and deliberately so: -emit
// writes the corpus from the live chains.SetRoot, and the default run answers
// it from the same function. Its answers are therefore a RECORDING, which is
// what the runner treats the expected file as — it can disagree with anyone and
// it cannot stand in for the second implementation a field needs.
//
// Vector line:  V <id> set <member>...     member = nodeIDhex:weight:keyhex
// Result line:  R <id> <root> <note>
package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-json-experiment/json"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains"
	avajson "github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/vms/platformvm"
	validators "github.com/luxfi/validators"
)

func main() {
	emit := flag.String("emit", "", "write the corpus to this directory instead of answering it")
	publish := flag.String("publish", "", "print the document luxd serves for one vector's set, and the root it commits to")
	flag.Parse()

	if *publish != "" {
		if err := document(*publish, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "setroot:", err)
			os.Exit(1)
		}
		return
	}

	if *emit != "" {
		if err := write(*emit); err != nil {
			fmt.Fprintln(os.Stderr, "setroot:", err)
			os.Exit(1)
		}
		return
	}

	path := flag.Arg(0)
	if path == "" {
		fmt.Fprintln(os.Stderr, "setroot: name the corpus to answer")
		os.Exit(2)
	}
	if err := answer(path, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "setroot:", err)
		os.Exit(1)
	}
}

// A member as the corpus spells it.
type member struct {
	node   ids.NodeID
	weight uint64
	key    []byte
}

// A vector: an id, and the set it names.
type vector struct {
	id  string
	set []member
}

// fill builds a node id of `n` repeated, and a key of `keyLen` bytes of
// `n ^ 0x5a` — distinct per member, so a field read from the wrong offset shows
// as a byte difference rather than an accidental match.
func fill(n byte, weight uint64, keyLen int) member {
	var id ids.NodeID
	for i := range id {
		id[i] = n
	}
	key := make([]byte, keyLen)
	for i := range key {
		key[i] = n ^ 0x5a
	}
	return member{node: id, weight: weight, key: key}
}

// bytesKey is a member whose key is stated outright, for the vectors that are
// about the key's length rather than its content.
func bytesKey(n byte, weight uint64, key []byte) member {
	m := fill(n, weight, 0)
	m.key = key
	return m
}

const uncompressed = 96 // blst's serialisation of a BLS12-381 G1 key
const compressed = 48   // what a proof of possession signs, and the wrong root

// The corpus. Every vector is a case where two implementations of the same
// sentence can part company.
func corpus() []vector {
	rep := func(b byte, n int) []byte {
		s := make([]byte, n)
		for i := range s {
			s[i] = b
		}
		return s
	}
	return []vector{
		// The empty set is the explicit "unbound" answer and it is the zero id.
		// SHA-256 of no bytes is a perfectly good hash of the wrong thing.
		{"SET_EMPTY", nil},

		{"SET_ONE", []member{fill(0x01, 1, uncompressed)}},

		// One set, two file orders. An implementation that hashes in the order
		// it was handed answers these two differently.
		{"SET_ORDER_SORTED", []member{fill(0x01, 10, uncompressed), fill(0x02, 20, uncompressed), fill(0x03, 30, uncompressed)}},
		{"SET_ORDER_REVERSED", []member{fill(0x03, 30, uncompressed), fill(0x02, 20, uncompressed), fill(0x01, 10, uncompressed)}},

		// Node ids either side of 0x80. A comparison on a signed char sorts
		// these the other way round, and nothing else in the corpus notices.
		{"SET_SIGN_BOUNDARY", []member{fill(0x7f, 1, uncompressed), fill(0x80, 1, uncompressed)}},

		// The weight is hashed, so a set re-weighted is a different set.
		{"SET_WEIGHT_CHANGED", []member{fill(0x01, 11, uncompressed)}},

		// A weight that a JSON number cannot hold. The document spells weights
		// as strings for exactly this reason; a reader that parses them as
		// doubles commits to a set that does not exist.
		{"SET_WEIGHT_MAX", []member{fill(0x01, ^uint64(0), uncompressed)}},

		// A seat with no stake. It is still in the set and still hashed.
		{"SET_WEIGHT_ZERO", []member{fill(0x01, 0, uncompressed), fill(0x02, 5, uncompressed)}},

		// The compressed key is what a committee file carries and what a proof
		// of possession signs. Hashing it here produces a root that verifies
		// against nothing, so the corpus states both and they must differ.
		{"SET_KEY_COMPRESSED", []member{fill(0x01, 1, compressed)}},

		// A validator with no BLS key at all: length prefix zero, no bytes.
		{"SET_KEY_ABSENT", []member{fill(0x01, 1, 0)}},

		// The length prefix, on the only pair that proves it is there. Without
		// it these two sets have the same preimage.
		{"SET_LENGTH_PREFIX_A", []member{bytesKey(0x01, 1, rep(0xAA, 32)), bytesKey(0x02, 1, rep(0xAA, 64))}},
		{"SET_LENGTH_PREFIX_B", []member{bytesKey(0x01, 1, rep(0xAA, 64)), bytesKey(0x02, 1, rep(0xAA, 32))}},
	}
}

// root is the answer, from the live definition. The set is handed over as the
// map the definition takes, which is also why the corpus cannot state a
// validator twice: the network's own type cannot hold it either.
func root(set []member) ids.ID {
	if len(set) == 0 {
		return chains.SetRoot(nil)
	}
	m := make(map[ids.NodeID]*validators.GetValidatorOutput, len(set))
	for _, v := range set {
		m[v.node] = &validators.GetValidatorOutput{
			NodeID:    v.node,
			PublicKey: v.key,
			Light:     v.weight,
			Weight:    v.weight,
		}
	}
	return chains.SetRoot(m)
}

func (m member) String() string {
	return fmt.Sprintf("%s:%d:%s", hex.EncodeToString(m.node[:]), m.weight, hex.EncodeToString(m.key))
}

func parseMember(s string) (member, error) {
	f := strings.Split(s, ":")
	if len(f) != 3 {
		return member{}, fmt.Errorf("a member is nodeID:weight:key, got %q", s)
	}
	raw, err := hex.DecodeString(f[0])
	if err != nil || len(raw) != ids.NodeIDLen {
		return member{}, fmt.Errorf("%q is not a %d-byte node id", f[0], ids.NodeIDLen)
	}
	weight, err := strconv.ParseUint(f[1], 10, 64)
	if err != nil {
		return member{}, fmt.Errorf("%q is not a weight: %w", f[1], err)
	}
	key, err := hex.DecodeString(f[2])
	if err != nil {
		return member{}, fmt.Errorf("%q is not a key: %w", f[2], err)
	}
	var m member
	copy(m.node[:], raw)
	m.weight, m.key = weight, key
	return m, nil
}

// Vectors returns the corpus lines, and Expected the recorded answers. They are
// two files because they are two different things: one is the question every
// implementation is asked, the other is what this one said.
func vectorLines() []string {
	var out []string
	for _, v := range corpus() {
		cols := []string{"V", v.id, "set"}
		for _, m := range v.set {
			cols = append(cols, m.String())
		}
		out = append(out, strings.Join(cols, "\t"))
	}
	return out
}

func expectedLines() []string {
	var out []string
	for _, v := range corpus() {
		out = append(out, strings.Join([]string{"R", v.id, root(v.set).Hex(), ""}, "\t"))
	}
	return out
}

func write(dir string) error {
	for name, lines := range map[string][]string{
		"setroot.tsv":          vectorLines(),
		"setroot-expected.tsv": expectedLines(),
	} {
		body := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// answer reads the corpus and prints one result line per vector. It reads the
// file rather than its own table, so an evaluator handed a corpus that has
// moved answers the corpus and not its memory of it.
func answer(path string, out *os.File) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(out)
	defer w.Flush()

	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 1<<20), 1<<26)
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 3 || cols[0] != "V" {
			return fmt.Errorf("not a vector line: %q", line)
		}
		var set []member
		for _, c := range cols[3:] {
			m, err := parseMember(c)
			if err != nil {
				return fmt.Errorf("%s: %w", cols[1], err)
			}
			set = append(set, m)
		}
		fmt.Fprintf(w, "R\t%s\t%s\t\n", cols[1], root(set).Hex())
	}
	return s.Err()
}

// document prints what luxd serves for one vector's set, and under it the root
// luxd commits to for that same set.
//
// The corpus proves the three hash a set alike. This is the join: a port only
// has a set to hash if it can read the one luxd publishes, and the document
// carries a weight per validator where the root hashes Light. Two lines, so a
// reader in another language can be handed the first and checked against the
// second.
func document(id string, out *os.File) error {
	for _, v := range corpus() {
		if v.id != id {
			continue
		}
		set := make(platformvm.ValidatorSet, 0, len(v.set))
		for _, m := range v.set {
			set = append(set, platformvm.Validator{
				NodeID:    m.node,
				PublicKey: m.key,
				Weight:    avajson.Uint64(m.weight),
			})
		}
		doc, err := json.Marshal(&platformvm.GetValidatorsAtReply{Validators: set})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s\n%s\n", doc, root(v.set).Hex())
		return nil
	}
	return fmt.Errorf("no vector named %s", id)
}
