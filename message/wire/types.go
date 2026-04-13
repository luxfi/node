// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package wire defines P2P message types for network communication.
// This package is wire-format agnostic - encoding is handled by the parent message package.
package wire

// EngineType represents the consensus engine type
type EngineType int32

const (
	EngineType_UNSPECIFIED EngineType = 0
	EngineType_CHAIN       EngineType = 1
	EngineType_DAG         EngineType = 2
)

// Message is the top-level P2P message container
type Message struct {
	// Only one of these should be set
	CompressedZstd              []byte
	Ping                        *Ping
	Pong                        *Pong
	Handshake                   *Handshake
	GetPeerList                 *GetPeerList
	PeerList                    *PeerList
	GetStateSummaryFrontier     *GetStateSummaryFrontier
	StateSummaryFrontier        *StateSummaryFrontier
	GetAcceptedStateSummary     *GetAcceptedStateSummary
	AcceptedStateSummary        *AcceptedStateSummary
	GetAcceptedFrontier         *GetAcceptedFrontier
	AcceptedFrontier            *AcceptedFrontier
	GetAccepted                 *GetAccepted
	Accepted                    *Accepted
	GetAncestors                *GetAncestors
	Ancestors                   *Ancestors
	Get                         *Get
	Put                         *Put
	PushQuery                   *PushQuery
	PullQuery                   *PullQuery
	Chits                       *Chits
	Request                     *Request
	Response                    *Response
	Gossip                      *Gossip
	BFT                     *BFT
}

// Ping message
type Ping struct {
	Uptime           uint32
	ChainSubnetPairs []*ChainSubnetPair
}

// ChainSubnetPair for ping
type ChainSubnetPair struct {
	ChainId  []byte
	SubnetId []byte
}

// Pong message
type Pong struct {
	Uptime           uint32
	ChainSubnetPairs []*ChainSubnetPair
}

// Handshake message
type Handshake struct {
	NetworkId      uint32
	MyTime         uint64
	IpAddr         []byte
	IpPort         uint32
	IpSigningTime  uint64
	IpNodeIdSig    []byte
	TrackedSubnets [][]byte
	Client         *Client
	SupportedAcps  []uint32
	ObjectedAcps   []uint32
	KnownPeers     *BloomFilter
	IpBlsSig       []byte
}

// Client info in handshake
type Client struct {
	Name  string
	Major uint32
	Minor uint32
	Patch uint32
}

// BloomFilter for peer discovery
type BloomFilter struct {
	Filter []byte
	Salt   []byte
}

// GetPeerList message
type GetPeerList struct {
	KnownPeers *BloomFilter
}

// PeerList message
type PeerList struct {
	ClaimedIpPorts []*ClaimedIpPort
}

// ClaimedIpPort in peer list
type ClaimedIpPort struct {
	X509Certificate []byte
	IpAddr          []byte
	IpPort          uint32
	Timestamp       uint64
	Signature       []byte
	TxId            []byte
}

// GetStateSummaryFrontier message
type GetStateSummaryFrontier struct {
	ChainId   []byte
	RequestId uint32
	Deadline  uint64
}

func (m *GetStateSummaryFrontier) GetChainId() []byte   { return m.ChainId }
func (m *GetStateSummaryFrontier) GetRequestId() uint32 { return m.RequestId }
func (m *GetStateSummaryFrontier) GetDeadline() uint64  { return m.Deadline }

// StateSummaryFrontier message
type StateSummaryFrontier struct {
	ChainId   []byte
	RequestId uint32
	Summary   []byte
}

func (m *StateSummaryFrontier) GetChainId() []byte   { return m.ChainId }
func (m *StateSummaryFrontier) GetRequestId() uint32 { return m.RequestId }

// GetAcceptedStateSummary message
type GetAcceptedStateSummary struct {
	ChainId   []byte
	RequestId uint32
	Deadline  uint64
	Heights   []uint64
}

func (m *GetAcceptedStateSummary) GetChainId() []byte   { return m.ChainId }
func (m *GetAcceptedStateSummary) GetRequestId() uint32 { return m.RequestId }
func (m *GetAcceptedStateSummary) GetDeadline() uint64  { return m.Deadline }

// AcceptedStateSummary message
type AcceptedStateSummary struct {
	ChainId    []byte
	RequestId  uint32
	SummaryIds [][]byte
}

func (m *AcceptedStateSummary) GetChainId() []byte   { return m.ChainId }
func (m *AcceptedStateSummary) GetRequestId() uint32 { return m.RequestId }

// GetAcceptedFrontier message
type GetAcceptedFrontier struct {
	ChainId    []byte
	RequestId  uint32
	Deadline   uint64
	EngineType EngineType
}

