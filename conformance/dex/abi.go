// SPDX-License-Identifier: BSD-3-Clause-Eco

package main

// Calldata, by hand. The encoding is four bytes of selector and a run of
// 32-byte words, and writing it out is shorter than the reflection needed to
// generate it — and leaves nothing between the harness and the bytes the three
// implementations disagree or agree about.

import (
	"math/big"

	"github.com/luxfi/crypto"
	"github.com/luxfi/geth/common"
)

// selector is the first four bytes of the keccak of the canonical signature.
func selector(sig string) []byte { return crypto.Keccak256([]byte(sig))[:4] }

// word left-pads a value into one 32-byte slot, which is how every static type
// smaller than a word is passed.
func word(v *big.Int) []byte { return common.LeftPadBytes(v.Bytes(), 32) }

func num(v uint64) []byte { return word(new(big.Int).SetUint64(v)) }

func boolWord(b bool) []byte {
	if b {
		return num(1)
	}
	return num(0)
}

func addrWord(a common.Address) []byte { return common.LeftPadBytes(a.Bytes(), 32) }

func call(sig string, args ...[]byte) []byte {
	out := selector(sig)
	for _, a := range args {
		out = append(out, a...)
	}
	return out
}

// deployData is the creation code: the contract's own bytecode followed by the
// constructor's arguments. The one dynamic argument is an address array, so the
// head carries its offset and the tail carries its length and elements.
func deployData(code []byte, traders []common.Address, base, quote uint64) []byte {
	head := append([]byte{}, code...)
	head = append(head, num(3*32)...) // offset of the array, past three head words
	head = append(head, num(base)...)
	head = append(head, num(quote)...)
	head = append(head, num(uint64(len(traders)))...)
	for _, t := range traders {
		head = append(head, addrWord(t)...)
	}
	return head
}
