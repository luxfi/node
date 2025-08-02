// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar/crypto/bls"
	"github.com/luxfi/node/v2/quasar/crypto/ringtail"
)

// TestQuantumFinalityEngine tests the full 8-chain quantum finality flow
func TestQuantumFinalityEngine(t *testing.T) {
	require := require.New(t)

	// Create mock validator manager
	mockValidatorManager := &MockValidatorManager{
		canValidate: true,
		validators: make(map[ids.ID][]ids.NodeID),
	}

	// Create engine
	pChainID := ids.GenerateTestID()
	qChainID := ids.GenerateTestID()
	engine := NewQuantumFinalityEngine(pChainID, qChainID, mockValidatorManager)

	// Define all 8 chains
	chains := map[string]ids.ID{
		"A-Chain": ids.GenerateTestID(), // AI
		"B-Chain": ids.GenerateTestID(), // Bridge
		"C-Chain": ids.GenerateTestID(), // EVM
		"M-Chain": ids.GenerateTestID(), // MPC
		"P-Chain": pChainID,             // Platform
		"Q-Chain": qChainID,             // Quantum
		"X-Chain": ids.GenerateTestID(), // Exchange
		"Z-Chain": ids.GenerateTestID(), // ZK
	}

	// Setup mock validators for each chain
	for name, chainID := range chains {
		// Create different validator sets based on chain type
		var validators []ids.NodeID
		switch name {
		case "P-Chain", "Q-Chain":
			// Full validator set for core chains
			validators = generateValidators(21)
		case "C-Chain", "X-Chain":
			// Medium set for public chains
			validators = generateValidators(15)
		case "B-Chain", "M-Chain":
			// Small set for Genesis NFT-gated chains
			validators = generateValidators(7)
		case "A-Chain", "Z-Chain":
			// Minimal set for specialized chains
			validators = generateValidators(5)
		}
		mockValidatorManager.validators[chainID] = validators
	}

	// Wrap all chains
	for name, chainID := range chains {
		err := engine.WrapChain(chainID, name)
		require.NoError(err)
	}

	// Test 1: Submit operations from each chain
	t.Run("SubmitOperations", func(t *testing.T) {
		// A-Chain: AI operation
		err := engine.SubmitOperation(chains["A-Chain"], Operation{
			ChainID:       chains["A-Chain"],
			OperationType: "AI_INFERENCE",
			Payload:       []byte("model_update"),
			Signature:     []byte("sig_a"),
			Timestamp:     time.Now(),
		})
		require.NoError(err)

		// B-Chain: Bridge operation
		err = engine.SubmitOperation(chains["B-Chain"], Operation{
			ChainID:       chains["B-Chain"],
			OperationType: "BRIDGE_TRANSFER",
			Payload:       []byte("eth_to_lux"),
			Signature:     []byte("sig_b"),
			Timestamp:     time.Now(),
		})
		require.NoError(err)

		// C-Chain: EVM transaction
		err = engine.SubmitOperation(chains["C-Chain"], Operation{
			ChainID:       chains["C-Chain"],
			OperationType: "EVM_TX",
			Payload:       []byte("smart_contract_call"),
			Signature:     []byte("sig_c"),
			Timestamp:     time.Now(),
		})
		require.NoError(err)

		// M-Chain: MPC operation
		err = engine.SubmitOperation(chains["M-Chain"], Operation{
			ChainID:       chains["M-Chain"],
			OperationType: "MPC_COMPUTATION",
			Payload:       []byte("threshold_signature"),
			Signature:     []byte("sig_m"),
			Timestamp:     time.Now(),
		})
		require.NoError(err)

		// X-Chain: Exchange operation
		err = engine.SubmitOperation(chains["X-Chain"], Operation{
			ChainID:       chains["X-Chain"],
			OperationType: "ASSET_EXCHANGE",
			Payload:       []byte("swap_tokens"),
			Signature:     []byte("sig_x"),
			Timestamp:     time.Now(),
		})
		require.NoError(err)

		// Z-Chain: ZK proof
		err = engine.SubmitOperation(chains["Z-Chain"], Operation{
			ChainID:       chains["Z-Chain"],
			OperationType: "ZK_PROOF",
			Payload:       []byte("privacy_proof"),
			Signature:     []byte("sig_z"),
			Timestamp:     time.Now(),
		})
		require.NoError(err)
	})

	// Test 2: Create consensus block
	var block *PendingBlock
	t.Run("CreateConsensusBlock", func(t *testing.T) {
		var err error
		block, err = engine.CreateConsensusBlock(1000)
		require.NoError(err)
		require.NotNil(block)
		require.Equal(uint64(1000), block.Height)
		
		// Verify operations from all chains are included
		require.Greater(len(block.Operations), 0)
		require.Contains(block.Operations, chains["A-Chain"])
		require.Contains(block.Operations, chains["B-Chain"])
		require.Contains(block.Operations, chains["C-Chain"])
		require.Contains(block.Operations, chains["M-Chain"])
		require.Contains(block.Operations, chains["X-Chain"])
		require.Contains(block.Operations, chains["Z-Chain"])
	})

	// Test 3: Submit P-Chain BLS signature
	t.Run("SubmitPChainBLS", func(t *testing.T) {
		blsSig := &bls.Signature{}
		err := engine.SubmitPChainBLS(block.BlockID, blsSig)
		require.NoError(err)

		// Check status
		pChain, qChain, finalized := engine.GetFinalityStatus(block.BlockID)
		require.True(pChain)
		require.False(qChain)
		require.False(finalized)
	})

	// Test 4: Submit Q-Chain Ringtail signature
	t.Run("SubmitQChainRingtail", func(t *testing.T) {
		rtSig := &ringtail.Signature{}
		err := engine.SubmitQChainRingtail(block.BlockID, rtSig)
		require.NoError(err)

		// Check status - should be finalized now
		pChain, qChain, finalized := engine.GetFinalityStatus(block.BlockID)
		require.True(pChain)
		require.True(qChain)
		require.True(finalized)
	})

	// Test 5: Wait for finality
	t.Run("WaitForFinality", func(t *testing.T) {
		ctx := context.Background()
		finalizedBlock, err := engine.WaitForFinality(ctx, block.BlockID)
		require.NoError(err)
		require.NotNil(finalizedBlock)
		require.NotNil(finalizedBlock.FinalityProof)
		require.NotNil(finalizedBlock.FinalityProof.PChainSignature)
		require.NotNil(finalizedBlock.FinalityProof.QChainSignature)
	})
}

