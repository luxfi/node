// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mvm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/avm/txs"
	"github.com/luxfi/node/vms/secp256k1fx"
)

// XChainSettlement handles all X-Chain mint/burn operations for the Teleport Protocol
type XChainSettlement struct {
	client          *XChainClient
	mpcWallet       *MPCWallet
	zkProver        *ZKProver
	
	// Settlement tracking
	pendingSettlements map[ids.ID]*Settlement
	settlementMutex    sync.RWMutex
	
	// Batch processing
	batchQueue      []*Settlement
	batchMutex      sync.Mutex
	batchTimer      *time.Timer
	
	config          XChainSettlementConfig
}

// Settlement represents a pending X-Chain settlement
type Settlement struct {
	ID              ids.ID
	Type            SettlementType
	AssetID         ids.ID
	Amount          uint64
	SourceChain     ids.ID
	DestChain       ids.ID
	Recipient       common.Address
	TeleportProof   *TeleportProof
	Status          SettlementStatus
	CreatedAt       time.Time
	CompletedAt     time.Time
	XChainTxID      ids.ID
}

type SettlementType uint8

const (
	SettlementTypeMint SettlementType = iota
	SettlementTypeBurn
)

type SettlementStatus uint8

const (
	SettlementStatusPending SettlementStatus = iota
	SettlementStatusInProgress
	SettlementStatusCompleted
	SettlementStatusFailed
)

// XChainSettlementConfig contains X-Chain settlement configuration
type XChainSettlementConfig struct {
	Endpoint            string
	BatchSize           int
	BatchInterval       time.Duration
	ConfirmationBlocks  uint64
	MaxRetries          int
	RetryDelay          time.Duration
}

// TeleportProof contains the ZK proof for a teleport operation
type TeleportProof struct {
	// Cross-chain transfer proof
	TransferProof   []byte
	
	// State transition proofs
	SourceStateProof []byte
	DestStateProof   []byte
	
	// Asset registry proof
	AssetValidityProof []byte
	
	// Execution proof (for intents)
	ExecutionProof   []byte
	
	// Aggregated validator signatures
	ValidatorSigs    [][]byte
	
	// Metadata
	ProofType       string
	GeneratedAt     time.Time
}

// NewXChainSettlement creates a new X-Chain settlement manager
func NewXChainSettlement(
	client *XChainClient,
	mpcWallet *MPCWallet,
	zkProver *ZKProver,
	config XChainSettlementConfig,
) *XChainSettlement {
	return &XChainSettlement{
		client:             client,
		mpcWallet:          mpcWallet,
		zkProver:           zkProver,
		pendingSettlements: make(map[ids.ID]*Settlement),
		batchQueue:         make([]*Settlement, 0),
		config:             config,
	}
}

// ProcessIncomingAssets handles assets coming into the Lux ecosystem
// This MINTS new assets on the X-Chain backed by the locked assets on the source chain
func (xs *XChainSettlement) ProcessIncomingAssets(
	ctx context.Context,
	sourceChain ids.ID,
	assetID ids.ID,
	amount uint64,
	recipient common.Address,
	proof *TeleportProof,
) (*Settlement, error) {
	log.Info("Processing incoming assets",
		"sourceChain", sourceChain,
		"assetID", assetID,
		"amount", amount,
		"recipient", recipient,
	)
	
	// Verify the teleport proof
	if err := xs.verifyTeleportProof(proof); err != nil {
		return nil, fmt.Errorf("invalid teleport proof: %w", err)
	}
	
	// Create settlement record
	settlement := &Settlement{
		ID:            ids.GenerateID(),
		Type:          SettlementTypeMint,
		AssetID:       assetID,
		Amount:        amount,
		SourceChain:   sourceChain,
		DestChain:     constants.XVMID,
		Recipient:     recipient,
		TeleportProof: proof,
		Status:        SettlementStatusPending,
		CreatedAt:     time.Now(),
	}
	
	// Add to pending settlements
	xs.settlementMutex.Lock()
	xs.pendingSettlements[settlement.ID] = settlement
	xs.settlementMutex.Unlock()
	
	// Add to batch queue
	xs.addToBatch(settlement)
	
	return settlement, nil
}

