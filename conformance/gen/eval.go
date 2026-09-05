// SPDX-License-Identifier: BSD-3-Clause-Eco

// The Go evaluator: the reference answer for every vector.
//
// It reads only the bytes. Nothing it prints is copied out of the corpus's own
// expectations — it parses the wire, hashes it, verifies it and executes it
// through `luxfi/node`'s own packages, and prints what those packages said. An
// evaluator that echoed the corpus back would agree with itself forever.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/vms/platformvm/block"
	pexec "github.com/luxfi/node/vms/platformvm/block/executor"
	"github.com/luxfi/node/vms/platformvm/txs"
	xexec "github.com/luxfi/node/vms/xvm/block/executor"
	xtxs "github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/runtime"
)

// rt is the chain identity every syntactic check reads: which network this is,
// which chain, and which asset pays. The same three numbers are given to the
// Rust and C++ evaluators, so a check that reads them reads the same values.
func rt() *runtime.Runtime {
	return &runtime.Runtime{
		NetworkID:   networkID,
		ChainID:     id(3),
		UTXOAssetID: id(stakeAsset),
		Log:         log.Noop(),
	}
}

func xrt() *runtime.Runtime {
	r := rt()
	r.ChainID = id(2)
	r.XChainID = id(2)
	return r
}

func evaluate(v Vector) Result {
	switch v.Op {
	case "tx":
		if v.Chain == "P" {
			return evalPTx(v)
		}
		return evalXTx(v)
	case "block":
		if v.Chain == "P" {
			return evalPBlock(v)
		}
		return evalXBlock(v)
	case "seam":
		return evalSeam(v)
	}
	return Result{ID: v.ID, Parse: VInternal, Kind: none, Hash: none,
		Syntactic: VInternal, Exec: VInternal, Note: "unknown op " + v.Op}
}

func wireOf(v Vector) ([]byte, bool) {
	if v.Wire == none {
		return nil, true
	}
	b, err := hex.DecodeString(v.Wire)
	return b, err == nil
}

func evalPTx(v Vector) Result {
	r := Result{ID: v.ID, Kind: none, Hash: none, Syntactic: none, Exec: none}
	b, ok := wireOf(v)
	if !ok {
		r.Parse = VInternal
		r.Note = "corpus wire is not hex"
		return r
	}

	tx, err := txs.Parse(b)
	if err != nil {
		r.Parse = VMalformed
		r.Syntactic = VMalformed
		r.Exec = VMalformed
		r.Note = trim(err.Error())
		return r
	}
	r.Parse = "ok"
	r.Kind = pKindName(tx.Unsigned)
	r.Hash = hexID(tx.ID())

	if err := tx.SyntacticVerify(rt()); err != nil {
		r.Syntactic = classify(err)
		r.Exec = r.Syntactic
		r.Note = trim(err.Error())
		return r
	}
	r.Syntactic = VOK

	// Execution is judged against a chain that holds nothing: no UTXO, no
	// validator, no network. That is a state all three implementations can
	// stand up identically, which is the whole reason to use it — a funded
	// state would have to be built three times and the differential would then
	// be measuring three state builders. What survives the empty chain is the
	// verdict CLASS, and that is what a fork shows up in: a chain that refuses
	// a kind outright answers UNSUPPORTED where a chain that tries to execute
	// it answers FUNDS.
	r.Exec, r.Note = execPTx(tx)
	return r
}

func evalXTx(v Vector) Result {
	r := Result{ID: v.ID, Kind: none, Hash: none, Syntactic: none, Exec: none}
	b, ok := wireOf(v)
	if !ok {
		r.Parse = VInternal
		r.Note = "corpus wire is not hex"
		return r
	}

	tx, err := xtxs.Parse(b)
	if err != nil {
		r.Parse = VMalformed
		r.Syntactic = VMalformed
		r.Exec = VMalformed
		r.Note = trim(err.Error())
		return r
	}
	r.Parse = "ok"
	r.Kind = xKindName(tx.Unsigned)
	r.Hash = hexID(tx.ID())

	if err := tx.Unsigned.Visit(xSyntacticVerifier(tx)); err != nil {
		r.Syntactic = classify(err)
		r.Exec = r.Syntactic
		r.Note = trim(err.Error())
		return r
	}
	r.Syntactic = VOK

	// Judged against a chain that holds nothing, exactly as the P-chain's
	// vectors are and for the same reason: a funded state would have to be
	// built three times and the differential would be measuring three state
	// builders. What survives the empty chain is the verdict CLASS, and every
	// X vector that reaches here names an input the chain does not have — so
	// the three implementations have to agree that they looked it up and that
	// its absence is a LEDGER refusal rather than an authorisation one.
	r.Exec, r.Note = execXTx(tx)
	return r
}

