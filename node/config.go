// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"crypto/tls"
	"net/netip"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/genesis/pkg/genesis"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/server/http"
	"github.com/luxfi/node/benchlist"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/nets"
	"github.com/luxfi/node/network"
	"github.com/luxfi/node/trace"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/vms/platformvm/txs/fee"
	"github.com/luxfi/timer"
	"github.com/luxfi/node/utils/profiler"
)

type APIIndexerConfig struct {
	IndexAPIEnabled      bool `json:"indexAPIEnabled"`
	IndexAllowIncomplete bool `json:"indexAllowIncomplete"`
}

type TargeterConfig struct {
	// VdrAlloc is the percentage of resource usage that is attributed to validators
	// The range is [0, 1], defaults to 1 (100%)
	VdrAlloc float64 `json:"vdrAlloc"`
	// MaxNonVdrUsage is the maximum amount of resources that non-validators can use
	// The range is [0, 1], defaults to 0
	MaxNonVdrUsage float64 `json:"maxNonVdrUsage"`
	// MaxNonVdrNodeUsage is the maximum amount of resources that a non-validator node can use
	// The range is [0, 1], defaults to 0
	MaxNonVdrNodeUsage float64 `json:"maxNonVdrNodeUsage"`
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
	genesis.StakingConfig
	SybilProtectionEnabled        bool            `json:"sybilProtectionEnabled"`
	PartialSyncPrimaryNetwork     bool            `json:"partialSyncPrimaryNetwork"`
	StakingTLSCert                tls.Certificate `json:"-"`
	StakingSigningKey             *bls.SecretKey  `json:"-"`
	SybilProtectionDisabledWeight uint64          `json:"sybilProtectionDisabledWeight"`
	StakingKeyPath                string          `json:"stakingKeyPath"`
	StakingCertPath               string          `json:"stakingCertPath"`
	StakingSignerPath             string          `json:"stakingSignerPath"`
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

	Bootstrappers []genesis.Bootstrapper `json:"bootstrappers"`

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
	HTTPConfig       `json:"httpConfig"`
	IPConfig         `json:"ipConfig"`
	StakingConfig    `json:"stakingConfig"`
	fee.StaticConfig `json:"txFeeConfig"`
	StateSyncConfig  `json:"stateSyncConfig"`
	BootstrapConfig  `json:"bootstrapConfig"`
	DatabaseConfig   `json:"databaseConfig"`

	UpgradeConfig upgrade.Config `json:"upgradeConfig"`

	// Genesis information
	GenesisBytes []byte `json:"-"`
	LuxAssetID   ids.ID `json:"xAssetID"`

	// ID of the network this node should connect to
	NetworkID uint32 `json:"networkID"`

	// Health
	HealthCheckFreq time.Duration `json:"healthCheckFreq"`

	// Network configuration
	NetworkConfig network.Config `json:"networkConfig"`

	AdaptiveTimeoutConfig timer.AdaptiveTimeoutConfig `json:"adaptiveTimeoutConfig"`

	BenchlistConfig benchlist.Config `json:"benchlistConfig"`

	ProfilerConfig profiler.Config `json:"profilerConfig"`

	// LoggingConfig log.Config `json:"loggingConfig"` // log.Config doesn't exist

	PluginDir string `json:"pluginDir"`
	// DevMode enables local PoA + auto-mine mode (network-id=local, sybil-protection disabled)
	DevMode bool `json:"devMode"`

	// File Descriptor Limit
	FdLimit uint64 `json:"fdLimit"`

	// Metrics
	MeterVMEnabled bool `json:"meterVMEnabled"`
	// Delay between automatic block proposals when DevMode is set
	DevBlockDelay time.Duration `json:"devBlockDelay"`

	RouterHealthConfig       HealthConfig  `json:"routerHealthConfig"`
	ConsensusShutdownTimeout time.Duration `json:"consensusShutdownTimeout"`
	// Poll for new frontiers every [FrontierPollFrequency]
	FrontierPollFrequency time.Duration `json:"consensusGossipFreq"`
	// ConsensusAppConcurrency defines the maximum number of goroutines to
	// handle App messages per chain.
	ConsensusAppConcurrency int `json:"consensusAppConcurrency"`

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

	CPUTargeterConfig TargeterConfig `json:"cpuTargeterConfig"`

	DiskTargeterConfig TargeterConfig `json:"diskTargeterConfig"`

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

	// ImportChainData is the path to import blockchain data from another chain
	ImportChainData string `json:"importChainData"`

	// Path to write process context to (including PID, API URI, and
	// staking address).
	ProcessContextFilePath string `json:"processContextFilePath"`

	// POA Mode Configuration
	POAModeEnabled     bool          `json:"poaModeEnabled"`
	POASingleNodeMode  bool          `json:"poaSingleNodeMode"`
	POAMinBlockTime    time.Duration `json:"poaMinBlockTime"`
	POAAuthorizedNodes []string      `json:"poaAuthorizedNodes"`

	// Logging
	Log log.Logger `json:"-"`
}
