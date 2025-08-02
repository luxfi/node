// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mpcvm

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/crypto"
	"github.com/luxfi/geth/log"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/choices"
	"github.com/luxfi/node/quasar/engine/chain/block"
	"github.com/luxfi/node/utils/hashing"
	"github.com/luxfi/node/vms/platformvm/warp"
)

var _ block.Block = &Block{}

// Block represents a block in the M-Chain
type Block struct {
	// Block metadata
	height      uint64
	ParentID    ids.ID
	Timestamp   uint64
	BlockID     ids.ID
	
	// Block content
	Header      *BlockHeader
	Body        *BlockBody
	
	// Consensus
	vm          *VM
	status      choices.Status
	bytes       []byte
}

// BlockHeader contains the block header information
type BlockHeader struct {
	ParentHash      common.Hash
	StateRoot       common.Hash
	TxRoot          common.Hash
	ReceiptRoot     common.Hash
	TeleportRoot    common.Hash // Root of teleport operations
	ValidatorRoot   common.Hash // Root of validator set changes
	Height          uint64
	Timestamp       uint64
	Extra           []byte
	
	// MPC signatures
	MPCSignature    []byte
	SignerBitmap    []byte // Bitmap of which validators signed
	
	// Warp messages
	WarpMessages    []*warp.UnsignedMessage
}

// BlockBody contains the block transactions and operations
type BlockBody struct {
	// Regular transactions
	Transactions    []*Transaction
	
	// Teleport operations
	TeleportOps     []*TeleportOperation
	
	// MPC operations
	MPCOps          []*MPCOperation
	
	// Validator updates
	ValidatorOps    []*ValidatorOperation
	
	// Cross-chain messages
	WarpPayloads    [][]byte
}

// Transaction represents a transaction in the M-Chain
type Transaction struct {
	Type            TxType
	Nonce           uint64
	From            common.Address
	To              common.Address
	Value           []byte // Encoded amount
	Data            []byte
	Signature       []byte
	
	// Teleport-specific fields
	IntentID        *ids.ID
	SourceChain     *ids.ID
	DestChain       *ids.ID
}

// TxType defines the type of transaction
type TxType uint8

const (
	TxTypeTransfer TxType = iota
	TxTypeTeleportIntent
	TxTypeMPCKeyGen
	TxTypeMPCSign
	TxTypeValidatorUpdate
	TxTypeAssetRegistry
)

// TeleportOperation represents a teleport operation in a block
type TeleportOperation struct {
	Type            TeleportOpType
	TransferID      ids.ID
	IntentID        ids.ID
	Status          TransferStatus
	
	// Settlement info
	SettlementType  SettlementType
	XChainTxID      *ids.ID
	
	// Proofs
	ZKProof         []byte
	ValidatorSigs   [][]byte
}

// TeleportOpType defines the type of teleport operation
type TeleportOpType uint8

const (
	TeleportOpTypeInitiate TeleportOpType = iota
	TeleportOpTypeSettle
	TeleportOpTypeComplete
	TeleportOpTypeRefund
)

// MPCOperation represents an MPC operation in a block
type MPCOperation struct {
	Type            MPCOpType
	SessionID       ids.ID
	Participants    []ids.NodeID
	Threshold       uint32
	
	// Operation-specific data
	Data            []byte
	
	// Results
	Result          []byte
	Signatures      [][]byte
}

// MPCOpType defines the type of MPC operation
type MPCOpType uint8

const (
	MPCOpTypeKeyGen MPCOpType = iota
	MPCOpTypeSign
	MPCOpTypeReshare
	MPCOpTypeRefresh
)

// ValidatorOperation represents a validator set change
type ValidatorOperation struct {
	Type            ValidatorOpType
	NodeID          ids.NodeID
	Weight          uint64
	
	// For NFT validators
	NFTAssetID      *ids.ID
	
	// Timing
	StartTime       uint64
	EndTime         uint64
}

