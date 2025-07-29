// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"context"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/set"
)

// FakeSender is a test implementation with channels for sent messages
type FakeSender struct {
	SentAppGossip            chan []byte
	SentAppRequest           chan []byte
	SentAppResponse          chan []byte
	SentCrossChainAppRequest chan []byte
}

func (s *FakeSender) SendAppGossip(_ context.Context, _ SendConfig, appGossipBytes []byte) error {
	if s.SentAppGossip != nil {
		s.SentAppGossip <- appGossipBytes
	}
	return nil
}

func (s *FakeSender) SendAppRequest(_ context.Context, _ set.Set[ids.NodeID], _ uint32, appRequestBytes []byte) error {
	if s.SentAppRequest != nil {
		s.SentAppRequest <- appRequestBytes
	}
	return nil
}

func (s *FakeSender) SendAppResponse(_ context.Context, _ ids.NodeID, _ uint32, appResponseBytes []byte) error {
	if s.SentAppResponse != nil {
		s.SentAppResponse <- appResponseBytes
	}
	return nil
}

func (s *FakeSender) SendCrossChainAppRequest(_ context.Context, _ ids.ID, _ uint32, appRequestBytes []byte) error {
	if s.SentCrossChainAppRequest != nil {
		s.SentCrossChainAppRequest <- appRequestBytes
	}
	return nil
}

func (s *FakeSender) SendCrossChainAppResponse(_ context.Context, _ ids.ID, _ uint32, _ []byte) error {
	return nil
}

func (s *FakeSender) SendAppError(_ context.Context, _ ids.NodeID, _ uint32, _ int32, _ string) error {
	return nil
}

func (s *FakeSender) SendCrossChainAppError(_ context.Context, _ ids.ID, _ uint32, _ int32, _ string) error {
	return nil
}

// Implement remaining Sender interface methods
func (s *FakeSender) SendGetStateSummaryFrontier(_ context.Context, _ set.Set[ids.NodeID], _ uint32) {}
func (s *FakeSender) SendStateSummaryFrontier(_ context.Context, _ ids.NodeID, _ uint32, _ []byte) {}
func (s *FakeSender) SendGetAcceptedStateSummary(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []uint64) {}
func (s *FakeSender) SendAcceptedStateSummary(_ context.Context, _ ids.NodeID, _ uint32, _ []ids.ID) {}
func (s *FakeSender) SendGetAcceptedFrontier(_ context.Context, _ set.Set[ids.NodeID], _ uint32) {}
func (s *FakeSender) SendAcceptedFrontier(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID) {}
func (s *FakeSender) SendGetAccepted(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []ids.ID) {}
func (s *FakeSender) SendAccepted(_ context.Context, _ ids.NodeID, _ uint32, _ []ids.ID) {}
func (s *FakeSender) SendGet(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID) {}
func (s *FakeSender) SendGetAncestors(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID) {}
func (s *FakeSender) SendAncestors(_ context.Context, _ ids.NodeID, _ uint32, _ [][]byte) {}
func (s *FakeSender) SendPut(_ context.Context, _ ids.NodeID, _ uint32, _ []byte) {}
func (s *FakeSender) SendPushQuery(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []byte, _ uint64) {}
func (s *FakeSender) SendPullQuery(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ ids.ID, _ uint64) {}
func (s *FakeSender) SendChits(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID, _ ids.ID, _ ids.ID, _ uint64) {}

// SenderTest provides function-based test implementations
type SenderTest struct {
	SendAppRequestF           func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	SendAppResponseF          func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppGossipF            func(context.Context, SendConfig, []byte) error
	SendAppErrorF             func(context.Context, ids.NodeID, uint32, int32, string) error
	SendCrossChainAppRequestF func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppResponseF func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppErrorF   func(context.Context, ids.ID, uint32, int32, string) error
}

func (s *SenderTest) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	if s.SendAppRequestF != nil {
		return s.SendAppRequestF(ctx, nodeIDs, requestID, appRequestBytes)
	}
	return nil
}

func (s *SenderTest) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	if s.SendAppResponseF != nil {
		return s.SendAppResponseF(ctx, nodeID, requestID, appResponseBytes)
	}
	return nil
}

func (s *SenderTest) SendAppGossip(ctx context.Context, config SendConfig, appGossipBytes []byte) error {
	if s.SendAppGossipF != nil {
		return s.SendAppGossipF(ctx, config, appGossipBytes)
	}
	return nil
}

func (s *SenderTest) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	if s.SendCrossChainAppRequestF != nil {
		return s.SendCrossChainAppRequestF(ctx, chainID, requestID, appRequestBytes)
	}
	return nil
}

func (s *SenderTest) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	if s.SendCrossChainAppResponseF != nil {
		return s.SendCrossChainAppResponseF(ctx, chainID, requestID, appResponseBytes)
	}
	return nil
}

func (s *SenderTest) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendAppErrorF != nil {
		return s.SendAppErrorF(ctx, nodeID, requestID, errorCode, errorMessage)
	}
	return nil
}

func (s *SenderTest) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendCrossChainAppErrorF != nil {
		return s.SendCrossChainAppErrorF(ctx, chainID, requestID, errorCode, errorMessage)
	}
	return nil
}

// Implement remaining Sender interface methods with no-ops
func (s *SenderTest) SendGetStateSummaryFrontier(_ context.Context, _ set.Set[ids.NodeID], _ uint32) {}
func (s *SenderTest) SendStateSummaryFrontier(_ context.Context, _ ids.NodeID, _ uint32, _ []byte) {}
func (s *SenderTest) SendGetAcceptedStateSummary(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []uint64) {}
func (s *SenderTest) SendAcceptedStateSummary(_ context.Context, _ ids.NodeID, _ uint32, _ []ids.ID) {}
func (s *SenderTest) SendGetAcceptedFrontier(_ context.Context, _ set.Set[ids.NodeID], _ uint32) {}
func (s *SenderTest) SendAcceptedFrontier(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID) {}
func (s *SenderTest) SendGetAccepted(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []ids.ID) {}
func (s *SenderTest) SendAccepted(_ context.Context, _ ids.NodeID, _ uint32, _ []ids.ID) {}
func (s *SenderTest) SendGet(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID) {}
func (s *SenderTest) SendGetAncestors(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID) {}
func (s *SenderTest) SendAncestors(_ context.Context, _ ids.NodeID, _ uint32, _ [][]byte) {}
func (s *SenderTest) SendPut(_ context.Context, _ ids.NodeID, _ uint32, _ []byte) {}
func (s *SenderTest) SendPushQuery(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []byte, _ uint64) {}
func (s *SenderTest) SendPullQuery(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ ids.ID, _ uint64) {}
func (s *SenderTest) SendChits(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID, _ ids.ID, _ ids.ID, _ uint64) {}