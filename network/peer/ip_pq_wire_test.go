// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"testing"

	"github.com/stretchr/testify/require"

	zappb "github.com/luxfi/proto/node/zap/p2p"
)

// TestHandshakeWire_LegacyFrameOmitsMLDSA verifies the wire-compat
// guarantee for the append-only IpMldsaSig field. A handshake frame
// emitted by a legacy peer (no IpMldsaSig at all) still round-trips
// through the new marshal/unmarshal pair, with IpMldsaSig reading as
// nil/empty.
func TestHandshakeWire_LegacyFrameOmitsMLDSA(t *testing.T) {
	require := require.New(t)

	// Build a handshake without ML-DSA.
	hs := &zappb.Handshake{
		NetworkId:     1337,
		MyTime:        12345,
		IpAddr:        []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4},
		IpPort:        9650,
		IpSigningTime: 12345,
		IpNodeIdSig:   []byte("legacy-tls-sig"),
		TrackedNets:   [][]byte{},
		Client: &zappb.Client{
			Name:  "lux",
			Major: 1,
			Minor: 26,
			Patch: 12,
		},
		SupportedLps: []uint32{},
		ObjectedLps:  []uint32{},
		KnownPeers: &zappb.BloomFilter{
			Filter: []byte{0x01, 0x02},
			Salt:   []byte{0x03, 0x04},
		},
		IpBlsSig:   []byte("legacy-bls-sig"),
		AllChains:  false,
		IpMldsaSig: nil,
	}

	// Wrap in a Message and round-trip via the codec.
	msg := &zappb.Message{Message: &zappb.Message_Handshake{Handshake: hs}}
	raw, err := zappb.Marshal(msg)
	require.NoError(err)
	var decoded zappb.Message
	require.NoError(zappb.Unmarshal(raw, &decoded))

	roundTripped, ok := decoded.Message.(*zappb.Message_Handshake)
	require.True(ok, "decoded message must be a Handshake")
	require.Empty(roundTripped.Handshake.IpMldsaSig,
		"empty ML-DSA on the wire decodes to empty on the receiver")
	require.Equal(hs.IpNodeIdSig, roundTripped.Handshake.IpNodeIdSig)
	require.Equal(hs.IpBlsSig, roundTripped.Handshake.IpBlsSig)
}

// TestHandshakeWire_TruncatedLegacyFrameDecodes mimics a true legacy
// wire payload that ends after IpBlsSig with no trailing IpMldsaSig
// length-prefix. The new unmarshaller MUST tolerate this — that's the
// whole point of the HasMore() guard around the new field read. Without
// the guard, decoding would fail with io.ErrUnexpectedEOF and the peer
// handshake would refuse every legacy peer on the network.
func TestHandshakeWire_TruncatedLegacyFrameDecodes(t *testing.T) {
	require := require.New(t)

	// First produce a new-format frame, then strip the trailing 4-byte
	// length prefix (the empty-bytes encoding of IpMldsaSig). The result
	// is byte-for-byte what a pre-PQ binary would have produced.
	hs := &zappb.Handshake{
		NetworkId:     1337,
		MyTime:        7777,
		IpAddr:        []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4},
		IpPort:        9650,
		IpSigningTime: 7777,
		IpNodeIdSig:   []byte("legacy-tls"),
		TrackedNets:   [][]byte{},
		Client:        &zappb.Client{Name: "lux", Major: 1, Minor: 0, Patch: 0},
		SupportedLps:  []uint32{},
		ObjectedLps:   []uint32{},
		KnownPeers:    &zappb.BloomFilter{Filter: []byte{}, Salt: []byte{}},
		IpBlsSig:      []byte("legacy-bls"),
		AllChains:     false,
		IpMldsaSig:    nil,
	}
	msg := &zappb.Message{Message: &zappb.Message_Handshake{Handshake: hs}}
	raw, err := zappb.Marshal(msg)
	require.NoError(err)
	// The last 4 bytes of the inner Handshake encoding are the empty
	// IpMldsaSig length-prefix (0x00000000). Truncating them simulates
	// a legacy peer that never wrote the field.
	require.GreaterOrEqual(len(raw), 4, "marshalled frame must be larger than the trailing field")
	truncated := raw[:len(raw)-4]

	var decoded zappb.Message
	require.NoError(zappb.Unmarshal(truncated, &decoded),
		"truncated legacy frame must decode without error")
	roundTripped, ok := decoded.Message.(*zappb.Message_Handshake)
	require.True(ok)
	require.Empty(roundTripped.Handshake.IpMldsaSig,
		"absent field reads as empty")
	require.Equal(hs.IpBlsSig, roundTripped.Handshake.IpBlsSig)
}

// TestHandshakeWire_NewFrameRoundTripsMLDSA verifies that a new-format
// frame (with a non-empty IpMldsaSig) round-trips intact.
func TestHandshakeWire_NewFrameRoundTripsMLDSA(t *testing.T) {
	require := require.New(t)

	mldsaSig := make([]byte, 3309) // ML-DSA-65 signature size
	for i := range mldsaSig {
		mldsaSig[i] = byte(i)
	}

	hs := &zappb.Handshake{
		NetworkId:     1,
		MyTime:        99999,
		IpAddr:        []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 10, 0, 0, 1},
		IpPort:        9630,
		IpSigningTime: 99999,
		IpNodeIdSig:   []byte("strict-pq-tls-sig"),
		TrackedNets:   [][]byte{},
		Client: &zappb.Client{
			Name:  "lux",
			Major: 1,
			Minor: 26,
			Patch: 13,
		},
		SupportedLps: []uint32{},
		ObjectedLps:  []uint32{},
		KnownPeers:   &zappb.BloomFilter{Filter: []byte{}, Salt: []byte{}},
		IpBlsSig:     []byte("strict-pq-bls-sig"),
		AllChains:    true,
		IpMldsaSig:   mldsaSig,
	}

	msg := &zappb.Message{Message: &zappb.Message_Handshake{Handshake: hs}}
	raw, err := zappb.Marshal(msg)
	require.NoError(err)
	var decoded zappb.Message
	require.NoError(zappb.Unmarshal(raw, &decoded))

	roundTripped, ok := decoded.Message.(*zappb.Message_Handshake)
	require.True(ok, "decoded message must be a Handshake")
	require.Equal(mldsaSig, roundTripped.Handshake.IpMldsaSig,
		"ML-DSA sig must round-trip byte-for-byte")
}
