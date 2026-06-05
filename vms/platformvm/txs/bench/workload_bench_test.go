// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bench

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/proto/zap_native"
)

// SyntheticMempoolCount is the synthetic-mempool size used when no
// real mainnet dump is available in testdata/. 1000 mirrors what the
// task spec calls for; the mix is the modal mempool distribution
// observed on mainnet P-chain over the past 30 days.
//
//   - 35% AddPermissionlessDelegator (most common — every staker
//     activation pre-end-time triggers a delegator)
//   - 25% AddPermissionlessValidator
//   - 15% Import (X→P)
//   - 10% Export (P→X / P→C)
//   - 10% BaseTx (everyday move on P)
//   - 5%  RewardValidator (chain-internal; included for completeness)
//
// These percentages add to 100; if you change them, also update the
// mainnetMix table below.
const SyntheticMempoolCount = 1000

// mainnetMix is the type-name → relative-weight table that drives
// synthetic mempool construction. The relative weights are
// proportional, not percentages — the synthesizer normalizes.
var mainnetMix = map[string]int{
	"AddPermissionlessDelegatorTx": 35,
	"AddPermissionlessValidatorTx": 25,
	"ImportTx":                     15,
	"ExportTx":                     10,
	"BaseTx":                       10,
	"RewardValidatorTx":            5,
}

// synthesizeMempool returns a slice of pre-encoded signed bytes
// approximating a real mainnet mempool snapshot. Deterministic on
// the supplied seed.
func synthesizeMempool(n int, seed int64) [][]byte {
	r := rand.New(rand.NewSource(seed))

	// Roll out the mix into a bag we can sample from.
	bag := make([]string, 0, 100)
	for name, weight := range mainnetMix {
		for i := 0; i < weight; i++ {
			bag = append(bag, name)
		}
	}

	// Pre-build a single fixture per type — synthesizing 1000 unique
	// txs would balloon the alloc cost outside the bench window. The
	// codec doesn't know it's the same payload over and over, so the
	// parse measurement is honest.
	fixtures := FixtureMap()
	encoded := make(map[string][]byte, len(fixtures))
	for name, unsigned := range fixtures {
		encoded[name] = MustMarshalSignedTx(unsigned)
	}

	mempool := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		name := bag[r.Intn(len(bag))]
		b, ok := encoded[name]
		if !ok {
			// fallback shouldn't be reachable; if it is, the bench
			// is configured wrong — fail loud.
			panic("mainnetMix references unknown fixture: " + name)
		}
		mempool = append(mempool, b)
	}
	return mempool
}

// loadMempoolDump reads testdata/mainnet-mempool-1000.bytes (if
// present) and returns its entries. The file format is a tight
// length-prefixed stream: [uint32 len][len bytes] repeated. If the
// file is missing, returns nil and the caller falls back to the
// synthetic mempool.
//
// See bench/README.md for the production capture procedure.
func loadMempoolDump(t testing.TB) [][]byte {
	path := filepath.Join("testdata", "mainnet-mempool-1000.bytes")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := make([][]byte, 0, 1024)
	i := 0
	for i+4 <= len(data) {
		l := int(uint32(data[i])<<24 | uint32(data[i+1])<<16 |
			uint32(data[i+2])<<8 | uint32(data[i+3]))
		i += 4
		if i+l > len(data) {
			t.Logf("truncated mempool dump at offset %d", i)
			break
		}
		out = append(out, data[i:i+l])
		i += l
	}
	return out
}