// ValidatorOpType defines the type of validator operation
type ValidatorOpType uint8

const (
	ValidatorOpTypeAdd ValidatorOpType = iota
	ValidatorOpTypeRemove
	ValidatorOpTypeUpdate
	ValidatorOpTypeNFTStake
	ValidatorOpTypeNFTUnstake
)

// ID returns the block ID
func (b *Block) ID() string {
	return b.BlockID.String()
}

// Accept marks the block as accepted
func (b *Block) Accept() error {
	if b.status == choices.Accepted {
		return nil
	}
	
	log.Info("Accepting block", "height", b.height, "id", b.BlockID)
	
	// Process all operations in the block
	if err := b.processOperations(); err != nil {
		return fmt.Errorf("failed to process operations: %w", err)
	}
	
	// Update state
	b.status = choices.Accepted
	
	// Persist to database
	if err := b.vm.blockChain.AcceptBlock(b); err != nil {
		return fmt.Errorf("failed to persist block: %w", err)
	}
	
	// Update metrics
	// TODO: fix metrics
	// b.vm.metrics.MarkAccepted(b)
	
	return nil
}

// Reject marks the block as rejected
func (b *Block) Reject() error {
	if b.status == choices.Rejected {
		return nil
	}
	
	log.Info("Rejecting block", "height", b.height, "id", b.BlockID)
	
	b.status = choices.Rejected
	
	// Clean up any pending operations
	b.cleanupOperations()
	
	// Update metrics
	// TODO: fix metrics
	// b.vm.metrics.MarkRejected(b)
	
	return nil
}

// Status returns the block's status
func (b *Block) Status() choices.Status {
	return b.status
}

// Parent returns the parent block ID
func (b *Block) Parent() ids.ID {
	return b.ParentID
}

// Height returns the block height
func (b *Block) Height() uint64 {
	return b.height
}

// Time returns the block timestamp
func (b *Block) Time() uint64 {
	return b.Timestamp
}

// Bytes returns the byte representation of the block
func (b *Block) Bytes() []byte {
	if b.bytes != nil {
		return b.bytes
	}
	
	// Serialize block
	bytes, err := b.serialize()
	if err != nil {
		log.Error("Failed to serialize block", "error", err)
		return nil
	}
	
	b.bytes = bytes
	return bytes
}

// Verify verifies the block's validity
func (b *Block) Verify(context.Context) error {
	// Verify header
	if err := b.verifyHeader(); err != nil {
		return fmt.Errorf("invalid header: %w", err)
	}
	
	// Verify MPC signature
	if err := b.verifyMPCSignature(); err != nil {
		return fmt.Errorf("invalid MPC signature: %w", err)
	}
	
	// Verify transactions
	if err := b.verifyTransactions(); err != nil {
		return fmt.Errorf("invalid transactions: %w", err)
	}
	
	// Verify teleport operations
	if err := b.verifyTeleportOps(); err != nil {
		return fmt.Errorf("invalid teleport operations: %w", err)
	}
	
	// Verify state transitions
	if err := b.verifyStateTransition(); err != nil {
		return fmt.Errorf("invalid state transition: %w", err)
	}
	
	return nil
}

// processOperations processes all operations in the block
func (b *Block) processOperations() error {
	// Process transactions
	for _, tx := range b.Body.Transactions {
		if err := b.processTransaction(tx); err != nil {
			return fmt.Errorf("failed to process transaction: %w", err)
		}
	}
	
	// Process teleport operations
	for _, op := range b.Body.TeleportOps {
		if err := b.processTeleportOp(op); err != nil {
			return fmt.Errorf("failed to process teleport operation: %w", err)
		}
	}
	
	// Process MPC operations
	for _, op := range b.Body.MPCOps {
		if err := b.processMPCOp(op); err != nil {
			return fmt.Errorf("failed to process MPC operation: %w", err)
		}
	}
	
	// Process validator operations
	for _, op := range b.Body.ValidatorOps {
		if err := b.processValidatorOp(op); err != nil {
			return fmt.Errorf("failed to process validator operation: %w", err)
		}
	}
	
	// Send Warp messages
	// TODO: fix warpBackend
	// for i, msg := range b.Header.WarpMessages {
	// 	if err := b.vm.warpBackend.AddMessage(msg, b.Body.WarpPayloads[i]); err != nil {
	// 		return fmt.Errorf("failed to add warp message: %w", err)
	// 	}
	// }
	
	return nil
}

