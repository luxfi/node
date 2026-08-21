// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wire

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The wire format is the contract with every peer on the network, so the
// bytes themselves are the specification. The vectors below are written by
// hand from that specification -- tag byte, then fields in declaration order,
// every integer big-endian, every variable-length field prefixed by a
// big-endian uint32 count. Generating them from the encoder would only pin
// that the encoder agrees with itself; writing them out pins the format.

// hexFrame turns a spaced hex vector into the bytes a peer would put on the
// wire. Spacing in the vectors marks field boundaries and carries no meaning.
func hexFrame(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.NewReplacer(" ", "", "\n", "", "\t", "").Replace(s))
	require.NoError(t, err)
	return b
}

// kind is one message type: the value, the exact bytes it must produce, and a
// predicate reporting whether a decoded container carries this payload.
type kind struct {
	name  string
	tag   uint8
	msg   *Message
	frame string
	set   func(*Message) bool
}

// kinds covers every tag the format defines. Fields are populated with
// non-empty values so that a decode reproduces the original exactly: the
// format cannot tell a nil slice from an empty one (see
// TestNilAndEmptyBytesAreTheSameOnTheWire), so a nil field would come back as
// an empty one and defeat an equality check for the wrong reason.
func kinds() []kind {
	return []kind{
		{
			name: "CompressedZstd",
			tag:  tagCompressedZstd,
			msg:  &Message{CompressedZstd: []byte{0x28, 0xb5, 0x2f}},
			frame: "01" + // tag
				"00000003 28b52f", // payload
			set: func(m *Message) bool { return m.CompressedZstd != nil },
		},
		{
			name: "Ping",
			tag:  tagPing,
			msg: &Message{Ping: &Ping{
				Uptime:   99,
				ChainIds: []*ChainPingEntry{{ChainId: []byte{0xc0}}},
			}},
			frame: "02" + // tag
				"00000063" + // uptime 99
				"00000001" + // one entry
				"00000001 c0" + // chain id
				"00000001 c0", // chain id, a second time -- see TestPingSpendsEveryChainIdTwice
			set: func(m *Message) bool { return m.Ping != nil },
		},
		{
			name: "Pong",
			tag:  tagPong,
			msg: &Message{Pong: &Pong{
				Uptime:   99,
				ChainIds: []*ChainPingEntry{{ChainId: []byte{0xc0}}},
			}},
			frame: "03" +
				"00000063" +
				"00000001" +
				"00000001 c0" +
				"00000001 c0",
			set: func(m *Message) bool { return m.Pong != nil },
		},
		{
			name: "Handshake",
			tag:  tagHandshake,
			msg: &Message{Handshake: &Handshake{
				NetworkId:     1,
				MyTime:        2,
				IpAddr:        []byte{0x7f, 0x00, 0x00, 0x01},
				IpPort:        9651,
				IpSigningTime: 3,
				IpNodeIdSig:   []byte{0xa1},
				TrackedChains: [][]byte{{0xc1}, {0xc2}},
				Client:        &Client{Name: "lux", Major: 1, Minor: 36, Patch: 144},
				SupportedAcps: []uint32{23},
				ObjectedAcps:  []uint32{24},
				KnownPeers:    &BloomFilter{Filter: []byte{0xf1}, Salt: []byte{0x5a}},
				IpBlsSig:      []byte{0xb1},
			}},
			frame: "04" + // tag
				"00000001" + // network 1
				"0000000000000002" + // my time 2
				"00000004 7f000001" + // 127.0.0.1
				"000025b3" + // port 9651
				"0000000000000003" + // signing time 3
				"00000001 a1" + // node id signature
				"00000002 00000001 c1 00000001 c2" + // two tracked chains
				"01" + // client present
				"00000003 6c7578" + // "lux"
				"00000001 00000024 00000090" + // 1.36.144
				"00000001 00000017" + // supports ACP 23
				"00000001 00000018" + // objects to ACP 24
				"01" + // known-peers filter present
				"00000001 f1" + // filter
				"00000001 5a" + // salt
				"00000001 b1", // bls signature
			set: func(m *Message) bool { return m.Handshake != nil },
		},
		{
			// A peer that does not name itself and has no bloom filter yet.
			// Both optional fields collapse to a single zero byte, which is
			// the only thing distinguishing them from the vector above.
			name: "HandshakeWithoutOptionalFields",
			tag:  tagHandshake,
			msg: &Message{Handshake: &Handshake{
				NetworkId:     1,
				MyTime:        2,
				IpAddr:        []byte{0x7f, 0x00, 0x00, 0x01},
				IpPort:        9651,
				IpSigningTime: 3,
				IpNodeIdSig:   []byte{0xa1},
				TrackedChains: [][]byte{},
				SupportedAcps: []uint32{},
				ObjectedAcps:  []uint32{},
				IpBlsSig:      []byte{0xb1},
			}},
			frame: "04" +
				"00000001" +
				"0000000000000002" +
				"00000004 7f000001" +
				"000025b3" +
				"0000000000000003" +
				"00000001 a1" +
				"00000000" + // no tracked chains
				"00" + // no client
				"00000000" + // supports nothing
				"00000000" + // objects to nothing
				"00" + // no known-peers filter
				"00000001 b1",
			set: func(m *Message) bool { return m.Handshake != nil },
		},
		{
			name: "GetPeerList",
			tag:  tagGetPeerList,
			msg: &Message{GetPeerList: &GetPeerList{
				KnownPeers: &BloomFilter{Filter: []byte{0xf1}, Salt: []byte{0x5a}},
			}},
			frame: "05" +
				"01" + // filter present
				"00000001 f1" +
				"00000001 5a",
			set: func(m *Message) bool { return m.GetPeerList != nil },
		},
		{
			name:  "GetPeerListNoFilter",
			tag:   tagGetPeerList,
			msg:   &Message{GetPeerList: &GetPeerList{}},
			frame: "05" + "00", // filter absent, and the frame ends there
			set:   func(m *Message) bool { return m.GetPeerList != nil },
		},
		{
			name: "PeerList",
			tag:  tagPeerList,
			msg: &Message{PeerList: &PeerList{ClaimedIpPorts: []*ClaimedIpPort{{
				X509Certificate: []byte{0xce},
				IpAddr:          []byte{0x0a, 0x00, 0x00, 0x01},
				IpPort:          9651,
				Timestamp:       7,
				Signature:       []byte{0x51},
				TxId:            []byte{0x71},
			}}}},
			frame: "06" +
				"00000001" + // one claim
				"00000001 ce" + // certificate
				"00000004 0a000001" + // 10.0.0.1
				"000025b3" + // port 9651
				"0000000000000007" + // timestamp
				"00000001 51" + // signature
				"00000001 71", // tx id
			set: func(m *Message) bool { return m.PeerList != nil },
		},
		{
			name: "GetStateSummaryFrontier",
			tag:  tagGetStateSummaryFrontier,
			msg: &Message{GetStateSummaryFrontier: &GetStateSummaryFrontier{
				ChainId: []byte{0xaa}, RequestId: 5, Deadline: 9,
			}},
			frame: "07" + "00000001 aa" + "00000005" + "0000000000000009",
			set:   func(m *Message) bool { return m.GetStateSummaryFrontier != nil },
		},
		{
			name: "StateSummaryFrontier",
			tag:  tagStateSummaryFrontier,
			msg: &Message{StateSummaryFrontier: &StateSummaryFrontier{
				ChainId: []byte{0xaa}, RequestId: 5, Summary: []byte{0x55, 0x01},
			}},
			frame: "08" + "00000001 aa" + "00000005" + "00000002 5501",
			set:   func(m *Message) bool { return m.StateSummaryFrontier != nil },
		},
		{
			name: "GetAcceptedStateSummary",
			tag:  tagGetAcceptedStateSummary,
			msg: &Message{GetAcceptedStateSummary: &GetAcceptedStateSummary{
				ChainId: []byte{0xaa}, RequestId: 5, Deadline: 9, Heights: []uint64{10, 11},
			}},
			frame: "09" + "00000001 aa" + "00000005" + "0000000000000009" +
				"00000002 000000000000000a 000000000000000b",
			set: func(m *Message) bool { return m.GetAcceptedStateSummary != nil },
		},
		{
			name: "AcceptedStateSummary",
			tag:  tagAcceptedStateSummary,
			msg: &Message{AcceptedStateSummary: &AcceptedStateSummary{
				ChainId: []byte{0xaa}, RequestId: 5, SummaryIds: [][]byte{{0x51}, {0x52}},
			}},
			frame: "0a" + "00000001 aa" + "00000005" +
				"00000002 00000001 51 00000001 52",
			set: func(m *Message) bool { return m.AcceptedStateSummary != nil },
		},
		{
			name: "GetAcceptedFrontier",
			tag:  tagGetAcceptedFrontier,
			msg: &Message{GetAcceptedFrontier: &GetAcceptedFrontier{
				ChainId: []byte{0xaa}, RequestId: 5, Deadline: 9, EngineType: EngineType_CHAIN,
			}},
			frame: "0b" + "00000001 aa" + "00000005" + "0000000000000009" + "00000001",
			set:   func(m *Message) bool { return m.GetAcceptedFrontier != nil },
		},
		{
			name: "AcceptedFrontier",
			tag:  tagAcceptedFrontier,
			msg: &Message{AcceptedFrontier: &AcceptedFrontier{
				ChainId: []byte{0xaa}, RequestId: 5, ContainerId: []byte{0xbb},
			}},
			frame: "0c" + "00000001 aa" + "00000005" + "00000001 bb",
			set:   func(m *Message) bool { return m.AcceptedFrontier != nil },
		},
		{
			name: "GetAccepted",
			tag:  tagGetAccepted,
			msg: &Message{GetAccepted: &GetAccepted{
				ChainId: []byte{0xaa}, RequestId: 5, Deadline: 9,
				ContainerIds: [][]byte{{0xb1}, {0xb2}}, EngineType: EngineType_CHAIN,
			}},
			frame: "0d" + "00000001 aa" + "00000005" + "0000000000000009" +
				"00000002 00000001 b1 00000001 b2" + "00000001",
			set: func(m *Message) bool { return m.GetAccepted != nil },
		},
		{
			name: "Accepted",
			tag:  tagAccepted,
			msg: &Message{Accepted: &Accepted{
				ChainId: []byte{0xaa}, RequestId: 5, ContainerIds: [][]byte{{0xb1}, {0xb2}},
			}},
			frame: "0e" + "00000001 aa" + "00000005" +
				"00000002 00000001 b1 00000001 b2",
			set: func(m *Message) bool { return m.Accepted != nil },
		},
		{
			name: "GetAncestors",
			tag:  tagGetAncestors,
			msg: &Message{GetAncestors: &GetAncestors{
				ChainId: []byte{0xaa}, RequestId: 5, Deadline: 9,
				ContainerId: []byte{0xbb}, EngineType: EngineType_DAG,
			}},
			frame: "0f" + "00000001 aa" + "00000005" + "0000000000000009" +
				"00000001 bb" + "00000002",
			set: func(m *Message) bool { return m.GetAncestors != nil },
		},
		{
			name: "Ancestors",
			tag:  tagAncestors,
			msg: &Message{Ancestors: &Ancestors{
				ChainId: []byte{0xaa}, RequestId: 5, Containers: [][]byte{{0xb1}, {0xb2}},
			}},
			frame: "10" + "00000001 aa" + "00000005" +
				"00000002 00000001 b1 00000001 b2",
			set: func(m *Message) bool { return m.Ancestors != nil },
		},
		{
			name: "Get",
			tag:  tagGet,
			msg: &Message{Get: &Get{
				ChainId: []byte{0xaa}, RequestId: 5, Deadline: 9,
				ContainerId: []byte{0xbb}, EngineType: EngineType_CHAIN,
			}},
			frame: "11" + "00000001 aa" + "00000005" + "0000000000000009" +
				"00000001 bb" + "00000001",
			set: func(m *Message) bool { return m.Get != nil },
		},
		{
			name: "Put",
			tag:  tagPut,
			msg: &Message{Put: &Put{
				ChainId: []byte{0xaa}, RequestId: 5,
				Container: []byte{0xc0, 0xde}, EngineType: EngineType_CHAIN,
			}},
			frame: "12" + "00000001 aa" + "00000005" + "00000002 c0de" + "00000001",
			set:   func(m *Message) bool { return m.Put != nil },
		},
		{
			name: "PushQuery",
			tag:  tagPushQuery,
			msg: &Message{PushQuery: &PushQuery{
				ChainId: []byte{0xaa}, RequestId: 5, Deadline: 9,
				Container: []byte{0xc0, 0xde}, EngineType: EngineType_CHAIN, RequestedHeight: 42,
			}},
			frame: "13" + "00000001 aa" + "00000005" + "0000000000000009" +
				"00000002 c0de" + "00000001" + "000000000000002a",
			set: func(m *Message) bool { return m.PushQuery != nil },
		},
		{
			name: "PullQuery",
			tag:  tagPullQuery,
			msg: &Message{PullQuery: &PullQuery{
				ChainId: []byte{0xaa}, RequestId: 5, Deadline: 9,
				ContainerId: []byte{0xbb}, EngineType: EngineType_CHAIN, RequestedHeight: 42,
			}},
			frame: "14" + "00000001 aa" + "00000005" + "0000000000000009" +
				"00000001 bb" + "00000001" + "000000000000002a",
			set: func(m *Message) bool { return m.PullQuery != nil },
		},
		{
			name: "Chits",
			tag:  tagChits,
			msg: &Message{Chits: &Chits{
				ChainId: []byte{0xaa}, RequestId: 5,
				PreferredId:         []byte{0xd1},
				PreferredIdAtHeight: []byte{0xd2},
				AcceptedId:          []byte{0xd3},
			}},
			frame: "15" + "00000001 aa" + "00000005" +
				"00000001 d1" + "00000001 d2" + "00000001 d3",
			set: func(m *Message) bool { return m.Chits != nil },
		},
		{
			name: "Request",
			tag:  tagRequest,
			msg: &Message{Request: &Request{
				ChainId: []byte{0xaa}, RequestId: 5, Deadline: 9, Request: []byte{0xbe, 0xef},
			}},
			frame: "16" + "00000001 aa" + "00000005" + "0000000000000009" + "00000002 beef",
			set:   func(m *Message) bool { return m.Request != nil },
		},
		{
			name: "Response",
			tag:  tagResponse,
			msg: &Message{Response: &Response{
				ChainId: []byte{0xaa}, RequestId: 5, Response: []byte{0xbe, 0xef},
			}},
			frame: "17" + "00000001 aa" + "00000005" + "00000002 beef",
			set:   func(m *Message) bool { return m.Response != nil },
		},
		{
			name: "Gossip",
			tag:  tagGossip,
			msg: &Message{Gossip: &Gossip{
				ChainId: []byte{0xaa}, Gossip: []byte{0x60, 0x55},
			}},
			frame: "18" + "00000001 aa" + "00000002 6055",
			set:   func(m *Message) bool { return m.Gossip != nil },
		},
		{
			name: "BFT",
			tag:  tagBFT,
			msg: &Message{BFT: &BFT{
				ChainId: []byte{0xaa}, Message: []byte{0xbf, 0x70, 0x11},
			}},
			frame: "19" + "00000001 aa" + "00000003 bf7011",
			set:   func(m *Message) bool { return m.BFT != nil },
		},
	}
}

