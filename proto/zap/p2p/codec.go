// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"encoding/binary"
	"errors"
	"io"
)

// Message tags for ZAP encoding
const (
	tagCompressedZstd = 1
	tagPing           = 2
	tagPong           = 3
	tagHandshake      = 4
	tagGetPeerList    = 5
	tagPeerList       = 6
	tagGetStateSummaryFrontier = 7
	tagStateSummaryFrontier    = 8
	tagGetAcceptedStateSummary = 9
	tagAcceptedStateSummary    = 10
	tagGetAcceptedFrontier     = 11
	tagAcceptedFrontier        = 12
	tagGetAccepted             = 13
	tagAccepted                = 14
	tagGetAncestors            = 15
	tagAncestors               = 16
	tagGet                     = 17
	tagPut                     = 18
	tagPushQuery               = 19
	tagPullQuery               = 20
	tagChits                   = 21
	tagRequest                 = 22
	tagResponse                = 23
	tagGossip                  = 24
	tagError                   = 25
	tagBFT                 = 26
)

var (
	ErrInvalidMessage = errors.New("invalid wire message")
	ErrUnknownTag     = errors.New("unknown message tag")

)

// Buffer for zero-copy encoding
type Buffer struct {
	data   []byte
	offset int
}

func NewBuffer(size int) *Buffer {
	return &Buffer{data: make([]byte, size)}
}

func (b *Buffer) grow(n int) {
	if b.offset+n > len(b.data) {
		newData := make([]byte, (b.offset+n)*2)
		copy(newData, b.data[:b.offset])
		b.data = newData
	}
}

func (b *Buffer) WriteUint8(v uint8) {
	b.grow(1)
	b.data[b.offset] = v
	b.offset++
}

func (b *Buffer) WriteUint32(v uint32) {
	b.grow(4)
	binary.BigEndian.PutUint32(b.data[b.offset:], v)
	b.offset += 4
}

func (b *Buffer) WriteUint64(v uint64) {
	b.grow(8)
	binary.BigEndian.PutUint64(b.data[b.offset:], v)
	b.offset += 8
}

func (b *Buffer) WriteInt32(v int32) {
	b.WriteUint32(uint32(v))
}

func (b *Buffer) WriteBytes(data []byte) {
	b.WriteUint32(uint32(len(data)))
	b.grow(len(data))
	copy(b.data[b.offset:], data)
	b.offset += len(data)
}

func (b *Buffer) WriteString(s string) {
	b.WriteBytes([]byte(s))
}

func (b *Buffer) WriteBytesSlice(slices [][]byte) {
	b.WriteUint32(uint32(len(slices)))
	for _, s := range slices {
		b.WriteBytes(s)
	}
}

func (b *Buffer) WriteUint32Slice(vals []uint32) {
	b.WriteUint32(uint32(len(vals)))
	for _, v := range vals {
		b.WriteUint32(v)
	}
}

func (b *Buffer) WriteUint64Slice(vals []uint64) {
	b.WriteUint32(uint32(len(vals)))
	for _, v := range vals {
		b.WriteUint64(v)
	}
}

func (b *Buffer) Bytes() []byte {
	return b.data[:b.offset]
}

// Reader for zero-copy decoding
type Reader struct {
	data   []byte
	offset int
}

func NewReader(data []byte) *Reader {
	return &Reader{data: data}
}

// HasMore reports whether the reader has any unread bytes remaining. Used
// by append-only field readers (e.g. unmarshalHandshake's IpMldsaSig) to
// tell a legacy short frame apart from a new frame with a (possibly
// empty) trailing field.
func (r *Reader) HasMore() bool {
	return r.offset < len(r.data)
}

func (r *Reader) ReadUint8() (uint8, error) {
	if r.offset+1 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := r.data[r.offset]
	r.offset++
	return v, nil
}

func (r *Reader) ReadUint32() (uint32, error) {
	if r.offset+4 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint32(r.data[r.offset:])
	r.offset += 4
	return v, nil
}

