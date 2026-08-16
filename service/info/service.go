// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"

	"github.com/gorilla/rpc/v2"

	apiinfo "github.com/luxfi/api/info"
	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/network"
	nodepeer "github.com/luxfi/node/network/peer"
	// "github.com/luxfi/consensus/networking/benchlist" // Unused
	"github.com/luxfi/constants"
	"github.com/luxfi/formatting"
	"github.com/luxfi/log"
	"github.com/luxfi/node/upgrade"
	avajson "github.com/luxfi/node/utils/json"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/utils"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
	validators "github.com/luxfi/validators"
)

var (
	errNoChainProvided = errors.New("argument 'chain' not given")

	mainnetGetTxFeeResponse = apiinfo.GetTxFeeResponse{
		CreateNetworkTxFee:     apitypes.Uint64(1 * constants.Lux),
		TransformChainTxFee:    apitypes.Uint64(10 * constants.Lux),
		CreateChainTxFee:       apitypes.Uint64(1 * constants.Lux),
		AddNetworkValidatorFee: apitypes.Uint64(constants.MilliLux),
		AddNetworkDelegatorFee: apitypes.Uint64(constants.MilliLux),
	}
	defaultGetTxFeeResponse = apiinfo.GetTxFeeResponse{
		CreateNetworkTxFee:     apitypes.Uint64(100 * constants.MilliLux),
		TransformChainTxFee:    apitypes.Uint64(100 * constants.MilliLux),
		CreateChainTxFee:       apitypes.Uint64(100 * constants.MilliLux),
		AddNetworkValidatorFee: apitypes.Uint64(constants.MilliLux),
		AddNetworkDelegatorFee: apitypes.Uint64(constants.MilliLux),
	}
)

// Info is the API service for unprivileged info on a node
type Info struct {
	Parameters
	log          log.Logger
	validators   validators.Manager
	myIP         *utils.Atomic[netip.AddrPort]
	networking   network.Network
	chainManager chains.Manager
	vmManager    vms.Manager
	// benchlist    benchlist.Manager // benchlist package doesn't exist
}

type Parameters struct {
	Version   *version.Application
	NodeID    ids.NodeID
	NodePOP   *signer.ProofOfPossession
	NetworkID uint32
	VMManager vms.Manager
	Upgrades  upgrade.Config

	TxFee            uint64
	CreateAssetTxFee uint64

	// Consensus is the consensus configuration snapshot for this node. The
	// node binary populates it at boot from the live engine state so the
	// info.getNodeVersion handler can return it without round-tripping
	// into the chain manager. Nil means "do not advertise" (pre-wired
	// callers, tests).
	Consensus *apiinfo.ConsensusInfo
}

func NewService(
	parameters Parameters,
	log log.Logger,
	validators validators.Manager,
	chainManager chains.Manager,
	vmManager vms.Manager,
	myIP *utils.Atomic[netip.AddrPort],
	network network.Network,
	// benchlist benchlist.Manager, // benchlist package doesn't exist
) (http.Handler, error) {
	server := rpc.NewServer()
	codec := avajson.NewCodec()
	server.RegisterCodec(codec, "application/json")
	server.RegisterCodec(codec, "application/json;charset=UTF-8")
	return server, server.RegisterService(
		&Info{
			Parameters:   parameters,
			log:          log,
			validators:   validators,
			chainManager: chainManager,
			vmManager:    vmManager,
			myIP:         myIP,
			networking:   network,
			// benchlist:    benchlist, // benchlist removed
		},
		"info",
	)
}

func toAPIProofOfPossession(pop *signer.ProofOfPossession) (*apiinfo.ProofOfPossession, error) {
	if pop == nil {
		return nil, nil
	}
	publicKey, err := formatting.Encode(formatting.HexNC, pop.PublicKey[:])
	if err != nil {
		return nil, err
	}
	proof, err := formatting.Encode(formatting.HexNC, pop.ProofOfPossession[:])
	if err != nil {
		return nil, err
	}
	return &apiinfo.ProofOfPossession{
		PublicKey:         publicKey,
		ProofOfPossession: proof,
	}, nil
}

func toP2PPeerInfo(info nodepeer.Info) apiinfo.PeerInfo {
	return apiinfo.PeerInfo{
		IP:             info.IP,
		PublicIP:       info.PublicIP,
		ID:             info.ID,
		Version:        info.Version,
		LastSent:       info.LastSent,
		LastReceived:   info.LastReceived,
		ObservedUptime: apitypes.Uint32(info.ObservedUptime),
		TrackedChains:  info.TrackedChains,
		SupportedLPs:   info.SupportedLPs,
		ObjectedLPs:    info.ObjectedLPs,
	}
}

