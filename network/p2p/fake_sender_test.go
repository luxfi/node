// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"time"

	"github.com/luxfi/ids"
)

// FakeSender is a test implementation
type FakeSender struct {
	SendAppRequestF            func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppResponseF           func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppGossipF             func(context.Context, ids.NodeID, []byte) error
	SendAppErrorF              func(context.Context, ids.NodeID, uint32, int32, string) error
	SendCrossChainAppRequestF  func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppResponseF func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppErrorF    func(context.Context, ids.ID, uint32, int32, string) error

	// Channels for test assertions
	SentAppRequest           chan []byte
	SentAppResponse          chan []byte
	SentAppGossip            chan []byte
	SentCrossChainAppRequest chan []byte
}

func (f *FakeSender) SendAppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request []byte) error {
	if f.SentAppRequest != nil {
		select {
		case f.SentAppRequest <- request:
		default:
		}
	}
	if f.SendAppRequestF != nil {
		return f.SendAppRequestF(ctx, nodeID, requestID, request)
	}
	return nil
}

func (f *FakeSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	if f.SentAppResponse != nil {
		select {
		case f.SentAppResponse <- response:
		default:
		}
	}
	if f.SendAppResponseF != nil {
		return f.SendAppResponseF(ctx, nodeID, requestID, response)
	}
	return nil
}

func (f *FakeSender) SendAppGossip(ctx context.Context, nodeID ids.NodeID, gossip []byte) error {
	if f.SentAppGossip != nil {
		select {
		case f.SentAppGossip <- gossip:
		default:
		}
	}
	if f.SendAppGossipF != nil {
		return f.SendAppGossipF(ctx, nodeID, gossip)
	}
	return nil
}

func (f *FakeSender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	if f.SendAppErrorF != nil {
		return f.SendAppErrorF(ctx, nodeID, requestID, errorCode, errorMessage)
	}
	return nil
}

func (f *FakeSender) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, request []byte) error {
	if f.SentCrossChainAppRequest != nil {
		select {
		case f.SentCrossChainAppRequest <- request:
		default:
		}
	}
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

func (f *FakeSender) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	if f.SendCrossChainAppErrorF != nil {
		return f.SendCrossChainAppErrorF(ctx, chainID, requestID, errorCode, errorMessage)
	}
	return nil
}

// SenderTest is a test implementation with similar methods
type SenderTest struct {
	SendAppRequestF            func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppResponseF           func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppGossipF             func(context.Context, ids.NodeID, []byte) error
	SendAppErrorF              func(context.Context, ids.NodeID, uint32, int32, string) error
	SendCrossChainAppRequestF  func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppResponseF func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppErrorF    func(context.Context, ids.ID, uint32, int32, string) error
}

func (s *SenderTest) SendAppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request []byte) error {
	if s.SendAppRequestF != nil {
		return s.SendAppRequestF(ctx, nodeID, requestID, request)
	}
	return nil
}

func (s *SenderTest) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	if s.SendAppResponseF != nil {
		return s.SendAppResponseF(ctx, nodeID, requestID, response)
	}
	return nil
}

func (s *SenderTest) SendAppGossip(ctx context.Context, nodeID ids.NodeID, gossip []byte) error {
	if s.SendAppGossipF != nil {
		return s.SendAppGossipF(ctx, nodeID, gossip)
	}
	return nil
}

func (s *SenderTest) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendAppErrorF != nil {
		return s.SendAppErrorF(ctx, nodeID, requestID, errorCode, errorMessage)
	}
	return nil
}

func (s *SenderTest) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, request []byte) error {
	if s.SendCrossChainAppRequestF != nil {
		return s.SendCrossChainAppRequestF(ctx, chainID, requestID, request)
	}
	return nil
}

func (s *SenderTest) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	if s.SendCrossChainAppResponseF != nil {
		return s.SendCrossChainAppResponseF(ctx, chainID, requestID, response)
	}
	return nil
}

func (s *SenderTest) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendCrossChainAppErrorF != nil {
		return s.SendCrossChainAppErrorF(ctx, chainID, requestID, errorCode, errorMessage)
	}
	return nil
}