// processTransaction processes a single transaction
func (b *Block) processTransaction(tx *Transaction) error {
	switch tx.Type {
	case TxTypeTeleportIntent:
		// Create teleport intent
		intent := &TeleportIntent{
			ID:        *tx.IntentID,
			Sender:    tx.From,
			Recipient: tx.To,
			// Additional fields would be decoded from tx.Data
		}
		
		// Add to intent pool
		return b.vm.intentPool.AddIntent(intent)
		
	case TxTypeMPCKeyGen:
		// Initiate MPC key generation
		return b.vm.mpcManager.InitiateKeyGen(tx.Data)
		
	case TxTypeValidatorUpdate:
		// Update validator set
		return b.vm.validators.ProcessUpdate(tx.Data)
		
	default:
		return fmt.Errorf("unknown transaction type: %v", tx.Type)
	}
}

// processTeleportOp processes a teleport operation
func (b *Block) processTeleportOp(op *TeleportOperation) error {
	switch op.Type {
	case TeleportOpTypeSettle:
		// Process X-Chain settlement
		if op.XChainTxID != nil {
			log.Debug("Teleport operation settled on X-Chain",
				"transferID", op.TransferID,
				"xchainTx", *op.XChainTxID,
			)
		}
		
	case TeleportOpTypeComplete:
		// Mark transfer as complete
		b.vm.teleportEngine.updateTransferStatus(op.TransferID, TransferStatusCompleted)
		
	case TeleportOpTypeRefund:
		// Process refund
		b.vm.teleportEngine.updateTransferStatus(op.TransferID, TransferStatusRefunded)
	}
	
	return nil
}

// processMPCOp processes an MPC operation
func (b *Block) processMPCOp(op *MPCOperation) error {
	switch op.Type {
	case MPCOpTypeKeyGen:
		// Store generated key shares
		return b.vm.mpcManager.StoreKeyGenResult(op.SessionID, op.Result)
		
	case MPCOpTypeSign:
		// Store signature
		return b.vm.mpcManager.StoreSignature(op.SessionID, op.Result)
		
	case MPCOpTypeReshare:
		// Update key shares
		return b.vm.mpcManager.UpdateKeyShares(op.SessionID, op.Result)
	}
	
	return nil
}

// processValidatorOp processes a validator operation
func (b *Block) processValidatorOp(op *ValidatorOperation) error {
	switch op.Type {
	case ValidatorOpTypeAdd:
		return b.vm.validators.AddValidator(op.NodeID, op.Weight, op.StartTime, op.EndTime)
		
	case ValidatorOpTypeRemove:
		return b.vm.validators.RemoveValidator(op.NodeID)
		
	case ValidatorOpTypeNFTStake:
		// Handle NFT-based validator staking
		if op.NFTAssetID != nil {
			return b.vm.validators.AddNFTValidator(op.NodeID, *op.NFTAssetID, op.Weight)
		}
		
	case ValidatorOpTypeNFTUnstake:
		// Handle NFT-based validator unstaking
		if op.NFTAssetID != nil {
			return b.vm.validators.RemoveNFTValidator(op.NodeID, *op.NFTAssetID)
		}
	}
	
	return nil
}