func (r *Reader) ReadInt32() (int32, error) {
	v, err := r.ReadUint32()
	return int32(v), err
}

func (r *Reader) ReadUint64() (uint64, error) {
	if r.offset+8 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint64(r.data[r.offset:])
	r.offset += 8
	return v, nil
}

func (r *Reader) ReadBytes() ([]byte, error) {
	length, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	if r.offset+int(length) > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	data := r.data[r.offset : r.offset+int(length)]
	r.offset += int(length)
	return data, nil
}

func (r *Reader) ReadString() (string, error) {
	b, err := r.ReadBytes()
	return string(b), err
}

func (r *Reader) ReadBytesSlice() ([][]byte, error) {
	count, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	result := make([][]byte, count)
	for i := uint32(0); i < count; i++ {
		result[i], err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *Reader) ReadUint32Slice() ([]uint32, error) {
	count, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	result := make([]uint32, count)
	for i := uint32(0); i < count; i++ {
		result[i], err = r.ReadUint32()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *Reader) ReadUint64Slice() ([]uint64, error) {
	count, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	result := make([]uint64, count)
	for i := uint32(0); i < count; i++ {
		result[i], err = r.ReadUint64()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// Marshal encodes a Message to ZAP wire format
func Marshal(m *Message) ([]byte, error) {
	buf := NewBuffer(4096)

	switch {
	case m.GetCompressedZstd() != nil:
		buf.WriteUint8(tagCompressedZstd)
		buf.WriteBytes(m.GetCompressedZstd())
	case m.GetPing() != nil:
		buf.WriteUint8(tagPing)
		marshalPing(buf, m.GetPing())
	case m.GetPong() != nil:
		buf.WriteUint8(tagPong)
		marshalPong(buf, m.GetPong())
	case m.GetHandshake() != nil:
		buf.WriteUint8(tagHandshake)
		marshalHandshake(buf, m.GetHandshake())
	case m.GetGetPeerList() != nil:
		buf.WriteUint8(tagGetPeerList)
		marshalGetPeerList(buf, m.GetGetPeerList())
	case m.GetPeerList() != nil:
		buf.WriteUint8(tagPeerList)
		marshalPeerList(buf, m.GetPeerList())
	case m.GetGetStateSummaryFrontier() != nil:
		buf.WriteUint8(tagGetStateSummaryFrontier)
		marshalGetStateSummaryFrontier(buf, m.GetGetStateSummaryFrontier())
	case m.GetStateSummaryFrontier() != nil:
		buf.WriteUint8(tagStateSummaryFrontier)
		marshalStateSummaryFrontier(buf, m.GetStateSummaryFrontier())
	case m.GetGetAcceptedStateSummary() != nil:
		buf.WriteUint8(tagGetAcceptedStateSummary)
		marshalGetAcceptedStateSummary(buf, m.GetGetAcceptedStateSummary())
	case m.GetAcceptedStateSummary() != nil:
		buf.WriteUint8(tagAcceptedStateSummary)
		marshalAcceptedStateSummary(buf, m.GetAcceptedStateSummary())
	case m.GetGetAcceptedFrontier() != nil:
		buf.WriteUint8(tagGetAcceptedFrontier)
		marshalGetAcceptedFrontier(buf, m.GetGetAcceptedFrontier())
	case m.GetAcceptedFrontier() != nil:
		buf.WriteUint8(tagAcceptedFrontier)
		marshalAcceptedFrontier(buf, m.GetAcceptedFrontier())
	case m.GetGetAccepted() != nil:
		buf.WriteUint8(tagGetAccepted)
		marshalGetAccepted(buf, m.GetGetAccepted())
	case m.GetAccepted() != nil:
		buf.WriteUint8(tagAccepted)
		marshalAccepted(buf, m.GetAccepted())
	case m.GetGetAncestors() != nil:
		buf.WriteUint8(tagGetAncestors)
		marshalGetAncestors(buf, m.GetGetAncestors())
	case m.GetAncestors() != nil:
		buf.WriteUint8(tagAncestors)
		marshalAncestors(buf, m.GetAncestors())
	case m.GetGet() != nil:
		buf.WriteUint8(tagGet)
		marshalGet(buf, m.GetGet())
	case m.GetPut() != nil:
		buf.WriteUint8(tagPut)
		marshalPut(buf, m.GetPut())
	case m.GetPushQuery() != nil:
		buf.WriteUint8(tagPushQuery)
		marshalPushQuery(buf, m.GetPushQuery())
	case m.GetPullQuery() != nil:
		buf.WriteUint8(tagPullQuery)
		marshalPullQuery(buf, m.GetPullQuery())
	case m.GetChits() != nil:
		buf.WriteUint8(tagChits)
		marshalChits(buf, m.GetChits())
	case m.GetRequest() != nil:
		buf.WriteUint8(tagRequest)
		marshalRequest(buf, m.GetRequest())
	case m.GetResponse() != nil:
		buf.WriteUint8(tagResponse)
		marshalResponse(buf, m.GetResponse())
	case m.GetGossip() != nil:
		buf.WriteUint8(tagGossip)
		marshalGossip(buf, m.GetGossip())
	case m.GetError() != nil:
		buf.WriteUint8(tagError)
		marshalError(buf, m.GetError())
	case m.GetBFT() != nil:
		buf.WriteUint8(tagBFT)
		marshalBFT(buf, m.GetBFT())
	default:
		return nil, ErrInvalidMessage
	}

	return buf.Bytes(), nil
}

// Unmarshal decodes a Message from ZAP wire format
func Unmarshal(data []byte, m *Message) error {
	if len(data) < 1 {
		return ErrInvalidMessage
	}

	r := NewReader(data)
	tag, _ := r.ReadUint8()
	var err error

	switch tag {
	case tagCompressedZstd:
		var v []byte
		v, err = r.ReadBytes()
		m.Message = &Message_CompressedZstd{CompressedZstd: v}
	case tagPing:
		var v *Ping
		v, err = unmarshalPing(r)
		m.Message = &Message_Ping{Ping: v}
	case tagPong:
		var v *Pong
		v, err = unmarshalPong(r)
		m.Message = &Message_Pong{Pong: v}
	case tagHandshake:
		var v *Handshake
		v, err = unmarshalHandshake(r)
		m.Message = &Message_Handshake{Handshake: v}
	case tagGetPeerList:
		var v *GetPeerList
		v, err = unmarshalGetPeerList(r)
		m.Message = &Message_GetPeerList{GetPeerList: v}
	case tagPeerList:
		var v *PeerList
		v, err = unmarshalPeerList(r)
		m.Message = &Message_PeerList_{PeerList_: v}
	case tagGetStateSummaryFrontier:
		var v *GetStateSummaryFrontier
		v, err = unmarshalGetStateSummaryFrontier(r)
		m.Message = &Message_GetStateSummaryFrontier{GetStateSummaryFrontier: v}
	case tagStateSummaryFrontier:
		var v *StateSummaryFrontier
		v, err = unmarshalStateSummaryFrontier(r)
		m.Message = &Message_StateSummaryFrontier_{StateSummaryFrontier_: v}
	case tagGetAcceptedStateSummary:
		var v *GetAcceptedStateSummary
		v, err = unmarshalGetAcceptedStateSummary(r)
		m.Message = &Message_GetAcceptedStateSummary{GetAcceptedStateSummary: v}
	case tagAcceptedStateSummary:
		var v *AcceptedStateSummary
		v, err = unmarshalAcceptedStateSummary(r)
		m.Message = &Message_AcceptedStateSummary_{AcceptedStateSummary_: v}
	case tagGetAcceptedFrontier:
		var v *GetAcceptedFrontier
		v, err = unmarshalGetAcceptedFrontier(r)
		m.Message = &Message_GetAcceptedFrontier{GetAcceptedFrontier: v}
	case tagAcceptedFrontier:
		var v *AcceptedFrontier
		v, err = unmarshalAcceptedFrontier(r)
		m.Message = &Message_AcceptedFrontier_{AcceptedFrontier_: v}
	case tagGetAccepted:
		var v *GetAccepted
		v, err = unmarshalGetAccepted(r)
		m.Message = &Message_GetAccepted{GetAccepted: v}
	case tagAccepted:
		var v *Accepted
		v, err = unmarshalAccepted(r)
		m.Message = &Message_Accepted_{Accepted_: v}
	case tagGetAncestors:
		var v *GetAncestors
		v, err = unmarshalGetAncestors(r)
		m.Message = &Message_GetAncestors{GetAncestors: v}
	case tagAncestors:
		var v *Ancestors
		v, err = unmarshalAncestors(r)
		m.Message = &Message_Ancestors_{Ancestors_: v}
	case tagGet:
		var v *Get
		v, err = unmarshalGet(r)
		m.Message = &Message_Get{Get: v}
	case tagPut:
		var v *Put
		v, err = unmarshalPut(r)
		m.Message = &Message_Put{Put: v}
	case tagPushQuery:
		var v *PushQuery
		v, err = unmarshalPushQuery(r)
		m.Message = &Message_PushQuery{PushQuery: v}
	case tagPullQuery:
		var v *PullQuery
		v, err = unmarshalPullQuery(r)
		m.Message = &Message_PullQuery{PullQuery: v}
	case tagChits:
		var v *Chits
		v, err = unmarshalChits(r)
		m.Message = &Message_Chits{Chits: v}
	case tagRequest:
		var v *Request
		v, err = unmarshalRequest(r)
		m.Message = &Message_Request{Request: v}
	case tagResponse:
		var v *Response
		v, err = unmarshalResponse(r)
		m.Message = &Message_Response{Response: v}
	case tagGossip:
		var v *Gossip
		v, err = unmarshalGossip(r)
		m.Message = &Message_Gossip{Gossip: v}
	case tagError:
		var v *Error
		v, err = unmarshalError(r)
		m.Message = &Message_Error{Error: v}
	case tagBFT:
		var v *BFT
		v, err = unmarshalBFT(r)
		m.Message = &Message_BFT{BFT: v}
	default:
		return ErrUnknownTag
	}

	return err
}

// Size returns the encoded size of a message
func Size(m *Message) int {
	// Estimate - will be calculated during Marshal
	return 4096
}

// Marshal helpers - each message type
func marshalPing(b *Buffer, m *Ping) {
	b.WriteUint32(m.Uptime)
	b.WriteUint32(uint32(len(m.SubnetUptimes)))
	for _, s := range m.SubnetUptimes {
		b.WriteBytes(s.ChainId)
		b.WriteUint32(s.Uptime)
	}
}

func marshalPong(b *Buffer, m *Pong) {
	b.WriteUint32(m.Uptime)
	b.WriteUint32(uint32(len(m.SubnetUptimes)))
	for _, s := range m.SubnetUptimes {
		b.WriteBytes(s.ChainId)
		b.WriteUint32(s.Uptime)
	}
}

func marshalHandshake(b *Buffer, m *Handshake) {
	b.WriteUint32(m.NetworkId)
	b.WriteUint64(m.MyTime)
	b.WriteBytes(m.IpAddr)
	b.WriteUint32(m.IpPort)
	b.WriteUint64(m.IpSigningTime)
	b.WriteBytes(m.IpNodeIdSig)
	b.WriteBytesSlice(m.TrackedNets)
	if m.Client != nil {
		b.WriteUint8(1)
		b.WriteString(m.Client.Name)
		b.WriteUint32(m.Client.Major)
		b.WriteUint32(m.Client.Minor)
		b.WriteUint32(m.Client.Patch)
	} else {
		b.WriteUint8(0)
	}
	b.WriteUint32Slice(m.SupportedLps)
	b.WriteUint32Slice(m.ObjectedLps)
	if m.KnownPeers != nil {
		b.WriteUint8(1)
		b.WriteBytes(m.KnownPeers.Filter)
		b.WriteBytes(m.KnownPeers.Salt)
	} else {
		b.WriteUint8(0)
	}
	b.WriteBytes(m.IpBlsSig)
	// IpMldsaSig is append-only: legacy decoders that ran on the
	// pre-PQ wire format simply stop after IpBlsSig and the reader
	// returns io.ErrUnexpectedEOF when it tries to keep going.
	// New decoders (unmarshalHandshake below) check for trailing
	// bytes before reading IpMldsaSig, so emitting an empty slice
	// (4 zero length-prefix bytes) is a safe extension.
	b.WriteBytes(m.IpMldsaSig)
}

func marshalGetPeerList(b *Buffer, m *GetPeerList) {
	if m.KnownPeers != nil {
		b.WriteUint8(1)
		b.WriteBytes(m.KnownPeers.Filter)
		b.WriteBytes(m.KnownPeers.Salt)
	} else {
		b.WriteUint8(0)
	}
}

func marshalPeerList(b *Buffer, m *PeerList) {
	b.WriteUint32(uint32(len(m.ClaimedIpPorts)))
	for _, p := range m.ClaimedIpPorts {
		b.WriteBytes(p.X509Certificate)
		b.WriteBytes(p.IpAddr)
		b.WriteUint32(p.IpPort)
		b.WriteUint64(p.Timestamp)
		b.WriteBytes(p.Signature)
		b.WriteBytes(p.TxId)
	}
}

func marshalGetStateSummaryFrontier(b *Buffer, m *GetStateSummaryFrontier) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteUint64(m.Deadline)
}

func marshalStateSummaryFrontier(b *Buffer, m *StateSummaryFrontier) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteBytes(m.Summary)
}

func marshalGetAcceptedStateSummary(b *Buffer, m *GetAcceptedStateSummary) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteUint64(m.Deadline)
	b.WriteUint64Slice(m.Heights)
}

func marshalAcceptedStateSummary(b *Buffer, m *AcceptedStateSummary) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteBytesSlice(m.SummaryIds)
}

