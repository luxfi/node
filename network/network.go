// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pires/go-proxyproto"
	"go.uber.org/zap"

	consensusconfig "github.com/luxfi/consensus/config"
	consensustracker "github.com/luxfi/consensus/networking/tracker"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/metric"
	"github.com/luxfi/net/endpoints"
	"github.com/luxfi/node/genesis/builder"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/network/dialer"
	"github.com/luxfi/node/network/kem"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/network/tracker"
	"github.com/luxfi/node/service/health"
	"github.com/luxfi/node/utils/bloom"
	"github.com/luxfi/node/utils/wrappers"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms/platformvm/genesis"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/warp"

	safemath "github.com/luxfi/math"
)

const (
	PrimaryNetworkValidatorHealthKey = "primary network validator health"
	ConnectedPeersKey                = "connectedPeers"
	TimeSinceLastMsgReceivedKey      = "timeSinceLastMsgReceived"
	TimeSinceLastMsgSentKey          = "timeSinceLastMsgSent"
	SendFailRateKey                  = "sendFailRate"
)

var (
	_ Network = (*network)(nil)

	errNotValidator           = errors.New("node is not a validator")
	errExpectedProxy          = errors.New("expected proxy")
	errExpectedTCPProtocol    = errors.New("expected TCP protocol")
	errTrackingPrimaryNetwork = errors.New("cannot track primary network")
)

// noOpAllower is an Allower that always returns true
type noOpAllower struct{}

func (noOpAllower) IsAllowed(ids.NodeID, bool) bool {
	return true
}

// Message represents a network message
type Message = message.OutboundMessage

// ExternalSender sends messages to peers
type ExternalSender interface {
	Send(msg Message, nodeIDs set.Set[ids.NodeID], chainID ids.ID, requestID uint32) set.Set[ids.NodeID]
	Gossip(msg Message, nodeIDs set.Set[ids.NodeID], chainID ids.ID, numValidatorsToSend int, numNonValidatorsToSend int, numPeersToSend int) set.Set[ids.NodeID]
}

// ExternalHandler handles incoming messages
type ExternalHandler interface {
	Connected(nodeID ids.NodeID, version *version.Application, chainID ids.ID)
	Disconnected(nodeID ids.NodeID)
	HandleInbound(ctx context.Context, msg message.InboundMessage)
}

// Network defines the functionality of the networking library.
type Network interface {
	// All consensus messages can be sent through this interface. Thread safety
	// must be managed internally in the network.
	ExternalSender

	// Has a health check
	health.Checker

	peer.Network

	// StartClose this network and all existing connections it has. Calling
	// StartClose multiple times is handled gracefully.
	StartClose()

	// Should only be called once, will run until either a fatal error occurs,
	// or the network is closed.
	Dispatch() error

	// Attempt to connect to this endpoint. The network will never stop attempting to
	// connect to this ID. The endpoint can be either an IP:port or hostname:port.
	ManuallyTrack(nodeID ids.NodeID, endpoint endpoints.Endpoint)

	// PeerInfo returns information about peers. If [nodeIDs] is empty, returns
	// info about all peers that have finished the handshake. Otherwise, returns
	// info about the peers in [nodeIDs] that have finished the handshake.
	PeerInfo(nodeIDs []ids.NodeID) []peer.Info

	// NodeUptime returns given node's primary network UptimeResults in the view of
	// this node's peer validators.
	NodeUptime() (UptimeResult, error)

	// TrackChain starts tracking a chain. This updates the local node's tracked
	// chains and will be included in handshakes with new peers.
	TrackChain(chainID ids.ID) error

	// TrackedChains returns the set of chains this node is tracking.
	TrackedChains() set.Set[ids.ID]

	// RegisterBlockchainNetwork registers a mapping from a blockchain ID to its
	// chain network ID. This is used by the gossip layer to resolve which
	// validator set sequences blocks for a given blockchain, and to check
	// whether peers track the chain network that owns the blockchain.
	RegisterBlockchainNetwork(blockchainID, networkID ids.ID)
}

type UptimeResult struct {
	// RewardingStakePercentage shows what percent of network stake thinks we're
	// above the uptime requirement.
	RewardingStakePercentage float64

	// WeightedAveragePercentage is the average perceived uptime of this node,
	// weighted by stake.
	// Note that this is different from RewardingStakePercentage, which shows
	// the percent of the network stake that thinks this node is above the
	// uptime requirement. WeightedAveragePercentage is weighted by uptime.
	// i.e If uptime requirement is 85 and a peer reports 40 percent it will be
	// counted (40*weight) in WeightedAveragePercentage but not in
	// RewardingStakePercentage since 40 < 85
	WeightedAveragePercentage float64
}

// To avoid potential deadlocks, we maintain that locks must be grabbed in the
// following order:
//
// 1. peersLock
// 2. manuallyTrackedIDsLock
//
// If a higher lock (e.g. manuallyTrackedIDsLock) is held when trying to grab a
// lower lock (e.g. peersLock) a deadlock could occur.
type network struct {
	config     *Config
	peerConfig *peer.Config
	metrics    *metricsImpl

	outboundMsgThrottler throttling.OutboundMsgThrottler

	// Limits the number of connection attempts based on IP.
	inboundConnUpgradeThrottler throttling.InboundConnUpgradeThrottler
	// Listens for and accepts new inbound connections
	listener net.Listener
	// Makes new outbound connections (supports both IP and hostname endpoints)
	dialer dialer.EndpointDialer
	// Does TLS handshakes for inbound connections
	serverUpgrader peer.Upgrader
	// Does TLS handshakes for outbound connections
	clientUpgrader peer.Upgrader

	// ensures the close of the network only happens once.
	closeOnce sync.Once
	// Cancelled on close
	onCloseCtx context.Context
	// Call [onCloseCtxCancel] to cancel [onCloseCtx] during close()
	onCloseCtxCancel context.CancelFunc

	sendFailRateCalculator safemath.Averager

	// Tracks which peers know about which peers
	ipTracker *ipTracker
	peersLock sync.RWMutex
	// trackedIPs contains the set of IPs that we are currently attempting to
	// connect to. An entry is added to this set when we first start attempting
	// to connect to the peer. An entry is deleted from this set once we have
	// finished the handshake.
	trackedIPs map[ids.NodeID]*trackedIP
	// manualEndpoints stores the explicitly configured endpoints for bootstrap
	// peers (set via ManuallyTrack). These take precedence over IPs learned
	// from PeerList gossip when reconnecting after a disconnect. This ensures
	// that port-forwarded bootstrap IPs (e.g. 127.0.0.1:19641) are preserved
	// and used for reconnection instead of the cluster-internal IPs advertised
	// by the remote node.
	manualEndpoints map[ids.NodeID]endpoints.Endpoint
	connectingPeers peer.Set
	connectedPeers  peer.Set
	closing         bool

	startupTime time.Time

	// router is notified about all peer [Connected] and [Disconnected] events
	// as well as all non-handshake peer messages.
	//
	// It is ensured that [Connected] and [Disconnected] are called in
	// consistent ways. Specifically, a peer starts in the disconnected
	// state and the network can change the peer's state from disconnected to
	// connected and back.
	//
	// It is ensured that [HandleInbound] is only called with a message from a
	// peer that is in the connected state.
	//
	// It is expected that the implementation of this interface can handle
	// concurrent calls to [Connected], [Disconnected], and [HandleInbound].
	router ExternalHandler

	// blockchainToNetwork maps blockchain IDs to their chain network IDs.
	// This is needed for chain gossip: when gossiping a block for another chain
	// blockchain, we need to know which chain network's validator set to
	// use for peer sampling, and which network ID to check in peers'
	// trackedChains. Protected by peersLock.
	blockchainToNetwork map[ids.ID]ids.ID
}