// TestEncodingMatchesTheSpecification pins the exact bytes. A round trip alone
// would still pass if the encoder and decoder agreed on the wrong thing --
// little-endian integers, a swapped field order, a shifted tag -- and every
// peer running a released build would then be unreachable.
func TestEncodingMatchesTheSpecification(t *testing.T) {
	for _, k := range kinds() {
		t.Run(k.name, func(t *testing.T) {
			want := hexFrame(t, k.frame)

			got, err := Marshal(k.msg)
			require.NoError(t, err)
			require.Equal(t, hex.EncodeToString(want), hex.EncodeToString(got))

			require.Equal(t, k.tag, want[0], "tag byte")

			back, err := Unmarshal(want)
			require.NoError(t, err)
			require.Equal(t, k.msg, back)
			require.True(t, k.set(back), "decoded container carries the payload")
		})
	}
}

// TestEveryTagIsReachable checks the table above is not quietly missing a
// message type: every tag the encoder can emit must appear in it.
func TestEveryTagIsReachable(t *testing.T) {
	seen := map[uint8]bool{}
	for _, k := range kinds() {
		seen[k.tag] = true
	}
	for tag := uint8(tagCompressedZstd); tag <= tagBFT; tag++ {
		require.True(t, seen[tag], "tag %d has no vector", tag)
	}
	require.Len(t, seen, tagBFT)
}

