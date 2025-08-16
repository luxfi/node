// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/ids"
	"github.com/luxfi/consensus/core"

	appsenderpb "github.com/luxfi/node/proto/pb/appsender"
)

var _ appsenderpb.AppSenderServer = (*Server)(nil)

type Server struct {
	appsenderpb.UnsafeAppSenderServer
	appSender core.AppSender
}

// NewServer returns a messenger connected to a remote channel
func NewServer(appSender core.AppSender) *Server {
	return &Server{appSender: appSender}
}

func (s *Server) SendAppRequest(ctx context.Context, req *appsenderpb.SendAppRequestMsg) (*emptypb.Empty, error) {
	// core.AppSender expects a single NodeID, not a set
	// Take the first node if multiple are provided
	if len(req.NodeIds) == 0 {
		return &emptypb.Empty{}, nil
	}
	
	nodeID, err := ids.ToNodeID(req.NodeIds[0])
	if err != nil {
		return nil, err
	}
	
	err = s.appSender.SendAppRequest(ctx, nodeID, req.RequestId, req.Request)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppResponse(ctx context.Context, req *appsenderpb.SendAppResponseMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	err = s.appSender.SendAppResponse(ctx, nodeID, req.RequestId, req.Response)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppError(ctx context.Context, req *appsenderpb.SendAppErrorMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}

	err = s.appSender.SendAppError(ctx, nodeID, req.RequestId, req.ErrorCode, req.ErrorMessage)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppGossip(ctx context.Context, req *appsenderpb.SendAppGossipMsg) (*emptypb.Empty, error) {
	// core.AppSender.SendAppGossip just takes bytes
	err := s.appSender.SendAppGossip(ctx, req.Msg)
	return &emptypb.Empty{}, err
}

// SendCrossChainAppRequest implements AppSenderServer
func (s *Server) SendCrossChainAppRequest(ctx context.Context, req *appsenderpb.SendCrossChainAppRequestMsg) (*emptypb.Empty, error) {
	// Not implemented - return empty response
	return &emptypb.Empty{}, nil
}

// SendCrossChainAppResponse implements AppSenderServer
func (s *Server) SendCrossChainAppResponse(ctx context.Context, req *appsenderpb.SendCrossChainAppResponseMsg) (*emptypb.Empty, error) {
	// Not implemented - return empty response
	return &emptypb.Empty{}, nil
}

// SendCrossChainAppError implements AppSenderServer
func (s *Server) SendCrossChainAppError(ctx context.Context, req *appsenderpb.SendCrossChainAppErrorMsg) (*emptypb.Empty, error) {
	// Not implemented - return empty response
	return &emptypb.Empty{}, nil
}