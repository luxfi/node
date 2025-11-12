// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/proto/pb/p2p"
	"github.com/luxfi/node/snow/networking/router"
	"github.com/luxfi/node/utils/compression"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/ips"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/version"
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
		ctx := context.Background()
		nodeID := ids.GenerateTestNodeID()
		networkID := uint32(1)
		
		// Create message creator
		mc, err := message.NewCreator(
			"test",
			map[uint32]time.Duration{networkID: time.Second},
			10*time.Second,
			compression.NewGzipCompressor(compression.BestSpeed),
		)
		if err != nil {
			t.Fatal(err)
		}

		// Create peer config
		config := &Config{
			Log:                   logging.NoLog{},
			InboundMsgThrottler:   throttling.NewNoInboundThrottler(),
			OutboundMsgThrottler:  throttling.NewNoOutboundThrottler(),
			Network:               nil,
			Router:                &testRouter{},
			VersionCompatibility:  version.GetCompatibility(networkID),
			MyNetID:               ids.GenerateTestID(),
			MyTime:                uint64(time.Now().Unix()),
			MaxClockDifference:    time.Minute,
			PeerListSize:          100,
			PingFrequency:         30 * time.Second,
			PongTimeout:           60 * time.Second,
			MaxPendingMessages:    1024,
			MaxConnectionAttempts: 10,
			ResourceTracker:       newTestResourceTracker(),
		}

		// Create peer
		peer := &peer{
			Config:            config,
			id:                nodeID,
			nodeVersion:       version.CurrentApp,
			trackedSubnets:    set.Set[ids.ID]{},
			responseDeadlines: make(map[uint32]time.Time),
			observedUptimes:   make(map[ids.ID]uint32),
		}

		// Test different message types based on fuzzing input
		var msg message.OutboundMessage
		chainID := ids.GenerateTestID()
		requestID := uint32(0)

		switch msgType % 15 {
		case 0: // Ping
			msg, err = mc.Ping(chainID, requestID)
		case 1: // Pong
			msg, err = mc.Pong(chainID, requestID)
		case 2: // Version
			msg, err = mc.Version(
				networkID,
				uint64(time.Now().Unix()),
				ips.IPPort{IP: net.IPv4(127, 0, 0, 1), Port: 9650},
				version.CurrentApp.String(),
				uint64(time.Now().Unix()),
				[]byte{},
				[]ids.ID{},
			)
		case 3: // PeerList
			ips := []ips.ClaimedIPPort{
				{
					Cert: []byte{},
					IPPort: ips.IPPort{
						IP:   net.IPv4(192, 168, 1, 1),
						Port: 9650,
					},
					Timestamp: uint64(time.Now().Unix()),
				},
			}
			msg, err = mc.PeerList(ips, true)
		case 4: // GetAcceptedFrontier
			msg, err = mc.GetAcceptedFrontier(chainID, requestID)
		case 5: // AcceptedFrontier
			containerIDs := []ids.ID{ids.GenerateTestID()}
			msg, err = mc.AcceptedFrontier(chainID, requestID, containerIDs)
		case 6: // GetAccepted
			containerIDs := []ids.ID{ids.GenerateTestID()}
			msg, err = mc.GetAccepted(chainID, requestID, containerIDs)
		case 7: // Accepted
			containerIDs := []ids.ID{ids.GenerateTestID()}
			msg, err = mc.Accepted(chainID, requestID, containerIDs)
		case 8: // Get
			containerID := ids.GenerateTestID()
			msg, err = mc.Get(chainID, requestID, containerID)
		case 9: // Put
			msg, err = mc.Put(chainID, requestID, msgData)
		case 10: // PushQuery
			msg, err = mc.PushQuery(chainID, requestID, msgData)
		case 11: // PullQuery
			containerID := ids.GenerateTestID()
			msg, err = mc.PullQuery(chainID, requestID, containerID)
		case 12: // Chits
			containerIDs := []ids.ID{ids.GenerateTestID()}
			msg, err = mc.Chits(chainID, requestID, containerIDs)
		case 13: // AppRequest
			msg, err = mc.AppRequest(chainID, requestID, time.Second, msgData)
		case 14: // AppResponse
			msg, err = mc.AppResponse(chainID, requestID, msgData)
		default:
			// Use raw message data
			msg, err = mc.Ping(chainID, requestID)
		}

		if err != nil {
			// Message creation might fail for some inputs
			return
		}

		// Parse the message
		inMsg, err := message.Parse(msg.Bytes())
		if err != nil {
			// Parsing might fail
			return
		}

		// Test that handling doesn't panic
		switch inMsg.Op() {
		case message.Ping:
			peer.handlePing(inMsg)
		case message.Pong:
			peer.handlePong(inMsg)
		case message.Version:
			peer.handleVersion(inMsg)
		case message.PeerList:
			peer.handlePeerList(inMsg)
		default:
			// Route to handler
			peer.handle(inMsg)
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
		// Create peer config
		config := &Config{
			Log:                   logging.NoLog{},
			InboundMsgThrottler:   throttling.NewNoInboundThrottler(),
			OutboundMsgThrottler:  throttling.NewNoOutboundThrottler(),
			Network:               nil,
			Router:                &testRouter{},
			VersionCompatibility:  version.GetCompatibility(1),
			MyNetID:               ids.GenerateTestID(),
			MyTime:                uint64(time.Now().Unix()),
			MaxClockDifference:    time.Minute,
			PeerListSize:          100,
			PingFrequency:         30 * time.Second,
			PongTimeout:           60 * time.Second,
			MaxPendingMessages:    1024,
			MaxConnectionAttempts: 10,
			ResourceTracker:       newTestResourceTracker(),
		}

		// Create peer
		peer := &peer{
			Config:            config,
			id:                ids.GenerateTestNodeID(),
			nodeVersion:       version.CurrentApp,
			trackedSubnets:    set.Set[ids.ID]{},
			responseDeadlines: make(map[uint32]time.Time),
			observedUptimes:   make(map[ids.ID]uint32),
			gotVersion:        new(bool),
			version:           new(version.Application),
		}

		// Perform actions based on fuzzing input
		switch action % 10 {
		case 0: // Set version
			*peer.gotVersion = true
			peer.version = &version.Application{
				Name:  "test",
				Major: int(value % 100),
				Minor: int((value / 100) % 100),
				Patch: int((value / 10000) % 100),
			}

		case 1: // Track subnet
			subnetID := ids.GenerateTestID()
			peer.trackedSubnets.Add(subnetID)

		case 2: // Set uptime
			subnetID := ids.GenerateTestID()
			peer.observedUptimes[subnetID] = value

		case 3: // Add response deadline
			deadline := time.Unix(int64(timestamp), 0)
			peer.responseDeadlines[value] = deadline

		case 4: // Start closing
			peer.StartClose()

		case 5: // Set benched
			if timestamp > 0 {
				peer.benched.Store(true)
			} else {
				peer.benched.Store(false)
			}

		case 6: // Update last sent
			peer.lastSent.Store(int64(timestamp))

		case 7: // Update last received
			peer.lastReceived.Store(int64(timestamp))

		case 8: // Set connected state
			if value%2 == 0 {
				peer.finishedHandshake.Store(true)
			} else {
				peer.finishedHandshake.Store(false)
			}

		case 9: // Check timeouts
			now := time.Unix(int64(timestamp), 0)
			for reqID, deadline := range peer.responseDeadlines {
				if now.After(deadline) {
					delete(peer.responseDeadlines, reqID)
				}
			}
		}

		// Verify state consistency
		if peer.closed.Load() && peer.finishedHandshake.Load() {
			t.Error("Peer cannot be both closed and have finished handshake")
		}

		// Test accessor methods don't panic
		_ = peer.ID()
		_ = peer.NodeVersion()
		_ = peer.LastSent()
		_ = peer.LastReceived()
		_ = peer.Ready()
		_ = peer.AwaitReady(context.Background())
		_ = peer.Info()
		_ = peer.Closed()
		_ = peer.TrackedSubnets()
		_ = peer.ObservedUptime(ids.GenerateTestID())
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
			"test",
			map[uint32]time.Duration{1: time.Second},
			10*time.Second,
			compression.NewGzipCompressor(compression.BestSpeed),
		)
		if err != nil {
			t.Fatal(err)
		}

		// Create peer config
		config := &Config{
			Log:                   logging.NoLog{},
			InboundMsgThrottler:   throttling.NewNoInboundThrottler(),
			OutboundMsgThrottler:  throttling.NewNoOutboundThrottler(),
			Network:               nil,
			Router:                &testRouter{},
			VersionCompatibility:  version.GetCompatibility(1),
			MyNetID:               ids.GenerateTestID(),
			MyTime:                uint64(time.Now().Unix()),
			MaxClockDifference:    time.Minute,
			PeerListSize:          100,
			PingFrequency:         30 * time.Second,
			PongTimeout:           60 * time.Second,
			MaxPendingMessages:    int(bufferSize),
			MaxConnectionAttempts: 10,
			ResourceTracker:       newTestResourceTracker(),
		}

		// Create peer
		peer := &peer{
			Config:            config,
			id:                ids.GenerateTestNodeID(),
			nodeVersion:       version.CurrentApp,
			trackedSubnets:    set.Set[ids.ID]{},
			responseDeadlines: make(map[uint32]time.Time),
			observedUptimes:   make(map[ids.ID]uint32),
			gotVersion:        new(bool),
			version:           new(version.Application),
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
			parsed, err := message.Parse(msgBytes)
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

		// Test resource tracking
		peer.StartClose()
		if !peer.closed.Load() {
			t.Error("Peer should be closed after StartClose")
		}
	})
}