// TestNoStrictPrefixDecodes is the truncation sweep. Every byte of a frame is
// consumed by some read, so cutting a frame anywhere must be refused -- a peer
// must never be able to get a shorter frame accepted as a whole message.
func TestNoStrictPrefixDecodes(t *testing.T) {
	for _, k := range kinds() {
		t.Run(k.name, func(t *testing.T) {
			frame := hexFrame(t, k.frame)
			for cut := 0; cut < len(frame); cut++ {
				_, err := Unmarshal(frame[:cut])
				require.Error(t, err, "frame truncated to %d of %d bytes decoded", cut, len(frame))
			}
		})
	}
}

// TestDecodeThenEncodeReproducesTheFrame pins that decoding loses nothing the
// encoder puts on the wire. A field written but never read would survive a
// value round trip unnoticed; it cannot survive this one.
func TestDecodeThenEncodeReproducesTheFrame(t *testing.T) {
	for _, k := range kinds() {
		t.Run(k.name, func(t *testing.T) {
			frame := hexFrame(t, k.frame)

			decoded, err := Unmarshal(frame)
			require.NoError(t, err)

			again, err := Marshal(decoded)
			require.NoError(t, err)
			require.Equal(t, hex.EncodeToString(frame), hex.EncodeToString(again))
		})
	}
}