func marshalGetAcceptedFrontier(b *Buffer, m *GetAcceptedFrontier) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteUint64(m.Deadline)
	b.WriteUint32(uint32(m.EngineType))
}

func marshalAcceptedFrontier(b *Buffer, m *AcceptedFrontier) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteBytes(m.ContainerId)
}

func marshalGetAccepted(b *Buffer, m *GetAccepted) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteUint64(m.Deadline)
	b.WriteBytesSlice(m.ContainerIds)
	b.WriteUint32(uint32(m.EngineType))
}

func marshalAccepted(b *Buffer, m *Accepted) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteBytesSlice(m.ContainerIds)
}

func marshalGetAncestors(b *Buffer, m *GetAncestors) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteUint64(m.Deadline)
	b.WriteBytes(m.ContainerId)
	b.WriteUint32(uint32(m.EngineType))
}

func marshalAncestors(b *Buffer, m *Ancestors) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteBytesSlice(m.Containers)
}

func marshalGet(b *Buffer, m *Get) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteUint64(m.Deadline)
	b.WriteBytes(m.ContainerId)
	b.WriteUint32(uint32(m.EngineType))
}

func marshalPut(b *Buffer, m *Put) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteBytes(m.Container)
	b.WriteUint32(uint32(m.EngineType))
}

