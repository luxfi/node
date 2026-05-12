// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"crypto/tls"
	"errors"
	"net/netip"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/server/http"
	"github.com/luxfi/node/benchlist"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/genesis/builder"
	"github.com/luxfi/node/network"
	// "github.com/luxfi/consensus/core/router" // Unused
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/mldsa"
	mlkemcrypto "github.com/luxfi/crypto/mlkem"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/network/tracker"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/trace"
	// "github.com/luxfi/log" // Unused
	"github.com/luxfi/math/set"
	"github.com/luxfi/timer"
	"github.com/luxfi/node/utils/profiler"
)

type APIIndexerConfig struct {
	IndexAPIEnabled      bool `json:"indexAPIEnabled"`
	IndexAllowIncomplete bool `json:"indexAllowIncomplete"`
}

type HTTPConfig struct {
	server.HTTPConfig
	APIConfig `json:"apiConfig"`
	HTTPHost  string `json:"httpHost"`
	HTTPPort  uint16 `json:"httpPort"`

	HTTPSEnabled bool   `json:"httpsEnabled"`
	HTTPSKey     []byte `json:"-"`
	HTTPSCert    []byte `json:"-"`

	HTTPAllowedOrigins []string `json:"httpAllowedOrigins"`
	HTTPAllowedHosts   []string `json:"httpAllowedHosts"`

	ShutdownTimeout time.Duration `json:"shutdownTimeout"`
	ShutdownWait    time.Duration `json:"shutdownWait"`
}

type APIConfig struct {
	APIIndexerConfig `json:"indexerConfig"`

	// Enable/Disable APIs
	AdminAPIEnabled    bool `json:"adminAPIEnabled"`
	InfoAPIEnabled     bool `json:"infoAPIEnabled"`
	KeystoreAPIEnabled bool `json:"keystoreAPIEnabled"`
	MetricsAPIEnabled  bool `json:"metricsAPIEnabled"`
	HealthAPIEnabled   bool `json:"healthAPIEnabled"`
}

type IPConfig struct {
	PublicIP                  string        `json:"publicIP"`
	PublicIPResolutionService string        `json:"publicIPResolutionService"`
	PublicIPResolutionFreq    time.Duration `json:"publicIPResolutionFreq"`
	// The host portion of the address to listen on. The port to
	// listen on will be sourced from IPPort.
	//
	// - If empty, listen on all interfaces (both ipv4 and ipv6).
	// - If populated, listen only on the specified address.
	ListenHost string `json:"listenHost"`
	ListenPort uint16 `json:"listenPort"`
}

type StakingConfig struct {
	builder.StakingConfig
	SybilProtectionEnabled        bool            `json:"sybilProtectionEnabled"`
	PartialSyncPrimaryNetwork     bool            `json:"partialSyncPrimaryNetwork"`
	StakingTLSCert                tls.Certificate `json:"-"`
	StakingSigningKey             bls.Signer      `json:"-"`
	SybilProtectionDisabledWeight uint64          `json:"sybilProtectionDisabledWeight"`
	// not accessed but used for logging
	StakingKeyPath    string `json:"stakingKeyPath"`
	StakingCertPath   string `json:"stakingCertPath"`
	StakingSignerPath string `json:"stakingSignerPath"`

	// Strict-PQ identity material. Set by config.GetNodeConfig when the
	// --staking-mldsa-*-file / --handshake-mlkem-*-file flags resolve.
	// All four fields are nil/empty under a classical-compat chain;
	// strict-PQ profile rejects a missing StakingMLDSAPub at boot.
	//
	// NodeID derivation pivots on StakingMLDSAPub: when non-empty,
	// ids.NodeIDSchemeMLDSA65.DeriveMLDSA(stakingChainID, StakingMLDSAPub)
	// replaces ids.NodeIDFromCert(StakingTLSCert) — the TLS cert becomes
	// transport-only, no longer the identity anchor.
	//
	// HandshakeMLKEMPub is the peer-facing KEM public key that lqd
	// publishes in its validator-set entry so peers can encapsulate to it
	// for session-key establishment with no classical fallback. The
	// HandshakeMLKEMPriv stays local to the pod.
	StakingMLDSA       *mldsa.PrivateKey         `json:"-"`
	StakingMLDSAPub    []byte                    `json:"-"`
	HandshakeMLKEMPriv *mlkemcrypto.PrivateKey   `json:"-"`
	HandshakeMLKEMPub  []byte                    `json:"-"`
	// File paths kept for log-line context, mirroring StakingKeyPath etc.
	StakingMLDSAKeyPath  string `json:"stakingMLDSAKeyPath,omitempty"`
	StakingMLDSAPubPath  string `json:"stakingMLDSAPubPath,omitempty"`
	HandshakeMLKEMKeyPath string `json:"handshakeMLKEMKeyPath,omitempty"`
	HandshakeMLKEMPubPath string `json:"handshakeMLKEMPubPath,omitempty"`
}

type StateSyncConfig struct {
	StateSyncIDs []ids.NodeID     `json:"stateSyncIDs"`
	StateSyncIPs []netip.AddrPort `json:"stateSyncIPs"`
}