// ProcessOutgoingAssets handles assets leaving the Lux ecosystem
// This BURNS assets on the X-Chain to release them on the destination chain
func (xs *XChainSettlement) ProcessOutgoingAssets(
	ctx context.Context,
	destChain ids.ID,
	assetID ids.ID,
	amount uint64,
	sender common.Address,
	proof *TeleportProof,
) (*Settlement, error) {
	log.Info("Processing outgoing assets",
		"destChain", destChain,
		"assetID", assetID,
		"amount", amount,
		"sender", sender,
	)
	
	// Verify the teleport proof
	if err := xs.verifyTeleportProof(proof); err != nil {
		return nil, fmt.Errorf("invalid teleport proof: %w", err)
	}
	
	// Verify sender has sufficient balance on X-Chain
	balance, err := xs.client.GetBalance(sender, assetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}
	
	if balance < amount {
		return nil, fmt.Errorf("insufficient balance: have %d, need %d", balance, amount)
	}
	
	// Create settlement record
	settlement := &Settlement{
		ID:            ids.GenerateID(),
		Type:          SettlementTypeBurn,
		AssetID:       assetID,
		Amount:        amount,
		SourceChain:   constants.XVMID,
		DestChain:     destChain,
		Recipient:     sender, // Sender is burning their own assets
		TeleportProof: proof,
		Status:        SettlementStatusPending,
		CreatedAt:     time.Now(),
	}
	
	// Add to pending settlements
	xs.settlementMutex.Lock()
	xs.pendingSettlements[settlement.ID] = settlement
	xs.settlementMutex.Unlock()
	
	// Add to batch queue
	xs.addToBatch(settlement)
	
	return settlement, nil
}

// verifyTeleportProof verifies the ZK proof for a teleport operation
func (xs *XChainSettlement) verifyTeleportProof(proof *TeleportProof) error {
	// Verify transfer proof
	if err := xs.zkProver.VerifyProof(
		"transfer_circuit",
		proof.TransferProof,
		nil, // Public inputs would be extracted from proof
	); err != nil {
		return fmt.Errorf("invalid transfer proof: %w", err)
	}
	
	// Verify state proofs
	if err := xs.zkProver.VerifyProof(
		"state_transition_circuit",
		proof.SourceStateProof,
		nil,
	); err != nil {
		return fmt.Errorf("invalid source state proof: %w", err)
	}
	
	if err := xs.zkProver.VerifyProof(
		"state_transition_circuit",
		proof.DestStateProof,
		nil,
	); err != nil {
		return fmt.Errorf("invalid destination state proof: %w", err)
	}
	
	// Verify asset validity
	if err := xs.zkProver.VerifyProof(
		"asset_validity_circuit",
		proof.AssetValidityProof,
		nil,
	); err != nil {
		return fmt.Errorf("invalid asset validity proof: %w", err)
	}
	
	// Verify validator signatures (2/3+ threshold)
	// This would check BLS aggregated signatures from M-Chain validators
	
	return nil
}

// addToBatch adds a settlement to the batch queue
func (xs *XChainSettlement) addToBatch(settlement *Settlement) {
	xs.batchMutex.Lock()
	defer xs.batchMutex.Unlock()
	
	xs.batchQueue = append(xs.batchQueue, settlement)
	
	// Process batch if it reaches the size limit
	if len(xs.batchQueue) >= xs.config.BatchSize {
		xs.processBatchNow()
		return
	}
	
	// Start timer if this is the first item in the batch
	if len(xs.batchQueue) == 1 {
		xs.batchTimer = time.AfterFunc(xs.config.BatchInterval, func() {
			xs.batchMutex.Lock()
			defer xs.batchMutex.Unlock()
			xs.processBatchNow()
		})
	}
}

// processBatchNow processes the current batch immediately
func (xs *XChainSettlement) processBatchNow() {
	if len(xs.batchQueue) == 0 {
		return
	}
	
	// Stop timer if running
	if xs.batchTimer != nil {
		xs.batchTimer.Stop()
		xs.batchTimer = nil
	}
	
	// Take current batch
	batch := xs.batchQueue
	xs.batchQueue = make([]*Settlement, 0)
	
	// Process batch asynchronously
	go xs.processBatch(batch)
}

// processBatch processes a batch of settlements on the X-Chain
func (xs *XChainSettlement) processBatch(settlements []*Settlement) {
	log.Info("Processing settlement batch", "count", len(settlements))
	
	// Group settlements by type for efficiency
	mints := make([]*Settlement, 0)
	burns := make([]*Settlement, 0)
	
	for _, s := range settlements {
		switch s.Type {
		case SettlementTypeMint:
			mints = append(mints, s)
		case SettlementTypeBurn:
			burns = append(burns, s)
		}
		
		// Update status
		xs.updateSettlementStatus(s.ID, SettlementStatusInProgress)
	}
	
	// Process mints
	if len(mints) > 0 {
		if err := xs.processMintBatch(mints); err != nil {
			log.Error("Failed to process mint batch", "error", err)
			for _, s := range mints {
				xs.updateSettlementStatus(s.ID, SettlementStatusFailed)
			}
		}
	}
	
	// Process burns
	if len(burns) > 0 {
		if err := xs.processBurnBatch(burns); err != nil {
			log.Error("Failed to process burn batch", "error", err)
			for _, s := range burns {
				xs.updateSettlementStatus(s.ID, SettlementStatusFailed)
			}
		}
	}
}

