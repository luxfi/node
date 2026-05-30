// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wire

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	// Message type tags for ZAP encoding
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
	tagBFT                 = 25
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

func (b *Buffer) WriteUint16(v uint16) {
	b.grow(2)
	binary.BigEndian.PutUint16(b.data[b.offset:], v)
	b.offset += 2
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

func (b *Buffer) Reset() {
	b.offset = 0
}

// Reader for zero-copy decoding
type Reader struct {
	data   []byte
	offset int
}

func NewReader(data []byte) *Reader {
	return &Reader{data: data}
}

func (r *Reader) ReadUint8() (uint8, error) {
	if r.offset+1 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := r.data[r.offset]
	r.offset++
	return v, nil
}

func (r *Reader) ReadUint16() (uint16, error) {
	if r.offset+2 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.BigEndian.Uint16(r.data[r.offset:])
	r.offset += 2
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
	// Zero-copy: return slice into original buffer
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
	case m.CompressedZstd != nil:
		buf.WriteUint8(tagCompressedZstd)
		buf.WriteBytes(m.CompressedZstd)
	case m.Ping != nil:
		buf.WriteUint8(tagPing)
		marshalPing(buf, m.Ping)
	case m.Pong != nil:
		buf.WriteUint8(tagPong)
		marshalPong(buf, m.Pong)
	case m.Handshake != nil:
		buf.WriteUint8(tagHandshake)
		marshalHandshake(buf, m.Handshake)
	case m.GetPeerList != nil:
		buf.WriteUint8(tagGetPeerList)
		marshalGetPeerList(buf, m.GetPeerList)
	case m.PeerList != nil:
		buf.WriteUint8(tagPeerList)
		marshalPeerList(buf, m.PeerList)
	case m.GetStateSummaryFrontier != nil:
		buf.WriteUint8(tagGetStateSummaryFrontier)
		marshalGetStateSummaryFrontier(buf, m.GetStateSummaryFrontier)
	case m.StateSummaryFrontier != nil:
		buf.WriteUint8(tagStateSummaryFrontier)
		marshalStateSummaryFrontier(buf, m.StateSummaryFrontier)
	case m.GetAcceptedStateSummary != nil:
		buf.WriteUint8(tagGetAcceptedStateSummary)
		marshalGetAcceptedStateSummary(buf, m.GetAcceptedStateSummary)
	case m.AcceptedStateSummary != nil:
		buf.WriteUint8(tagAcceptedStateSummary)
		marshalAcceptedStateSummary(buf, m.AcceptedStateSummary)
	case m.GetAcceptedFrontier != nil:
		buf.WriteUint8(tagGetAcceptedFrontier)
		marshalGetAcceptedFrontier(buf, m.GetAcceptedFrontier)
	case m.AcceptedFrontier != nil:
		buf.WriteUint8(tagAcceptedFrontier)
		marshalAcceptedFrontier(buf, m.AcceptedFrontier)
	case m.GetAccepted != nil:
		buf.WriteUint8(tagGetAccepted)
		marshalGetAccepted(buf, m.GetAccepted)
	case m.Accepted != nil:
		buf.WriteUint8(tagAccepted)
		marshalAccepted(buf, m.Accepted)
	case m.GetAncestors != nil:
		buf.WriteUint8(tagGetAncestors)
		marshalGetAncestors(buf, m.GetAncestors)
	case m.Ancestors != nil:
		buf.WriteUint8(tagAncestors)
		marshalAncestors(buf, m.Ancestors)
	case m.Get != nil:
		buf.WriteUint8(tagGet)
		marshalGet(buf, m.Get)
	case m.Put != nil:
		buf.WriteUint8(tagPut)
		marshalPut(buf, m.Put)
	case m.PushQuery != nil:
		buf.WriteUint8(tagPushQuery)
		marshalPushQuery(buf, m.PushQuery)
	case m.PullQuery != nil:
		buf.WriteUint8(tagPullQuery)
		marshalPullQuery(buf, m.PullQuery)
	case m.Chits != nil:
		buf.WriteUint8(tagChits)
		marshalChits(buf, m.Chits)
	case m.Request != nil:
		buf.WriteUint8(tagRequest)
		marshalRequest(buf, m.Request)
	case m.Response != nil:
		buf.WriteUint8(tagResponse)
		marshalResponse(buf, m.Response)
	case m.Gossip != nil:
		buf.WriteUint8(tagGossip)
		marshalGossip(buf, m.Gossip)
	case m.BFT != nil:
		buf.WriteUint8(tagBFT)
		marshalBFT(buf, m.BFT)
	default:
		return nil, ErrInvalidMessage
	}

	return buf.Bytes(), nil
}

