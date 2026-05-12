// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package p2p provides P2P message types for network communication.
// This is the ZAP-based implementation (zero protobuf).
package p2p

import "fmt"

// EngineType represents the consensus engine type
type EngineType int32

const (
	EngineType_ENGINE_TYPE_UNSPECIFIED EngineType = 0
	EngineType_ENGINE_TYPE_CHAIN       EngineType = 1
	EngineType_ENGINE_TYPE_DAG         EngineType = 2
)

func (e EngineType) String() string {
	switch e {
	case EngineType_ENGINE_TYPE_CHAIN:
		return "CHAIN"
	case EngineType_ENGINE_TYPE_DAG:
		return "DAG"
	default:
		return "UNSPECIFIED"
	}
}

// Message is the top-level P2P message container
type Message struct {
	Message isMessage_Message
}

type isMessage_Message interface {
	isMessage_Message()
}

type Message_CompressedZstd struct{ CompressedZstd []byte }
type Message_Ping struct{ Ping *Ping }
type Message_Pong struct{ Pong *Pong }
type Message_Handshake struct{ Handshake *Handshake }
type Message_GetPeerList struct{ GetPeerList *GetPeerList }
type Message_PeerList_ struct{ PeerList_ *PeerList }
type Message_GetStateSummaryFrontier struct{ GetStateSummaryFrontier *GetStateSummaryFrontier }
type Message_StateSummaryFrontier_ struct{ StateSummaryFrontier_ *StateSummaryFrontier }
type Message_GetAcceptedStateSummary struct{ GetAcceptedStateSummary *GetAcceptedStateSummary }
type Message_AcceptedStateSummary_ struct{ AcceptedStateSummary_ *AcceptedStateSummary }
type Message_GetAcceptedFrontier struct{ GetAcceptedFrontier *GetAcceptedFrontier }
type Message_AcceptedFrontier_ struct{ AcceptedFrontier_ *AcceptedFrontier }
type Message_GetAccepted struct{ GetAccepted *GetAccepted }
type Message_Accepted_ struct{ Accepted_ *Accepted }
type Message_GetAncestors struct{ GetAncestors *GetAncestors }
type Message_Ancestors_ struct{ Ancestors_ *Ancestors }
type Message_Get struct{ Get *Get }
type Message_Put struct{ Put *Put }
type Message_PushQuery struct{ PushQuery *PushQuery }
type Message_PullQuery struct{ PullQuery *PullQuery }
type Message_Chits struct{ Chits *Chits }
type Message_Request struct{ Request *Request }
type Message_Response struct{ Response *Response }
type Message_Gossip struct{ Gossip *Gossip }
type Message_Error struct{ Error *Error }
type Message_BFT struct{ BFT *BFT }

func (*Message_CompressedZstd) isMessage_Message()          {}
func (*Message_Ping) isMessage_Message()                    {}
func (*Message_Pong) isMessage_Message()                    {}
func (*Message_Handshake) isMessage_Message()               {}
func (*Message_GetPeerList) isMessage_Message()             {}
func (*Message_PeerList_) isMessage_Message()               {}
func (*Message_GetStateSummaryFrontier) isMessage_Message() {}
func (*Message_StateSummaryFrontier_) isMessage_Message()   {}
func (*Message_GetAcceptedStateSummary) isMessage_Message() {}
func (*Message_AcceptedStateSummary_) isMessage_Message()   {}
func (*Message_GetAcceptedFrontier) isMessage_Message()     {}
func (*Message_AcceptedFrontier_) isMessage_Message()       {}
func (*Message_GetAccepted) isMessage_Message()             {}
func (*Message_Accepted_) isMessage_Message()               {}
func (*Message_GetAncestors) isMessage_Message()            {}
func (*Message_Ancestors_) isMessage_Message()              {}
func (*Message_Get) isMessage_Message()                     {}
func (*Message_Put) isMessage_Message()                     {}
func (*Message_PushQuery) isMessage_Message()               {}
func (*Message_PullQuery) isMessage_Message()               {}
func (*Message_Chits) isMessage_Message()                   {}
func (*Message_Request) isMessage_Message()                 {}
func (*Message_Response) isMessage_Message()                {}
func (*Message_Gossip) isMessage_Message()                  {}
func (*Message_Error) isMessage_Message()                   {}
func (*Message_BFT) isMessage_Message()                 {}

