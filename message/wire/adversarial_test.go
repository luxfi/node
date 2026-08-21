// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wire

import (
	"encoding/hex"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// Everything in this file feeds the decoder bytes a peer chose rather than
// bytes the encoder produced. Unmarshal is reached before a peer has proved
// anything about itself, so its behaviour on hostile input is the security
// boundary of the node.

// sink keeps a decoded message reachable so the compiler cannot drop the work
// being measured.
var sink *Message

// allocatedBy reports the bytes f asks the allocator for. ReadMemStats stops
// the world, so the reading covers f and nothing else.
func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestACountFromAPeerCannotSizeAnAllocation.
//
// Every repeated field on the wire is a uint32 count followed by the elements.
// The count arrives from the peer; the elements do not have to. The invariant
// is that a decoder may only commit memory the frame in hand can account for
// -- the cheapest element in this format costs four bytes on the wire, so an
// N-byte frame can describe at most N/4 of anything.
//
// The counts below are held to 2^22 so the test is survivable. The field is a
// full uint32, so a peer may claim 4,294,967,295 and multiply every number
// here by 1024: a five-byte PeerList frame asks for 34GB of slice headers, and
// a thirteen-byte Accepted frame asks for 103GB. That is a remote out-of-memory
// kill of a validator, pre-handshake, from a frame that fits in a tweet.
func TestACountFromAPeerCannotSizeAnAllocation(t *testing.T) {
	const claimed = 1 << 22 // elements promised, none delivered

	frame := func(build func(*Buffer)) []byte {
		b := NewBuffer(64)
		build(b)
		return b.Bytes()
	}

	cases := []struct {
		name  string
		frame []byte
	}{
		{"Accepted.ContainerIds", frame(func(b *Buffer) {
			b.WriteUint8(tagAccepted)
			b.WriteBytes(nil) // chain id
			b.WriteUint32(7)  // request id
			b.WriteUint32(claimed)
		})},
		{"Ancestors.Containers", frame(func(b *Buffer) {
			b.WriteUint8(tagAncestors)
			b.WriteBytes(nil)
			b.WriteUint32(7)
			b.WriteUint32(claimed)
		})},
		{"GetAcceptedStateSummary.Heights", frame(func(b *Buffer) {
			b.WriteUint8(tagGetAcceptedStateSummary)
			b.WriteBytes(nil)
			b.WriteUint32(7)
			b.WriteUint64(9) // deadline
			b.WriteUint32(claimed)
		})},
		{"Ping.ChainIds", frame(func(b *Buffer) {
			b.WriteUint8(tagPing)
			b.WriteUint32(1) // uptime
			b.WriteUint32(claimed)
		})},
		{"Pong.ChainIds", frame(func(b *Buffer) {
			b.WriteUint8(tagPong)
			b.WriteUint32(1)
			b.WriteUint32(claimed)
		})},
		{"PeerList.ClaimedIpPorts", frame(func(b *Buffer) {
			b.WriteUint8(tagPeerList)
			b.WriteUint32(claimed)
		})},
		{"Handshake.SupportedAcps", frame(func(b *Buffer) {
			b.WriteUint8(tagHandshake)
			b.WriteUint32(1)  // network id
			b.WriteUint64(2)  // my time
			b.WriteBytes(nil) // ip
			b.WriteUint32(3)  // port
			b.WriteUint64(4)  // signing time
			b.WriteBytes(nil) // node id signature
			b.WriteUint32(0)  // no tracked chains
			b.WriteUint8(0)   // no client
			b.WriteUint32(claimed)
		})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// What the frame could honestly describe, with generous slack for
			// the container and the decode itself.
			budget := uint64(64<<10) + 64*uint64(len(tc.frame))

			used := allocatedBy(func() {
				sink, _ = Unmarshal(tc.frame)
			})

			require.LessOrEqualf(t, used, budget,
				"a %d-byte frame claiming %d elements allocated %d bytes (%.0fx amplification); "+
					"at the maximum count a peer may send this is %d bytes",
				len(tc.frame), claimed, used, float64(used)/float64(len(tc.frame)),
				used*(1<<32-1)/claimed)
		})
	}
}

