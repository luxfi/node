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
// implementation and the path it serves the C-chain's Ethereum RPC on — the
// two differ, and that is the only per-node knowledge the harness carries.
//
// There is deliberately no network name here. A name is an assertion about a
// chain made without asking it, and the probe already asks: eth_chainId is
// reported for every node on every run. Carrying both invites the failure
// where the label and the chain disagree and the label is believed.
type Endpoint struct{ Lang, URL string }

// The fleet this harness reaches when told nothing. A port is where a node
// happened to be started, not a property of the implementation, so -fleet
// replaces an entry rather than the harness carrying a second list.
var fleet = []Endpoint{
	{"go", "http://127.0.0.1:21610/v1/chain/c"},
	{"cpp", "http://127.0.0.1:21730/"},
	{"rust", "http://127.0.0.1:21780/v1/chain/hanzo"},
}

// retarget rewrites the URL of each named language. The argument is
// `lang=url` repeated, comma separated: `-fleet go=http://…,rust=http://…`.
// A language the fleet does not carry is an error rather than a silent
// addition, because the whole point is a differential across known
// implementations and a typo would otherwise quietly run two.
func retarget(spec string) error {
	for _, one := range strings.Split(spec, ",") {
		one = strings.TrimSpace(one)
		if one == "" {
			continue
		}
		lang, url, ok := strings.Cut(one, "=")
		if !ok || url == "" {
			return fmt.Errorf("fleet entry %q is not lang=url", one)
		}
		found := false
		for i := range fleet {
			if fleet[i].Lang == lang {
				fleet[i].URL, found = url, true
				break
			}
		}
		if !found {
			return fmt.Errorf("fleet has no language %q", lang)
		}
	}
	return nil
}

func main() {
	var (
		only   = flag.String("only", "", "comma-separated languages to run")
		probe  = flag.Bool("probe", false, "only report what each node admits to")
		settle = flag.Duration("settle", 3*time.Second, "how long to watch for the height to move on its own")
		target = flag.String("fleet", "", "override node URLs: lang=url[,lang=url...]")
	)
	flag.Parse()

	if err := retarget(*target); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	var nodes []*Node
	for _, e := range fleet {
		if *only != "" && !strings.Contains(","+*only+",", ","+e.Lang+",") {
			continue
		}
		nodes = append(nodes, NewNode(e.Lang, e.URL))
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
