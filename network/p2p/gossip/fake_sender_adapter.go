// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"

	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/p2p"
)

// FakeSenderAdapter wraps core.FakeSender to implement ExtendedAppSender
type FakeSenderAdapter struct {
	*core.FakeSender
}

// Ensure FakeSenderAdapter implements ExtendedAppSender
var _ p2p.ExtendedAppSender = (*FakeSenderAdapter)(nil)

// SendAppGossip implements ExtendedAppSender by ignoring the nodeIDs parameter
func (f *FakeSenderAdapter) SendAppGossip(ctx context.Context, _ set.Set[ids.NodeID], appGossipBytes []byte) error {
	return f.FakeSender.SendAppGossip(ctx, appGossipBytes)
}

// SendAppGossipSpecific implements ExtendedAppSender by ignoring the nodeIDs parameter
func (f *FakeSenderAdapter) SendAppGossipSpecific(ctx context.Context, _ set.Set[ids.NodeID], appGossipBytes []byte) error {
	// Just delegate to SendAppGossip since FakeSender doesn't distinguish
	return f.FakeSender.SendAppGossip(ctx, appGossipBytes)
}

// SendAppRequest implements ExtendedAppSender by sending to the first node in the set
func (f *FakeSenderAdapter) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	// FakeSender expects a single nodeID, so we'll use the first one or EmptyNodeID
	nodeID := ids.EmptyNodeID
	if nodeIDs.Len() > 0 {
		for id := range nodeIDs {
			nodeID = id
			break
		}
	}
	return f.FakeSender.SendAppRequest(ctx, nodeID, requestID, appRequestBytes)
}

// SendCrossChainAppRequest implements ExtendedAppSender
func (f *FakeSenderAdapter) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	// FakeSender doesn't support cross-chain, just no-op
	return nil
}

// SendCrossChainAppResponse implements ExtendedAppSender
func (f *FakeSenderAdapter) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	// FakeSender doesn't support cross-chain, just no-op
	return nil
}

// SendCrossChainAppError implements ExtendedAppSender
func (f *FakeSenderAdapter) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	// FakeSender doesn't support cross-chain, just no-op
	return nil
}