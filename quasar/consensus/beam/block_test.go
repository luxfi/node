// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/choices"
	"github.com/stretchr/testify/require"
)

// TestBlockCreation tests block creation
func TestBlockCreation(t *testing.T) {
	require := require.New(t)

	parentID := ids.GenerateTestID()
	height := uint64(42)
	timestamp := time.Now().Unix()
	proposerID := ids.GenerateTestNodeID()
	txs := [][]byte{{1, 2, 3}, {4, 5, 6}}

	block := BuildBlock(parentID, height, timestamp, proposerID, txs)

	// Verify fields
	require.Equal(parentID, block.Parent())
	require.Equal(height, block.Height())
	require.Equal(timestamp, block.Timestamp())
	require.Equal(proposerID, block.ProposerID)
	require.Equal(txs, block.TxList)
	require.Equal(choices.Processing, block.Status())

	// Verify ID is deterministic
	id1 := block.ID()
	id2 := block.ID()
	require.Equal(id1, id2)
}

// TestBlockVerification tests block verification
func TestBlockVerification(t *testing.T) {
	tests := []struct {
		name    string
		block   *Block
		wantErr bool
	}{
		{
			name: "valid genesis block",
			block: &Block{
				PrntID: ids.Empty,
				Hght:   0,
				Tmstmp: time.Now().Unix(),
			},
			wantErr: false,
		},
		{
			name: "future timestamp",
			block: &Block{
				PrntID: ids.GenerateTestID(),
				Hght:   1,
				Tmstmp: time.Now().Add(1 * time.Hour).Unix(),
			},
			wantErr: true,
		},
		{
			name: "genesis with parent",
			block: &Block{
				PrntID: ids.GenerateTestID(),
				Hght:   0,
				Tmstmp: time.Now().Unix(),
			},
			wantErr: true,
		},
		{
			name: "missing dual cert",
			block: &Block{
				PrntID: ids.GenerateTestID(),
				Hght:   1,
				Tmstmp: time.Now().Unix(),
			},
			wantErr: true,
		},
		{
			name: "valid with dual cert",
			block: &Block{
				PrntID: ids.GenerateTestID(),
				Hght:   1,
				Tmstmp: time.Now().Unix(),
				Certs: CertBundle{
					BLSAgg: [96]byte{1, 2, 3}, // Mock BLS
					RTCert: make([]byte, 3072), // Mock RT
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.block.Verify()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestBlockSerialization tests block serialization
func TestBlockSerialization(t *testing.T) {
	require := require.New(t)

	// Create block
	original := &Block{
		PrntID:     ids.GenerateTestID(),
		Hght:       42,
		Tmstmp:     time.Now().Unix(),
		ProposerID: ids.GenerateTestNodeID(),
		TxList:     [][]byte{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}},
		Certs: CertBundle{
			BLSAgg: [96]byte{1, 2, 3},
			RTCert: make([]byte, 3072),
		},
	}

	// Serialize
	bytes := original.Bytes()
	require.NotEmpty(bytes)

	// Parse
	parsed, err := ParseBlock(bytes)
	require.NoError(err)

	// Compare
	require.Equal(original.PrntID, parsed.PrntID)
	require.Equal(original.Hght, parsed.Hght)
	require.Equal(original.Tmstmp, parsed.Tmstmp)
	require.Equal(original.ProposerID, parsed.ProposerID)
	require.Equal(original.TxList, parsed.TxList)
	require.Equal(original.Certs.BLSAgg, parsed.Certs.BLSAgg)
	require.Equal(original.Certs.RTCert, parsed.Certs.RTCert)
	require.Equal(original.ID(), parsed.ID())
}

// TestBlockStatus tests block status transitions
func TestBlockStatus(t *testing.T) {
	require := require.New(t)

	block := &Block{status: choices.Processing}

	// Accept block
	err := block.Accept()
	require.NoError(err)
	require.Equal(choices.Accepted, block.Status())

	// Try to reject accepted block
	err = block.Reject()
	require.Error(err)

	// Create new block for rejection test
	block2 := &Block{status: choices.Processing}
	err = block2.Reject()
	require.NoError(err)
	require.Equal(choices.Rejected, block2.Status())

	// Try to accept rejected block
	err = block2.Accept()
	require.Error(err)
}

// TestQuantumStatus tests quantum status
func TestQuantumStatus(t *testing.T) {
	require := require.New(t)

	// Block without dual cert
	block := &Block{
		status: choices.Accepted,
	}
	err := block.SetQuantum()
	require.Error(err)

	// Block with dual cert but not accepted
	block2 := &Block{
		status: choices.Processing,
		Certs: CertBundle{
			BLSAgg: [96]byte{1, 2, 3},
			RTCert: make([]byte, 3072),
		},
	}
	err = block2.SetQuantum()
	require.Error(err)

	// Valid quantum transition
	block3 := &Block{
		status: choices.Accepted,
		Certs: CertBundle{
			BLSAgg: [96]byte{1, 2, 3},
			RTCert: make([]byte, 3072),
		},
	}
	err = block3.SetQuantum()
	require.NoError(err)
	require.Equal(choices.Quantum, block3.Status())
	require.True(block3.Status().IsQuantum())
}

// TestAttachCertificates tests certificate attachment
func TestAttachCertificates(t *testing.T) {
	require := require.New(t)

	block := BuildBlock(
		ids.GenerateTestID(),
		1,
		time.Now().Unix(),
		ids.GenerateTestNodeID(),
		[][]byte{{1, 2, 3}},
	)

	// Initial state
	require.False(block.HasDualCert())
	id1 := block.ID()

	// Attach certificates
	blsAgg := [96]byte{1, 2, 3}
	rtCert := make([]byte, 3072)
	err := block.AttachCertificates(blsAgg, rtCert)
	require.NoError(err)

	// Verify certificates
	require.True(block.HasDualCert())
	require.Equal(blsAgg[:], block.BLSSignature())
	require.Equal(rtCert, block.RTCertificate())

	// ID should change after attaching certs
	id2 := block.ID()
	require.NotEqual(id1, id2)

	// Try to attach again
	err = block.AttachCertificates(blsAgg, rtCert)
	require.Error(err)
}

// BenchmarkBlockSerialization benchmarks block serialization
func BenchmarkBlockSerialization(b *testing.B) {
	block := &Block{
		PrntID:     ids.GenerateTestID(),
		Hght:       42,
		Tmstmp:     time.Now().Unix(),
		ProposerID: ids.GenerateTestNodeID(),
		TxList:     make([][]byte, 100),
		Certs: CertBundle{
			BLSAgg: [96]byte{1, 2, 3},
			RTCert: make([]byte, 3072),
		},
	}

	// Fill transactions
	for i := 0; i < 100; i++ {
		block.TxList[i] = make([]byte, 250)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = block.Bytes()
	}
}

// BenchmarkBlockParsing benchmarks block parsing
func BenchmarkBlockParsing(b *testing.B) {
	block := &Block{
		PrntID:     ids.GenerateTestID(),
		Hght:       42,
		Tmstmp:     time.Now().Unix(),
		ProposerID: ids.GenerateTestNodeID(),
		TxList:     make([][]byte, 100),
		Certs: CertBundle{
			BLSAgg: [96]byte{1, 2, 3},
			RTCert: make([]byte, 3072),
		},
	}

	// Fill transactions
	for i := 0; i < 100; i++ {
		block.TxList[i] = make([]byte, 250)
	}

	bytes := block.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseBlock(bytes)
	}
}