// NewNetwork returns a new Network implementation with the provided parameters.
func NewNetwork(
	config *Config,
	minCompatibleTime time.Time,
	msgCreator message.Creator,
	metricsRegistry metric.Registry,
	log log.Logger,
	listener net.Listener,
	dialer dialer.EndpointDialer,
	router ExternalHandler,
) (Network, error) {
	if config.ProxyEnabled {
		// Wrap the listener to process the proxy header.
		listener = &proxyproto.Listener{
			Listener: listener,
			Policy: func(net.Addr) (proxyproto.Policy, error) {
				// Do not perform any fuzzy matching, the header must be
				// provided.
				return proxyproto.REQUIRE, nil
			},
			ValidateHeader: func(h *proxyproto.Header) error {
				if !h.Command.IsProxy() {
					return errExpectedProxy
				}
				if h.TransportProtocol != proxyproto.TCPv4 && h.TransportProtocol != proxyproto.TCPv6 {
					return errExpectedTCPProtocol
				}
				return nil
			},
			ReadHeaderTimeout: config.ProxyReadHeaderTimeout,
		}
	}

	if config.TrackedChains.Contains(constants.PrimaryNetworkID) {
		return nil, errTrackingPrimaryNetwork
	}

	// Wrap consensus ResourceTracker to match network tracker interface
	resourceTrackerWrapper := &resourceTrackerWrapper{rt: config.ResourceTracker}

	inboundMsgThrottler, err := throttling.NewInboundMsgThrottler(
		log,
		metricsRegistry,
		config.Validators,
		config.ThrottlerConfig.InboundMsgThrottlerConfig,
		resourceTrackerWrapper,
		config.CPUTargeter,
		config.DiskTargeter,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing inbound message throttler failed with: %w", err)
	}

	outboundMsgThrottler, err := throttling.NewSybilOutboundMsgThrottler(
		log,
		metricsRegistry,
		config.Validators,
		config.ThrottlerConfig.OutboundMsgThrottlerConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("initializing outbound message throttler failed with: %w", err)
	}

	peerMetrics, err := peer.NewMetrics(metricsRegistry)
	if err != nil {
		return nil, fmt.Errorf("initializing peer metrics failed with: %w", err)
	}

	metrics, err := newMetrics(metricsRegistry, config.TrackedChains)
	if err != nil {
		return nil, fmt.Errorf("initializing network metrics failed with: %w", err)
	}

	ipTracker, err := newIPTracker(config.TrackedChains, log, metricsRegistry)
	if err != nil {
		return nil, fmt.Errorf("initializing ip tracker failed with: %w", err)
	}
	config.Validators.RegisterCallbackListener(ipTracker)

	// Track all default bootstrappers to ensure their current IPs are gossiped
	// like validator IPs.
	bootstrappers, err := builder.GetBootstrappers(config.NetworkID)
	if err != nil {
		log.Warn("failed to get bootstrappers", zap.Error(err))
	}
	for _, bootstrapper := range bootstrappers {
		ipTracker.ManuallyGossip(constants.PrimaryNetworkID, bootstrapper.ID)
	}
	// Track all recent validators to optimistically connect to them before the
	// P-chain has finished syncing.
	//
	// CRITICAL: We first try to parse the actual genesis bytes passed to the node.
	// This ensures we use the correct validators for networks started with custom
	// genesis (e.g., netrunner) rather than canonical configs which may differ.
	var genesisStakers []struct {
		NodeID ids.NodeID
		Weight uint64
		BLSKey []byte
	}

	// First, try to parse actual genesis bytes from node config using P-chain codec
	if len(config.GenesisBytes) > 0 {
		parsedGenesis, err := genesis.Parse(config.GenesisBytes)
		if err == nil && len(parsedGenesis.Validators) > 0 {
			log.Info("using actual P-chain genesis bytes for initial stakers",
				zap.Int("count", len(parsedGenesis.Validators)),
			)
			for _, validatorTx := range parsedGenesis.Validators {
				// Extract validator details from the transaction.
				// Genesis may encode validators as AddValidatorTx (legacy) or
				// AddPermissionlessValidatorTx (modern). Handle both.
				switch tx := validatorTx.Unsigned.(type) {
				case *txs.AddPermissionlessValidatorTx:
					validator := tx.Validator()
					nodeID := validator.NodeID
					weight := validator.Wght
					if weight == 0 {
						weight = 1
					}

					var blsKey []byte
					if s := tx.Signer(); s != nil {
						if pubKey := s.Key(); pubKey != nil {
							blsKey = bls.PublicKeyToCompressedBytes(pubKey)
						}
					}

					genesisStakers = append(genesisStakers, struct {
						NodeID ids.NodeID
						Weight uint64
						BLSKey []byte
					}{
						NodeID: nodeID,
						Weight: weight,
						BLSKey: blsKey,
					})
					log.Debug("parsed genesis validator from P-chain genesis",
						zap.Stringer("nodeID", nodeID),
						zap.Uint64("weight", weight),
						zap.Int("blsKeyLen", len(blsKey)),
					)
				case *txs.AddValidatorTx:
					validator := tx.Validator()
					nodeID := validator.NodeID
					weight := validator.Wght
					if weight == 0 {
						weight = 1
					}

					genesisStakers = append(genesisStakers, struct {
						NodeID ids.NodeID
						Weight uint64
						BLSKey []byte
					}{
						NodeID: nodeID,
						Weight: weight,
					})
					log.Debug("parsed genesis validator (AddValidatorTx) from P-chain genesis",
						zap.Stringer("nodeID", nodeID),
						zap.Uint64("weight", weight),
					)
				default:
					log.Warn("unknown validator tx type in genesis",
						zap.String("type", fmt.Sprintf("%T", validatorTx.Unsigned)),
					)
				}
			}
		} else if err != nil {
			log.Debug("failed to parse P-chain genesis bytes, will try canonical config",
				zap.Error(err),
			)
		}
	}

	// Fall back to canonical config if no genesis bytes or parsing failed
	if len(genesisStakers) == 0 {
		genesisConfig := builder.GetConfig(config.NetworkID)
		if genesisConfig != nil && len(genesisConfig.InitialStakers) > 0 {
			log.Info("using canonical genesis config for initial stakers",
				zap.Int("count", len(genesisConfig.InitialStakers)),
			)
			for _, staker := range genesisConfig.InitialStakers {
				var blsKey []byte
				if staker.Signer != nil && staker.Signer.PublicKey != "" {
					blsKey, _ = base64.StdEncoding.DecodeString(staker.Signer.PublicKey)
				}
				weight := staker.Weight
				if weight == 0 {
					weight = 1
				}
				genesisStakers = append(genesisStakers, struct {
					NodeID ids.NodeID
					Weight uint64
					BLSKey []byte
				}{
					NodeID: staker.NodeID,
					Weight: weight,
					BLSKey: blsKey,
				})
			}
		}
	}

	// Add all genesis stakers to validators manager and track their IPs
	for _, staker := range genesisStakers {
		ipTracker.ManuallyTrack(staker.NodeID)

		// CRITICAL FIX: Add genesis validators to the validators manager
		// so the network layer can sample them for consensus voting.
		// Without this, NumValidators() returns 0 and SendPullQuery fails
		// because samplePeers() has no validators to query.

		// Generate a dummy txID from nodeID for genesis validators
		dummyTxID := ids.Empty
		copy(dummyTxID[:], staker.NodeID.Bytes())

		if err := config.Validators.AddStaker(
			constants.PrimaryNetworkID,
			staker.NodeID,
			staker.BLSKey,
			dummyTxID,
			staker.Weight,
		); err != nil {
			log.Warn("failed to add genesis validator",
				zap.Stringer("nodeID", staker.NodeID),
				zap.Error(err),
			)
		} else {
			log.Info("added genesis validator to network",
				zap.Stringer("nodeID", staker.NodeID),
				zap.Uint64("weight", staker.Weight),
			)
		}
	}

	peerConfig := &peer.Config{
		ReadBufferSize:       config.PeerReadBufferSize,
		WriteBufferSize:      config.PeerWriteBufferSize,
		Metrics:              peerMetrics,
		MessageCreator:       msgCreator,
		Log:                  log,
		InboundMsgThrottler:  inboundMsgThrottler,
		Network:              nil, // This is set below.
		Router:               router,
		VersionCompatibility: version.GetCompatibility(minCompatibleTime),
		MyNodeID:             config.MyNodeID,
		MyChains:             config.TrackedChains,
		Beacons:              config.Beacons,
		Validators:           config.Validators,
		NetworkID:            config.NetworkID,
		PingFrequency:        config.PingFrequency,
		PongTimeout:          config.PingPongTimeout,
		MaxClockDifference:   config.MaxClockDifference,
		SupportedLPs:         config.SupportedLPs.List(),
		ObjectedLPs:          config.ObjectedLPs.List(),
		ResourceTracker:      resourceTrackerWrapper,
		UptimeCalculator:     config.UptimeCalculator,
		IPSigner:             peer.NewIPSigner(config.MyIPPort, config.TLSKey, config.BLSKey),
	}

	// Build the per-chain peer.SchemeGate exactly once at network bootstrap.
	// The gate is the single cross-axis primitive that funnels every
	// inbound NodeID through the chain's ChainSecurityProfile pin.
	//
	// The gate is built under the SAME predicate that builds the PQ
	// handshake (profileRequiresPQHandshake), not merely "SecurityProfile
	// != nil". This is load-bearing: the gate's pinned scheme byte
	// (SigSchemeMLDSA65) only becomes presentable by a peer once the
	// application-layer PQ handshake establishes the ML-DSA identity. A
	// profile that does NOT run the PQ handshake (permissive /
	// classical-compat) still presents classical secp256k1 cert schemes,
	// which SchemeGate.Classify refuses unconditionally — its
	// AcceptsValidatorScheme(_, classicalCompatUnsafe=false) hardcodes the
	// classical escape hatch off. Building a gate in that state refuses
	// every peer at the upgrade with no PQ handshake to recover, the exact
	// "TLS upgrade failed" / 0-peers stall a permissive-pinned classical
	// network hits. One axis, one predicate: gate + handshake + ML-DSA
	// identity are built together or not at all. Chains that ship no
	// profile, or a non-PQ-handshake profile, leave the gate nil —
	// upgrader.connToIDAndCert is nil-safe and refuses nobody.
	var schemeGate *peer.SchemeGate
	if profileRequiresPQHandshake(config.SecurityProfile) {
		schemeGate, err = peer.NewSchemeGate(config.SecurityProfile, 0)
		if err != nil {
			return nil, fmt.Errorf("building peer SchemeGate: %w", err)
		}
		log.Info("peer SchemeGate active",
			zap.String("profile", config.SecurityProfile.ProfileName),
			zap.Uint32("profileID", config.SecurityProfile.ProfileID),
		)
	}

	// On a strict-PQ profile, also pin the application-layer PQ handshake
	// config onto every peer goroutine. peer.Start runs the ML-KEM +
	// ML-DSA handshake the moment TLS upgrade succeeds; bare TLS is
	// refused. Permissive / classical-compat profiles leave PQHandshake
	// nil and use the legacy bare-TLS path. Same predicate as the
	// SchemeGate build above — profileRequiresPQHandshake is nil-safe, so
	// the gate and the handshake are guarded by one identical condition.
	if profileRequiresPQHandshake(config.SecurityProfile) {
		// The PQ peer handshake MUST sign with the node's PERSISTENT
		// staking ML-DSA-65 keypair — the one whose public half derives
		// MyNodeID — so that completing the handshake proves possession of
		// the validator identity. The old peer.NewLocalIdentity path
		// generated an EPHEMERAL keypair per process, signing a NodeID that
		// no peer could bind back to the validator set; that is exactly why
		// every strict-PQ peer was classified as a non-validator and the
		// P-chain never formed consensus (no block production).
		if config.StakingMLDSA == nil || len(config.StakingMLDSAPub) == 0 {
			return nil, errors.New("network: strict-PQ profile requires the staking ML-DSA keypair, but StakingMLDSA/StakingMLDSAPub were not populated on the network config")
		}
		pqIdent, err := peer.NewLocalIdentityFromStakingKey(config.MyNodeID, config.StakingMLDSAPub, config.StakingMLDSA.Bytes())
		if err != nil {
			return nil, fmt.Errorf("building PQ local identity from staking ML-DSA key: %w", err)
		}
		peerConfig.PQHandshakeConfig = &peer.HandshakeConfig{
			Profile:            peer.ProfileStrictPQ,
			KEMScheme:          kemSessionScheme(config.SecurityProfile),
			ForbidClassicalKEM: true,
		}
		peerConfig.PQLocalIdentity = pqIdent
		log.Info("peer PQ handshake active",
			zap.Stringer("scheme", peerConfig.PQHandshakeConfig.KEMScheme),
			zap.Stringer("nodeID", config.MyNodeID),
		)
	}

	// When the application-layer PQ handshake is active, TLS is transport-
	// only: peer leaf certs are ephemeral ECDSA (classical scheme) by design
	// (schemeFromCert classifies every current cert as classical until an
	// ML-DSA cert extension lands), and the authoritative ML-DSA NodeID is
	// established + bound by the PQ handshake — which forbids classical KEM
	// and verifies the key-derived NodeID (see verifyPQIdentityBinding).
	// Feeding the classical TLS-cert scheme to the strict-PQ SchemeGate would
	// refuse every connection at the upgrade, before the PQ handshake can run
	// (the cause of the strict-PQ "TLS upgrade failed" / 0-peers stall).
	// Defer scheme admission to the PQ handshake by passing a nil gate to the
	// upgrader (connToIDAndCert is nil-safe). The SchemeGate still guards the
	// legacy / classical-compat path (no PQ handshake), and bare-TLS peers are
	// still refused — just at the PQ handshake, not the gate.
	upgraderGate := schemeGate
	if peerConfig.PQHandshakeConfig != nil {
		upgraderGate = nil
	}

	onCloseCtx, cancel := context.WithCancel(context.Background())
	n := &network{
		startupTime:          time.Now(),
		config:               config,
		peerConfig:           peerConfig,
		metrics:              metrics,
		outboundMsgThrottler: outboundMsgThrottler,

		inboundConnUpgradeThrottler: throttling.NewInboundConnUpgradeThrottler(config.ThrottlerConfig.InboundConnUpgradeThrottlerConfig),
		listener:                    listener,
		dialer:                      dialer,
		serverUpgrader:              peer.NewTLSServerUpgrader(config.TLSConfig, metrics.tlsConnRejected, upgraderGate),
		clientUpgrader:              peer.NewTLSClientUpgrader(config.TLSConfig, metrics.tlsConnRejected, upgraderGate),

		onCloseCtx:       onCloseCtx,
		onCloseCtxCancel: cancel,

		sendFailRateCalculator: safemath.NewSyncAverager(safemath.NewAverager(
			0,
			config.SendFailRateHalflife,
			time.Now(),
		)),

		trackedIPs:      make(map[ids.NodeID]*trackedIP),
		manualEndpoints: make(map[ids.NodeID]endpoints.Endpoint),
		ipTracker:       ipTracker,
		connectingPeers: peer.NewSet(),
		connectedPeers:  peer.NewSet(),
		router:          router,

		blockchainToNetwork: make(map[ids.ID]ids.ID),
	}
	n.peerConfig.Network = n

	// Log network initialization details for debugging identity issues.
	ephemeralCert := config.TLSConfig != nil && config.TLSConfig.Certificates != nil
	log.Info("network initialized",
		zap.Stringer("nodeID", config.MyNodeID),
		zap.Uint32("networkID", config.NetworkID),
		zap.String("listenerAddr", listener.Addr().String()),
		zap.Bool("proxyEnabled", config.ProxyEnabled),
		zap.Bool("hasTLSCerts", ephemeralCert),
	)
	return n, nil
}

