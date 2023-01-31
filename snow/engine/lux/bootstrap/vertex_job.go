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

package bootstrap

import (
<<<<<<< HEAD
=======
	"context"
>>>>>>> 53a8245a8 (Update consensus)
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

<<<<<<< HEAD
=======
<<<<<<< HEAD:snow/engine/avalanche/bootstrap/vertex_job.go
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/snow/consensus/avalanche"
	"github.com/luxdefi/node/snow/engine/avalanche/vertex"
	"github.com/luxdefi/node/snow/engine/common/queue"
	"github.com/luxdefi/node/utils/logging"
	"github.com/luxdefi/node/utils/set"
=======
>>>>>>> 53a8245a8 (Update consensus)
	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/snow/choices"
	"github.com/luxdefi/node/snow/consensus/lux"
	"github.com/luxdefi/node/snow/engine/lux/vertex"
	"github.com/luxdefi/node/snow/engine/common/queue"
	"github.com/luxdefi/node/utils/logging"
<<<<<<< HEAD
=======
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/bootstrap/vertex_job.go
>>>>>>> 53a8245a8 (Update consensus)
)

var errMissingVtxDependenciesOnAccept = errors.New("attempting to execute blocked vertex")

type vtxParser struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	manager                 vertex.Manager
}

<<<<<<< HEAD
func (p *vtxParser) Parse(vtxBytes []byte) (queue.Job, error) {
	vtx, err := p.manager.ParseVtx(vtxBytes)
=======
func (p *vtxParser) Parse(ctx context.Context, vtxBytes []byte) (queue.Job, error) {
	vtx, err := p.manager.ParseVtx(ctx, vtxBytes)
>>>>>>> 53a8245a8 (Update consensus)
	if err != nil {
		return nil, err
	}
	return &vertexJob{
		log:         p.log,
		numAccepted: p.numAccepted,
		numDropped:  p.numDropped,
		vtx:         vtx,
	}, nil
}

type vertexJob struct {
	log                     logging.Logger
	numAccepted, numDropped prometheus.Counter
	vtx                     lux.Vertex
}

<<<<<<< HEAD
func (v *vertexJob) ID() ids.ID { return v.vtx.ID() }

func (v *vertexJob) MissingDependencies() (ids.Set, error) {
	missing := ids.Set{}
=======
func (v *vertexJob) ID() ids.ID {
	return v.vtx.ID()
}

<<<<<<< HEAD
<<<<<<< HEAD
func (v *vertexJob) MissingDependencies(context.Context) (set.Set[ids.ID], error) {
	missing := set.Set[ids.ID]{}
=======
func (v *vertexJob) MissingDependencies(context.Context) (ids.Set, error) {
	missing := ids.Set{}
>>>>>>> 5be92660b (Pass message context through the VM interface (#2219))
=======
func (v *vertexJob) MissingDependencies(context.Context) (set.Set[ids.ID], error) {
	missing := set.Set[ids.ID]{}
>>>>>>> 87ce2da8a (Replace type specific sets with a generic implementation (#1861))
>>>>>>> 53a8245a8 (Update consensus)
	parents, err := v.vtx.Parents()
	if err != nil {
		return missing, err
	}
	for _, parent := range parents {
		if parent.Status() != choices.Accepted {
			missing.Add(parent.ID())
		}
	}
	return missing, nil
}

// Returns true if this vertex job has at least 1 missing dependency
<<<<<<< HEAD
func (v *vertexJob) HasMissingDependencies() (bool, error) {
=======
func (v *vertexJob) HasMissingDependencies(context.Context) (bool, error) {
>>>>>>> 53a8245a8 (Update consensus)
	parents, err := v.vtx.Parents()
	if err != nil {
		return false, err
	}
	for _, parent := range parents {
		if parent.Status() != choices.Accepted {
			return true, nil
		}
	}
	return false, nil
}

<<<<<<< HEAD
func (v *vertexJob) Execute() error {
	hasMissingDependencies, err := v.HasMissingDependencies()
=======
func (v *vertexJob) Execute(ctx context.Context) error {
	hasMissingDependencies, err := v.HasMissingDependencies(ctx)
>>>>>>> 53a8245a8 (Update consensus)
	if err != nil {
		return err
	}
	if hasMissingDependencies {
		v.numDropped.Inc()
		return errMissingVtxDependenciesOnAccept
	}
<<<<<<< HEAD
	txs, err := v.vtx.Txs()
=======
	txs, err := v.vtx.Txs(ctx)
>>>>>>> 53a8245a8 (Update consensus)
	if err != nil {
		return err
	}
	for _, tx := range txs {
		if tx.Status() != choices.Accepted {
			v.numDropped.Inc()
			v.log.Warn("attempting to execute vertex with non-accepted transactions")
			return nil
		}
	}
	status := v.vtx.Status()
	switch status {
	case choices.Unknown, choices.Rejected:
		v.numDropped.Inc()
		return fmt.Errorf("attempting to execute vertex with status %s", status)
	case choices.Processing:
		v.numAccepted.Inc()
		v.log.Trace("accepting vertex in bootstrapping",
			zap.Stringer("vtxID", v.vtx.ID()),
		)
<<<<<<< HEAD
		if err := v.vtx.Accept(); err != nil {
=======
		if err := v.vtx.Accept(ctx); err != nil {
>>>>>>> 53a8245a8 (Update consensus)
			return fmt.Errorf("failed to accept vertex in bootstrapping: %w", err)
		}
	}
	return nil
}

<<<<<<< HEAD
func (v *vertexJob) Bytes() []byte { return v.vtx.Bytes() }
=======
func (v *vertexJob) Bytes() []byte {
	return v.vtx.Bytes()
}
>>>>>>> 53a8245a8 (Update consensus)