// TestUnknownTagIsRefused: the tag space is closed. Anything outside it is a
// peer speaking a format we do not have, not something to guess at.
func TestUnknownTagIsRefused(t *testing.T) {
	for tag := 0; tag < 256; tag++ {
		if tag >= tagCompressedZstd && tag <= tagBFT {
			continue
		}
		// A generous body so the refusal cannot be mistaken for a short read.
		frame := append([]byte{byte(tag)}, make([]byte, 64)...)
		_, err := Unmarshal(frame)
		require.ErrorIs(t, err, ErrUnknownTag, "tag %d", tag)
	}
}

func TestEmptyInputIsRefused(t *testing.T) {
	_, err := Unmarshal(nil)
	require.ErrorIs(t, err, ErrInvalidMessage)

	_, err = Unmarshal([]byte{})
	require.ErrorIs(t, err, ErrInvalidMessage)
}

// TestMarshalRefusesAnEmptyContainer: a container with no payload has no tag
// to write, so it must be refused rather than sent as a zero-length frame.
func TestMarshalRefusesAnEmptyContainer(t *testing.T) {
	out, err := Marshal(&Message{})
	require.ErrorIs(t, err, ErrInvalidMessage)
	require.Nil(t, out)
}

// TestPayloadPrecedenceIsFixed pins which payload wins when a container
// carries more than one. The order is what the encoder walks, and a peer's
// view of the message depends on it, so it is part of the contract.
func TestPayloadPrecedenceIsFixed(t *testing.T) {
	both := &Message{
		CompressedZstd: []byte{0x01},
		Ping:           &Ping{Uptime: 1},
		BFT:            &BFT{ChainId: []byte{0xaa}},
	}
	out, err := Marshal(both)
	require.NoError(t, err)
	require.Equal(t, uint8(tagCompressedZstd), out[0])

	noZstd := &Message{
		Ping: &Ping{Uptime: 1},
		BFT:  &BFT{ChainId: []byte{0xaa}},
	}
	out, err = Marshal(noZstd)
	require.NoError(t, err)
	require.Equal(t, uint8(tagPing), out[0])
}

