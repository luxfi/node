// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// The questions this node answers, and the one place each answer is written.
//
// A handler here IS the operation. There is no second copy of it behind a
// JSON-RPC method table: registering it yields the REST route, the OpenAPI
// document, the MCP tool, the CLI command, the generated SDK and the by-name call
// plane, all from this one registration and the doc comment above it.
//
// EVERY OPERATION IS A GET, and that is the whole of its authorization. The
// node's rule reads the method (server/http/authorize.go) and info is what a node
// tells anyone; nothing here changes the node.
//
// # Filtering a read over REST
//
// A GET carries no body (RFC 9110), so an argument that narrows a read has to
// ride the URL. peers takes a list of node ids and lps takes none; a list is
// spelled comma-separated in one value — ?nodeIDs=NodeID-a,NodeID-b — which is
// `style: form, explode: false`, what the document already publishes for a
// repeated parameter. Each id reads itself from that text, so the filter a
// caller sends over MCP or the call plane is the same filter a URL can carry.

package info

import (
	"context"
	"fmt"
	"slices"

	apiinfo "github.com/luxfi/api/info"
	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/math/set"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/zap-proto/zip"

	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/version"
)

//go:generate go run github.com/zap-proto/zip/cmd/zipdoc

// Ops is this service's typed operations. The paths are relative to where the
// app is mounted, which the node decides — a service does not name its own
// address.
func (i *Info) Ops() *zip.App {
	app := zip.New(zip.Config{
		AppName:               "info",
		Logger:                i.log,
		DisableStartupMessage: true,
		OpenAPI: zip.OpenAPIConfig{
			Title:       "Lux node info",
			Description: "What a Lux node tells anyone who asks: what it is running, what network it is on, who it is connected to, and what its chains cost.",
			Version:     i.release(),
		},
	})
	zip.Get(app, "/node/version", i.nodeVersion)
	zip.Get(app, "/node/id", i.identity)
	zip.Get(app, "/node/ip", i.address)
	zip.Get(app, "/network/id", i.networkID)
	zip.Get(app, "/network/name", i.networkName)
	zip.Get(app, "/chain/id", i.chainID)
	zip.Get(app, "/chain/bootstrapped", i.bootstrapped)
	zip.Get(app, "/chains", i.chains)
	zip.Get(app, "/peers", i.peers)
	zip.Get(app, "/lps", i.lps)
	zip.Get(app, "/vms", i.vms)
	zip.Get(app, "/upgrades", i.upgrades)
	zip.Get(app, "/uptime", i.uptime)
	zip.Get(app, "/fees", i.fees)
	return app
}

// release is the node's version for the document's info block, or the empty
// string when this service was built without one — a test, or a caller that has
// not wired Parameters through.
func (i *Info) release() string {
	if i.Version == nil {
		return ""
	}
	return i.Version.String()
}

// NodeVersion is what this node is running: its own release, the database format
// it reads, the VM protocol it speaks, and the consensus it is configured for.
//
// Response: {"version": "luxd/1.36.178", "databaseVersion": "v1.4.5", "rpcProtocolVersion": "39", "gitCommit": "", "vmVersions": {}}
func (i *Info) nodeVersion(_ context.Context, _ *struct{}) (*apiinfo.GetNodeVersionReply, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getNodeVersion"),
	)

	reply := &apiinfo.GetNodeVersionReply{
		Version:            i.Version.String(),
		DatabaseVersion:    version.CurrentDatabase.String(),
		RPCProtocolVersion: apitypes.Uint32(version.RPCChainVMProtocol),
		GitCommit:          version.GitCommit,
		VMVersions:         apiinfo.VMVersions{},
	}
	if i.Consensus != nil {
		consensus := *i.Consensus
		reply.Consensus = &consensus
	}
	return reply, nil
}

// Identity is this node's id, with the proof it holds the staking key that id is
// derived from.
//
// Response: {"nodeID": "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg", "nodePOP": {"publicKey": "0x8f95423f7142d00a48e1014a3de8d28907d420dc33b3052a6dee03a3f2941a393c2351e354704ca66a3fc29870282e15", "proofOfPossession": "0x86a3ab4c45cfe31cae34c1d06f212434ac71b1be6cfe046c80c162e057614a94a5bc9f1ded1a7029deb0ba4ca7c9b71411e293438691be79c2dbf19d1ca7c3eadb9c756246fc5de5b7b89511c7d7dda6"}}
func (i *Info) identity(_ context.Context, _ *struct{}) (*apiinfo.GetNodeIDReply, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getNodeID"),
	)

	pop, err := toAPIProofOfPossession(i.NodePOP)
	if err != nil {
		return nil, err
	}
	return &apiinfo.GetNodeIDReply{NodeID: i.NodeID, NodePOP: pop}, nil
}

