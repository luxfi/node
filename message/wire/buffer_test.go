// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wire

import (
	"bytes"
	"encoding/hex"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIntegersAreBigEndian. Every width, pinned by the bytes rather than by a
// round trip, because a round trip agrees with itself in either byte order.
func TestIntegersAreBigEndian(t *testing.T) {
	b := NewBuffer(0)
	b.WriteUint8(0x01)
	b.WriteUint16(0x0203)
	b.WriteUint32(0x04050607)
	b.WriteUint64(0x08090a0b0c0d0e0f)
	require.Equal(t, "0102030405060708090a0b0c0d0e0f", hex.EncodeToString(b.Bytes()))

	r := NewReader(b.Bytes())

	v8, err := r.ReadUint8()
	require.NoError(t, err)
	require.Equal(t, uint8(0x01), v8)

	v16, err := r.ReadUint16()
	require.NoError(t, err)
	require.Equal(t, uint16(0x0203), v16)

	v32, err := r.ReadUint32()
	require.NoError(t, err)
	require.Equal(t, uint32(0x04050607), v32)

	v64, err := r.ReadUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(0x08090a0b0c0d0e0f), v64)

	require.Equal(t, len(b.Bytes()), r.offset, "reads consume exactly what writes produced")
}

// TestExtremeIntegersSurvive: the top bit of every width has to make it
// through untouched. A signed conversion anywhere in the path shows up here.
func TestExtremeIntegersSurvive(t *testing.T) {
	b := NewBuffer(0)
	b.WriteUint8(math.MaxUint8)
	b.WriteUint16(math.MaxUint16)
	b.WriteUint32(math.MaxUint32)
	b.WriteUint64(math.MaxUint64)

	r := NewReader(b.Bytes())

	v8, err := r.ReadUint8()
	require.NoError(t, err)
	require.Equal(t, uint8(math.MaxUint8), v8)

	v16, err := r.ReadUint16()
	require.NoError(t, err)
	require.Equal(t, uint16(math.MaxUint16), v16)

	v32, err := r.ReadUint32()
	require.NoError(t, err)
	require.Equal(t, uint32(math.MaxUint32), v32)

	v64, err := r.ReadUint64()
	require.NoError(t, err)
	require.Equal(t, uint64(math.MaxUint64), v64)
}

// TestBufferGrowsWithoutLosingBytes. The buffer starts at whatever size the
// caller guessed; a message larger than the guess must come out whole, not
// truncated at the seam.
func TestBufferGrowsWithoutLosingBytes(t *testing.T) {
	for _, start := range []int{0, 1, 7, 4096} {
		big := make([]byte, 100_000)
		for i := range big {
			big[i] = byte(i)
		}

		b := NewBuffer(start)
		b.WriteUint8(0xff)
		b.WriteBytes(big)
		b.WriteUint64(math.MaxUint64)

		out := b.Bytes()
		require.Len(t, out, 1+4+len(big)+8)

		r := NewReader(out)
		lead, err := r.ReadUint8()
		require.NoError(t, err)
		require.Equal(t, uint8(0xff), lead)

		got, err := r.ReadBytes()
		require.NoError(t, err)
		require.True(t, bytes.Equal(big, got), "payload survived the growth at start size %d", start)

		tail, err := r.ReadUint64()
		require.NoError(t, err)
		require.Equal(t, uint64(math.MaxUint64), tail)
		require.Equal(t, len(out), r.offset)
	}
}

// TestBufferBytesStopsAtTheWriteHead. NewBuffer preallocates; Bytes must hand
// back only what was written, never the zero padding behind it -- padding on
// the wire is a frame the peer cannot parse.
func TestBufferBytesStopsAtTheWriteHead(t *testing.T) {
	b := NewBuffer(4096)
	b.WriteUint32(0xdeadbeef)
	require.Equal(t, "deadbeef", hex.EncodeToString(b.Bytes()))
}

func TestBufferResetRewindsTheWriteHead(t *testing.T) {
	b := NewBuffer(16)
	b.WriteUint32(0xdeadbeef)
	b.Reset()
	require.Empty(t, b.Bytes())

	b.WriteUint16(0x0102)
	require.Equal(t, "0102", hex.EncodeToString(b.Bytes()))
}

