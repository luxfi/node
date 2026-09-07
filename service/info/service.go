// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// What this node will tell anyone who asks.
//
// The service is a value with a registry of typed operations on it — see ops.go,
// which is where every question and its answer live. This file is the value: what
// the service is built from and what it holds.
package info

import (
	"errors"
	"net/netip"

	apiinfo "github.com/luxfi/api/info"
	apitypes "github.com/luxfi/api/types"
	"github.com/luxfi/constants"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/network"
	nodepeer "github.com/luxfi/node/network/peer"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/version"
	"github.com/luxfi/node/vms"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/util"
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
	// node version handler can return it without round-tripping into the
	// chain manager. Nil means "do not advertise" (pre-wired callers, tests).
	Consensus *apiinfo.ConsensusInfo
}

// New builds the info service. Its operations are registered by [Info.Ops].
func New(
	parameters Parameters,
	log log.Logger,
	validators validators.Manager,
	chainManager chains.Manager,
	vmManager vms.Manager,
	myIP *utils.Atomic[netip.AddrPort],
	network network.Network,
) *Info {
	return &Info{
		Parameters:   parameters,
		log:          log,
		validators:   validators,
		chainManager: chainManager,
		vmManager:    vmManager,
		myIP:         myIP,
		networking:   network,
	}
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

// toP2PPeerInfo is a live connection as the wire carries it. The addresses and
// the instants become values with a layout, and the three sets become lists in
// the order the JSON already sorts them by — see [apitypes.Addr], [apitypes.Time].
func toP2PPeerInfo(info nodepeer.Info) apiinfo.PeerInfo {
	return apiinfo.PeerInfo{
		IP:             apitypes.AddrOf(info.IP),
		PublicIP:       apitypes.AddrOf(info.PublicIP),
		ID:             info.ID,
		Version:        info.Version,
		LastSent:       apitypes.TimeOf(info.LastSent),
		LastReceived:   apitypes.TimeOf(info.LastReceived),
		ObservedUptime: apitypes.Uint32(info.ObservedUptime),
		TrackedChains:  sortedIDs(info.TrackedChains),
		SupportedLPs:   sortedNumbers(info.SupportedLPs),
		ObjectedLPs:    sortedNumbers(info.ObjectedLPs),
	}
}

// GetChainsReply is the response for the chains this node runs.
type GetChainsReply struct {
	// Chains are the chains this node is running.
	Chains []chains.ChainInfo `json:"chains"`
}