func (m *Message) GetMessage() isMessage_Message      { return m.Message }
func (m *Message) GetCompressedZstd() []byte          { if x, ok := m.Message.(*Message_CompressedZstd); ok { return x.CompressedZstd }; return nil }
func (m *Message) GetPing() *Ping                     { if x, ok := m.Message.(*Message_Ping); ok { return x.Ping }; return nil }
func (m *Message) GetPong() *Pong                     { if x, ok := m.Message.(*Message_Pong); ok { return x.Pong }; return nil }
func (m *Message) GetHandshake() *Handshake           { if x, ok := m.Message.(*Message_Handshake); ok { return x.Handshake }; return nil }
func (m *Message) GetGetPeerList() *GetPeerList       { if x, ok := m.Message.(*Message_GetPeerList); ok { return x.GetPeerList }; return nil }
func (m *Message) GetPeerList() *PeerList             { if x, ok := m.Message.(*Message_PeerList_); ok { return x.PeerList_ }; return nil }
func (m *Message) GetGetStateSummaryFrontier() *GetStateSummaryFrontier { if x, ok := m.Message.(*Message_GetStateSummaryFrontier); ok { return x.GetStateSummaryFrontier }; return nil }
func (m *Message) GetStateSummaryFrontier() *StateSummaryFrontier { if x, ok := m.Message.(*Message_StateSummaryFrontier_); ok { return x.StateSummaryFrontier_ }; return nil }
func (m *Message) GetGetAcceptedStateSummary() *GetAcceptedStateSummary { if x, ok := m.Message.(*Message_GetAcceptedStateSummary); ok { return x.GetAcceptedStateSummary }; return nil }
func (m *Message) GetAcceptedStateSummary() *AcceptedStateSummary { if x, ok := m.Message.(*Message_AcceptedStateSummary_); ok { return x.AcceptedStateSummary_ }; return nil }
func (m *Message) GetGetAcceptedFrontier() *GetAcceptedFrontier { if x, ok := m.Message.(*Message_GetAcceptedFrontier); ok { return x.GetAcceptedFrontier }; return nil }
func (m *Message) GetAcceptedFrontier() *AcceptedFrontier { if x, ok := m.Message.(*Message_AcceptedFrontier_); ok { return x.AcceptedFrontier_ }; return nil }
func (m *Message) GetGetAccepted() *GetAccepted       { if x, ok := m.Message.(*Message_GetAccepted); ok { return x.GetAccepted }; return nil }
func (m *Message) GetAccepted() *Accepted             { if x, ok := m.Message.(*Message_Accepted_); ok { return x.Accepted_ }; return nil }
func (m *Message) GetGetAncestors() *GetAncestors     { if x, ok := m.Message.(*Message_GetAncestors); ok { return x.GetAncestors }; return nil }
func (m *Message) GetAncestors() *Ancestors           { if x, ok := m.Message.(*Message_Ancestors_); ok { return x.Ancestors_ }; return nil }
func (m *Message) GetGet() *Get                       { if x, ok := m.Message.(*Message_Get); ok { return x.Get }; return nil }
func (m *Message) GetPut() *Put                       { if x, ok := m.Message.(*Message_Put); ok { return x.Put }; return nil }
func (m *Message) GetPushQuery() *PushQuery           { if x, ok := m.Message.(*Message_PushQuery); ok { return x.PushQuery }; return nil }
func (m *Message) GetPullQuery() *PullQuery           { if x, ok := m.Message.(*Message_PullQuery); ok { return x.PullQuery }; return nil }
func (m *Message) GetChits() *Chits                   { if x, ok := m.Message.(*Message_Chits); ok { return x.Chits }; return nil }
func (m *Message) GetRequest() *Request               { if x, ok := m.Message.(*Message_Request); ok { return x.Request }; return nil }
func (m *Message) GetResponse() *Response             { if x, ok := m.Message.(*Message_Response); ok { return x.Response }; return nil }
func (m *Message) GetGossip() *Gossip                 { if x, ok := m.Message.(*Message_Gossip); ok { return x.Gossip }; return nil }
func (m *Message) GetError() *Error                   { if x, ok := m.Message.(*Message_Error); ok { return x.Error }; return nil }
func (m *Message) GetBFT() *BFT               { if x, ok := m.Message.(*Message_BFT); ok { return x.BFT }; return nil }
func (m *Message) Reset()                             { *m = Message{} }
func (m *Message) String() string                     { return fmt.Sprintf("%+v", m.Message) }

