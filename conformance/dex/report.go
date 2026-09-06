// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

// Putting the answers next to each other. The report compares and says what it
// found; it decides nothing about what an AMM or an order book ought to do,
// because a reporter that knew the rules would be a fourth implementation and
// would hide a disagreement on the day it was wrong.

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
)

func Report(runs []*Run) {
	fmt.Println("== what each implementation could do ==")
	for _, r := range runs {
		if r.Ready {
			fmt.Printf("  %-5s %-14s market at %s\n", r.Node.Lang, r.Node.Net, r.Contract.Hex())
		} else {
			fmt.Printf("  %-5s %-14s COULD NOT: %s\n", r.Node.Lang, r.Node.Net, r.Why)
		}
	}

	ready := []*Run{}
	for _, r := range runs {
		if r.Ready {
			ready = append(ready, r)
		}
	}
	if len(ready) == 0 {
		fmt.Println("\nno implementation reached a deployed market; nothing to compare")
		return
	}

	fmt.Println("\n== the script, step by step ==")
	fmt.Printf("  %-46s %-8s", "operation", "expect")
	for _, r := range ready {
		fmt.Printf(" %-22s", r.Node.Lang)
	}
	fmt.Println()
	for i := range ready[0].Steps {
		s0 := ready[0].Steps[i]
		fmt.Printf("  %-46s %-8s", trunc(s0.Name, 46), s0.Expect)
		for _, r := range ready {
			if i >= len(r.Steps) {
				fmt.Printf(" %-22s", "-")
				continue
			}
			fmt.Printf(" %-22s", verdict(r.Steps[i]))
		}
		fmt.Println()
	}

	fmt.Println("\n== the market's state, slot by slot ==")
	fmt.Printf("  %-12s", "slot")
	for _, r := range ready {
		fmt.Printf(" %-22s", r.Node.Lang)
	}
	fmt.Println()
	for _, name := range slotNames {
		fmt.Printf("  %-12s", name)
		for _, r := range ready {
			fmt.Printf(" %-22s", show(name, r.Slots[name]))
		}
		fmt.Println()
	}

	fmt.Println("\n== agreement ==")
	byDigest := map[string][]string{}
	for _, r := range ready {
		byDigest[r.Digest] = append(byDigest[r.Digest], r.Node.Lang)
	}
	keys := make([]string, 0, len(byDigest))
	for k := range byDigest {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s  %s\n", short(k), strings.Join(byDigest[k], " "))
	}
	if len(byDigest) == 1 && len(ready) > 1 {
		fmt.Printf("\n  the %d implementations that ran the script hold the same market state\n", len(ready))
	} else if len(byDigest) > 1 {
		fmt.Println("\n  THE IMPLEMENTATIONS DISAGREE. The differing slots:")
		for _, name := range slotNames {
			seen := map[string]bool{}
			for _, r := range ready {
				seen[r.Slots[name]] = true
			}
			if len(seen) > 1 {
				fmt.Printf("    %-12s", name)
				for _, r := range ready {
					fmt.Printf(" %s=%s", r.Node.Lang, short(r.Slots[name]))
				}
				fmt.Println()
			}
		}
	}

	// The block's state root is reported but never compared: three chains with
	// three genesis allocations cannot share one, and a harness that compared
	// them anyway would report a disagreement on every run and mean nothing by
	// it. `fills` above is the root that is comparable, and it is the one
	// matching order is visible in.
	fmt.Println("\n== each chain's own head, for the record (not comparable across chains) ==")
	for _, r := range ready {
		fmt.Printf("  %-5s height=%-10s stateRoot=%s\n", r.Node.Lang, r.Height, short(r.Root))
	}
}

func verdict(s Step) string {
	switch {
	case !s.Sent:
		return "not sent"
	case !s.Mined:
		return "never executed"
	case s.Expect == "refuse" && !s.Changed:
		return "refused (state held)"
	case s.Expect == "refuse" && s.Changed:
		return "ACCEPTED (should not)"
	case s.Expect == "apply" && s.Changed:
		return "applied"
	default:
		return "NO EFFECT"
	}
}

// digestSlots are the two slots whose value is a hash. Everything else in the
// comparable set is a count or an amount, and printing the first few nibbles of
// a left-padded number shows a column of zeros where the answer is — which is
// how a real disagreement hides in a report that says nothing.
var digestSlots = map[string]bool{"fills": true, "book": true}

func show(name, hexVal string) string {
	if digestSlots[name] {
		return short(hexVal)
	}
	v, ok := new(big.Int).SetString(strings.TrimPrefix(hexVal, "0x"), 16)
	if !ok {
		return short(hexVal)
	}
	return v.String()
}

func short(h string) string {
	if len(h) > 18 {
		return h[:18]
	}
	if h == "" {
		return "-"
	}
	return h
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// Control reports the permuted run against the ordinary one. It answers one
// question: does the digest this differential compares actually move when the
// matching order moves? If it does not, everything above is a comparison of
// something that cannot disagree, and the harness says so rather than claiming
// an agreement it did not test for.
func Control(base, perm []*Run) {
	fmt.Println("== control: the same book, the two equal-priced asks arriving in the other order ==")

	// The aggregates that must NOT move: same quantity trades at the same
	// prices either way.
	invariant := []string{"poolBase", "poolQuote", "poolShares", "nextId",
		"fillCount", "liveOrders", "refusals", "tradedQty"}

	sensitive, blind, broken := 0, 0, 0
	for i := range base {
		b, p := base[i], perm[i]
		lang := b.Node.Lang
		if !b.Ready || !p.Ready {
			fmt.Printf("  %-5s could not run the control\n", lang)
			broken++
			continue
		}
		drifted := []string{}
		for _, name := range invariant {
			if b.Slots[name] != p.Slots[name] {
				drifted = append(drifted, name)
			}
		}
		moved := b.Slots["fills"] != p.Slots["fills"]
		switch {
		case len(drifted) > 0:
			fmt.Printf("  %-5s the control changed more than the order: %s\n", lang, strings.Join(drifted, " "))
			broken++
		case moved:
			fmt.Printf("  %-5s totals identical, fills %s -> %s  (order is visible)\n",
				lang, short(b.Slots["fills"]), short(p.Slots["fills"]))
			sensitive++
		default:
			fmt.Printf("  %-5s totals identical and fills IDENTICAL — order is NOT visible\n", lang)
			blind++
		}
	}

	// And the permuted run must still agree across implementations: a different
	// order, but the same different order everywhere.
	seen := map[string][]string{}
	for _, p := range perm {
		if p.Ready {
			seen[p.Digest] = append(seen[p.Digest], p.Node.Lang)
		}
	}
	fmt.Println()
	if len(seen) == 1 && len(perm) > 1 {
		fmt.Println("  the permuted book also comes out the same on every implementation")
	} else if len(seen) > 1 {
		fmt.Println("  THE IMPLEMENTATIONS DISAGREE ON THE PERMUTED BOOK:")
		for k, v := range seen {
			fmt.Printf("    %s  %s\n", short(k), strings.Join(v, " "))
		}
	}
	if blind > 0 || broken > 0 {
		fmt.Printf("\n  CONTROL FAILED on %d of %d: the comparison above is not evidence of matching order\n",
			blind+broken, len(base))
		return
	}
	fmt.Printf("\n  control passed on %d of %d: the digest moves with matching order and nothing else\n",
		sensitive, len(base))
}