// TestAFailedDecodeYieldsNoMessage.
//
// Unmarshal reports an error and a message. When the error is set the message
// must not be usable, because a caller that mishandles the error would then be
// routing a structurally valid message whose contents a peer never sent: the
// fields decoded before the failure carry the peer's values, and the fields
// after it are zero. A zero Deadline is an already-expired request, a zero
// EngineType is UNSPECIFIED, a zero RequestedHeight is genesis.
//
// The package already does this correctly in five places -- Ping, Pong,
// PeerList, GetPeerList and the compressed payload all hand back nothing on a
// failed decode -- and incorrectly in the rest. One rule, two doors.
func TestAFailedDecodeYieldsNoMessage(t *testing.T) {
	for _, k := range kinds() {
		t.Run(k.name, func(t *testing.T) {
			frame := hexFrame(t, k.frame)

			// Cut the last byte: every earlier field decodes from the peer's
			// bytes and the final read fails.
			m, err := Unmarshal(frame[:len(frame)-1])
			require.Error(t, err)
			if m == nil {
				return // nothing handed back at all, which is the strongest form
			}
			require.Falsef(t, k.set(m),
				"a failed decode handed back a populated %s: %+v", k.name, m)
		})
	}
}

// TestAcceptedFramesAreCanonical.
//
// One message, one encoding. The decoder must not accept a frame the encoder
// would never produce, because everything downstream measures a message by its
// bytes: compression, byte-rate accounting, and any identity taken over the
// frame. Where the decoder is lax a peer gets to send unbounded distinct
// encodings of the same message, and gets to hide bytes the node still pays to
// receive and to decompress.
//
// Each case below is a frame Unmarshal accepts today whose re-encoding differs
// from what came in.
func TestAcceptedFramesAreCanonical(t *testing.T) {
	// Refusing the frame settles the question; accepting it and then
	// re-encoding it differently does not.
	canonical := func(t *testing.T, frame []byte) {
		t.Helper()
		decoded, err := Unmarshal(frame)
		if err != nil {
			return
		}

		again, err := Marshal(decoded)
		require.NoError(t, err)
		require.Equal(t, hex.EncodeToString(frame), hex.EncodeToString(again),
			"the decoder accepted a frame the encoder would never produce")
	}

	// Bytes past the end of the message. Eight here to keep the diff readable;
	// nothing in the decoder caps it, so a peer may pad any message to any
	// length the transport will carry and the decoder never notices.
	t.Run("trailing bytes", func(t *testing.T) {
		frame, err := Marshal(&Message{Gossip: &Gossip{
			ChainId: []byte{0xaa}, Gossip: []byte{0x01},
		}})
		require.NoError(t, err)
		canonical(t, append(frame, make([]byte, 8)...))
	})

	// An optional field is introduced by a presence byte the encoder only ever
	// writes as 0 or 1. Every other value is silently read as absent.
	t.Run("presence byte is neither 0 nor 1", func(t *testing.T) {
		canonical(t, []byte{tagGetPeerList, 0x02})
	})

	// Ping and Pong put each chain id on the wire twice. The decoder reads the
	// first copy into the entry and then overwrites it with the second, so the
	// first copy is peer-controlled bytes that nothing ever looks at.
	t.Run("ping's duplicate chain ids disagree", func(t *testing.T) {
		b := NewBuffer(64)
		b.WriteUint8(tagPing)
		b.WriteUint32(1) // uptime
		b.WriteUint32(1) // one entry
		b.WriteBytes([]byte{0xaa})
		b.WriteBytes([]byte{0xbb})
		canonical(t, b.Bytes())
	})
}

// TestPingSpendsEveryChainIdTwice records the cost of that duplicate field.
// Ping and Pong are the steady-state traffic between every pair of peers, and
// the chain id list grows with the number of chains a node tracks, so the
// waste is per-chain, per-peer, per-ping.
func TestPingSpendsEveryChainIdTwice(t *testing.T) {
	chains := make([]*ChainPingEntry, 32)
	for i := range chains {
		chains[i] = &ChainPingEntry{ChainId: make([]byte, 32)}
	}

	frame, err := Marshal(&Message{Ping: &Ping{Uptime: 1, ChainIds: chains}})
	require.NoError(t, err)

	const header = 1 + 4 + 4      // tag, uptime, count
	const perEntry = 2 * (4 + 32) // length-prefixed chain id, twice over
	require.Len(t, frame, header+32*perEntry)

	// Half of that is the second copy, which the decoder overwrites the first
	// with. Whether the two copies are checked against each other is a
	// separate question, in TestAcceptedFramesAreCanonical.
	require.Equal(t, header+32*(4+32), len(frame)-32*(4+32),
		"half the frame is the duplicate")
}

