// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bft

import (
	"errors"
	"fmt"

	"github.com/luxfi/bft"
	"go.uber.org/zap"

	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/log"
	"github.com/luxfi/node/consensus/networking/sender"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/subnets"
	"github.com/luxfi/node/utils/set"
)

var (
	_               bft.Communication = (*Comm)(nil)
	errNodeNotFound                       = errors.New("node not found in the validator list")
)

// loggerAdapter adapts log.Logger to bft.Logger
type loggerAdapter struct {
	wrapped log.Logger
}

// Ensure loggerAdapter implements bft.Logger
var _ bft.Logger = (*loggerAdapter)(nil)

func (l *loggerAdapter) Fatal(msg string, fields ...zap.Field) {
	l.wrapped.Error(msg, fieldsToInterface(fields)...)
}

func (l *loggerAdapter) Error(msg string, fields ...zap.Field) {
	l.wrapped.Error(msg, fieldsToInterface(fields)...)
}

func (l *loggerAdapter) Warn(msg string, fields ...zap.Field) {
	l.wrapped.Warn(msg, fieldsToInterface(fields)...)
}

func (l *loggerAdapter) Info(msg string, fields ...zap.Field) {
	l.wrapped.Info(msg, fieldsToInterface(fields)...)
}

func (l *loggerAdapter) Trace(msg string, fields ...zap.Field) {
	l.wrapped.Trace(msg, fieldsToInterface(fields)...)
}

func (l *loggerAdapter) Debug(msg string, fields ...zap.Field) {
	l.wrapped.Debug(msg, fieldsToInterface(fields)...)
}

func (l *loggerAdapter) Verbo(msg string, fields ...zap.Field) {
	l.wrapped.Debug(msg, fieldsToInterface(fields)...)
}

// fieldsToInterface converts zap.Field slice to interface{} slice
func fieldsToInterface(fields []zap.Field) []interface{} {
	result := make([]interface{}, 0, len(fields)*2)
	for _, field := range fields {
		result = append(result, field.Key, field.Interface)
	}
	return result
}

type Comm struct {
	logger   bft.Logger
	subnetID ids.ID
	chainID  ids.ID
	// broadcastNodes are the nodes that should receive broadcast messages
	broadcastNodes set.Set[ids.NodeID]
	// allNodes are the IDs of all the nodes in the subnet
	allNodes []bft.NodeID

	// sender is used to send messages to other nodes
	sender     sender.ExternalSender
	msgBuilder message.OutboundMsgBuilder
}

func NewComm(config *Config) (*Comm, error) {
	if _, ok := config.Validators[config.Ctx.NodeID]; !ok {
		config.Log.Warn("Node is not a validator for the subnet",
			zap.Stringer("nodeID", config.Ctx.NodeID),
			zap.Stringer("chainID", config.Ctx.ChainID),
			zap.Stringer("subnetID", config.Ctx.SubnetID),
		)
		return nil, fmt.Errorf("our %w: %s", errNodeNotFound, config.Ctx.NodeID)
	}

	broadcastNodes := set.NewSet[ids.NodeID](len(config.Validators) - 1)
	allNodes := make([]bft.NodeID, 0, len(config.Validators))
	// grab all the nodes that are validators for the subnet
	for _, vd := range config.Validators {
		allNodes = append(allNodes, vd.NodeID[:])
		if vd.NodeID == config.Ctx.NodeID {
			continue // skip our own node ID
		}

		broadcastNodes.Add(vd.NodeID)
	}

	return &Comm{
		subnetID:       config.Ctx.SubnetID,
		broadcastNodes: broadcastNodes,
		allNodes:       allNodes,
		logger:         &loggerAdapter{wrapped: config.Log},
		sender:         config.Sender,
		msgBuilder:     config.OutboundMsgBuilder,
		chainID:        config.Ctx.ChainID,
	}, nil
}

func (c *Comm) Nodes() []bft.NodeID {
	return c.allNodes
}

func (c *Comm) Send(msg *bft.Message, destination bft.NodeID) {
	outboundMsg, err := c.bftMessageToOutboundMessage(msg)
	if err != nil {
		c.logger.Error("Failed creating message", zap.Error(err))
		return
	}

	dest, err := ids.ToNodeID(destination)
	if err != nil {
		c.logger.Error("Failed to convert destination NodeID", zap.Error(err))
		return
	}

	c.sender.Send(outboundMsg, core.SendConfig{NodeIDs: set.Of(dest)}, c.subnetID, subnets.NoOpAllower)
}

func (c *Comm) Broadcast(msg *bft.Message) {
	outboundMsg, err := c.bftMessageToOutboundMessage(msg)
	if err != nil {
		c.logger.Error("Failed creating message", zap.Error(err))
		return
	}

	c.sender.Send(outboundMsg, core.SendConfig{NodeIDs: c.broadcastNodes}, c.subnetID, subnets.NoOpAllower)
}

func (c *Comm) bftMessageToOutboundMessage(msg *bft.Message) (message.OutboundMessage, error) {
	var bftMsg *p2p.BFT
	switch {
	case msg.VerifiedBlockMessage != nil:
		bytes, err := msg.VerifiedBlockMessage.VerifiedBlock.Bytes()
		if err != nil {
			return nil, fmt.Errorf("failed to serialize block: %w", err)
		}
		bftMsg = newBlockProposal(c.chainID, bytes, msg.VerifiedBlockMessage.Vote)
	case msg.VoteMessage != nil:
		bftMsg = newVote(c.chainID, msg.VoteMessage)
	case msg.EmptyVoteMessage != nil:
		bftMsg = newEmptyVote(c.chainID, msg.EmptyVoteMessage)
	case msg.FinalizeVote != nil:
		bftMsg = newFinalizeVote(c.chainID, msg.FinalizeVote)
	case msg.Notarization != nil:
		bftMsg = newNotarization(c.chainID, msg.Notarization)
	case msg.EmptyNotarization != nil:
		bftMsg = newEmptyNotarization(c.chainID, msg.EmptyNotarization)
	case msg.Finalization != nil:
		bftMsg = newFinalization(c.chainID, msg.Finalization)
	case msg.ReplicationRequest != nil:
		bftMsg = newReplicationRequest(c.chainID, msg.ReplicationRequest)
	case msg.VerifiedReplicationResponse != nil:
		msg, err := newReplicationResponse(c.chainID, msg.VerifiedReplicationResponse)
		if err != nil {
			return nil, fmt.Errorf("failed to create replication response: %w", err)
		}
		bftMsg = msg
	default:
		return nil, fmt.Errorf("unknown message type: %+v", msg)
	}

	return c.msgBuilder.BFTMessage(bftMsg)
}
