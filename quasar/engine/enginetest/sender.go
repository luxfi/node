// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"context"

	"github.com/luxfi/ids"
)

// SenderStub is a stub implementation of the consensus.Sender interface for testing
type SenderStub struct{}

func (s *SenderStub) SendGetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	return nil
}

func (s *SenderStub) SendGet(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	return nil
}

func (s *SenderStub) SendPut(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	return nil
}

func (s *SenderStub) SendPushQuery(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, container []byte) error {
	return nil
}

func (s *SenderStub) SendPullQuery(ctx context.Context, nodeIDs []ids.NodeID, requestID uint32, containerID ids.ID) error {
	return nil
}

func (s *SenderStub) SendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error {
	return nil
}

func (s *SenderStub) SendGossip(ctx context.Context, container []byte) error {
	return nil
}