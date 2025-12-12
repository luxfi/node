// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
)

// WarpSender is a test implementation of p2p.WarpSender.
// Set function fields to customize behavior, or leave nil for default no-op.
// Set Cant* fields to true to fail on unexpected calls.
type WarpSender struct {
	T *testing.T

	// Function hooks - set these to customize behavior
	SendAppRequestF            func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	SendAppResponseF           func(context.Context, ids.NodeID, uint32, []byte) error
	SendAppErrorF              func(context.Context, ids.NodeID, uint32, int32, string) error
	SendAppGossipF             func(context.Context, set.Set[ids.NodeID], []byte) error
	SendCrossChainAppRequestF  func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppResponseF func(context.Context, ids.ID, uint32, []byte) error
	SendCrossChainAppErrorF    func(context.Context, ids.ID, uint32, int32, string) error

	// Fail flags - set to true to fail on unexpected calls
	CantSendAppRequest            bool
	CantSendAppResponse           bool
	CantSendAppError              bool
	CantSendAppGossip             bool
	CantSendCrossChainAppRequest  bool
	CantSendCrossChainAppResponse bool
	CantSendCrossChainAppError    bool
}

func (s *WarpSender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, msg []byte) error {
	if s.SendAppRequestF != nil {
		return s.SendAppRequestF(ctx, nodeIDs, requestID, msg)
	}
	if s.CantSendAppRequest && s.T != nil {
		s.T.Fatal("unexpected SendAppRequest")
	}
	return nil
}

func (s *WarpSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	if s.SendAppResponseF != nil {
		return s.SendAppResponseF(ctx, nodeID, requestID, msg)
	}
	if s.CantSendAppResponse && s.T != nil {
		s.T.Fatal("unexpected SendAppResponse")
	}
	return nil
}

func (s *WarpSender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, code int32, message string) error {
	if s.SendAppErrorF != nil {
		return s.SendAppErrorF(ctx, nodeID, requestID, code, message)
	}
	if s.CantSendAppError && s.T != nil {
		s.T.Fatal("unexpected SendAppError")
	}
	return nil
}

func (s *WarpSender) SendAppGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], msg []byte) error {
	if s.SendAppGossipF != nil {
		return s.SendAppGossipF(ctx, nodeIDs, msg)
	}
	if s.CantSendAppGossip && s.T != nil {
		s.T.Fatal("unexpected SendAppGossip")
	}
	return nil
}

func (s *WarpSender) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], msg []byte) error {
	return s.SendAppGossip(ctx, nodeIDs, msg)
}

func (s *WarpSender) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	if s.SendCrossChainAppRequestF != nil {
		return s.SendCrossChainAppRequestF(ctx, chainID, requestID, msg)
	}
	if s.CantSendCrossChainAppRequest && s.T != nil {
		s.T.Fatal("unexpected SendCrossChainAppRequest")
	}
	return nil
}

func (s *WarpSender) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, msg []byte) error {
	if s.SendCrossChainAppResponseF != nil {
		return s.SendCrossChainAppResponseF(ctx, chainID, requestID, msg)
	}
	if s.CantSendCrossChainAppResponse && s.T != nil {
		s.T.Fatal("unexpected SendCrossChainAppResponse")
	}
	return nil
}

func (s *WarpSender) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, code int32, message string) error {
	if s.SendCrossChainAppErrorF != nil {
		return s.SendCrossChainAppErrorF(ctx, chainID, requestID, code, message)
	}
	if s.CantSendCrossChainAppError && s.T != nil {
		s.T.Fatal("unexpected SendCrossChainAppError")
	}
	return nil
}
