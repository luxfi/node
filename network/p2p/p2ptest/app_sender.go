// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"testing"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/network/p2p"
)

// AppSender is a test implementation of p2p.Sender.
// Set function fields to customize behavior, or leave nil for default no-op.
// Set Cant* fields to true to fail on unexpected calls.
type AppSender struct {
	T *testing.T

	// Function hooks - set these to customize behavior
	SendRequestF  func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	SendResponseF func(context.Context, ids.NodeID, uint32, []byte) error
	SendGossipF   func(context.Context, p2p.SendConfig, []byte) error
	SendErrorF    func(context.Context, ids.NodeID, uint32, int32, string) error

	// Fail flags - set to true to fail on unexpected calls
	CantSendRequest  bool
	CantSendResponse bool
	CantSendGossip   bool
	CantSendError    bool
}

var _ p2p.Sender = (*AppSender)(nil)

func (s *AppSender) SendRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, msg []byte) error {
	if s.SendRequestF != nil {
		return s.SendRequestF(ctx, nodeIDs, requestID, msg)
	}
	if s.CantSendRequest && s.T != nil {
		s.T.Fatal("unexpected SendRequest")
	}
	return nil
}

func (s *AppSender) SendResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, msg []byte) error {
	if s.SendResponseF != nil {
		return s.SendResponseF(ctx, nodeID, requestID, msg)
	}
	if s.CantSendResponse && s.T != nil {
		s.T.Fatal("unexpected SendResponse")
	}
	return nil
}

func (s *AppSender) SendGossip(ctx context.Context, config p2p.SendConfig, msg []byte) error {
	if s.SendGossipF != nil {
		return s.SendGossipF(ctx, config, msg)
	}
	if s.CantSendGossip && s.T != nil {
		s.T.Fatal("unexpected SendGossip")
	}
	return nil
}

func (s *AppSender) SendError(ctx context.Context, nodeID ids.NodeID, requestID uint32, code int32, message string) error {
	if s.SendErrorF != nil {
		return s.SendErrorF(ctx, nodeID, requestID, code, message)
	}
	if s.CantSendError && s.T != nil {
		s.T.Fatal("unexpected SendError")
	}
	return nil
}