// Ping message
type Ping struct {
	Uptime        uint32
	SubnetUptimes []*SubnetUptime
}

func (m *Ping) GetUptime() uint32                 { return m.Uptime }
func (m *Ping) GetSubnetUptimes() []*SubnetUptime { return m.SubnetUptimes }
func (m *Ping) Reset()                            { *m = Ping{} }
func (m *Ping) String() string                    { return fmt.Sprintf("Ping{Uptime:%d}", m.Uptime) }

// SubnetUptime for ping/pong
type SubnetUptime struct {
	ChainId []byte
	Uptime   uint32
}

func (m *SubnetUptime) GetChainId() []byte { return m.ChainId }
func (m *SubnetUptime) GetUptime() uint32   { return m.Uptime }

// Pong message
type Pong struct {
	Uptime        uint32
	SubnetUptimes []*SubnetUptime
}

func (m *Pong) GetUptime() uint32                 { return m.Uptime }
func (m *Pong) GetSubnetUptimes() []*SubnetUptime { return m.SubnetUptimes }
func (m *Pong) Reset()                            { *m = Pong{} }
func (m *Pong) String() string                    { return fmt.Sprintf("Pong{Uptime:%d}", m.Uptime) }

// Handshake message
type Handshake struct {
	NetworkId     uint32
	MyTime        uint64
	IpAddr        []byte
	IpPort        uint32
	IpSigningTime uint64
	IpNodeIdSig   []byte
	TrackedNets   [][]byte
	Client        *Client
	SupportedLps  []uint32
	ObjectedLps   []uint32
	KnownPeers    *BloomFilter
	IpBlsSig      []byte
	AllChains     bool
	// IpMldsaSig is the FIPS 204 ML-DSA-65 signature over the canonical
	// SignedIP bytes. Append-only field on the gossip wire: legacy peers
	// that never set this send an empty bytes blob, which the receiver
	// treats as "classical-only" and refuses only when its chain runs
	// under a strict-PQ profile. New strict-PQ producers MUST populate
	// it; otherwise their gossip is rejected by strict-PQ verifiers.
	IpMldsaSig []byte
}

func (m *Handshake) GetNetworkId() uint32        { return m.NetworkId }
func (m *Handshake) GetMyTime() uint64           { return m.MyTime }
func (m *Handshake) GetIpAddr() []byte           { return m.IpAddr }
func (m *Handshake) GetIpPort() uint32           { return m.IpPort }
func (m *Handshake) GetIpSigningTime() uint64    { return m.IpSigningTime }
func (m *Handshake) GetIpNodeIdSig() []byte      { return m.IpNodeIdSig }
func (m *Handshake) GetTrackedNets() [][]byte    { return m.TrackedNets }
func (m *Handshake) GetClient() *Client          { return m.Client }
func (m *Handshake) GetSupportedLps() []uint32   { return m.SupportedLps }
func (m *Handshake) GetObjectedLps() []uint32    { return m.ObjectedLps }
func (m *Handshake) GetKnownPeers() *BloomFilter { return m.KnownPeers }
func (m *Handshake) GetIpBlsSig() []byte         { return m.IpBlsSig }
func (m *Handshake) GetAllChains() bool          { return m.AllChains }
func (m *Handshake) GetIpMldsaSig() []byte       { return m.IpMldsaSig }
func (m *Handshake) Reset()                     { *m = Handshake{} }
func (m *Handshake) String() string             { return fmt.Sprintf("Handshake{NetworkId:%d}", m.NetworkId) }

// Client info
type Client struct {
	Name  string
	Major uint32
	Minor uint32
	Patch uint32
}

func (m *Client) GetName() string  { return m.Name }
func (m *Client) GetMajor() uint32 { return m.Major }
func (m *Client) GetMinor() uint32 { return m.Minor }
func (m *Client) GetPatch() uint32 { return m.Patch }

// BloomFilter for peer discovery
type BloomFilter struct {
	Filter []byte
	Salt   []byte
}

func (m *BloomFilter) GetFilter() []byte { return m.Filter }
func (m *BloomFilter) GetSalt() []byte   { return m.Salt }

// GetPeerList message
type GetPeerList struct {
	KnownPeers *BloomFilter
	AllChains  bool
}

