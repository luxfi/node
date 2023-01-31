<<<<<<< HEAD
<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
=======
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
>>>>>>> 34554f662 (Update LICENSE)
>>>>>>> c5eafdb72 (Update LICENSE)
// See the file LICENSE for licensing terms.

package lux

import (
	"context"

	"go.uber.org/zap"

<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
	"github.com/luxdefi/luxd/snow/engine/lux/vertex"
=======
<<<<<<< HEAD:snow/engine/avalanche/voter.go
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/consensus/snowstorm"
	"github.com/luxdefi/node/snow/engine/avalanche/vertex"
	"github.com/luxdefi/node/utils/set"
=======
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/snow/consensus/snowstorm"
	"github.com/luxdefi/luxd/snow/engine/lux/vertex"
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/voter.go
>>>>>>> 53a8245a8 (Update consensus)
)

// Voter records chits received from [vdr] once its dependencies are met.
type voter struct {
	t         *Transitive
	vdr       ids.NodeID
	requestID uint32
	response  []ids.ID
<<<<<<< HEAD
	deps      ids.Set
}

func (v *voter) Dependencies() ids.Set { return v.deps }
=======
	deps      set.Set[ids.ID]
}

<<<<<<< HEAD
<<<<<<< HEAD
func (v *voter) Dependencies() set.Set[ids.ID] {
=======
func (v *voter) Dependencies() ids.Set {
>>>>>>> 55bd9343c (Add EmptyLines linter (#2233))
=======
func (v *voter) Dependencies() set.Set[ids.ID] {
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
	return v.deps
}
>>>>>>> 53a8245a8 (Update consensus)

// Mark that a dependency has been met.
func (v *voter) Fulfill(ctx context.Context, id ids.ID) {
	v.deps.Remove(id)
	v.Update(ctx)
}

// Abandon this attempt to record chits.
<<<<<<< HEAD
func (v *voter) Abandon(ctx context.Context, id ids.ID) { v.Fulfill(ctx, id) }
=======
func (v *voter) Abandon(ctx context.Context, id ids.ID) {
	v.Fulfill(ctx, id)
}
>>>>>>> 53a8245a8 (Update consensus)

func (v *voter) Update(ctx context.Context) {
	if v.deps.Len() != 0 || v.t.errs.Errored() {
		return
	}

	results := v.t.polls.Vote(v.requestID, v.vdr, v.response)
	if len(results) == 0 {
		return
	}
<<<<<<< HEAD
	for _, result := range results {
		_, err := v.bubbleVotes(result)
=======

	for _, result := range results {
<<<<<<< HEAD
<<<<<<< HEAD
=======
>>>>>>> 4cf818ef3 (Log poll responses before bubbling (#2357))
		result := result
		v.t.Ctx.Log.Debug("filtering poll results",
			zap.Stringer("result", &result),
		)

<<<<<<< HEAD
=======
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
>>>>>>> 4cf818ef3 (Log poll responses before bubbling (#2357))
		_, err := v.bubbleVotes(ctx, result)
>>>>>>> 53a8245a8 (Update consensus)
		if err != nil {
			v.t.errs.Add(err)
			return
		}
<<<<<<< HEAD
	}

	for _, result := range results {
		result := result
=======
>>>>>>> 53a8245a8 (Update consensus)

		v.t.Ctx.Log.Debug("finishing poll",
			zap.Stringer("result", &result),
		)
<<<<<<< HEAD
		if err := v.t.Consensus.RecordPoll(result); err != nil {
=======
		if err := v.t.Consensus.RecordPoll(ctx, result); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
			v.t.errs.Add(err)
			return
		}
	}

	orphans := v.t.Consensus.Orphans()
	txs := make([]snowstorm.Tx, 0, orphans.Len())
	for orphanID := range orphans {
<<<<<<< HEAD
		if tx, err := v.t.VM.GetTx(orphanID); err == nil {
=======
		if tx, err := v.t.VM.GetTx(ctx, orphanID); err == nil {
>>>>>>> 53a8245a8 (Update consensus)
			txs = append(txs, tx)
		} else {
			v.t.Ctx.Log.Warn("failed to fetch tx during attempted re-issuance",
				zap.Stringer("txID", orphanID),
				zap.Error(err),
			)
		}
	}
	if len(txs) > 0 {
		v.t.Ctx.Log.Debug("re-issuing transactions",
			zap.Int("numTxs", len(txs)),
		)
	}
	if _, err := v.t.batch(ctx, txs, batchOption{force: true}); err != nil {
		v.t.errs.Add(err)
		return
	}

	if v.t.Consensus.Quiesce() {
		v.t.Ctx.Log.Debug("lux engine can quiesce")
		return
	}

	v.t.Ctx.Log.Debug("lux engine can't quiesce")
	v.t.repoll(ctx)
}

<<<<<<< HEAD
func (v *voter) bubbleVotes(votes ids.UniqueBag) (ids.UniqueBag, error) {
	vertexHeap := vertex.NewHeap()
	for vote, set := range votes {
		vtx, err := v.t.Manager.GetVtx(vote)
=======
func (v *voter) bubbleVotes(ctx context.Context, votes ids.UniqueBag) (ids.UniqueBag, error) {
	vertexHeap := vertex.NewHeap()
	for vote, set := range votes {
		vtx, err := v.t.Manager.GetVtx(ctx, vote)
>>>>>>> 53a8245a8 (Update consensus)
		if err != nil {
			v.t.Ctx.Log.Debug("dropping vote(s)",
				zap.String("reason", "failed to fetch vertex"),
				zap.Stringer("voteID", vote),
				zap.Int("numVotes", set.Len()),
				zap.Error(err),
			)
			votes.RemoveSet(vote)
			continue
		}
		vertexHeap.Push(vtx)
	}

	for vertexHeap.Len() > 0 {
		vtx := vertexHeap.Pop()
		vtxID := vtx.ID()
		set := votes.GetSet(vtxID)
		status := vtx.Status()

		if !status.Fetched() {
			v.t.Ctx.Log.Debug("dropping vote(s)",
				zap.String("reason", "vertex unknown"),
				zap.Int("numVotes", set.Len()),
				zap.Stringer("vtxID", vtxID),
			)
			votes.RemoveSet(vtxID)
			continue
		}

		if status.Decided() {
			v.t.Ctx.Log.Verbo("dropping vote(s)",
				zap.String("reason", "vertex already decided"),
				zap.Int("numVotes", set.Len()),
				zap.Stringer("vtxID", vtxID),
				zap.Stringer("status", status),
			)

			votes.RemoveSet(vtxID)
			continue
		}

		if !v.t.Consensus.VertexIssued(vtx) {
			v.t.Ctx.Log.Verbo("bubbling vote(s)",
				zap.String("reason", "vertex not issued"),
				zap.Int("numVotes", set.Len()),
				zap.Stringer("vtxID", vtxID),
			)
			votes.RemoveSet(vtxID) // Remove votes for this vertex because it hasn't been issued

			parents, err := vtx.Parents()
			if err != nil {
				return votes, err
			}
			for _, parentVtx := range parents {
				votes.UnionSet(parentVtx.ID(), set)
				vertexHeap.Push(parentVtx)
			}
		}
	}

	return votes, nil
}