// TestReadsPastTheEndAreRefused. Short reads are the ordinary case on a
// truncated frame, and every width must refuse rather than read whatever the
// previous message left in the buffer.
func TestReadsPastTheEndAreRefused(t *testing.T) {
	reads := map[string]struct {
		width int
		read  func(*Reader) error
	}{
		"uint8":  {1, func(r *Reader) error { _, err := r.ReadUint8(); return err }},
		"uint16": {2, func(r *Reader) error { _, err := r.ReadUint16(); return err }},
		"uint32": {4, func(r *Reader) error { _, err := r.ReadUint32(); return err }},
		"uint64": {8, func(r *Reader) error { _, err := r.ReadUint64(); return err }},
	}

	for name, tc := range reads {
		t.Run(name, func(t *testing.T) {
			for short := 0; short < tc.width; short++ {
				r := NewReader(make([]byte, short))
				require.ErrorIs(t, tc.read(r), io.ErrUnexpectedEOF, "%d bytes available", short)
				require.Zero(t, r.offset, "a refused read consumes nothing")
			}
			// Exactly enough succeeds: the refusal is about the boundary, not
			// about being pessimistic.
			r := NewReader(make([]byte, tc.width))
			require.NoError(t, tc.read(r))
			require.Equal(t, tc.width, r.offset)
		})
	}
}

// TestReadBytesStopsAtTheEndOfTheFrame. The length prefix comes from the peer,
// so the one thing that must hold is that it cannot reach past the bytes the
// peer actually sent.
func TestReadBytesStopsAtTheEndOfTheFrame(t *testing.T) {
	payload := []byte{0x11, 0x22, 0x33}

	// A length that names exactly the remaining bytes is the legitimate case.
	b := NewBuffer(0)
	b.WriteBytes(payload)
	got, err := NewReader(b.Bytes()).ReadBytes()
	require.NoError(t, err)
	require.Equal(t, payload, got)

	// One byte further than the frame goes is not.
	overrun := append([]byte{}, b.Bytes()...)
	overrun[3] = byte(len(payload) + 1)
	_, err = NewReader(overrun).ReadBytes()
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)

	// And a length prefix that is itself cut short is refused before it is used.
	_, err = NewReader([]byte{0x00, 0x00, 0x00}).ReadBytes()
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

// TestNilAndEmptyBytesAreTheSameOnTheWire. The format has no way to say
// "absent" for a byte field, so a nil goes out as a zero length and comes back
// as an empty slice. Callers comparing decoded messages must know this.
func TestNilAndEmptyBytesAreTheSameOnTheWire(t *testing.T) {
	fromNil := NewBuffer(0)
	fromNil.WriteBytes(nil)
	fromEmpty := NewBuffer(0)
	fromEmpty.WriteBytes([]byte{})
	require.Equal(t, "00000000", hex.EncodeToString(fromNil.Bytes()))
	require.Equal(t, fromNil.Bytes(), fromEmpty.Bytes())

	got, err := NewReader(fromNil.Bytes()).ReadBytes()
	require.NoError(t, err)
	require.NotNil(t, got, "a decoded byte field is never nil")
	require.Empty(t, got)

	// The same at the message level: a nil ChainId comes back empty, so a
	// decoded message is not always equal to the one that was sent.
	sent := &Message{Gossip: &Gossip{}}
	frame, err := Marshal(sent)
	require.NoError(t, err)
	back, err := Unmarshal(frame)
	require.NoError(t, err)
	require.NotNil(t, back.Gossip.ChainId)
	require.Empty(t, back.Gossip.ChainId)
}

