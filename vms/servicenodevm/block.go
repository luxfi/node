// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package servicenodevm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"time"

	"github.com/luxfi/consensus/core/choices"
	"github.com/luxfi/ids"
)

// Block represents a block in the ServiceNodeVM chain
type Block struct {
	ParentID_       ids.ID     `json:"parentId"`
	BlockHeight     uint64     `json:"height"`
	BlockTimestamp  int64      `json:"timestamp"`
	EpochID         uint64     `json:"epochId"`

	// Transactions
	Registrations   []*RegistrationTx    `json:"registrations,omitempty"`
	Exits           []*ExitTx            `json:"exits,omitempty"`
	Slashes         []*SlashTx           `json:"slashes,omitempty"`
	ChallengeResults []*Challenge        `json:"challengeResults,omitempty"`
	UptimeProofs    []*UptimeProof       `json:"uptimeProofs,omitempty"`
	StorageCommits  []*StorageCommitment `json:"storageCommits,omitempty"`

	// State roots
	RegistryRoot    [32]byte `json:"registryRoot"`
	AssignmentRoot  [32]byte `json:"assignmentRoot"`
	ChallengeRoot   [32]byte `json:"challengeRoot"`

	// Cached values
	id     ids.ID
	bytes  []byte
	status choices.Status
	vm     interface{} // *VM, avoiding import cycle
}

// ID returns the block ID
func (b *Block) ID() ids.ID {
	if b.id == ids.Empty {
		b.id = b.computeID()
	}
	return b.id
}

// computeID computes the block ID
func (b *Block) computeID() ids.ID {
	h := sha256.New()
	h.Write(b.ParentID_[:])
	binary.Write(h, binary.BigEndian, b.BlockHeight)
	binary.Write(h, binary.BigEndian, b.BlockTimestamp)
	binary.Write(h, binary.BigEndian, b.EpochID)
	h.Write(b.RegistryRoot[:])
	h.Write(b.AssignmentRoot[:])
	h.Write(b.ChallengeRoot[:])

	// Include transaction counts for uniqueness
	binary.Write(h, binary.BigEndian, uint32(len(b.Registrations)))
	binary.Write(h, binary.BigEndian, uint32(len(b.Exits)))
	binary.Write(h, binary.BigEndian, uint32(len(b.Slashes)))

	return ids.ID(h.Sum(nil))
}

// ParentID returns the parent block ID
func (b *Block) ParentID() ids.ID {
	return b.ParentID_
}

// Parent is an alias for ParentID
func (b *Block) Parent() ids.ID {
	return b.ParentID_
}

// Height returns the block height
func (b *Block) Height() uint64 {
	return b.BlockHeight
}

// Timestamp returns the block timestamp
func (b *Block) Timestamp() time.Time {
	return time.Unix(b.BlockTimestamp, 0)
}

// Status returns the block status
func (b *Block) Status() uint8 {
	return uint8(b.status)
}

// Verify verifies the block
func (b *Block) Verify(ctx context.Context) error {
	// Verify height
	if b.BlockHeight == 0 && b.ParentID_ != ids.Empty {
		return errors.New("invalid genesis block")
	}

	// Verify timestamp is not too far in future
	if b.BlockTimestamp > time.Now().Unix()+60 {
		return errors.New("block timestamp too far in future")
	}

	// Verify all registrations have minimum stake
	for _, reg := range b.Registrations {
		if reg.StakeAmount == 0 {
			return errors.New("registration missing stake")
		}
		if len(reg.PublicKey) == 0 {
			return errors.New("registration missing public key")
		}
	}

	return nil
}

// Accept accepts the block
func (b *Block) Accept(ctx context.Context) error {
	b.status = choices.Accepted
	return nil
}

// Reject rejects the block
func (b *Block) Reject(ctx context.Context) error {
	b.status = choices.Rejected
	return nil
}

// Bytes returns the serialized block
func (b *Block) Bytes() []byte {
	if b.bytes != nil {
		return b.bytes
	}

	bytes, err := json.Marshal(b)
	if err != nil {
		return nil
	}

	b.bytes = bytes
	return bytes
}

// SetVM sets the VM reference
func (b *Block) SetVM(vm interface{}) {
	b.vm = vm
}

// SetStatus sets the block status
func (b *Block) SetStatus(status choices.Status) {
	b.status = status
}

// Hash returns the block hash
func (b *Block) Hash() [32]byte {
	id := b.ID()
	var hash [32]byte
	copy(hash[:], id[:])
	return hash
}
