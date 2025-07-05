// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/choices"
	"github.com/luxfi/node/utils"
)

// Block represents a block in the ZK UTXO chain
type Block struct {
	ParentID   ids.ID         `json:"parentId"`
	BlockHeight uint64         `json:"height"`
	BlockTimestamp  int64          `json:"timestamp"`
	Txs        []*Transaction `json:"transactions"`
	StateRoot  []byte         `json:"stateRoot"`   // Merkle tree root of UTXO set
	
	// Aggregated proof for the block (optional)
	BlockProof *ZKProof       `json:"blockProof,omitempty"`
	
	// Cached values
	ID_        ids.ID
	bytes      []byte
	status     choices.Status
	vm         *VM
}

// ID returns the block ID
func (b *Block) ID() ids.ID {
	if b.ID_ == ids.Empty {
		b.ID_ = b.computeID()
	}
	return b.ID_
}

// computeID computes the block ID
func (b *Block) computeID() ids.ID {
	h := sha256.New()
	h.Write(b.ParentID[:])
	binary.Write(h, binary.BigEndian, b.Height)
	binary.Write(h, binary.BigEndian, b.Timestamp)
	
	// Include transaction IDs
	for _, tx := range b.Txs {
		txID := tx.ID
		if txID == ids.Empty {
			txID = tx.ComputeID()
		}
		h.Write(txID[:])
	}
	
	// Include state root
	h.Write(b.StateRoot)
	
	// Include block proof if present
	if b.BlockProof != nil {
		h.Write([]byte(b.BlockProof.ProofType))
		h.Write(b.BlockProof.ProofData)
	}
	
	return ids.ID(h.Sum(nil))
}

// Parent returns the parent block ID
func (b *Block) Parent() ids.ID {
	return b.ParentID
}

// Verify verifies the block
func (b *Block) Verify() error {
	// Basic validation
	if b.Height == 0 && b.ParentID != ids.Empty {
		return errInvalidBlock
	}
	
	// Verify timestamp
	if b.Timestamp > time.Now().Unix()+maxClockSkew {
		return errFutureBlock
	}
	
	// Verify each transaction
	for _, tx := range b.Txs {
		if err := tx.ValidateBasic(); err != nil {
			return err
		}
		
		// Verify transaction proof
		if err := b.vm.verifyTransaction(tx); err != nil {
			return err
		}
	}
	
	// Verify block proof if present
	if b.BlockProof != nil {
		if err := b.vm.proofVerifier.VerifyBlockProof(b); err != nil {
			return err
		}
	}
	
	// Verify against parent
	if b.Height > 0 {
		parent, err := b.vm.GetBlock(nil, b.ParentID)
		if err != nil {
			return err
		}
		
		parentBlock := parent.(*Block)
		if b.Height != parentBlock.Height+1 {
			return errInvalidHeight
		}
		
		if b.Timestamp < parentBlock.Timestamp {
			return errInvalidTimestamp
		}
	}
	
	// Verify state root
	expectedRoot, err := b.vm.computeStateRoot(b.Txs)
	if err != nil {
		return err
	}
	
	if !bytes.Equal(b.StateRoot, expectedRoot) {
		return errInvalidStateRoot
	}
	
	return nil
}

// Accept accepts the block
func (b *Block) Accept() error {
	b.status = choices.Accepted
	
	// Update VM state
	b.vm.mu.Lock()
	defer b.vm.mu.Unlock()
	
	b.vm.lastAccepted = b
	b.vm.lastAcceptedID = b.ID()
	
	// Save to database
	if err := b.vm.db.Put(lastAcceptedKey, b.ID()[:]); err != nil {
		return err
	}
	
	// Save block
	blockBytes, err := b.Bytes()
	if err != nil {
		return err
	}
	
	if err := b.vm.db.Put(b.ID()[:], blockBytes); err != nil {
		return err
	}
	
	// Apply transactions to state
	for _, tx := range b.Txs {
		// Add nullifiers to spent set
		for _, nullifier := range tx.Nullifiers {
			if err := b.vm.nullifierDB.MarkNullifierSpent(nullifier, b.Height); err != nil {
				return err
			}
		}
		
		// Add outputs to UTXO set
		for i, output := range tx.Outputs {
			utxo := &UTXO{
				TxID:        tx.ID,
				OutputIndex: uint32(i),
				Commitment:  output.Commitment,
				Ciphertext:  output.EncryptedNote,
				EphemeralPK: output.EphemeralPubKey,
				Height:      b.Height,
			}
			
			if err := b.vm.utxoDB.AddUTXO(utxo); err != nil {
				return err
			}
		}
		
		// Remove from mempool
		b.vm.mempool.RemoveTransaction(tx.ID)
	}
	
	// Update state tree
	if err := b.vm.stateTree.Finalize(b.StateRoot); err != nil {
		return err
	}
	
	// Remove from pending
	delete(b.vm.pendingBlocks, b.ID())
	
	b.vm.log.Info("Block accepted",
		"height", b.Height,
		"id", b.ID().String(),
		"txCount", len(b.Txs),
	)
	
	return nil
}

