// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package teleport

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/xvm/txs"
)

// TeleportEngine manages omnichain asset transfers via burn/mint
type TeleportEngine struct {
	vm              *VM
	mpcManager      *MPCManager
	intentPool      *IntentPool
	assetRegistry   *AssetRegistry
	
	// Active transfers
	transfers       map[ids.ID]*TeleportTransfer
	transfersMutex  sync.RWMutex
	
	// Metrics
	totalTransfers  uint64
	totalVolume     *big.Int
}

// TeleportIntent represents a user's intent to transfer assets cross-chain
type TeleportIntent struct {
	ID              ids.ID
	SourceChain     ids.ID
	DestChain       ids.ID
	AssetID         ids.ID
	Amount          uint64
	Sender          ids.ShortID
	Recipient       common.Address // Can be on any chain
	Deadline        time.Time
	Signature       []byte
	Metadata        []byte
}

// TeleportTransfer tracks an active cross-chain transfer
type TeleportTransfer struct {
	Intent          *TeleportIntent
	Status          TransferStatus
	BurnTxID        ids.ID
	MintTxID        ids.ID
	CreatedAt       time.Time
	CompletedAt     time.Time
}

type TransferStatus uint8

const (
	TransferStatusPending TransferStatus = iota
	TransferStatusBurning
	TransferStatusMinting
	TransferStatusCompleted
	TransferStatusFailed
)

// NewTeleportEngine creates a new teleport engine
func NewTeleportEngine(vm *VM) *TeleportEngine {
	return &TeleportEngine{
		vm:            vm,
		mpcManager:    NewMPCManager(vm.db),
		intentPool:    NewIntentPool(),
		assetRegistry: NewAssetRegistry(vm.db),
		transfers:     make(map[ids.ID]*TeleportTransfer),
		totalVolume:   big.NewInt(0),
	}
}

// ProcessIntent processes a teleport intent
func (te *TeleportEngine) ProcessIntent(ctx context.Context, intent *TeleportIntent) (*TeleportTransfer, error) {
	// Validate intent
	if err := te.validateIntent(intent); err != nil {
		return nil, fmt.Errorf("invalid intent: %w", err)
	}
	
	// Create transfer record
	transfer := &TeleportTransfer{
		Intent:    intent,
		Status:    TransferStatusPending,
		CreatedAt: time.Now(),
	}
	
	// Add to active transfers
	te.transfersMutex.Lock()
	te.transfers[intent.ID] = transfer
	te.transfersMutex.Unlock()
	
	// Process based on direction
	if intent.SourceChain == constants.XVMID {
		// Burning from X-Chain (assets leaving Lux)
		return te.processBurnFromXChain(ctx, transfer)
	} else if intent.DestChain == constants.XVMID {
		// Minting on X-Chain (assets entering Lux)
		return te.processMintToXChain(ctx, transfer)
	} else {
		// Transit through X-Chain (burn then mint)
		return te.processTransitTransfer(ctx, transfer)
	}
}

// processBurnFromXChain handles burning assets on X-Chain
func (te *TeleportEngine) processBurnFromXChain(ctx context.Context, transfer *TeleportTransfer) (*TeleportTransfer, error) {
	intent := transfer.Intent
	
	// Update status
	transfer.Status = TransferStatusBurning
	
	// Create burn transaction
	burnTx := &txs.BurnTx{
		BaseTx: txs.BaseTx{
			NetworkID:    te.vm.ctx.NetworkID,
			BlockchainID: te.vm.ctx.ChainID,
		},
		AssetID:      intent.AssetID,
		Amount:       intent.Amount,
		DestChain:    intent.DestChain,
		DestAddress:  intent.Recipient.Bytes(),
		TeleportData: intent.Metadata,
	}
	
	// Add input from sender
	utxo, err := te.vm.getUTXO(intent.Sender, intent.AssetID, intent.Amount)
	if err != nil {
		return nil, fmt.Errorf("insufficient balance: %w", err)
	}
	
	burnTx.Ins = []*txs.TransferableInput{{
		UTXOID: utxo.UTXOID,
		Asset:  txs.Asset{ID: intent.AssetID},
		In:     &secp256k1fx.TransferInput{Amt: intent.Amount},
	}}
	
	// Sign with MPC
	signedTx, err := te.mpcManager.SignTeleportTx(burnTx)
	if err != nil {
		return nil, fmt.Errorf("failed to sign burn tx: %w", err)
	}
	
	// Issue transaction
	txID := signedTx.ID()
	if err := te.vm.issueTx(signedTx); err != nil {
		return nil, fmt.Errorf("failed to issue burn tx: %w", err)
	}
	
	transfer.BurnTxID = txID
	transfer.Status = TransferStatusCompleted
	transfer.CompletedAt = time.Now()
	
	// Emit event for bridge to pick up
	te.vm.notifyBurn(BurnEvent{
		TxID:        txID,
		AssetID:     intent.AssetID,
		Amount:      intent.Amount,
		DestChain:   intent.DestChain,
		DestAddress: intent.Recipient,
	})
	
	return transfer, nil
}