func (m *GetPeerList) GetKnownPeers() *BloomFilter { return m.KnownPeers }
func (m *GetPeerList) GetAllChains() bool          { return m.AllChains }
func (m *GetPeerList) Reset()                      { *m = GetPeerList{} }
func (m *GetPeerList) String() string              { return "GetPeerList{}" }

// PeerList message
type PeerList struct {
	ClaimedIpPorts []*ClaimedIpPort
}

func (m *PeerList) GetClaimedIpPorts() []*ClaimedIpPort { return m.ClaimedIpPorts }
func (m *PeerList) Reset()                              { *m = PeerList{} }
func (m *PeerList) String() string                      { return fmt.Sprintf("PeerList{count:%d}", len(m.ClaimedIpPorts)) }

// ClaimedIpPort in peer list
type ClaimedIpPort struct {
	X509Certificate []byte
	IpAddr          []byte
	IpPort          uint32
	Timestamp       uint64
	Signature       []byte
	TxId            []byte
}

func (m *ClaimedIpPort) GetX509Certificate() []byte { return m.X509Certificate }
func (m *ClaimedIpPort) GetIpAddr() []byte          { return m.IpAddr }
func (m *ClaimedIpPort) GetIpPort() uint32          { return m.IpPort }
func (m *ClaimedIpPort) GetTimestamp() uint64       { return m.Timestamp }
func (m *ClaimedIpPort) GetSignature() []byte       { return m.Signature }
func (m *ClaimedIpPort) GetTxId() []byte            { return m.TxId }

// GetStateSummaryFrontier message
type GetStateSummaryFrontier struct {
	ChainId   []byte
	RequestId uint32
	Deadline  uint64
}

func (m *GetStateSummaryFrontier) GetChainId() []byte   { return m.ChainId }
func (m *GetStateSummaryFrontier) GetRequestId() uint32 { return m.RequestId }
func (m *GetStateSummaryFrontier) GetDeadline() uint64  { return m.Deadline }
func (m *GetStateSummaryFrontier) Reset()               { *m = GetStateSummaryFrontier{} }
func (m *GetStateSummaryFrontier) String() string       { return fmt.Sprintf("GetStateSummaryFrontier{RequestId:%d}", m.RequestId) }

// StateSummaryFrontier message
type StateSummaryFrontier struct {
	ChainId   []byte
	RequestId uint32
	Summary   []byte
}

func (m *StateSummaryFrontier) GetChainId() []byte   { return m.ChainId }
func (m *StateSummaryFrontier) GetRequestId() uint32 { return m.RequestId }
func (m *StateSummaryFrontier) GetSummary() []byte   { return m.Summary }
func (m *StateSummaryFrontier) Reset()               { *m = StateSummaryFrontier{} }
func (m *StateSummaryFrontier) String() string       { return fmt.Sprintf("StateSummaryFrontier{RequestId:%d}", m.RequestId) }

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
func (m *GetAcceptedStateSummary) GetHeights() []uint64 { return m.Heights }
func (m *GetAcceptedStateSummary) Reset()               { *m = GetAcceptedStateSummary{} }
func (m *GetAcceptedStateSummary) String() string       { return fmt.Sprintf("GetAcceptedStateSummary{RequestId:%d}", m.RequestId) }

// AcceptedStateSummary message
type AcceptedStateSummary struct {
	ChainId    []byte
	RequestId  uint32
	SummaryIds [][]byte
}

func (m *AcceptedStateSummary) GetChainId() []byte      { return m.ChainId }
func (m *AcceptedStateSummary) GetRequestId() uint32    { return m.RequestId }
func (m *AcceptedStateSummary) GetSummaryIds() [][]byte { return m.SummaryIds }
func (m *AcceptedStateSummary) Reset()                  { *m = AcceptedStateSummary{} }
func (m *AcceptedStateSummary) String() string          { return fmt.Sprintf("AcceptedStateSummary{RequestId:%d}", m.RequestId) }

// GetAcceptedFrontier message
type GetAcceptedFrontier struct {
	ChainId    []byte
	RequestId  uint32
	Deadline   uint64
	EngineType EngineType
}

func (m *GetAcceptedFrontier) GetChainId() []byte        { return m.ChainId }
func (m *GetAcceptedFrontier) GetRequestId() uint32      { return m.RequestId }
func (m *GetAcceptedFrontier) GetDeadline() uint64       { return m.Deadline }
func (m *GetAcceptedFrontier) GetEngineType() EngineType { return m.EngineType }
func (m *GetAcceptedFrontier) Reset()                    { *m = GetAcceptedFrontier{} }
func (m *GetAcceptedFrontier) String() string            { return fmt.Sprintf("GetAcceptedFrontier{RequestId:%d}", m.RequestId) }

