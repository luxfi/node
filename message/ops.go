// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/luxfi/math/set"
	"github.com/luxfi/node/proto/p2p"
)

// Op is an opcode
type Op byte

// Types of messages that may be sent between nodes
// Note: If you add a new parseable Op below, you must add it to either
// [UnrequestedOps] or [FailedToResponseOps].
const (
	// Handshake:
	PingOp Op = iota
	PongOp
	HandshakeOp
	GetPeerListOp
	PeerListOp
	// State sync:
	GetStateSummaryFrontierOp
	GetStateSummaryFrontierFailedOp
	StateSummaryFrontierOp
	GetAcceptedStateSummaryOp
	GetAcceptedStateSummaryFailedOp
	AcceptedStateSummaryOp
	// Bootstrapping:
	GetAcceptedFrontierOp
	GetAcceptedFrontierFailedOp
	AcceptedFrontierOp
	GetAcceptedOp
	GetAcceptedFailedOp
	AcceptedOp
	GetAncestorsOp
	GetAncestorsFailedOp
	AncestorsOp
	// Consensus:
	GetOp
	GetFailedOp
	PutOp
	PushQueryOp
	PullQueryOp
	QueryFailedOp
	QbitOp // Authenticated preference signal (formerly ChitsOp)
	// Application:
	RequestOp
	ErrorOp
	ResponseOp
	GossipOp
	// Internal:
	ConnectedOp
	DisconnectedOp
	NotifyOp
	GossipRequestOp
	// BFT
	BFTOp
)

var (
	// UnrequestedOps are operations that are expected to be seen without having
	// requested them. For example, a peer may receive a Get request for a block
	// without having sent a message previously.
	UnrequestedOps = set.Of(
		GetAcceptedFrontierOp,
		GetAcceptedOp,
		GetAncestorsOp,
		GetOp,
		PushQueryOp,
		PullQueryOp,
		RequestOp,
		GossipOp,
		GetStateSummaryFrontierOp,
		GetAcceptedStateSummaryOp,
		BFTOp,
	)
	// FailedToResponseOps maps response failure messages to their successful
	// counterparts.
	FailedToResponseOps = map[Op]Op{
		GetStateSummaryFrontierFailedOp: StateSummaryFrontierOp,
		GetAcceptedStateSummaryFailedOp: AcceptedStateSummaryOp,
		GetAcceptedFrontierFailedOp:     AcceptedFrontierOp,
		GetAcceptedFailedOp:             AcceptedOp,
		GetAncestorsFailedOp:            AncestorsOp,
		GetFailedOp:                     PutOp,
		QueryFailedOp:                   QbitOp,
		ErrorOp:                         ResponseOp,
	}

	errUnknownMessageType = errors.New("unknown message type")
)

func (op Op) String() string {
	switch op {
	// Handshake
	case PingOp:
		return "ping"
	case PongOp:
		return "pong"
	case HandshakeOp:
		return "handshake"
	case GetPeerListOp:
		return "get_peerlist"
	case PeerListOp:
		return "peerlist"
	// State sync
	case GetStateSummaryFrontierOp:
		return "get_state_summary_frontier"
	case GetStateSummaryFrontierFailedOp:
		return "get_state_summary_frontier_failed"
	case StateSummaryFrontierOp:
		return "state_summary_frontier"
	case GetAcceptedStateSummaryOp:
		return "get_accepted_state_summary"
	case GetAcceptedStateSummaryFailedOp:
		return "get_accepted_state_summary_failed"
	case AcceptedStateSummaryOp:
		return "accepted_state_summary"
	// Bootstrapping
	case GetAcceptedFrontierOp:
		return "get_accepted_frontier"
	case GetAcceptedFrontierFailedOp:
		return "get_accepted_frontier_failed"
	case AcceptedFrontierOp:
		return "accepted_frontier"
	case GetAcceptedOp:
		return "get_accepted"
	case GetAcceptedFailedOp:
		return "get_accepted_failed"
	case AcceptedOp:
		return "accepted"
	case GetAncestorsOp:
		return "get_ancestors"
	case GetAncestorsFailedOp:
		return "get_ancestors_failed"
	case AncestorsOp:
		return "ancestors"
	// Consensus
	case GetOp:
		return "get"
	case GetFailedOp:
		return "get_failed"
	case PutOp:
		return "put"
	case PushQueryOp:
		return "push_query"
	case PullQueryOp:
		return "pull_query"
	case QueryFailedOp:
		return "query_failed"
	case QbitOp:
		return "qbit"
	// Application
	case RequestOp:
		return "app_request"
	case ErrorOp:
		return "app_error"
	case ResponseOp:
		return "app_response"
	case GossipOp:
		return "app_gossip"
	// Internal
	case ConnectedOp:
		return "connected"
	case DisconnectedOp:
		return "disconnected"
	case NotifyOp:
		return "notify"
	case GossipRequestOp:
		return "gossip_request"
	// BFT
	case BFTOp:
		return "bft"
	default:
		return "unknown"
	}
}