// profileRequiresPQHandshake reports whether the chain's locked
// ChainSecurityProfile mandates the application-layer ML-KEM + ML-DSA
// handshake on top of TLS. Strict-PQ and FIPS profiles do; permissive
// profiles fall back to the legacy bare-TLS handshake. Centralised here
// so the same predicate decides "build SchemeGate" and "build PQ
// handshake config" — one rule, one place.
func profileRequiresPQHandshake(p *consensusconfig.ChainSecurityProfile) bool {
	if p == nil {
		return false
	}
	id := consensusconfig.ProfileID(p.ProfileID)
	return id == consensusconfig.ProfileStrictPQ || id == consensusconfig.ProfileFIPS
}

// kemSessionScheme returns the KEM byte the chain's ChainSecurityProfile
// pins for per-peer session keys. Strict-PQ defaults to ML-KEM-768 (NIST
// PQ Cat 3); a chain profile that pins ML-KEM-1024 (Cat 5) for sessions
// is honoured. The DKG KEM (ML-KEM-1024 on strict-PQ — a separate axis
// captured in HighValueKEM) is not consumed by the peer handshake.
func kemSessionScheme(p *consensusconfig.ChainSecurityProfile) kem.KeyExchangeID {
	if p != nil && p.KeyExchangeID == consensusconfig.KeyExchangeMLKEM1024 {
		return kem.KeyExchangeMLKEM1024
	}
	return kem.KeyExchangeMLKEM768
}

// sequencerID returns the validator-set identity that sequences chainID.
// This resolves the distinction between:
//   - chainID: execution domain (C-Chain, Zoo chain, etc.)
//   - sequencerID: validator-set / sequencing authority for a given chain
//
// For example:
//   - C-Chain is sequenced by PrimaryNetworkID validators
//   - A self-sequenced chain uses its own chainID as sequencerID
//   - non-primary chains map to their chain ID for validator lookups
func (n *network) sequencerID(chainID ids.ID) ids.ID {
	// Primary network is a special routing concept; membership is still primary.
	if chainID == constants.PrimaryNetworkID {
		return constants.PrimaryNetworkID
	}
	// Native chains (P, C, X, Q, A, B, T, Z, G, K, D) are all sequenced
	// by the primary network. This check MUST come before the callback check
	// because PrimaryNetworkID == ids.Empty, and the callback returns
	// PrimaryNetworkID for native chains, which would be incorrectly
	// filtered out by the `sid != ids.Empty` guard below.
	if ids.IsNativeChain(chainID) {
		return constants.PrimaryNetworkID
	}
	if n.config.SequencerIDForChain != nil {
		if sid := n.config.SequencerIDForChain(chainID); sid != ids.Empty {
			return sid
		}
	}
	// Check if this is a blockchain ID that maps to a chain ID.
	// This is needed for chain gossip: validators are registered under
	// chain IDs, not blockchain IDs.
	if netID, ok := n.blockchainToNetwork[chainID]; ok {
		return netID
	}
	// Safe default: self-sequenced or unknown mapping.
	return chainID
}

func (n *network) Send(
	msg Message,
	nodeIDs set.Set[ids.NodeID],
	chainID ids.ID,
	requestID uint32,
) set.Set[ids.NodeID] {
	// Create a default allowance policy that allows all connections
	var allower nets.Allower = &noOpAllower{}

	// Use provided nodeIDs directly
	namedPeers := n.getPeers(nodeIDs, chainID, allower)
	n.peerConfig.Metrics.MultipleSendsFailed(
		msg.Op(),
		nodeIDs.Len()-len(namedPeers),
	)

	// Only sample additional peers if no specific nodeIDs were requested
	var sampledPeers []peer.Peer
	if nodeIDs.Len() == 0 {
		// Create default send config for sampling.
		// We sample validators by default (defaultSampleK) to ensure
		// consensus messages reach validators.
		sendConfig := warp.SendConfig{
			NodeIDs:       nil,            // Don't restrict to specific nodes for sampling
			Validators:    defaultSampleK, // Sample validators (samplePeers will clamp to available)
			NonValidators: 0,              // No specific non-validator requirement
			Peers:         1,              // Sample 1 peer by default
		}
		sampledPeers = n.samplePeers(sendConfig, chainID, allower)
	}

	var (
		sentTo = set.NewSet[ids.NodeID](len(namedPeers) + len(sampledPeers))
		now    = n.peerConfig.Clock.Time()
	)

	// send to peers and update metrics
	//
	// Note: It is guaranteed that namedPeers and sampledPeers are disjoint.
	for _, peers := range [][]peer.Peer{namedPeers, sampledPeers} {
		for _, peer := range peers {
			if peer.Send(n.onCloseCtx, msg) {
				sentTo.Add(peer.ID())

				// record metrics for success
				n.sendFailRateCalculator.Observe(0, now)
			} else {
				// record metrics for failure
				n.sendFailRateCalculator.Observe(1, now)
			}
		}
	}
	return sentTo
}

// Gossip implements the ExternalSender interface
func (n *network) Gossip(
	msg Message,
	nodeIDs set.Set[ids.NodeID],
	chainID ids.ID,
	numValidatorsToSend int,
	numNonValidatorsToSend int,
	numPeersToSend int,
) set.Set[ids.NodeID] {
	// If specific nodeIDs are provided, send to them directly
	if nodeIDs != nil && nodeIDs.Len() > 0 {
		return n.Send(msg, nodeIDs, chainID, 0)
	}

	// Sample peers based on the gossip parameters
	var allower nets.Allower = &noOpAllower{}

	// Handle -1 as "all validators"
	validators := numValidatorsToSend
	if validators < 0 {
		// Get all connected validators for this network
		validators = 1000 // Large number to get all connected validators
	}

	sendConfig := warp.SendConfig{
		NodeIDs:       nil,
		Validators:    validators,
		NonValidators: numNonValidatorsToSend,
		Peers:         numPeersToSend,
	}

	sampledPeers := n.samplePeers(sendConfig, chainID, allower)

	var (
		sentTo = set.NewSet[ids.NodeID](len(sampledPeers))
		now    = n.peerConfig.Clock.Time()
	)

	for _, peer := range sampledPeers {
		if peer.Send(n.onCloseCtx, msg) {
			sentTo.Add(peer.ID())
			n.sendFailRateCalculator.Observe(0, now)
		} else {
			n.sendFailRateCalculator.Observe(1, now)
		}
	}

	return sentTo
}

