// SPDX-License-Identifier: BSD-3-Clause-Eco

// The chain identity each evaluator was configured with, asked as a vector.
//
// Three of the chains hash something that is NOT on the wire into every id they
// derive: the Z-chain opens its block id with sha256(ChainID ‖ NetworkID), the
// F-chain does the same and binds its signing preimage to the chain id besides,
// and the Q-chain carries the pair in the block but refuses a block whose pair
// is not the one the node serves. So the identity is part of the corpus's
// contract, and an evaluator that picked its own numbers derives a different id
// for every well-formed vector on that chain.
//
// That is not a hypothetical. The first run of this differential disagreed on
// the id of all twenty-five Z vectors, and on none of the malformed ones, for
// exactly one reason: the Go evaluator was configured for chain 40 and the C++
// one for chain 4. Twenty-five identical-looking rows said "the two chains
// derive different ids", which is what a genuine hash fork looks like, and none
// of them said which number was wrong.
//
// One vector per affected chain fixes that. It carries no bytes: each
// evaluator prints the identity IT was built with, so a mismatch is one row
// naming both numbers, and the rows underneath can be read for what they
// actually are.
package main

import "fmt"

// identities is the contract, in one place. Everything that reads a chain
// identity in this generator reads it from here.
var identities = []struct {
	Chain   string
	VectorI string
	ChainID byte
	Network uint32
}{
	{"Q", "Q_CHAIN_IDENTITY", qChain, networkID},
	{"Z", "Z_CHAIN_IDENTITY", zChain, networkID},
	{"F", "F_CHAIN_IDENTITY", fChain, networkID},
}

func identityVectors() []Vector {
	v := make([]Vector, 0, len(identities))
	for _, in := range identities {
		v = append(v, Vector{ID: in.VectorI, Chain: in.Chain, Op: "identity", Wire: none})
	}
	return v
}

// evalIdentity answers with the numbers THIS evaluator holds, never with the
// ones the corpus asked about — a row that echoed the question would agree with
// every implementation, including one configured for another chain entirely.
func evalIdentity(v Vector) Result {
	for _, in := range identities {
		if in.Chain != v.Chain {
			continue
		}
		return Result{
			ID:        v.ID,
			Parse:     "ok",
			Kind:      "ChainIdentity",
			Hash:      hexID(id(in.ChainID)),
			Syntactic: fmt.Sprintf("network=%d", in.Network),
			Exec:      VOK,
			Note:      "the identity this evaluator derives ids under",
		}
	}
	return Result{ID: v.ID, Parse: VInternal, Kind: none, Hash: none,
		Syntactic: VInternal, Exec: VInternal, Note: "no identity for chain " + v.Chain}
}