// GetNodeVersion returns the version this node is running
func (i *Info) GetNodeVersion(_ *http.Request, _ *struct{}, reply *apiinfo.GetNodeVersionReply) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getNodeVersion"),
	)

	reply.Version = i.Version.String()
	reply.DatabaseVersion = version.CurrentDatabase.String()
	reply.RPCProtocolVersion = apitypes.Uint32(version.RPCChainVMProtocol)
	reply.GitCommit = version.GitCommit
	reply.VMVersions = map[string]string{}
	if i.Consensus != nil {
		c := *i.Consensus
		reply.Consensus = &c
	}
	return nil
}

// GetNodeID returns the node ID of this node
func (i *Info) GetNodeID(_ *http.Request, _ *struct{}, reply *apiinfo.GetNodeIDReply) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getNodeID"),
	)

	reply.NodeID = i.NodeID
	pop, err := toAPIProofOfPossession(i.NodePOP)
	if err != nil {
		return err
	}
	reply.NodePOP = pop
	return nil
}

// GetNodeIP returns the IP of this node
func (i *Info) GetNodeIP(_ *http.Request, _ *struct{}, reply *apiinfo.GetNodeIPReply) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getNodeIP"),
	)

	reply.IP = i.myIP.Get()
	return nil
}

// GetNetworkID returns the network ID this node is running on
func (i *Info) GetNetworkID(_ *http.Request, _ *struct{}, reply *apiinfo.GetNetworkIDReply) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getNetworkID"),
	)

	reply.NetworkID = apitypes.Uint32(i.NetworkID)
	return nil
}

// GetNetworkName returns the network name this node is running on
func (i *Info) GetNetworkName(_ *http.Request, _ *struct{}, reply *apiinfo.GetNetworkNameReply) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getNetworkName"),
	)

	reply.NetworkName = constants.NetworkName(i.NetworkID)
	return nil
}

// GetBlockchainID returns the blockchain ID that resolves the alias that was supplied
func (i *Info) GetBlockchainID(_ *http.Request, args *apiinfo.GetBlockchainIDArgs, reply *apiinfo.GetBlockchainIDReply) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getBlockchainID"),
	)

	bID, err := i.chainManager.Lookup(args.Alias)
	reply.BlockchainID = bID
	return err
}

// Peers returns the list of current validators
func (i *Info) Peers(_ *http.Request, args *apiinfo.PeersArgs, reply *apiinfo.PeersReply) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "peers"),
	)

	peers := i.networking.PeerInfo(args.NodeIDs)
	peerInfo := make([]apiinfo.Peer, len(peers))
	for index, peer := range peers {
		// benchlist removed
		// benchedIDs := i.benchlist.GetBenched(peer.ID)
		benchedAliases := make([]string, 0) // Empty list since benchlist is removed
		// for idx, id := range benchedIDs {
		// 	alias, err := i.chainManager.PrimaryAlias(id)
		// 	if err != nil {
		// 		return fmt.Errorf("failed to get primary alias for chain ID %s: %w", id, err)
		// 	}
		// 	benchedAliases[idx] = alias
		// }
		peerInfo[index] = apiinfo.Peer{
			PeerInfo: toP2PPeerInfo(peer),
			Benched:  benchedAliases,
		}
	}

	reply.Peers = peerInfo
	reply.NumPeers = apitypes.Uint64(len(reply.Peers))
	return nil
}

// IsBootstrapped returns nil and sets [reply.IsBootstrapped] == true iff [args.Chain] exists and is done bootstrapping
// Returns an error if the chain doesn't exist
func (i *Info) IsBootstrapped(_ *http.Request, args *apiinfo.IsBootstrappedArgs, reply *apiinfo.IsBootstrappedResponse) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "isBootstrapped"),
		log.String("chain", args.Chain),
	)

	if args.Chain == "" {
		return errNoChainProvided
	}
	chainID, err := i.chainManager.Lookup(args.Chain)
	if err != nil {
		return fmt.Errorf("there is no chain with alias/ID '%s'", args.Chain)
	}
	reply.IsBootstrapped = i.chainManager.IsBootstrapped(chainID)
	return nil
}

