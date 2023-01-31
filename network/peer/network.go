// Copyright (C) 2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
<<<<<<< HEAD
	"github.com/luxdefi/luxd/ids"
	"github.com/luxdefi/luxd/message"
	"github.com/luxdefi/luxd/utils/ips"
=======
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/ips"
<<<<<<< HEAD
>>>>>>> 6fc3d3f7c (Remove `Version` from the `peer.Network` interface (#2320))
=======

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
>>>>>>> 8fe3833a0 (Support IP updates in PeerList gossip tracking (#2374))
)

// Network defines the interface that is used by a peer to help establish a well
// connected p2p network.
type Network interface {
	// Connected is called by the peer once the handshake is finished.
	Connected(peerID ids.NodeID)

	// AllowConnection enables the network is signal to the peer that its
	// connection is no longer desired and should be terminated.
	AllowConnection(peerID ids.NodeID) bool

	// Track allows the peer to notify the network of a potential new peer to
	// connect to, given the [ips] of the peers it sent us during the peer
	// handshake.
	//
	// Returns which IPs should not be gossipped to this node again.
	Track(peerID ids.NodeID, ips []*ips.ClaimedIPPort) ([]*p2ppb.PeerAck, error)

	// MarkTracked stops sending gossip about [ips] to [peerID].
	MarkTracked(peerID ids.NodeID, ips []*p2ppb.PeerAck) error

	// Disconnected is called when the peer finishes shutting down. It is not
	// guaranteed that [Connected] was called for the provided peer. However, it
	// is guaranteed that [Connected] will not be called after [Disconnected]
	// for a given [Peer] object.
	Disconnected(peerID ids.NodeID)

	// Peers returns peers that [peerID] might not know about.
	Peers(peerID ids.NodeID) ([]ips.ClaimedIPPort, error)
}
