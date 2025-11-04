// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/ids"
)

// FakeSender is a test implementation of AppSender
type FakeSender struct {
	SendAppRequestF            func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	SendAppResponseF           func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppErrorF              func(context.Context, ids.NodeID, uint32, int32, string) error
	SendAppGossipF             func(context.Context, set.Set[ids.NodeID], []byte) error
	SendAppGossipSpecificF     func(context.Context, set.Set[ids.NodeID], []byte) error
	SendCrossChainAppRequestF  func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppResponseF func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppErrorF    func(context.Context, ids.ID, uint32, int32, string) error

	// Test channels
	SentAppRequest           chan []byte
	SentAppResponse          chan []byte
	SentAppGossip            chan []byte
	SentCrossChainAppRequest chan []byte
}

func (f *FakeSender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
	if f.SendAppRequestF != nil {
		return f.SendAppRequestF(ctx, nodeIDs, requestID, request)
	}
	if f.SentAppRequest != nil {
		f.SentAppRequest <- request
	}
	return nil
}

func (f *FakeSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	if f.SendAppResponseF != nil {
		return f.SendAppResponseF(ctx, nodeID, requestID, response)
	}
	if f.SentAppResponse != nil {
		f.SentAppResponse <- response
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
	if f.SentAppGossip != nil {
		f.SentAppGossip <- gossip
	}
	return nil
}

func (f *FakeSender) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], gossip []byte) error {
	if f.SendAppGossipSpecificF != nil {
		return f.SendAppGossipSpecificF(ctx, nodeIDs, gossip)
	}
	return f.SendAppGossip(ctx, nodeIDs, gossip)
}

func (f *FakeSender) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, request []byte) error {
	if f.SendCrossChainAppRequestF != nil {
		return f.SendCrossChainAppRequestF(ctx, chainID, requestID, request)
	}
	if f.SentCrossChainAppRequest != nil {
		f.SentCrossChainAppRequest <- request
	}
	return nil
}

func (f *FakeSender) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	if f.SendCrossChainAppResponseF != nil {
		return f.SendCrossChainAppResponseF(ctx, chainID, requestID, response)
	}
	// Just return nil for testing
	return nil
}

func (f *FakeSender) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	if f.SendCrossChainAppErrorF != nil {
		return f.SendCrossChainAppErrorF(ctx, chainID, requestID, errorCode, errorMessage)
	}
	return nil
}
