// SPDX-License-Identifier: BSD-3-Clause-Eco

// The Go Z-chain's answers.
//
// It reads only the bytes. Every verdict below comes out of a method on the
// reference — ParseBlock, ValidateBasic, Verify, Reject — so what this prints
// is what the Go Z-chain said, never a rule restated beside it.
//
// The chain each vector meets is stood up fresh and holds nothing: no spent
// note, no output, no accepted block. That is the same arrangement the P and X
// vectors are judged under and for the same reason — a funded chain would have
// to be built twice and the differential would then be measuring two state
// builders instead of two chains.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/luxfi/chains/zkvm"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/log"
	"github.com/luxfi/runtime"
	vmcore "github.com/luxfi/vm"
)

// zvm brings up a Z-chain on an empty store.
//
// The configuration is the DEFAULT one — genesis carries no ZConfig — because
// the Z-chain's default profile is the thing most worth comparing: it is
// strict-PQ, so every classical proof system is refused by name and only
// STARK/FRI reaches a verifier. A permissive chain would answer differently on
// three of these vectors, which is exactly the fork this vector set is for.
func zvm() (*zkvm.VM, error) {
	logger := log.NewNoOpLogger()
	vm := &zkvm.VM{}
	genesis, err := json.Marshal(map[string]any{"timestamp": 0})
	if err != nil {
		return nil, err
	}
	err = vm.Initialize(context.Background(), vmcore.Init{
		Runtime:  &runtime.Runtime{ChainID: zChainID, NetworkID: zNetworkID, Log: logger},
		DB:       memdb.New(),
		ToEngine: make(chan vmcore.Message, 8),
		Log:      logger,
		Genesis:  genesis,
	})
	if err != nil {
		return nil, err
	}
	return vm, nil
}

// zKindName names what a block carries. The Z-chain has one block kind, so the
// kind field would carry no information at all if it named that; what a Z block
// is decided about is its transactions, and their types come off the parsed
// frame rather than out of any table here.
//
// A block of one kind repeated is named once with its count, so a
// hundred-transaction vector does not print a hundred names; a mixed block
// spells every one out, because which types a block mixes is the thing the
// field is there to compare.
func zKindName(txs []*zkvm.Transaction) string {
	if len(txs) == 0 {
		return "Empty"
	}
	names := make([]string, 0, len(txs))
	uniform := true
	for _, tx := range txs {
		n := zTxKindName(tx.Type)
		if len(names) > 0 && n != names[0] {
			uniform = false
		}
		names = append(names, n)
	}
	if uniform {
		if len(names) == 1 {
			return names[0]
		}
		return fmt.Sprintf("%sx%d", names[0], len(names))
	}
	return strings.Join(names, "+")
}

func zTxKindName(t zkvm.TransactionType) string {
	switch t {
	case zkvm.TransactionTypeTransfer:
		return "Transfer"
	case zkvm.TransactionTypeMint:
		return "Mint"
	case zkvm.TransactionTypeBurn:
		return "Burn"
	case zkvm.TransactionTypeShield:
		return "Shield"
	case zkvm.TransactionTypeUnshield:
		return "Unshield"
	}
	return "unknown"
}

func evalZBlock(v Vector) Result {
	r := Result{ID: v.ID, Kind: none, Hash: none, Syntactic: none, Exec: none}
	b, ok := wireOf(v)
	if !ok {
		r.Parse = VInternal
		r.Note = "corpus wire is not hex"
		return r
	}

	vm, err := zvm()
	if err != nil {
		r.Parse = VInternal
		r.Note = "cannot stand up a Z-chain: " + trim(err.Error())
		return r
	}
	defer vm.Shutdown(context.Background())

	parsed, err := vm.ParseBlock(context.Background(), b)
	if err != nil {
		r.Parse = VMalformed
		r.Syntactic = VMalformed
		r.Exec = VMalformed
		r.Note = trim(err.Error())
		return r
	}
	blk, ok := parsed.(*zkvm.Block)
	if !ok {
		r.Parse = VInternal
		r.Note = "the reference returned a block this evaluator does not know"
		return r
	}
	r.Parse = "ok"
	r.Kind = zKindName(blk.Txs)
	r.Hash = hexID(blk.ID())

	// The shape check, per transaction, from the reference's own method. It is
	// the Z-chain's analogue of a syntactic pass: everything about a
	// transaction that can be decided without asking the chain anything.
	for _, tx := range blk.Txs {
		if err := tx.ValidateBasic(); err != nil {
			r.Syntactic = classify(err)
			r.Exec = r.Syntactic
			r.Note = trim(err.Error())
			return r
		}
	}
	r.Syntactic = VOK

	// Verify is this node's whole verdict on the block: the transaction cap,
	// the clock, the block-level spent set, the one admission predicate over
	// every transaction — which is where the strict-PQ profile gate fires —
	// and the state root.
	if err := blk.Verify(context.Background()); err != nil {
		r.Exec = classify(err)
		r.Note = trim(err.Error())
		return r
	}
	r.Exec = VOK
	r.Note = fmt.Sprintf("height=%d txs=%d", blk.BlockHeight, len(blk.Txs))
	return r
}

// evalZSeam answers from the compiler, like the P and X seam probes: the
// method expression below only compiles if the reference's block really has a
// reject with that shape.
func evalZSeam(v Vector) Result {
	var _ func(*zkvm.Block, context.Context) error = (*zkvm.Block).Reject
	return Result{
		ID: v.ID, Parse: "ok", Kind: "Block.Reject", Hash: none, Syntactic: none,
		Exec: "PRESENT",
		Note: "zkvm Block.Reject drops the block from the chain index and returns its " +
			"transactions to the mempool",
	}
}
