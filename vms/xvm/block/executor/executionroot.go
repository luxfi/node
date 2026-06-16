// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"runtime"
	"sync"

	"github.com/luxfi/crypto/merkle"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/xvm/state"
	"github.com/luxfi/node/vms/xvm/state/xvmroot"
	"github.com/luxfi/node/vms/xvm/txs"
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
// because both observe the same ordered tx set. The UTXO/asset families come
// from postBlockUTXOLeaves / postBlockAssetLeaves; see those for the
// snapshot-producer seam.
func BlockExecutionRoot(
	parentExecutionRoot ids.ID,
	blkTxs []*txs.Tx,
	postState state.Chain,
	height uint64,
) ids.ID {
	leafTxs := txLeaves(blkTxs)
	utxos := postBlockUTXOLeaves(postState)
	assets := postBlockAssetLeaves(postState)
	executionRoot, _, _, _ := xvmExecutionRoot([xvmroot.Size]byte(parentExecutionRoot), utxos, assets, leafTxs, height)
	return ids.ID(executionRoot)
}

// postBlockUTXOLeaves and postBlockAssetLeaves project the post-block state into
// the xvm execution_root's UTXO and asset family leaves — the occupied set in
// canonical ascending slot order, as the kernel snapshot lays it out.
//
// SEAM (gated off; not yet wired): the canonical mapping from the xvm executor's
// codec/output-interface UTXO and asset types to the accelerator's flat
// state-snapshot leaf fields (UTXOLeaf.OwnerRoot / AmountHi / Threshold,
// AssetLeaf.TotalSupply / MintAuthorityRoot / FreezeFlag / Denomination) is owned
// by the GPU state-snapshot producer — the same component that feeds xvmroot's
// KAT — and enumerating the full occupied set needs a state iterator the xvm
// state layer does not yet expose. Until that producer lands, these families are
// empty, so the execution_root commits to the parent root, the empty UTXO/asset
// roots, the tx family (fully derived from the block), and the height. This is
// consensus-inert today: the gate defaults to the never sentinel on every
// published network (see upgrade.MerkleRootNeverActivate), so the path is never
// taken until a real upgrade both sets a height AND wires the snapshot producer.
// The builder and verifier call this identical projection, so the stamped and
// verified roots always agree.
func postBlockUTXOLeaves(state.Chain) []xvmroot.UTXOLeaf { return nil }

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
