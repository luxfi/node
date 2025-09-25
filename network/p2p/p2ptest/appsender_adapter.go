// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"testing"

	"github.com/luxfi/consensus/utils/set"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/network/p2p"
)

// SenderTest is a test implementation of AppSender
type SenderTest struct {
	T                             *testing.T
	SendAppRequestF               func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppResponseF              func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppErrorF                 func(context.Context, ids.NodeID, uint32, int32, string) error
	SendAppGossipF                func(context.Context, []byte) error
	SendCrossChainAppRequestF     func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppResponseF    func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppErrorF       func(context.Context, ids.ID, uint32, int32, string) error
	CantSendAppRequest            bool
	CantSendAppResponse           bool
	CantSendAppError              bool
	CantSendAppGossip             bool
	CantSendCrossChainAppRequest  bool
	CantSendCrossChainAppResponse bool
	CantSendCrossChainAppError    bool
}

// SenderTestAdapter adapts SenderTest to p2p.ExtendedAppSender
type SenderTestAdapter struct {
	*SenderTest
}

func NewSenderTestAdapter(sender *SenderTest) p2p.ExtendedAppSender {
	return &SenderTestAdapter{SenderTest: sender}
}

func (s *SenderTestAdapter) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	// Send to the first node in the set
	for nodeID := range nodeIDs {
		if s.SendAppRequestF != nil {
			return s.SendAppRequestF(ctx, nodeID, requestID, appRequestBytes)
		}
		if s.CantSendAppRequest && s.T != nil {
			s.T.Fatal("unexpected SendAppRequest")
		}
		return nil
	}
	return nil
}

func (s *SenderTestAdapter) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	if s.SendAppResponseF != nil {
		return s.SendAppResponseF(ctx, nodeID, requestID, appResponseBytes)
	}
	if s.CantSendAppResponse && s.T != nil {
		s.T.Fatal("unexpected SendAppResponse")
	}
	return nil
}

func (s *SenderTestAdapter) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendAppErrorF != nil {
		return s.SendAppErrorF(ctx, nodeID, requestID, errorCode, errorMessage)
	}
	if s.CantSendAppError && s.T != nil {
		s.T.Fatal("unexpected SendAppError")
	}
	return nil
}

func (s *SenderTestAdapter) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	if s.SendAppGossipF != nil {
		return s.SendAppGossipF(ctx, appGossipBytes)
	}
	if s.CantSendAppGossip && s.T != nil {
		s.T.Fatal("unexpected SendAppGossip")
	}
	return nil
}

func (s *SenderTestAdapter) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// SendAppGossipSpecificF expects node's set.Set, but we get consensus set.Set
	// For testing, we just call SendAppGossip
	return s.SendAppGossip(ctx, nodeIDs, appGossipBytes)
}

func (s *SenderTestAdapter) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	if s.SendCrossChainAppRequestF != nil {
		return s.SendCrossChainAppRequestF(ctx, chainID, requestID, appRequestBytes)
	}
	if s.CantSendCrossChainAppRequest && s.T != nil {
		s.T.Fatal("unexpected SendCrossChainAppRequest")
	}
	return nil
}

func (s *SenderTestAdapter) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	if s.SendCrossChainAppResponseF != nil {
		return s.SendCrossChainAppResponseF(ctx, chainID, requestID, appResponseBytes)
	}
	if s.CantSendCrossChainAppResponse && s.T != nil {
		s.T.Fatal("unexpected SendCrossChainAppResponse")
	}
	return nil
}

func (s *SenderTestAdapter) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendCrossChainAppErrorF != nil {
		return s.SendCrossChainAppErrorF(ctx, chainID, requestID, errorCode, errorMessage)
	}
	if s.CantSendCrossChainAppError && s.T != nil {
		s.T.Fatal("unexpected SendCrossChainAppError")
	}
	return nil
}