// TestStringsCarryArbitraryBytes. The client name comes from a peer; it is
// length-prefixed, not terminated, so embedded NULs and invalid UTF-8 must
// survive rather than cut the field short.
func TestStringsCarryArbitraryBytes(t *testing.T) {
	for _, s := range []string{"", "lux", "with\x00nul", "\xff\xfe not utf8", "日本語"} {
		b := NewBuffer(0)
		b.WriteString(s)
		got, err := NewReader(b.Bytes()).ReadString()
		require.NoError(t, err)
		require.Equal(t, s, got)
	}

	_, err := NewReader([]byte{0x00, 0x00, 0x00, 0x04, 'a'}).ReadString()
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

// TestSlicesRoundTripThroughTheirCountPrefix, at the boundaries that matter:
// empty, one, and many.
func TestSlicesRoundTripThroughTheirCountPrefix(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		for _, in := range [][][]byte{
			{},
			{{0x01}},
			{{}, {0x01, 0x02}, {0x03}},
		} {
			b := NewBuffer(0)
			b.WriteBytesSlice(in)
			got, err := NewReader(b.Bytes()).ReadBytesSlice()
			require.NoError(t, err)
			require.Len(t, got, len(in))
			for i := range in {
				require.True(t, bytes.Equal(in[i], got[i]))
			}
		}
	})

	t.Run("uint32", func(t *testing.T) {
		for _, in := range [][]uint32{{}, {0}, {1, math.MaxUint32, 7}} {
			b := NewBuffer(0)
			b.WriteUint32Slice(in)
			got, err := NewReader(b.Bytes()).ReadUint32Slice()
			require.NoError(t, err)
			require.Len(t, got, len(in))
			for i := range in {
				require.Equal(t, in[i], got[i])
			}
		}
	})

	t.Run("uint64", func(t *testing.T) {
		for _, in := range [][]uint64{{}, {0}, {1, math.MaxUint64, 7}} {
			b := NewBuffer(0)
			b.WriteUint64Slice(in)
			got, err := NewReader(b.Bytes()).ReadUint64Slice()
			require.NoError(t, err)
			require.Len(t, got, len(in))
			for i := range in {
				require.Equal(t, in[i], got[i])
			}
		}
	})
}

// TestASliceCutShortIsRefused. The count says there are more elements than the
// frame holds; the decoder must stop rather than hand back a half-filled slice
// with zero values where the missing elements were.
func TestASliceCutShortIsRefused(t *testing.T) {
	// Which refusal is not the point -- running out of bytes and refusing the
	// count up front are both correct -- so these ask only that the decode is
	// refused, never that a short slice comes back.
	t.Run("bytes", func(t *testing.T) {
		b := NewBuffer(0)
		b.WriteUint32(3) // claims three
		b.WriteBytes([]byte{0x01})
		got, err := NewReader(b.Bytes()).ReadBytesSlice()
		require.Error(t, err)
		require.Nil(t, got)
	})
	t.Run("uint32", func(t *testing.T) {
		b := NewBuffer(0)
		b.WriteUint32(3)
		b.WriteUint32(1)
		got, err := NewReader(b.Bytes()).ReadUint32Slice()
		require.Error(t, err)
		require.Nil(t, got)
	})
	t.Run("uint64", func(t *testing.T) {
		b := NewBuffer(0)
		b.WriteUint32(3)
		b.WriteUint64(1)
		got, err := NewReader(b.Bytes()).ReadUint64Slice()
		require.Error(t, err)
		require.Nil(t, got)
	})
	t.Run("missing count", func(t *testing.T) {
		short := []byte{0x00, 0x00}
		_, err := NewReader(short).ReadBytesSlice()
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		_, err = NewReader(short).ReadUint32Slice()
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		_, err = NewReader(short).ReadUint64Slice()
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})
}

// TestDecodedFieldsAliasTheFrame. Decoding is zero-copy by design: byte fields
// point into the frame the caller passed in. Anyone reusing a read buffer
// across messages is overwriting messages already handed upstream, so this
// contract is pinned rather than left to be rediscovered.
func TestDecodedFieldsAliasTheFrame(t *testing.T) {
	frame, err := Marshal(&Message{Put: &Put{
		ChainId:   []byte{0xaa},
		RequestId: 5,
		Container: []byte{0xc0, 0xde},
	}})
	require.NoError(t, err)

	decoded, err := Unmarshal(frame)
	require.NoError(t, err)
	require.Equal(t, []byte{0xc0, 0xde}, decoded.Put.Container)

	// Scribbling on the frame changes the message that was already returned.
	for i := range frame {
		frame[i] = 0x00
	}
	require.Equal(t, []byte{0x00, 0x00}, decoded.Put.Container,
		"decoded fields alias the frame; a reused read buffer corrupts delivered messages")
}