// AcceptedFrontier message
type AcceptedFrontier struct {
	ChainId     []byte
	RequestId   uint32
	ContainerId []byte
}

func (m *AcceptedFrontier) GetChainId() []byte     { return m.ChainId }
func (m *AcceptedFrontier) GetRequestId() uint32   { return m.RequestId }
func (m *AcceptedFrontier) GetContainerId() []byte { return m.ContainerId }
func (m *AcceptedFrontier) Reset()                 { *m = AcceptedFrontier{} }
func (m *AcceptedFrontier) String() string         { return fmt.Sprintf("AcceptedFrontier{RequestId:%d}", m.RequestId) }

// GetAccepted message
type GetAccepted struct {
	ChainId      []byte
	RequestId    uint32
	Deadline     uint64
	ContainerIds [][]byte
	EngineType   EngineType
}

func (m *GetAccepted) GetChainId() []byte        { return m.ChainId }
func (m *GetAccepted) GetRequestId() uint32      { return m.RequestId }
func (m *GetAccepted) GetDeadline() uint64       { return m.Deadline }
func (m *GetAccepted) GetContainerIds() [][]byte { return m.ContainerIds }
func (m *GetAccepted) GetEngineType() EngineType { return m.EngineType }
func (m *GetAccepted) Reset()                    { *m = GetAccepted{} }
func (m *GetAccepted) String() string            { return fmt.Sprintf("GetAccepted{RequestId:%d}", m.RequestId) }

// Accepted message
type Accepted struct {
	ChainId      []byte
	RequestId    uint32
	ContainerIds [][]byte
}

func (m *Accepted) GetChainId() []byte        { return m.ChainId }
func (m *Accepted) GetRequestId() uint32      { return m.RequestId }
func (m *Accepted) GetContainerIds() [][]byte { return m.ContainerIds }
func (m *Accepted) Reset()                    { *m = Accepted{} }
func (m *Accepted) String() string            { return fmt.Sprintf("Accepted{RequestId:%d}", m.RequestId) }

// GetAncestors message
type GetAncestors struct {
	ChainId     []byte
	RequestId   uint32
	Deadline    uint64
	ContainerId []byte
	EngineType  EngineType
}

func (m *GetAncestors) GetChainId() []byte        { return m.ChainId }
func (m *GetAncestors) GetRequestId() uint32      { return m.RequestId }
func (m *GetAncestors) GetDeadline() uint64       { return m.Deadline }
func (m *GetAncestors) GetContainerId() []byte    { return m.ContainerId }
func (m *GetAncestors) GetEngineType() EngineType { return m.EngineType }
func (m *GetAncestors) Reset()                    { *m = GetAncestors{} }
func (m *GetAncestors) String() string            { return fmt.Sprintf("GetAncestors{RequestId:%d}", m.RequestId) }

// Ancestors message
type Ancestors struct {
	ChainId    []byte
	RequestId  uint32
	Containers [][]byte
}

func (m *Ancestors) GetChainId() []byte      { return m.ChainId }
func (m *Ancestors) GetRequestId() uint32    { return m.RequestId }
func (m *Ancestors) GetContainers() [][]byte { return m.Containers }
func (m *Ancestors) Reset()                  { *m = Ancestors{} }
func (m *Ancestors) String() string          { return fmt.Sprintf("Ancestors{RequestId:%d}", m.RequestId) }

// Get message
type Get struct {
	ChainId     []byte
	RequestId   uint32
	Deadline    uint64
	ContainerId []byte
	EngineType  EngineType
}

func (m *Get) GetChainId() []byte        { return m.ChainId }
func (m *Get) GetRequestId() uint32      { return m.RequestId }
func (m *Get) GetDeadline() uint64       { return m.Deadline }
func (m *Get) GetContainerId() []byte    { return m.ContainerId }
func (m *Get) GetEngineType() EngineType { return m.EngineType }
func (m *Get) Reset()                    { *m = Get{} }
func (m *Get) String() string            { return fmt.Sprintf("Get{RequestId:%d}", m.RequestId) }

// Put message
type Put struct {
	ChainId    []byte
	RequestId  uint32
	Container  []byte
	EngineType EngineType
}

