// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build go1.18
// +build go1.18

package beam

import (
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/choices"
	"github.com/luxfi/node/v2/quasar/consensus/beam/poll"
	"github.com/luxfi/node/v2/utils/set"
)

// FuzzBlockParsing tests block parsing with random inputs
func FuzzBlockParsing(f *testing.F) {
	// Add seed corpus
	validBlock := &Block{
		PrntID:     ids.GenerateTestID(),
		Hght:       42,
		Tmstmp:     time.Now().Unix(),
		ProposerID: ids.GenerateTestNodeID(),
		TxList:     [][]byte{{1, 2, 3}},
		Certs: CertBundle{
			BLSAgg: [96]byte{1, 2, 3},
			RTCert: make([]byte, 3072),
		},
	}
	f.Add(validBlock.Bytes())

	// Add edge cases
	f.Add([]byte{})
	f.Add([]byte{0})
	f.Add(make([]byte, 100))
	f.Add(make([]byte, 10000))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Should not panic
		block, err := ParseBlock(data)
		if err != nil {
			// Error is expected for invalid data
			return
		}

		// If parsing succeeded, verify roundtrip
		bytes := block.Bytes()
		block2, err := ParseBlock(bytes)
		if err != nil {
			t.Errorf("Failed to parse serialized block: %v", err)
		}

		// Verify fields match
		if block.ID() != block2.ID() {
			t.Error("Block IDs don't match after roundtrip")
		}
	})
}

// FuzzShareCollection tests share collection with random inputs
func FuzzShareCollection(f *testing.F) {
	// Add seed corpus
	f.Add(uint64(1), []byte("share1"), 3)
	f.Add(uint64(100), []byte("share2"), 15)
	f.Add(uint64(^uint64(0)), []byte{0xFF}, 21)

	f.Fuzz(func(t *testing.T, height uint64, shareData []byte, threshold int) {
		if threshold < 1 || threshold > 100 {
			return // Skip invalid thresholds
		}

		// Create Quasar
		rtSK := make([]byte, 64)
		rtSK[0] = byte(height) // Deterministic for fuzzing

		config := QuasarConfig{
			Threshold: threshold,
		}

		quasar, err := NewQuasar(ids.GenerateTestNodeID(), rtSK, config)
		if err != nil {
			return
		}

		// Create valid share from fuzz data
		share := make([]byte, 430)
		copy(share, shareData)

		// Should not panic
		for i := 0; i < threshold; i++ {
			nodeID := ids.GenerateTestNodeID()
			nodeID[0] = byte(i) // Make unique
			quasar.OnRTShare(height, nodeID, share)
		}
	})
}

// FuzzBlockVerification tests block verification with random fields
func FuzzBlockVerification(f *testing.F) {
	// Add seed corpus
	f.Add(uint64(0), int64(0), false, false)
	f.Add(uint64(1), time.Now().Unix(), true, true)
	f.Add(uint64(100), time.Now().Add(-1*time.Hour).Unix(), true, false)

	f.Fuzz(func(t *testing.T, height uint64, timestamp int64, hasBLS bool, hasRT bool) {
		block := &Block{
			Hght:   height,
			Tmstmp: timestamp,
		}

		// Set parent based on height
		if height > 0 {
			block.PrntID = ids.GenerateTestID()
		}

		// Add certificates based on flags
		if hasBLS {
			block.Certs.BLSAgg = [96]byte{1, 2, 3}
		}
		if hasRT {
			block.Certs.RTCert = make([]byte, 3072)
		}

		// Should not panic
		err := block.Verify()
		_ = err // Error is expected for invalid blocks
	})
}

