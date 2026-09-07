// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/luxfi/crypto/merkle"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/xvm/state"
	"github.com/luxfi/node/vms/xvm/state/xvmroot"
	"github.com/luxfi/node/vms/xvm/txs"
	lux "github.com/luxfi/utxo"
)

// xvmExecutionRoot computes the xvm execution_root over a block's post-block
// state, byte-identical to xvmroot.ExecutionRoot, with the three family subtrees
// (utxo / asset / tx) built concurrently and each family's leaf digests hashed
// in parallel across a GOMAXPROCS-bounded worker pool. It is the single root
// computation shared by the builder (which stamps the result into the block) and
// the executor (which recomputes it to verify the block's stamped root). Having
// exactly one implementation means the producer and the verifier can never
// disagree on the rule.
//
// Byte-identity to the serial xvmroot.ExecutionRoot is structural, not
// best-effort: the canonical leaf ORDER (occupied, ascending slot index) and the
// canonical COMBINE (RFC-6962 lone-right Merkle fold via merkle.Root, then the
// fixed-shape keccak compose) are unchanged — only the per-leaf keccak hashing
// and the three independent family folds run on separate goroutines. Each worker
// writes a disjoint, pre-indexed slot of its family's leaf slice, so the slice
// fed to merkle.Root is identical to the one the serial path builds. A property
// test (TestParallelExecutionRootMatchesSerial) pins this over random state
// sizes including odd leaf counts.
func xvmExecutionRoot(
	parentExecutionRoot [xvmroot.Size]byte,
	utxos []xvmroot.UTXOLeaf,
	assets []xvmroot.AssetLeaf,
	leafTxs []xvmroot.TxLeaf,
	height uint64,
) (executionRoot, utxoRoot, assetRoot, txRoot [xvmroot.Size]byte) {
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); utxoRoot = parallelUTXORoot(utxos) }()
	go func() { defer wg.Done(); assetRoot = parallelAssetRoot(assets) }()
	go func() { defer wg.Done(); txRoot = parallelTxRoot(leafTxs) }()
	wg.Wait()

	executionRoot = xvmroot.Compose(parentExecutionRoot, utxoRoot, assetRoot, txRoot, height)
	return executionRoot, utxoRoot, assetRoot, txRoot
}

// parallelUTXORoot is the parallel peer of xvmroot.UTXORoot. It compacts the
// occupied slots to their canonical ascending leaf positions (serial, O(n) and
// cheap), then computes the family root with the parallel fold — byte-identical
// to xvmroot.UTXORoot's merkle.Root over the same compacted digests.
func parallelUTXORoot(utxos []xvmroot.UTXOLeaf) [xvmroot.Size]byte {
	idx := make([]int, 0, len(utxos))
	for i := range utxos {
		if utxos[i].Status&xvmroot.UTXOOccupied == 0 {
			continue
		}
		idx = append(idx, i)
	}
	return parallelMerkleRoot(len(idx), func(k int) [xvmroot.Size]byte {
		return xvmroot.UTXOLeafDigest(utxos[idx[k]], uint32(idx[k]))
	})
}

// parallelAssetRoot is the parallel peer of xvmroot.AssetRoot.
func parallelAssetRoot(assets []xvmroot.AssetLeaf) [xvmroot.Size]byte {
	idx := make([]int, 0, len(assets))
	for i := range assets {
		if assets[i].Occupied == 0 {
			continue
		}
		idx = append(idx, i)
	}
	return parallelMerkleRoot(len(idx), func(k int) [xvmroot.Size]byte {
		return xvmroot.AssetLeafDigest(assets[idx[k]], uint32(idx[k]))
	})
}

// parallelTxRoot is the parallel peer of xvmroot.TxRoot. Every tx is folded (no
// skip predicate), so leaf position equals slot index.
func parallelTxRoot(leafTxs []xvmroot.TxLeaf) [xvmroot.Size]byte {
	return parallelMerkleRoot(len(leafTxs), func(i int) [xvmroot.Size]byte {
		return xvmroot.TxLeafDigest(leafTxs[i], uint32(i))
	})
}

// parallelMerkleRoot computes the RFC-6962 tagged binary Merkle root over n leaf
// element-digests, where digest(k) yields the k-th leaf's element digest. It is
// byte-identical to merkle.Root(allDigests): it composes merkle's own exported
// LeafHash / NodeHash primitives (so the keccak/tagging spec is not duplicated)
// and follows the same level-by-level reduction with lone-right promotion — only
// the per-level loops run across a GOMAXPROCS-bounded worker pool. Both the
// element-digest stage and every internal merkle level are independent across
// their index range, so parallelizing the loops cannot change the bytes; the
// reduction shape is fixed by the leaf count alone.
//
//	n == 0 → merkle.EmptyRoot (keccak256(""))
//	n == 1 → merkle.LeafHash(digest(0))
//	n  > 1 → parallel level reduction
func parallelMerkleRoot(n int, digest func(k int) [xvmroot.Size]byte) [xvmroot.Size]byte {
	if n == 0 {
		return merkle.EmptyRoot()
	}

	// Level 0: tag each element digest. merkle.Root tags leaves[i] via
	// LeafHash; we compute the element digest and tag it in the same step.
	level := make([][xvmroot.Size]byte, n)
	parallelFor(n, func(i int) {
		level[i] = merkle.LeafHash(digest(i))
	})

	// Reduce level-by-level. parents = ceil(cnt/2); a lone right node on an odd
	// level is promoted unchanged — identical to merkle.Root.
	for len(level) > 1 {
		cnt := len(level)
		parents := (cnt + 1) / 2
		pairs := cnt / 2
		next := make([][xvmroot.Size]byte, parents)
		parallelFor(pairs, func(j int) {
			next[j] = merkle.NodeHash(level[2*j], level[2*j+1])
		})
		if cnt&1 == 1 {
			next[parents-1] = level[cnt-1]
		}
		level = next
	}
	return level[0]
}

