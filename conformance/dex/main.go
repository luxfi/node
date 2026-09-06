// SPDX-License-Identifier: BSD-3-Clause-Eco

// Command dex runs the AMM and order-book differential against every running
// implementation of the C-chain, in the same order, and puts the answers next
// to each other.
//
// See README.md for what is compared and why the comparison is of contract
// state rather than of the block's state root.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// Fleet is the set of live nodes. Each entry names the language that wrote the
// implementation, the testnet it serves, and the path it serves the C-chain's
// Ethereum RPC on — the three differ, and that is the only per-node knowledge
// the harness carries.
type Endpoint struct{ Lang, Net, URL string }

var fleet = []Endpoint{
	{"go", "lux-1337", "http://127.0.0.1:21610/v1/chain/C"},
	{"cpp", "zoo-200201", "http://127.0.0.1:21730/"},
	{"rust", "hanzo-36962", "http://127.0.0.1:21780/v1/chain/hanzo"},
}

func main() {
	var (
		only   = flag.String("only", "", "comma-separated languages to run")
		probe  = flag.Bool("probe", false, "only report what each node admits to")
		settle = flag.Duration("settle", 3*time.Second, "how long to watch for the height to move on its own")
	)
	flag.Parse()

	var nodes []*Node
	for _, e := range fleet {
		if *only != "" && !strings.Contains(","+*only+",", ","+e.Lang+",") {
			continue
		}
		nodes = append(nodes, NewNode(e.Lang, e.Net, e.URL))
	}
	if len(nodes) == 0 {
		fmt.Fprintln(os.Stderr, "no nodes selected")
		os.Exit(2)
	}

	cands := Candidates(6)
	fmt.Println("== probe ==")
	probes := make([]*Probe, len(nodes))
	for i, n := range nodes {
		probes[i] = n.Probe(cands, *settle)
		fmt.Println("  " + probes[i].Line())
	}

	if *probe {
		return
	}

	fmt.Println()
	runs := make([]*Run, 0, len(nodes))
	for i, n := range nodes {
		runs = append(runs, Exercise(n, probes[i], false))
	}
	Report(runs)

	// The control. The same script with the two same-price asks arriving in the
	// other order, on a second market on the same chains. Everything that is a
	// count or an amount must be unchanged; only the matching SEQUENCE differs.
	// Run last, and reported against the first run, because it is the only
	// evidence that the agreement above was worth having.
	fmt.Println()
	ctrl := make([]*Run, 0, len(nodes))
	for i, n := range nodes {
		ctrl = append(ctrl, Exercise(n, probes[i], true))
	}
	Control(runs, ctrl)
}