// Address is where this node tells its peers to reach it.
//
// Response: {"ip": "203.0.113.9:9651"}
func (i *Info) address(_ context.Context, _ *struct{}) (*apiinfo.GetNodeIPReply, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getNodeIP"),
	)

	return &apiinfo.GetNodeIPReply{IP: apitypes.AddrOf(i.myIP.Get())}, nil
}

// NetworkID is the number of the network this node is on.
//
// Response: {"networkID": "96369"}
func (i *Info) networkID(_ context.Context, _ *struct{}) (*apiinfo.GetNetworkIDReply, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getNetworkID"),
	)

	return &apiinfo.GetNetworkIDReply{NetworkID: apitypes.Uint32(i.Parameters.NetworkID)}, nil
}

// NetworkName is the name of the network this node is on.
//
// Response: {"networkName": "mainnet"}
func (i *Info) networkName(_ context.Context, _ *struct{}) (*apiinfo.GetNetworkNameReply, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getNetworkName"),
	)

	return &apiinfo.GetNetworkNameReply{NetworkName: constants.NetworkName(i.Parameters.NetworkID)}, nil
}

// ChainID resolves a chain's alias to the id it names.
//
// Example: {"alias": "X"}
// Response: {"blockchainID": "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM"}
func (i *Info) chainID(_ context.Context, in *apiinfo.GetBlockchainIDArgs) (*apiinfo.GetBlockchainIDReply, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getBlockchainID"),
	)

	id, err := i.chainManager.Lookup(in.Alias)
	if err != nil {
		return nil, err
	}
	return &apiinfo.GetBlockchainIDReply{BlockchainID: id}, nil
}

// Bootstrapped reports whether a chain has finished bootstrapping on this node.
//
// Example: {"chain": "X"}
// Response: {"isBootstrapped": true}
func (i *Info) bootstrapped(_ context.Context, in *apiinfo.IsBootstrappedArgs) (*apiinfo.IsBootstrappedResponse, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "isBootstrapped"),
		log.String("chain", in.Chain),
	)

	if in.Chain == "" {
		return nil, errNoChainProvided
	}
	chainID, err := i.chainManager.Lookup(in.Chain)
	if err != nil {
		return nil, fmt.Errorf("there is no chain with alias/ID '%s'", in.Chain)
	}
	return &apiinfo.IsBootstrappedResponse{IsBootstrapped: i.chainManager.IsBootstrapped(chainID)}, nil
}

// Chains are the chains this node is running, which is a subset of the chains
// the P-Chain knows about — platform.getBlockchains answers for those.
//
// Response: {"chains": [{"id": "11111111111111111111111111111111LpoYY", "name": "P-Chain", "vmID": "11111111111111111111111111111111LpoYY", "bootstrapped": true}]}
func (i *Info) chains(_ context.Context, _ *struct{}) (*GetChainsReply, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getChains"),
	)

	return &GetChainsReply{Chains: i.chainManager.GetChains()}, nil
}

// Peers are the nodes this one is connected to. A list of node ids narrows the
// answer to those, and no list asks for all of them.
//
// Example: {"nodeIDs": []}
// Response: {"numPeers": "1", "peers": [{"ip": "203.0.113.9:9651", "publicIP": "203.0.113.9:9651", "nodeID": "NodeID-7Xhw2mDxuDS44j42TCB6U5579esbSt3Lg", "version": "luxd/1.36.178", "lastSent": "2026-08-29T00:00:00Z", "lastReceived": "2026-08-29T00:00:00Z", "observedUptime": "99", "trackedChains": [], "supportedLPs": [], "objectedLPs": [], "benched": []}]}
func (i *Info) peers(_ context.Context, in *apiinfo.PeersArgs) (*apiinfo.PeersReply, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "peers"),
	)

	found := i.networking.PeerInfo(in.NodeIDs)
	peers := make([]apiinfo.Peer, len(found))
	for index, peer := range found {
		peers[index] = apiinfo.Peer{
			PeerInfo: toP2PPeerInfo(peer),
			// Benching is not wired on this node, so no chain is benched.
			Benched: []string{},
		}
	}
	return &apiinfo.PeersReply{
		Peers:    peers,
		NumPeers: apitypes.Uint64(len(peers)),
	}, nil
}

// LPs is where the network's stake stands on every current Lux Proposal.
//
// Response: {"lps": {"23": {"supportWeight": "0", "supporters": [], "objectWeight": "0", "objectors": [], "abstainWeight": "1000000000000"}}}
func (i *Info) lps(_ context.Context, _ *struct{}) (*apiinfo.LPsReply, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "lps"),
	)

	totalWeight, err := i.validators.TotalWeight(constants.PrimaryNetworkID)
	if err != nil {
		return nil, err
	}

	// A peer's own votes are not carried on the connection this node holds, so
	// every validator abstains: the weight is the whole primary network's.
	lps := make(apiinfo.LPs, 0, constants.CurrentLPs.Len())
	for number := range constants.CurrentLPs {
		lps = append(lps, apiinfo.LPStatus{
			Number: number,
			LP: apiinfo.LP{
				Supporters:    []ids.NodeID{},
				Objectors:     []ids.NodeID{},
				AbstainWeight: apitypes.Uint64(totalWeight),
			},
		})
	}
	slices.SortFunc(lps, func(x, y apiinfo.LPStatus) int { return int(x.Number) - int(y.Number) })
	return &apiinfo.LPsReply{LPs: lps}, nil
}

