// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"crypto"
	"crypto/tls"
	"net/netip"
	"time"

	compression "github.com/luxfi/compress"
	consensusconfig "github.com/luxfi/consensus/config"
	consensustracker "github.com/luxfi/consensus/networking/tracker"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/network/dialer"
	"github.com/luxfi/node/network/throttling"
	"github.com/luxfi/node/network/tracker"
	"github.com/luxfi/utils"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/validators/uptime"
)

// HealthConfig describes parameters for network layer health checks.
type HealthConfig struct {
	// Marks if the health check should be enabled
	Enabled bool `json:"-"`

	// NoIngressValidatorConnectionGracePeriod denotes the time after which the health check fails
	// for primary network validators with no ingress connections.
	NoIngressValidatorConnectionGracePeriod time.Duration

	// MinConnectedPeers is the minimum number of peers that the network should
	// be connected to be considered healthy.
	MinConnectedPeers uint `json:"minConnectedPeers"`

	// MaxTimeSinceMsgReceived is the maximum amount of time since the network
	// last received a message to be considered healthy.
	MaxTimeSinceMsgReceived time.Duration `json:"maxTimeSinceMsgReceived"`

	// MaxTimeSinceMsgSent is the maximum amount of time since the network last
	// sent a message to be considered healthy.
	MaxTimeSinceMsgSent time.Duration `json:"maxTimeSinceMsgSent"`

	// MaxPortionSendQueueBytesFull is the maximum percentage of the pending
	// send byte queue that should be used for the network to be considered
	// healthy. Should be in (0,1].
	MaxPortionSendQueueBytesFull float64 `json:"maxPortionSendQueueBytesFull"`

	// MaxSendFailRate is the maximum percentage of send attempts that should be
	// failing for the network to be considered healthy. This does not include
	// send attempts that were not made due to benching. Should be in [0,1].
	MaxSendFailRate float64 `json:"maxSendFailRate"`

	// SendFailRateHalflife is the halflife of the averager used to calculate
	// the send fail rate percentage. Should be > 0. Larger values mean that the
	// fail rate is affected less by recently dropped messages.
	SendFailRateHalflife time.Duration `json:"sendFailRateHalflife"`
}

type PeerListGossipConfig struct {
	// PeerListNumValidatorIPs is the number of validator IPs to gossip in every
	// gossip event.
	PeerListNumValidatorIPs uint32 `json:"peerListNumValidatorIPs"`

	// PeerListPullGossipFreq is the frequency that this node will attempt to
	// request signed IPs from its peers.
	PeerListPullGossipFreq time.Duration `json:"peerListPullGossipFreq"`

	// PeerListBloomResetFreq is how frequently this node will recalculate the
	// IP tracker's bloom filter.
	PeerListBloomResetFreq time.Duration `json:"peerListBloomResetFreq"`
}

type TimeoutConfig struct {
	// PingPongTimeout is the maximum amount of time to wait for a Pong response
	// from a peer we sent a Ping to.
	PingPongTimeout time.Duration `json:"pingPongTimeout"`

	// ReadHandshakeTimeout is the maximum amount of time to wait for the peer's
	// connection upgrade to finish before starting the p2p handshake.
	ReadHandshakeTimeout time.Duration `json:"readHandshakeTimeout"`
}

type DelayConfig struct {
	// InitialReconnectDelay is the minimum amount of time the node will delay a
	// reconnection to a peer. This value is used to start the exponential
	// backoff.
	InitialReconnectDelay time.Duration `json:"initialReconnectDelay"`

	// MaxReconnectDelay is the maximum amount of time the node will delay a
	// reconnection to a peer.
	MaxReconnectDelay time.Duration `json:"maxReconnectDelay"`
}

type ThrottlerConfig struct {
	InboundConnUpgradeThrottlerConfig throttling.InboundConnUpgradeThrottlerConfig `json:"inboundConnUpgradeThrottlerConfig"`
	InboundMsgThrottlerConfig         throttling.InboundMsgThrottlerConfig         `json:"inboundMsgThrottlerConfig"`
	OutboundMsgThrottlerConfig        throttling.MsgByteThrottlerConfig            `json:"outboundMsgThrottlerConfig"`
	MaxInboundConnsPerSec             float64                                      `json:"maxInboundConnsPerSec"`
}