// Unmarshal decodes a Message from ZAP wire format
func Unmarshal(data []byte) (*Message, error) {
	if len(data) < 1 {
		return nil, ErrInvalidMessage
	}

	r := NewReader(data)
	tag, _ := r.ReadUint8()
	m := &Message{}
	var err error

	switch tag {
	case tagCompressedZstd:
		m.CompressedZstd, err = r.ReadBytes()
	case tagPing:
		m.Ping, err = unmarshalPing(r)
	case tagPong:
		m.Pong, err = unmarshalPong(r)
	case tagHandshake:
		m.Handshake, err = unmarshalHandshake(r)
	case tagGetPeerList:
		m.GetPeerList, err = unmarshalGetPeerList(r)
	case tagPeerList:
		m.PeerList, err = unmarshalPeerList(r)
	case tagGetStateSummaryFrontier:
		m.GetStateSummaryFrontier, err = unmarshalGetStateSummaryFrontier(r)
	case tagStateSummaryFrontier:
		m.StateSummaryFrontier, err = unmarshalStateSummaryFrontier(r)
	case tagGetAcceptedStateSummary:
		m.GetAcceptedStateSummary, err = unmarshalGetAcceptedStateSummary(r)
	case tagAcceptedStateSummary:
		m.AcceptedStateSummary, err = unmarshalAcceptedStateSummary(r)
	case tagGetAcceptedFrontier:
		m.GetAcceptedFrontier, err = unmarshalGetAcceptedFrontier(r)
	case tagAcceptedFrontier:
		m.AcceptedFrontier, err = unmarshalAcceptedFrontier(r)
	case tagGetAccepted:
		m.GetAccepted, err = unmarshalGetAccepted(r)
	case tagAccepted:
		m.Accepted, err = unmarshalAccepted(r)
	case tagGetAncestors:
		m.GetAncestors, err = unmarshalGetAncestors(r)
	case tagAncestors:
		m.Ancestors, err = unmarshalAncestors(r)
	case tagGet:
		m.Get, err = unmarshalGet(r)
	case tagPut:
		m.Put, err = unmarshalPut(r)
	case tagPushQuery:
		m.PushQuery, err = unmarshalPushQuery(r)
	case tagPullQuery:
		m.PullQuery, err = unmarshalPullQuery(r)
	case tagChits:
		m.Chits, err = unmarshalChits(r)
	case tagRequest:
		m.Request, err = unmarshalRequest(r)
	case tagResponse:
		m.Response, err = unmarshalResponse(r)
	case tagGossip:
		m.Gossip, err = unmarshalGossip(r)
	case tagBFT:
		m.BFT, err = unmarshalBFT(r)
	default:
		return nil, ErrUnknownTag
	}

	return m, err
}

// Marshal helpers
func marshalPing(b *Buffer, m *Ping) {
	b.WriteUint32(m.Uptime)
	b.WriteUint32(uint32(len(m.ChainIds)))
	for _, p := range m.ChainIds {
		b.WriteBytes(p.ChainId)
		b.WriteBytes(p.ChainId)
	}
}

func marshalPong(b *Buffer, m *Pong) {
	b.WriteUint32(m.Uptime)
	b.WriteUint32(uint32(len(m.ChainIds)))
	for _, p := range m.ChainIds {
		b.WriteBytes(p.ChainId)
		b.WriteBytes(p.ChainId)
	}
}

func marshalHandshake(b *Buffer, m *Handshake) {
	b.WriteUint32(m.NetworkId)
	b.WriteUint64(m.MyTime)
	b.WriteBytes(m.IpAddr)
	b.WriteUint32(m.IpPort)
	b.WriteUint64(m.IpSigningTime)
	b.WriteBytes(m.IpNodeIdSig)
	b.WriteBytesSlice(m.TrackedChains)
	if m.Client != nil {
		b.WriteUint8(1)
		b.WriteString(m.Client.Name)
		b.WriteUint32(m.Client.Major)
		b.WriteUint32(m.Client.Minor)
		b.WriteUint32(m.Client.Patch)
	} else {
		b.WriteUint8(0)
	}
	b.WriteUint32Slice(m.SupportedAcps)
	b.WriteUint32Slice(m.ObjectedAcps)
	if m.KnownPeers != nil {
		b.WriteUint8(1)
		b.WriteBytes(m.KnownPeers.Filter)
		b.WriteBytes(m.KnownPeers.Salt)
	} else {
		b.WriteUint8(0)
	}
	b.WriteBytes(m.IpBlsSig)
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
	b.WriteBytes(m.Request)
}

func marshalResponse(b *Buffer, m *Response) {
	b.WriteBytes(m.ChainId)
	b.WriteUint32(m.RequestId)
	b.WriteBytes(m.Response)
}

func marshalGossip(b *Buffer, m *Gossip) {
	b.WriteBytes(m.ChainId)
	b.WriteBytes(m.Gossip)
}

func marshalBFT(b *Buffer, m *BFT) {
	b.WriteBytes(m.ChainId)
	b.WriteBytes(m.Message)
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
	m.ChainIds = make([]*ChainPingEntry, count)
	for i := uint32(0); i < count; i++ {
		p := &ChainPingEntry{}
		p.ChainId, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		p.ChainId, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		m.ChainIds[i] = p
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
	m.ChainIds = make([]*ChainPingEntry, count)
	for i := uint32(0); i < count; i++ {
		p := &ChainPingEntry{}
		p.ChainId, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		p.ChainId, err = r.ReadBytes()
		if err != nil {
			return nil, err
		}
		m.ChainIds[i] = p
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
	m.TrackedChains, err = r.ReadBytesSlice()
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
	m.SupportedAcps, err = r.ReadUint32Slice()
	if err != nil {
		return nil, err
	}
	m.ObjectedAcps, err = r.ReadUint32Slice()
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
	return m, err
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
	m.Request, err = r.ReadBytes()
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
	m.Response, err = r.ReadBytes()
	return m, err
}

func unmarshalGossip(r *Reader) (*Gossip, error) {
	m := &Gossip{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.Gossip, err = r.ReadBytes()
	return m, err
}

func unmarshalBFT(r *Reader) (*BFT, error) {
	m := &BFT{}
	var err error
	m.ChainId, err = r.ReadBytes()
	if err != nil {
		return nil, err
	}
	m.Message, err = r.ReadBytes()
	return m, err
}
