// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"crypto"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"

	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/network/tracker"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/version"
	"github.com/luxfi/utils"
	compression "github.com/luxfi/compress"
)

type testPeer struct {
	Peer
	inboundMsgChan <-chan message.InboundMessage
}

type rawTestPeer struct {
	config         *Config
	cert           *staking.Certificate
	inboundMsgChan <-chan message.InboundMessage
}

// noOpTracker implements tracker.Tracker for testing
type noOpTracker struct{}

func (n *noOpTracker) Usage(nodeID ids.NodeID, t time.Time) float64 { return 0 }
func (n *noOpTracker) TimeUntilUsage(nodeID ids.NodeID, t time.Time, usage float64) time.Duration {
	return time.Hour
}
func (n *noOpTracker) TotalUsage() float64 { return 0 }

// addStaker is a helper to call AddStaker using reflection since it's not in the public interface
func addStaker(mgr validators.Manager, netID ids.ID, nodeID ids.NodeID, pubKey []byte, txID ids.ID, weight uint64) error {
	v := reflect.ValueOf(mgr)
	method := v.MethodByName("AddStaker")
	if !method.IsValid() {
		return nil // Skip if method doesn't exist
	}
	result := method.Call([]reflect.Value{
		reflect.ValueOf(netID),
		reflect.ValueOf(nodeID),
		reflect.ValueOf(pubKey),
		reflect.ValueOf(txID),
		reflect.ValueOf(weight),
	})
	if len(result) > 0 && !result[0].IsNil() {
		return result[0].Interface().(error)
	}
	return nil
}

// noOpConsensusResourceTracker implements consensustracker.ResourceTracker for testing
type noOpConsensusResourceTracker struct {
	cpuTracker  *noOpTracker
	diskTracker *noOpTracker
}

func (n *noOpConsensusResourceTracker) StartProcessing(nodeID ids.NodeID, t time.Time) {}
func (n *noOpConsensusResourceTracker) StopProcessing(nodeID ids.NodeID, t time.Time)  {}
func (n *noOpConsensusResourceTracker) CPUTracker() tracker.Tracker {
	return n.cpuTracker
}
func (n *noOpConsensusResourceTracker) DiskTracker() tracker.Tracker {
	return n.diskTracker
}

func newMessageCreator(t *testing.T) message.Creator {
	t.Helper()

	mc, err := message.NewCreator(
		metric.NewRegistry(),
		compression.Type(constants.DefaultNetworkCompressionType),
		10*time.Second,
	)
	require.NoError(t, err)

	return mc
}

func newConfig(t *testing.T) *Config {
	t.Helper()
	require := require.New(t)

	metrics, err := NewMetrics(metric.NewRegistry())
	require.NoError(err)

	// Create a no-op consensus resource tracker for testing
	consensusResourceTracker := &noOpConsensusResourceTracker{
		cpuTracker:  &noOpTracker{},
		diskTracker: &noOpTracker{},
	}

	return &Config{
		ReadBufferSize:       constants.DefaultNetworkPeerReadBufferSize,
		WriteBufferSize:      constants.DefaultNetworkPeerWriteBufferSize,
		Metrics:              metrics,
		MessageCreator:       newMessageCreator(t),
		Log:                  log.NewNoOpLogger(),
		InboundMsgThrottler:  throttling.NewNoInboundThrottler(),
		Network:              TestNetwork,
		Router:               nil,
		VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
		MyChains:             nil,
		Beacons:              validators.NewManager(),
		Validators:           validators.NewManager(),
		NetworkID:            constants.LocalID,
		PingFrequency:        constants.DefaultPingFrequency,
		PongTimeout:          constants.DefaultPingPongTimeout,
		MaxClockDifference:   time.Minute,
		ResourceTracker:      consensusResourceTracker,
		UptimeCalculator:     uptime.NoOpCalculator{},
		IPSigner:             nil,
	}
}

func newRawTestPeer(t *testing.T, config *Config) *rawTestPeer {
	t.Helper()
	require := require.New(t)

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)
	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	config.MyNodeID = ids.NodeIDFromCert(cert)

	ip := utils.NewAtomic(netip.AddrPortFrom(
		netip.IPv6Loopback(),
		1,
	))
	tls := tlsCert.PrivateKey.(crypto.Signer)
	blsKey, err := localsigner.New()
	require.NoError(err)

	config.IPSigner = NewIPSigner(ip, tls, blsKey)

	inboundMsgChan := make(chan message.InboundMessage, 10)
	config.Router = InboundHandlerFunc(func(_ context.Context, inboundMsg message.InboundMessage) {
		inboundMsgChan <- inboundMsg
	})

	return &rawTestPeer{
		config:         config,
		cert:           cert,
		inboundMsgChan: inboundMsgChan,
	}
}