// cleanupOperations cleans up operations from a rejected block
func (b *Block) cleanupOperations() {
	// Remove pending intents
	for _, tx := range b.Body.Transactions {
		if tx.Type == TxTypeTeleportIntent && tx.IntentID != nil {
			b.vm.intentPool.RemoveIntent(*tx.IntentID)
		}
	}
	
	// Clean up MPC sessions
	for _, op := range b.Body.MPCOps {
		b.vm.mpcManager.CleanupSession(op.SessionID)
	}
}

// verifyHeader verifies the block header
func (b *Block) verifyHeader() error {
	// Verify parent exists
	parent, err := b.vm.blockChain.GetBlock(b.ParentID)
	if err != nil {
		return fmt.Errorf("parent block not found: %w", err)
	}
	
	// Verify height
	if b.height != parent.Height()+1 {
		return fmt.Errorf("invalid height: expected %d, got %d", parent.Height()+1, b.height)
	}
	
	// Verify timestamp
	if b.Timestamp <= parent.Time() {
		return fmt.Errorf("timestamp not increasing")
	}
	
	// Verify merkle roots
	if err := b.verifyMerkleRoots(); err != nil {
		return fmt.Errorf("invalid merkle roots: %w", err)
	}
	
	return nil
}

// verifyMPCSignature verifies the MPC signature on the block
func (b *Block) verifyMPCSignature() error {
	// Get current validator set
	validators := b.vm.validators.GetValidatorSet(b.height)
	
	// Check threshold (2/3+ required)
	signerCount := 0
	for i := 0; i < len(validators); i++ {
		byteIndex := i / 8
		bitIndex := uint(i % 8)
		
		if byteIndex < len(b.Header.SignerBitmap) && (b.Header.SignerBitmap[byteIndex]&(1<<bitIndex)) != 0 {
			signerCount++
		}
	}
	
	threshold := (len(validators)*2 + 2) / 3 // 2/3 + 1
	if signerCount < threshold {
		return fmt.Errorf("insufficient signatures: %d < %d", signerCount, threshold)
	}
	
	// Verify MPC signature
	message := b.signingMessage()
	return b.vm.mpcManager.VerifyBlockSignature(message, b.Header.MPCSignature, b.Header.SignerBitmap)
}

// verifyTransactions verifies all transactions in the block
func (b *Block) verifyTransactions() error {
	for i, tx := range b.Body.Transactions {
		if err := b.verifyTransaction(tx); err != nil {
			return fmt.Errorf("transaction %d invalid: %w", i, err)
		}
	}
	return nil
}

// verifyTransaction verifies a single transaction
func (b *Block) verifyTransaction(tx *Transaction) error {
	// Verify signature
	hash := tx.Hash()
	pubKey, err := crypto.SigToPub(hash[:], tx.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}
	
	recoveredAddr := crypto.PubkeyToAddress(*pubKey)
	if recoveredAddr != tx.From {
		return fmt.Errorf("signature mismatch")
	}
	
	// Type-specific validation
	switch tx.Type {
	case TxTypeTeleportIntent:
		if tx.IntentID == nil || tx.SourceChain == nil || tx.DestChain == nil {
			return fmt.Errorf("missing teleport fields")
		}
	}
	
	return nil
}

// verifyTeleportOps verifies all teleport operations
func (b *Block) verifyTeleportOps() error {
	for i, op := range b.Body.TeleportOps {
		// Verify ZK proof
		if len(op.ZKProof) > 0 {
			if err := b.vm.zkVerifier.VerifyProof("teleport", op.ZKProof, nil); err != nil {
				return fmt.Errorf("teleport op %d: invalid ZK proof: %w", i, err)
			}
		}
		
		// Verify validator signatures
		if len(op.ValidatorSigs) > 0 {
			// Would verify threshold signatures
		}
	}
	return nil
}

// verifyStateTransition verifies the state transition
func (b *Block) verifyStateTransition() error {
	// Implementation would verify state root matches expected state after applying all operations
	return nil
}