// HealthCheck returns information about several network layer health checks.
// 1) Information about health check results
// 2) An error if the health check reports unhealthy
func (n *network) HealthCheck(context.Context) (interface{}, error) {
	n.peersLock.RLock()
	connectedTo := n.connectedPeers.Len()
	n.peersLock.RUnlock()

	sendFailRate := n.sendFailRateCalculator.Read()

	// Make sure we're connected to at least the minimum number of peers
	isConnected := connectedTo >= int(n.config.HealthConfig.MinConnectedPeers)
	healthy := isConnected
	details := map[string]interface{}{
		ConnectedPeersKey: connectedTo,
	}

	// Make sure we've received an incoming message within the threshold
	now := n.peerConfig.Clock.Time()

	lastMsgReceivedAt, msgReceived := n.getLastReceived()
	wasMsgReceivedRecently := msgReceived
	timeSinceLastMsgReceived := time.Duration(0)
	if msgReceived {
		timeSinceLastMsgReceived = now.Sub(lastMsgReceivedAt)
		wasMsgReceivedRecently = timeSinceLastMsgReceived <= n.config.HealthConfig.MaxTimeSinceMsgReceived
		details[TimeSinceLastMsgReceivedKey] = timeSinceLastMsgReceived.String()
		n.metrics.timeSinceLastMsgReceived.Set(float64(timeSinceLastMsgReceived))
	}
	healthy = healthy && wasMsgReceivedRecently

	// Make sure we've sent an outgoing message within the threshold
	lastMsgSentAt, msgSent := n.getLastSent()
	wasMsgSentRecently := msgSent
	timeSinceLastMsgSent := time.Duration(0)
	if msgSent {
		timeSinceLastMsgSent = now.Sub(lastMsgSentAt)
		wasMsgSentRecently = timeSinceLastMsgSent <= n.config.HealthConfig.MaxTimeSinceMsgSent
		details[TimeSinceLastMsgSentKey] = timeSinceLastMsgSent.String()
		n.metrics.timeSinceLastMsgSent.Set(float64(timeSinceLastMsgSent))
	}
	healthy = healthy && wasMsgSentRecently

	// Make sure the message send failed rate isn't too high
	isMsgFailRate := sendFailRate <= n.config.HealthConfig.MaxSendFailRate
	healthy = healthy && isMsgFailRate
	details[SendFailRateKey] = sendFailRate
	n.metrics.sendFailRate.Set(sendFailRate)

	reachablePrimaryNetworkValidator := true
	// If we're a primary network validator, make sure we have ingress connections
	if time.Since(n.startupTime) > n.config.NoIngressValidatorConnectionGracePeriod {
		connectedPrimaryValidatorInfo, isConnectedPrimaryValidatorErr := checkNoIngressConnections(n.config.MyNodeID, n, n.config.Validators)
		reachablePrimaryNetworkValidator = isConnectedPrimaryValidatorErr == nil
		details[PrimaryNetworkValidatorHealthKey] = connectedPrimaryValidatorInfo
	}
	healthy = healthy && reachablePrimaryNetworkValidator

	// emit metrics about the lifetime of peer connections
	n.metrics.updatePeerConnectionLifetimeMetrics()

	// Network layer is healthy
	if healthy || !n.config.HealthConfig.Enabled {
		return details, nil
	}

	var errorReasons []string
	if !isConnected {
		errorReasons = append(errorReasons, fmt.Sprintf("not connected to a minimum of %d peer(s) only %d", n.config.HealthConfig.MinConnectedPeers, connectedTo))
	}
	if !msgReceived {
		errorReasons = append(errorReasons, "no messages received from network")
	} else if !wasMsgReceivedRecently {
		errorReasons = append(errorReasons, fmt.Sprintf("no messages from network received in %s > %s", timeSinceLastMsgReceived, n.config.HealthConfig.MaxTimeSinceMsgReceived))
	}
	if !msgSent {
		errorReasons = append(errorReasons, "no messages sent to network")
	} else if !wasMsgSentRecently {
		errorReasons = append(errorReasons, fmt.Sprintf("no messages from network sent in %s > %s", timeSinceLastMsgSent, n.config.HealthConfig.MaxTimeSinceMsgSent))
	}

	if !isMsgFailRate {
		errorReasons = append(errorReasons, fmt.Sprintf("messages failure send rate %g > %g", sendFailRate, n.config.HealthConfig.MaxSendFailRate))
	}

	if !reachablePrimaryNetworkValidator {
		errorReasons = append(errorReasons, ErrNoIngressConnections.Error())
	}

	return details, fmt.Errorf("network layer is unhealthy reason: %s", strings.Join(errorReasons, ", "))
}

func (n *network) IngressConnCount() int {
	return int(n.peerConfig.IngressConnectionCount.Load())
}

// Connected is called after the peer finishes the handshake.
// Will not be called after [Disconnected] is called with this peer.
func (n *network) Connected(nodeID ids.NodeID) {
	n.peerConfig.Log.Warn("[CONNECTED] PEER HANDSHAKE COMPLETED - moving to connectedPeers",
		zap.Stringer("nodeID", nodeID),
	)
	n.peersLock.Lock()
	peer, ok := n.connectingPeers.GetByID(nodeID)
	if !ok {
		n.peerConfig.Log.Error(
			"unexpectedly connected to peer when not marked as attempting to connect",
			"nodeID", nodeID.String(),
		)
		n.peersLock.Unlock()
		return
	}

	if tracked, ok := n.trackedIPs[nodeID]; ok {
		tracked.stopTracking()
		delete(n.trackedIPs, nodeID)
	}
	n.connectingPeers.Remove(nodeID)
	n.connectedPeers.Add(peer)
	n.peersLock.Unlock()

	peerIP := peer.IP()
	newIP := endpoints.NewClaimedIPPort(
		peer.Cert(),
		peerIP.AddrPort,
		peerIP.Timestamp,
		peerIP.TLSSignature,
	)
	trackedChains := peer.TrackedChains()
	n.ipTracker.Connected(newIP, trackedChains)

	n.metrics.markConnected(peer)

	peerVersion := peer.Version()
	n.router.Connected(nodeID, peerVersion, constants.PrimaryNetworkID)
	for chainID := range n.peerConfig.MyChains {
		if trackedChains.Contains(chainID) {
			n.router.Connected(nodeID, peerVersion, chainID)
		}
	}
}

// AllowConnection returns true if this node should have a connection to the
// provided nodeID. If the node is attempting to connect to the minimum number
// of peers, then it should only connect if this node is a validator, or the
// peer is a validator/beacon.
func (n *network) AllowConnection(nodeID ids.NodeID) bool {
	if !n.config.RequireValidatorToConnect {
		return true
	}
	_, areWeAPrimaryNetworkAValidator := n.config.Validators.GetValidator(constants.PrimaryNetworkID, n.config.MyNodeID)
	return areWeAPrimaryNetworkAValidator || n.ipTracker.WantsConnection(nodeID)
}

func (n *network) Track(claimedIPPorts []*endpoints.ClaimedIPPort) error {
	_, areWeAPrimaryNetworkAValidator := n.config.Validators.GetValidator(constants.PrimaryNetworkID, n.config.MyNodeID)
	for _, ip := range claimedIPPorts {
		if err := n.track(ip, areWeAPrimaryNetworkAValidator); err != nil {
			// Skip invalid entries (e.g. stale timestamps) rather than
			// failing the whole batch. Only invalid TLS signatures are truly
			// malformed; stale timestamps just mean the remote's IP is old.
			n.peerConfig.Log.Debug("skipping claimed IP in Track",
				"nodeID", ip.NodeID.String(),
				"addr", ip.AddrPort.String(),
				"error", err.Error(),
			)
		}
	}
	return nil
}

// Disconnected is called after the peer's handling has been shutdown.
// It is not guaranteed that [Connected] was previously called with [nodeID].
// It is guaranteed that [Connected] will not be called with [nodeID] after this
// call. Note that this is from the perspective of a single peer object, because
// a peer with the same ID can reconnect to this network instance.
func (n *network) Disconnected(nodeID ids.NodeID) {
	n.peersLock.RLock()
	_, connecting := n.connectingPeers.GetByID(nodeID)
	peer, connected := n.connectedPeers.GetByID(nodeID)
	n.peersLock.RUnlock()

	if connecting {
		n.disconnectedFromConnecting(nodeID)
	}
	if connected {
		n.disconnectedFromConnected(peer, nodeID)
	}
}

func (n *network) KnownPeers() ([]byte, []byte) {
	return n.ipTracker.Bloom()
}

