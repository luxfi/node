// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package quasar provides EVM precompile access to Quasar consensus state.
// This precompile exposes the hybrid BLS+Ringtail quantum finality mechanism
// to smart contracts via the C-Chain.
//
// Precompile Address: 0x0200000000000000000000000000000000000020
//
// Methods:
//   - getQuasarState() returns (pChainHeight, qChainHeight, isRunning, finalizedBlocks)
//   - getValidatorInfo(nodeID) returns (weight, active, blsPubKey, ringtailKey)
//   - getConsensusMetrics() returns (avgBLSLatency, avgRingtailLatency, totalFinalized)
//   - getFinality(blockID) returns (blockID, pHeight, qHeight, blsProof, ringtailProof)
//   - getRingtailStats() returns (numParties, threshold, sessionID, initialized)
package quasar

import (
	"encoding/binary"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/quasar"
)

var (
	// ErrQuasarNotConnected is returned when Quasar consensus is not connected
	ErrQuasarNotConnected = errors.New("quasar consensus not connected")
	// ErrInvalidInput is returned for malformed precompile input
	ErrInvalidInput = errors.New("invalid precompile input")
	// ErrUnknownMethod is returned for unrecognized function selector
	ErrUnknownMethod = errors.New("unknown precompile method")
	// ErrBlockNotFinalized is returned when querying unfinalized block
	ErrBlockNotFinalized = errors.New("block not finalized")
)

// Gas costs for precompile operations
const (
	GetQuasarStateGas      uint64 = 2_000  // Read-only state query
	GetValidatorInfoGas    uint64 = 5_000  // Validator lookup + key retrieval
	GetConsensusMetricsGas uint64 = 3_000  // Metrics aggregation
	GetFinalityGas         uint64 = 10_000 // Block finality proof retrieval
	GetRingtailStatsGas    uint64 = 2_000  // Ringtail state query
)

// Function selectors (first 4 bytes of keccak256 hash)
var (
	// getQuasarState() => keccak256("getQuasarState()")[:4]
	SelectorGetQuasarState = [4]byte{0x71, 0x8c, 0x52, 0xa4}
	// getValidatorInfo(bytes20) => keccak256("getValidatorInfo(bytes20)")[:4]
	SelectorGetValidatorInfo = [4]byte{0x2b, 0x9f, 0x1d, 0x67}
	// getConsensusMetrics() => keccak256("getConsensusMetrics()")[:4]
	SelectorGetConsensusMetrics = [4]byte{0x8d, 0x3e, 0x4a, 0x52}
	// getFinality(bytes32) => keccak256("getFinality(bytes32)")[:4]
	SelectorGetFinality = [4]byte{0x6f, 0x2a, 0x8b, 0xc3}
	// getRingtailStats() => keccak256("getRingtailStats()")[:4]
	SelectorGetRingtailStats = [4]byte{0x4e, 0x71, 0xd9, 0x28}
)

// QuasarPrecompile provides EVM access to Quasar consensus state.
// This is a stateless precompile - it reads from the connected Quasar instance.
type QuasarPrecompile struct {
	quasar *quasar.Quasar
}

// NewQuasarPrecompile creates a new Quasar precompile instance.
// The quasar parameter can be nil - methods will return ErrQuasarNotConnected.
func NewQuasarPrecompile(q *quasar.Quasar) *QuasarPrecompile {
	return &QuasarPrecompile{quasar: q}
}

// SetQuasar connects or updates the Quasar consensus instance.
func (p *QuasarPrecompile) SetQuasar(q *quasar.Quasar) {
	p.quasar = q
}

// RequiredGas returns the gas cost for the given input.
func (p *QuasarPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 4 {
		return GetQuasarStateGas // Minimum gas for error return
	}

	var selector [4]byte
	copy(selector[:], input[:4])

	switch selector {
	case SelectorGetQuasarState:
		return GetQuasarStateGas
	case SelectorGetValidatorInfo:
		return GetValidatorInfoGas
	case SelectorGetConsensusMetrics:
		return GetConsensusMetricsGas
	case SelectorGetFinality:
		return GetFinalityGas
	case SelectorGetRingtailStats:
		return GetRingtailStatsGas
	default:
		return GetQuasarStateGas
	}
}

// Run executes the precompile based on the function selector.
func (p *QuasarPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 4 {
		return nil, ErrInvalidInput
	}

	var selector [4]byte
	copy(selector[:], input[:4])

	switch selector {
	case SelectorGetQuasarState:
		return p.getQuasarState()
	case SelectorGetValidatorInfo:
		return p.getValidatorInfo(input[4:])
	case SelectorGetConsensusMetrics:
		return p.getConsensusMetrics()
	case SelectorGetFinality:
		return p.getFinality(input[4:])
	case SelectorGetRingtailStats:
		return p.getRingtailStats()
	default:
		return nil, ErrUnknownMethod
	}
}