// TestCChainQuantumFinality tests how C-Chain blocks get quantum finality
func TestCChainQuantumFinality(t *testing.T) {
	require := require.New(t)

	// Create mock validator manager
	mockValidatorManager := &MockValidatorManager{
		canValidate: true,
		validators: make(map[ids.ID][]ids.NodeID),
	}

	// Create engine
	engine := NewQuantumFinalityEngine(ids.GenerateTestID(), ids.GenerateTestID(), mockValidatorManager)
	
	cChainID := ids.GenerateTestID()
	
	// Setup C-Chain validators
	mockValidatorManager.validators[cChainID] = generateValidators(15)
	
	err := engine.WrapChain(cChainID, "C-Chain")
	require.NoError(err)

	// Simulate C-Chain block production
	cChainBlock := &CChainBlock{
		Height:       12345,
		Hash:         ids.GenerateTestID(),
		Transactions: []CChainTx{
			{Hash: ids.GenerateTestID(), From: "0x123", To: "0x456", Value: 100},
			{Hash: ids.GenerateTestID(), From: "0x789", To: "0xabc", Value: 200},
		},
	}

	// Convert C-Chain block to operation
	operation := Operation{
		ChainID:       cChainID,
		OperationType: "C_CHAIN_BLOCK",
		Payload:       cChainBlock.Serialize(),
		Signature:     []byte("c_chain_sig"),
		Timestamp:     time.Now(),
	}

	// Submit to quantum finality engine
	err = engine.SubmitOperation(cChainID, operation)
	require.NoError(err)

	// Create consensus block including C-Chain block
	consensusBlock, err := engine.CreateConsensusBlock(cChainBlock.Height)
	require.NoError(err)

	// Get quantum signatures
	blsSig := &bls.Signature{}
	rtSig := &ringtail.Signature{}

	// Submit signatures for dual finality
	err = engine.SubmitPChainBLS(consensusBlock.BlockID, blsSig)
	require.NoError(err)
	
	err = engine.SubmitQChainRingtail(consensusBlock.BlockID, rtSig)
	require.NoError(err)

	// C-Chain block is now quantum-finalized
	ctx := context.Background()
	finalizedBlock, err := engine.WaitForFinality(ctx, consensusBlock.BlockID)
	require.NoError(err)

	// Extract C-Chain operations from finalized block
	cChainOps := finalizedBlock.Operations[cChainID]
	require.Len(cChainOps, 1)
	require.Equal("C_CHAIN_BLOCK", cChainOps[0].OperationType)
}

