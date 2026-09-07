// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

// What each node will admit to. Run before anything is deployed, because a
// chain that cannot include a transaction cannot host an order book, and
// finding that out from a failed swap would say the swap was wrong.

import (
	"fmt"
	"math/big"
	"time"

	"github.com/luxfi/geth/common"
)

// Probe is one node's answer to "are you there, whose money is here, and do
// you build blocks".
type Probe struct {
	Node     *Node
	ChainID  *big.Int
	Height0  *big.Int
	Height1  *big.Int
	Funded   *Account
	Balance  *big.Int
	Advances bool   // did the height move on its own
	Includes bool   // did a submitted transaction reach a receipt
	Note     string // why not, when not
}

func (n *Node) Probe(cands []Account, settle time.Duration) *Probe {
	p := &Probe{Node: n}

	id, err := n.Quantity("eth_chainId")
	if err != nil {
		p.Note = "eth_chainId: " + err.Error()
		return p
	}
	p.ChainID = id
	n.ChainID = id

	if h, err := n.BlockNumber(); err == nil {
		p.Height0 = h
	}

	// Whose key do we hold here. The first candidate with a balance that can
	// pay for a transaction is the one this chain gets driven by.
	for i := range cands {
		b, err := n.Balance(cands[i].Addr)
		if err != nil || b.Sign() == 0 {
			continue
		}
		if p.Funded == nil || b.Cmp(p.Balance) > 0 {
			a := cands[i]
			p.Funded, p.Balance = &a, b
		}
	}
	if p.Funded == nil {
		p.Note = "no candidate key holds a balance on this chain"
	}

	// Does the chain move without being pushed. Asked before pushing, so a
	// chain that only advances because we sent something is distinguishable
	// from one that is producing on its own.
	time.Sleep(settle)
	if h, err := n.BlockNumber(); err == nil {
		p.Height1 = h
		p.Advances = p.Height0 != nil && h.Cmp(p.Height0) > 0
	}

	if p.Funded == nil {
		return p
	}

	// The decisive question: will a well-formed, funded, self-paying transfer
	// be executed. Value zero to self, so nothing is spent but gas and nothing
	// about the chain's balances is disturbed by the probe.
	//
	// Execution is judged by the sender's nonce, not by a receipt. One of the
	// three implementations serves no receipts at all, and a probe that asked
	// for one would report that chain as dead when it is mining perfectly well.
	// A nonce that moved is the EVM's own statement that the transaction ran.
	before, err := n.Nonce(p.Funded.Addr)
	if err != nil {
		p.Note = "nonce: " + err.Error()
		return p
	}
	tx, err := SignNonce(*p.Funded, n, before, &p.Funded.Addr, big.NewInt(0), nil, 21000)
	if err != nil {
		p.Note = "sign: " + err.Error()
		return p
	}
	if _, err := n.SendRaw(tx); err != nil {
		p.Note = "eth_sendRawTransaction: " + err.Error()
		return p
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		got, err := n.Nonce(p.Funded.Addr)
		if err == nil && got > before {
			p.Includes = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !p.Includes {
		p.Note = "accepted into the pool but never executed within 60s"
	}
	return p
}

func (p *Probe) Line() string {
	who, bal := "-", "-"
	if p.Funded != nil {
		who = p.Funded.Path + " " + p.Funded.Addr.Hex()
		bal = fmt.Sprintf("%s", weth(p.Balance))
	}
	h := "-"
	if p.Height1 != nil {
		h = p.Height1.String()
	}
	return fmt.Sprintf("%-5s chainid=%-8v height=%-8s advances=%-5v includes=%-5v funded=%s bal=%s %s",
		p.Node.Lang, p.ChainID, h, p.Advances, p.Includes, who, bal, p.Note)
}

func weth(v *big.Int) string {
	if v == nil {
		return "-"
	}
	e := new(big.Int).Div(v, big.NewInt(1e18))
	return e.String() + "e18"
}

var _ = common.Address{}
