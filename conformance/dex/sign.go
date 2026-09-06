// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

// Signing. The C-chain's external format is Ethereum's, and RLP is the
// encoding that format is defined in — this is the one place in the tree where
// RLP is the right answer rather than ZAP, because the bytes here are the ones
// a foreign wallet must be able to produce.

import (
	"fmt"
	"math/big"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
)

// Sign builds and signs one legacy EIP-155 transaction. Legacy rather than
// dynamic-fee on purpose: it is the form all three implementations decode, and
// a differential whose transactions only one node can read measures the
// encoding, not the chain.
func Sign(a Account, n *Node, to *common.Address, value *big.Int, data []byte, gas uint64) ([]byte, error) {
	nonce, err := n.Nonce(a.Addr)
	if err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return SignNonce(a, n, nonce, to, value, data, gas)
}

// SignNonce is Sign with the nonce supplied, for a run that sends several
// transactions before any of them is mined.
func SignNonce(a Account, n *Node, nonce uint64, to *common.Address, value *big.Int, data []byte, gas uint64) ([]byte, error) {
	price, err := n.Quantity("eth_gasPrice")
	if err != nil {
		return nil, fmt.Errorf("gasPrice: %w", err)
	}
	// Bid well over the quote, for two separate reasons.
	//
	// A node that quotes zero still charges the base fee of the block it lands
	// in, so a bid at the quote can be priced out between the quote and the
	// block. And the Lux fee rule charges a block that arrives sooner than the
	// target rate an extra block cost, payable out of the tip: a transaction
	// bidding only the base fee leaves a tip of zero, the builder reports
	// "insufficient gas to cover the block cost", and the chain stalls until
	// enough time passes. Bidding four times the quote leaves a tip that pays
	// it, which is the difference between a chain that mines and one that looks
	// hung.
	price = new(big.Int).Mul(price, big.NewInt(4))
	floor := big.NewInt(200_000_000_000)
	if price.Cmp(floor) < 0 {
		price = floor
	}
	if value == nil {
		value = big.NewInt(0)
	}
	tx := types.NewTx(&types.LegacyTx{
		Nonce: nonce, To: to, Value: value, Gas: gas, GasPrice: price, Data: data,
	})
	signed, err := types.SignTx(tx, types.NewEIP155Signer(n.ChainID), a.Priv)
	if err != nil {
		return nil, err
	}
	return signed.MarshalBinary()
}