func Unwrap(m *p2p.Message) (fmt.Stringer, error) {
	switch msg := m.GetMessage().(type) {
	// Handshake:
	case *p2p.Message_Ping:
		return msg.Ping, nil
	case *p2p.Message_Pong:
		return msg.Pong, nil
	case *p2p.Message_Handshake:
		return msg.Handshake, nil
	case *p2p.Message_GetPeerList:
		return msg.GetPeerList, nil
	case *p2p.Message_PeerList_:
		return msg.PeerList_, nil
	// State sync:
	case *p2p.Message_GetStateSummaryFrontier:
		return msg.GetStateSummaryFrontier, nil
	case *p2p.Message_StateSummaryFrontier_:
		return msg.StateSummaryFrontier_, nil
	case *p2p.Message_GetAcceptedStateSummary:
		return msg.GetAcceptedStateSummary, nil
	case *p2p.Message_AcceptedStateSummary_:
		return msg.AcceptedStateSummary_, nil
	// Bootstrapping:
	case *p2p.Message_GetAcceptedFrontier:
		return msg.GetAcceptedFrontier, nil
	case *p2p.Message_AcceptedFrontier_:
		return msg.AcceptedFrontier_, nil
	case *p2p.Message_GetAccepted:
		return msg.GetAccepted, nil
	case *p2p.Message_Accepted_:
		return msg.Accepted_, nil
	case *p2p.Message_GetAncestors:
		return msg.GetAncestors, nil
	case *p2p.Message_Ancestors_:
		return msg.Ancestors_, nil
	// Consensus:
	case *p2p.Message_Get:
		return msg.Get, nil
	case *p2p.Message_Put:
		return msg.Put, nil
	case *p2p.Message_PushQuery:
		return msg.PushQuery, nil
	case *p2p.Message_PullQuery:
		return msg.PullQuery, nil
	case *p2p.Message_Chits:
		return msg.Chits, nil
	// Application:
	case *p2p.Message_Request:
		return msg.Request, nil
	case *p2p.Message_Response:
		return msg.Response, nil
	case *p2p.Message_Error:
		return msg.Error, nil
	case *p2p.Message_Gossip:
		return msg.Gossip, nil
	// BFT
	case *p2p.Message_BFT:
		return extractBFT(msg), nil
	default:
		return nil, fmt.Errorf("%w: %T", errUnknownMessageType, msg)
	}
}

// ToConsensusOp maps message.Op to consensus router Op values
// Returns the consensus Op value and whether the mapping exists
func ToConsensusOp(op Op) (byte, bool) {
	switch op {
	case GetAcceptedFrontierOp:
		return 0, true // GetAcceptedFrontier
	case AcceptedFrontierOp:
		return 1, true // AcceptedFrontier
	case GetAcceptedOp:
		return 2, true // GetAccepted
	case AcceptedOp:
		return 3, true // Accepted
	case GetOp:
		return 4, true // Get
	case PutOp:
		return 5, true // Put
	case PushQueryOp:
		return 6, true // PushQuery
	case PullQueryOp:
		return 7, true // PullQuery
	case QbitOp:
		return 8, true // Qbit
	case GetAncestorsOp:
		return 9, true // GetContext (wire protocol still uses GetAncestors)
	case AncestorsOp:
		return 10, true // Context (wire protocol still uses Ancestors)
	default:
		return 0, false
	}
}