// getQuasarState returns the current Quasar consensus state.
// Output: (pChainHeight uint64, qChainHeight uint64, isRunning bool, finalizedBlocks uint32)
// Encoded as: 8 + 8 + 1 + 4 = 21 bytes, padded to 32-byte words for EVM
func (p *QuasarPrecompile) getQuasarState() ([]byte, error) {
	if p.quasar == nil {
		return nil, ErrQuasarNotConnected
	}

	stats := p.quasar.Stats()

	// Pack output: 4 x 32-byte words
	output := make([]byte, 128)
	// Word 0: pChainHeight (uint256)
	binary.BigEndian.PutUint64(output[24:32], stats.PChainHeight)
	// Word 1: qChainHeight (uint256)
	binary.BigEndian.PutUint64(output[56:64], stats.QChainHeight)
	// Word 2: isRunning (bool as uint256)
	if stats.Running {
		output[95] = 1
	}
	// Word 3: finalizedBlocks (uint256)
	binary.BigEndian.PutUint32(output[124:128], uint32(stats.FinalizedBlocks))

	return output, nil
}

// getValidatorInfo returns info for a specific validator.
// Input: nodeID (20 bytes, padded to 32)
// Output: (weight uint64, active bool, blsPubKeyLen uint32, ringtailKeyLen uint32)
func (p *QuasarPrecompile) getValidatorInfo(input []byte) ([]byte, error) {
	if p.quasar == nil {
		return nil, ErrQuasarNotConnected
	}

	if len(input) < 32 {
		return nil, ErrInvalidInput
	}

	// Extract nodeID from input (last 20 bytes of 32-byte word)
	var nodeID ids.NodeID
	copy(nodeID[:], input[12:32])

	// Query P-Chain for validator state via Quasar
	// For now, return placeholder - real implementation would query validator manager
	// This demonstrates the wiring; full implementation requires P-Chain integration

	output := make([]byte, 128)
	// Word 0: weight = 0 (not found placeholder)
	// Word 1: active = false
	// Word 2: blsPubKeyLen = 0
	// Word 3: ringtailKeyLen = 0

	return output, nil
}

// getConsensusMetrics returns aggregated consensus performance metrics.
// Output: (avgBLSLatencyNs uint64, avgRingtailLatencyNs uint64, totalFinalized uint64, ringtailParties uint32, ringtailThreshold uint32)
func (p *QuasarPrecompile) getConsensusMetrics() ([]byte, error) {
	if p.quasar == nil {
		return nil, ErrQuasarNotConnected
	}

	stats := p.quasar.Stats()

	output := make([]byte, 160)
	// Word 0: avgBLSLatencyNs (placeholder - would need to track in Quasar)
	// Word 1: avgRingtailLatencyNs (placeholder)
	// Word 2: totalFinalized
	binary.BigEndian.PutUint64(output[88:96], uint64(stats.FinalizedBlocks))
	// Word 3: ringtailParties
	binary.BigEndian.PutUint32(output[124:128], uint32(stats.RingtailParties))
	// Word 4: ringtailThreshold
	binary.BigEndian.PutUint32(output[156:160], uint32(stats.RingtailThreshold))

	return output, nil
}

// getFinality returns the finality proof for a specific block.
// Input: blockID (32 bytes)
// Output: (found bool, pHeight uint64, qHeight uint64, signerWeight uint64, totalWeight uint64)
func (p *QuasarPrecompile) getFinality(input []byte) ([]byte, error) {
	if p.quasar == nil {
		return nil, ErrQuasarNotConnected
	}

	if len(input) < 32 {
		return nil, ErrInvalidInput
	}

	var blockID ids.ID
	copy(blockID[:], input[:32])

	finality, found := p.quasar.GetFinality(blockID)

	output := make([]byte, 160)
	if !found {
		// Word 0: found = false (all zeros)
		return output, nil
	}

	// Word 0: found = true
	output[31] = 1
	// Word 1: pHeight
	binary.BigEndian.PutUint64(output[56:64], finality.PChainHeight)
	// Word 2: qHeight
	binary.BigEndian.PutUint64(output[88:96], finality.QChainHeight)
	// Word 3: signerWeight
	binary.BigEndian.PutUint64(output[120:128], finality.SignerWeight)
	// Word 4: totalWeight
	binary.BigEndian.PutUint64(output[152:160], finality.TotalWeight)

	return output, nil
}

// getRingtailStats returns Ringtail threshold signature coordinator stats.
// Output: (numParties uint32, threshold uint32, sessionID uint32, initialized bool)
func (p *QuasarPrecompile) getRingtailStats() ([]byte, error) {
	if p.quasar == nil {
		return nil, ErrQuasarNotConnected
	}

	stats := p.quasar.Stats()

	output := make([]byte, 128)
	// Word 0: numParties
	binary.BigEndian.PutUint32(output[28:32], uint32(stats.RingtailParties))
	// Word 1: threshold
	binary.BigEndian.PutUint32(output[60:64], uint32(stats.RingtailThreshold))
	// Word 2: sessionID (placeholder - would need tracking in coordinator)
	// Word 3: initialized
	if stats.RingtailReady {
		output[127] = 1
	}

	return output, nil
}