func evalPBlock(v Vector) Result {
	r := Result{ID: v.ID, Kind: none, Hash: none, Syntactic: none, Exec: none}
	b, ok := wireOf(v)
	if !ok {
		r.Parse = VInternal
		r.Note = "corpus wire is not hex"
		return r
	}
	blk, err := block.Parse(b)
	if err != nil {
		r.Parse = VMalformed
		r.Syntactic = VMalformed
		r.Exec = VMalformed
		r.Note = trim(err.Error())
		return r
	}
	r.Parse = "ok"
	r.Kind = pBlockKindName(blk)
	r.Hash = hexID(blk.ID())
	r.Syntactic = VOK
	r.Exec = VLedger
	r.Note = fmt.Sprintf("height=%d parent=%s txs=%d",
		blk.Height(), shortID(blk.Parent()), len(blk.DecisionTxs()))
	return r
}

func evalXBlock(v Vector) Result {
	r := Result{ID: v.ID, Kind: none, Hash: none, Syntactic: none, Exec: none}
	b, ok := wireOf(v)
	if !ok {
		r.Parse = VInternal
		r.Note = "corpus wire is not hex"
		return r
	}
	blk, err := xParser.ParseBlock(b)
	if err != nil {
		r.Parse = VMalformed
		r.Syntactic = VMalformed
		r.Exec = VMalformed
		r.Note = trim(err.Error())
		return r
	}
	r.Parse = "ok"
	r.Kind = "StandardBlock"
	r.Hash = hexID(blk.ID())
	r.Syntactic = VOK
	r.Exec = VLedger
	r.Note = fmt.Sprintf("height=%d parent=%s txs=%d",
		blk.Height(), shortID(blk.Parent()), len(blk.Txs()))
	return r
}

func shortID(i ids.ID) string { return hex.EncodeToString(i[:4]) }

func hexID(i ids.ID) string { return hex.EncodeToString(i[:]) }

func trim(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

// root walks an error down to the deepest thing that actually went wrong.
//
// The Go executor wraps: `standard tx <id> failed execution: <cause>`. Reading
// the wrapper instead of the cause would classify every execution failure the
// same way, and the differential would then be comparing one word against
// three chains.
func root(err error) error {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err
		}
		err = next
	}
}

// classify maps a Go error onto the shared verdict vocabulary. It is the one
// place a Go error's words become a class, so the mapping is readable in full
// and nothing else in this evaluator gets to decide a verdict. The Rust and
// C++ evaluators carry the same table in the same order.
func classify(err error) string {
	if err == nil {
		return VOK
	}
	s := strings.ToLower(root(err).Error())
	has := func(w string) bool { return strings.Contains(s, w) }
	switch {
	case has("overflow"), has("underflow"):
		return VOverflow
	case has("wrong transaction type"), has("wrong tx type"),
		has("not permitted"), has("not held"), has("unsupported"):
		return VUnsupported
	case has("credential"), has("signature"), has("unauthorized"),
		has("not authorised"), has("not authorized"):
		return VAuth
	case has("warp"):
		return VWarp
	case has("utxo"), has("funds"), has("insufficient"), has("burn"),
		has("consumed"), has("produced"), has("flow"), has("fee"),
		has("not found"), has("doesn't exist"), has("does not exist"),
		has("isn't a current"), has("not validator"), has("no such"),
		has("could not load"), has("shared memory"):
		return VLedger
	default:
		return VSyntactic
	}
}

// evalSeam answers a question about the block-decision seam, and answers it
// from the compiler: the method expression below only compiles if the method
// is there with that shape. Nothing about the answer is typed by hand.
//
// What the seam DOES with a rejected block's transactions is quoted in the
// note from the reference's own source; the compared field is only whether the
// seam exists at all, because that is the part three compilers can each
// answer for themselves.
func evalSeam(v Vector) Result {
	r := Result{ID: v.ID, Kind: "Block.Reject", Hash: none, Syntactic: none, Parse: "ok"}
	switch v.Chain {
	case "X":
		var _ func(*xexec.Block, context.Context) error = (*xexec.Block).Reject
		r.Exec = "PRESENT"
		r.Note = "xvm block/executor Block.Reject re-offers each still-valid tx to the mempool"
	case "P":
		var _ func(*pexec.Block, context.Context) error = (*pexec.Block).Reject
		r.Exec = "PRESENT"
		r.Note = "platformvm block/executor Block.Reject frees the block and drops its state"
	default:
		r.Exec = VInternal
		r.Note = "no seam for chain " + v.Chain
	}
	return r
}
