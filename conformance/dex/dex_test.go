// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

// What can be checked without a chain. The differential itself needs three
// running nodes, but the pieces that decide what those nodes are asked — the
// keys, the calldata, and the fold the whole comparison rests on — are
// arithmetic, and arithmetic is checkable here.

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/luxfi/crypto"
	"github.com/luxfi/geth/common"
)

// The published Anvil account. If this address ever moved, the C++ chain's
// genesis would fund a stranger and every run would report "no key holds a
// balance", so it is worth pinning.
func TestAnvilKeyRecoversToItsPublishedAddress(t *testing.T) {
	a, err := FromHex(AnvilKey, "anvil/0")
	if err != nil {
		t.Fatal(err)
	}
	const want = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	if a.Addr.Hex() != want {
		t.Fatalf("anvil key recovers to %s, want %s", a.Addr.Hex(), want)
	}
}

// BIP39 + BIP32 are implemented here rather than pulled in, so the derivation
// is pinned against the addresses the harness actually funds.
func TestLightMnemonicDerivesTheTraderAddresses(t *testing.T) {
	want := []string{
		"0x35D64Ff3f618f7a17DF34DCb21be375A4686a8de",
		"0xdAF82928dE0ABBAE133322020B253283d335d3A8",
	}
	for i, w := range want {
		a, err := BIP44(Mnemonic, 60, uint32(i))
		if err != nil {
			t.Fatal(err)
		}
		if a.Addr.Hex() != w {
			t.Errorf("m/44'/60'/0'/0/%d = %s, want %s", i, a.Addr.Hex(), w)
		}
	}
}

// The three traders must be the same three addresses in the same order on every
// chain. Letting a chain's genesis choose them folded three different addresses
// into the digest and reported a disagreement about matching order that was
// really a disagreement about whose money it was.
func TestTradersArePinnedAndDistinct(t *testing.T) {
	who := Traders()
	if len(who) != 3 {
		t.Fatalf("got %d traders, want 3", len(who))
	}
	seen := map[common.Address]bool{}
	for _, a := range who {
		if seen[a.Addr] {
			t.Fatalf("%s appears twice; a self-trade could not be told from a cross", a.Addr.Hex())
		}
		seen[a.Addr] = true
	}
	if who[0].Addr != mustAnvil(t).Addr {
		t.Errorf("maker is %s, want the anvil account", who[0].Addr.Hex())
	}
}

