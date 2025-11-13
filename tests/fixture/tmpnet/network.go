// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/luxfi/log"
	logfields "github.com/luxfi/log"

	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/config"
	"github.com/luxfi/genesis/pkg/genesis"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/node/utils/perms"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/vms/platformvm"
)

// The Network type is defined in this file (orchestration) and
// network_config.go (reading/writing configuration).

const (
	// Constants defining the names of shell variables whose value can
	// configure network orchestration.
	NetworkDirEnvName     = "TMPNET_NETWORK_DIR"

	// Message to log indicating where to look for metrics and logs for network
	MetricsAvailableMessage = "metrics and logs available via grafana (collectors must be running)"

	// This interval was chosen to avoid spamming node APIs during
	// startup, as smaller intervals (e.g. 50ms) seemed to noticeably
	// increase the time for a network's nodes to be seen as healthy.
	networkHealthCheckInterval = 200 * time.Millisecond

	// All temporary networks will use this arbitrary network ID by default.
	defaultNetworkID = 88888

	// eth address: 0x8db97C7cEcE249c2b98bDC0226Cc4C2A57BF52FC
	HardHatKeyStr = "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"

	// grafanaURI is remote Grafana URI
	grafanaURI = "grafana-poc.lux-dev.network"
)

var (
	// Key expected to be funded for net-evm hardhat testing
	// TODO(marun) Remove when net-evm configures the genesis with this key.
	HardhatKey *secp256k1.PrivateKey

	errInsufficientNodes    = errors.New("at least one node is required")
	errMissingRuntimeConfig = errors.New("DefaultRuntimeConfig must not be empty")

	// Labels expected to be available in the environment when running in GitHub Actions
	githubLabels = []string{
		"gh_repo",
		"gh_workflow",
		"gh_run_id",
		"gh_run_number",
		"gh_run_attempt",
		"gh_job_id",
	}
)

func init() {
	hardhatKeyBytes, err := hex.DecodeString(HardHatKeyStr)
	if err != nil {
		panic(err)
	}
	HardhatKey, err = secp256k1.ToPrivateKey(hardhatKeyBytes)
	if err != nil {
		panic(err)
	}
}

// ConfigMap enables defining configuration in a format appropriate
// for round-tripping through JSON back to golang structs.
type ConfigMap map[string]any

// Collects the configuration for running a temporary node network
type Network struct {
	// Uniquely identifies the temporary network for metrics
	// collection. Distinct from node's concept of network ID
	// since the utility of special network ID values (e.g. to trigger
	// specific fork behavior in a given network) precludes requiring
	// unique network ID values across all temporary networks.
	UUID string

	// A string identifying the entity that started or maintains this
	// network. Useful for differentiating between networks when a
	// given CI job uses multiple networks.
	Owner string

	// Path where network configuration and data is stored
	Dir string

	// Id of the network. If zero, must be set in Genesis. Consider
	// using the GetNetworkID method if needing to retrieve the ID of
	// a running network.
	NetworkID uint32

	// Configuration common across nodes

	// Genesis for the network. If nil, NetworkID must be non-zero
	Genesis *genesis.UnparsedConfig

	// Configuration for primary subnets
	PrimaryNetConfig ConfigMap

	// Configuration for primary network chains (P, X, C)
	PrimaryChainConfigs map[string]ConfigMap

	// Default configuration to use when creating new nodes
	DefaultFlags         FlagsMap
	DefaultRuntimeConfig NodeRuntimeConfig

	// Keys pre-funded in the genesis on both the X-Chain and the C-Chain
	PreFundedKeys []*secp256k1.PrivateKey

	// Nodes that constitute the network
	Nodes []*Node

	// Nets that have been enabled on the network
	Nets []*Net

	log log.Logger
}

func NewDefaultNetwork(owner string) *Network {
	return &Network{
		UUID:  uuid.NewString(),
		Owner: owner,
		Nodes: NewNodesOrPanic(DefaultNodeCount),
	}
}

// Ensure a real and absolute network dir so that node
// configuration that embeds the network path will continue to
// work regardless of symlink and working directory changes.
func toCanonicalDir(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absDir)
}

// CreateNodes creates n new nodes with default configuration
func (n *Network) CreateNodes(count int) error {
	for i := 0; i < count; i++ {
		node := NewNode()
		n.Nodes = append(n.Nodes, node)
	}
	return nil
}