// There are 3 types of responses:
//
// - Respond with net IPs tracked by both ourselves and the peer
//   - We do not consider ourself to be a primary network validator
//
// - Respond with all net IPs
//   - The peer requests all peers
//   - We believe ourself to be a primary network validator
//
// - Respond with net IPs tracked by the peer
//   - The peer does not request all peers
//   - We believe ourself to be a primary network validator
//
// The reason we allow the peer to request all peers is so that we can avoid
// sending unnecessary data in the case that we consider them a primary network
// validator but they do not consider themselves one.
func (n *network) Peers(
	peerID ids.NodeID,
	trackedNets set.Set[ids.ID],
	requestAllPeers bool,
	knownPeers *bloom.ReadFilter,
	salt []byte,
) []*endpoints.ClaimedIPPort {
	_, areWeAPrimaryNetworkValidator := n.config.Validators.GetValidator(constants.PrimaryNetworkID, n.config.MyNodeID)

	// Only return IPs for nets that we are tracking.
	var allowedNets func(ids.ID) bool
	if areWeAPrimaryNetworkValidator {
		allowedNets = func(ids.ID) bool { return true }
	} else {
		allowedNets = func(chainID ids.ID) bool {
			return chainID == constants.PrimaryNetworkID || n.ipTracker.trackedNets.Contains(chainID)
		}
	}

	if areWeAPrimaryNetworkValidator && requestAllPeers {
		// Return IPs for all nets.
		return getGossipableIPs(
			n.ipTracker,
			n.ipTracker.net,
			allowedNets,
			peerID,
			knownPeers,
			salt,
			int(n.config.PeerListNumValidatorIPs),
		)
	}
	return getGossipableIPs(
		n.ipTracker,
		trackedNets,
		allowedNets,
		peerID,
		knownPeers,
		salt,
		int(n.config.PeerListNumValidatorIPs),
	)
}

// Dispatch starts accepting connections from other nodes attempting to connect
// to this node.
func (n *network) Dispatch() error {
	n.peerConfig.Log.Info("Dispatch starting accept loop",
		zap.Stringer("nodeID", n.config.MyNodeID),
		zap.String("listenerAddr", n.listener.Addr().String()),
	)

	go n.runTimers() // Periodically perform operations
	go n.inboundConnUpgradeThrottler.Dispatch()

	var acceptCount uint64
	for n.onCloseCtx.Err() == nil { // Continuously accept new connections
		conn, err := n.listener.Accept() // Returns error when n.Close() is called
		if err != nil {
			// Distinguish normal close from unexpected accept errors.
			if n.onCloseCtx.Err() != nil {
				n.peerConfig.Log.Info("accept loop ending: context cancelled",
					zap.Uint64("totalAccepted", acceptCount),
				)
				break
			}
			n.peerConfig.Log.Debug("error during server accept", "error", err)
			// Sleep for a small amount of time to try to wait for the
			// error to go away.
			time.Sleep(time.Millisecond)
			n.metrics.acceptFailed.Inc()
			continue
		}
		acceptCount++

		// Note: listener.Accept is rate limited outside of this package, so a
		// peer can not just arbitrarily spin up goroutines here.
		go func() {
			// Note: Calling [RemoteAddr] with the Proxy protocol enabled may
			// block for up to ProxyReadHeaderTimeout. Therefore, we ensure to
			// call this function inside the go-routine, rather than the main
			// accept loop.
			remoteAddr := conn.RemoteAddr().String()
			ip, err := endpoints.ParseAddrPort(remoteAddr)
			if err != nil {
				n.peerConfig.Log.Error("failed to parse remote address",
					"peerIP", remoteAddr,
					"error", err,
				)
				_ = conn.Close()
				return
			}

			if !n.inboundConnUpgradeThrottler.ShouldUpgrade(ip) {
				n.peerConfig.Log.Debug("failed to upgrade connection",
					"reason", "rate-limiting",
					"peerIP", ip.String(),
				)
				n.metrics.inboundConnRateLimited.Inc()
				_ = conn.Close()
				return
			}
			n.metrics.inboundConnAllowed.Inc()

			n.peerConfig.Log.Debug("starting to upgrade connection",
				"direction", "inbound",
				"peerIP", ip.String(),
			)

			if err := n.upgrade(conn, n.serverUpgrader, true); err != nil {
				n.peerConfig.Log.Verbo("failed to upgrade connection",
					log.String("direction", "inbound"),
					zap.Error(err),
				)
			}
		}()
	}

	// Log why the accept loop exited.
	if ctxErr := n.onCloseCtx.Err(); ctxErr != nil {
		n.peerConfig.Log.Info("Dispatch: accept loop exited due to context cancellation",
			zap.Uint64("totalAccepted", acceptCount),
			zap.Error(ctxErr),
		)
	} else {
		n.peerConfig.Log.Warn("Dispatch: accept loop exited unexpectedly without context cancellation",
			zap.Uint64("totalAccepted", acceptCount),
		)
	}

	n.inboundConnUpgradeThrottler.Stop()
	n.StartClose()

	n.peersLock.RLock()
	connecting := n.connectingPeers.Sample(n.connectingPeers.Len(), peer.NoPrecondition)
	connected := n.connectedPeers.Sample(n.connectedPeers.Len(), peer.NoPrecondition)
	n.peersLock.RUnlock()

	n.peerConfig.Log.Info("Dispatch: draining peer connections",
		zap.Int("connecting", len(connecting)),
		zap.Int("connected", len(connected)),
	)

	errs := wrappers.Errs{}
	for _, peer := range append(connecting, connected...) {
		errs.Add(peer.AwaitClosed(context.TODO()))
	}

	if errs.Err != nil {
		n.peerConfig.Log.Warn("Dispatch: peer drain completed with errors",
			zap.Error(errs.Err),
		)
	} else {
		n.peerConfig.Log.Info("Dispatch: all peers drained cleanly")
	}

	return errs.Err
}

func (n *network) ManuallyTrack(nodeID ids.NodeID, endpoint endpoints.Endpoint) {
	n.peerConfig.Log.Info("ManuallyTrack",
		zap.Stringer("nodeID", nodeID),
		zap.String("endpoint", endpoint.String()),
	)

	// Endpoint-only bootstrap: NodeID is zero, meaning it will be discovered
	// from the peer's staking certificate during the TLS handshake.
	// We dial the endpoint directly and let the upgrader extract the NodeID.
	if nodeID == ids.EmptyNodeID {
		n.peerConfig.Log.Info("ManuallyTrack endpoint-only (NodeID from cert)",
			zap.String("endpoint", endpoint.String()),
		)
		tracked := newTrackedIP(endpoint)
		// Dial directly without using trackedIPs map to avoid key collision
		// (all endpoint-only nodes share EmptyNodeID). The dial goroutine
		// connects, performs TLS handshake, discovers the real NodeID, and
		// the connection upgrade process handles the rest.
		go n.dialEndpointOnly(tracked)
		return
	}

	n.ipTracker.ManuallyTrack(nodeID)

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	// Store the manual endpoint so reconnection after disconnect uses the
	// configured address (e.g., port-forwarded 127.0.0.1:19641) rather than
	// the cluster-internal IP advertised by the remote node via PeerList gossip.
	n.manualEndpoints[nodeID] = endpoint

	_, connected := n.connectedPeers.GetByID(nodeID)
	if connected {
		return
	}

	_, isTracked := n.trackedIPs[nodeID]
	if !isTracked {
		tracked := newTrackedIP(endpoint)
		n.trackedIPs[nodeID] = tracked
		n.dial(nodeID, tracked)
	}
}

func (n *network) track(ip *endpoints.ClaimedIPPort, trackAllNets bool) error {
	// To avoid signature verification when the IP isn't needed, we
	// optimistically filter out IPs. This can result in us not tracking an IP
	// that we otherwise would have. This case can only happen if the node
	// became a validator between the time we verified the signature and when we
	// processed the IP; which should be very rare.
	//
	// Note: Avoiding signature verification when the IP isn't needed is a
	// **significant** performance optimization.
	if !n.ipTracker.ShouldVerifyIP(ip, trackAllNets) {
		n.metrics.numUselessPeerListBytes.Add(float64(ip.Size()))
		return nil
	}

	// Perform all signature verification and hashing before grabbing the peer
	// lock.
	signedIP := peer.SignedIP{
		UnsignedIP: peer.UnsignedIP{
			AddrPort:  ip.AddrPort,
			Timestamp: ip.Timestamp,
		},
		TLSSignature: ip.Signature,
	}
	maxTimestamp := n.peerConfig.Clock.Time().Add(n.peerConfig.MaxClockDifference)
	if err := signedIP.Verify(ip.Cert, maxTimestamp); err != nil {
		return err
	}

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	if !n.ipTracker.AddIP(ip) {
		return nil
	}

	if _, connected := n.connectedPeers.GetByID(ip.NodeID); connected {
		// If I'm currently connected to [nodeID] then I'll attempt to dial them
		// when we disconnect.
		return nil
	}

	tracked, isTracked := n.trackedIPs[ip.NodeID]
	if isTracked {
		// Stop tracking the old IP and start tracking the new one.
		tracked = tracked.trackNewEndpoint(endpoints.NewIPEndpoint(ip.AddrPort))
	} else {
		tracked = newTrackedIP(endpoints.NewIPEndpoint(ip.AddrPort))
	}
	n.trackedIPs[ip.NodeID] = tracked
	n.dial(ip.NodeID, tracked)
	return nil
}

// getPeers returns a slice of connected peers from a set of [nodeIDs].
//
//   - [nodeIDs] the IDs of the peers that should be returned if they are
//     connected.
//   - [chainID] the chainID whose membership should be considered to
//     determine if the node is a validator.
//   - [allower] interface that determines if a node is allowed to connect to
//     the net based on its validator status.
func (n *network) getPeers(
	nodeIDs set.Set[ids.NodeID],
	chainID ids.ID,
	allower nets.Allower,
) []peer.Peer {
	peers := make([]peer.Peer, 0, nodeIDs.Len())

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	// Resolve sequencerID for validator lookups
	// chainID = execution domain (routing), sequencerID = validator membership
	sid := n.sequencerID(chainID)

	for nodeID := range nodeIDs {
		peer, ok := n.connectedPeers.GetByID(nodeID)
		if !ok {
			continue
		}

		// Use sequencerID for validator membership check
		_, areTheyAValidator := n.config.Validators.GetValidator(sid, nodeID)
		// check if the peer is allowed to connect to the net
		if !allower.IsAllowed(nodeID, areTheyAValidator) {
			continue
		}

		peers = append(peers, peer)
	}

	return peers
}

