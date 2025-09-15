// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"

	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/p2p"
)

// FakeSender is a test implementation
type FakeSender struct {
	SendAppRequestF  func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	SendAppResponseF func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppErrorF    func(context.Context, ids.NodeID, uint32, int32, string) error
	SendAppGossipF   func(context.Context, set.Set[ids.NodeID], []byte) error
	SendCrossChainAppRequestF  func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppResponseF func(context.Context, ids.ID, uint32, []byte) error
	
	// Fields for test channels
	SentAppRequest  chan []byte
	SentAppResponse chan []byte
	SentAppGossip   chan []byte
}

func (f *FakeSender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
	if f.SendAppRequestF != nil {
		return f.SendAppRequestF(ctx, nodeIDs, requestID, request)
	}
	return nil
}

func (f *FakeSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	if f.SendAppResponseF != nil {
		return f.SendAppResponseF(ctx, nodeID, requestID, response)
	}
	return nil
}

func (f *FakeSender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	if f.SendAppErrorF != nil {
		return f.SendAppErrorF(ctx, nodeID, requestID, errorCode, errorMessage)
	}
	return nil
}

func (f *FakeSender) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], gossip []byte) error {
	if f.SendAppGossipF != nil {
		return f.SendAppGossipF(ctx, nodeIDs, gossip)
	}
	return nil
}

func (f *FakeSender) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], gossip []byte) error {
	// Just delegate to SendAppGossip for test purposes
	return f.SendAppGossip(ctx, nodeIDs, gossip)
}

func (f *FakeSender) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, request []byte) error {
	if f.SendCrossChainAppRequestF != nil {
		return f.SendCrossChainAppRequestF(ctx, chainID, requestID, request)
	}
	return nil
}

func (f *FakeSender) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	if f.SendCrossChainAppResponseF != nil {
		return f.SendCrossChainAppResponseF(ctx, chainID, requestID, response)
	}
	return nil
}

// FakeSenderAdapter wraps FakeSender to implement ExtendedAppSender
type FakeSenderAdapter struct {
	*FakeSender
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