// verifyMerkleRoots verifies all merkle roots in the header
func (b *Block) verifyMerkleRoots() error {
	// Verify transaction root
	txRoot := b.calculateTxRoot()
	if txRoot != b.Header.TxRoot {
		return fmt.Errorf("invalid transaction root")
	}
	
	// Verify teleport root
	teleportRoot := b.calculateTeleportRoot()
	if teleportRoot != b.Header.TeleportRoot {
		return fmt.Errorf("invalid teleport root")
	}
	
	return nil
}

// calculateTxRoot calculates the transaction merkle root
func (b *Block) calculateTxRoot() common.Hash {
	hashes := make([][]byte, len(b.Body.Transactions))
	for i, tx := range b.Body.Transactions {
		hash := tx.Hash()
		hashes[i] = hash[:]
	}
	return common.BytesToHash(computeMerkleRoot(hashes))
}

// calculateTeleportRoot calculates the teleport operations merkle root
func (b *Block) calculateTeleportRoot() common.Hash {
	hashes := make([][]byte, len(b.Body.TeleportOps))
	for i, op := range b.Body.TeleportOps {
		hash := op.Hash()
		hashes[i] = hash[:]
	}
	return common.BytesToHash(computeMerkleRoot(hashes))
}

// signingMessage returns the message to be signed for the block
func (b *Block) signingMessage() []byte {
	// Create signing message from header fields
	msg := make([]byte, 0, 256)
	msg = append(msg, b.Header.ParentHash.Bytes()...)
	msg = append(msg, b.Header.StateRoot.Bytes()...)
	msg = append(msg, b.Header.TxRoot.Bytes()...)
	msg = append(msg, b.Header.TeleportRoot.Bytes()...)
	
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, b.height)
	msg = append(msg, heightBytes...)
	
	timestampBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timestampBytes, b.Timestamp)
	msg = append(msg, timestampBytes...)
	
	return crypto.Keccak256(msg)
}

// serialize serializes the block
func (b *Block) serialize() ([]byte, error) {
	// Implementation would serialize the block
	return nil, nil
}

// computeMerkleRoot computes the merkle root of a list of hashes
func computeMerkleRoot(hashes [][]byte) []byte {
	if len(hashes) == 0 {
		return make([]byte, 32)
	}
	
	// If there's only one hash, return it
	if len(hashes) == 1 {
		return hashes[0]
	}
	
	// Build the merkle tree level by level
	for len(hashes) > 1 {
		var nextLevel [][]byte
		
		// Process pairs of hashes
		for i := 0; i < len(hashes); i += 2 {
			var combined []byte
			if i+1 < len(hashes) {
				// Concatenate two hashes
				combined = append(hashes[i], hashes[i+1]...)
			} else {
				// Odd number of hashes, duplicate the last one
				combined = append(hashes[i], hashes[i]...)
			}
			// Hash the concatenated value
			hash := hashing.ComputeHash256(combined)
			nextLevel = append(nextLevel, hash)
		}
		
		hashes = nextLevel
	}
	
	return hashes[0]
}

// Hash returns the hash of a transaction
func (tx *Transaction) Hash() common.Hash {
	data := make([]byte, 0, 256)
	data = append(data, byte(tx.Type))
	
	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, tx.Nonce)
	data = append(data, nonceBytes...)
	
	data = append(data, tx.From.Bytes()...)
	data = append(data, tx.To.Bytes()...)
	data = append(data, tx.Value...)
	data = append(data, tx.Data...)
	
	return crypto.Keccak256Hash(data)
}

// Hash returns the hash of a teleport operation
func (op *TeleportOperation) Hash() common.Hash {
	data := make([]byte, 0, 256)
	data = append(data, byte(op.Type))
	data = append(data, op.TransferID[:]...)
	data = append(data, op.IntentID[:]...)
	data = append(data, byte(op.Status))
	
	return crypto.Keccak256Hash(data)
}