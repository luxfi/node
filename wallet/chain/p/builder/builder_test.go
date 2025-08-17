// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/secp256k1fx"
)

func generateUTXOs(random *rand.Rand, assetID ids.ID, locktime uint64) []*lux.UTXO {
	utxos := make([]*lux.UTXO, random.Intn(10))
	for i := range utxos {
		var output lux.TransferableOut = &secp256k1fx.TransferOutput{
			Amt: random.Uint64(),
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  random.Uint64(),
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
		}
		if locktime != 0 {
			output = &stakeable.LockOut{
				Locktime:        locktime,
				TransferableOut: output,
			}
		}
		utxos[i] = &lux.UTXO{
			UTXOID: lux.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: random.Uint32(),
			},
			Asset: lux.Asset{
				ID: assetID,
			},
			Out: output,
		}
	}
	return utxos
}

func TestSplitByLocktime(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("Seed: %d", seed)
	random := rand.New(rand.NewSource(seed)) // #nosec G404

	var (
		require = require.New(t)

		unlockedTime     uint64 = 100
		expectedUnlocked        = slices.Concat(
			generateUTXOs(random, ids.GenerateTestID(), 0),
			generateUTXOs(random, ids.GenerateTestID(), unlockedTime-1),
			generateUTXOs(random, ids.GenerateTestID(), unlockedTime),
		)
		expectedLocked = slices.Concat(
			generateUTXOs(random, ids.GenerateTestID(), unlockedTime+100),
			generateUTXOs(random, ids.GenerateTestID(), unlockedTime+1),
		)
		utxos = slices.Concat(
			expectedUnlocked,
			expectedLocked,
		)
	)
	random.Shuffle(len(utxos), func(i, j int) {
		utxos[i], utxos[j] = utxos[j], utxos[i]
	})

	utxosByLocktime := splitByLocktime(utxos, unlockedTime)
	require.ElementsMatch(expectedUnlocked, utxosByLocktime.unlocked)
	require.ElementsMatch(expectedLocked, utxosByLocktime.locked)
}

func TestByAssetID(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("Seed: %d", seed)
	random := rand.New(rand.NewSource(seed)) // #nosec G404

	var (
		require = require.New(t)

		assetID           = ids.GenerateTestID()
		expectedRequested = generateUTXOs(random, assetID, random.Uint64())
		expectedOther     = generateUTXOs(random, ids.GenerateTestID(), random.Uint64())
		utxos             = slices.Concat(
			expectedRequested,
			expectedOther,
		)
	)
	random.Shuffle(len(utxos), func(i, j int) {
		utxos[i], utxos[j] = utxos[j], utxos[i]
	})

	utxosByAssetID := splitByAssetID(utxos, assetID)
	require.ElementsMatch(expectedRequested, utxosByAssetID.requested)
	require.ElementsMatch(expectedOther, utxosByAssetID.other)
}

func TestUnwrapOutput(t *testing.T) {
	normalOutput := &secp256k1fx.TransferOutput{
		Amt: 123,
		OutputOwners: secp256k1fx.OutputOwners{
			Locktime:  456,
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.ShortEmpty},
		},
	}

	tests := []struct {
		name             string
		output           verify.State
		expectedOutput   *secp256k1fx.TransferOutput
		expectedLocktime uint64
		expectedErr      error
	}{
		{
			name:             "normal output",
			output:           normalOutput,
			expectedOutput:   normalOutput,
			expectedLocktime: 0,
			expectedErr:      nil,
		},
		{
			name: "locked output",
			output: &stakeable.LockOut{
				Locktime:        789,
				TransferableOut: normalOutput,
			},
			expectedOutput:   normalOutput,
			expectedLocktime: 789,
			expectedErr:      nil,
		},
		{
			name: "locked output with no locktime",
			output: &stakeable.LockOut{
				Locktime:        0,
				TransferableOut: normalOutput,
			},
			expectedOutput:   normalOutput,
			expectedLocktime: 0,
			expectedErr:      nil,
		},
		{
			name:             "invalid output",
			output:           nil,
			expectedOutput:   nil,
			expectedLocktime: 0,
			expectedErr:      ErrUnknownOutputType,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			output, locktime, err := unwrapOutput(test.output)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expectedOutput, output)
			require.Equal(test.expectedLocktime, locktime)
		})
	}
}

// Helper types and functions for tests

type utxosByLocktime struct {
	unlocked []*lux.UTXO
	locked   []*lux.UTXO
}

func splitByLocktime(utxos []*lux.UTXO, unlockedTime uint64) utxosByLocktime {
	result := utxosByLocktime{}
	for _, utxo := range utxos {
		if out, ok := utxo.Out.(*secp256k1fx.TransferOutput); ok {
			if out.Locktime < unlockedTime {
				result.unlocked = append(result.unlocked, utxo)
			} else {
				result.locked = append(result.locked, utxo)
			}
		} else if stakeableOut, ok := utxo.Out.(*stakeable.LockOut); ok {
			if innerOut, ok := stakeableOut.TransferableOut.(*secp256k1fx.TransferOutput); ok {
				if innerOut.Locktime < unlockedTime || stakeableOut.Locktime < unlockedTime {
					result.unlocked = append(result.unlocked, utxo)
				} else {
					result.locked = append(result.locked, utxo)
				}
			}
		}
	}
	return result
}

type utxosByAssetID struct {
	requested []*lux.UTXO
	other     []*lux.UTXO
}

func splitByAssetID(utxos []*lux.UTXO, assetID ids.ID) utxosByAssetID {
	result := utxosByAssetID{}
	for _, utxo := range utxos {
		if utxo.AssetID() == assetID {
			result.requested = append(result.requested, utxo)
		} else {
			result.other = append(result.other, utxo)
		}
	}
	return result
}

func unwrapOutput(output lux.TransferableOut) (lux.TransferableOut, uint64, error) {
	if stakeableOut, ok := output.(*stakeable.LockOut); ok {
		return stakeableOut.TransferableOut, stakeableOut.Locktime, nil
	}
	if transferOut, ok := output.(*secp256k1fx.TransferOutput); ok {
		return transferOut, transferOut.Locktime, nil
	}
	return output, 0, nil
}