func marshalPushQuery(b *Buffer, m *PushQuery) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteUint64(m.Deadline)
	b.WriteBytes(m.Container)
	b.WriteUint32(uint32(m.EngineType))
	b.WriteUint64(m.RequestedHeight)
}

func marshalPullQuery(b *Buffer, m *PullQuery) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteUint64(m.Deadline)
	b.WriteBytes(m.ContainerId)
	b.WriteUint32(uint32(m.EngineType))
	b.WriteUint64(m.RequestedHeight)
}

func marshalChits(b *Buffer, m *Chits) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteBytes(m.PreferredId)
	b.WriteBytes(m.PreferredIdAtHeight)
	b.WriteBytes(m.AcceptedId)
}

func marshalRequest(b *Buffer, m *Request) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteUint64(m.Deadline)
	b.WriteBytes(m.AppBytes)
}

func marshalResponse(b *Buffer, m *Response) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteBytes(m.AppBytes)
}

func marshalGossip(b *Buffer, m *Gossip) {
	b.WriteBytes(m.ChainId)
	b.WriteBytes(m.AppBytes)
}

func marshalError(b *Buffer, m *Error) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteInt32(m.ErrorCode)
	b.WriteString(m.ErrorMessage)
}

func marshalBFT(b *Buffer, m *BFT) {
	b.WriteBytes(m.ChainId)
	// BFT message type tag + data
	switch msg := m.Message.(type) {
	case *BFT_BlockProposal:
		b.WriteUint8(1)
		b.WriteBytes(msg.BlockProposal.Block)
	case *BFT_Vote:
		b.WriteUint8(2)
		b.WriteBytes(msg.Vote.BlockHash)
		b.WriteBytes(msg.Vote.Signature)
	case *BFT_ReplicationRequest:
		b.WriteUint8(8)
		b.WriteUint64Slice(msg.ReplicationRequest.Seqs)
		b.WriteUint64(msg.ReplicationRequest.LatestRound)
	case *BFT_ReplicationResponse:
		b.WriteUint8(9)
		b.WriteBytesSlice(msg.ReplicationResponse.Messages)
	default:
		b.WriteUint8(0) // unknown/nil
	}
}

