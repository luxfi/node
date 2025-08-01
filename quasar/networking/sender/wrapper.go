// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"context"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils/set"
)

// wrapper wraps an ExternalSender to implement the Sender interface
type wrapper struct {
	ctx           *consensus.Context
	externalSender ExternalSender
	msgCreator    message.Creator
	subnetTracker subnets.Tracker
}

// New creates a new Sender that wraps an ExternalSender
func New(
	ctx *consensus.Context,
	msgCreator message.Creator,
	externalSender ExternalSender,
	subnetTracker subnets.Tracker,
) Sender {
	return &wrapper{
		ctx:            ctx,
		externalSender: externalSender,
		msgCreator:     msgCreator,
		subnetTracker:  subnetTracker,
	}
}

// SendGetStateSummaryFrontier implements Sender
func (w *wrapper) SendGetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) {
	msg, err := w.msgCreator.GetStateSummaryFrontier(w.ctx.ChainID, requestID, 0)
	if err != nil {
		w.ctx.Log.Error("failed to create GetStateSummaryFrontier message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendStateSummaryFrontier implements Sender
func (w *wrapper) SendStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) {
	msg, err := w.msgCreator.StateSummaryFrontier(w.ctx.ChainID, requestID, summary)
	if err != nil {
		w.ctx.Log.Error("failed to create StateSummaryFrontier message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendGetAcceptedStateSummary implements Sender
func (w *wrapper) SendGetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, heights []uint64) {
	msg, err := w.msgCreator.GetAcceptedStateSummary(w.ctx.ChainID, requestID, time.Minute, heights)
	if err != nil {
		w.ctx.Log.Error("failed to create GetAcceptedStateSummary message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendAcceptedStateSummary implements Sender
func (w *wrapper) SendAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) {
	msg, err := w.msgCreator.AcceptedStateSummary(w.ctx.ChainID, requestID, summaryIDs)
	if err != nil {
		w.ctx.Log.Error("failed to create AcceptedStateSummary message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendGetAcceptedFrontier implements Sender
func (w *wrapper) SendGetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) {
	msg, err := w.msgCreator.GetAcceptedFrontier(w.ctx.ChainID, requestID, 0)
	if err != nil {
		w.ctx.Log.Error("failed to create GetAcceptedFrontier message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendAcceptedFrontier implements Sender
func (w *wrapper) SendAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	msg, err := w.msgCreator.AcceptedFrontier(w.ctx.ChainID, requestID, containerID)
	if err != nil {
		w.ctx.Log.Error("failed to create AcceptedFrontier message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendGetAccepted implements Sender
func (w *wrapper) SendGetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) {
	msg, err := w.msgCreator.GetAccepted(w.ctx.ChainID, requestID, time.Minute, containerIDs)
	if err != nil {
		w.ctx.Log.Error("failed to create GetAccepted message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendAccepted implements Sender
func (w *wrapper) SendAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) {
	msg, err := w.msgCreator.Accepted(w.ctx.ChainID, requestID, containerIDs)
	if err != nil {
		w.ctx.Log.Error("failed to create Accepted message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendGet implements Sender
func (w *wrapper) SendGet(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	msg, err := w.msgCreator.Get(w.ctx.ChainID, requestID, time.Minute, containerID)
	if err != nil {
		w.ctx.Log.Error("failed to create Get message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendGetAncestors implements Sender
func (w *wrapper) SendGetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	msg, err := w.msgCreator.GetAncestors(w.ctx.ChainID, requestID, time.Minute, containerID, 0) // TODO: make engineType configurable
	if err != nil {
		w.ctx.Log.Error("failed to create GetAncestors message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendPut implements Sender
func (w *wrapper) SendPut(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) {
	msg, err := w.msgCreator.Put(w.ctx.ChainID, requestID, container)
	if err != nil {
		w.ctx.Log.Error("failed to create Put message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendAncestors implements Sender
func (w *wrapper) SendAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) {
	msg, err := w.msgCreator.Ancestors(w.ctx.ChainID, requestID, containers)
	if err != nil {
		w.ctx.Log.Error("failed to create Ancestors message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendPushQuery implements Sender
func (w *wrapper) SendPushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte, requestedHeight uint64) {
	msg, err := w.msgCreator.PushQuery(w.ctx.ChainID, requestID, time.Minute, container, requestedHeight)
	if err != nil {
		w.ctx.Log.Error("failed to create PushQuery message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendPullQuery implements Sender
func (w *wrapper) SendPullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID, requestedHeight uint64) {
	msg, err := w.msgCreator.PullQuery(w.ctx.ChainID, requestID, time.Minute, containerID, requestedHeight)
	if err != nil {
		w.ctx.Log.Error("failed to create PullQuery message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendChits implements Sender
func (w *wrapper) SendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID) {
	// TODO: Get proper requestedHeight from context
	requestedHeight := uint64(0)
	msg, err := w.msgCreator.Chits(w.ctx.ChainID, requestID, preferredID, preferredIDAtHeight, acceptedID, requestedHeight)
	if err != nil {
		w.ctx.Log.Error("failed to create Chits message", "error", err)
		return
	}
	w.externalSender.Send(msg, nodeID)
}

// SendGossip implements Sender
func (w *wrapper) SendGossip(ctx context.Context, container []byte) {
	msg, err := w.msgCreator.Put(w.ctx.ChainID, 0, container) // TODO: use proper gossip message when available
	if err != nil {
		w.ctx.Log.Error("failed to create gossip message", "error", err)
		return
	}
	
	// TODO: Implement proper peer sampling when available
	// For now, the network will handle gossip distribution
	w.externalSender.Send(msg)
}

// SendAppRequest implements Sender
func (w *wrapper) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, message []byte) error {
	msg, err := w.msgCreator.AppRequest(w.ctx.ChainID, requestID, 0, message)
	if err != nil {
		return err
	}
	nodeIDSlice := nodeIDs.List()
	w.externalSender.Send(msg, nodeIDSlice...)
	return nil
}

// SendAppResponse implements Sender
func (w *wrapper) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, message []byte) error {
	msg, err := w.msgCreator.AppResponse(w.ctx.ChainID, requestID, message)
	if err != nil {
		return err
	}
	w.externalSender.Send(msg, nodeID)
	return nil
}

// SendAppGossip implements Sender
func (w *wrapper) SendAppGossip(ctx context.Context, message []byte) error {
	msg, err := w.msgCreator.AppGossip(w.ctx.ChainID, message)
	if err != nil {
		return err
	}
	
	// TODO: Implement proper peer sampling when available
	// For now, the network will handle gossip distribution
	w.externalSender.Send(msg)
	return nil
}

// SendCrossChainAppRequest implements Sender
func (w *wrapper) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, message []byte) error {
	// TODO: Implement cross-chain messaging
	return nil
}

// SendCrossChainAppResponse implements Sender
func (w *wrapper) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, message []byte) error {
	// TODO: Implement cross-chain messaging
	return nil
}