// defaultSampleK is the default sample size when Validators is 0.
// This matches the consensus K parameter from DefaultCoreParams().
const defaultSampleK = 20

// samplePeers samples connected peers attempting to align with the number of
// requested validators, non-validators, and peers. This function will
// explicitly ignore nodeIDs already included in the send config.
//
// IMPORTANT: If config.Validators == 0, we default to sampling up to
// defaultSampleK validators rather than sampling none. This prevents
// the common bug where callers forget to set Validators.
func (n *network) samplePeers(
	config warp.SendConfig,
	chainID ids.ID,
	allower nets.Allower,
) []peer.Peer {
	// Resolve sequencerID for validator lookups
	// chainID = execution domain (routing), sequencerID = validator membership
	sid := n.sequencerID(chainID)

	// As an optimization, if there are fewer validators than
	// [numValidatorsToSample], only attempt to sample [numValidatorsToSample]
	// validators to potentially avoid iterating over the entire peer set.
	numValidatorsInManager := n.config.Validators.NumValidators(sid)

	// FIX: If Validators == 0 was passed (likely caller forgot to set it),
	// default to sampling up to defaultSampleK validators instead of none.
	requestedValidators := config.Validators
	if requestedValidators <= 0 {
		requestedValidators = defaultSampleK
	}
	// Chicken-and-egg fallback: in a freshly bootstrapped sovereign
	// primary network, the validator manager is briefly empty until
	// the first P-chain block commits initial stakers from genesis.
	// During that window, treating peers tracking the chain as
	// validator candidates is the only way out — otherwise P-chain
	// can never produce its first block, because the proposer would
	// sample 0 peers and reach 0 votes. Once the manager has any
	// validators registered, the strict cap returns.
	bootstrapFallback := numValidatorsInManager == 0
	numValidatorsToSample := min(requestedValidators, numValidatorsInManager)
	if bootstrapFallback {
		numValidatorsToSample = requestedValidators
	}

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	return n.connectedPeers.Sample(
		numValidatorsToSample+config.NonValidators+config.Peers,
		func(p peer.Peer) bool {
			trackedChains := p.TrackedChains()

			// Native chains (P, C, X, Q, A, B, T, Z, G, K, D) are all part of
			// the primary network. Peers advertise PrimaryNetworkID in their
			// trackedChains set, not individual native chain IDs. So when
			// gossip uses a native chainID (e.g., PChainID), we must treat
			// it like primary network to avoid filtering out all peers.
			isPrimaryNetwork := chainID == constants.PrimaryNetworkID || ids.IsNativeChain(chainID)
			containsChainID := isPrimaryNetwork || trackedChains.Contains(chainID)

			// For non-primary chains, also check if the peer tracks the chain ID.
			// Peers advertise chain IDs (not blockchain IDs) in their tracked chains,
			// but gossip uses blockchain IDs as the chainID parameter.
			if !containsChainID {
				if netID, ok := n.blockchainToNetwork[chainID]; ok {
					containsChainID = trackedChains.Contains(netID)
				}
			}

			// Only return peers that are tracking [chainID]
			if !containsChainID {
				return false
			}

			peerID := p.ID()
			// if the peer was already explicitly included, don't include in the
			// sample
			if config.NodeIDs != nil && config.NodeIDs.Contains(peerID) {
				return false
			}

			_, areTheyAValidator := n.config.Validators.GetValidator(sid, peerID)
			// check if the peer is allowed to connect to the net
			if !allower.IsAllowed(peerID, areTheyAValidator) {
				return false
			}

			if config.Peers > 0 {
				config.Peers--
				return true
			}

			// Bootstrap fallback (see numValidatorsToSample setup above):
			// when validator manager is empty, every chain-tracking peer
			// counts as a validator candidate. Without this, sample
			// always returns zero because GetValidator() can't yet see
			// the genesis-declared initial stakers.
			if areTheyAValidator || bootstrapFallback {
				numValidatorsToSample--
				return numValidatorsToSample >= 0
			}

			config.NonValidators--
			return config.NonValidators >= 0
		},
	)
}

func (n *network) disconnectedFromConnecting(nodeID ids.NodeID) {
	n.peerConfig.Log.Warn("[DISCONNECT] REMOVING PEER FROM connectingPeers (handshake failed)",
		zap.Stringer("nodeID", nodeID),
	)
	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	n.connectingPeers.Remove(nodeID)

	// The peer that is disconnecting from us didn't finish the handshake
	tracked, ok := n.trackedIPs[nodeID]
	if ok {
		if n.ipTracker.WantsConnection(nodeID) {
			tracked := tracked.trackNewEndpoint(tracked.endpoint)
			n.trackedIPs[nodeID] = tracked
			n.dial(nodeID, tracked)
		} else {
			tracked.stopTracking()
			delete(n.trackedIPs, nodeID)
		}
	}

	n.metrics.disconnected.Inc()
}

func (n *network) disconnectedFromConnected(peer peer.Peer, nodeID ids.NodeID) {
	n.ipTracker.Disconnected(nodeID)
	n.router.Disconnected(nodeID)

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	n.connectedPeers.Remove(nodeID)

	// The peer that is disconnecting from us finished the handshake.
	// If this peer has a manually configured endpoint (e.g. a port-forward
	// address like 127.0.0.1:19641 for bootstrap peers), prefer that over
	// the cluster-internal IP the peer advertised in its Version/PeerList.
	if manualEndpoint, hasManual := n.manualEndpoints[nodeID]; hasManual {
		tracked := newTrackedIP(manualEndpoint)
		n.trackedIPs[nodeID] = tracked
		n.dial(nodeID, tracked)
	} else if ip, wantsConnection := n.ipTracker.GetIP(nodeID); wantsConnection {
		tracked := newTrackedIP(endpoints.NewIPEndpoint(ip.AddrPort))
		n.trackedIPs[nodeID] = tracked
		n.dial(nodeID, tracked)
	}

	n.metrics.markDisconnected(peer)
}

// dial will spin up a new goroutine and attempt to establish a connection with
// [nodeID] at [ip].
//
// If the connection established at [ip] doesn't match [nodeID]:
// - attempts to reach [nodeID] at [ip] will be halted.
// - the connection will be checked to see if the connection is desired or not.
//
// If [ip] has been flagged with [ip.stopTracking] then this goroutine will
// exit.
//
// If [nodeID] is marked as connecting or connected then this goroutine will
// dialEndpointOnly dials a bootstrap endpoint where the NodeID is unknown.
// It connects, performs TLS handshake, and the upgrade process discovers the
// real NodeID from the peer's staking certificate. Unlike dial(), this skips
// the WantsConnection check since there's no NodeID to check against.
func (n *network) dialEndpointOnly(ip *trackedIP) {
	n.peerConfig.Log.Info("dialEndpointOnly: starting",
		zap.String("endpoint", ip.endpoint.String()),
	)
	n.metrics.numTracked.Inc()
	defer n.metrics.numTracked.Dec()

	for {
		select {
		case <-n.onCloseCtx.Done():
			return
		default:
		}

		delay := ip.getDelay()
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-n.onCloseCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}

		conn, err := n.dialer.DialEndpoint(n.onCloseCtx, ip.endpoint)
		if err != nil {
			n.peerConfig.Log.Warn("dialEndpointOnly: TCP dial failed",
				zap.String("endpoint", ip.endpoint.String()),
				zap.Error(err),
			)
			ip.increaseDelay(
				n.config.InitialReconnectDelay,
				n.config.MaxReconnectDelay,
			)
			continue
		}

		n.peerConfig.Log.Info("dialEndpointOnly: TCP connected, upgrading",
			zap.String("endpoint", ip.endpoint.String()),
		)

		err = n.upgrade(conn, n.clientUpgrader, false)
		if err != nil {
			n.peerConfig.Log.Warn("dialEndpointOnly: TLS upgrade failed",
				zap.String("endpoint", ip.endpoint.String()),
				zap.Error(err),
			)
			ip.increaseDelay(
				n.config.InitialReconnectDelay,
				n.config.MaxReconnectDelay,
			)
			continue
		}
		n.peerConfig.Log.Info("dialEndpointOnly: connected successfully",
			zap.String("endpoint", ip.endpoint.String()),
		)
		return
	}
}