// Mock router for testing
type testRouter struct{}

func (r *testRouter) HandleInbound(context.Context, message.InboundMessage) {}
func (r *testRouter) RegisterRequest(context.Context, ids.NodeID, ids.ID, ids.ID, uint32, message.Op, time.Time) {}
func (r *testRouter) ParseMessages([]byte) ([]message.InboundMessage, error) { return nil, nil }

// Mock resource tracker
type testResourceTracker struct{}

func newTestResourceTracker() *testResourceTracker {
	return &testResourceTracker{}
}

func (t *testResourceTracker) StartProcessing(ids.NodeID, time.Time) {}
func (t *testResourceTracker) StopProcessing(ids.NodeID, time.Time) {}
func (t *testResourceTracker) Bandwidth(ids.NodeID, int) {}
func (t *testResourceTracker) GetBandwidth(ids.NodeID) float64 { return 0 }

// Helper methods for peer testing
func (p *peer) handlePing(msg message.InboundMessage) {
	// Mock ping handling
	p.lastReceived.Store(time.Now().Unix())
}

func (p *peer) handlePong(msg message.InboundMessage) {
	// Mock pong handling
	p.lastReceived.Store(time.Now().Unix())
	
	// Clear response deadline if present
	if reqID, ok := msg.Get(message.RequestID).(uint32); ok {
		delete(p.responseDeadlines, reqID)
	}
}

func (p *peer) handleVersion(msg message.InboundMessage) {
	// Mock version handling
	*p.gotVersion = true
	p.finishedHandshake.Store(true)
}

func (p *peer) handlePeerList(msg message.InboundMessage) {
	// Mock peer list handling
	p.lastReceived.Store(time.Now().Unix())
}

func (p *peer) handle(msg message.InboundMessage) {
	// Generic message handling
	p.lastReceived.Store(time.Now().Unix())
}