// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import "testing"

// The map this replaced named six letters while eleven chains were running.
// Derivation is what stops a list going stale without looking stale.
func TestEveryChainOnTheNetworkEarnsItsLetter(t *testing.T) {
	for _, name := range []string{
		"C-Chain", "X-Chain", "P-Chain", "M-Chain", "B-Chain",
		"A-Chain", "Z-Chain", "Q-Chain", "G-Chain", "K-Chain", "F-Chain", "D-Chain",
	} {
		got := lettersFor(name)
		if len(got) != 2 {
			t.Fatalf("%s earned %d routes, want its letter in both cases", name, len(got))
		}
		upper, lower := name[:1], string(name[0]|0x20)
		if _, ok := got[upper]; !ok {
			t.Fatalf("%s has no %s route", name, upper)
		}
		if _, ok := got[lower]; !ok {
			t.Fatalf("%s has no %s route — paths are case-sensitive and a reader does not expect it", name, lower)
		}
	}
}

// A name that is not <letter>-Chain earns no letter. Otherwise "zoo" would
// claim "z".
func TestOnlyALetterChainEarnsALetter(t *testing.T) {
	for _, name := range []string{"zoo", "hanzo", "", "-Chain", "CC-Chain", "C-Net", "1-Chain"} {
		if got := lettersFor(name); len(got) != 0 {
			t.Fatalf("%q earned %v", name, got)
		}
	}
}

// The reservation. Without it a chain names itself "m", registers at
// /v1/chain/m, and sits beside M-Chain at /v1/chain/M — two routes, because
// HTTP paths are case-sensitive, and a reader cannot tell which one signs.
func TestThePrimaryNetworksNamesAreNotAvailable(t *testing.T) {
	for _, seg := range []string{
		"m", "M", "b", "B", "c", "x", "p", "q", "z", "a", "g", "k", "f",
		"m-chain", "M-Chain", "mchain", "mvm", "bvm", "cvm",
		// The VMs by name. `mpc` is what M-Chain does and `mpcvm` implements
		// it; a reader would take either for the register that signs.
		"mpc", "mpcvm", "MPC", "mpc-chain",
		// V-Chain (LP-1350) is unbuilt and its name is already held.
		"v", "V", "v-chain", "vchain", "vvm",
		"bridge", "bridgevm", "Bridge", "bridge-chain",
		"oracle", "oraclevm", "relay", "relayvm", "dex", "dexvm",
		"zk", "zkvm", "quantum", "quantumvm", "evm", "avm", "platform",
	} {
		if !reserved(seg) {
			t.Fatalf("%q is available to any chain that asks for it", seg)
		}
	}
}

// And the reservation is bounded — it takes the letters and their forms, not
// every name anyone might want.
func TestReservationDoesNotTakeOrdinaryNames(t *testing.T) {
	for _, seg := range []string{
		"zoo", "hanzo", "pars", "osage", "ethereum", "base", "bitcoin",
		"", "chain", "vm", "mm", "m-net", "my-chain",
		// Near misses that are somebody else's to take.
		"mpcx", "bridged", "oracles", "relayer", "dexes", "zoovm",
	} {
		if reserved(seg) {
			t.Fatalf("%q was reserved, which takes a name nobody needed to take", seg)
		}
	}
}