// AddNode adds a new node to the network
func (n *Network) AddNode(ctx context.Context, node *Node) error {
	if err := n.EnsureNodeConfig(node); err != nil {
		return fmt.Errorf("failed to configure node: %w", err)
	}
	
	// Set bootstrap configuration for the new node
	bootstrapIPs, bootstrapIDs := n.GetBootstrapIPsAndIDs(node)
	if len(bootstrapIPs) == 0 {
		return errors.New("no bootstrap nodes available")
	}
	
	node.Flags[config.BootstrapIPsKey] = strings.Join(bootstrapIPs, ",")
	node.Flags[config.BootstrapIDsKey] = strings.Join(bootstrapIDs, ",")
	
	n.Nodes = append(n.Nodes, node)
	
	// Write configuration
	if err := node.Write(); err != nil {
		return fmt.Errorf("failed to write node configuration: %w", err)
	}
	
	// Update network configuration
	if err := n.Write(); err != nil {
		return fmt.Errorf("failed to update network configuration: %w", err)
	}
	
	return nil
}

// RemoveNode removes a node from the network
func (n *Network) RemoveNode(ctx context.Context, nodeID ids.NodeID) error {
	var nodeToRemove *Node
	var nodeIndex int
	
	for i, node := range n.Nodes {
		if node.NodeID == nodeID {
			nodeToRemove = node
			nodeIndex = i
			break
		}
	}
	
	if nodeToRemove == nil {
		return fmt.Errorf("node %s not found in network", nodeID)
	}
	
	// Stop the node if it's running
	if nodeToRemove.IsRunning() {
		if err := nodeToRemove.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop node %s: %w", nodeID, err)
		}
	}
	
	// Remove node from list
	n.Nodes = append(n.Nodes[:nodeIndex], n.Nodes[nodeIndex+1:]...)
	
	// Update network configuration
	if err := n.Write(); err != nil {
		return fmt.Errorf("failed to update network configuration: %w", err)
	}
	
	return nil
}

// RestartNode restarts a specific node in the network
func (n *Network) RestartNode(ctx context.Context, nodeID ids.NodeID) error {
	node, err := n.GetNode(nodeID)
	if err != nil {
		return err
	}
	
	if err := node.Restart(ctx); err != nil {
		return fmt.Errorf("failed to restart node %s: %w", nodeID, err)
	}
	
	return WaitForHealthyNodes(ctx, n.log, []*Node{node})
}

// GetHealthStatus returns the health status of all nodes
func (n *Network) GetHealthStatus(ctx context.Context) (map[ids.NodeID]bool, error) {
	healthStatus := make(map[ids.NodeID]bool)
	
	for _, node := range n.Nodes {
		if !node.IsRunning() {
			healthStatus[node.NodeID] = false
			continue
		}
		
		healthy, err := node.IsHealthy(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to check health of node %s: %w", node.NodeID, err)
		}
		healthStatus[node.NodeID] = healthy
	}
	
	return healthStatus, nil
}

