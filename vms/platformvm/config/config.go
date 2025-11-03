// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/chains"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/units"
)

var Default = Config{
	Network:                       DefaultNetwork,
	BlockCacheSize:                64 * units.MiB,
	TxCacheSize:                   128 * units.MiB,
	TransformedSubnetTxCacheSize:  4 * units.MiB,
	RewardUTXOsCacheSize:          2048,
	ChainCacheSize:                2048,
	ChainDBCacheSize:              2048,
	BlockIDCacheSize:              8192,
	FxOwnerCacheSize:              4 * units.MiB,
	SubnetToL1ConversionCacheSize: 4 * units.MiB,
	L1WeightsCacheSize:            16 * units.KiB,
	L1InactiveValidatorsCacheSize: 256 * units.KiB,
	L1SubnetIDNodeIDCacheSize:     16 * units.KiB,
	ChecksumsEnabled:              false,
	MempoolPruneFrequency:         30 * time.Minute,
}

// Config contains all of the user-configurable parameters of the PlatformVM.
type Config struct {
	Network                       Network          `json:"network"`
	BlockCacheSize                int              `json:"block-cache-size"`
	TxCacheSize                   int              `json:"tx-cache-size"`
	TransformedSubnetTxCacheSize  int              `json:"transformed-subnet-tx-cache-size"`
	RewardUTXOsCacheSize          int              `json:"reward-utxos-cache-size"`
	ChainCacheSize                int              `json:"chain-cache-size"`
	ChainDBCacheSize              int              `json:"chain-db-cache-size"`
	BlockIDCacheSize              int              `json:"block-id-cache-size"`
	FxOwnerCacheSize              int              `json:"fx-owner-cache-size"`
	SubnetToL1ConversionCacheSize int              `json:"subnet-to-l1-conversion-cache-size"`
	L1WeightsCacheSize            int              `json:"l1-weights-cache-size"`
	L1InactiveValidatorsCacheSize int              `json:"l1-inactive-validators-cache-size"`
	L1SubnetIDNodeIDCacheSize     int              `json:"l1-subnet-id-node-id-cache-size"`
	ChecksumsEnabled              bool             `json:"checksums-enabled"`
	MempoolPruneFrequency         time.Duration    `json:"mempool-prune-frequency"`
	SybilProtectionEnabled        bool             `json:"sybil-protection-enabled"`
	TrackedSubnets                set.Set[ids.ID]  `json:"tracked-subnets"`
	Chains                        *chains.Manager  `json:"-"`
}

// GetConfig returns a Config from the provided json encoded bytes. If a
// configuration is not provided in the bytes, the default value is set. If
// empty bytes are provided, the default config is returned.
func GetConfig(b []byte) (*Config, error) {
	ec := Default

	// An empty slice is invalid json, so handle that as a special case.
	if len(b) == 0 {
		return &ec, nil
	}

	return &ec, json.Unmarshal(b, &ec)
}

// QueueExistingChain queues an existing chain for creation with minimal parameters
// This is used when discovering orphaned chains that have data but no CreateChainTx
func (c *Config) QueueExistingChain(chainID ids.ID, netID ids.ID, vmID ids.ID) {
	if c.SybilProtectionEnabled && // Sybil protection is enabled, so nodes might not validate all chains
		constants.PrimaryNetworkID != netID && // All nodes must validate the primary network
		!c.TrackedSubnets.Contains(netID) { // This node doesn't validate this blockchain
		return
	}

	// For existing chains, we need to provide minimal genesis data
	// The EVM will load existing data from disk
	genesisData := []byte(`{}`)

	chainParams := chains.ChainParameters{
		ID:          chainID,
		NetID:       netID,
		GenesisData: genesisData,
		VMID:        vmID,
		FxIDs:       nil, // No FxIDs for existing chains
	}
	c.Chains.QueueChainCreation(chainParams)
}

// QueueExistingChainWithGenesis queues an existing chain with genesis data
// This is used when discovering orphaned chains that have config data
func (c *Config) QueueExistingChainWithGenesis(chainID ids.ID, netID ids.ID, vmID ids.ID, genesisData []byte) {
	if c.SybilProtectionEnabled && // Sybil protection is enabled, so nodes might not validate all chains
		constants.PrimaryNetworkID != netID && // All nodes must validate the primary network
		!c.TrackedSubnets.Contains(netID) { // This node doesn't validate this blockchain
		return
	}

	chainParams := chains.ChainParameters{
		ID:          chainID,
		NetID:       netID,
		GenesisData: genesisData,
		VMID:        vmID,
		FxIDs:       nil, // No FxIDs for existing chains
	}
	c.Chains.QueueChainCreation(chainParams)
}
