// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"

	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/ids"
)

// TestAppSender implements ExtendedAppSender for testing
type TestAppSender struct {
	SentAppGossip            chan []byte
	SentAppRequest           chan []byte
	SentCrossChainAppRequest chan []byte
	SentAppResponse          chan []byte
	SentAppError             chan error
}

func (f *TestAppSender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	if f.SentAppRequest != nil {
		f.SentAppRequest <- appRequestBytes
	}
	return nil
}

func (f *TestAppSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	if f.SentAppResponse != nil {
		f.SentAppResponse <- appResponseBytes
	}
	return nil
}

func (f *TestAppSender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	return nil
}

func (f *TestAppSender) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	if f.SentAppGossip != nil {
		f.SentAppGossip <- appGossipBytes
	}
	return nil
}

func (f *TestAppSender) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	if f.SentAppGossip != nil {
		f.SentAppGossip <- appGossipBytes
	}
	return nil
}

func (f *TestAppSender) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	if f.SentCrossChainAppRequest != nil {
		f.SentCrossChainAppRequest <- appRequestBytes
	}
	return nil
}

func (f *TestAppSender) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	return nil
}

func (f *TestAppSender) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	return nil
}

// SenderTest implements ExtendedAppSender for testing with configurable behavior
type SenderTest struct {
	T *testing.T

	CantSendAppGossip, CantSendAppGossipSpecific,
	CantSendAppRequest, CantSendAppResponse, CantSendAppError,
	CantSendCrossChainAppRequest, CantSendCrossChainAppResponse, CantSendCrossChainAppError bool

	SendAppGossipF             func(context.Context, set.Set[ids.NodeID], []byte) error
	SendAppGossipSpecificF     func(context.Context, set.Set[ids.NodeID], []byte) error
	SendAppRequestF            func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	SendAppResponseF           func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppErrorF              func(context.Context, ids.NodeID, uint32, int32, string) error
	SendCrossChainAppRequestF  func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppResponseF func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppErrorF    func(context.Context, ids.ID, uint32, int32, string) error
}

func (s *SenderTest) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	if s.SendAppRequestF != nil {
		return s.SendAppRequestF(ctx, nodeIDs, requestID, appRequestBytes)
	}
	if s.CantSendAppRequest && s.T != nil {
		s.T.Fatal("unexpected SendAppRequest")
	}
	return nil
}

func (s *SenderTest) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	if s.SendAppResponseF != nil {
		return s.SendAppResponseF(ctx, nodeID, requestID, appResponseBytes)
	}
	if s.CantSendAppResponse && s.T != nil {
		s.T.Fatal("unexpected SendAppResponse")
	}
	return nil
}

func (s *SenderTest) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendAppErrorF != nil {
		return s.SendAppErrorF(ctx, nodeID, requestID, errorCode, errorMessage)
	}
	if s.CantSendAppError && s.T != nil {
		s.T.Fatal("unexpected SendAppError")
	}
	return nil
}

func (s *SenderTest) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	if s.SendAppGossipF != nil {
		return s.SendAppGossipF(ctx, nodeIDs, appGossipBytes)
	}
	if s.CantSendAppGossip && s.T != nil {
		s.T.Fatal("unexpected SendAppGossip")
	}
	return nil
}

func (s *SenderTest) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	if s.SendAppGossipSpecificF != nil {
		return s.SendAppGossipSpecificF(ctx, nodeIDs, appGossipBytes)
	}
	if s.CantSendAppGossipSpecific && s.T != nil {
		s.T.Fatal("unexpected SendAppGossipSpecific")
	}
	return nil
}

func (s *SenderTest) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	if s.SendCrossChainAppRequestF != nil {
		return s.SendCrossChainAppRequestF(ctx, chainID, requestID, appRequestBytes)
	}
	if s.CantSendCrossChainAppRequest && s.T != nil {
		s.T.Fatal("unexpected SendCrossChainAppRequest")
	}
	return nil
}

func (s *SenderTest) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	if s.SendCrossChainAppResponseF != nil {
		return s.SendCrossChainAppResponseF(ctx, chainID, requestID, appResponseBytes)
	}
	if s.CantSendCrossChainAppResponse && s.T != nil {
		s.T.Fatal("unexpected SendCrossChainAppResponse")
	}
	return nil
}

func (s *SenderTest) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendCrossChainAppErrorF != nil {
		return s.SendCrossChainAppErrorF(ctx, chainID, requestID, errorCode, errorMessage)
	}
	if s.CantSendCrossChainAppError && s.T != nil {
		s.T.Fatal("unexpected SendCrossChainAppError")
	}
	return nil
}