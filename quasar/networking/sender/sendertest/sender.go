// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sendertest

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/engine/core/appsender"
	"github.com/luxfi/node/utils/set"
)

// Sender is a test sender implementation
type Sender struct {
	SentAppRequest         chan []byte
	SentAppResponse        chan []byte
	SentAppGossip          chan []byte
	SentAppError           chan *AppError
	SentCrossChainRequest  chan []byte
	SentCrossChainResponse chan []byte
}

// NewSender creates a new test sender
func NewSender() *Sender {
	return &Sender{
		SentAppRequest:         make(chan []byte, 1),
		SentAppResponse:        make(chan []byte, 1),
		SentAppGossip:          make(chan []byte, 1),
		SentAppError:           make(chan *AppError, 1),
		SentCrossChainRequest:  make(chan []byte, 1),
		SentCrossChainResponse: make(chan []byte, 1),
	}
}

func (s *Sender) SendAppRequest(_ context.Context, _ set.Set[ids.NodeID], _ uint32, msgBytes []byte) error {
	s.SentAppRequest <- msgBytes
	return nil
}

func (s *Sender) SendAppResponse(_ context.Context, _ ids.NodeID, _ uint32, msgBytes []byte) error {
	s.SentAppResponse <- msgBytes
	return nil
}

func (s *Sender) SendAppGossip(_ context.Context, _ appsender.SendConfig, msgBytes []byte) error {
	s.SentAppGossip <- msgBytes
	return nil
}

func (s *Sender) SendAppGossipSpecific(_ context.Context, _ set.Set[ids.NodeID], msgBytes []byte) error {
	s.SentAppGossip <- msgBytes
	return nil
}

func (s *Sender) SendAppError(_ context.Context, nodeID ids.NodeID, requestID uint32, code int32, msg string) error {
	s.SentAppError <- &AppError{
		NodeID:    nodeID,
		RequestID: requestID,
		Code:      code,
		Message:   msg,
	}
	return nil
}

func (s *Sender) SendCrossChainAppRequest(_ context.Context, _ ids.ID, _ uint32, msgBytes []byte) error {
	s.SentCrossChainRequest <- msgBytes
	return nil
}

func (s *Sender) SendCrossChainAppResponse(_ context.Context, _ ids.ID, _ uint32, msgBytes []byte) error {
	s.SentCrossChainResponse <- msgBytes
	return nil
}

// NetworkAppSender methods
func (s *Sender) SendAppRequestFailed(_ context.Context, _ ids.NodeID, _ uint32) error {
	return nil
}

func (s *Sender) SendGetStateSummaryFrontier(_ context.Context, _ set.Set[ids.NodeID], _ uint32) {}

func (s *Sender) SendStateSummaryFrontier(_ context.Context, _ ids.NodeID, _ uint32, _ []byte) {}

func (s *Sender) SendGetAcceptedStateSummary(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []uint64) {}

func (s *Sender) SendAcceptedStateSummary(_ context.Context, _ ids.NodeID, _ uint32, _ []ids.ID) {}

func (s *Sender) SendGetAcceptedFrontier(_ context.Context, _ set.Set[ids.NodeID], _ uint32) {}

func (s *Sender) SendAcceptedFrontier(_ context.Context, _ ids.NodeID, _ uint32, _ []ids.ID) {}

func (s *Sender) SendGetAccepted(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []ids.ID) {}

func (s *Sender) SendAccepted(_ context.Context, _ ids.NodeID, _ uint32, _ []ids.ID) {}

func (s *Sender) SendGet(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID) {}

func (s *Sender) SendGetAncestors(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID) {}

func (s *Sender) SendPut(_ context.Context, _ ids.NodeID, _ uint32, _ []byte) {}

func (s *Sender) SendAncestors(_ context.Context, _ ids.NodeID, _ uint32, _ [][]byte) {}

func (s *Sender) SendPushQuery(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []byte, _ uint64) {}

func (s *Sender) SendPullQuery(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ ids.ID, _ uint64) {}

func (s *Sender) SendChits(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID, _ ids.ID, _ ids.ID) {}

func (s *Sender) SendGossip(_ context.Context, _ []byte) {}

// Errors to return
type AppError struct {
	NodeID    ids.NodeID
	RequestID uint32
	Code      int32
	Message   string
}