func (m *GetAcceptedFrontier) GetChainId() []byte   { return m.ChainId }
func (m *GetAcceptedFrontier) GetRequestId() uint32 { return m.RequestId }
func (m *GetAcceptedFrontier) GetDeadline() uint64  { return m.Deadline }

// AcceptedFrontier message
type AcceptedFrontier struct {
	ChainId     []byte
	RequestId   uint32
	ContainerId []byte
}

func (m *AcceptedFrontier) GetChainId() []byte   { return m.ChainId }
func (m *AcceptedFrontier) GetRequestId() uint32 { return m.RequestId }

// GetAccepted message
type GetAccepted struct {
	ChainId      []byte
	RequestId    uint32
	Deadline     uint64
	ContainerIds [][]byte
	EngineType   EngineType
}

func (m *GetAccepted) GetChainId() []byte   { return m.ChainId }
func (m *GetAccepted) GetRequestId() uint32 { return m.RequestId }
func (m *GetAccepted) GetDeadline() uint64  { return m.Deadline }

// Accepted message
type Accepted struct {
	ChainId      []byte
	RequestId    uint32
	ContainerIds [][]byte
}

func (m *Accepted) GetChainId() []byte   { return m.ChainId }
func (m *Accepted) GetRequestId() uint32 { return m.RequestId }

// GetAncestors message
type GetAncestors struct {
	ChainId     []byte
	RequestId   uint32
	Deadline    uint64
	ContainerId []byte
	EngineType  EngineType
}

func (m *GetAncestors) GetChainId() []byte      { return m.ChainId }
func (m *GetAncestors) GetRequestId() uint32    { return m.RequestId }
func (m *GetAncestors) GetDeadline() uint64     { return m.Deadline }
func (m *GetAncestors) GetEngineType() EngineType { return m.EngineType }

// Ancestors message
type Ancestors struct {
	ChainId    []byte
	RequestId  uint32
	Containers [][]byte
}

func (m *Ancestors) GetChainId() []byte   { return m.ChainId }
func (m *Ancestors) GetRequestId() uint32 { return m.RequestId }

// Get message
type Get struct {
	ChainId     []byte
	RequestId   uint32
	Deadline    uint64
	ContainerId []byte
	EngineType  EngineType
}

func (m *Get) GetChainId() []byte   { return m.ChainId }
func (m *Get) GetRequestId() uint32 { return m.RequestId }
func (m *Get) GetDeadline() uint64  { return m.Deadline }

// Put message
type Put struct {
	ChainId     []byte
	RequestId   uint32
	Container   []byte
	EngineType  EngineType
}

func (m *Put) GetChainId() []byte   { return m.ChainId }
func (m *Put) GetRequestId() uint32 { return m.RequestId }

// PushQuery message
type PushQuery struct {
	ChainId      []byte
	RequestId    uint32
	Deadline     uint64
	Container    []byte
	EngineType   EngineType
	RequestedHeight uint64
}

func (m *PushQuery) GetChainId() []byte   { return m.ChainId }
func (m *PushQuery) GetRequestId() uint32 { return m.RequestId }
func (m *PushQuery) GetDeadline() uint64  { return m.Deadline }

// PullQuery message
type PullQuery struct {
	ChainId         []byte
	RequestId       uint32
	Deadline        uint64
	ContainerId     []byte
	EngineType      EngineType
	RequestedHeight uint64
}

func (m *PullQuery) GetChainId() []byte   { return m.ChainId }
func (m *PullQuery) GetRequestId() uint32 { return m.RequestId }
func (m *PullQuery) GetDeadline() uint64  { return m.Deadline }

// Chits message
type Chits struct {
	ChainId        []byte
	RequestId      uint32
	PreferredId    []byte
	PreferredIdAtHeight []byte
	AcceptedId     []byte
}

func (m *Chits) GetChainId() []byte   { return m.ChainId }
func (m *Chits) GetRequestId() uint32 { return m.RequestId }

// Request message (app-level)
type Request struct {
	ChainId   []byte
	RequestId uint32
	Deadline  uint64
	Request   []byte
}

func (m *Request) GetChainId() []byte   { return m.ChainId }
func (m *Request) GetRequestId() uint32 { return m.RequestId }
func (m *Request) GetDeadline() uint64  { return m.Deadline }

// Response message (app-level)
type Response struct {
	ChainId   []byte
	RequestId uint32
	Response  []byte
}

func (m *Response) GetChainId() []byte   { return m.ChainId }
func (m *Response) GetRequestId() uint32 { return m.RequestId }

// Gossip message (app-level)
type Gossip struct {
	ChainId []byte
	Gossip  []byte
}

func (m *Gossip) GetChainId() []byte { return m.ChainId }

// BFT message
type BFT struct {
	ChainId []byte
	Message []byte
}

func (m *BFT) GetChainId() []byte { return m.ChainId }