// processMintBatch mints assets on the X-Chain
func (xs *XChainSettlement) processMintBatch(settlements []*Settlement) error {
	// Build mint transaction using MPC wallet
	mintTx := &txs.BaseTx{
		BaseTx: txs.BaseTx{
			NetworkID:    xs.client.NetworkID(),
			BlockchainID: constants.XVMID,
		},
	}
	
	// Add outputs for each mint
	for _, s := range settlements {
		output := &secp256k1fx.TransferOutput{
			Amt: s.Amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.ShortID(s.Recipient)},
			},
		}
		
		mintTx.Outs = append(mintTx.Outs, &txs.TransferableOutput{
			Asset: txs.Asset{ID: s.AssetID},
			Out:   output,
		})
	}
	
	// Sign transaction with MPC wallet
	signedTx, err := xs.mpcWallet.SignXChainTx(mintTx)
	if err != nil {
		return fmt.Errorf("failed to sign mint transaction: %w", err)
	}
	
	// Submit to X-Chain
	txID, err := xs.client.IssueTx(signedTx)
	if err != nil {
		return fmt.Errorf("failed to issue mint transaction: %w", err)
	}
	
	// Wait for confirmation
	if err := xs.waitForConfirmation(txID); err != nil {
		return fmt.Errorf("mint transaction failed: %w", err)
	}
	
	// Update settlement records
	for _, s := range settlements {
		s.XChainTxID = txID
		s.CompletedAt = time.Now()
		xs.updateSettlementStatus(s.ID, SettlementStatusCompleted)
	}
	
	log.Info("Mint batch completed", "txID", txID, "count", len(settlements))
	return nil
}

// processBurnBatch burns assets on the X-Chain
func (xs *XChainSettlement) processBurnBatch(settlements []*Settlement) error {
	// Build burn transaction
	// In X-Chain, burning is done by sending to a burn address or using a special output type
	burnTx := &txs.BaseTx{
		BaseTx: txs.BaseTx{
			NetworkID:    xs.client.NetworkID(),
			BlockchainID: constants.XVMID,
		},
	}
	
	// Add inputs for assets to burn
	for _, s := range settlements {
		// Get UTXOs for the asset
		utxos, err := xs.client.GetUTXOs(s.Recipient, s.AssetID, s.Amount)
		if err != nil {
			return fmt.Errorf("failed to get UTXOs for burn: %w", err)
		}
		
		for _, utxo := range utxos {
			burnTx.Ins = append(burnTx.Ins, &txs.TransferableInput{
				UTXOID: utxo.UTXOID,
				Asset:  txs.Asset{ID: s.AssetID},
				In: &secp256k1fx.TransferInput{
					Amt: utxo.Amount,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			})
		}
	}
	
	// No outputs for burn transaction - assets are destroyed
	
	// Sign transaction with MPC wallet
	signedTx, err := xs.mpcWallet.SignXChainTx(burnTx)
	if err != nil {
		return fmt.Errorf("failed to sign burn transaction: %w", err)
	}
	
	// Submit to X-Chain
	txID, err := xs.client.IssueTx(signedTx)
	if err != nil {
		return fmt.Errorf("failed to issue burn transaction: %w", err)
	}
	
	// Wait for confirmation
	if err := xs.waitForConfirmation(txID); err != nil {
		return fmt.Errorf("burn transaction failed: %w", err)
	}
	
	// Update settlement records
	for _, s := range settlements {
		s.XChainTxID = txID
		s.CompletedAt = time.Now()
		xs.updateSettlementStatus(s.ID, SettlementStatusCompleted)
	}
	
	log.Info("Burn batch completed", "txID", txID, "count", len(settlements))
	return nil
}

// waitForConfirmation waits for a transaction to be confirmed
func (xs *XChainSettlement) waitForConfirmation(txID ids.ID) error {
	// Implementation would poll X-Chain for transaction status
	// and wait for required number of confirmations
	return nil
}

// updateSettlementStatus updates the status of a settlement
func (xs *XChainSettlement) updateSettlementStatus(id ids.ID, status SettlementStatus) {
	xs.settlementMutex.Lock()
	defer xs.settlementMutex.Unlock()
	
	if settlement, exists := xs.pendingSettlements[id]; exists {
		settlement.Status = status
		
		// Remove from pending if completed or failed
		if status == SettlementStatusCompleted || status == SettlementStatusFailed {
			delete(xs.pendingSettlements, id)
		}
	}
}

// GetSettlement returns a settlement by ID
func (xs *XChainSettlement) GetSettlement(id ids.ID) (*Settlement, error) {
	xs.settlementMutex.RLock()
	defer xs.settlementMutex.RUnlock()
	
	if settlement, exists := xs.pendingSettlements[id]; exists {
		return settlement, nil
	}
	
	return nil, fmt.Errorf("settlement not found: %s", id)
}

// GetPendingSettlements returns all pending settlements
func (xs *XChainSettlement) GetPendingSettlements() []*Settlement {
	xs.settlementMutex.RLock()
	defer xs.settlementMutex.RUnlock()
	
	settlements := make([]*Settlement, 0, len(xs.pendingSettlements))
	for _, s := range xs.pendingSettlements {
		settlements = append(settlements, s)
	}
	
	return settlements
}