func (m *Put) GetChainId() []byte        { return m.ChainId }
func (m *Put) GetRequestId() uint32      { return m.RequestId }
func (m *Put) GetContainer() []byte      { return m.Container }
func (m *Put) GetEngineType() EngineType { return m.EngineType }
func (m *Put) Reset()                    { *m = Put{} }
func (m *Put) String() string            { return fmt.Sprintf("Put{RequestId:%d}", m.RequestId) }

// PushQuery message
type PushQuery struct {
	ChainId         []byte
	RequestId       uint32
	Deadline        uint64
	Container       []byte
	EngineType      EngineType
	RequestedHeight uint64
}

func (m *PushQuery) GetChainId() []byte         { return m.ChainId }
func (m *PushQuery) GetRequestId() uint32       { return m.RequestId }
func (m *PushQuery) GetDeadline() uint64        { return m.Deadline }
func (m *PushQuery) GetContainer() []byte       { return m.Container }
func (m *PushQuery) GetEngineType() EngineType  { return m.EngineType }
func (m *PushQuery) GetRequestedHeight() uint64 { return m.RequestedHeight }
func (m *PushQuery) Reset()                     { *m = PushQuery{} }
func (m *PushQuery) String() string             { return fmt.Sprintf("PushQuery{RequestId:%d}", m.RequestId) }

// PullQuery message
type PullQuery struct {
	ChainId         []byte
	RequestId       uint32
	Deadline        uint64
	ContainerId     []byte
	EngineType      EngineType
	RequestedHeight uint64
}

func (m *PullQuery) GetChainId() []byte         { return m.ChainId }
func (m *PullQuery) GetRequestId() uint32       { return m.RequestId }
func (m *PullQuery) GetDeadline() uint64        { return m.Deadline }
func (m *PullQuery) GetContainerId() []byte     { return m.ContainerId }
func (m *PullQuery) GetEngineType() EngineType  { return m.EngineType }
func (m *PullQuery) GetRequestedHeight() uint64 { return m.RequestedHeight }
func (m *PullQuery) Reset()                     { *m = PullQuery{} }
func (m *PullQuery) String() string             { return fmt.Sprintf("PullQuery{RequestId:%d}", m.RequestId) }

// Chits message
type Chits struct {
	ChainId             []byte
	RequestId           uint32
	PreferredId         []byte
	PreferredIdAtHeight []byte
	AcceptedId          []byte
	AcceptedHeight      uint64
}

func (m *Chits) GetChainId() []byte             { return m.ChainId }
func (m *Chits) GetRequestId() uint32           { return m.RequestId }
func (m *Chits) GetPreferredId() []byte         { return m.PreferredId }
func (m *Chits) GetPreferredIdAtHeight() []byte { return m.PreferredIdAtHeight }
func (m *Chits) GetAcceptedId() []byte          { return m.AcceptedId }
func (m *Chits) GetAcceptedHeight() uint64      { return m.AcceptedHeight }
func (m *Chits) Reset()                         { *m = Chits{} }
func (m *Chits) String() string                 { return fmt.Sprintf("Chits{RequestId:%d}", m.RequestId) }

// Request message (application-level)
type Request struct {
	ChainId   []byte
	RequestId uint32
	Deadline  uint64
	AppBytes  []byte
}

func (m *Request) GetChainId() []byte   { return m.ChainId }
func (m *Request) GetRequestId() uint32 { return m.RequestId }
func (m *Request) GetDeadline() uint64  { return m.Deadline }
func (m *Request) GetAppBytes() []byte  { return m.AppBytes }
func (m *Request) Reset()               { *m = Request{} }
func (m *Request) String() string       { return fmt.Sprintf("Request{RequestId:%d}", m.RequestId) }

// Response message (application-level)
type Response struct {
	ChainId   []byte
	RequestId uint32
	AppBytes  []byte
}

func (m *Response) GetChainId() []byte   { return m.ChainId }
func (m *Response) GetRequestId() uint32 { return m.RequestId }
func (m *Response) GetAppBytes() []byte  { return m.AppBytes }
func (m *Response) Reset()               { *m = Response{} }
func (m *Response) String() string       { return fmt.Sprintf("Response{RequestId:%d}", m.RequestId) }

// Gossip message (application-level)
type Gossip struct {
	ChainId  []byte
	AppBytes []byte
}