// CollectLogs collects logs from all nodes to the specified directory
func (n *Network) CollectLogs(destDir string) error {
	if err := os.MkdirAll(destDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}
	
	var errs []error
	for _, node := range n.Nodes {
		nodeLogDir := filepath.Join(destDir, node.NodeID.String())
		if err := os.MkdirAll(nodeLogDir, perms.ReadWriteExecute); err != nil {
			errs = append(errs, fmt.Errorf("failed to create log dir for node %s: %w", node.NodeID, err))
			continue
		}
		
		// Copy logs from node data directory
		srcLogDir := filepath.Join(node.DataDir, "logs")
		if _, err := os.Stat(srcLogDir); err == nil {
			if err := copyDir(srcLogDir, nodeLogDir); err != nil {
				errs = append(errs, fmt.Errorf("failed to copy logs for node %s: %w", node.NodeID, err))
			}
		}
	}
	
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// copyDir copies a directory recursively
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		
		dstPath := filepath.Join(dst, relPath)
		
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()
		
		dstFile, err := os.Create(dstPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()
		
		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func BootstrapNewNetwork(
	ctx context.Context,
	log log.Logger,
	network *Network,
	rootNetworkDir string,
) error {
	if len(network.Nodes) == 0 {
		return errInsufficientNodes
	}

	if err := checkVMBinaries(log, network.Nets, network.DefaultRuntimeConfig.Process); err != nil {
		return err
	}
	if err := network.EnsureDefaultConfig(log); err != nil {
		return err
	}
	if err := network.Create(rootNetworkDir); err != nil {
		return err
	}
	return network.Bootstrap(ctx, log)
}

// Stops the nodes of the network configured in the provided directory.
func StopNetwork(ctx context.Context, log log.Logger, dir string) error {
	network, err := ReadNetwork(ctx, log, dir)
	if err != nil {
		return err
	}
	return network.Stop(ctx)
}

// Restarts the nodes of the network configured in the provided directory.
func RestartNetwork(ctx context.Context, log log.Logger, dir string) error {
	network, err := ReadNetwork(ctx, log, dir)
	if err != nil {
		return err
	}
	return network.Restart(ctx)
}

// Restart the provided nodes. Blocks on the nodes accepting API requests but not their health.
func restartNodes(ctx context.Context, nodes []*Node) error {
	for _, node := range nodes {
		if err := node.Restart(ctx); err != nil {
			return fmt.Errorf("failed to restart node %s: %w", node.NodeID, err)
		}
	}
	return nil
}

// Reads a network from the provided directory.
func ReadNetwork(ctx context.Context, log log.Logger, dir string) (*Network, error) {
	canonicalDir, err := toCanonicalDir(dir)
	if err != nil {
		return nil, err
	}
	network := &Network{
		Dir: canonicalDir,
		log: log,
	}
	if err := network.Read(ctx); err != nil {
		return nil, fmt.Errorf("failed to read network: %w", err)
	}
	if network.DefaultFlags == nil {
		network.DefaultFlags = FlagsMap{}
	}
	return network, nil
}

// Initializes a new network with default configuration.
func (n *Network) EnsureDefaultConfig(log log.Logger) error {
	log.Info("preparing configuration for new network",
		logfields.Any("runtimeConfig", n.DefaultRuntimeConfig),
	)

	n.log = log

	// A UUID supports centralized metrics collection
	if len(n.UUID) == 0 {
		n.UUID = uuid.NewString()
	}

	if n.DefaultFlags == nil {
		n.DefaultFlags = FlagsMap{}
	}

	// Ensure pre-funded keys if the genesis is not predefined
	if n.Genesis == nil && len(n.PreFundedKeys) == 0 {
		keys, err := NewPrivateKeys(DefaultPreFundedKeyCount)
		if err != nil {
			return err
		}
		n.PreFundedKeys = keys
	}

	// Ensure primary chains are configured
	if n.PrimaryChainConfigs == nil {
		n.PrimaryChainConfigs = map[string]ConfigMap{}
	}
	defaultChainConfigs := DefaultChainConfigs()
	for alias, defaultChainConfig := range defaultChainConfigs {
		if _, ok := n.PrimaryChainConfigs[alias]; !ok {
			n.PrimaryChainConfigs[alias] = ConfigMap{}
		}
		primaryChainConfig := n.PrimaryChainConfigs[alias]
		for key, value := range defaultChainConfig {
			if _, ok := primaryChainConfig[key]; !ok {
				primaryChainConfig[key] = value
			}
		}
	}

	emptyRuntime := NodeRuntimeConfig{}
	if n.DefaultRuntimeConfig == emptyRuntime {
		return errMissingRuntimeConfig
	}

	return nil
}

// Creates the network on disk, generating its genesis and configuring its nodes in the process.
func (n *Network) Create(rootDir string) error {
	// Ensure creation of the root dir
	if len(rootDir) == 0 {
		// Use the default root dir
		var err error
		rootDir, err = getDefaultRootNetworkDir()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(rootDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create root network dir: %w", err)
	}

	// A time-based name ensures consistent directory ordering
	dirName := time.Now().Format("20060102-150405.999999")
	if len(n.Owner) > 0 {
		// Include the owner to differentiate networks created at similar times
		dirName = fmt.Sprintf("%s-%s", dirName, n.Owner)
	}

	// Ensure creation of the network dir
	networkDir := filepath.Join(rootDir, dirName)
	if err := os.MkdirAll(networkDir, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to create network dir: %w", err)
	}
	canonicalDir, err := toCanonicalDir(networkDir)
	if err != nil {
		return err
	}
	n.Dir = canonicalDir

	if n.NetworkID == 0 && n.Genesis == nil {
		genesis, err := n.DefaultGenesis()
		if err != nil {
			return err
		}
		n.Genesis = genesis
	}

	for _, node := range n.Nodes {
		// Ensure the node is configured for use with the network and
		// knows where to write its configuration.
		if err := n.EnsureNodeConfig(node); err != nil {
			return err
		}
	}

	// Ensure configuration on disk is current
	return n.Write()
}

func (n *Network) DefaultGenesis() (*genesis.UnparsedConfig, error) {
	// Pre-fund known legacy keys to support ad-hoc testing. Usage of a legacy key will
	// require knowing the key beforehand rather than retrieving it from the set of pre-funded
	// keys exposed by a network. Since allocation will not be exclusive, a test using a
	// legacy key is unlikely to be a good candidate for parallel execution.
	keysToFund := []*secp256k1.PrivateKey{
		genesis.VMRQKey,
		genesis.EWOQKey,
		HardhatKey,
	}
	keysToFund = append(keysToFund, n.PreFundedKeys...)

	return NewTestGenesisWithFunds(defaultNetworkID, n.Nodes, keysToFund)
}

// Starts the specified nodes
func (n *Network) StartNodes(ctx context.Context, log log.Logger, nodesToStart ...*Node) error {
	if len(nodesToStart) == 0 {
		return errInsufficientNodes
	}
	nodesToWaitFor := nodesToStart
	if !slices.Contains(nodesToStart, n.Nodes[0]) {
		// If starting all nodes except the bootstrap node (because the bootstrap node is already
		// running), ensure that the health of the bootstrap node will be logged by including it in
		// the set of nodes to wait for.
		nodesToWaitFor = n.Nodes
	} else {
		// Simplify output by only logging network start when starting all nodes or when starting
		// the first node by itself to bootstrap subnet creation.
		log.Info("starting network",
			logfields.UserString("networkDir", n.Dir),
			logfields.UserString("uuid", n.UUID),
		)
	}

	// Record the time before nodes are started to ensure visibility of subsequently collected metrics via the emitted link
	startTime := time.Now()

	for _, node := range nodesToStart {
		if err := n.StartNode(ctx, node); err != nil {
			return err
		}
	}

	log.Info("waiting for nodes to report healthy")
	if err := waitForHealthy(ctx, log, nodesToWaitFor); err != nil {
		return err
	}
	log.Info("started network",
		logfields.UserString("networkDir", n.Dir),
		logfields.UserString("uuid", n.UUID),
	)
	// Provide a link to the main dashboard filtered by the uuid and showing results from now till whenever the link is viewed
	startTimeStr := strconv.FormatInt(startTime.UnixMilli(), 10)
	metricsURL := MetricsLinkForNetwork(n.UUID, startTimeStr, "")

	// Write link to the network path
	metricsPath := filepath.Join(n.Dir, "metrics.txt")
	if err := os.WriteFile(metricsPath, []byte(metricsURL+"\n"), perms.ReadWrite); err != nil {
		return fmt.Errorf("failed to write metrics link to %s: %w", metricsPath, err)
	}

	log.Info(MetricsAvailableMessage,
		logfields.UserString("url", metricsURL),
		logfields.UserString("linkPath", metricsPath),
	)

	return nil
}

// Start the network for the first time
func (n *Network) Bootstrap(ctx context.Context, log log.Logger) error {
	if len(n.Nets) == 0 {
		// Without the need to coordinate net configuration,
		// starting all nodes at once is the simplest option.
		return n.StartNodes(ctx, log, n.Nodes...)
	}

	// The node that will be used to create subnets and bootstrap the network
	bootstrapNode := n.Nodes[0]

	// An existing sybil protection value that may need to be restored after subnet creation
	var existingSybilProtectionValue *string

	if len(n.Nodes) > 1 {
		// Reduce the cost of net creation for a network of multiple nodes by
		// creating subnets with a single node with sybil protection
		// disabled. This allows the creation of initial net state without
		// requiring coordination between multiple nodes.

		log.Info("starting a single-node network with sybil protection disabled for quicker subnet creation")

		// If sybil protection is enabled, it should be re-enabled before the node is used to bootstrap the other nodes
		if value, ok := bootstrapNode.Flags[config.SybilProtectionEnabledKey]; ok {
			strValue := value.(string)
			existingSybilProtectionValue = &strValue
		}
		// Ensure sybil protection is disabled for the bootstrap node.
		bootstrapNode.Flags[config.SybilProtectionEnabledKey] = "false"
	}

	if err := n.StartNodes(ctx, log, bootstrapNode); err != nil {
		return err
	}

	// Don't restart the node during subnet creation since it will always be restarted afterwards.
	uri, cancel, err := bootstrapNode.GetLocalURI(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	if err := n.CreateNets(ctx, log, uri, false /* restartRequired */); err != nil {
		return err
	}

	if existingSybilProtectionValue == nil {
		log.Info("re-enabling sybil protection",
			logfields.Stringer("nodeID", bootstrapNode.NodeID),
		)
		delete(bootstrapNode.Flags, config.SybilProtectionEnabledKey)
	} else {
		log.Info("restoring previous sybil protection value",
			logfields.Stringer("nodeID", bootstrapNode.NodeID),
			logfields.UserString("sybilProtectionEnabled", *existingSybilProtectionValue),
		)
		bootstrapNode.Flags[config.SybilProtectionEnabledKey] = *existingSybilProtectionValue
	}

	// Ensure the bootstrap node is restarted to pick up subnet and chain configuration
	//
	// TODO(marun) This restart might be unnecessary if:
	// - sybil protection didn't change
	// - the node is not a subnet validator
	log.Info("restarting bootstrap node",
		logfields.Stringer("nodeID", bootstrapNode.NodeID),
	)
	if err := bootstrapNode.Restart(ctx); err != nil {
		return err
	}

	if len(n.Nodes) == 1 {
		return nil
	}

	log.Info("starting remaining nodes")
	return n.StartNodes(ctx, log, n.Nodes[1:]...)
}

// Starts the provided node after configuring it for the network.
func (n *Network) StartNode(ctx context.Context, node *Node) error {
	if err := n.EnsureNodeConfig(node); err != nil {
		return err
	}
	if err := node.Write(); err != nil {
		return err
	}

	if err := node.Start(ctx); err != nil {
		// Attempt to stop an unhealthy node to provide some assurance to the caller
		// that an error condition will not result in a lingering process.
		err = errors.Join(err, node.Stop(ctx))
		return err
	}

	return nil
}

// Stops all nodes in the network.
func (n *Network) Stop(ctx context.Context) error {
	// Ensure the node state is up-to-date
	if err := n.readNodes(ctx); err != nil {
		return err
	}

	var errs []error

	// Initiate stop on all nodes
	for _, node := range n.Nodes {
		if err := node.InitiateStop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop node %s: %w", node.NodeID, err))
		}
	}

	// Wait for stop to complete on all nodes
	for _, node := range n.Nodes {
		if err := node.WaitForStopped(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to wait for node %s to stop: %w", node.NodeID, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to stop network:\n%w", errors.Join(errs...))
	}
	return nil
}

// Restarts all running nodes in the network.
func (n *Network) Restart(ctx context.Context) error {
	n.log.Info("restarting network")
	nodes := make([]*Node, 0, len(n.Nodes))
	for _, node := range n.Nodes {
		if !node.IsRunning() {
			continue
		}
		nodes = append(nodes, node)
	}
	if err := restartNodes(ctx, nodes); err != nil {
		return err
	}
	return WaitForHealthyNodes(ctx, n.log, nodes)
}

// Waits for the provided nodes to become healthy.
func WaitForHealthyNodes(ctx context.Context, log log.Logger, nodes []*Node) error {
	for _, node := range nodes {
		log.Info("waiting for node to become healthy",
			logfields.Stringer("nodeID", node.NodeID),
		)
		if err := node.WaitForHealthy(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Ensures the provided node has the configuration it needs to start. If the data dir is not
// set, it will be defaulted to [nodeParentDir]/[node ID].
func (n *Network) EnsureNodeConfig(node *Node) error {
	// Ensure the node has access to network configuration
	node.network = n

	if err := node.EnsureKeys(); err != nil {
		return err
	}

	// Ensure a data directory if not already set
	if len(node.DataDir) == 0 {
		node.DataDir = filepath.Join(n.Dir, node.NodeID.String())
	}

	return nil
}

// TrackedNetsForNode returns the net IDs for the given node
func (n *Network) TrackedNetsForNode(nodeID ids.NodeID) string {
	netIDs := make([]string, 0, len(n.Nets))
	for _, net := range n.Nets {
		if net.NetID == ids.Empty {
			// Net has not yet been created
			continue
		}
		// Only track subnets that this node validates
		if slices.Contains(net.ValidatorIDs, nodeID) {
			netIDs = append(netIDs, net.NetID.String())
		}
	}
	return strings.Join(netIDs, ",")
}

func (n *Network) GetNet(name string) *Net {
	for _, net := range n.Nets {
		if net.Name == name {
			return net
		}
	}
	return nil
}

// Ensure that each net on the network is created. If restartRequired is false, node restart
// to pick up configuration changes becomes the responsibility of the caller.
func (n *Network) CreateNets(ctx context.Context, log log.Logger, apiURI string, restartRequired bool) error {
	createdNets := make([]*Net, 0, len(n.Nets))
	for _, net := range n.Nets {
		if len(net.ValidatorIDs) == 0 {
			return fmt.Errorf("net %s needs at least one validator", net.NetID)
		}
		if net.NetID != ids.Empty {
			// The net already exists
			continue
		}

		log.Info("creating subnet",
			logfields.UserString("name", net.Name),
		)

		if net.OwningKey == nil {
			// Allocate a pre-funded key and remove it from the network so it won't be used for
			// other purposes
			if len(n.PreFundedKeys) == 0 {
				return fmt.Errorf("no pre-funded keys available to create net %q", net.Name)
			}
			net.OwningKey = n.PreFundedKeys[len(n.PreFundedKeys)-1]
			n.PreFundedKeys = n.PreFundedKeys[:len(n.PreFundedKeys)-1]
		}

		// Create the subnet on the network
		if err := net.Create(ctx, apiURI); err != nil {
			return err
		}

		log.Info("created subnet",
			logfields.UserString("name", net.Name),
			logfields.Stringer("id", net.NetID),
		)

		// Persist the subnet configuration
		if err := net.Write(n.GetNetDir()); err != nil {
			return err
		}

		log.Info("wrote subnet configuration",
			logfields.UserString("name", net.Name),
		)

		createdNets = append(createdNets, net)
	}

	if len(createdNets) == 0 {
		return nil
	}

	// Ensure the pre-funded key changes are persisted to disk
	if err := n.Write(); err != nil {
		return err
	}

	reconfiguredNodes := []*Node{}
	for _, node := range n.Nodes {
		existingTrackedNets := node.Flags[config.TrackNetsKey]
		trackedNets := n.TrackedNetsForNode(node.NodeID)
		if existingTrackedNets == trackedNets {
			continue
		}
		node.Flags[config.TrackNetsKey] = trackedNets
		reconfiguredNodes = append(reconfiguredNodes, node)
	}

	if restartRequired {
		log.Info("restarting node(s) to enable them to track the new subnet(s)")

		runningNodes := make([]*Node, 0, len(reconfiguredNodes))
		for _, node := range reconfiguredNodes {
			if node.IsRunning() {
				runningNodes = append(runningNodes, node)
			}
		}

		if err := restartNodes(ctx, runningNodes); err != nil {
			return err
		}

		if err := WaitForHealthyNodes(ctx, n.log, runningNodes); err != nil {
			return err
		}
	}

	// Add validators for the subnet
	for _, subnet := range createdNets {
		log.Info("adding validators for subnet",
			logfields.UserString("name", subnet.Name),
		)

		// Collect the nodes intended to validate the net
		validatorIDs := set.NewSet[ids.NodeID](len(subnet.ValidatorIDs))
		validatorIDs.Add(subnet.ValidatorIDs...)
		validatorNodes := []*Node{}
		for _, node := range n.Nodes {
			if !validatorIDs.Contains(node.NodeID) {
				continue
			}
			validatorNodes = append(validatorNodes, node)
		}

		if err := subnet.AddValidators(ctx, log, apiURI, validatorNodes...); err != nil {
			return err
		}
	}

	// Wait for nodes to become subnet validators
	pChainClient := platformvm.NewClient(apiURI)
	validatorsToRestart := make(set.Set[ids.NodeID])
	for _, subnet := range createdNets {
		if err := WaitForActiveValidators(ctx, log, pChainClient, subnet); err != nil {
			return err
		}

		// It should now be safe to create chains for the subnet
		if err := subnet.CreateChains(ctx, log, apiURI); err != nil {
			return err
		}

		if err := subnet.Write(n.GetNetDir()); err != nil {
			return err
		}
		log.Info("wrote subnet configuration",
			logfields.UserString("name", subnet.Name),
			logfields.Stringer("id", subnet.NetID),
		)

		// If one or more of the subnets chains have explicit configuration, the
		// net's validator nodes will need to be restarted for those nodes to read
		// the newly written chain configuration and apply it to the chain(s).
		if subnet.HasChainConfig() {
			validatorsToRestart.Add(subnet.ValidatorIDs...)
		}
	}

	if !restartRequired || len(validatorsToRestart) == 0 {
		return nil
	}

	log.Info("restarting node(s) to pick up chain configuration")

	// Restart nodes to allow configuration for the new chains to take effect
	nodesToRestart := make([]*Node, 0, len(n.Nodes))
	for _, node := range n.Nodes {
		if validatorsToRestart.Contains(node.NodeID) {
			nodesToRestart = append(nodesToRestart, node)
		}
	}

	if err := restartNodes(ctx, nodesToRestart); err != nil {
		return err
	}

	if err := WaitForHealthyNodes(ctx, log, nodesToRestart); err != nil {
		return err
	}

	return nil
}

func (n *Network) GetNode(nodeID ids.NodeID) (*Node, error) {
	for _, node := range n.Nodes {
		if node.NodeID == nodeID {
			return node, nil
		}
	}
	return nil, fmt.Errorf("%s is not known to the network", nodeID)
}

// GetNodeURIs returns the URIs of nodes in the network that are running and not ephemeral. The URIs
// returned are guaranteed be reachable by the caller until the cleanup function is called regardless
// of whether the nodes are running as local processes or in a kube cluster.
func (n *Network) GetNodeURIs(ctx context.Context, deferCleanupFunc func(func())) ([]NodeURI, error) {
	return GetNodeURIs(ctx, n.Nodes, deferCleanupFunc)
}

// GetAvailableNodeIDs returns the node IDs of nodes in the network that are running and not ephemeral.
func (n *Network) GetAvailableNodeIDs() []string {
	availableNodes := FilterAvailableNodes(n.Nodes)
	ids := []string{}
	for _, node := range availableNodes {
		ids = append(ids, node.NodeID.String())
	}
	return ids
}

// Retrieves bootstrap IPs and IDs for all non-ephemeral nodes except the skipped one
// (this supports collecting the bootstrap details for restarting a node).
//
// For consumption outside of node. Needs to be kept exported.
func (n *Network) GetBootstrapIPsAndIDs(skippedNode *Node) ([]string, []string) {
	bootstrapIPs := []string{}
	bootstrapIDs := []string{}
	for _, node := range n.Nodes {
		if node.IsEphemeral {
			// Ephemeral nodes are not guaranteed to stay running
			continue
		}
		if skippedNode != nil && node.NodeID == skippedNode.NodeID {
			continue
		}

		if node.StakingAddress == (netip.AddrPort{}) {
			// Node is not running
			continue
		}

		bootstrapIPs = append(bootstrapIPs, node.StakingAddress.String())
		bootstrapIDs = append(bootstrapIDs, node.NodeID.String())
	}

	return bootstrapIPs, bootstrapIDs
}

// GetNetworkID returns the effective ID of the network. If the network
// defines a genesis, the network ID in the genesis will be returned. If a
// genesis is not present (i.e. a network with a genesis included in the
// node binary - mainnet, testnet and local), the value of the
// NetworkID field will be returned
func (n *Network) GetNetworkID() uint32 {
	if n.Genesis != nil && n.Genesis.NetworkID > 0 {
		return n.Genesis.NetworkID
	}
	return n.NetworkID
}

// GetGenesisFileContent returns the base64-encoded JSON-marshaled
// network genesis.
func (n *Network) GetGenesisFileContent() (string, error) {
	bytes, err := json.Marshal(n.Genesis)
	if err != nil {
		return "", fmt.Errorf("failed to marshal genesis: %w", err)
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// GetNetConfigContent returns the base64-encoded and
// JSON-marshaled map of subnetID to subnet configuration.
func (n *Network) GetNetConfigContent() (string, error) {
	subnetConfigs := map[ids.ID]ConfigMap{}

	if len(n.PrimaryNetConfig) > 0 {
		subnetConfigs[constants.PrimaryNetworkID] = n.PrimaryNetConfig
	}

	// Collect configuration for non-primary subnets
	for _, subnet := range n.Nets {
		if subnet.NetID == ids.Empty {
			// The subnet hasn't been created yet and it's not
			// possible to supply configuration without an ID.
			continue
		}
		if len(subnet.Config) == 0 {
			continue
		}
		subnetConfigs[subnet.NetID] = subnet.Config
	}

	if len(subnetConfigs) == 0 {
		return "", nil
	}

	marshaledConfigs, err := json.Marshal(subnetConfigs)
	if err != nil {
		return "", fmt.Errorf("failed to marshal subnet configs: %w", err)
	}
	return base64.StdEncoding.EncodeToString(marshaledConfigs), nil
}

// GetChainConfigContent returns the base64-encoded and JSON-marshaled map of chain alias/ID
// to JSON-marshaled chain configuration for both primary and custom chains.
func (n *Network) GetChainConfigContent() (string, error) {
	chainConfigs := map[string]chains.ChainConfig{}
	for alias, flags := range n.PrimaryChainConfigs {
		marshaledFlags, err := json.Marshal(flags)
		if err != nil {
			return "", fmt.Errorf("failed to marshal flags map for %s-Chain: %w", alias, err)
		}
		chainConfigs[alias] = chains.ChainConfig{
			Config: marshaledFlags,
		}
	}

	// Collect custom chain configuration
	for _, subnet := range n.Nets {
		for _, chain := range subnet.Chains {
			if chain.ChainID == ids.Empty {
				// The chain hasn't been created yet and it's not possible to supply
				// configuration without a chain ID.
				continue
			}
			chainConfigs[chain.ChainID.String()] = chains.ChainConfig{
				Config: []byte(chain.Config),
			}
		}
	}

	marshaledConfigs, err := json.Marshal(chainConfigs)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chain configs: %w", err)
	}
	return base64.StdEncoding.EncodeToString(marshaledConfigs), nil
}

// GetMonitoringLabels retrieves the map of labels and their values to be
// applied to metrics and logs collected from nodes and other collection
// targets for the network (including test workloads). Callers may need
// to set a unique value for the `instance` label to ensure a stable
// identity for the collection target.
func (n *Network) GetMonitoringLabels() map[string]string {
	labels := map[string]string{
		"network_uuid": n.UUID,
		// This label must be set for compatibility with the expected
		// filtering. Nodes should override this value.
		"is_ephemeral_node": "false",
		"network_owner":     n.Owner,
	}
	for label, value := range GetGitHubLabels() {
		labels[label] = value
	}
	return labels
}

// GetGitHubLabels returns a map of GitHub labels and their values if available.
func GetGitHubLabels() map[string]string {
	labels := map[string]string{}
	for _, label := range githubLabels {
		value := os.Getenv(strings.ToUpper(label))
		if len(value) > 0 {
			labels[label] = value
		}
	}
	return labels
}

// Waits until the provided nodes are healthy.
func waitForHealthy(ctx context.Context, log log.Logger, nodes []*Node) error {
	ticker := time.NewTicker(networkHealthCheckInterval)
	defer ticker.Stop()

	unhealthyNodes := set.Of(nodes...)
	for {
		for node := range unhealthyNodes {
			healthy, err := node.IsHealthy(ctx)
			if err != nil {
				return err
			}
			if !healthy {
				continue
			}

			unhealthyNodes.Remove(node)
			log.Info("node is healthy",
				logfields.Stringer("nodeID", node.NodeID),
				logfields.UserString("uri", node.URI),
			)
		}

		if unhealthyNodes.Len() == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("failed to see all nodes healthy before timeout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// Retrieves the root dir for tmpnet data.
func getTmpnetPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".tmpnet"), nil
}

// Retrieves the default root dir for storing networks and their
// configuration.
func getDefaultRootNetworkDir() (string, error) {
	tmpnetPath, err := getTmpnetPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpnetPath, "networks"), nil
}

// Retrieves the path to a reusable network path for the given owner.
func GetReusableNetworkPathForOwner(owner string) (string, error) {
	networkPath, err := getDefaultRootNetworkDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(networkPath, "latest_"+owner), nil
}

const invalidRPCVersion = 0

// checkVMBinaries checks that VM binaries for the given subnets exist and optionally checks that VM
// binaries have the same rpcchainvm version as the indicated node binary.
func checkVMBinaries(log log.Logger, subnets []*Net, config *ProcessRuntimeConfig) error {
	if len(subnets) == 0 {
		// Without subnets there are no VM binaries to check
		return nil
	}

	if config == nil {
		log.Info("skipping rpcchainvm version check because the process runtime is not configured")
		return nil
	}

	nodeRPCVersion, err := getRPCVersion(log, config.LuxNodePath, "--version-json")
	if err != nil {
		log.Warn("unable to check rpcchainvm version for node", logfields.Err(err))
		return nil
	}

	var incompatibleChains bool
	for _, subnet := range subnets {
		for _, chain := range subnet.Chains {
			vmPath := filepath.Join(config.PluginDir, chain.VMID.String())

			// Check that the path exists
			if _, err := os.Stat(vmPath); err != nil {
				log.Warn("unable to check rpcchainvm version for VM",
					logfields.UserString("vmPath", vmPath),
					logfields.Err(err),
				)
				continue
			}

			if len(chain.VersionArgs) == 0 || nodeRPCVersion == invalidRPCVersion {
				// Not possible to check the rpcchainvm version
				continue
			}

			// Check that the VM's rpcchainvm version matches node's version
			vmRPCVersion, err := getRPCVersion(log, vmPath, chain.VersionArgs...)
			if err != nil {
				log.Warn("unable to check rpcchainvm version for VM Binary",
					logfields.UserString("subnet", subnet.Name),
					logfields.Err(err),
				)
			} else if nodeRPCVersion != vmRPCVersion {
				log.Error("unexpected rpcchainvm version for VM binary",
					logfields.UserString("subnet", subnet.Name),
					logfields.UserString("nodePath", config.LuxNodePath),
					logfields.Uint64("nodeRPCVersion", nodeRPCVersion),
					logfields.UserString("vmPath", vmPath),
					logfields.Uint64("vmRPCVersion", vmRPCVersion),
				)
				incompatibleChains = true
			}
		}
	}

	if incompatibleChains {
		return errors.New("the rpcchainvm version of the VMs for one or more chains may not be compatible with the specified node binary")
	}
	return nil
}

type RPCChainVMVersion struct {
	RPCChainVM uint64 `json:"rpcchainvm"`
}

// getRPCVersion attempts to invoke the given command with the specified version arguments and
// retrieve an rpcchainvm version from its output.
func getRPCVersion(log log.Logger, command string, versionArgs ...string) (uint64, error) {
	cmd := exec.Command(command, versionArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("command %q failed with output: %s", command, output)
	}

	// Ignore output before the opening brace to tolerate the case of a command being invoked
	// with `go run` and the go toolchain emitting diagnostic logging before the version output.
	if idx := bytes.IndexByte(output, '{'); idx > 0 {
		log.Info("ignoring leading bytes of JSON version output in advance of opening `{`",
			logfields.UserString("command", command),
			logfields.UserString("ignoredLeadingBytes", string(output[:idx])),
		)
		output = output[idx:]
	}

	version := &RPCChainVMVersion{}
	if err := json.Unmarshal(output, version); err != nil {
		return 0, fmt.Errorf("failed to unmarshal output from command %q: %w, output: %s", command, err, output)
	}

	return version.RPCChainVM, nil
}

// MetricsLinkForNetwork returns a link to the default metrics dashboard for the network
// with the given UUID. The start and end times are accepted as strings to support the
// use of Grafana's time range syntax (e.g. `now`, `now-1h`).
func MetricsLinkForNetwork(networkUUID string, startTime string, endTime string) string {
	if startTime == "" {
		startTime = "now-1h"
	}
	if endTime == "" {
		endTime = "now"
	}
	return fmt.Sprintf(
		"https://%s/d/kBQpRdWnk/lux-main-dashboard?&var-filter=network_uuid%%7C%%3D%%7C%s&var-filter=is_ephemeral_node%%7C%%3D%%7Cfalse&from=%s&to=%s",
		grafanaURI,
		networkUUID,
		startTime,
		endTime,
	)
}
