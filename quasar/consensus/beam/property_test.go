// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	cryptoRand "crypto/rand"
	"math/rand"
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/choices"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/assert"
)

// Property: Block ID should be deterministic
func TestPropertyBlockIDDeterministic(t *testing.T) {
	for i := 0; i < 100; i++ {
		// Generate random block data
		parentID := ids.GenerateTestID()
		height := uint64(rand.Int63n(1000))
		timestamp := time.Now().Unix() - int64(rand.Int63n(3600))
		proposerID := ids.GenerateTestNodeID()

		// Create transactions
		numTxs := rand.Intn(10)
		txs := make([][]byte, numTxs)
		for j := 0; j < numTxs; j++ {
			tx := make([]byte, rand.Intn(500)+50)
			cryptoRand.Read(tx)
			txs[j] = tx
		}

		// Create block
		block1 := BuildBlock(parentID, height, timestamp, proposerID, txs)
		block2 := BuildBlock(parentID, height, timestamp, proposerID, txs)

		// Property: Same inputs produce same ID
		assert.Equal(t, block1.ID(), block2.ID())
	}
}

// Property: Serialization should be reversible
func TestPropertySerializationReversible(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 100; i++ {
		// Create random block
		original := &Block{
			PrntID:     ids.GenerateTestID(),
			Hght:       uint64(rand.Int63n(10000)),
			Tmstmp:     time.Now().Unix() - int64(rand.Int63n(86400)),
			ProposerID: ids.GenerateTestNodeID(),
		}

		// Random transactions
		numTxs := rand.Intn(50)
		original.TxList = make([][]byte, numTxs)
		for j := 0; j < numTxs; j++ {
			tx := make([]byte, rand.Intn(1000)+10)
			cryptoRand.Read(tx)
			original.TxList[j] = tx
		}

		// Random certificates
		if rand.Float32() > 0.5 {
			rand.Read(original.Certs.BLSAgg[:])
			original.Certs.RTCert = make([]byte, 3072)
			rand.Read(original.Certs.RTCert)
		}

		// Serialize and parse
		bytes := original.Bytes()
		parsed, err := ParseBlock(bytes)
		require.NoError(err)

		// Property: Parsed block equals original
		require.Equal(original.ID(), parsed.ID())
		require.Equal(original.PrntID, parsed.PrntID)
		require.Equal(original.Hght, parsed.Hght)
		require.Equal(original.Tmstmp, parsed.Tmstmp)
		require.Equal(original.ProposerID, parsed.ProposerID)
		require.Equal(original.TxList, parsed.TxList)
		require.Equal(original.Certs, parsed.Certs)
	}
}

// Property: Status transitions should follow state machine rules
func TestPropertyStatusTransitions(t *testing.T) {
	transitions := []struct {
		from   choices.Status
		action string
		to     choices.Status
		valid  bool
	}{
		// From Processing
		{choices.Processing, "accept", choices.Accepted, true},
		{choices.Processing, "reject", choices.Rejected, true},
		{choices.Processing, "quantum", choices.Quantum, false}, // Need accepted first

		// From Accepted
		{choices.Accepted, "accept", choices.Accepted, true}, // Idempotent
		{choices.Accepted, "reject", choices.Rejected, false},
		{choices.Accepted, "quantum", choices.Quantum, true}, // With dual cert

		// From Rejected
		{choices.Rejected, "accept", choices.Accepted, false},
		{choices.Rejected, "reject", choices.Rejected, true}, // Idempotent
		{choices.Rejected, "quantum", choices.Quantum, false},

		// From Quantum
		{choices.Quantum, "accept", choices.Accepted, true}, // Already accepted
		{choices.Quantum, "reject", choices.Rejected, false},
		{choices.Quantum, "quantum", choices.Quantum, true}, // Idempotent
	}

	for _, tt := range transitions {
		t.Run(tt.from.String()+"_"+tt.action, func(t *testing.T) {
			block := &Block{
				status: tt.from,
				Certs: CertBundle{
					BLSAgg: [96]byte{1}, // Has dual cert
					RTCert: make([]byte, 3072),
				},
			}

			var err error
			switch tt.action {
			case "accept":
				err = block.Accept()
			case "reject":
				err = block.Reject()
			case "quantum":
				err = block.SetQuantum()
			}

			if tt.valid {
				assert.Equal(t, err, nil)
				if tt.to != tt.from { // Not idempotent
					assert.Equal(t, block.Status(), tt.to)
				}
			} else {
				assert.NotEqual(t, err, nil)
				assert.Equal(t, block.Status(), tt.from) // No change
			}
		})
	}
}