func (m *Gossip) GetChainId() []byte  { return m.ChainId }
func (m *Gossip) GetAppBytes() []byte { return m.AppBytes }
func (m *Gossip) Reset()              { *m = Gossip{} }
func (m *Gossip) String() string      { return "Gossip{}" }

// Error message (application-level error)
type Error struct {
	ChainId      []byte
	RequestId    uint32
	ErrorCode    int32
	ErrorMessage string
}

func (m *Error) GetChainId() []byte      { return m.ChainId }
func (m *Error) GetRequestId() uint32    { return m.RequestId }
func (m *Error) GetErrorCode() int32     { return m.ErrorCode }
func (m *Error) GetErrorMessage() string { return m.ErrorMessage }
func (m *Error) Reset()                  { *m = Error{} }
func (m *Error) String() string          { return fmt.Sprintf("Error{RequestId:%d,Code:%d}", m.RequestId, m.ErrorCode) }

// BFT message
type BFT struct {
	ChainId []byte
	Message isBFT_Message
}

type isBFT_Message interface {
	isBFT_Message()
}

type BFT_BlockProposal struct{ BlockProposal *BlockProposal }
type BFT_Vote struct{ Vote *Vote }
type BFT_EmptyVote struct{ EmptyVote *EmptyVote }
type BFT_FinalizeVote struct{ FinalizeVote *Vote }
type BFT_Notarization struct{ Notarization *QuorumCertificate }
type BFT_EmptyNotarization struct{ EmptyNotarization *EmptyNotarization }
type BFT_Finalization struct{ Finalization *QuorumCertificate }
type BFT_ReplicationRequest struct{ ReplicationRequest *ReplicationRequest }
type BFT_ReplicationResponse struct{ ReplicationResponse *ReplicationResponse }

func (*BFT_BlockProposal) isBFT_Message()         {}
func (*BFT_Vote) isBFT_Message()                  {}
func (*BFT_EmptyVote) isBFT_Message()             {}
func (*BFT_FinalizeVote) isBFT_Message()          {}
func (*BFT_Notarization) isBFT_Message()          {}
func (*BFT_EmptyNotarization) isBFT_Message()     {}
func (*BFT_Finalization) isBFT_Message()          {}
func (*BFT_ReplicationRequest) isBFT_Message()    {}
func (*BFT_ReplicationResponse) isBFT_Message()   {}

func (m *BFT) GetChainId() []byte           { return m.ChainId }
func (m *BFT) GetMessage() isBFT_Message { return m.Message }
func (m *BFT) GetBlockProposal() *BlockProposal { if x, ok := m.Message.(*BFT_BlockProposal); ok { return x.BlockProposal }; return nil }
func (m *BFT) GetVote() *Vote { if x, ok := m.Message.(*BFT_Vote); ok { return x.Vote }; return nil }
func (m *BFT) GetReplicationRequest() *ReplicationRequest { if x, ok := m.Message.(*BFT_ReplicationRequest); ok { return x.ReplicationRequest }; return nil }
func (m *BFT) GetReplicationResponse() *ReplicationResponse { if x, ok := m.Message.(*BFT_ReplicationResponse); ok { return x.ReplicationResponse }; return nil }
func (m *BFT) Reset()                       { *m = BFT{} }
func (m *BFT) String() string               { return "BFT{}" }

// BlockProposal message
type BlockProposal struct {
	Block []byte
}

func (m *BlockProposal) GetBlock() []byte { return m.Block }

// Vote message
type Vote struct {
	BlockHash []byte
	Signature []byte
}

func (m *Vote) GetBlockHash() []byte { return m.BlockHash }
func (m *Vote) GetSignature() []byte { return m.Signature }

// EmptyVote message
type EmptyVote struct {
	View      uint64
	Seq       uint64
	Signature []byte
}

// QuorumCertificate message
type QuorumCertificate struct {
	BlockHash           []byte
	View                uint64
	Seq                 uint64
	AggregatedSignature []byte
	Signers             []byte
}

// EmptyNotarization message
type EmptyNotarization struct {
	View                uint64
	Seq                 uint64
	AggregatedSignature []byte
	Signers             []byte
}

// ReplicationRequest message
type ReplicationRequest struct {
	Seqs        []uint64
	LatestRound uint64
}

func (m *ReplicationRequest) GetSeqs() []uint64      { return m.Seqs }
func (m *ReplicationRequest) GetLatestRound() uint64 { return m.LatestRound }

// ReplicationResponse message
type ReplicationResponse struct {
	Messages [][]byte
}