func startTestPeer(self *rawTestPeer, peer *rawTestPeer, conn net.Conn) *testPeer {
	return &testPeer{
		Peer: Start(
			self.config,
			conn,
			peer.cert,
			peer.config.MyNodeID,
			NewThrottledMessageQueue(
				self.config.Metrics,
				peer.config.MyNodeID,
				log.NewNoOpLogger(),
				throttling.NewNoOutboundThrottler(),
			),
			false,
			nil,
		),
		inboundMsgChan: self.inboundMsgChan,
	}
}

func startTestPeers(rawPeer0 *rawTestPeer, rawPeer1 *rawTestPeer) (*testPeer, *testPeer) {
	conn0, conn1 := net.Pipe()
	peer0 := startTestPeer(rawPeer0, rawPeer1, conn0)
	peer1 := startTestPeer(rawPeer1, rawPeer0, conn1)
	return peer0, peer1
}

func awaitReady(t *testing.T, peers ...Peer) {
	t.Helper()
	require := require.New(t)

	for _, peer := range peers {
		require.NoError(peer.AwaitReady(context.Background()))
		require.True(peer.Ready())
	}
}

func must[T any](t *testing.T) func(T, error) T {
	return func(val T, err error) T {
		require.NoError(t, err)
		return val
	}
}

func TestReady(t *testing.T) {
	require := require.New(t)

	config0 := newConfig(t)
	config1 := newConfig(t)

	rawPeer0 := newRawTestPeer(t, config0)
	rawPeer1 := newRawTestPeer(t, config1)

	conn0, conn1 := net.Pipe()

	peer0 := startTestPeer(rawPeer0, rawPeer1, conn0)
	require.False(peer0.Ready())

	peer1 := startTestPeer(rawPeer1, rawPeer0, conn1)
	awaitReady(t, peer0, peer1)

	peer0.StartClose()
	require.NoError(peer0.AwaitClosed(context.Background()))
	require.NoError(peer1.AwaitClosed(context.Background()))
}

func TestSend(t *testing.T) {
	require := require.New(t)

	config0 := newConfig(t)
	config1 := newConfig(t)

	rawPeer0 := newRawTestPeer(t, config0)
	rawPeer1 := newRawTestPeer(t, config1)

	peer0, peer1 := startTestPeers(rawPeer0, rawPeer1)
	awaitReady(t, peer0, peer1)

	outboundGetMsg, err := config0.MessageCreator.Get(ids.Empty, 1, time.Second, ids.Empty)
	require.NoError(err)

	require.True(peer0.Send(context.Background(), outboundGetMsg))

	inboundGetMsg := <-peer1.inboundMsgChan
	require.Equal(message.GetOp, inboundGetMsg.Op())

	peer1.StartClose()
	require.NoError(peer0.AwaitClosed(context.Background()))
	require.NoError(peer1.AwaitClosed(context.Background()))
}

func TestPingUptimes(t *testing.T) {
	config0 := newConfig(t)
	config1 := newConfig(t)

	// The raw peers are generated outside of the test cases to avoid generating
	// many TLS keys.
	rawPeer0 := newRawTestPeer(t, config0)
	rawPeer1 := newRawTestPeer(t, config1)

	require := require.New(t)

	peer0, peer1 := startTestPeers(rawPeer0, rawPeer1)
	awaitReady(t, peer0, peer1)
	defer func() {
		peer1.StartClose()
		peer0.StartClose()
		require.NoError(peer0.AwaitClosed(context.Background()))
		require.NoError(peer1.AwaitClosed(context.Background()))
	}()
	pingMsg, err := config0.MessageCreator.Ping(1)
	require.NoError(err)
	require.True(peer0.Send(context.Background(), pingMsg))

	// we send Get message after ping to ensure Ping is handled by the
	// time Get is handled. This is because Get is routed to the handler
	// whereas Ping is handled by the peer directly. We have no way to
	// know when the peer has handled the Ping message.
	sendAndFlush(t, peer0, peer1)

	uptime := peer1.ObservedUptime()
	require.Equal(uint32(1), uptime)
}

