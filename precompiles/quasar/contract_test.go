// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"testing"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/consensus/quasar"
	"github.com/stretchr/testify/require"
)

func newTestQuasar(t *testing.T) *quasar.Quasar {
	logger := log.NewNoOpLogger()
	q, err := quasar.NewQuasar(logger, 3, 2, 3) // threshold=3, quorum 2/3
	require.NoError(t, err)
	return q
}

func TestQuasarPrecompile_NotConnected(t *testing.T) {
	p := NewQuasarPrecompile(nil)

	// All methods should return ErrQuasarNotConnected
	input := append(SelectorGetQuasarState[:], make([]byte, 0)...)
	_, err := p.Run(input)
	require.ErrorIs(t, err, ErrQuasarNotConnected)

	input = append(SelectorGetConsensusMetrics[:], make([]byte, 0)...)
	_, err = p.Run(input)
	require.ErrorIs(t, err, ErrQuasarNotConnected)

	input = append(SelectorGetRingtailStats[:], make([]byte, 0)...)
	_, err = p.Run(input)
	require.ErrorIs(t, err, ErrQuasarNotConnected)
}

func TestQuasarPrecompile_InvalidInput(t *testing.T) {
	q := newTestQuasar(t)
	p := NewQuasarPrecompile(q)

	// Too short input
	_, err := p.Run([]byte{0x01, 0x02})
	require.ErrorIs(t, err, ErrInvalidInput)

	// Unknown selector
	_, err = p.Run([]byte{0xff, 0xff, 0xff, 0xff})
	require.ErrorIs(t, err, ErrUnknownMethod)
}

func TestQuasarPrecompile_GetQuasarState(t *testing.T) {
	q := newTestQuasar(t)
	p := NewQuasarPrecompile(q)

	input := SelectorGetQuasarState[:]
	output, err := p.Run(input)
	require.NoError(t, err)
	require.Len(t, output, 128) // 4 x 32-byte words

	// Verify initial state (not running, no finalized blocks)
	// Word 2 (isRunning) should be 0
	require.Equal(t, byte(0), output[95])
	// Word 3 (finalizedBlocks) should be 0
	require.Equal(t, []byte{0, 0, 0, 0}, output[124:128])
}

func TestQuasarPrecompile_GetValidatorInfo(t *testing.T) {
	q := newTestQuasar(t)
	p := NewQuasarPrecompile(q)

	// Create input with nodeID padded to 32 bytes
	nodeID := ids.GenerateTestNodeID()
	input := make([]byte, 36)
	copy(input[:4], SelectorGetValidatorInfo[:])
	copy(input[16:36], nodeID[:]) // Last 20 bytes of 32-byte word

	output, err := p.Run(input)
	require.NoError(t, err)
	require.Len(t, output, 128)
}

func TestQuasarPrecompile_GetConsensusMetrics(t *testing.T) {
	q := newTestQuasar(t)
	p := NewQuasarPrecompile(q)

	input := SelectorGetConsensusMetrics[:]
	output, err := p.Run(input)
	require.NoError(t, err)
	require.Len(t, output, 160) // 5 x 32-byte words
}

func TestQuasarPrecompile_GetFinality_NotFound(t *testing.T) {
	q := newTestQuasar(t)
	p := NewQuasarPrecompile(q)

	// Query non-existent block
	blockID := ids.GenerateTestID()
	input := make([]byte, 36)
	copy(input[:4], SelectorGetFinality[:])
	copy(input[4:36], blockID[:])

	output, err := p.Run(input)
	require.NoError(t, err)
	require.Len(t, output, 160)
	// Word 0 (found) should be false
	require.Equal(t, byte(0), output[31])
}

func TestQuasarPrecompile_GetFinality_Found(t *testing.T) {
	q := newTestQuasar(t)
	p := NewQuasarPrecompile(q)

	// Add a finality record directly
	blockID := ids.GenerateTestID()
	finality := &quasar.QuantumFinality{
		BlockID:       blockID,
		PChainHeight:  100,
		QChainHeight:  50,
		SignerWeight:  8000,
		TotalWeight:   10000,
		BLSProof:      []byte("RT"), // Dummy BLS proof
		RingtailProof: []byte("RT-PROOF"),
		Timestamp:     time.Now(),
	}
	q.SetFinalized(blockID, finality)

	input := make([]byte, 36)
	copy(input[:4], SelectorGetFinality[:])
	copy(input[4:36], blockID[:])

	output, err := p.Run(input)
	require.NoError(t, err)
	require.Len(t, output, 160)
	// Word 0 (found) should be true
	require.Equal(t, byte(1), output[31])
}

func TestQuasarPrecompile_GetRingtailStats(t *testing.T) {
	q := newTestQuasar(t)
	p := NewQuasarPrecompile(q)

	input := SelectorGetRingtailStats[:]
	output, err := p.Run(input)
	require.NoError(t, err)
	require.Len(t, output, 128) // 4 x 32-byte words
}

func TestQuasarPrecompile_RequiredGas(t *testing.T) {
	p := NewQuasarPrecompile(nil)

	tests := []struct {
		selector [4]byte
		expected uint64
	}{
		{SelectorGetQuasarState, GetQuasarStateGas},
		{SelectorGetValidatorInfo, GetValidatorInfoGas},
		{SelectorGetConsensusMetrics, GetConsensusMetricsGas},
		{SelectorGetFinality, GetFinalityGas},
		{SelectorGetRingtailStats, GetRingtailStatsGas},
	}

	for _, tt := range tests {
		input := tt.selector[:]
		gas := p.RequiredGas(input)
		require.Equal(t, tt.expected, gas, "selector %x", tt.selector)
	}
}

func TestQuasarPrecompile_SetQuasar(t *testing.T) {
	p := NewQuasarPrecompile(nil)

	// Initially not connected
	_, err := p.Run(SelectorGetQuasarState[:])
	require.ErrorIs(t, err, ErrQuasarNotConnected)

	// Connect Quasar
	q := newTestQuasar(t)
	p.SetQuasar(q)

	// Now should work
	_, err = p.Run(SelectorGetQuasarState[:])
	require.NoError(t, err)
}
