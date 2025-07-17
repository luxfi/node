// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils/set"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/secp256k1fx"
)

// getAllUTXOsBenchmark is a helper func to benchmark the GetAllUTXOs depending on the size
func getAllUTXOsBenchmark(b *testing.B, utxoCount int, randSrc rand.Source) {
	require := require.New(b)

	env := setup(b, &envConfig{fork: upgradetest.Latest})
	defer env.vm.ctx.Lock.Unlock()

	addr := ids.GenerateTestShortID()

	for i := 0; i < utxoCount; i++ {
		utxo := &lux.UTXO{
			UTXOID: lux.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: uint32(randSrc.Int63()),
			},
			Asset: lux.Asset{ID: env.vm.ctx.LUXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 100000,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Addrs:     []ids.ShortID{addr},
					Threshold: 1,
				},
			},
		}

		env.vm.state.AddUTXO(utxo)
	}
	require.NoError(env.vm.state.Commit())

	addrsSet := set.Of(addr)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Fetch all UTXOs older version
		notPaginatedUTXOs, err := lux.GetAllUTXOs(env.vm.state, addrsSet)
		require.NoError(err)
		require.Len(notPaginatedUTXOs, utxoCount)
	}
}

func BenchmarkGetUTXOs(b *testing.B) {
	tests := []struct {
		name      string
		utxoCount int
	}{
		{
			name:      "100",
			utxoCount: 100,
		},
		{
			name:      "10k",
			utxoCount: 10_000,
		},
		{
			name:      "100k",
			utxoCount: 100_000,
		},
	}

	for testIdx, count := range tests {
		randSrc := rand.NewSource(int64(testIdx))
		b.Run(count.name, func(b *testing.B) {
			getAllUTXOsBenchmark(b, count.utxoCount, randSrc)
		})
	}
}
