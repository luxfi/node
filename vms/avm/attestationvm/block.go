// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow/choices"
	"github.com/luxfi/node/utils"
)

// Block represents a block in the attestation chain
type Block struct {
	ParentID     ids.ID         `json:"parentId"`
	Height       uint64         `json:"height"`
	Timestamp    int64          `json:"timestamp"`
	Attestations []*Attestation `json:"attestations"`
	
	// Cached values
	ID_     ids.ID
	bytes   []byte
	status  choices.Status
	vm      *VM
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
	
	for _, att := range b.Attestations {
		attID := att.ComputeID()
		h.Write(attID[:])
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
	
	// Verify each attestation
	for _, att := range b.Attestations {
		if err := b.vm.verifyAttestation(att); err != nil {
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
	
	// Mark attestations as accepted
	for _, att := range b.Attestations {
		if err := b.vm.attestationDB.MarkAttestationAccepted(att.ID); err != nil {
			return err
		}
	}
	
	// Remove from pending
	delete(b.vm.pendingBlocks, b.ID())
	
	b.vm.log.Info("Block accepted",
		"height", b.Height,
		"id", b.ID().String(),
		"attestations", len(b.Attestations),
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
	
	return nil
}

// Status returns the block status
func (b *Block) Status() choices.Status {
	return b.status
}

// Height returns the block height
func (b *Block) BlockHeight() uint64 {
	return b.Height
}

// Timestamp returns the block timestamp
func (b *Block) BlockTimestamp() time.Time {
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
	Timestamp           int64          `json:"timestamp"`
	InitialOracles      []*OracleInfo  `json:"initialOracles,omitempty"`
	InitialAttestations []*Attestation `json:"initialAttestations,omitempty"`
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

const (
	codecVersion = 0
	maxClockSkew = 60 // seconds
)

var (
	errInvalidBlock     = errors.New("invalid block")
	errFutureBlock      = errors.New("block timestamp too far in future")
	errInvalidHeight    = errors.New("invalid block height")
	errInvalidTimestamp = errors.New("invalid block timestamp")
)