func TestTrackedChains(t *testing.T) {
	rawPeer0 := newRawTestPeer(t, newConfig(t))
	rawPeer1 := newRawTestPeer(t, newConfig(t))

	makeChainIDs := func(numChains int) []ids.ID {
		chainIDs := make([]ids.ID, numChains)
		for i := range chainIDs {
			chainIDs[i] = ids.GenerateTestID()
		}
		return chainIDs
	}

	tests := []struct {
		name             string
		trackedChains    []ids.ID
		shouldDisconnect bool
	}{
		{
			name:             "primary network only",
			trackedChains:    makeChainIDs(0),
			shouldDisconnect: false,
		},
		{
			name:             "single chain",
			trackedChains:    makeChainIDs(1),
			shouldDisconnect: false,
		},
		{
			name:             "max chains",
			trackedChains:    makeChainIDs(maxNumTrackedChains),
			shouldDisconnect: false,
		},
		{
			name:             "too many chains",
			trackedChains:    makeChainIDs(maxNumTrackedChains + 1),
			shouldDisconnect: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			rawPeer0.config.MyChains = set.Of(test.trackedChains...)
			peer0, peer1 := startTestPeers(rawPeer0, rawPeer1)
			if test.shouldDisconnect {
				require.NoError(peer0.AwaitClosed(context.Background()))
				require.NoError(peer1.AwaitClosed(context.Background()))
				return
			}

			defer func() {
				peer1.StartClose()
				peer0.StartClose()
				require.NoError(peer0.AwaitClosed(context.Background()))
				require.NoError(peer1.AwaitClosed(context.Background()))
			}()

			awaitReady(t, peer0, peer1)

			require.Equal(set.Of(constants.PrimaryNetworkID), peer0.TrackedChains())

			expectedTrackedChains := set.Of(test.trackedChains...)
			expectedTrackedChains.Add(constants.PrimaryNetworkID)
			require.Equal(expectedTrackedChains, peer1.TrackedChains())
		})
	}
}

// Test that a peer using the wrong BLS key is disconnected from.
func TestInvalidBLSKeyDisconnects(t *testing.T) {
	require := require.New(t)

	sharedConfig0 := newConfig(t)
	sharedConfig1 := newConfig(t)

	rawPeer0 := newRawTestPeer(t, sharedConfig0)
	rawPeer1 := newRawTestPeer(t, sharedConfig1)

	require.NoError(addStaker(
		rawPeer0.config.Validators,
		constants.PrimaryNetworkID,
		rawPeer1.config.MyNodeID,
		bls.PublicKeyToCompressedBytes(rawPeer1.config.IPSigner.blsSigner.PublicKey()),
		ids.GenerateTestID(),
		1,
	))

	bogusBLSKey, err := localsigner.New()
	require.NoError(err)
	require.NoError(addStaker(
		rawPeer1.config.Validators,
		constants.PrimaryNetworkID,
		rawPeer0.config.MyNodeID,
		bls.PublicKeyToCompressedBytes(bogusBLSKey.PublicKey()), // This is the wrong BLS key for this peer
		ids.GenerateTestID(),
		1,
	))

	peer0, peer1 := startTestPeers(rawPeer0, rawPeer1)

	// Because peer1 thinks that peer0 is using the wrong BLS key, they should
	// disconnect from each other.
	require.NoError(peer0.AwaitClosed(context.Background()))
	require.NoError(peer1.AwaitClosed(context.Background()))
}

