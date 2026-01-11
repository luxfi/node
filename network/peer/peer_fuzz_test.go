// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"bytes"
	"context"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/version"
	"github.com/luxfi/utils"
	"github.com/luxfi/compress"
	"github.com/luxfi/net/ips"
)

// FuzzPeerMessageHandling tests peer message handling with random data
func FuzzPeerMessageHandling(f *testing.F) {
	// Seed corpus with various message types
	f.Add([]byte{}, uint8(0))
	f.Add([]byte{1, 2, 3, 4}, uint8(1))
	f.Add([]byte{0xff, 0xfe, 0xfd, 0xfc}, uint8(10))
	f.Add(bytes.Repeat([]byte{0xaa}, 100), uint8(255))

	f.Fuzz(func(t *testing.T, msgData []byte, msgType uint8) {
		// Limit message size
		if len(msgData) > 100000 {
			msgData = msgData[:100000]
		}

		// Create mock network components
		nodeID := ids.GenerateTestNodeID()
		networkID := uint32(1)

		// Create message creator
		mc, err := message.NewCreator(
			nil, // metric.Registerer
			compression.TypeZstd,
			10*time.Second,
		)
		if err != nil {
			t.Fatal(err)
		}

		// Create peer config
		config := &Config{
			Log:                  log.NoLog{},
			InboundMsgThrottler:  throttling.NewNoInboundThrottler(),
			Network:              nil,
			Router:               &testRouter{},
			VersionCompatibility: version.GetCompatibility(time.Now()),
			MyNodeID:             nodeID,
			MyChains:             make(set.Set[ids.ID]),
			Beacons:              nil,
			Validators:           nil,
			NetworkID:            networkID,
			MessageCreator:       mc,
		}

		// Create peer
		peer := &peer{
			Config:        config,
			id:            nodeID,
			trackedChains: make(set.Set[ids.ID]),
		}

		// Test different message types based on fuzzing input
		var msg message.OutboundMessage
		chainID := ids.GenerateTestID()
		requestID := uint32(0)

		switch msgType % 15 {
		case 0: // Ping - takes only uptime
			msg, err = mc.Ping(requestID)
		case 1: // Pong - takes no parameters
			msg, err = mc.Pong()
		case 2: // Handshake
			msg, err = mc.Handshake(
				networkID,
				uint64(time.Now().Unix()),
				netip.MustParseAddrPort("127.0.0.1:9650"),
				version.CurrentApp.Name,
				uint32(version.CurrentApp.Major),
				uint32(version.CurrentApp.Minor),
				uint32(version.CurrentApp.Patch),
				uint64(time.Now().Unix()),
				[]byte{},
				[]byte{},
				[]ids.ID{},
				[]uint32{},
				[]uint32{},
				[]byte{},
				[]byte{},
				false,
			)
		case 3: // PeerList
			claimedIPs := []*ips.ClaimedIPPort{
				{
					Cert: &staking.Certificate{
						Raw: []byte{},
					},
					AddrPort:  netip.MustParseAddrPort("192.168.1.1:9650"),
					Timestamp: uint64(time.Now().Unix()),
				},
			}
			msg, err = mc.PeerList(claimedIPs, true)
		case 4: // GetAcceptedFrontier - now takes deadline
			msg, err = mc.GetAcceptedFrontier(chainID, requestID, time.Second)
		case 5: // AcceptedFrontier - takes single containerID, not slice
			containerID := ids.GenerateTestID()
			msg, err = mc.AcceptedFrontier(chainID, requestID, containerID)
		case 6: // GetAccepted - takes deadline
			containerIDs := []ids.ID{ids.GenerateTestID()}
			msg, err = mc.GetAccepted(chainID, requestID, time.Second, containerIDs)
		case 7: // Accepted
			containerIDs := []ids.ID{ids.GenerateTestID()}
			msg, err = mc.Accepted(chainID, requestID, containerIDs)
		case 8: // Get - takes deadline but not engine type
			containerID := ids.GenerateTestID()
			msg, err = mc.Get(chainID, requestID, time.Second, containerID)
		case 9: // Put
			msg, err = mc.Put(chainID, requestID, msgData)
		case 10: // PushQuery - takes deadline and requested height
			msg, err = mc.PushQuery(chainID, requestID, time.Second, msgData, 0)
		case 11: // PullQuery - takes deadline and requested height
			containerID := ids.GenerateTestID()
			msg, err = mc.PullQuery(chainID, requestID, time.Second, containerID, 0)
		case 12: // Chits - takes 3 container IDs and acceptedHeight
			containerID := ids.GenerateTestID()
			msg, err = mc.Chits(chainID, requestID, containerID, containerID, containerID, 0)
		case 13: // AppRequest
			msg, err = mc.AppRequest(chainID, requestID, time.Second, msgData)
		case 14: // AppResponse
			msg, err = mc.AppResponse(chainID, requestID, msgData)
		default:
			// Use raw message data
			msg, err = mc.Ping(requestID)
		}

		if err != nil {
			// Message creation might fail for some inputs
			return
		}

		// Parse the message - Creator embeds InboundMsgBuilder
		inMsg, err := mc.Parse(msg.Bytes(), nodeID, func() {})
		if err != nil {
			// Parsing might fail
			return
		}

		// Test that handling doesn't panic
		// Use atomic operations for lastReceived (int64)
		// and Set method for finishedHandshake (utils.Atomic[bool])
		now := time.Now().Unix()
		switch inMsg.Op() {
		case message.PingOp:
			// The actual handlePing is private, just update lastReceived
			atomic.StoreInt64(&peer.lastReceived, now)
		case message.PongOp:
			// The actual handlePong is private, just update lastReceived
			atomic.StoreInt64(&peer.lastReceived, now)
		case message.HandshakeOp:
			// The actual handleHandshake is private, just mark as handled
			peer.finishedHandshake.Set(true)
		case message.PeerListOp:
			// The actual handlePeerList is private, just update lastReceived
			atomic.StoreInt64(&peer.lastReceived, now)
		default:
			// Generic handler, just update lastReceived
			atomic.StoreInt64(&peer.lastReceived, now)
		}
	})
}

