// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
<<<<<<< HEAD
<<<<<<< HEAD
	"github.com/ava-labs/avalanchego/utils/set"
=======
>>>>>>> 23402731f (Add consensus tracers (#2237))
=======
	"github.com/ava-labs/avalanchego/utils/set"
>>>>>>> 483d9bd18 (Move bit sets to the set package (#2365))
)

var _ Consensus = (*tracedConsensus)(nil)

type tracedConsensus struct {
	Consensus
	tracer trace.Tracer
}

func Trace(consensus Consensus, tracer trace.Tracer) Consensus {
	return &tracedConsensus{
		Consensus: consensus,
		tracer:    tracer,
	}
}

func (c *tracedConsensus) Add(ctx context.Context, vtx Vertex) error {
	ctx, span := c.tracer.Start(ctx, "tracedConsensus.Add", oteltrace.WithAttributes(
		attribute.Stringer("vtxID", vtx.ID()),
	))
	defer span.End()

	return c.Consensus.Add(ctx, vtx)
}

func (c *tracedConsensus) RecordPoll(ctx context.Context, votes ids.UniqueBag) error {
<<<<<<< HEAD
<<<<<<< HEAD
	var allVotes set.Bits64
=======
	var allVotes ids.BitSet64
>>>>>>> 23402731f (Add consensus tracers (#2237))
=======
	var allVotes set.Bits64
>>>>>>> 483d9bd18 (Move bit sets to the set package (#2365))
	for _, vote := range votes {
		allVotes.Union(vote)
	}

	ctx, span := c.tracer.Start(ctx, "tracedConsensus.RecordPoll", oteltrace.WithAttributes(
		attribute.Int("numVotes", allVotes.Len()),
		attribute.Int("numVtxIDs", len(votes)),
	))
	defer span.End()

	return c.Consensus.RecordPoll(ctx, votes)
}