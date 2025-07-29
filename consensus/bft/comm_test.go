// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bft

import (
	"testing"
	"time"

	"github.com/luxfi/bft"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/consensus/networking/sender/sendermock"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/set"
)

var testBFTMessage = bft.Message{
	VoteMessage: &bft.Vote{
		Vote: bft.ToBeSignedVote{
			BlockHeader: bft.BlockHeader{
				ProtocolMetadata: bft.ProtocolMetadata{
					Version: 1,
					Epoch:   1,
					Round:   1,
					Seq:     1,
				},
			},
		},
		Signature: bft.Signature{
			Signer: []byte("dummy_node_id"),
			Value:  []byte("dummy_signature"),
		},
	},
}

func TestCommSendMessage(t *testing.T) {
	config := newEngineConfig(t, 1)

	destinationNodeID := ids.GenerateTestNodeID()
	ctrl := gomock.NewController(t)
	sender := sendermock.NewExternalSender(ctrl)
	mc, err := message.NewCreator(
		logging.NoLog{},
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(t, err)

	config.OutboundMsgBuilder = mc
	config.Sender = sender

	comm, err := NewComm(config)
	require.NoError(t, err)

	outboundMsg, err := mc.BFTMessage(newVote(config.Ctx.ChainID, testBFTMessage.VoteMessage))
	require.NoError(t, err)
	expectedSendConfig := core.SendConfig{
		NodeIDs: set.Of(destinationNodeID),
	}
	sender.EXPECT().Send(outboundMsg, expectedSendConfig, comm.subnetID, gomock.Any())

	comm.Send(&testBFTMessage, destinationNodeID[:])
}

// TestCommBroadcast tests the Broadcast method sends to all nodes in the subnet
// not including the sending node.
func TestCommBroadcast(t *testing.T) {
	config := newEngineConfig(t, 3)

	ctrl := gomock.NewController(t)
	sender := sendermock.NewExternalSender(ctrl)
	mc, err := message.NewCreator(
		logging.NoLog{},
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(t, err)

	config.OutboundMsgBuilder = mc
	config.Sender = sender

	comm, err := NewComm(config)
	require.NoError(t, err)
	outboundMsg, err := mc.BFTMessage(newVote(config.Ctx.ChainID, testBFTMessage.VoteMessage))
	require.NoError(t, err)
	nodes := make([]ids.NodeID, 0, len(comm.Nodes()))
	for _, node := range comm.Nodes() {
		if node.Equals(config.Ctx.NodeID[:]) {
			continue // skip the sending node
		}
		nodes = append(nodes, ids.NodeID(node))
	}

	expectedSendConfig := core.SendConfig{
		NodeIDs: set.Of(nodes...),
	}

	sender.EXPECT().Send(outboundMsg, expectedSendConfig, comm.subnetID, gomock.Any())

	comm.Broadcast(&testBFTMessage)
}

func TestCommFailsWithoutCurrentNode(t *testing.T) {
	config := newEngineConfig(t, 3)

	ctrl := gomock.NewController(t)
	mc, err := message.NewCreator(
		logging.NoLog{},
		prometheus.NewRegistry(),
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(t, err)
	sender := sendermock.NewExternalSender(ctrl)

	config.OutboundMsgBuilder = mc
	config.Sender = sender

	// set the curNode to a different nodeID than the one in the config
	vdrs := generateTestNodes(t, 3)
	config.Validators = newTestValidatorInfo(vdrs)

	_, err = NewComm(config)
	require.ErrorIs(t, err, errNodeNotFound)
}