// parallelFor invokes body(i) for every i in [0, n), spread over a
// GOMAXPROCS-bounded pool of worker goroutines partitioning the range into
// contiguous shards. Each i is handled exactly once by one worker, so a body
// that writes a disjoint slot per i needs no further synchronization. Below a
// keccak-tuned threshold it runs inline, where fork/join would cost more than
// the work.
func parallelFor(n int, body func(i int)) {
	if n == 0 {
		return
	}
	workers := runtime.GOMAXPROCS(0)
	// Below this many keccak invocations the fork/join overhead outweighs the
	// parallel hashing; run inline. Tuned to keccak cost, not block size.
	const inlineThreshold = 1024
	if workers <= 1 || n < inlineThreshold {
		for i := 0; i < n; i++ {
			body(i)
		}
		return
	}
	if workers > n {
		workers = n
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	// Contiguous shards: the first [rem] workers take one extra element so every
	// i in [0,n) is covered exactly once.
	base, rem := n/workers, n%workers
	start := 0
	for w := 0; w < workers; w++ {
		size := base
		if w < rem {
			size++
		}
		lo, hi := start, start+size
		start = hi
		go func() {
			defer wg.Done()
			for i := lo; i < hi; i++ {
				body(i)
			}
		}()
	}
	wg.Wait()
}

// BlockExecutionRoot resolves the canonical leaf projection for a block and
// returns the execution_root the block must carry at [height]. It is the bridge
// from a stateless block to the pure xvm root computation: the builder calls it
// to stamp the root and the executor calls it to verify the stamped root, so the
// produced root and the verified root are derived by exactly one code path and
// can never disagree.
//
// [parentExecutionRoot] is the parent block's MerkleRoot (the empty root for a
// genesis or pre-activation parent). [postState] is the post-block state (the
// applied state diff) the UTXO/asset families are projected from. The leaf
// projection is the canonical, deterministic, pure-function-of-the-block image
// of the post-block state the root commits to. The tx family is fully realized
// from the block's transactions (TxID = tx.ID(), every tx folded in block
// order, status = accepted); it is identical on the builder and the verifier
// because both observe the same ordered tx set. The UTXO family is enumerated
// from the post-block occupied UTXO set and projected by postBlockUTXOLeaves;
// the asset family is empty (see postBlockAssetLeaves for why xvm's UTXO-only
// executor state has no enumerable asset arena to project).
//
// It returns an error if the post-block state cannot be enumerated or an output
// carries an owner model with no canonical owner_root yet — a wrong root must
// never be stamped or accepted silently.
func BlockExecutionRoot(
	parentExecutionRoot ids.ID,
	blkTxs []*txs.Tx,
	postState state.Chain,
	height uint64,
) (ids.ID, error) {
	leafTxs := txLeaves(blkTxs)
	utxos, err := postBlockUTXOLeaves(postState)
	if err != nil {
		return ids.Empty, err
	}
	assets := postBlockAssetLeaves(postState)
	executionRoot, _, _, _ := xvmExecutionRoot([xvmroot.Size]byte(parentExecutionRoot), utxos, assets, leafTxs, height)
	return ids.ID(executionRoot), nil
}

// postBlockUTXOLeaves projects the post-block state's occupied UTXO set into the
// execution_root UTXO family leaves, in canonical ascending-UTXOID order — the
// deterministic full-set order state.Chain.UTXOs returns and the order xvmroot
// folds. Every enumerated UTXO is occupied (the iterator never returns a spent
// or removed UTXO), so each leaf carries Status = UTXOOccupied and its leaf
// index is its position in the ascending enumeration (0,1,2,…), matching the
// `index` the kernel binds into each occupied slot's leaf preimage.
//
// Each lux.UTXO is projected field-by-field onto the canonical UTXOLeaf the GPU
// state-snapshot producer must match (xvm_gpu_layout.hpp's UTXO struct):
//
//	UTXOID    = utxo.InputID()                  (keccak(tx_id ‖ output_index))
//	AssetID   = utxo.AssetID()
//	AmountLo  = output amount (uint64)          (0 for a non-value output)
//	AmountHi  = 0                               (no xvm output exceeds 2^64-1;
//	                                             Amt is a single uint64, so the
//	                                             high limb is always 0)
//	OwnerRoot = OwnerRoot(extractOwners(out))   (the canonical owner commitment;
//	                                             see ownerroot.go — this is the
//	                                             Go definition the GPU matches)
//	Locktime  = owner locktime                  (0 when the owner has none)
//	Threshold = owner threshold                 (the N-of-M spend parameter)
//	Status    = UTXOOccupied                    (the set is the occupied set)
//
// It is a pure, deterministic function of the post-block state: the builder and
// the verifier both call it over the same post-block state, so the stamped and
// verified UTXO roots always agree.
func postBlockUTXOLeaves(postState state.Chain) ([]xvmroot.UTXOLeaf, error) {
	utxos, err := postState.UTXOs(ids.Empty, 0) // 0 = unbounded: the full occupied set
	if err != nil {
		return nil, fmt.Errorf("enumerating post-block UTXO set: %w", err)
	}
	leaves := make([]xvmroot.UTXOLeaf, len(utxos))
	for i, utxo := range utxos {
		leaf, err := utxoLeaf(utxo)
		if err != nil {
			return nil, fmt.Errorf("projecting UTXO %s: %w", utxo.InputID(), err)
		}
		leaves[i] = leaf
	}
	return leaves, nil
}

// utxoLeaf projects one lux.UTXO onto its canonical UTXOLeaf field values. See
// postBlockUTXOLeaves for the field-by-field mapping; the owner_root derivation
// is defined in ownerroot.go.
func utxoLeaf(utxo *lux.UTXO) (xvmroot.UTXOLeaf, error) {
	o, ok := extractOwners(utxo.Out)
	if !ok {
		return xvmroot.UTXOLeaf{}, fmt.Errorf("%w: output type %T", errUnsupportedOwnerModel, utxo.Out)
	}

	var amount uint64
	if a, ok := utxo.Out.(lux.Amounter); ok {
		amount = a.Amount()
	}

	return xvmroot.UTXOLeaf{
		UTXOID:    ids.ID(utxo.InputID()),
		AssetID:   ids.ID(utxo.AssetID()),
		AmountLo:  amount,
		AmountHi:  0, // xvm amounts are uint64; the high limb is always zero.
		OwnerRoot: OwnerRoot(o),
		Locktime:  o.locktime,
		Threshold: o.threshold,
		Status:    xvmroot.UTXOOccupied,
	}, nil
}

// errUnsupportedOwnerModel is returned when a UTXO output carries an owner model
// the execution_root projection does not yet define a canonical owner_root for.
// It surfaces as a block-verification/build error rather than silently hashing
// an unknown output under an invented rule.
var errUnsupportedOwnerModel = errors.New("xvm execution_root: output owner model has no canonical owner_root")

// postBlockAssetLeaves projects the post-block asset family. It is intentionally
// EMPTY: xvm's executor state is UTXO-only and maintains no enumerable asset
// arena.
//
// The GPU substrate (xvm_gpu_layout.hpp) carries a dedicated Asset arena with
// total_supply / mint_authority_root / freeze_flag / denomination because it is
// a GPU-NATIVE state model. The xvm executor's authoritative state has no such
// structure: an asset exists only as the AssetID stamped on the UTXOs a
// CreateAssetTx produces (txs/executor/executor.go CreateAssetTx), and the
// asset's denomination/name live only in that creating transaction's history —
// there is no GetAsset, no asset index, and no per-asset total-supply or
// mint-authority record the state layer can enumerate.
//
// Projecting a faithful asset arena would therefore require INVENTING state the
// executor does not track (e.g. summing UTXO amounts per asset for
// total_supply, and deriving a mint_authority_root from the asset's mint
// outputs) — unverified semantics with no executor-side authority to validate
// against. That is deliberately NOT auto-invented here: it is a consensus-state
// decision flagged for human ratification (a future asset-arena state model
// would have to be added to the executor first, then projected). Until then the
// asset family is the empty set, so asset_root = keccak256("") and the
// execution_root commits to a faithful UTXO family, the tx family, the parent,
// and the height. The UTXOs themselves still carry their AssetID, so the asset
// binding is not lost — it is committed through the UTXO family.
func postBlockAssetLeaves(state.Chain) []xvmroot.AssetLeaf { return nil }

// txLeaves projects a block's transactions onto the canonical tx-family leaf
// layout, in block order (the consensus-fixed, deterministic ordering). Every
// tx is folded; a tx included in an accepted block has status = accepted, with
// the kernel-snapshot reject_reason / proof_digest / kind fields zero (an xvm tx
// in a built or verified block carries no rejection and no proof digest at this
// layer). This is a pure function of the block — builder and verifier produce
// the same leaves.
func txLeaves(blkTxs []*txs.Tx) []xvmroot.TxLeaf {
	const statusAccepted uint32 = 1
	leaves := make([]xvmroot.TxLeaf, len(blkTxs))
	for i, tx := range blkTxs {
		leaves[i] = xvmroot.TxLeaf{
			TxID:   ids.ID(tx.ID()),
			Status: statusAccepted,
		}
	}
	return leaves
}
