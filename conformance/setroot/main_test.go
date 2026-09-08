// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/go-json-experiment/json"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/vms/platformvm"
	validators "github.com/luxfi/validators"
)

const corpusDir = "../corpus"

// The committed corpus must be what the live definition produces today. A
// corpus that drifts from chains.SetRoot would hand the other two
// implementations an answer this build no longer gives, and they would agree
// with a number the network has stopped using.
func TestCorpusIsWhatTheDefinitionProduces(t *testing.T) {
	for name, want := range map[string][]string{
		"setroot.tsv":          vectorLines(),
		"setroot-expected.tsv": expectedLines(),
	} {
		got, err := os.ReadFile(corpusDir + "/" + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != strings.Join(want, "\n")+"\n" {
			t.Errorf("%s is not what this build emits.\n"+
				"The set-root encoding moved, and every other implementation of a Lux\n"+
				"validator is now wrong against this build. If that was intended, run\n"+
				"`go run ./conformance/setroot -emit conformance/corpus`, read the diff as a\n"+
				"protocol change, and rerun `make setroot`.", name)
		}
	}
}

// A corpus whose vectors all hash the same would pass in every language and
// measure nothing. Every root is distinct except the one pair that is meant to
// collide: one set written in two orders.
func TestEveryVectorSaysSomethingDifferent(t *testing.T) {
	seen := map[string]string{}
	for _, v := range corpus() {
		r := root(v.set).Hex()
		if first, dup := seen[r]; dup {
			if first == "SET_ORDER_SORTED" && v.id == "SET_ORDER_REVERSED" {
				continue // the one collision the corpus is asserting
			}
			t.Errorf("%s and %s have the same root, so neither can fail alone", first, v.id)
			continue
		}
		seen[r] = v.id
	}
	if len(seen) < 2 {
		t.Fatal("a corpus of one root cannot separate two implementations")
	}
}

// The order pair is the assertion, not an accident: an implementation that
// hashed in the order it was handed the members would answer these two
// differently, and this is what says so.
func TestOneSetInTwoOrdersIsOneRoot(t *testing.T) {
	var sorted, reversed []member
	for _, v := range corpus() {
		switch v.id {
		case "SET_ORDER_SORTED":
			sorted = v.set
		case "SET_ORDER_REVERSED":
			reversed = v.set
		}
	}
	if len(sorted) == 0 || len(reversed) == 0 {
		t.Fatal("the corpus lost its order pair")
	}
	if sorted[0].node == reversed[0].node {
		t.Fatal("the two orders are the same order")
	}
	if root(sorted) != root(reversed) {
		t.Error("one set written two ways gave two roots")
	}
}

// The vector line an evaluator is handed must parse back to the members it was
// written from, or the corpus asks a different question than it recorded an
// answer to.
func TestAVectorParsesBackToItsMembers(t *testing.T) {
	for _, v := range corpus() {
		for _, m := range v.set {
			back, err := parseMember(m.String())
			if err != nil {
				t.Fatalf("%s: %v", v.id, err)
			}
			if back.node != m.node || back.weight != m.weight || string(back.key) != string(m.key) {
				t.Errorf("%s: a member did not survive the round trip", v.id)
			}
		}
	}
}

// The set-root corpus asks whether three implementations hash a set the same.
// This asks the question underneath it: whether the document the network
// publishes a set in can be read back into the set it committed to.
//
// The wire carries one weight per validator. The root hashes Light. A reader
// that filled in Weight and left Light at zero would rebuild a set that looks
// right and commits to different bytes — so the reply says once what it means,
// and this is what holds it to that.
func TestTheDocumentReadsBackIntoTheSetItCommitsTo(t *testing.T) {
	var a, b ids.NodeID
	a[0], b[0] = 0x01, 0x02
	key := make([]byte, 96)
	for i := range key {
		key[i] = 0x5b
	}

	held := map[ids.NodeID]*validators.GetValidatorOutput{
		a: {NodeID: a, PublicKey: key, Light: 7, Weight: 7},
		b: {NodeID: b, PublicKey: nil, Light: 3, Weight: 3},
	}

	doc, err := json.Marshal(&platformvm.GetValidatorsAtReply{Validators: platformvm.ValidatorSet{
		{NodeID: a, PublicKey: key, Weight: 7},
		{NodeID: b, PublicKey: nil, Weight: 3},
	}})
	if err != nil {
		t.Fatalf("publish the set: %v", err)
	}

	var read platformvm.GetValidatorsAtReply
	if err := json.Unmarshal(doc, &read); err != nil {
		t.Fatalf("read the document back: %v", err)
	}

	if got, want := chains.SetRoot(read.Set()), chains.SetRoot(held); got != want {
		t.Errorf("the published set does not commit to what the network committed to\n"+
			" held      %s\n rebuilt   %s\n document  %s", want, got, doc)
	}
}
