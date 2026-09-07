// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package adopt

import (
	"testing"

	"github.com/luxfi/ids"
)

func id(b byte) ids.ID {
	var i ids.ID
	i[0] = b
	return i
}

// The real networks, as they actually relate.
var (
	ethereum = Record{
		ChainID: 1, Identity: id(1), Anchor: Attested, Holding: Gateway,
		Custody: "mchain/eth", Endpoints: []string{"https://eth"}, Depth: 64,
	}
	base = Record{
		ChainID: 8453, Identity: id(2), Parent: id(1), Anchor: Attested, Holding: Gateway,
		Custody: "mchain/base", Endpoints: []string{"https://base"}, Depth: 200,
	}
	bitcoin = Record{
		ChainID: 0x1000, Identity: id(3), Anchor: Attested, Holding: Address,
		Custody: "mchain/btc", Endpoints: []string{"https://btc"}, Depth: 6,
	}
)

// An L2 may not be adopted before the L1 it takes security from. Adopting Base
// alone records a belief whose basis is a chain the register has never heard
// of.
func TestAnL2CannotBeAdoptedBeforeItsL1(t *testing.T) {
	g := NewRegistry()

	if err := g.Adopt(base); err == nil {
		t.Fatal("Base was adopted with no Ethereum to take security from")
	}
	if g.Len() != 0 {
		t.Fatal("a refused adoption left a record behind")
	}

	if err := g.Adopt(ethereum); err != nil {
		t.Fatalf("ethereum: %v", err)
	}
	if err := g.Adopt(base); err != nil {
		t.Fatalf("base, after ethereum: %v", err)
	}
}

// And the same rule backwards: releasing the L1 would make the L2's anchor
// unreadable, because that anchor is a claim about the L1.
func TestAnL1CannotBeReleasedWhileAnL2DependsOnIt(t *testing.T) {
	g := NewRegistry()
	mustAdopt(t, g, ethereum, base)

	if err := g.Release(ethereum.Key()); err == nil {
		t.Fatal("ethereum was released while base still took security from it")
	}

	// Release the dependent first and the order opens up.
	if err := g.Release(base.Key()); err != nil {
		t.Fatalf("base: %v", err)
	}
	if err := g.Release(ethereum.Key()); err != nil {
		t.Fatalf("ethereum, after base: %v", err)
	}
}

// The line the permissionless path rests on. Paying buys a record; a record is
// not a trust decision, and a declared anchor may not hold value.
func TestADeclaredAnchorCannotHoldValue(t *testing.T) {
	paid := Record{
		ChainID: 999, Identity: id(9), Anchor: Declared, Holding: Gateway,
		Endpoints: []string{"https://newchain"},
	}

	g := NewRegistry()
	if err := g.Adopt(paid); err != nil {
		t.Fatalf("a declared adoption should be accepted: %v", err)
	}
	if g.MayHold(paid.Key()) {
		t.Fatal("value may cross to a network nobody has verified")
	}

	// And it cannot be smuggled in by attaching a key to it.
	bought := paid
	bought.Custody = "mchain/newchain"
	if err := NewRegistry().Adopt(bought); err == nil {
		t.Fatal("a declared anchor was allowed a custody key")
	}
}

// An anchor that may hold value and names no key promises something it cannot
// do.
func TestACustodialAnchorNeedsAKey(t *testing.T) {
	r := ethereum
	r.Custody = ""
	if err := r.Valid(); err == nil {
		t.Fatal("an attested record with no custody key was accepted")
	}
}

// Strengthening is free; weakening is a separate act, because it changes what
// every downstream consumer is trusting without any of them being asked.
func TestAnAnchorStrengthensFreelyAndWeakensDeliberately(t *testing.T) {
	g := NewRegistry()
	mustAdopt(t, g, ethereum)

	up := ethereum
	up.Anchor = Proven
	if err := g.Revise(up); err != nil {
		t.Fatalf("strengthening should be free: %v", err)
	}

	down := up
	down.Anchor = Attested
	if err := g.Revise(down); err == nil {
		t.Fatal("Revise weakened an anchor")
	}

	if err := g.Weaken(ethereum.Key(), Attested); err != nil {
		t.Fatalf("Weaken: %v", err)
	}
	got, _ := g.Get(ethereum.Key())
	if got.Anchor != Attested {
		t.Fatalf("anchor is %s", got.Anchor)
	}

	// Dropping below the custodial line drops the key with it, or the record
	// would contradict itself.
	if err := g.Weaken(ethereum.Key(), Declared); err != nil {
		t.Fatalf("Weaken to declared: %v", err)
	}
	got, _ = g.Get(ethereum.Key())
	if got.Custody != "" {
		t.Fatal("a non-custodial anchor kept its custody key")
	}
	if g.MayHold(ethereum.Key()) {
		t.Fatal("value may still cross after the anchor was weakened")
	}
}

// A fork inherits the chain id of the chain it left, so the identity
// commitment is what keeps the two apart.
func TestAForkDoesNotOverwriteTheChainItLeft(t *testing.T) {
	g := NewRegistry()
	mustAdopt(t, g, ethereum)

	fork := ethereum
	fork.Identity = id(0xFF) // same chain id, different chain
	if err := g.Adopt(fork); err != nil {
		t.Fatalf("a fork is its own record: %v", err)
	}
	if g.Len() != 2 {
		t.Fatalf("a fork overwrote the chain it left: %d records", g.Len())
	}
}

// Where security comes from is not revisable. A network that changed it would
// have become something else.
func TestSecuritySourceIsNotRevisable(t *testing.T) {
	g := NewRegistry()
	mustAdopt(t, g, ethereum, bitcoin, base)

	moved := base
	moved.Parent = bitcoin.Key()
	if err := g.Revise(moved); err == nil {
		t.Fatal("Base was moved onto a different L1")
	}
}

// An unadopted network is not bridgeable — the difference this register makes.
// Before it, a bridge trusted a config file and nothing said the estate had
// sanctioned it.
func TestAnUnadoptedNetworkMayNotHoldValue(t *testing.T) {
	g := NewRegistry()
	if g.MayHold(id(42)) {
		t.Fatal("value may cross to a network with no record at all")
	}

	// Positive control: an adopted, attested one may.
	mustAdopt(t, g, ethereum)
	if !g.MayHold(ethereum.Key()) {
		t.Fatal("an attested network may not hold value, so the check refuses everything")
	}
}

// Bitcoin has nowhere to put a gateway, so the custody address is the gateway.
// Adoption does not care; it is the same record with a different client.
func TestAChainWithNoContractsIsStillAdoptable(t *testing.T) {
	g := NewRegistry()
	if err := g.Adopt(bitcoin); err != nil {
		t.Fatalf("bitcoin: %v", err)
	}
	got, _ := g.Get(bitcoin.Key())
	if got.Holding != Address {
		t.Fatalf("holding is %s", got.Holding)
	}
	if !g.MayHold(bitcoin.Key()) {
		t.Fatal("an attested UTXO chain may not hold value")
	}
}

func mustAdopt(t *testing.T, g *Registry, rs ...Record) {
	t.Helper()
	for _, r := range rs {
		if err := g.Adopt(r); err != nil {
			t.Fatalf("adopt %d: %v", r.ChainID, err)
		}
	}
}
