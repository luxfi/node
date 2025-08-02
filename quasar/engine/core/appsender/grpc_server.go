// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/engine/core"
	"google.golang.org/protobuf/types/known/emptypb"

	appsenderpb "github.com/luxfi/node/proto/pb/appsender"
)

// GRPCServer wraps a core.AppSender to implement the gRPC AppSenderServer interface
type GRPCServer struct {
	appsenderpb.UnimplementedAppSenderServer
	appSender core.AppSender
}

// NewGRPCServer creates a new gRPC server wrapper for AppSender
func NewGRPCServer(appSender core.AppSender) *GRPCServer {
	return &GRPCServer{
		appSender: appSender,
	}
}

// SendAppRequest implements the gRPC AppSenderServer interface
func (s *GRPCServer) SendAppRequest(ctx context.Context, req *appsenderpb.SendAppRequestMsg) (*emptypb.Empty, error) {
	nodeIDs := make([]ids.NodeID, len(req.NodeIds))
	for i, nodeIDBytes := range req.NodeIds {
		nodeID, err := ids.ToNodeID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs[i] = nodeID
	}
	
	err := s.appSender.SendAppRequest(ctx, nodeIDs, req.RequestId, req.Request)
	return &emptypb.Empty{}, err
}

// SendAppResponse implements the gRPC AppSenderServer interface
func (s *GRPCServer) SendAppResponse(ctx context.Context, req *appsenderpb.SendAppResponseMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	
	err = s.appSender.SendAppResponse(ctx, nodeID, req.RequestId, req.Response)
	return &emptypb.Empty{}, err
}

// SendAppError implements the gRPC AppSenderServer interface
func (s *GRPCServer) SendAppError(ctx context.Context, req *appsenderpb.SendAppErrorMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	
	err = s.appSender.SendAppError(ctx, nodeID, req.RequestId, req.ErrorCode, req.ErrorMessage)
	return &emptypb.Empty{}, err
}

// SendAppGossip implements the gRPC AppSenderServer interface
func (s *GRPCServer) SendAppGossip(ctx context.Context, req *appsenderpb.SendAppGossipMsg) (*emptypb.Empty, error) {
	// Simple version - just send the gossip message
	err := s.appSender.SendAppGossip(ctx, req.Msg)
	return &emptypb.Empty{}, err
}