// Property: Dual certificates should be required for quantum status
func TestPropertyQuantumRequiresDualCert(t *testing.T) {
	tests := []struct {
		name     string
		hasBLS   bool
		hasRT    bool
		canQuant bool
	}{
		{"no certs", false, false, false},
		{"BLS only", true, false, false},
		{"RT only", false, true, false},
		{"dual cert", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := &Block{status: choices.Accepted}

			if tt.hasBLS {
				rand.Read(block.Certs.BLSAgg[:])
			}
			if tt.hasRT {
				block.Certs.RTCert = make([]byte, 3072)
				rand.Read(block.Certs.RTCert)
			}

			err := block.SetQuantum()

			if tt.canQuant {
				assert.Equal(t, err, nil)
				assert.Equal(t, block.Status(), choices.Quantum)
			} else {
				assert.NotEqual(t, err, nil)
				assert.Equal(t, block.Status(), choices.Accepted)
			}
		})
	}
}

// Property: Share aggregation should be consistent
func TestPropertyShareAggregation(t *testing.T) {
	mock := &MockRingtail{}

	// Property: Aggregating same shares produces same result
	for i := 0; i < 50; i++ {
		numShares := rand.Intn(20) + 1
		shares := make([][]byte, numShares)

		for j := 0; j < numShares; j++ {
			share := make([]byte, 430)
			rand.Read(share)
			shares[j] = share
		}

		// Aggregate twice
		cert1, err1 := mock.Aggregate(shares)
		cert2, err2 := mock.Aggregate(shares)

		assert.Equal(t, err1, err2)
		if err1 == nil {
			assert.Equal(t, cert1, cert2)
		}
	}

	// Property: Order shouldn't matter (in mock)
	for i := 0; i < 50; i++ {
		numShares := rand.Intn(10) + 2
		shares := make([][]byte, numShares)

		for j := 0; j < numShares; j++ {
			share := make([]byte, 430)
			share[0] = byte(j)
			shares[j] = share
		}

		// Reverse order
		reversed := make([][]byte, numShares)
		for j := 0; j < numShares; j++ {
			reversed[j] = shares[numShares-1-j]
		}

		cert1, _ := mock.Aggregate(shares)
		cert2, _ := mock.Aggregate(reversed)

		// In real implementation order might matter,
		// but in mock it shouldn't
		assert.Equal(t, len(cert1), len(cert2))
	}
}

// Property: Precomputation pool should maintain capacity
func TestPropertyPrecompPoolCapacity(t *testing.T) {
	for capacity := 1; capacity <= 20; capacity++ {
		pool := NewPrecompPool(capacity)
		sk := make([]byte, 64)
		rand.Read(sk)

		go pool.Start(sk)
		time.Sleep(time.Duration(capacity*150) * time.Millisecond)

		// Drain pool
		count := 0
		for pool.Get() != nil {
			count++
			if count > capacity {
				break
			}
		}

		// Property: Pool size should not exceed capacity
		assert.Equal(t, count <= capacity, true)
	}
}

// Property: Concurrent operations should be safe
func TestPropertyConcurrentSafety(t *testing.T) {
	for i := 0; i < 10; i++ {
		// Create Quasar
		rtSK := make([]byte, 64)
		rand.Read(rtSK)

		config := QuasarConfig{
			Threshold: rand.Intn(10) + 5,
		}

		quasar, err := NewQuasar(ids.GenerateTestNodeID(), rtSK, config)
		require.NoError(t, err)

		// Concurrent operations
		done := make(chan bool)
		numGoroutines := 10

		for j := 0; j < numGoroutines; j++ {
			go func(idx int) {
				for k := 0; k < 100; k++ {
					height := uint64(rand.Int63n(100))
					nodeID := ids.GenerateTestNodeID()
					share := make([]byte, 430)
					rand.Read(share)

					// Random operation
					switch rand.Intn(3) {
					case 0:
						quasar.OnRTShare(height, nodeID, share)
					case 1:
						ch := make(chan []byte, 1)
						quasar.RegisterForCertificate(height, ch)
					case 2:
						msg := make([]byte, 32)
						rand.Read(msg)
						_, _ = quasar.QuickSign(msg)
					}
				}
				done <- true
			}(j)
		}

		// Wait for completion
		for j := 0; j < numGoroutines; j++ {
			<-done
		}

		// Property: No panics or deadlocks
		t.Log("Concurrent test", i, "completed successfully")
	}
}