// TestAnEngineTypeIsWhateverThePeerSays. The field goes out as a uint32 and
// comes back into an int32, unchecked against the three values the type
// defines. A peer setting the top bit gets a negative engine type, so any
// consumer switching on it needs a default arm and any consumer indexing on it
// needs a range check.
func TestAnEngineTypeIsWhateverThePeerSays(t *testing.T) {
	b := NewBuffer(64)
	b.WriteUint8(tagGet)
	b.WriteBytes([]byte{0xaa})
	b.WriteUint32(5)
	b.WriteUint64(9)
	b.WriteBytes([]byte{0xbb})
	b.WriteUint32(0xffffffff)

	m, err := Unmarshal(b.Bytes())
	require.NoError(t, err)
	require.Equal(t, EngineType(-1), m.Get.EngineType)
	require.Negative(t, int32(m.Get.EngineType))
	require.NotEqual(t, EngineType_CHAIN, m.Get.EngineType)
	require.NotEqual(t, EngineType_DAG, m.Get.EngineType)

	// And the round trip is stable, so a relay hands the same bad value on.
	again, err := Marshal(m)
	require.NoError(t, err)
	require.Equal(t, b.Bytes(), again)
}

// TestAnOversizedLengthPrefixIsRefused. The classic frame attack: claim a
// payload far larger than the bytes on hand and see whether the decoder tries
// to make it true.
func TestAnOversizedLengthPrefixIsRefused(t *testing.T) {
	for _, claimed := range []uint32{16, 1 << 20, 1 << 31, 0xffffffff} {
		b := NewBuffer(64)
		b.WriteUint8(tagPut)
		b.WriteBytes([]byte{0xaa})
		b.WriteUint32(5)
		b.WriteUint32(claimed) // container length, with two bytes actually present
		b.WriteUint8(0xc0)
		b.WriteUint8(0xde)

		used := allocatedBy(func() {
			var err error
			sink, err = Unmarshal(b.Bytes())
			require.Error(t, err, "claimed length %d was accepted", claimed)
		})
		require.Lessf(t, used, uint64(1<<20),
			"refusing a %d-byte claim allocated %d bytes", claimed, used)
	}
}

// TestDecodingIsNotRecursive. Nothing in the format nests, so no frame can
// drive the decoder deeper than one message. Pinned so that adding a nested
// field does not quietly introduce a stack-exhaustion path.
func TestDecodingIsNotRecursive(t *testing.T) {
	// A frame whose payload is a whole other frame decodes as opaque bytes.
	inner, err := Marshal(&Message{Ping: &Ping{Uptime: 1}})
	require.NoError(t, err)

	outer, err := Marshal(&Message{CompressedZstd: inner})
	require.NoError(t, err)

	m, err := Unmarshal(outer)
	require.NoError(t, err)
	require.Equal(t, inner, m.CompressedZstd)
	require.Nil(t, m.Ping, "the payload stays bytes; decoding does not descend into it")
}

// TestShortHostileFramesNeverPanic walks every tag against every body of up to
// three bytes drawn from the values that break decoders -- zero, one, the sign
// boundary, and all ones. A decode must refuse or return; it must never take
// the process down, because a peer that can panic the decoder can panic every
// node it connects to.
//
// Three bytes is not an arbitrary limit: no count field in the format is fully
// present in a frame this short, so the sweep cannot itself trip the
// unbounded allocation that TestACountFromAPeerCannotSizeAnAllocation covers.
func TestShortHostileFramesNeverPanic(t *testing.T) {
	edges := []byte{0x00, 0x01, 0x7f, 0x80, 0xff}

	bodies := [][]byte{{}}
	for range 3 {
		next := make([][]byte, 0, len(bodies)*len(edges))
		for _, b := range bodies {
			for _, e := range edges {
				next = append(next, append(append([]byte{}, b...), e))
			}
		}
		bodies = append(bodies, next...)
	}

	for tag := 0; tag < 256; tag++ {
		for _, body := range bodies {
			frame := append([]byte{byte(tag)}, body...)
			require.NotPanicsf(t, func() {
				sink, _ = Unmarshal(frame)
			}, "frame %x", frame)
		}
	}
}