func mustAnvil(t *testing.T) Account {
	t.Helper()
	a, err := FromHex(AnvilKey, "anvil/0")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSelectorIsTheKeccakOfTheSignature(t *testing.T) {
	sigs := []string{
		"swap(bool,uint128)", "place(bool,uint96,uint96)", "cancel(uint256)",
		"addLiquidity(uint128,uint128)", "removeLiquidity(uint256)",
	}
	seen := map[string]string{}
	for _, sig := range sigs {
		got := selector(sig)
		full := crypto.Keccak256([]byte(sig))
		if len(got) != 4 || hex.EncodeToString(got) != hex.EncodeToString(full[:4]) {
			t.Errorf("%s: selector %x is not the first four bytes of %x", sig, got, full)
		}
		// Distinct signatures must not collide, or one call would run another.
		k := hex.EncodeToString(got)
		if prev, ok := seen[k]; ok {
			t.Fatalf("%s and %s share selector %s", prev, sig, k)
		}
		seen[k] = sig
	}
}

// The constructor takes one dynamic array, so the head carries an offset and
// the tail carries the length and the elements. Getting that wrong deploys a
// market whose traders are unfunded, which looks exactly like a chain refusing
// every order.
func TestDeployDataPlacesTheArrayAfterTheStaticHead(t *testing.T) {
	code := []byte{0x60, 0x80}
	who := Traders()
	out := deployData(code, []common.Address{who[0].Addr, who[1].Addr, who[2].Addr}, 1_000_000, 2_000_000)

	args := out[len(code):]
	if len(args) != 32*7 {
		t.Fatalf("args are %d bytes, want 7 words", len(args))
	}
	if got := new(big.Int).SetBytes(args[0:32]).Uint64(); got != 96 {
		t.Errorf("array offset is %d, want 96 (three head words)", got)
	}
	if got := new(big.Int).SetBytes(args[32:64]).Uint64(); got != 1_000_000 {
		t.Errorf("base is %d, want 1000000", got)
	}
	if got := new(big.Int).SetBytes(args[64:96]).Uint64(); got != 2_000_000 {
		t.Errorf("quote is %d, want 2000000", got)
	}
	if got := new(big.Int).SetBytes(args[96:128]).Uint64(); got != 3 {
		t.Errorf("array length is %d, want 3", got)
	}
	for i, w := range who {
		at := 128 + i*32
		if common.BytesToAddress(args[at:at+32]) != w.Addr {
			t.Errorf("trader %d is %s, want %s", i, common.BytesToAddress(args[at:at+32]).Hex(), w.Addr.Hex())
		}
	}
}

// The embedded bytecode is what actually gets deployed. An empty or truncated
// blob deploys a contract with no code, and every step then reads zeros and
// reports three implementations agreeing perfectly about nothing.
func TestEmbeddedBytecodeIsPresentAndWellFormed(t *testing.T) {
	s := strings.TrimSpace(dexHex)
	if len(s) < 2000 {
		t.Fatalf("embedded bytecode is %d hex chars, far too short to be the market", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("embedded bytecode is not hex: %v", err)
	}
	if b[0] != 0x60 {
		t.Errorf("creation code starts with %#x, want a PUSH1", b[0])
	}
}

// The script must exercise what it claims to. A refusal that is never sent, or
// a crossing order that never crosses, would pass every comparison silently.
func TestScriptCoversTheOperationsThatWereAskedFor(t *testing.T) {
	s := script(false)
	var refusals, swaps, places, cancels, liq int
	for _, o := range s {
		if o.expect == "refuse" {
			refusals++
		}
		switch {
		case strings.Contains(o.name, "swap"):
			swaps++
		case strings.Contains(o.name, "cancel"):
			cancels++
		case strings.Contains(o.name, "Liquidity"):
			liq++
		case strings.Contains(o.name, "ask") || strings.Contains(o.name, "bid"):
			places++
		}
	}
	if refusals < 3 {
		t.Errorf("only %d refusals; the unfunded order, the self-trade and the unfunded swap must all be tried", refusals)
	}
	if swaps < 2 || liq < 3 || cancels < 1 || places < 5 {
		t.Errorf("thin script: swaps=%d liquidity=%d cancels=%d places=%d", swaps, liq, cancels, places)
	}
}

// The control must differ from the base run in the order of the two
// equal-priced asks and in nothing else. If it differed elsewhere, a moved
// digest would prove nothing about matching order.
func TestControlPermutesOnlyTheArrivalOfTheEqualPricedAsks(t *testing.T) {
	base, perm := script(false), script(true)
	if len(base) != len(perm) {
		t.Fatalf("the control has %d steps, the base run %d", len(perm), len(base))
	}
	differing := []int{}
	for i := range base {
		if base[i].sender != perm[i].sender || hex.EncodeToString(base[i].data) != hex.EncodeToString(perm[i].data) {
			differing = append(differing, i)
		}
	}
	if len(differing) != 2 {
		t.Fatalf("the control changes %d steps, want exactly the two equal-priced asks", len(differing))
	}
	// And it must be a swap of the two, not two new orders.
	i, j := differing[0], differing[1]
	if hex.EncodeToString(base[i].data) != hex.EncodeToString(perm[j].data) ||
		hex.EncodeToString(base[j].data) != hex.EncodeToString(perm[i].data) {
		t.Error("the two differing steps are not each other's swap")
	}
	if base[i].sender != perm[j].sender || base[j].sender != perm[i].sender {
		t.Error("the swap did not carry the senders with it")
	}
}