// FuzzStatusTransitions tests status transitions with random operations
func FuzzStatusTransitions(f *testing.F) {
	// Add seed corpus
	f.Add([]byte{0, 1, 0, 1})
	f.Add([]byte{1, 1, 0, 0})
	f.Add([]byte{0, 0, 1, 1})

	f.Fuzz(func(t *testing.T, operations []byte) {
		block := &Block{
			status: choices.Processing,
			Certs: CertBundle{
				BLSAgg: [96]byte{1}, // Has dual cert
				RTCert: make([]byte, 3072),
			},
		}

		for _, op := range operations {
			switch op % 4 {
			case 0: // Accept
				_ = block.Accept()
			case 1: // Reject
				_ = block.Reject()
			case 2: // Set quantum
				_ = block.SetQuantum()
			case 3: // Check status
				_ = block.Status()
			}
		}

		// Verify invariants
		status := block.Status()
		if status == choices.Quantum && !block.HasDualCert() {
			t.Error("Quantum status without dual cert")
		}
	})
}

// FuzzCertificateAggregation tests certificate aggregation
func FuzzCertificateAggregation(f *testing.F) {
	// Add seed corpus
	f.Add(1, []byte{1})
	f.Add(15, []byte{1, 2, 3})
	f.Add(21, []byte{0xFF})

	f.Fuzz(func(t *testing.T, numShares int, seed []byte) {
		if numShares < 0 || numShares > 100 {
			return
		}

		// Create shares based on fuzz input
		shares := make([][]byte, numShares)
		for i := 0; i < numShares; i++ {
			share := make([]byte, 430)
			if len(seed) > 0 {
				share[0] = seed[0] + byte(i)
			}
			shares[i] = share
		}

		// Mock aggregation
		mock := &MockRingtail{}
		cert, err := mock.Aggregate(shares)

		if numShares == 0 {
			if err == nil {
				t.Error("Expected error for zero shares")
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if len(cert) != 3072 {
				t.Errorf("Invalid cert size: %d", len(cert))
			}
		}
	})
}

// FuzzMessageParsing tests message parsing with random data
func FuzzMessageParsing(f *testing.F) {
	// Add seed corpus
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{255, 254, 253, 252})
	f.Add(make([]byte, 1000))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Create message from fuzz data
		if len(data) < 4 {
			return
		}

		msg := message{
			msgType:   messageType(data[0] % 5),
			requestID: uint32(data[1]),
			container: data[2:],
		}

		// Parse node ID
		if len(data) >= 36 {
			copy(msg.nodeID[:], data[4:36])
		}

		// Create engine
		engine, _ := createTestEngine(t)

		// Should not panic
		_ = engine.handleMessage(msg)
	})
}

// FuzzPolls tests poll operations with random inputs
func FuzzPolls(f *testing.F) {
	// Add seed corpus
	f.Add(uint32(1), 5, []byte{0, 1})
	f.Add(uint32(100), 21, []byte{1, 0})

	f.Fuzz(func(t *testing.T, requestID uint32, numVoters int, votePattern []byte) {
		if numVoters < 1 || numVoters > 100 {
			return
		}

		// Create poll set
		pollSet := poll.NewSet()

		// Create voters
		voters := set.NewSet[ids.NodeID](numVoters)
		for i := 0; i < numVoters; i++ {
			nodeID := ids.GenerateTestNodeID()
			nodeID[0] = byte(i)
			voters.Add(nodeID)
		}

		// Create poll
		p := poll.NewPoll(requestID, voters)
		err := pollSet.Add(requestID, p)
		if err != nil {
			return
		}

		// Vote based on pattern
		for i, nodeID := range voters.List() {
			if i < len(votePattern) {
				vote := poll.Vote{
					PreferredID: ids.GenerateTestID(),
					AcceptedID:  ids.GenerateTestID(),
				}
				if votePattern[i]%2 == 0 {
					vote.PreferredID = ids.Empty
				}
				_ = pollSet.Vote(requestID, nodeID, vote)
			}
		}

		// Check result if finished
		if p.Finished() {
			result, err := p.Result()
			if err != nil {
				t.Errorf("Failed to get result: %v", err)
			}
			_ = result
		}
	})
}