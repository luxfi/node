// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/compress"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
	"github.com/luxfi/node/proto/p2p"
)

// A node started with --track-all-chains asks its peers for every network's
// IPs, and it asks by setting AllChains on the Handshake and on every
// GetPeerList it sends. The peer decides what to answer from that one bit:
// network/peer/peer.go passes msg.AllChains straight into Network.Peers.
//
// The bit therefore has to survive the encode. It did not — the codec never
// wrote it, so every receiver read false and answered as though the node had
// never asked. Both directions are pinned here.

func TestAllChainsCrossesTheWire(t *testing.T) {
	require := require.New(t)

	out, err := outFor(t).Handshake(
		1, // networkID
		uint64(time.Now().Unix()),
		netip.MustParseAddrPort("127.0.0.1:9651"),
		"lux", 1, 0, 0,
		uint64(time.Now().Unix()),
		[]byte{0xa1}, // ipNodeIDSig
		[]byte{0xb1}, // ipBLSSig
		[]ids.ID{},   // trackedNets
		nil, nil,     // supported / objected LPs
		[]byte{0xf1}, []byte{0x5a}, // known-peers filter, salt
		true, // requestAllNetIPs
		nil,  // ipMLDSASig
		nil,  // chains
	)
	require.NoError(err)

	in, err := inFor(t).Parse(out.Bytes(), ids.EmptyNodeID, func() {})
	require.NoError(err)
	require.True(in.Message().(*p2p.Handshake).AllChains,
		"the peer must see that all-network IPs were requested")
}

func TestGetPeerListAllChainsCrossesTheWire(t *testing.T) {
	require := require.New(t)

	out, err := outFor(t).GetPeerList([]byte{0xf1}, []byte{0x5a}, true)
	require.NoError(err)

	in, err := inFor(t).Parse(out.Bytes(), ids.EmptyNodeID, func() {})
	require.NoError(err)
	require.True(in.Message().(*p2p.GetPeerList).AllChains)
}

// The send side fills AcceptedHeight from the responder's own accepted
// frontier. It has to arrive.
func TestChitsAcceptedHeightCrossesTheWire(t *testing.T) {
	require := require.New(t)

	out, err := outFor(t).Chits(
		ids.GenerateTestID(), 7,
		ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID(),
		9_000_001,
	)
	require.NoError(err)

	in, err := inFor(t).Parse(out.Bytes(), ids.EmptyNodeID, func() {})
	require.NoError(err)
	require.Equal(uint64(9_000_001), in.Message().(*p2p.Chits).AcceptedHeight)
}

func builders(t *testing.T) (OutboundMsgBuilder, InboundMsgBuilder) {
	t.Helper()
	mb, err := newMsgBuilder(metric.NewRegistry(), 5*time.Second)
	require.NoError(t, err)
	return newOutboundBuilder(compress.TypeNone, mb), newInboundBuilder(mb)
}

func outFor(t *testing.T) OutboundMsgBuilder { o, _ := builders(t); return o }
func inFor(t *testing.T) InboundMsgBuilder   { _, i := builders(t); return i }
