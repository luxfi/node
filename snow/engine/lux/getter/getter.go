<<<<<<< HEAD
// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
=======
// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
>>>>>>> 53a8245a8 (Update consensus)
// See the file LICENSE for licensing terms.

package getter

import (
	"context"
	"time"

	"go.uber.org/zap"

<<<<<<< HEAD
=======
<<<<<<< HEAD:snow/engine/avalanche/getter/getter.go
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/engine/avalanche/vertex"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
=======
>>>>>>> 53a8245a8 (Update consensus)
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/message"
	"github.com/luxdefi/luxd/snow/choices"
	"github.com/luxdefi/luxd/snow/consensus/lux"
	"github.com/luxdefi/luxd/snow/engine/lux/vertex"
	"github.com/luxdefi/luxd/snow/engine/common"
	"github.com/luxdefi/luxd/utils/constants"
	"github.com/luxdefi/luxd/utils/logging"
	"github.com/luxdefi/luxd/utils/metric"
	"github.com/luxdefi/luxd/utils/wrappers"
<<<<<<< HEAD
=======
>>>>>>> 04d685aa2 (Update consensus):snow/engine/lux/getter/getter.go
>>>>>>> 53a8245a8 (Update consensus)
)

// Get requests are always served, regardless node state (bootstrapping or normal operations).
var _ common.AllGetsServer = (*getter)(nil)

func New(storage vertex.Storage, commonCfg common.Config) (common.AllGetsServer, error) {
	gh := &getter{
		storage: storage,
		sender:  commonCfg.Sender,
		cfg:     commonCfg,
		log:     commonCfg.Ctx.Log,
	}

	var err error
	gh.getAncestorsVtxs, err = metric.NewAverager(
		"bs",
		"get_ancestors_vtxs",
		"vertices fetched in a call to GetAncestors",
		commonCfg.Ctx.Registerer,
	)
	return gh, err
}

type getter struct {
	storage vertex.Storage
	sender  common.Sender
	cfg     common.Config

	log              logging.Logger
	getAncestorsVtxs metric.Averager
}

func (gh *getter) GetStateSummaryFrontier(_ context.Context, nodeID ids.NodeID, requestID uint32) error {
	gh.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
<<<<<<< HEAD
		zap.Stringer("messageOp", message.GetStateSummaryFrontier),
=======
		zap.Stringer("messageOp", message.GetStateSummaryFrontierOp),
>>>>>>> 53a8245a8 (Update consensus)
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (gh *getter) GetAcceptedStateSummary(_ context.Context, nodeID ids.NodeID, requestID uint32, _ []uint64) error {
	gh.log.Debug("dropping request",
		zap.String("reason", "unhandled by this gear"),
<<<<<<< HEAD
		zap.Stringer("messageOp", message.GetAcceptedStateSummary),
=======
		zap.Stringer("messageOp", message.GetAcceptedStateSummaryOp),
>>>>>>> 53a8245a8 (Update consensus)
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
	)
	return nil
}

func (gh *getter) GetAcceptedFrontier(ctx context.Context, validatorID ids.NodeID, requestID uint32) error {
<<<<<<< HEAD
	acceptedFrontier := gh.storage.Edge()
=======
	acceptedFrontier := gh.storage.Edge(ctx)
>>>>>>> 53a8245a8 (Update consensus)
	gh.sender.SendAcceptedFrontier(ctx, validatorID, requestID, acceptedFrontier)
	return nil
}

func (gh *getter) GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error {
	acceptedVtxIDs := make([]ids.ID, 0, len(containerIDs))
	for _, vtxID := range containerIDs {
<<<<<<< HEAD
		if vtx, err := gh.storage.GetVtx(vtxID); err == nil && vtx.Status() == choices.Accepted {
=======
		if vtx, err := gh.storage.GetVtx(ctx, vtxID); err == nil && vtx.Status() == choices.Accepted {
>>>>>>> 53a8245a8 (Update consensus)
			acceptedVtxIDs = append(acceptedVtxIDs, vtxID)
		}
	}
	gh.sender.SendAccepted(ctx, nodeID, requestID, acceptedVtxIDs)
	return nil
}

func (gh *getter) GetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, vtxID ids.ID) error {
	startTime := time.Now()
	gh.log.Verbo("called GetAncestors",
		zap.Stringer("nodeID", nodeID),
		zap.Uint32("requestID", requestID),
		zap.Stringer("vtxID", vtxID),
	)
<<<<<<< HEAD
	vertex, err := gh.storage.GetVtx(vtxID)
=======
	vertex, err := gh.storage.GetVtx(ctx, vtxID)
>>>>>>> 53a8245a8 (Update consensus)
	if err != nil || vertex.Status() == choices.Unknown {
		gh.log.Verbo("dropping getAncestors")
		return nil // Don't have the requested vertex. Drop message.
	}

	queue := make([]lux.Vertex, 1, gh.cfg.AncestorsMaxContainersSent) // for BFS
	queue[0] = vertex
	ancestorsBytesLen := 0                                                 // length, in bytes, of vertex and its ancestors
	ancestorsBytes := make([][]byte, 0, gh.cfg.AncestorsMaxContainersSent) // vertex and its ancestors in BFS order
<<<<<<< HEAD
	visited := ids.Set{}                                                   // IDs of vertices that have been in queue before
=======
	visited := set.Set[ids.ID]{}                                           // IDs of vertices that have been in queue before
>>>>>>> 53a8245a8 (Update consensus)
	visited.Add(vertex.ID())

	for len(ancestorsBytes) < gh.cfg.AncestorsMaxContainersSent && len(queue) > 0 && time.Since(startTime) < gh.cfg.MaxTimeGetAncestors {
		var vtx lux.Vertex
		vtx, queue = queue[0], queue[1:] // pop
		vtxBytes := vtx.Bytes()
		// Ensure response size isn't too large. Include wrappers.IntLen because the size of the message
		// is included with each container, and the size is repr. by an int.
		if newLen := wrappers.IntLen + ancestorsBytesLen + len(vtxBytes); newLen < constants.MaxContainersLen {
			ancestorsBytes = append(ancestorsBytes, vtxBytes)
			ancestorsBytesLen = newLen
		} else { // reached maximum response size
			break
		}
		parents, err := vtx.Parents()
		if err != nil {
			return err
		}
		for _, parent := range parents {
			if parent.Status() == choices.Unknown { // Don't have this vertex;ignore
				continue
			}
			if parentID := parent.ID(); !visited.Contains(parentID) { // If already visited, ignore
				queue = append(queue, parent)
				visited.Add(parentID)
			}
		}
	}

	gh.getAncestorsVtxs.Observe(float64(len(ancestorsBytes)))
	gh.sender.SendAncestors(ctx, nodeID, requestID, ancestorsBytes)
	return nil
}

func (gh *getter) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, vtxID ids.ID) error {
	// If this engine has access to the requested vertex, provide it
<<<<<<< HEAD
	if vtx, err := gh.storage.GetVtx(vtxID); err == nil {
=======
	if vtx, err := gh.storage.GetVtx(ctx, vtxID); err == nil {
>>>>>>> 53a8245a8 (Update consensus)
		gh.sender.SendPut(ctx, nodeID, requestID, vtx.Bytes())
	}
	return nil
}
