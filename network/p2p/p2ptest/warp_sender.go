// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
)

// WarpSender is a test implementation of p2p.Sender.
// Set function fields to customize behavior, or leave nil for default no-op.
// Set Cant* fields to true to fail on unexpected calls.
type WarpSender struct {
	T *testing.T

	// Function hooks - set these to customize behavior
	SendRequestF        func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	SendResponseF       func(context.Context, ids.NodeID, uint32, []byte) error
	SendErrorF          func(context.Context, ids.NodeID, uint32, int32, string) error
	SendGossipF         func(context.Context, set.Set[ids.NodeID], []byte) error
	SendGossipSpecificF func(context.Context, set.Set[ids.NodeID], []byte) error

	// Fail flags - set to true to fail on unexpected calls
	CantSendRequest        bool
	CantSendResponse       bool
	CantSendError          bool
	CantSendGossip         bool
	CantSendGossipSpecific bool
}

func (s *WarpSender) SendRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, msg []byte) error {
	if s.SendRequestF != nil {
		return s.SendRequestF(ctx, nodeIDs, requestID, msg)
	}
	if s.CantSendRequest && s.T != nil {
		s.T.Fatal("unexpected SendRequest")
	}
	return nil
}

func (s *WarpSender) SendResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	if s.SendResponseF != nil {
		return s.SendResponseF(ctx, nodeID, requestID, msg)
	}
	if s.CantSendResponse && s.T != nil {
		s.T.Fatal("unexpected SendResponse")
	}
	return nil
}

func (s *WarpSender) SendError(ctx context.Context, nodeID ids.NodeID, requestID uint32, code int32, message string) error {
	if s.SendErrorF != nil {
		return s.SendErrorF(ctx, nodeID, requestID, code, message)
	}
	if s.CantSendError && s.T != nil {
		s.T.Fatal("unexpected SendError")
	}
	return nil
}

func (s *WarpSender) SendGossip(ctx context.Context, nodeIDs set.Set[ids.NodeID], msg []byte) error {
	if s.SendGossipF != nil {
		return s.SendGossipF(ctx, nodeIDs, msg)
	}
	if s.CantSendGossip && s.T != nil {
		s.T.Fatal("unexpected SendGossip")
	}
	return nil
}

func (s *WarpSender) SendGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], msg []byte) error {
	if s.SendGossipSpecificF != nil {
		return s.SendGossipSpecificF(ctx, nodeIDs, msg)
	}
	if s.CantSendGossipSpecific && s.T != nil {
		s.T.Fatal("unexpected SendGossipSpecific")
	}
	return s.SendGossip(ctx, nodeIDs, msg)
}