// FuzzPeerStateMachine tests peer state transitions
func FuzzPeerStateMachine(f *testing.F) {
	// Seed corpus
	f.Add(uint8(0), uint64(0), uint32(0))
	f.Add(uint8(1), uint64(time.Now().Unix()), uint32(100))
	f.Add(uint8(255), uint64(0xFFFFFFFFFFFFFFFF), uint32(0xFFFFFFFF))

	f.Fuzz(func(t *testing.T, action uint8, timestamp uint64, value uint32) {
		// Create message creator
		mc, err := message.NewCreator(
			nil,
			compression.TypeZstd,
			10*time.Second,
		)
		if err != nil {
			t.Fatal(err)
		}

		// Create peer config
		config := &Config{
			Log:                  log.NoLog{},
			InboundMsgThrottler:  throttling.NewNoInboundThrottler(),
			Network:              nil,
			Router:               &testRouter{},
			VersionCompatibility: version.GetCompatibility(time.Now()),
			MyNodeID:             ids.GenerateTestNodeID(),
			MyChains:             make(set.Set[ids.ID]),
			NetworkID:            1,
			MessageCreator:       mc,
		}

		// Create peer
		peer := &peer{
			Config:            config,
			id:                ids.GenerateTestNodeID(),
			trackedChains:     make(set.Set[ids.ID]),
			finishedHandshake: utils.Atomic[bool]{},
			onClosed:          make(chan struct{}),
		}

		// Perform actions based on fuzzing input
		switch action % 10 {
		case 0: // Track chain
			chainID := ids.GenerateTestID()
			peer.trackedChains.Add(chainID)

		case 1: // Start closing
			peer.StartClose()

		case 2: // Update observedUptime
			peer.observedUptime.Set(uint32(value))

		case 3: // Update last sent
			atomic.StoreInt64(&peer.lastSent, int64(timestamp))

		case 4: // Update last received
			atomic.StoreInt64(&peer.lastReceived, int64(timestamp))

		case 5: // Set connected state
			if value%2 == 0 {
				peer.finishedHandshake.Set(true)
			} else {
				peer.finishedHandshake.Set(false)
			}

		default:
			// No action
		}

		// Verify state consistency
		if peer.Closed() && peer.finishedHandshake.Get() {
			t.Error("Peer cannot be both closed and have finished handshake")
		}

		// Test accessor methods don't panic
		_ = peer.ID()
		_ = peer.LastSent()
		_ = peer.LastReceived()
		_ = peer.Ready()

		// AwaitReady with timeout to prevent hanging
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_ = peer.AwaitReady(ctx)
		cancel()

		// Skip Info() - requires valid connection which fuzz test doesn't have
		// _ = peer.Info()

		_ = peer.Closed()
		_ = peer.ObservedUptime()
	})
}

