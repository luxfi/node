// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package p2p re-exports P2P types from the ZAP wire format
// implementation. ZAP is the only supported transport.
package p2p

import "github.com/luxfi/proto/node/zap/p2p"

// Re-export all types from ZAP implementation
type (
	EngineType              = p2p.EngineType
	Message                 = p2p.Message
	Message_CompressedZstd  = p2p.Message_CompressedZstd
	Message_Ping            = p2p.Message_Ping
	Message_Pong            = p2p.Message_Pong
	Message_Handshake       = p2p.Message_Handshake
	Message_GetPeerList     = p2p.Message_GetPeerList
	Message_PeerList_       = p2p.Message_PeerList_
	Message_GetStateSummaryFrontier = p2p.Message_GetStateSummaryFrontier
	Message_StateSummaryFrontier_   = p2p.Message_StateSummaryFrontier_
	Message_GetAcceptedStateSummary = p2p.Message_GetAcceptedStateSummary
	Message_AcceptedStateSummary_   = p2p.Message_AcceptedStateSummary_
	Message_GetAcceptedFrontier     = p2p.Message_GetAcceptedFrontier
	Message_AcceptedFrontier_       = p2p.Message_AcceptedFrontier_
	Message_GetAccepted             = p2p.Message_GetAccepted
	Message_Accepted_               = p2p.Message_Accepted_
	Message_GetAncestors            = p2p.Message_GetAncestors
	Message_Ancestors_              = p2p.Message_Ancestors_
	Message_Get                     = p2p.Message_Get
	Message_Put                     = p2p.Message_Put
	Message_PushQuery               = p2p.Message_PushQuery
	Message_PullQuery               = p2p.Message_PullQuery
	Message_Chits                   = p2p.Message_Chits
	Message_Request                 = p2p.Message_Request
	Message_Response                = p2p.Message_Response
	Message_Gossip                  = p2p.Message_Gossip
	Message_Error                   = p2p.Message_Error
	Message_BFT                 = p2p.Message_BFT
	Ping                    = p2p.Ping
	Pong                    = p2p.Pong
	SubnetUptime            = p2p.SubnetUptime
	Handshake               = p2p.Handshake
	Client                  = p2p.Client
	BloomFilter             = p2p.BloomFilter
	GetPeerList             = p2p.GetPeerList
	PeerList                = p2p.PeerList
	ClaimedIpPort           = p2p.ClaimedIpPort
	GetStateSummaryFrontier = p2p.GetStateSummaryFrontier
	StateSummaryFrontier    = p2p.StateSummaryFrontier
	GetAcceptedStateSummary = p2p.GetAcceptedStateSummary
	AcceptedStateSummary    = p2p.AcceptedStateSummary
	GetAcceptedFrontier     = p2p.GetAcceptedFrontier
	AcceptedFrontier        = p2p.AcceptedFrontier
	GetAccepted             = p2p.GetAccepted
	Accepted                = p2p.Accepted
	GetAncestors            = p2p.GetAncestors
	Ancestors               = p2p.Ancestors
	Get                     = p2p.Get
	Put                     = p2p.Put
	PushQuery               = p2p.PushQuery
	PullQuery               = p2p.PullQuery
	Chits                   = p2p.Chits
	Request                 = p2p.Request
	Response                = p2p.Response
	Gossip                  = p2p.Gossip
	Error                   = p2p.Error
	BFT                 = p2p.BFT
	BFT_BlockProposal       = p2p.BFT_BlockProposal
	BFT_Vote                = p2p.BFT_Vote
	BFT_EmptyVote           = p2p.BFT_EmptyVote
	BFT_FinalizeVote        = p2p.BFT_FinalizeVote
	BFT_Notarization        = p2p.BFT_Notarization
	BFT_EmptyNotarization   = p2p.BFT_EmptyNotarization
	BFT_Finalization        = p2p.BFT_Finalization
	BFT_ReplicationRequest  = p2p.BFT_ReplicationRequest
	BFT_ReplicationResponse = p2p.BFT_ReplicationResponse
	BlockProposal               = p2p.BlockProposal
	Vote                        = p2p.Vote
	EmptyVote                   = p2p.EmptyVote
	QuorumCertificate           = p2p.QuorumCertificate
	EmptyNotarization           = p2p.EmptyNotarization
	ReplicationRequest          = p2p.ReplicationRequest
	ReplicationResponse         = p2p.ReplicationResponse
)

// Re-export constants
const (
	EngineType_ENGINE_TYPE_UNSPECIFIED = p2p.EngineType_ENGINE_TYPE_UNSPECIFIED
	EngineType_ENGINE_TYPE_CHAIN       = p2p.EngineType_ENGINE_TYPE_CHAIN
	EngineType_ENGINE_TYPE_DAG         = p2p.EngineType_ENGINE_TYPE_DAG
)

// Re-export codec functions
var (
	Marshal   = p2p.Marshal
	Unmarshal = p2p.Unmarshal
	Size      = p2p.Size
)
