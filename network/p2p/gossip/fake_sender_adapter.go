// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"

	enginecore "github.com/luxfi/consensus/engine/core"
	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/p2p"
)

// FakeSenderAdapter wraps enginecore.FakeSender to implement ExtendedAppSender
type FakeSenderAdapter struct {
	*enginecore.FakeSender
}

// Ensure FakeSenderAdapter implements ExtendedAppSender
var _ p2p.ExtendedAppSender = (*FakeSenderAdapter)(nil)

// SendAppGossip implements ExtendedAppSender
func (f *FakeSenderAdapter) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	return f.FakeSender.SendAppGossip(ctx, nodeIDs, appGossipBytes)
}

// SendAppGossipSpecific implements ExtendedAppSender
func (f *FakeSenderAdapter) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// Just delegate to SendAppGossip since FakeSender doesn't distinguish
	return f.FakeSender.SendAppGossip(ctx, nodeIDs, appGossipBytes)
}

// SendAppRequest implements ExtendedAppSender
func (f *FakeSenderAdapter) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	return f.FakeSender.SendAppRequest(ctx, nodeIDs, requestID, appRequestBytes)
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

// SendAppResponse implements ExtendedAppSender
func (f *FakeSenderAdapter) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	return f.FakeSender.SendAppResponse(ctx, nodeID, requestID, appResponseBytes)
}

// SendAppError implements ExtendedAppSender
func (f *FakeSenderAdapter) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	// FakeSender doesn't have SendAppError, just no-op
	return nil
}

// SendCrossChainAppError implements ExtendedAppSender
func (f *FakeSenderAdapter) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	// FakeSender doesn't support cross-chain, just no-op
	return nil
}