// GetContainerBytes extracts the container/body bytes from various message types
func GetContainerBytes(msg fmt.Stringer) []byte {
	switch m := msg.(type) {
	case *p2p.Put:
		return m.Container
	case *p2p.PushQuery:
		return m.Container
	case *p2p.Ancestors:
		// Encode all containers with length prefix so handlers can decode
		// multiple blocks from a single byte slice.
		if len(m.Containers) == 0 {
			return nil
		}
		totalLen := 0
		for _, container := range m.Containers {
			totalLen += 4 + len(container)
		}
		encoded := make([]byte, 0, totalLen)
		for _, container := range m.Containers {
			var lenBuf [4]byte
			binary.BigEndian.PutUint32(lenBuf[:], uint32(len(container)))
			encoded = append(encoded, lenBuf[:]...)
			encoded = append(encoded, container...)
		}
		return encoded
	case *p2p.Gossip:
		return m.AppBytes
	case *p2p.Request:
		return m.AppBytes
	case *p2p.Response:
		return m.AppBytes
	// Request messages with container IDs for P-Chain sync
	case *p2p.GetAccepted:
		containerIDs := m.GetContainerIds()
		if len(containerIDs) == 0 {
			return nil
		}
		result := make([]byte, 0, len(containerIDs)*32)
		for _, id := range containerIDs {
			result = append(result, id...)
		}
		return result
	case *p2p.Get:
		return m.GetContainerId()
	case *p2p.GetAncestors:
		return m.GetContainerId()
	case *p2p.PullQuery:
		return m.GetContainerId()
	case *p2p.Chits:
		// For Qbit messages (formerly Chits), return the PreferredId (the block being voted for)
		return m.GetPreferredId()
	case *p2p.AcceptedFrontier:
		return m.GetContainerId()
	case *p2p.Accepted:
		containerIDs := m.GetContainerIds()
		if len(containerIDs) == 0 {
			return nil
		}
		result := make([]byte, 0, len(containerIDs)*32)
		for _, id := range containerIDs {
			result = append(result, id...)
		}
		return result
	default:
		return nil
	}
}

func ToOp(m *p2p.Message) (Op, error) {
	switch msg := m.GetMessage().(type) {
	case *p2p.Message_Ping:
		return PingOp, nil
	case *p2p.Message_Pong:
		return PongOp, nil
	case *p2p.Message_Handshake:
		return HandshakeOp, nil
	case *p2p.Message_GetPeerList:
		return GetPeerListOp, nil
	case *p2p.Message_PeerList_:
		return PeerListOp, nil
	case *p2p.Message_GetStateSummaryFrontier:
		return GetStateSummaryFrontierOp, nil
	case *p2p.Message_StateSummaryFrontier_:
		return StateSummaryFrontierOp, nil
	case *p2p.Message_GetAcceptedStateSummary:
		return GetAcceptedStateSummaryOp, nil
	case *p2p.Message_AcceptedStateSummary_:
		return AcceptedStateSummaryOp, nil
	case *p2p.Message_GetAcceptedFrontier:
		return GetAcceptedFrontierOp, nil
	case *p2p.Message_AcceptedFrontier_:
		return AcceptedFrontierOp, nil
	case *p2p.Message_GetAccepted:
		return GetAcceptedOp, nil
	case *p2p.Message_Accepted_:
		return AcceptedOp, nil
	case *p2p.Message_GetAncestors:
		return GetAncestorsOp, nil
	case *p2p.Message_Ancestors_:
		return AncestorsOp, nil
	case *p2p.Message_Get:
		return GetOp, nil
	case *p2p.Message_Put:
		return PutOp, nil
	case *p2p.Message_PushQuery:
		return PushQueryOp, nil
	case *p2p.Message_PullQuery:
		return PullQueryOp, nil
	case *p2p.Message_Chits:
		return QbitOp, nil
	case *p2p.Message_Request:
		return RequestOp, nil
	case *p2p.Message_Response:
		return ResponseOp, nil
	case *p2p.Message_Error:
		return ErrorOp, nil
	case *p2p.Message_Gossip:
		return GossipOp, nil
	case *p2p.Message_BFT:
		return BFTOp, nil
	default:
		return 0, fmt.Errorf("%w: %T", errUnknownMessageType, msg)
	}
}