// Reject rejects the block
func (b *Block) Reject() error {
	b.status = choices.Rejected
	
	// Remove from pending
	b.vm.mu.Lock()
	delete(b.vm.pendingBlocks, b.ID())
	b.vm.mu.Unlock()
	
	// Return transactions to mempool
	for _, tx := range b.Txs {
		b.vm.mempool.AddTransaction(tx)
	}
	
	return nil
}

// Status returns the block status
func (b *Block) Status() choices.Status {
	return b.status
}

// Height returns the block height
func (b *Block) Height() uint64 {
	return b.Height
}

// Timestamp returns the block timestamp
func (b *Block) Timestamp() time.Time {
	return time.Unix(b.Timestamp, 0)
}

// Bytes returns the block bytes
func (b *Block) Bytes() ([]byte, error) {
	if b.bytes != nil {
		return b.bytes, nil
	}
	
	bytes, err := utils.Codec.Marshal(codecVersion, b)
	if err != nil {
		return nil, err
	}
	
	b.bytes = bytes
	return bytes, nil
}

// Genesis represents genesis data
type Genesis struct {
	Timestamp   int64          `json:"timestamp"`
	InitialTxs  []*Transaction `json:"initialTransactions,omitempty"`
	
	// Initial setup parameters
	SetupParams *SetupParams   `json:"setupParams,omitempty"`
}

// SetupParams contains trusted setup parameters
type SetupParams struct {
	// Groth16 CRS
	PowersOfTau       []byte `json:"powersOfTau,omitempty"`
	VerifyingKey      []byte `json:"verifyingKey,omitempty"`
	
	// PLONK setup
	PlonkSRS          []byte `json:"plonkSRS,omitempty"`
	
	// FHE parameters
	FHEPublicParams   []byte `json:"fhePublicParams,omitempty"`
}

// ParseGenesis parses genesis bytes
func ParseGenesis(genesisBytes []byte) (*Genesis, error) {
	var genesis Genesis
	if err := utils.Codec.Unmarshal(genesisBytes, &genesis); err != nil {
		return nil, err
	}
	
	if genesis.Timestamp == 0 {
		genesis.Timestamp = time.Now().Unix()
	}
	
	return &genesis, nil
}

// BlockSummary represents a lightweight block summary
type BlockSummary struct {
	ID           ids.ID   `json:"id"`
	Height       uint64   `json:"height"`
	Timestamp    int64    `json:"timestamp"`
	TxCount      int      `json:"txCount"`
	StateRoot    []byte   `json:"stateRoot"`
}

// ToSummary converts a block to a summary
func (b *Block) ToSummary() *BlockSummary {
	return &BlockSummary{
		ID:        b.ID(),
		Height:    b.Height,
		Timestamp: b.Timestamp,
		TxCount:   len(b.Txs),
		StateRoot: b.StateRoot,
	}
}

const (
	codecVersion = 0
	maxClockSkew = 60 // seconds
)

var (
	errInvalidBlock      = errors.New("invalid block")
	errFutureBlock       = errors.New("block timestamp too far in future")
	errInvalidHeight     = errors.New("invalid block height")
	errInvalidTimestamp  = errors.New("invalid block timestamp")
	errInvalidStateRoot  = errors.New("invalid state root")
)