type Config struct {
	HealthConfig         `json:"healthConfig"`
	PeerListGossipConfig `json:"peerListGossipConfig"`
	TimeoutConfig        `json:"timeoutConfigs"`
	DelayConfig          `json:"delayConfig"`
	ThrottlerConfig      ThrottlerConfig `json:"throttlerConfig"`

	ProxyEnabled           bool          `json:"proxyEnabled"`
	ProxyReadHeaderTimeout time.Duration `json:"proxyReadHeaderTimeout"`

	DialerConfig dialer.Config `json:"dialerConfig"`
	TLSConfig    *tls.Config   `json:"-"`

	TLSKeyLogFile string `json:"tlsKeyLogFile"`

	MyNodeID ids.NodeID `json:"myNodeID"`

	// StakingMLDSA / StakingMLDSAPub are this node's persistent strict-PQ
	// ML-DSA-65 staking keypair (FIPS 204), mirrored from
	// StakingConfig.StakingMLDSA{,Pub} by node.Node so the network layer
	// can build the PQ peer-handshake identity WITHOUT generating an
	// ephemeral key. They are nil/empty on classical-compat chains.
	//
	// Why this matters: MyNodeID is derived from StakingMLDSAPub
	// (StakingConfig.DeriveNodeID), so the peer handshake MUST sign with
	// THIS keypair — not a throwaway one — for a completed handshake to
	// prove possession of the validator identity. See
	// peer.NewLocalIdentityFromStakingKey and peer.adoptVerifiedPQIdentity.
	StakingMLDSA    *mldsa.PrivateKey `json:"-"`
	StakingMLDSAPub []byte            `json:"-"`

	MyIPPort           *utils.Atomic[netip.AddrPort] `json:"myIP"`
	NetworkID          uint32                        `json:"networkID"`
	MaxClockDifference time.Duration                 `json:"maxClockDifference"`
	PingFrequency      time.Duration                 `json:"pingFrequency"`
	AllowPrivateIPs    bool                          `json:"allowPrivateIPs"`

	SupportedLPs set.Set[uint32] `json:"supportedLPs"`
	ObjectedLPs  set.Set[uint32] `json:"objectedLPs"`

	// The compression type to use when compressing outbound messages.
	// Assumes all peers support this compression type.
	CompressionType compression.Type `json:"compressionType"`

	// TLSKey is this node's TLS key that is used to sign IPs.
	TLSKey crypto.Signer `json:"-"`
	// BLSKey is this node's BLS key that is used to sign IPs.
	BLSKey bls.Signer `json:"-"`

	// TrackedChains of the node.
	// It must not include the primary network ID.
	TrackedChains set.Set[ids.ID]    `json:"-"`
	Beacons       validators.Manager `json:"-"`

	// Validators are the current validators in the Lux network
	Validators validators.Manager `json:"-"`

	// SequencerIDForChain returns the validator-set identity that sequences chainID.
	// This allows chains to be sequenced by a different validator set than their own.
	// Examples:
	//   - C-Chain → returns PrimaryNetworkID (sequenced by primary network validators)
	//   - Zoo chain (self-sequenced) → returns ZooChainID
	//   - Zoo chain (Lux-sequenced) → returns the Lux network's sequencerID
	// Default: returns chainID (self-sequenced).
	SequencerIDForChain func(chainID ids.ID) ids.ID `json:"-"`

	// GenesisBytes is the raw genesis configuration passed to the node.
	// This is used to extract the actual initial stakers for the network,
	// rather than relying on canonical genesis configs which may differ
	// from the actual genesis used (e.g., in netrunner-generated networks).
	GenesisBytes []byte `json:"-"`

	UptimeCalculator uptime.Calculator `json:"-"`

	// UptimeMetricFreq marks how frequently this node will recalculate the
	// observed average uptime metric.
	UptimeMetricFreq time.Duration `json:"uptimeMetricFreq"`

	// UptimeRequirement is the fraction of time a validator must be online and
	// responsive for us to vote that they should receive a staking reward.
	UptimeRequirement float64 `json:"-"`

	// RequireValidatorToConnect require that all connections must have at least
	// one validator between the 2 peers. This can be useful to enable if the
	// node wants to connect to the minimum number of nodes without impacting
	// the network negatively.
	RequireValidatorToConnect bool `json:"requireValidatorToConnect"`

	// MaximumInboundMessageTimeout is the maximum deadline duration in a
	// message. Messages sent by clients setting values higher than this value
	// will be reset to this value.
	MaximumInboundMessageTimeout time.Duration `json:"maximumInboundMessageTimeout"`

	// Size, in bytes, of the buffer that we read peer messages into
	// (there is one buffer per peer)
	PeerReadBufferSize int `json:"peerReadBufferSize"`

	// Size, in bytes, of the buffer that we write peer messages into
	// (there is one buffer per peer)
	PeerWriteBufferSize int `json:"peerWriteBufferSize"`

	// Tracks the CPU/disk usage caused by processing messages of each peer.
	ResourceTracker consensustracker.ResourceTracker `json:"-"`

	// Specifies how much CPU usage each peer can cause before
	// we rate-limit them.
	CPUTargeter tracker.Targeter `json:"-"`

	// Specifies how much disk usage each peer can cause before
	// we rate-limit them.
	DiskTargeter tracker.Targeter `json:"-"`

	// SecurityProfile is the chain-wide ChainSecurityProfile this node is
	// operating under (resolved at boot from the genesis pin in
	// node.Node.initSecurityProfile). When it mandates the application-layer
	// PQ handshake (strict-PQ / FIPS — see profileRequiresPQHandshake), the
	// network builds a peer.SchemeGate from it AND runs the ML-KEM + ML-DSA
	// handshake; the gate and handshake are gated by one identical
	// predicate so an ML-DSA identity always exists for the gate to bind.
	// A nil profile, or a non-PQ-handshake profile (permissive), leaves the
	// cross-axis gate disabled: peers present classical secp256k1 cert
	// schemes the gate would refuse unconditionally, so building it there
	// would refuse every connection with no PQ handshake to recover. Such
	// chains remain accepted by the upgrader's nil-safe path.
	SecurityProfile *consensusconfig.ChainSecurityProfile `json:"-"`
}