// TestEngineTypeValues pins the numbers that go on the wire. The names are
// ours; the numbers are the peer's.
func TestEngineTypeValues(t *testing.T) {
	require.Equal(t, EngineType(0), EngineType_UNSPECIFIED)
	require.Equal(t, EngineType(1), EngineType_CHAIN)
	require.Equal(t, EngineType(2), EngineType_DAG)
}

// TestAccessorsReadTheFieldsTheyName. The routing layer picks a chain and a
// pending request off these, so an accessor returning the wrong field would
// deliver a response to the wrong chain.
func TestAccessorsReadTheFieldsTheyName(t *testing.T) {
	chain := []byte{0xaa}
	const req = uint32(5)
	const dl = uint64(9)

	t.Run("GetStateSummaryFrontier", func(t *testing.T) {
		m := &GetStateSummaryFrontier{ChainId: chain, RequestId: req, Deadline: dl}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
		require.Equal(t, dl, m.GetDeadline())
	})
	t.Run("StateSummaryFrontier", func(t *testing.T) {
		m := &StateSummaryFrontier{ChainId: chain, RequestId: req}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
	})
	t.Run("GetAcceptedStateSummary", func(t *testing.T) {
		m := &GetAcceptedStateSummary{ChainId: chain, RequestId: req, Deadline: dl}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
		require.Equal(t, dl, m.GetDeadline())
	})
	t.Run("AcceptedStateSummary", func(t *testing.T) {
		m := &AcceptedStateSummary{ChainId: chain, RequestId: req}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
	})
	t.Run("GetAcceptedFrontier", func(t *testing.T) {
		m := &GetAcceptedFrontier{ChainId: chain, RequestId: req, Deadline: dl}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
		require.Equal(t, dl, m.GetDeadline())
	})
	t.Run("AcceptedFrontier", func(t *testing.T) {
		m := &AcceptedFrontier{ChainId: chain, RequestId: req}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
	})
	t.Run("GetAccepted", func(t *testing.T) {
		m := &GetAccepted{ChainId: chain, RequestId: req, Deadline: dl}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
		require.Equal(t, dl, m.GetDeadline())
	})
	t.Run("Accepted", func(t *testing.T) {
		m := &Accepted{ChainId: chain, RequestId: req}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
	})
	t.Run("GetAncestors", func(t *testing.T) {
		m := &GetAncestors{ChainId: chain, RequestId: req, Deadline: dl, EngineType: EngineType_DAG}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
		require.Equal(t, dl, m.GetDeadline())
		require.Equal(t, EngineType_DAG, m.GetEngineType())
	})
	t.Run("Ancestors", func(t *testing.T) {
		m := &Ancestors{ChainId: chain, RequestId: req}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
	})
	t.Run("Get", func(t *testing.T) {
		m := &Get{ChainId: chain, RequestId: req, Deadline: dl}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
		require.Equal(t, dl, m.GetDeadline())
	})
	t.Run("Put", func(t *testing.T) {
		m := &Put{ChainId: chain, RequestId: req}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
	})
	t.Run("PushQuery", func(t *testing.T) {
		m := &PushQuery{ChainId: chain, RequestId: req, Deadline: dl}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
		require.Equal(t, dl, m.GetDeadline())
	})
	t.Run("PullQuery", func(t *testing.T) {
		m := &PullQuery{ChainId: chain, RequestId: req, Deadline: dl}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
		require.Equal(t, dl, m.GetDeadline())
	})
	t.Run("Chits", func(t *testing.T) {
		m := &Chits{ChainId: chain, RequestId: req}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
	})
	t.Run("Request", func(t *testing.T) {
		m := &Request{ChainId: chain, RequestId: req, Deadline: dl}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
		require.Equal(t, dl, m.GetDeadline())
	})
	t.Run("Response", func(t *testing.T) {
		m := &Response{ChainId: chain, RequestId: req}
		require.Equal(t, chain, m.GetChainId())
		require.Equal(t, req, m.GetRequestId())
	})
	t.Run("Gossip", func(t *testing.T) {
		require.Equal(t, chain, (&Gossip{ChainId: chain}).GetChainId())
	})
	t.Run("BFT", func(t *testing.T) {
		require.Equal(t, chain, (&BFT{ChainId: chain}).GetChainId())
	})
}