type BootstrapConfig struct {
	// Timeout before emitting a warn log when connecting to bootstrapping beacons
	BootstrapBeaconConnectionTimeout time.Duration `json:"bootstrapBeaconConnectionTimeout"`

	// Max number of containers in an ancestors message sent by this node.
	BootstrapAncestorsMaxContainersSent int `json:"bootstrapAncestorsMaxContainersSent"`

	// This node will only consider the first [AncestorsMaxContainersReceived]
	// containers in an ancestors message it receives.
	BootstrapAncestorsMaxContainersReceived int `json:"bootstrapAncestorsMaxContainersReceived"`

	// Max time to spend fetching a container and its
	// ancestors while responding to a GetAncestors message
	BootstrapMaxTimeGetAncestors time.Duration `json:"bootstrapMaxTimeGetAncestors"`

	Bootstrappers []builder.Bootstrapper `json:"bootstrappers"`

	// Skip bootstrapping and start processing immediately
	SkipBootstrap bool `json:"skipBootstrap"`

	// Enable automining in POA mode
	EnableAutomining bool `json:"enableAutomining"`
}

type DatabaseConfig struct {
	// If true, all writes are to memory and are discarded at node shutdown.
	ReadOnly bool `json:"readOnly"`

	// Path to database
	Path string `json:"path"`

	// Name of the database type to use
	Name string `json:"name"`

	// Path to config file
	Config []byte `json:"-"`
}

// Config contains all of the configurations of a Lux node.
type Config struct {
	HTTPConfig          `json:"httpConfig"`
	IPConfig            `json:"ipConfig"`
	StakingConfig       `json:"stakingConfig"`
	builder.TxFeeConfig `json:"txFeeConfig"`
	StateSyncConfig     `json:"stateSyncConfig"`
	BootstrapConfig     `json:"bootstrapConfig"`
	DatabaseConfig      `json:"databaseConfig"`

	UpgradeConfig upgrade.Config `json:"upgradeConfig"`

	// Genesis information
	GenesisBytes       []byte `json:"-"`
	XAssetID         ids.ID `json:"xAssetID"`
	AllowGenesisUpdate bool   `json:"allowGenesisUpdate,omitempty"`

	// ID of the network this node should connect to
	NetworkID uint32 `json:"networkID"`

	// Health
	HealthCheckFreq time.Duration `json:"healthCheckFreq"`

	// Network configuration
	NetworkConfig network.Config `json:"networkConfig"`

	AdaptiveTimeoutConfig timer.AdaptiveTimeoutConfig `json:"adaptiveTimeoutConfig"`

	BenchlistConfig benchlist.Config `json:"benchlistConfig"`

	ProfilerConfig profiler.Config `json:"profilerConfig"`

	// LoggingConfig logging.Config `json:"loggingConfig"` // logging package not available

	PluginDir string `json:"pluginDir"`

	// File Descriptor Limit
	FdLimit uint64 `json:"fdLimit"`

	// Metrics
	MeterVMEnabled bool `json:"meterVMEnabled"`

	// RouterHealthConfig       router.HealthConfig `json:"routerHealthConfig"` // router.HealthConfig not available
	ConsensusShutdownTimeout time.Duration `json:"consensusShutdownTimeout"`
	// Poll for new frontiers every [FrontierPollFrequency]
	FrontierPollFrequency time.Duration `json:"consensusGossipFreq"`
	// ConsensusAppConcurrency defines the maximum number of goroutines to
	// handle App messages per chain.
	ConsensusAppConcurrency int `json:"consensusAppConcurrency"`

	// must initialize successfully or the node shuts down.  Default: ["P","Q"].

	TrackedChains  set.Set[ids.ID] `json:"trackedChains"`
	TrackAllChains bool            `json:"trackAllChains"`

	NetConfigs map[ids.ID]nets.Config `json:"chainConfigs"`

	ChainConfigs map[string]chains.ChainConfig `json:"-"`
	ChainAliases map[ids.ID][]string           `json:"chainAliases"`

	VMAliases map[ids.ID][]string `json:"vmAliases"`

	// Halflife to use for the processing requests tracker.
	// Larger halflife --> usage metrics change more slowly.
	SystemTrackerProcessingHalflife time.Duration `json:"systemTrackerProcessingHalflife"`

	// Frequency to check the real resource usage of tracked processes.
	// More frequent checks --> usage metrics are more accurate, but more
	// expensive to track
	SystemTrackerFrequency time.Duration `json:"systemTrackerFrequency"`

	// Halflife to use for the cpu tracker.
	// Larger halflife --> cpu usage metrics change more slowly.
	SystemTrackerCPUHalflife time.Duration `json:"systemTrackerCPUHalflife"`

	// Halflife to use for the disk tracker.
	// Larger halflife --> disk usage metrics change more slowly.
	SystemTrackerDiskHalflife time.Duration `json:"systemTrackerDiskHalflife"`

	CPUTargeterConfig tracker.TargeterConfig `json:"cpuTargeterConfig"`

	DiskTargeterConfig tracker.TargeterConfig `json:"diskTargeterConfig"`

	RequiredAvailableDiskSpace         uint64 `json:"requiredAvailableDiskSpace"`
	WarningThresholdAvailableDiskSpace uint64 `json:"warningThresholdAvailableDiskSpace"`

	TraceConfig trace.Config `json:"traceConfig"`

	// See comment on [UseCurrentHeight] in platformvm.Config
	UseCurrentHeight bool `json:"useCurrentHeight"`

	// ProvidedFlags contains all the flags set by the user
	ProvidedFlags map[string]interface{} `json:"-"`

	// ChainDataDir is the root path for per-chain directories where VMs can
	// write arbitrary data.
	ChainDataDir string `json:"chainDataDir"`

	// Path to write process context to (including PID, API URI, and
	// staking address).
	ProcessContextFilePath string `json:"processContextFilePath"`

	// Low Memory Configuration
	LowMemoryEnabled    bool   `json:"lowMemoryEnabled"`
	DBCacheSize         uint64 `json:"dbCacheSize"`
	DBMemtableSize      uint64 `json:"dbMemtableSize"`
	StateCacheSize      uint64 `json:"stateCacheSize"`
	BlockCacheSize      uint64 `json:"blockCacheSize"`
	DisableBloomFilters bool   `json:"disableBloomFilters"`
	LazyChainLoading    bool   `json:"lazyChainLoading"`
	SingleValidatorMode bool   `json:"singleValidatorMode"`
}