// Upgrades returns the upgrade schedule this node is running.
func (i *Info) Upgrades(_ *http.Request, _ *struct{}, reply *upgrade.Config) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "upgrades"),
	)

	*reply = i.Parameters.Upgrades
	return nil
}

func (i *Info) Uptime(_ *http.Request, _ *struct{}, reply *apiinfo.UptimeResponse) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "uptime"),
	)

	result, err := i.networking.NodeUptime()
	if err != nil {
		return fmt.Errorf("couldn't get node uptime: %w", err)
	}
	reply.WeightedAveragePercentage = apitypes.Float64(result.WeightedAveragePercentage)
	reply.RewardingStakePercentage = apitypes.Float64(result.RewardingStakePercentage)
	return nil
}

func getLP(reply *apiinfo.LPsReply, lpNum uint32) *apiinfo.LP {
	lp, ok := reply.LPs[lpNum]
	if !ok {
		lp = &apiinfo.LP{}
		reply.LPs[lpNum] = lp
	}
	return lp
}

func (i *Info) Lps(_ *http.Request, _ *struct{}, reply *apiinfo.LPsReply) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "lps"),
	)

	reply.LPs = make(map[uint32]*apiinfo.LP, constants.CurrentLPs.Len())
	peers := i.networking.PeerInfo(nil)
	for _, peer := range peers {
		w := i.validators.GetWeight(constants.PrimaryNetworkID, peer.ID)
		weight := apitypes.Uint64(w)
		if weight == 0 {
			continue
		}

		// SupportedLPs and ObjectedLPs not available on peer.Info type
		// for lpNum := range peer.SupportedLPs {
		// 	lp := reply.getLP(lpNum)
		// 	lp.Supporters.Add(peer.ID)
		// 	lp.SupportWeight += weight
		// }
		// for lpNum := range peer.ObjectedLPs {
		// 	lp := reply.getLP(lpNum)
		// 	lp.Objectors.Add(peer.ID)
		// 	lp.ObjectWeight += weight
		// }
		_ = peer // Silence unused variable warning
	}

	totalWeight, err := i.validators.TotalWeight(constants.PrimaryNetworkID)
	if err != nil {
		return err
	}
	for lpNum := range constants.CurrentLPs {
		lp := getLP(reply, lpNum)
		lp.AbstainWeight = apitypes.Uint64(totalWeight) - lp.SupportWeight - lp.ObjectWeight
	}
	return nil
}

// GetTxFee returns the transaction fee in µLUX.
func (i *Info) GetTxFee(_ *http.Request, _ *struct{}, reply *apiinfo.GetTxFeeResponse) error {
	i.log.Warn("deprecated API called",
		log.String("service", "info"),
		log.String("method", "getTxFee"),
	)

	switch i.NetworkID {
	case constants.MainnetID:
		*reply = mainnetGetTxFeeResponse
	default:
		*reply = defaultGetTxFeeResponse
	}
	reply.TxFee = apitypes.Uint64(i.TxFee)
	reply.CreateAssetTxFee = apitypes.Uint64(i.CreateAssetTxFee)
	return nil
}

// GetVMs lists the virtual machines installed on the node
func (i *Info) GetVMs(r *http.Request, _ *struct{}, reply *apiinfo.GetVMsReply) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getVMs"),
	)

	// Fetch the VMs registered on this node.
	ctx := r.Context()
	vmIDs, err := i.VMManager.ListFactories(ctx)
	if err != nil {
		return err
	}

	reply.VMs = make(map[ids.ID][]string, len(vmIDs))
	for _, vmID := range vmIDs {
		alias, err := i.VMManager.PrimaryAlias(ctx, vmID)
		if err != nil || alias == vmID.String() {
			reply.VMs[vmID] = nil
			continue
		}
		reply.VMs[vmID] = []string{alias}
	}
	reply.Fxs = map[ids.ID]string{
		secp256k1fx.ID: secp256k1fx.Name,
		nftfx.ID:       nftfx.Name,
		propertyfx.ID:  propertyfx.Name,
	}
	return nil
}

// GetChainsReply is the response for info.getChains.
type GetChainsReply struct {
	Chains []chains.ChainInfo `json:"chains"`
}

// GetChains returns the locally running chains on this node.
// Unlike platform.getBlockchains (which returns ALL registered chains),
// this returns only chains this node is actively tracking and serving.
func (i *Info) GetChains(_ *http.Request, _ *struct{}, reply *GetChainsReply) error {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getChains"),
	)
	reply.Chains = i.chainManager.GetChains()
	return nil
}