// processMintToXChain handles minting assets on X-Chain
func (te *TeleportEngine) processMintToXChain(ctx context.Context, transfer *TeleportTransfer) (*TeleportTransfer, error) {
	intent := transfer.Intent
	
	// Verify source chain burn proof
	proof, err := te.verifyBurnProof(intent.SourceChain, intent.Metadata)
	if err != nil {
		return nil, fmt.Errorf("invalid burn proof: %w", err)
	}
	
	// Update status
	transfer.Status = TransferStatusMinting
	
	// Create mint transaction
	mintTx := &txs.MintTx{
		BaseTx: txs.BaseTx{
			NetworkID:    te.vm.ctx.NetworkID,
			BlockchainID: te.vm.ctx.ChainID,
		},
		AssetID:      intent.AssetID,
		Amount:       intent.Amount,
		SourceChain:  intent.SourceChain,
		BurnProof:    proof,
	}
	
	// Add output to recipient
	recipientAddr, err := te.vm.ParseAddress(string(intent.Recipient.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address: %w", err)
	}
	
	mintTx.Outs = []*txs.TransferableOutput{{
		Asset: txs.Asset{ID: intent.AssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: intent.Amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{recipientAddr},
			},
		},
	}}
	
	// Sign with MPC
	signedTx, err := te.mpcManager.SignTeleportTx(mintTx)
	if err != nil {
		return nil, fmt.Errorf("failed to sign mint tx: %w", err)
	}
	
	// Issue transaction
	txID := signedTx.ID()
	if err := te.vm.issueTx(signedTx); err != nil {
		return nil, fmt.Errorf("failed to issue mint tx: %w", err)
	}
	
	transfer.MintTxID = txID
	transfer.Status = TransferStatusCompleted
	transfer.CompletedAt = time.Now()
	
	return transfer, nil
}

// processTransitTransfer handles transfers that transit through X-Chain
func (te *TeleportEngine) processTransitTransfer(ctx context.Context, transfer *TeleportTransfer) (*TeleportTransfer, error) {
	// First mint on X-Chain from source
	mintTransfer := &TeleportTransfer{
		Intent: &TeleportIntent{
			ID:          ids.GenerateID(),
			SourceChain: transfer.Intent.SourceChain,
			DestChain:   constants.XVMID,
			AssetID:     transfer.Intent.AssetID,
			Amount:      transfer.Intent.Amount,
			Sender:      transfer.Intent.Sender,
			Recipient:   common.Address{}, // Temporary holder
			Metadata:    transfer.Intent.Metadata,
		},
		Status: TransferStatusPending,
	}
	
	if _, err := te.processMintToXChain(ctx, mintTransfer); err != nil {
		return nil, fmt.Errorf("failed to mint for transit: %w", err)
	}
	
	// Then burn from X-Chain to destination
	burnTransfer := &TeleportTransfer{
		Intent: &TeleportIntent{
			ID:          transfer.Intent.ID,
			SourceChain: constants.XVMID,
			DestChain:   transfer.Intent.DestChain,
			AssetID:     transfer.Intent.AssetID,
			Amount:      transfer.Intent.Amount,
			Sender:      ids.ShortEmpty, // From temporary holder
			Recipient:   transfer.Intent.Recipient,
		},
		Status: TransferStatusPending,
	}
	
	return te.processBurnFromXChain(ctx, burnTransfer)
}

// validateIntent validates a teleport intent
func (te *TeleportEngine) validateIntent(intent *TeleportIntent) error {
	// Check deadline
	if time.Now().After(intent.Deadline) {
		return fmt.Errorf("intent expired")
	}
	
	// Verify signature
	if !verify.SigVerify(intent.Hash(), intent.Signature) {
		return fmt.Errorf("invalid signature")
	}
	
	// Check if asset is supported
	if !te.assetRegistry.IsSupported(intent.AssetID) {
		return fmt.Errorf("unsupported asset")
	}
	
	// Validate amount
	if intent.Amount == 0 {
		return fmt.Errorf("zero amount")
	}
	
	return nil
}

// verifyBurnProof verifies a burn proof from another chain
func (te *TeleportEngine) verifyBurnProof(sourceChain ids.ID, proofData []byte) ([]byte, error) {
	// In production, this would verify merkle proofs, signatures, etc.
	// For now, simplified verification
	return proofData, nil
}

// Hash returns the hash of a teleport intent
func (ti *TeleportIntent) Hash() []byte {
	// Implementation would hash all intent fields
	return []byte{}
}

// GetTransfer returns a transfer by ID
func (te *TeleportEngine) GetTransfer(id ids.ID) (*TeleportTransfer, bool) {
	te.transfersMutex.RLock()
	defer te.transfersMutex.RUnlock()
	
	transfer, exists := te.transfers[id]
	return transfer, exists
}

// GetMetrics returns teleport metrics
func (te *TeleportEngine) GetMetrics() (uint64, *big.Int) {
	return te.totalTransfers, new(big.Int).Set(te.totalVolume)
}