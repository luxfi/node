// Copyright (C) 2019-2025, Lux Partners Limited All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/consensus/core"

	messengerpb "github.com/luxfi/node/proto/pb/messenger"
)

var (
	errFullQueue = errors.New("full message queue")

	_ messengerpb.MessengerServer = (*Server)(nil)
)

// Server is a messenger that is managed over RPC.
type Server struct {
	messengerpb.UnsafeMessengerServer
	messenger chan<- core.Message
}

// NewServer returns a messenger connected to a remote channel
func NewServer(messenger chan<- core.Message) *Server {
	return &Server{messenger: messenger}
}

func (s *Server) Notify(_ context.Context, req *messengerpb.NotifyRequest) (*messengerpb.NotifyResponse, error) {
	// Convert protobuf Message to core.Message
	var nodeID ids.NodeID
	copy(nodeID[:], req.Message.NodeId)
	
	msg := core.Message{
		Type:    core.MessageType(req.Message.Type),
		NodeID:  nodeID,
		Content: req.Message.Content,
	}
	
	select {
	case s.messenger <- msg:
		return &messengerpb.NotifyResponse{}, nil
	default:
		return nil, errFullQueue
	}
}