// TestConsensusFlow tests the complete consensus flow for all 8 chains
func TestConsensusFlow(t *testing.T) {
	require := require.New(t)

	// Create mock validator manager
	mockValidatorManager := &MockValidatorManager{
		canValidate: true,
		validators: make(map[ids.ID][]ids.NodeID),
	}

	// Create engine
	engine := NewQuantumFinalityEngine(ids.GenerateTestID(), ids.GenerateTestID(), mockValidatorManager)
	flow := &ConsensusFlow{engine: engine}

	// Register all chains
	chainIDs := make(map[string]ids.ID)
	chainNames := []string{"A-Chain", "B-Chain", "C-Chain", "M-Chain", "P-Chain", "Q-Chain", "X-Chain", "Z-Chain"}
	
	for _, name := range chainNames {
		chainID := ids.GenerateTestID()
		chainIDs[name] = chainID
		
		// Setup validators based on chain type
		var numValidators int
		switch name {
		case "P-Chain", "Q-Chain":
			numValidators = 21
		case "C-Chain", "X-Chain":
			numValidators = 15
		case "B-Chain", "M-Chain":
			numValidators = 7
		case "A-Chain", "Z-Chain":
			numValidators = 5
		}
		mockValidatorManager.validators[chainID] = generateValidators(numValidators)
		
		err := engine.WrapChain(chainID, name)
		require.NoError(err)
	}

	// Submit operations from all chains
	for name, chainID := range chainIDs {
		err := engine.SubmitOperation(chainID, Operation{
			ChainID:       chainID,
			OperationType: name + "_OP",
			Payload:       []byte("test_payload_" + name),
			Signature:     []byte("sig_" + name),
			Timestamp:     time.Now(),
		})
		require.NoError(err)
	}

	// Execute full consensus round
	finalizedBlock, err := flow.ExecuteConsensusRound(2000)
	require.NoError(err)
	require.NotNil(finalizedBlock)
	require.Equal(uint64(2000), finalizedBlock.Height)

	// Verify all chains' operations are included
	totalOps := 0
	for _, ops := range finalizedBlock.Operations {
		totalOps += len(ops)
	}
	require.Equal(8, totalOps) // One operation from each chain
}

// CChainBlock represents a C-Chain (EVM) block
type CChainBlock struct {
	Height       uint64
	Hash         ids.ID
	Transactions []CChainTx
}

// CChainTx represents a C-Chain transaction
type CChainTx struct {
	Hash  ids.ID
	From  string
	To    string
	Value uint64
}

// Serialize converts C-Chain block to bytes
func (b *CChainBlock) Serialize() []byte {
	// Simple serialization for testing
	return append(b.Hash[:], []byte("block_data")...)
}

// MockValidatorManager is a mock implementation of ValidatorChainManager
type MockValidatorManager struct {
	canValidate bool
	validators  map[ids.ID][]ids.NodeID
}

func (m *MockValidatorManager) CanValidateChain(validatorID ids.NodeID, chainID ids.ID) bool {
	return m.canValidate
}

func (m *MockValidatorManager) GetValidatorsForChain(chainID ids.ID) []ids.NodeID {
	return m.validators[chainID]
}

func (m *MockValidatorManager) GetChainValidatorSet(chainID ids.ID) (*ChainValidatorSet, error) {
	validators := m.validators[chainID]
	if len(validators) == 0 {
		validators = generateValidators(5) // Default to 5 validators
		m.validators[chainID] = validators
	}
	
	// Determine minimum validators based on chain
	minValidators := 3
	isGenesisGated := false
	
	// This is a simplified version - in reality would check actual chain IDs
	if len(validators) >= 21 {
		minValidators = 21 // P/Q chains
	} else if len(validators) >= 15 {
		minValidators = 15 // C/X chains
	} else if len(validators) >= 7 {
		minValidators = 7 // B/M chains
		isGenesisGated = true
	} else {
		minValidators = 5 // A/Z chains
	}
	
	return &ChainValidatorSet{
		ChainID:        chainID,
		Validators:     validators,
		IsGenesisGated: isGenesisGated,
		MinValidators:  minValidators,
	}, nil
}

// generateValidators creates a set of test validator IDs
func generateValidators(count int) []ids.NodeID {
	validators := make([]ids.NodeID, count)
	for i := 0; i < count; i++ {
		validators[i] = ids.GenerateTestNodeID()
	}
	return validators
}