// VMs are the virtual machines installed on this node, and the feature
// extensions its UTXO chains understand.
//
// Response: {"vms": {"mgj786NP7uDwBCcq6YwThhaN8FLyybkCa4zBWTQbNgmK6k9A6": ["platformvm"]}, "fxs": {"spqBHsy2UBGpjaQJezEywUy1AB98eVjJ3WQ38x1vpjsx4xPkY": "secp256k1fx"}}
func (i *Info) vms(ctx context.Context, _ *struct{}) (*apiinfo.GetVMsReply, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "getVMs"),
	)

	vmIDs, err := i.VMManager.ListFactories(ctx)
	if err != nil {
		return nil, err
	}

	installed := make(apiinfo.VMAliases, 0, len(vmIDs))
	for _, vmID := range vmIDs {
		entry := apiinfo.VMAlias{VM: vmID}
		if alias, err := i.VMManager.PrimaryAlias(ctx, vmID); err == nil && alias != vmID.String() {
			entry.Aliases = []string{alias}
		}
		installed = append(installed, entry)
	}
	slices.SortFunc(installed, func(x, y apiinfo.VMAlias) int { return x.VM.Compare(y.VM) })

	fxs := apiinfo.FxNames{
		{Fx: secp256k1fx.ID, Name: secp256k1fx.Name},
		{Fx: nftfx.ID, Name: nftfx.Name},
		{Fx: propertyfx.ID, Name: propertyfx.Name},
	}
	slices.SortFunc(fxs, func(x, y apiinfo.FxName) int { return x.Fx.Compare(y.Fx) })

	return &apiinfo.GetVMsReply{VMs: installed, Fxs: fxs}, nil
}

// Upgrades is the upgrade schedule this node runs.
//
// Response: {"xChainStopVertexID": "jrGWDh5Po9FMj54depyunNixpia5PN4aAYxfmNzU8n752Rjga", "epochDuration": 300000000000}
func (i *Info) upgrades(_ context.Context, _ *struct{}) (*upgrade.Config, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "upgrades"),
	)

	schedule := i.Parameters.Upgrades
	return &schedule, nil
}

// Uptime is how much of the network, by stake, reports having seen this node up.
//
// Response: {"rewardingStakePercentage": "100.0000", "weightedAveragePercentage": "99.9999"}
func (i *Info) uptime(_ context.Context, _ *struct{}) (*apiinfo.UptimeResponse, error) {
	i.log.Debug("API called",
		log.String("service", "info"),
		log.String("method", "uptime"),
	)

	result, err := i.networking.NodeUptime()
	if err != nil {
		return nil, fmt.Errorf("couldn't get node uptime: %w", err)
	}
	return &apiinfo.UptimeResponse{
		WeightedAveragePercentage: apitypes.Float64(result.WeightedAveragePercentage),
		RewardingStakePercentage:  apitypes.Float64(result.RewardingStakePercentage),
	}, nil
}

// Fees are the transaction fees this node charges, in nLUX. Deprecated: the
// P-Chain's fees are dynamic and platform.getFeeConfig is the live answer.
//
// Response: {"txFee": "1000000", "createAssetTxFee": "10000000", "createNetworkTxFee": "1000000000", "transformChainTxFee": "10000000000", "createChainTxFee": "1000000000", "addNetworkValidatorFee": "1000000", "addNetworkDelegatorFee": "1000000"}
func (i *Info) fees(_ context.Context, _ *struct{}) (*apiinfo.GetTxFeeResponse, error) {
	i.log.Warn("deprecated API called",
		log.String("service", "info"),
		log.String("method", "getTxFee"),
	)

	reply := defaultGetTxFeeResponse
	if i.Parameters.NetworkID == constants.MainnetID {
		reply = mainnetGetTxFeeResponse
	}
	reply.TxFee = apitypes.Uint64(i.TxFee)
	reply.CreateAssetTxFee = apitypes.Uint64(i.CreateAssetTxFee)
	return &reply, nil
}

// sortedIDs and sortedNumbers are a set as the wire carries it: a list, in an
// order, because a map has neither. The JSON was already an array — an
// unordered one, so a caller could read the same answer two ways.
func sortedIDs(s set.Set[ids.ID]) []ids.ID {
	out := s.List()
	slices.SortFunc(out, func(x, y ids.ID) int { return x.Compare(y) })
	return out
}

func sortedNumbers(s set.Set[uint32]) []uint32 {
	out := s.List()
	slices.Sort(out)
	return out
}
