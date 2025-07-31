// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/engine/core/appsender"
	"github.com/luxfi/node/utils/set"
)

// Sender is a test implementation of appsender.AppSender
type Sender struct {
	SendAppGossipF         func(ctx context.Context, sendConfig appsender.SendConfig, gossipBytes []byte) error
	SendAppGossipSpecificF func(ctx context.Context, nodeIDs set.Set[ids.NodeID], gossipBytes []byte) error
	SendAppRequestF        func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error
	SendAppResponseF       func(ctx context.Context, nodeID ids.NodeID, requestID uint32, responseBytes []byte) error
	SendAppErrorF          func(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error
	SendCrossChainAppRequestF  func(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error
	SendCrossChainAppResponseF func(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error
	SendCrossChainAppErrorF    func(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error
}

// SendAppGossip implements appsender.AppSender
func (s *Sender) SendAppGossip(ctx context.Context, sendConfig appsender.SendConfig, appGossipBytes []byte) error {
	if s.SendAppGossipF != nil {
		return s.SendAppGossipF(ctx, sendConfig, appGossipBytes)
	}
	return nil
}

// SendAppGossipSpecific implements core.AppSender
func (s *Sender) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	if s.SendAppGossipSpecificF != nil {
		return s.SendAppGossipSpecificF(ctx, nodeIDs, appGossipBytes)
	}
	return nil
}

// SendAppRequest implements core.AppSender
func (s *Sender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	if s.SendAppRequestF != nil {
		return s.SendAppRequestF(ctx, nodeIDs, requestID, appRequestBytes)
	}
	return nil
}

// SendAppResponse implements core.AppSender
func (s *Sender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	if s.SendAppResponseF != nil {
		return s.SendAppResponseF(ctx, nodeID, requestID, appResponseBytes)
	}
	return nil
}

// SendAppError implements core.AppSender
func (s *Sender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendAppErrorF != nil {
		return s.SendAppErrorF(ctx, nodeID, requestID, errorCode, errorMessage)
	}
	return nil
}

// SendCrossChainAppRequest implements core.AppSender
func (s *Sender) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	if s.SendCrossChainAppRequestF != nil {
		return s.SendCrossChainAppRequestF(ctx, chainID, requestID, appRequestBytes)
	}
	return nil
}

// SendCrossChainAppResponse implements core.AppSender
func (s *Sender) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	if s.SendCrossChainAppResponseF != nil {
		return s.SendCrossChainAppResponseF(ctx, chainID, requestID, appResponseBytes)
	}
	return nil
}

// SendCrossChainAppError implements core.AppSender
func (s *Sender) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	if s.SendCrossChainAppErrorF != nil {
		return s.SendCrossChainAppErrorF(ctx, chainID, requestID, errorCode, errorMessage)
	}
	return nil
}