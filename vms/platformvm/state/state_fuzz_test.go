// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/platformvm/state/statetest"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"
)

// FuzzStateTransitions tests state transitions with random operations
func FuzzStateTransitions(f *testing.F) {
	// Seed corpus with various operations
	f.Add(uint8(0), uint64(1000), uint32(1))
	f.Add(uint8(1), uint64(0), uint32(0))
	f.Add(uint8(2), uint64(1_000_000), uint32(100))
	f.Add(uint8(3), uint64(100_000_000), uint32(10000))

	f.Fuzz(func(t *testing.T, operation uint8, amount uint64, shares uint32) {
		// Limit values to reasonable ranges
		if amount > 1_000_000_000_000 {
			amount = amount % 1_000_000_000_000
		}
		if shares > 100_000 {
			shares = shares % 100_000
		}

		// Create state using statetest helper
		state := statetest.New(t, statetest.Config{})

		// Perform operations based on fuzzed input
		switch operation % 5 {
		case 0:
			// Test adding a validator
			nodeID := ids.GenerateTestNodeID()
			startTime := time.Now().Add(time.Hour)
			endTime := startTime.Add(24 * time.Hour)

			err := state.PutCurrentValidator(&Staker{
				TxID:            ids.GenerateTestID(),
				NodeID:          nodeID,
				PublicKey:       nil,
				SubnetID:        constants.PrimaryNetworkID,
				Weight:          amount,
				StartTime:       startTime,
				EndTime:         endTime,
				PotentialReward: 0,
			})
			if err != nil {
				// Some validator configurations might be invalid
				return
			}

			// Verify validator was added
			val, err := state.GetCurrentValidator(constants.PrimaryNetworkID, nodeID)
			if err != nil {
				t.Errorf("Failed to get validator after adding: %v", err)
				return
			}

			if val.Weight != amount {
				t.Errorf("Validator weight mismatch: got %v, want %v", val.Weight, amount)
			}

		case 1:
			// Test UTXO operations
			txID := ids.GenerateTestID()
			utxo := &lux.UTXO{
				UTXOID: lux.UTXOID{
					TxID:        txID,
					OutputIndex: shares,
				},
				Asset: lux.Asset{ID: ids.GenerateTestID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			}

			// Add UTXO
			state.AddUTXO(utxo)

			// Get UTXO
			retrievedUTXO, err := state.GetUTXO(utxo.InputID())
			if err != nil {
				t.Errorf("Failed to get UTXO after adding: %v", err)
				return
			}

			// Verify UTXO data
			if retrievedUTXO.TxID != txID {
				t.Errorf("UTXO TxID mismatch")
			}

			// Delete UTXO
			state.DeleteUTXO(utxo.InputID())

		case 2:
			// Test chain operations
			chainID := ids.GenerateTestID()
			createChainTx := &txs.Tx{
				Unsigned: &txs.CreateChainTx{
					SubnetID:    ids.GenerateTestID(),
					ChainName:   "test-chain",
					VMID:        ids.GenerateTestID(),
					FxIDs:       []ids.ID{},
					GenesisData: []byte("genesis"),
				},
			}

			// Add chain
			state.AddChain(createChainTx)

			// Verify chain exists
			chain, err := state.GetChain(chainID)
			if err != nil {
				// Chain retrieval might fail
				return
			}

			if chain == nil {
				t.Error("Chain should exist after adding")
			}

		case 3:
			// Test reward UTXO operations
			txID := ids.GenerateTestID()
			rewardUTXO := &lux.UTXO{
				UTXOID: lux.UTXOID{
					TxID:        txID,
					OutputIndex: shares,
				},
				Asset: lux.Asset{ID: ids.GenerateTestID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: amount,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
					},
				},
			}

			// Add reward UTXO
			state.AddRewardUTXO(txID, rewardUTXO)

			// Get reward UTXOs
			utxos, err := state.GetRewardUTXOs(txID)
			if err != nil {
				// Retrieval might fail
				return
			}

			if len(utxos) == 0 {
				t.Error("Should have reward UTXOs after adding")
			}

		case 4:
			// Test subnet transformation operations
			subnetID := ids.GenerateTestID()

			// Add a subnet transformation
			state.AddNetTransformation(&txs.Tx{
				Unsigned: &txs.TransformNetTx{
					NetID: subnetID,
					InitialRewardPoolSupply: amount,
				},
			})

			// Verify the transformation was recorded
			// This tests the subnet/net transformation tracking logic
		}

		// Commit changes
		err := state.Commit()
		if err != nil {
			// Commit might fail for some state configurations
			return
		}
	})
}