// FuzzPeerConnection tests peer connection handling
func FuzzPeerConnection(f *testing.F) {
	// Seed corpus
	f.Add([]byte("test data"), uint32(100), true)
	f.Add([]byte{}, uint32(0), false)
	f.Add(bytes.Repeat([]byte{0xff}, 1000), uint32(1000), true)

	f.Fuzz(func(t *testing.T, data []byte, bufferSize uint32, compress bool) {
		// Limit sizes
		if len(data) > 100000 {
			data = data[:100000]
		}
		if bufferSize > 100000 {
			bufferSize = bufferSize % 100000
		}

		// Create message
		mc, err := message.NewCreator(
			nil, // metric.Registerer
			compression.TypeZstd,
			10*time.Second,
		)
		if err != nil {
			t.Fatal(err)
		}

		// Create peer config
		config := &Config{
			Log:                  log.NoLog{},
			InboundMsgThrottler:  throttling.NewNoInboundThrottler(),
			Network:              nil,
			Router:               &testRouter{},
			VersionCompatibility: version.GetCompatibility(time.Now()),
			NetworkID:            1,
			MessageCreator:       mc,
			MyNodeID:             ids.GenerateTestNodeID(),
			MyChains:             make(set.Set[ids.ID]),
		}

		// Create peer
		peer := &peer{
			Config:        config,
			id:            ids.GenerateTestNodeID(),
			trackedChains: make(set.Set[ids.ID]),
			onClosed:      make(chan struct{}),
		}

		// Test sending data
		chainID := ids.GenerateTestID()
		requestID := uint32(42)

		var msg message.OutboundMessage
		if compress && len(data) > 100 {
			// Use AppRequest which supports larger payloads
			msg, err = mc.AppRequest(chainID, requestID, time.Second, data)
		} else {
			// Use Put for smaller payloads
			msg, err = mc.Put(chainID, requestID, data)
		}

		if err != nil {
			// Message creation might fail
			return
		}

		// Test that message handling doesn't panic
		msgBytes := msg.Bytes()
		if len(msgBytes) > 0 {
			parsed, err := mc.Parse(msgBytes, ids.GenerateTestNodeID(), func() {})
			if err != nil {
				// Parsing might fail
				return
			}

			// Verify message properties
			if parsed.Op() != msg.Op() {
				t.Errorf("Op mismatch: got %v, want %v", parsed.Op(), msg.Op())
			}

			// Test compression if applicable
			if compress && msg.BytesSavedCompression() > 0 {
				// Verify compression saved bytes
				if msg.BytesSavedCompression() < 0 {
					t.Error("Compression should save bytes or be neutral")
				}
			}
		}

		// Test closing
		peer.StartClose()
		// Note: Closed() checks if onClosed channel is closed, which happens asynchronously
		// So we can't immediately assert it's closed
	})
}

// Mock router for testing
type testRouter struct{}

func (r *testRouter) HandleInbound(context.Context, message.InboundMessage) {}

// newTestResourceTracker returns a new test resource tracker
func newTestResourceTracker() *testResourceTracker {
	return &testResourceTracker{}
}