func TestShouldDisconnect(t *testing.T) {
	peerID := ids.GenerateTestNodeID()
	txID := ids.GenerateTestID()
	blsKey, err := localsigner.New()
	require.NoError(t, err)
	must := must[*bls.Signature](t)

	// Create shared config and version for old version test
	_ = &Config{
		Log:                  log.NewNoOpLogger(),
		VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
	}
	_ = &version.Application{
		Name:  version.Client,
		Major: 0,
		Minor: 0,
		Patch: 0,
	}

	tests := []struct {
		name                     string
		initialPeer              *peer
		expectedPeer             *peer
		expectedShouldDisconnect bool
	}{
		{
			name: "peer is reporting old version",
			initialPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
				},
				version: &version.Application{
					Name:  version.Client,
					Major: 0,
					Minor: 0,
					Patch: 0,
				},
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
				},
				version: &version.Application{
					Name:  version.Client,
					Major: 0,
					Minor: 0,
					Patch: 0,
				},
			},
			expectedShouldDisconnect: true,
		},
		{
			name: "peer is not a validator",
			initialPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators:           validators.NewManager(),
				},
				version: version.CurrentApp,
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators:           validators.NewManager(),
				},
				version: version.CurrentApp,
			},
			expectedShouldDisconnect: false,
		},
		{
			name: "peer is a validator without a BLS key",
			initialPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, addStaker(vdrs,
							constants.PrimaryNetworkID,
							peerID,
							nil,
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, addStaker(vdrs,
							constants.PrimaryNetworkID,
							peerID,
							nil,
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
			},
			expectedShouldDisconnect: false,
		},
		{
			name: "already verified peer",
			initialPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, addStaker(vdrs,
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicKeyToUncompressedBytes(blsKey.PublicKey()),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:                   peerID,
				version:              version.CurrentApp,
				txIDOfVerifiedBLSKey: txID,
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, addStaker(vdrs,
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicKeyToUncompressedBytes(blsKey.PublicKey()),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:                   peerID,
				version:              version.CurrentApp,
				txIDOfVerifiedBLSKey: txID,
			},
			expectedShouldDisconnect: false,
		},
		{
			name: "peer without signature",
			initialPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, addStaker(vdrs,
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicKeyToUncompressedBytes(blsKey.PublicKey()),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip:      &SignedIP{},
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, addStaker(vdrs,
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicKeyToUncompressedBytes(blsKey.PublicKey()),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip:      &SignedIP{},
			},
			expectedShouldDisconnect: true,
		},
		{
			name: "peer with invalid signature",
			initialPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, addStaker(vdrs,
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicKeyToUncompressedBytes(blsKey.PublicKey()),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip: &SignedIP{
					BLSSignature: must(blsKey.SignProofOfPossession([]byte("wrong message"))),
				},
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, addStaker(vdrs,
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicKeyToUncompressedBytes(blsKey.PublicKey()),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip: &SignedIP{
					BLSSignature: must(blsKey.SignProofOfPossession([]byte("wrong message"))),
				},
			},
			expectedShouldDisconnect: true,
		},
		{
			name: "peer with valid signature",
			initialPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, addStaker(vdrs,
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicKeyToUncompressedBytes(blsKey.PublicKey()),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip: &SignedIP{
					BLSSignature: must(blsKey.SignProofOfPossession((&UnsignedIP{}).bytes())),
				},
			},
			expectedPeer: &peer{
				Config: &Config{
					Log:                  log.NewNoOpLogger(),
					VersionCompatibility: version.GetCompatibility(upgrade.InitiallyActiveTime),
					Validators: func() validators.Manager {
						vdrs := validators.NewManager()
						require.NoError(t, addStaker(vdrs,
							constants.PrimaryNetworkID,
							peerID,
							bls.PublicKeyToUncompressedBytes(blsKey.PublicKey()),
							txID,
							1,
						))
						return vdrs
					}(),
				},
				id:      peerID,
				version: version.CurrentApp,
				ip: &SignedIP{
					BLSSignature: must(blsKey.SignProofOfPossession((&UnsignedIP{}).bytes())),
				},
				txIDOfVerifiedBLSKey: txID,
			},
			expectedShouldDisconnect: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Initialize version strings in both peers to ensure consistent comparison.
			// NoOpLogger doesn't evaluate arguments, so we must manually trigger
			// the lazy initialization of version.String() for both peers.
			if test.initialPeer.version != nil {
				_ = test.initialPeer.version.String()
			}
			if test.expectedPeer.version != nil {
				_ = test.expectedPeer.version.String()
			}

			shouldDisconnect := test.initialPeer.shouldDisconnect()
			require.Equal(test.expectedPeer, test.initialPeer)
			require.Equal(test.expectedShouldDisconnect, shouldDisconnect)
		})
	}
}

// Helper to send a message from sender to receiver and assert that the
// receiver receives the message. This can be used to test a prior message
// was handled by the peer.
func sendAndFlush(t *testing.T, sender *testPeer, receiver *testPeer) {
	t.Helper()
	mc := newMessageCreator(t)
	outboundGetMsg, err := mc.Get(ids.Empty, 1, time.Second, ids.Empty)
	require.NoError(t, err)
	require.True(t, sender.Send(context.Background(), outboundGetMsg))
	inboundGetMsg := <-receiver.inboundMsgChan
	require.Equal(t, message.GetOp, inboundGetMsg.Op())
}