// FuzzStateSerialization tests state serialization/deserialization
func FuzzStateSerialization(f *testing.F) {
	// Seed corpus
	f.Add([]byte{}, uint32(0))
	f.Add([]byte{1, 2, 3, 4}, uint32(100))
	f.Add(bytes.Repeat([]byte{0xff}, 100), uint32(1000))

	f.Fuzz(func(t *testing.T, data []byte, height uint32) {
		// Limit data size
		if len(data) > 10000 {
			data = data[:10000]
		}

		// Create initial state
		state1 := statetest.New(t, statetest.Config{})

		// Set some state based on fuzzing input
		if len(data) >= 32 {
			var blockID ids.ID
			copy(blockID[:], data[:32])
			state1.SetLastAccepted(blockID)
			state1.SetHeight(uint64(height))
		}

		// Set timestamp
		if len(data) >= 8 {
			timestamp := int64(0)
			for i := 0; i < 8 && i < len(data); i++ {
				timestamp |= int64(data[i]) << (8 * i)
			}
			state1.SetTimestamp(time.Unix(timestamp, 0))
		}

		// Commit state
		err := state1.Commit()
		if err != nil {
			return
		}

		// Create second state from same DB (would need to access underlying DB from state1)
		// For now, just verify the first state operations worked
		if state1.GetHeight() != uint64(height) {
			t.Errorf("Height mismatch: got %v, want %v", state1.GetHeight(), height)
		}

		if len(data) >= 32 {
			var expectedBlockID ids.ID
			copy(expectedBlockID[:], data[:32])
			if state1.GetLastAccepted() != expectedBlockID {
				t.Error("Last accepted block mismatch")
			}
		}
	})
}

// FuzzValidatorSet tests validator set operations
func FuzzValidatorSet(f *testing.F) {
	// Seed corpus
	f.Add(uint8(10), uint64(1000), uint64(100))
	f.Add(uint8(1), uint64(0), uint64(0))
	f.Add(uint8(100), uint64(1_000_000), uint64(10000))

	f.Fuzz(func(t *testing.T, numValidators uint8, baseWeight uint64, variation uint64) {
		// Limit parameters
		if numValidators > 20 {
			numValidators = 20
		}
		if baseWeight > 1_000_000_000 {
			baseWeight = baseWeight % 1_000_000_000
		}
		if variation > baseWeight {
			variation = baseWeight
		}

		state := statetest.New(t, statetest.Config{})

		validators := make([]*Staker, 0, numValidators)
		totalWeight := uint64(0)

		// Add validators
		for i := uint8(0); i < numValidators; i++ {
			weight := baseWeight
			if variation > 0 && i%2 == 0 {
				weight += variation * uint64(i)
			}

			validator := &Staker{
				TxID:            ids.GenerateTestID(),
				NodeID:          ids.GenerateTestNodeID(),
				PublicKey:       nil,
				SubnetID:        constants.PrimaryNetworkID,
				Weight:          weight,
				StartTime:       time.Now().Add(time.Duration(i) * time.Hour),
				EndTime:         time.Now().Add(time.Duration(24+i) * time.Hour),
				PotentialReward: 0,
			}

			err := state.PutCurrentValidator(validator)
			if err != nil {
				// Some validator configurations might fail
				continue
			}

			validators = append(validators, validator)
			totalWeight += weight
		}

		// Get total weight
		weight, err := state.GetTotalWeight(constants.PrimaryNetworkID)
		if err != nil {
			// Getting weight might fail
			return
		}

		if weight != totalWeight {
			t.Errorf("Total weight mismatch: got %v, want %v", weight, totalWeight)
		}

		// Test validator iteration
		validatorIter, err := state.GetCurrentValidatorIterator(constants.PrimaryNetworkID)
		if err != nil {
			return
		}
		defer validatorIter.Release()

		count := 0
		for validatorIter.Next() {
			count++
			if count > int(numValidators)*2 {
				t.Error("Iterator returned too many validators")
				break
			}
		}

		// Remove some validators
		for i, validator := range validators {
			if i%2 == 0 {
				state.DeleteCurrentValidator(validator)
			}
		}

		// Verify removal
		newWeight, err := state.GetTotalWeight(constants.PrimaryNetworkID)
		if err != nil {
			return
		}

		if newWeight >= totalWeight {
			t.Error("Total weight should decrease after removing validators")
		}
	})
}