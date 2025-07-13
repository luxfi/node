// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"path"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/nftfx"
	"github.com/luxfi/node/vms/platformvm/genesis"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/propertyfx"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var (
	DChainAliases = []string{"D", "dao", "platform"}  // D-Chain for DAO governance (legacy platform alias)
	XChainAliases = []string{"X", "xvm", "exchange"}
	CChainAliases = []string{"C", "cvm", "evm"}
	AChainAliases = []string{"A", "avm", "attestation"}
	BChainAliases = []string{"B", "bvm", "bridge"}
	ZChainAliases = []string{"Z", "zvm", "zero", "privacy"}
	VMAliases     = map[ids.ID][]string{
		constants.PlatformVMID: {"dvm", "dao", "platform"},  // D-Chain DAO VM
		constants.AVMID:        {"xvm", "exchange"},         // X-Chain Exchange VM
		constants.EVMID:        {"cvm", "evm"},              // C-Chain Contract VM
		constants.AttestVMID:   {"avm", "attestation"},      // A-Chain Attestation VM
		constants.BridgeVMID:   {"bvm", "bridge"},           // B-Chain Bridge VM
		constants.ZVMID:        {"zvm", "zero", "privacy"},   // Z-Chain Zero-knowledge VM
		secp256k1fx.ID:         {"secp256k1fx"},
		nftfx.ID:               {"nftfx"},
		propertyfx.ID:          {"propertyfx"},
	}
)

// Aliases returns the default aliases based on the network ID
func Aliases(genesisBytes []byte) (map[string][]string, map[ids.ID][]string, error) {
	apiAliases := map[string][]string{
		path.Join(constants.ChainAliasPrefix, constants.PlatformChainID.String()): {
			"D",
			"dao",
			"platform",  // Keep for backwards compatibility
			path.Join(constants.ChainAliasPrefix, "D"),
			path.Join(constants.ChainAliasPrefix, "dao"),
			path.Join(constants.ChainAliasPrefix, "platform"),
		},
	}
	chainAliases := map[ids.ID][]string{
		constants.PlatformChainID: DChainAliases,
	}

	genesis, err := genesis.Parse(genesisBytes) // TODO let's not re-create genesis to do aliasing
	if err != nil {
		return nil, nil, err
	}
	for _, chain := range genesis.Chains {
		uChain := chain.Unsigned.(*txs.CreateChainTx)
		chainID := chain.ID()
		endpoint := path.Join(constants.ChainAliasPrefix, chainID.String())
		switch uChain.VMID {
		case constants.AVMID:
			apiAliases[endpoint] = []string{
				"X",
				"avm",
				path.Join(constants.ChainAliasPrefix, "X"),
				path.Join(constants.ChainAliasPrefix, "avm"),
			}
			chainAliases[chainID] = XChainAliases
		case constants.EVMID:
			apiAliases[endpoint] = []string{
				"C",
				"evm",
				path.Join(constants.ChainAliasPrefix, "C"),
				path.Join(constants.ChainAliasPrefix, "evm"),
			}
			chainAliases[chainID] = CChainAliases
		case constants.AttestVMID:
			apiAliases[endpoint] = []string{
				"A",
				"avm",
				path.Join(constants.ChainAliasPrefix, "A"),
				path.Join(constants.ChainAliasPrefix, "avm"),
			}
			chainAliases[chainID] = AChainAliases
		case constants.BridgeVMID:
			apiAliases[endpoint] = []string{
				"B",
				"bridgevm",
				path.Join(constants.ChainAliasPrefix, "B"),
				path.Join(constants.ChainAliasPrefix, "bridgevm"),
			}
			chainAliases[chainID] = BChainAliases
		case constants.ZVMID:
			apiAliases[endpoint] = []string{
				"Z",
				"zvm",
				path.Join(constants.ChainAliasPrefix, "Z"),
				path.Join(constants.ChainAliasPrefix, "zvm"),
			}
			chainAliases[chainID] = ZChainAliases
		}
	}
	return apiAliases, chainAliases, nil
}
