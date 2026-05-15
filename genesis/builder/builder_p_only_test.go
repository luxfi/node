// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/vms/platformvm/genesis"
	pchaintxs "github.com/luxfi/node/vms/platformvm/txs"
)

// TestVMGenesisOptInChains asserts that VMGenesis returns an error for VM IDs
// not present in the genesis chains list — the core "P-only" contract relied
// upon by node.initChainManager to log "skipping" and run the node without
// the missing chain. The behaviour MUST be deterministic and must not panic.
func TestVMGenesisOptInChains(t *testing.T) {
	require := require.New(t)

	// Build a genesis with zero chains — pure P-only.
	pOnly := &genesis.Genesis{
		Chains: nil,
	}
	pOnlyBytes, err := genesis.Codec.Marshal(pchaintxs.CodecVersion, pOnly)
	require.NoError(err)

	for name, vmID := range map[string]ids.ID{
		"XVM":     constants.XVMID,
		"EVM":     constants.EVMID,
		"Quantum": constants.QuantumVMID,
		"DexVM":   constants.DexVMID,
	} {
		t.Run(name, func(t *testing.T) {
			tx, lookupErr := VMGenesis(pOnlyBytes, vmID)
			require.Nil(tx, "P-only genesis must not surface a tx for %s", name)
			require.Error(lookupErr, "P-only genesis must report %s as missing", name)
		})
	}
}