// dial attempts to create a connection with the peer at [ip].
//
// The dial will be cancelled when [ip.onStopTracking] is closed or this network
// exit.
//
// If [nodeID] is no longer marked as desired then this goroutine will exit and
// the entry in the [trackedIP]s set will be removed.
//
// If initiating a connection to [ip] fails, then dial will reattempt. However,
// there is a randomized exponential backoff to avoid spamming connection
// attempts.
func (n *network) dial(nodeID ids.NodeID, ip *trackedIP) {
	n.peerConfig.Log.Warn("[DIAL] FUNCTION ENTRY - dial() called",
		zap.Stringer("nodeID", nodeID),
		zap.String("endpoint", ip.endpoint.String()),
	)
	go func() {
		n.peerConfig.Log.Warn("[DIAL] GOROUTINE STARTED",
			zap.Stringer("nodeID", nodeID),
			zap.String("endpoint", ip.endpoint.String()),
		)
		n.metrics.numTracked.Inc()
		defer n.metrics.numTracked.Dec()

		for {
			n.peerConfig.Log.Warn("[DIAL] LOOP ITERATION START",
				zap.Stringer("nodeID", nodeID),
			)
			// Check for early termination before starting the delay timer.
			// This ensures that if the context is already cancelled when dial
			// is called, we return immediately without attempting to connect.
			select {
			case <-n.onCloseCtx.Done():
				n.peerConfig.Log.Warn("[DIAL] context cancelled in pre-check, exiting",
					zap.Stringer("nodeID", nodeID),
				)
				return
			case <-ip.onStopTracking:
				n.peerConfig.Log.Warn("[DIAL] stop tracking in pre-check, exiting",
					zap.Stringer("nodeID", nodeID),
				)
				return
			default:
				n.peerConfig.Log.Warn("[DIAL] pre-check passed, proceeding",
					zap.Stringer("nodeID", nodeID),
				)
			}

			delay := ip.getDelay()
			n.peerConfig.Log.Warn("[DIAL] DELAY VALUE",
				zap.Stringer("nodeID", nodeID),
				zap.Duration("delay", delay),
				zap.Int64("delayMs", delay.Milliseconds()),
			)

			// CRITICAL: For bootstrap nodes with delay=0, skip timer entirely
			if delay == 0 {
				n.peerConfig.Log.Warn("[DIAL] ZERO DELAY - skipping timer",
					zap.Stringer("nodeID", nodeID),
				)
			} else {
				timer := time.NewTimer(delay)
				n.peerConfig.Log.Warn("[DIAL] WAITING ON TIMER",
					zap.Stringer("nodeID", nodeID),
					zap.Duration("delay", delay),
				)
				select {
				case <-n.onCloseCtx.Done():
					timer.Stop()
					n.peerConfig.Log.Warn("[DIAL] context cancelled during timer wait",
						zap.Stringer("nodeID", nodeID),
					)
					return
				case <-ip.onStopTracking:
					timer.Stop()
					n.peerConfig.Log.Warn("[DIAL] stop tracking during timer wait",
						zap.Stringer("nodeID", nodeID),
					)
					return
				case <-timer.C:
					n.peerConfig.Log.Warn("[DIAL] TIMER FIRED",
						zap.Stringer("nodeID", nodeID),
					)
				}
			}

			n.peerConfig.Log.Warn("[DIAL] CHECKING WANTS CONNECTION",
				zap.Stringer("nodeID", nodeID),
			)
			n.peersLock.Lock()
			wantsConn := n.ipTracker.WantsConnection(nodeID)
			n.peerConfig.Log.Warn("[DIAL] WANTS CONNECTION RESULT",
				zap.Stringer("nodeID", nodeID),
				zap.Bool("wantsConnection", wantsConn),
			)
			// If we no longer desire a connect to nodeID, we should cleanup
			// trackedIPs and this goroutine. This prevents a memory leak when
			// the tracked nodeID leaves the validator set and is never able to
			// be connected to.
			if !wantsConn {
				n.peerConfig.Log.Warn("[DIAL] NO LONGER WANTS CONNECTION - cleaning up",
					zap.Stringer("nodeID", nodeID),
				)
				// Typically [n.trackedIPs[nodeID]] will already equal [ip], but
				// the reference to [ip] is refreshed to avoid any potential
				// race conditions before removing the entry.
				if ip, exists := n.trackedIPs[nodeID]; exists {
					ip.stopTracking()
					delete(n.trackedIPs, nodeID)
				}
				n.peersLock.Unlock()
				return
			}
			_, connecting := n.connectingPeers.GetByID(nodeID)
			_, connected := n.connectedPeers.GetByID(nodeID)
			n.peersLock.Unlock()

			n.peerConfig.Log.Warn("[DIAL] CONNECTION STATUS",
				zap.Stringer("nodeID", nodeID),
				zap.Bool("connecting", connecting),
				zap.Bool("connected", connected),
			)

			// If already connected, we can safely exit - the connection is established.
			if connected {
				n.peerConfig.Log.Warn("[DIAL] ALREADY CONNECTED - exiting",
					zap.Stringer("nodeID", nodeID),
				)
				return
			}

			// CRITICAL FIX: If the peer is in "connecting" state (handshake in progress),
			// we should NOT exit. The inbound connection might fail, and we need to be
			// ready to retry. Instead, we continue the loop which will re-check after
			// the delay. If the inbound connection succeeds, we'll see connected=true
			// and exit. If it fails, we'll see connecting=false and proceed to dial.
			if connecting {
				n.peerConfig.Log.Warn("[DIAL] PEER CONNECTING (handshake in progress) - will retry",
					zap.Stringer("nodeID", nodeID),
				)
				// Increase delay before retrying to avoid busy-loop
				ip.increaseDelay(
					n.config.InitialReconnectDelay,
					n.config.MaxReconnectDelay,
				)
				continue
			}

			// Increase the delay that we will use for a future connection
			// attempt.
			ip.increaseDelay(
				n.config.InitialReconnectDelay,
				n.config.MaxReconnectDelay,
			)

			// If the network is configured to disallow private IPs and the
			// provided IP is private, we skip all attempts to initiate a
			// connection. Hostname endpoints are allowed through since we
			// can't know if they're private without resolving.
			//
			// Invariant: We perform this check inside of the looping goroutine
			// because this goroutine must clean up the trackedIPs entry if
			// nodeID leaves the validator set. This is why we continue the loop
			// rather than returning even though we will never initiate an
			// outbound connection with this IP.
			if !n.config.AllowPrivateIPs && ip.endpoint.IsIP() && !endpoints.IsPublic(ip.endpoint.AddrPort.Addr()) {
				n.peerConfig.Log.Warn("[DIAL] SKIPPING PRIVATE IP",
					zap.Stringer("nodeID", nodeID),
					zap.String("peerEndpoint", ip.endpoint.String()),
					zap.Bool("allowPrivateIPs", n.config.AllowPrivateIPs),
				)
				continue
			}

			n.peerConfig.Log.Warn("[DIAL] >>> ATTEMPTING TCP DIAL <<<",
				zap.Stringer("nodeID", nodeID),
				zap.String("peerEndpoint", ip.endpoint.String()),
			)
			conn, err := n.dialer.DialEndpoint(n.onCloseCtx, ip.endpoint)
			if err != nil {
				n.peerConfig.Log.Warn("[DIAL] TCP DIAL FAILED",
					zap.Stringer("nodeID", nodeID),
					zap.String("peerEndpoint", ip.endpoint.String()),
					zap.Error(err),
				)
				continue
			}

			n.peerConfig.Log.Warn("[DIAL] TCP CONNECTED - upgrading to TLS",
				zap.Stringer("nodeID", nodeID),
				zap.String("peerEndpoint", ip.endpoint.String()),
			)

			err = n.upgrade(conn, n.clientUpgrader, false)
			if err != nil {
				n.peerConfig.Log.Warn("[DIAL] TLS UPGRADE FAILED",
					zap.Stringer("nodeID", nodeID),
					zap.String("peerEndpoint", ip.endpoint.String()),
					zap.Error(err),
				)
				continue
			}
			n.peerConfig.Log.Warn("[DIAL] SUCCESS - FULLY CONNECTED",
				zap.Stringer("nodeID", nodeID),
				zap.String("peerEndpoint", ip.endpoint.String()),
			)
			return
		}
	}()
}

// upgrade the provided connection, which may be an inbound connection or an
// outbound connection, with the provided [upgrader].
//
// If the connection is successfully upgraded, [nil] will be returned.
//
// If the connection is desired by the node, then the resulting upgraded
// connection will be used to create a new peer. Otherwise the connection will
// be immediately closed.
func (n *network) upgrade(conn net.Conn, upgrader peer.Upgrader, isIngress bool) error {
	direction := "outbound"
	if isIngress {
		direction = "inbound"
	}

	upgradeTimeout := n.peerConfig.Clock.Time().Add(n.config.ReadHandshakeTimeout)
	if err := conn.SetReadDeadline(upgradeTimeout); err != nil {
		_ = conn.Close()
		n.peerConfig.Log.Debug("failed to set the read deadline",
			"error", err,
		)
		return err
	}

	nodeID, tlsConn, cert, err := upgrader.Upgrade(conn)
	if err != nil {
		_ = conn.Close()
		n.peerConfig.Log.Debug("failed to upgrade connection",
			"error", err,
		)
		return err
	}

	if err := tlsConn.SetReadDeadline(time.Time{}); err != nil {
		_ = tlsConn.Close()
		n.peerConfig.Log.Debug("failed to clear the read deadline",
			"error", err,
		)
		return err
	}

	// At this point we have successfully upgraded the connection and will
	// return a nil error.

	// Strict-PQ: run the application-layer ML-KEM + ML-DSA-65 handshake on
	// this connection BEFORE the dedup/lock below. The handshake authenticates
	// the peer's stable, key-bound ML-DSA NodeID (replacing the ephemeral
	// TLS-cert NodeID the upgrader derived), so the self / AllowConnection /
	// connecting + connected dedup all key on the validator-set NodeID.
	// Running it here — in this per-conn goroutine (the dial goroutine or the
	// Dispatch accept goroutine), holding no lock — lets both ends of a
	// simultaneous mutual dial complete independently: each TCP conn has a
	// distinct dialer=initiator / acceptor=responder. The prior in-Start,
	// under-peersLock run made every node a lone initiator (each kept its
	// outbound, dropped the peer's inbound as "already connecting") → INIT
	// sent, no responder → deadlock → 0 peers. Classical / no-PQ chains leave
	// PQHandshakeConfig nil and skip this entirely.
	var pq *peer.PQPreHandshake
	if n.peerConfig.PQHandshakeConfig != nil && n.peerConfig.PQLocalIdentity != nil {
		mldsaID, aeadKey, herr := peer.RunPQHandshakeConn(
			tlsConn,
			n.peerConfig.PQHandshakeConfig,
			n.peerConfig.PQLocalIdentity,
			isIngress,
			n.peerConfig.MaxClockDifference,
		)
		if herr != nil {
			_ = tlsConn.Close()
			n.peerConfig.Log.Debug("PQ handshake failed during upgrade",
				"direction", direction,
				"ephemeralNodeID", nodeID.String(),
				"error", herr,
			)
			return herr
		}
		nodeID = mldsaID
		pq = &peer.PQPreHandshake{AEADKey: aeadKey, PeerNodeID: mldsaID}
	}

	if nodeID == n.config.MyNodeID {
		_ = tlsConn.Close()
		n.peerConfig.Log.Debug("dropping connection to myself")
		return nil
	}

	if !n.AllowConnection(nodeID) {
		_ = tlsConn.Close()
		n.peerConfig.Log.Debug(
			"dropping undesired connection",
			"nodeID", nodeID.String(),
		)
		return nil
	}

	n.peersLock.Lock()
	if n.closing {
		n.peersLock.Unlock()

		_ = tlsConn.Close()
		n.peerConfig.Log.Debug(
			"dropping connection",
			"reason", "shutting down the p2p network",
			"nodeID", nodeID.String(),
		)
		return nil
	}

	if _, connecting := n.connectingPeers.GetByID(nodeID); connecting {
		n.peersLock.Unlock()

		_ = tlsConn.Close()
		n.peerConfig.Log.Debug(
			"dropping connection",
			"reason", "already connecting to peer",
			"nodeID", nodeID.String(),
		)
		return nil
	}

	if _, connected := n.connectedPeers.GetByID(nodeID); connected {
		n.peersLock.Unlock()

		_ = tlsConn.Close()
		n.peerConfig.Log.Debug(
			"dropping connection",
			"reason", "already connecting to peer",
			"nodeID", nodeID.String(),
		)
		return nil
	}

	n.peerConfig.Log.Warn("[UPGRADE] ADDING PEER TO connectingPeers",
		zap.Stringer("nodeID", nodeID),
		zap.String("direction", direction),
		zap.String("remoteAddr", tlsConn.RemoteAddr().String()),
	)

	// peer.Start requires there is only ever one peer instance running with the
	// same [peerConfig.InboundMsgThrottler]. This is guaranteed by the above
	// de-duplications for [connectingPeers] and [connectedPeers].
	peer := peer.Start(
		n.peerConfig,
		tlsConn,
		cert,
		nodeID,
		peer.NewThrottledMessageQueue(
			n.peerConfig.Metrics,
			nodeID,
			n.peerConfig.Log,
			n.outboundMsgThrottler,
		),
		isIngress,
		pq,
	)
	n.connectingPeers.Add(peer)
	n.peersLock.Unlock()
	return nil
}

