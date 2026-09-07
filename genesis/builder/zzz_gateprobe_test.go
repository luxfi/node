// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// zzz_gateprobe_test.go is a BLUE-team gate instrument for the
// mpcvm-decomplect surgery (LP-134 / LP-7050). NOT a behavioral
// assertion — it emits a deterministic per-chain digest of the fully
// built P-Chain genesis for every network so before/after can be diffed
// to PROVE which chains changed and which stayed byte-identical.
// zzz_ prefix keeps it last. Delete after RED sign-off.
package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"testing"

	"github.com/luxfi/node/vms/platformvm/genesis"
	pchaintxs "github.com/luxfi/node/vms/platformvm/txs"
)

func TestZZZ_GateProbe_PerChainDigest(t *testing.T) {
	nets := []struct {
		name string
		id   uint32
	}{
		{"mainnet", 1},
		{"testnet", 2},
		{"devnet", 3},
		{"localnet", 1337},
	}

	for _, n := range nets {
		cfg, err := GetConfig(n.id)
		if err != nil {
			t.Fatalf("%s GetConfig: %v", n.name, err)
		}
		gb, rootID, err := FromConfig(cfg)
		if err != nil {
			t.Fatalf("%s FromConfig: %v", n.name, err)
		}
		gen, err := genesis.Parse(gb)
		if err != nil {
			t.Fatalf("%s Parse: %v", n.name, err)
		}

		type row struct {
			name    string
			vmid    string
			dataSum string
			chainID string
		}
		rows := make([]row, 0, len(gen.Chains))
		for _, c := range gen.Chains {
			u := c.Unsigned.(*pchaintxs.CreateChainTx)
			sum := sha256.Sum256(u.GenesisData())
			rows = append(rows, row{
				name:    u.BlockchainName(),
				vmid:    u.VMID().String(),
				dataSum: hex.EncodeToString(sum[:]),
				chainID: c.ID().String(),
			})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

		fmt.Printf("=== NET %s (id=%d) P-ROOT=%s ===\n", n.name, n.id, rootID.String())
		for _, r := range rows {
			fmt.Printf("  CHAIN name=%-10s vmid=%-52s dataSHA256=%s chainTxID=%s\n",
				r.name, r.vmid, r.dataSum, r.chainID)
		}
	}
}