// IsStrictPQ reports whether the staking config has strict-PQ
// identity material loaded. Strict-PQ requires an ML-DSA-65 public
// key (signing identity) — the ML-KEM-768 KEM is a complementary
// handshake primitive that the validator-set entry publishes for
// peers but is NOT what defines the validator's identity. The
// 0-byte slice case is "classical-compat": NodeID derives from the
// TLS staking cert in StakingTLSCert.
func (c *StakingConfig) IsStrictPQ() bool {
	return len(c.StakingMLDSAPub) > 0
}

// DeriveNodeID returns the canonical 20-byte NodeID for this
// staking identity under chainID:
//
//   - Strict-PQ (StakingMLDSAPub set):
//     NodeID = SHAKE256-384("LUX_NODE_ID_V1" || chainID || 0x42 || pubKey)[:20]
//     via ids.NodeIDSchemeMLDSA65.DeriveMLDSA. chainID-bound so the
//     same key on a different chain produces a different NodeID,
//     blocking cross-chain replay of validator registrations.
//   - Classical-compat (no ML-DSA pub):
//     NodeID = ids.NodeIDFromCert(StakingTLSCert.Leaf) — the
//     legacy upstream derivation. A strict-PQ chain MUST refuse
//     this branch at the validator-set boundary; the choice lives
//     in the consensus profile, not here.
//
// This is the single seam every "what's my NodeID" call site should
// route through. Direct calls to ids.NodeIDFromCert in new code are
// a bug — they bypass the strict-PQ pivot.
func (c *StakingConfig) DeriveNodeID(chainID ids.ID) (ids.NodeID, error) {
	if c.IsStrictPQ() {
		id, _, err := ids.NodeIDSchemeMLDSA65.DeriveMLDSA(chainID, c.StakingMLDSAPub)
		return id, err
	}
	if c.StakingTLSCert.Leaf == nil {
		return ids.EmptyNodeID, errStakingTLSCertLeafNil
	}
	return ids.NodeIDFromCert(&ids.Certificate{
		Raw:       c.StakingTLSCert.Leaf.Raw,
		PublicKey: c.StakingTLSCert.Leaf.PublicKey,
	}), nil
}

// DeriveTypedNodeID returns the wire-canonical TypedNodeID — scheme
// byte (0x42 for strict-PQ ML-DSA-65, 0x90 for classical secp256k1)
// followed by the 20-byte NodeID. The scheme byte travels with the
// NodeID on every handshake / validator-set serialization so a
// receiver MUST know which verifier to dispatch before consulting
// the chain profile. Profile is a downgrade-detection gate; scheme
// byte is a primitive-mismatch gate.
func (c *StakingConfig) DeriveTypedNodeID(chainID ids.ID) (ids.TypedNodeID, error) {
	if c.IsStrictPQ() {
		t, _, err := ids.TypedNodeIDFromMLDSA(
			ids.NodeIDSchemeMLDSA65, chainID, c.StakingMLDSAPub)
		return t, err
	}
	if c.StakingTLSCert.Leaf == nil {
		return ids.TypedNodeID{}, errStakingTLSCertLeafNil
	}
	return ids.TypedNodeIDFromCert(&ids.Certificate{
		Raw:       c.StakingTLSCert.Leaf.Raw,
		PublicKey: c.StakingTLSCert.Leaf.PublicKey,
	}), nil
}

var errStakingTLSCertLeafNil = errors.New(
	"node: StakingConfig.StakingTLSCert.Leaf is nil — neither ML-DSA-65 nor a parsed TLS cert is available")