// Unmarshal helpers
func unmarshalPing(r *Reader) (*Ping, error) {
	m := &Ping{}
	var err error
	m.Uptime, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	count, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.SubnetUptimes = make([]*SubnetUptime, count)
	for i := uint32(0); i < count; i++ {
		s := &SubnetUptime{}
		s.ChainId, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		s.Uptime, err = r.ReadUint32()
		if err != nil {
			return nil, err
		}
		m.SubnetUptimes[i] = s
	}
	return m, nil
}

func unmarshalPong(r *Reader) (*Pong, error) {
	m := &Pong{}
	var err error
	m.Uptime, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	count, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.SubnetUptimes = make([]*SubnetUptime, count)
	for i := uint32(0); i < count; i++ {
		s := &SubnetUptime{}
		s.ChainId, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		s.Uptime, err = r.ReadUint32()
		if err != nil {
			return nil, err
		}
		m.SubnetUptimes[i] = s
	}
	return m, nil
}

func unmarshalHandshake(r *Reader) (*Handshake, error) {
	m := &Handshake{}
	var err error
	m.NetworkId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.MyTime, err = r.ReadUint64()
	if err != nil {
		return nil, err
	}
	m.IpAddr, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.IpPort, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.IpSigningTime, err = r.ReadUint64()
	if err != nil {
		return nil, err
	}
	m.IpNodeIdSig, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.TrackedNets, err = r.ReadBytesSlice()
	if err != nil {
		return nil, err
	}
	hasClient, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	if hasClient == 1 {
		m.Client = &Client{}
		m.Client.Name, err = r.ReadString()
		if err != nil {
			return nil, err
		}
		m.Client.Major, err = r.ReadUint32()
		if err != nil {
			return nil, err
		}
		m.Client.Minor, err = r.ReadUint32()
		if err != nil {
			return nil, err
		}
		m.Client.Patch, err = r.ReadUint32()
		if err != nil {
			return nil, err
		}
	}
	m.SupportedLps, err = r.ReadUint32Slice()
	if err != nil {
		return nil, err
	}
	m.ObjectedLps, err = r.ReadUint32Slice()
	if err != nil {
		return nil, err
	}
	hasKnownPeers, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	if hasKnownPeers == 1 {
		m.KnownPeers = &BloomFilter{}
		m.KnownPeers.Filter, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		m.KnownPeers.Salt, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
	}
	m.IpBlsSig, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	// IpMldsaSig is append-only on the wire. Legacy peers don't write it;
	// their handshake frame ends after IpBlsSig. New peers write a
	// length-prefixed blob (possibly empty). HasMore() lets us tell them
	// apart without breaking either case.
	if r.HasMore() {
		m.IpMldsaSig, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}

func unmarshalGetPeerList(r *Reader) (*GetPeerList, error) {
	m := &GetPeerList{}
	hasKnownPeers, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	if hasKnownPeers == 1 {
		m.KnownPeers = &BloomFilter{}
		m.KnownPeers.Filter, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		m.KnownPeers.Salt, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}

func unmarshalPeerList(r *Reader) (*PeerList, error) {
	m := &PeerList{}
	count, err := r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.ClaimedIpPorts = make([]*ClaimedIpPort, count)
	for i := uint32(0); i < count; i++ {
		p := &ClaimedIpPort{}
		p.X509Certificate, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		p.IpAddr, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		p.IpPort, err = r.ReadUint32()
		if err != nil {
			return nil, err
		}
		p.Timestamp, err = r.ReadUint64()
		if err != nil {
			return nil, err
		}
		p.Signature, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		p.TxId, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		m.ClaimedIpPorts[i] = p
	}
	return m, nil
}

func unmarshalGetStateSummaryFrontier(r *Reader) (*GetStateSummaryFrontier, error) {
	m := &GetStateSummaryFrontier{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Deadline, err = r.ReadUint64()
	return m, err
}

func unmarshalStateSummaryFrontier(r *Reader) (*StateSummaryFrontier, error) {
	m := &StateSummaryFrontier{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Summary, err = r.ReadBytes()
	return m, err
}

func unmarshalGetAcceptedStateSummary(r *Reader) (*GetAcceptedStateSummary, error) {
	m := &GetAcceptedStateSummary{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Deadline, err = r.ReadUint64()
	if err != nil {
		return nil, err
	}
	m.Heights, err = r.ReadUint64Slice()
	return m, err
}

func unmarshalAcceptedStateSummary(r *Reader) (*AcceptedStateSummary, error) {
	m := &AcceptedStateSummary{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.SummaryIds, err = r.ReadBytesSlice()
	return m, err
}

func unmarshalGetAcceptedFrontier(r *Reader) (*GetAcceptedFrontier, error) {
	m := &GetAcceptedFrontier{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Deadline, err = r.ReadUint64()
	if err != nil {
		return nil, err
	}
	et, err := r.ReadUint32()
	m.EngineType = EngineType(et)
	return m, err
}

func unmarshalAcceptedFrontier(r *Reader) (*AcceptedFrontier, error) {
	m := &AcceptedFrontier{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.ContainerId, err = r.ReadBytes()
	return m, err
}

func unmarshalGetAccepted(r *Reader) (*GetAccepted, error) {
	m := &GetAccepted{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Deadline, err = r.ReadUint64()
	if err != nil {
		return nil, err
	}
	m.ContainerIds, err = r.ReadBytesSlice()
	if err != nil {
		return nil, err
	}
	et, err := r.ReadUint32()
	m.EngineType = EngineType(et)
	return m, err
}

func unmarshalAccepted(r *Reader) (*Accepted, error) {
	m := &Accepted{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.ContainerIds, err = r.ReadBytesSlice()
	return m, err
}

func unmarshalGetAncestors(r *Reader) (*GetAncestors, error) {
	m := &GetAncestors{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Deadline, err = r.ReadUint64()
	if err != nil {
		return nil, err
	}
	m.ContainerId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	et, err := r.ReadUint32()
	m.EngineType = EngineType(et)
	return m, err
}

func unmarshalAncestors(r *Reader) (*Ancestors, error) {
	m := &Ancestors{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Containers, err = r.ReadBytesSlice()
	return m, err
}

func unmarshalGet(r *Reader) (*Get, error) {
	m := &Get{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Deadline, err = r.ReadUint64()
	if err != nil {
		return nil, err
	}
	m.ContainerId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	et, err := r.ReadUint32()
	m.EngineType = EngineType(et)
	return m, err
}

func unmarshalPut(r *Reader) (*Put, error) {
	m := &Put{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Container, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	et, err := r.ReadUint32()
	m.EngineType = EngineType(et)
	return m, err
}

func unmarshalPushQuery(r *Reader) (*PushQuery, error) {
	m := &PushQuery{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Deadline, err = r.ReadUint64()
	if err != nil {
		return nil, err
	}
	m.Container, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	et, err := r.ReadUint32()
	m.EngineType = EngineType(et)
	if err != nil {
		return nil, err
	}
	m.RequestedHeight, err = r.ReadUint64()
	return m, err
}

func unmarshalPullQuery(r *Reader) (*PullQuery, error) {
	m := &PullQuery{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Deadline, err = r.ReadUint64()
	if err != nil {
		return nil, err
	}
	m.ContainerId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	et, err := r.ReadUint32()
	m.EngineType = EngineType(et)
	if err != nil {
		return nil, err
	}
	m.RequestedHeight, err = r.ReadUint64()
	return m, err
}

func unmarshalChits(r *Reader) (*Chits, error) {
	m := &Chits{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.PreferredId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.PreferredIdAtHeight, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.AcceptedId, err = r.ReadBytes()
	return m, err
}

func unmarshalRequest(r *Reader) (*Request, error) {
	m := &Request{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.Deadline, err = r.ReadUint64()
	if err != nil {
		return nil, err
	}
	m.AppBytes, err = r.ReadBytes()
	return m, err
}

func unmarshalResponse(r *Reader) (*Response, error) {
	m := &Response{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.AppBytes, err = r.ReadBytes()
	return m, err
}

func unmarshalGossip(r *Reader) (*Gossip, error) {
	m := &Gossip{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.AppBytes, err = r.ReadBytes()
	return m, err
}

func unmarshalError(r *Reader) (*Error, error) {
	m := &Error{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.RequestId, err = r.ReadUint32()
	if err != nil {
		return nil, err
	}
	m.ErrorCode, err = r.ReadInt32()
	if err != nil {
		return nil, err
	}
	m.ErrorMessage, err = r.ReadString()
	return m, err
}

func unmarshalBFT(r *Reader) (*BFT, error) {
	m := &BFT{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	msgType, err := r.ReadUint8()
	if err != nil {
		return nil, err
	}
	switch msgType {
	case 1: // BlockProposal
		block, err := r.ReadBytes()
		if err != nil {
			return nil, err
		}
		m.Message = &BFT_BlockProposal{BlockProposal: &BlockProposal{Block: block}}
	case 2: // Vote
		hash, err := r.ReadBytes()
		if err != nil {
			return nil, err
		}
		sig, err := r.ReadBytes()
		if err != nil {
			return nil, err
		}
		m.Message = &BFT_Vote{Vote: &Vote{BlockHash: hash, Signature: sig}}
	case 8: // ReplicationRequest
		seqs, err := r.ReadUint64Slice()
		if err != nil {
			return nil, err
		}
		round, err := r.ReadUint64()
		if err != nil {
			return nil, err
		}
		m.Message = &BFT_ReplicationRequest{ReplicationRequest: &ReplicationRequest{Seqs: seqs, LatestRound: round}}
	case 9: // ReplicationResponse
		msgs, err := r.ReadBytesSlice()
		if err != nil {
			return nil, err
		}
		m.Message = &BFT_ReplicationResponse{ReplicationResponse: &ReplicationResponse{Messages: msgs}}
	}
	return m, nil
}
