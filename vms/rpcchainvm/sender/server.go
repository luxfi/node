//go:build grpc

// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	senderpb "github.com/luxfi/node/proto/pb/sender"
	"github.com/luxfi/warp"
)

var _ senderpb.SenderServer = (*Server)(nil)

type Server struct {
	senderpb.UnsafeSenderServer
	sender warp.Sender
}

// NewServer returns a messenger connected to a remote channel
func NewServer(sender warp.Sender) *Server {
	return &Server{sender: sender}
}

func (s *Server) SendRequest(ctx context.Context, req *senderpb.SendRequestMsg) (*emptypb.Empty, error) {
	// Convert byte slices to NodeID set
	nodeIDs := set.NewSet[ids.NodeID](len(req.NodeIds))
	for _, nodeIDBytes := range req.NodeIds {
		nodeID, err := ids.ToNodeID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs.Add(nodeID)
	}

	err := s.sender.SendRequest(ctx, nodeIDs, req.RequestId, req.Request)
	return &emptypb.Empty{}, err
}

func (s *Server) SendResponse(ctx context.Context, req *senderpb.SendResponseMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	err = s.sender.SendResponse(ctx, nodeID, req.RequestId, req.Response)
	return &emptypb.Empty{}, err
}

func (s *Server) SendError(ctx context.Context, req *senderpb.SendErrorMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}

	err = s.sender.SendError(ctx, nodeID, req.RequestId, req.ErrorCode, req.ErrorMessage)
	return &emptypb.Empty{}, err
}

func (s *Server) SendGossip(ctx context.Context, req *senderpb.SendGossipMsg) (*emptypb.Empty, error) {
	// For RPC gossip, we don't have specific nodes, so use an empty config
	config := warp.SendConfig{
		NodeIDs: set.NewSet[ids.NodeID](0),
	}
	err := s.sender.SendGossip(ctx, config, req.Msg)
	return &emptypb.Empty{}, err
}