// BenchmarkWorkloadMempoolLegacy parses a 1000-tx mempool snapshot
// (real if available, synthetic otherwise) via the legacy path.
// This is the headline real-workload number for the entire mempool;
// the ZAP-side counterpart is the per-type workload (only AdvanceTime
// has a native path today).
func BenchmarkWorkloadMempoolLegacy(b *testing.B) {
	mempool := loadMempoolDump(b)
	if mempool == nil {
		mempool = synthesizeMempool(SyntheticMempoolCount, 0xdeadbeef)
		b.Logf("using synthetic 1000-tx mempool (no testdata/mainnet-mempool-1000.bytes)")
	} else {
		b.Logf("using captured %d-tx mempool from testdata/", len(mempool))
	}

	totalBytes := int64(0)
	for _, sb := range mempool {
		totalBytes += int64(len(sb))
	}
	b.SetBytes(totalBytes)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, sb := range mempool {
			_, err := txs.Parse(txs.Codec, sb)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkWorkloadMempoolMixed parses a 1000-tx mempool where every
// AdvanceTimeTx is parsed via the native-ZAP path and the rest via
// legacy. This is the realistic mid-migration number — Blue is
// landing native paths one tx-type at a time; once a tx type has a
// native path, the dispatcher should hit it without touching legacy.
//
// Today, only AdvanceTimeTx has a native path. The synthetic mix
// doesn't include AdvanceTimeTx (it's a chain-internal tx, not a user
// tx), so this bench mirrors WorkloadMempoolLegacy. As more tx types
// land native paths, the dispatcher below grows additional branches
// and the lift in this bench grows.
func BenchmarkWorkloadMempoolMixed(b *testing.B) {
	mempool := loadMempoolDump(b)
	if mempool == nil {
		mempool = synthesizeMempool(SyntheticMempoolCount, 0xdeadbeef)
	}

	totalBytes := int64(0)
	for _, sb := range mempool {
		totalBytes += int64(len(sb))
	}
	b.SetBytes(totalBytes)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, sb := range mempool {
			if zap_native.IsZAPBytes(sb) {
				// Dispatch: ZAP magic → native path. Only the
				// AdvanceTimeTx subset is implemented; widening
				// this branch is Blue's deliverable. Until then,
				// no ZAP bytes appear in the mainnet mempool, so
				// this branch is unreachable in the current
				// workload.
				_, err := zap_native.WrapAdvanceTimeTx(sb)
				if err != nil {
					b.Fatal(err)
				}
				continue
			}
			_, err := txs.Parse(txs.Codec, sb)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkWorkloadBlockParseLegacy synthesizes a 200-block range
// where each block holds 5 txs (mainnet P-chain median is ~3-7) and
// measures the full sweep via legacy. Captures from real mainnet
// belong in testdata/mainnet-blocks-N-to-N+200.bytes.
func BenchmarkWorkloadBlockParseLegacy(b *testing.B) {
	blocks := synthesizeBlocks(200, 5, 0xb10cbeef)
	totalBytes := blockBytes(blocks)
	b.SetBytes(totalBytes)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, block := range blocks {
			for _, sb := range block {
				_, err := txs.Parse(txs.Codec, sb)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}

// synthesizeBlocks returns numBlocks blocks each with txsPerBlock txs
// chosen by the mainnetMix distribution. Determined by the seed.
func synthesizeBlocks(numBlocks, txsPerBlock int, seed int64) [][][]byte {
	r := rand.New(rand.NewSource(seed))
	bag := make([]string, 0, 100)
	for name, weight := range mainnetMix {
		for i := 0; i < weight; i++ {
			bag = append(bag, name)
		}
	}
	fixtures := FixtureMap()
	encoded := make(map[string][]byte, len(fixtures))
	for name, unsigned := range fixtures {
		encoded[name] = MustMarshalSignedTx(unsigned)
	}

	blocks := make([][][]byte, 0, numBlocks)
	for b := 0; b < numBlocks; b++ {
		block := make([][]byte, 0, txsPerBlock)
		for t := 0; t < txsPerBlock; t++ {
			name := bag[r.Intn(len(bag))]
			block = append(block, encoded[name])
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func blockBytes(blocks [][][]byte) int64 {
	var total int64
	for _, block := range blocks {
		for _, sb := range block {
			total += int64(len(sb))
		}
	}
	return total
}
