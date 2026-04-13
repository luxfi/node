// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap

import (
	"context"

	zapwire "github.com/luxfi/api/zap"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/p2p"
)

var _ p2p.Sender = (*Sender)(nil)

// Sender implements p2p.Sender over ZAP transport
type Sender struct {
	conn *zapwire.Conn
}

// NewSender creates a new ZAP-based p2p.Sender
func NewSender(conn *zapwire.Conn) *Sender {
	return &Sender{conn: conn}
}

// SendRequest sends a request to the specified nodes
func (s *Sender) SendRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
	nodeIDBytes := make([][]byte, 0, nodeIDs.Len())
	for nodeID := range nodeIDs {
		nodeIDBytes = append(nodeIDBytes, nodeID[:])
	}

	msg := &zapwire.SendRequestMsg{
		NodeIDs:   nodeIDBytes,
		RequestID: requestID,
		Request:   request,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	msg.Encode(buf)

	return s.conn.Send(zapwire.MsgSendRequest, buf.Bytes())
}

// SendResponse sends a response to a previous request
func (s *Sender) SendResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	msg := &zapwire.SendResponseMsg{
		NodeID:    nodeID[:],
		RequestID: requestID,
		Response:  response,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	msg.Encode(buf)

	return s.conn.Send(zapwire.MsgSendResponse, buf.Bytes())
}

// SendError sends an error response to a previous request
func (s *Sender) SendError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	msg := &zapwire.SendErrorMsg{
		NodeID:       nodeID[:],
		RequestID:    requestID,
		ErrorCode:    errorCode,
		ErrorMessage: errorMessage,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	msg.Encode(buf)

	return s.conn.Send(zapwire.MsgSendError, buf.Bytes())
}

// SendGossip sends a gossip message
func (s *Sender) SendGossip(ctx context.Context, config p2p.SendConfig, msg []byte) error {
	nodeIDBytes := make([][]byte, 0, config.NodeIDs.Len())
	for nodeID := range config.NodeIDs {
		nodeIDBytes = append(nodeIDBytes, nodeID[:])
	}

	gossipMsg := &zapwire.SendGossipMsg{
		NodeIDs:       nodeIDBytes,
		Validators:    uint64(config.Validators),
		NonValidators: uint64(config.NonValidators),
		Peers:         uint64(config.Peers),
		Msg:           msg,
	}

	buf := zapwire.GetBuffer()
	defer zapwire.PutBuffer(buf)
	gossipMsg.Encode(buf)

	return s.conn.Send(zapwire.MsgSendGossip, buf.Bytes())
}
