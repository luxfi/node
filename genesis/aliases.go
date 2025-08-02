// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"path"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/vms/nftfx"
	"github.com/luxfi/node/v2/vms/platformvm/genesis"
	"github.com/luxfi/node/v2/vms/platformvm/txs"
	"github.com/luxfi/node/v2/vms/propertyfx"
	"github.com/luxfi/node/v2/vms/secp256k1fx"
)

var (
	QChainAliases = []string{"Q", "quantum"}                 // Q-Chain aliases
	PChainAliases = []string{"Q", "quantum", "P", "platform"} // Deprecated: Use QChainAliases. Includes legacy P aliases for compatibility
	XChainAliases = []string{"X", "xvm"}
	CChainAliases = []string{"C", "evm"}
	VMAliases     = map[ids.ID][]string{
		constants.QuantumVMID: {"quantum", "platform"}, // Include platform for backward compatibility
		constants.XVMID:       {"xvm"},
		constants.EVMID:       {"evm"},
		secp256k1fx.ID:        {"secp256k1fx"},
		nftfx.ID:              {"nftfx"},
		propertyfx.ID:         {"propertyfx"},
	}
)

// Aliases returns the default aliases based on the network ID
func Aliases(genesisBytes []byte) (map[string][]string, map[ids.ID][]string, error) {
	apiAliases := map[string][]string{
		path.Join(constants.ChainAliasPrefix, constants.QuantumChainID.String()): {
			"Q",
			"quantum",
			"P",                                             // Legacy alias for backward compatibility
			"platform",                                      // Legacy alias for backward compatibility
			path.Join(constants.ChainAliasPrefix, "Q"),
			path.Join(constants.ChainAliasPrefix, "quantum"),
			path.Join(constants.ChainAliasPrefix, "P"),        // Legacy
			path.Join(constants.ChainAliasPrefix, "platform"), // Legacy
		},
	}
	chainAliases := map[ids.ID][]string{
		constants.QuantumChainID: QChainAliases,
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
		case constants.XVMID:
			apiAliases[endpoint] = []string{
				"X",
				"xvm",
				path.Join(constants.ChainAliasPrefix, "X"),
				path.Join(constants.ChainAliasPrefix, "xvm"),
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
		}
	}
	return apiAliases, chainAliases, nil
}