func (n *network) PeerInfo(nodeIDs []ids.NodeID) []peer.Info {
	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	if len(nodeIDs) == 0 {
		return n.connectedPeers.AllInfo()
	}
	return n.connectedPeers.Info(nodeIDs)
}

func (n *network) StartClose() {
	n.closeOnce.Do(func() {
		n.peerConfig.Log.Info("shutting down the p2p networking")

		if err := n.listener.Close(); err != nil {
			n.peerConfig.Log.Debug("closing the network listener",
				"error", err,
			)
		}

		n.peersLock.Lock()
		defer n.peersLock.Unlock()

		n.closing = true
		n.onCloseCtxCancel()

		for nodeID, tracked := range n.trackedIPs {
			tracked.stopTracking()
			delete(n.trackedIPs, nodeID)
		}

		for i := 0; i < n.connectingPeers.Len(); i++ {
			peer, _ := n.connectingPeers.GetByIndex(i)
			peer.StartClose()
		}

		for i := 0; i < n.connectedPeers.Len(); i++ {
			peer, _ := n.connectedPeers.GetByIndex(i)
			peer.StartClose()
		}
	})
}

func (n *network) NodeUptime() (UptimeResult, error) {
	myStake := n.config.Validators.GetWeight(constants.PrimaryNetworkID, n.config.MyNodeID)
	if myStake == 0 {
		return UptimeResult{}, errNotValidator
	}

	totalWeightInt, err := n.config.Validators.TotalWeight(constants.PrimaryNetworkID)
	if err != nil {
		return UptimeResult{}, fmt.Errorf("error while fetching weight for primary network %w", err)
	}

	var (
		totalWeight          = float64(totalWeightInt)
		totalWeightedPercent = 100 * float64(myStake)
		rewardingStake       = float64(myStake)
	)

	n.peersLock.RLock()
	defer n.peersLock.RUnlock()

	for i := 0; i < n.connectedPeers.Len(); i++ {
		peer, _ := n.connectedPeers.GetByIndex(i)

		nodeID := peer.ID()
		weight := n.config.Validators.GetWeight(constants.PrimaryNetworkID, nodeID)
		if weight == 0 {
			// this is not a validator skip it.
			continue
		}

		observedUptime := peer.ObservedUptime()
		percent := float64(observedUptime)
		weightFloat := float64(weight)
		totalWeightedPercent += percent * weightFloat

		// if this peer thinks we're above requirement add the weight
		if percent/100 >= n.config.UptimeRequirement {
			rewardingStake += weightFloat
		}
	}

	return UptimeResult{
		WeightedAveragePercentage: math.Abs(totalWeightedPercent / totalWeight),
		RewardingStakePercentage:  math.Abs(100 * rewardingStake / totalWeight),
	}, nil
}

// TrackChain adds a chain to the set of tracked chains
func (n *network) TrackChain(chainID ids.ID) error {
	if chainID == constants.PrimaryNetworkID {
		return errTrackingPrimaryNetwork
	}

	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	// Update the config's tracked chains
	n.config.TrackedChains.Add(chainID)

	// Update the peer config so new peers get the updated chains
	n.peerConfig.MyChains.Add(chainID)

	// Update metrics to track the new chain
	n.metrics.trackedChains.Add(chainID)

	// Update IP tracker
	n.ipTracker.OnChainTracked(chainID)

	n.peerConfig.Log.Info("now tracking chain",
		"chainID", chainID,
	)
	return nil
}

// TrackedChains returns the set of chains this node is tracking
func (n *network) TrackedChains() set.Set[ids.ID] {
	n.peersLock.RLock()
	defer n.peersLock.RUnlock()
	// Return a copy to avoid external modifications
	result := set.NewSet[ids.ID](n.config.TrackedChains.Len())
	result.Union(n.config.TrackedChains)
	return result
}

// RegisterBlockchainNetwork registers a mapping from a blockchain ID to its
// chain network ID. This allows the gossip layer to correctly resolve which
// validator set to use when gossiping blocks for non-primary chains, and to check
// whether peers are tracking the chain network that owns the blockchain.
func (n *network) RegisterBlockchainNetwork(blockchainID, networkID ids.ID) {
	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	n.blockchainToNetwork[blockchainID] = networkID
	n.peerConfig.Log.Info("registered blockchain-to-network mapping",
		"blockchainID", blockchainID,
		"networkID", networkID,
	)
}

func (n *network) runTimers() {
	pullGossipPeerlists := time.NewTicker(n.config.PeerListPullGossipFreq)
	resetPeerListBloom := time.NewTicker(n.config.PeerListBloomResetFreq)
	updateUptimes := time.NewTicker(n.config.UptimeMetricFreq)
	defer func() {
		resetPeerListBloom.Stop()
		updateUptimes.Stop()
	}()

	for {
		select {
		case <-n.onCloseCtx.Done():
			return
		case <-pullGossipPeerlists.C:
			n.pullGossipPeerLists()
		case <-resetPeerListBloom.C:
			if err := n.ipTracker.ResetBloom(); err != nil {
				n.peerConfig.Log.Error("failed to reset ip tracker bloom filter",
					"error", err,
				)
			} else {
				n.peerConfig.Log.Debug("reset ip tracker bloom filter")
			}
		case <-updateUptimes.C:
			primaryUptime, err := n.NodeUptime()
			if err != nil {
				n.peerConfig.Log.Debug("failed to get primary network uptime",
					"error", err,
				)
			}
			n.metrics.nodeUptimeWeightedAverage.Set(primaryUptime.WeightedAveragePercentage)
			n.metrics.nodeUptimeRewardingStake.Set(primaryUptime.RewardingStakePercentage)
		}
	}
}

// pullGossipPeerLists requests validators from peers in the network
func (n *network) pullGossipPeerLists() {
	peers := n.samplePeers(
		warp.SendConfig{
			NonValidators: 1,
		},
		constants.PrimaryNetworkID,
		&noOpAllower{},
	)

	for _, p := range peers {
		p.StartSendGetPeerList()
	}
}

func (n *network) getLastReceived() (time.Time, bool) {
	lastReceived := atomic.LoadInt64(&n.peerConfig.LastReceived)
	if lastReceived == 0 {
		return time.Time{}, false
	}
	return time.Unix(lastReceived, 0), true
}

func (n *network) getLastSent() (time.Time, bool) {
	lastSent := atomic.LoadInt64(&n.peerConfig.LastSent)
	if lastSent == 0 {
		return time.Time{}, false
	}
	return time.Unix(lastSent, 0), true
}

// resourceTrackerWrapper wraps consensustracker.ResourceTracker to match tracker.ResourceTracker
type resourceTrackerWrapper struct {
	rt consensustracker.ResourceTracker
}

func (r *resourceTrackerWrapper) CPUTracker() tracker.Tracker {
	// Wrap the CPU tracker
	cpuTracker := r.rt.CPUTracker()
	return &trackerWrapper{t: cpuTracker}
}

func (r *resourceTrackerWrapper) DiskTracker() tracker.Tracker {
	// Wrap the disk tracker
	diskTracker := r.rt.DiskTracker()
	return &trackerWrapper{t: diskTracker}
}

// trackerWrapper wraps consensustracker.CPUTracker to match tracker.Tracker
type trackerWrapper struct {
	t consensustracker.CPUTracker
}

func (t *trackerWrapper) Usage(nodeID ids.NodeID, now time.Time) float64 {
	return t.t.Usage(nodeID, now)
}

func (t *trackerWrapper) TimeUntilUsage(nodeID ids.NodeID, now time.Time, value float64) time.Duration {
	return t.t.TimeUntilUsage(nodeID, now, value)
}

func (t *trackerWrapper) TotalUsage() float64 {
	// The consensus CPUTracker doesn't have TotalUsage, so return 0.0 as default
	return 0.0
}
