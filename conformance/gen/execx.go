// SPDX-License-Identifier: BSD-3-Clause-Eco

// Executing an X-chain transaction, in Go, against a chain that holds nothing.
//
// The state here is the Go node's OWN X-chain state — `xstate.New` over an
// in-memory database, read through the X-chain's own parser. Nothing about it
// is a stand-in: it is the type the running node executes against, which is the
// only reason its verdict is worth comparing to anyone else's.
//
// It holds no UTXO the corpus spends and no asset the corpus names, and it is
// meant to. This is exactly the arrangement `execp.go` uses for the P-chain,
// and for the same reason: a funded state would have to be built three times,
// once per language, and the differential would then be measuring three state
// builders instead of three chains. An empty chain is a state all three can
// stand up identically.
//
// What survives an empty chain is the verdict CLASS, and that is the shape a
// fork has. Every X vector that gets this far names an input, and the two
// implementations must agree that the input is not there — that they refuse for
// the LEDGER reason rather than for an authorisation reason, a fee reason, or
// not at all. A chain that answered OK here would be one that never looked the
// input up.
package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/versiondb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	xconfig "github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/vms/xvm/fxs"
	xstate "github.com/luxfi/node/vms/xvm/state"
	xtxs "github.com/luxfi/node/vms/xvm/txs"
	xexecutor "github.com/luxfi/node/vms/xvm/txs/executor"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/chains/atomic"
)

// xChainOnce builds the empty X-chain once; every vector is executed on a fresh
// diff over it, so no vector can see another's writes.
var (
	xChainOnce sync.Once
	xChainErr  error
	xChainSt   xstate.State
	xChainBk   *xexecutor.Backend
)

func buildXChain() {
	reg := metric.NewRegistry()
	st, err := xstate.New(versiondb.New(memdb.New()), xParser, reg, false)
	if err != nil {
		xChainErr = fmt.Errorf("state: %w", err)
		return
	}

	// The same three feature extensions, in the same order, with the same
	// index that the syntactic verifier is given. Two verifiers of one chain
	// reading two fx tables would be two chains.
	bk := xBackend()
	bk.SharedMemory = emptyPeer{}

	xChainSt = st
	xChainBk = bk
}

// emptyPeer is a shared area that holds nothing.
//
// An import whose base inputs are on this chain would reach for the peer's
// UTXOs; there are none, and saying so is the honest answer for a chain that
// has never been handed anything. It is the shared-area half of the empty
// ledger, and the Rust evaluator is given the same.
type emptyPeer struct{}

func (emptyPeer) Get(_ ids.ID, keys [][]byte) ([][]byte, error) {
	return nil, fmt.Errorf("shared memory holds no utxo for %d keys", len(keys))
}

func (emptyPeer) Apply(map[ids.ID]interface{}, ...interface{}) error {
	return nil
}

// execXTx runs the X-chain's semantic pass and then its executor over a fresh
// diff on the empty chain, and reports the verdict class Go produced.
//
// Both passes, in that order, because that is the order a block runs them in
// (`block/executor.Manager.VerifyTx`): semantic verification asks whether the
// transaction is allowed and execution applies it. A vector that passed the
// first and failed the second would be hidden by running only one.
func execXTx(tx *xtxs.Tx) (string, string) {
	xChainOnce.Do(buildXChain)
	if xChainErr != nil {
		return VInternal, trim(xChainErr.Error())
	}

	diff, err := xstate.NewDiffOn(xChainSt)
	if err != nil {
		return VInternal, trim(err.Error())
	}

	sem := &xexecutor.SemanticVerifier{Backend: xChainBk, State: diff, Tx: tx}
	if err := tx.Unsigned.Visit(sem); err != nil {
		return classify(err), trim(root(err).Error())
	}

	exe := &xexecutor.Executor{
		State:          diff,
		Tx:             tx,
		AtomicRequests: map[ids.ID]*atomic.Requests{},
	}
	if err := tx.Unsigned.Visit(exe); err != nil {
		return classify(err), trim(root(err).Error())
	}
	return VOK, "executed on the empty chain"
}

// xBackend is the one X-chain backend both passes read: one chain identity, one
// fx table, one fee schedule. The syntactic verifier is built on it too, so a
// vector cannot be judged syntactically under one wiring and semantically under
// another.
func xBackend() *xexecutor.Backend {
	return &xexecutor.Backend{
		Ctx:     context.Background(),
		Runtime: xrt(),
		// The three feature extensions the X-chain runs, in their canonical
		// order, and the index that says which family lives where. Without them
		// the verifier cannot resolve a mint output and answers "unknown
		// feature extension" — which would be the evaluator's missing wiring
		// reported as the chain's verdict.
		Fxs: []*fxs.ParsedFx{
			{ID: id(1), Fx: &secp256k1fx.Fx{}},
			{ID: id(2), Fx: &nftfx.Fx{}},
			{ID: id(3), Fx: &propertyfx.Fx{}},
		},
		FxIndex: xFxIndex(),
		// The corpus's X vectors are built fee-neutral — inputs equal outputs —
		// so the differential measures the chain's rules and not three fee
		// schedules. A non-zero schedule here would reject every vector before
		// any rule was reached.
		Config:       &xconfig.Config{},
		FeeAssetID:   id(50),
		XChainID:     id(2),
		Bootstrapped: true,
		Log:          log.Noop(),
	}
}
