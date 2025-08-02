// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"encoding/binary"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/codec"
	"github.com/luxfi/node/v2/utils/hashing"
)

// ConsensusBlock represents a network-wide consensus block containing operations from all chains
type ConsensusBlock struct {
	// Block metadata
	Height    uint64    `serialize:"true"`
	Timestamp time.Time `serialize:"true"`
	ParentID  ids.ID    `serialize:"true"`
	
	// Chain operations - Operations from each of the 8 chains
	AChainOps []Operation `serialize:"true"` // AI operations
	BChainOps []Operation `serialize:"true"` // Bridge operations
	CChainOps []Operation `serialize:"true"` // EVM operations
	MChainOps []Operation `serialize:"true"` // MPC operations
	PChainOps []Operation `serialize:"true"` // Platform operations
	QChainOps []Operation `serialize:"true"` // Quantum operations
	XChainOps []Operation `serialize:"true"` // Exchange operations
	ZChainOps []Operation `serialize:"true"` // ZK operations
	
	// Finality requirements
	RequiredPChainBLS      bool `serialize:"true"`
	RequiredQChainRingtail bool `serialize:"true"`
	
	// Computed fields (not serialized)
	id    ids.ID
	bytes []byte
}

// ID returns the block ID
func (cb *ConsensusBlock) ID() ids.ID {
	if cb.id == ids.Empty {
		cb.id = cb.calculateID()
	}
	return cb.id
}

// calculateID computes the block ID from its contents
func (cb *ConsensusBlock) calculateID() ids.ID {
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, cb.Height)
	
	data := append(heightBytes, cb.ParentID[:]...)
	data = append(data, []byte(cb.Timestamp.String())...)
	
	// Include operations hash from each chain
	data = append(data, cb.hashOperations(cb.AChainOps)...)
	data = append(data, cb.hashOperations(cb.BChainOps)...)
	data = append(data, cb.hashOperations(cb.CChainOps)...)
	data = append(data, cb.hashOperations(cb.MChainOps)...)
	data = append(data, cb.hashOperations(cb.PChainOps)...)
	data = append(data, cb.hashOperations(cb.QChainOps)...)
	data = append(data, cb.hashOperations(cb.XChainOps)...)
	data = append(data, cb.hashOperations(cb.ZChainOps)...)
	
	return hashing.ComputeHash256Array(data)
}

// hashOperations computes a merkle root of operations
func (cb *ConsensusBlock) hashOperations(ops []Operation) []byte {
	if len(ops) == 0 {
		return make([]byte, 32)
	}
	
	hashes := make([][]byte, len(ops))
	for i, op := range ops {
		hashes[i] = op.Hash()
	}
	
	// Simple merkle root calculation
	for len(hashes) > 1 {
		newHashes := make([][]byte, (len(hashes)+1)/2)
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {
				combined := append(hashes[i], hashes[i+1]...)
				newHashes[i/2] = hashing.ComputeHash256(combined)
			} else {
				newHashes[i/2] = hashes[i]
			}
		}
		hashes = newHashes
	}
	
	return hashes[0]
}

// Bytes returns the serialized block
func (cb *ConsensusBlock) Bytes() []byte {
	if cb.bytes == nil {
		manager := codec.NewDefaultManager()
		bytes, _ := manager.Marshal(0, cb)
		cb.bytes = bytes
	}
	return cb.bytes
}

// GetAllOperations returns all operations in the block
func (cb *ConsensusBlock) GetAllOperations() []Operation {
	var allOps []Operation
	allOps = append(allOps, cb.AChainOps...)
	allOps = append(allOps, cb.BChainOps...)
	allOps = append(allOps, cb.CChainOps...)
	allOps = append(allOps, cb.MChainOps...)
	allOps = append(allOps, cb.PChainOps...)
	allOps = append(allOps, cb.QChainOps...)
	allOps = append(allOps, cb.XChainOps...)
	allOps = append(allOps, cb.ZChainOps...)
	return allOps
}

// GetOperationsByChain returns operations for a specific chain
func (cb *ConsensusBlock) GetOperationsByChain(chainID ids.ID) []Operation {
	// This would use a map in production, but for clarity:
	switch chainID.String()[:1] {
	case "a":
		return cb.AChainOps
	case "b":
		return cb.BChainOps
	case "c":
		return cb.CChainOps
	case "m":
		return cb.MChainOps
	case "p":
		return cb.PChainOps
	case "q":
		return cb.QChainOps
	case "x":
		return cb.XChainOps
	case "z":
		return cb.ZChainOps
	default:
		return nil
	}
}

// ConsensusBlockBuilder builds consensus blocks
type ConsensusBlockBuilder struct {
	chainOperations map[ids.ID][]Operation
	height          uint64
	parentID        ids.ID
}

// NewConsensusBlockBuilder creates a new builder
func NewConsensusBlockBuilder(height uint64, parentID ids.ID) *ConsensusBlockBuilder {
	return &ConsensusBlockBuilder{
		chainOperations: make(map[ids.ID][]Operation),
		height:          height,
		parentID:        parentID,
	}
}

// AddOperations adds operations from a chain
func (cbb *ConsensusBlockBuilder) AddOperations(chainID ids.ID, ops []Operation) {
	cbb.chainOperations[chainID] = append(cbb.chainOperations[chainID], ops...)
}

// Build creates the consensus block
func (cbb *ConsensusBlockBuilder) Build() *ConsensusBlock {
	block := &ConsensusBlock{
		Height:                 cbb.height,
		Timestamp:              time.Now(),
		ParentID:               cbb.parentID,
		RequiredPChainBLS:      true,
		RequiredQChainRingtail: true,
	}
	
	// Assign operations to appropriate chains
	for chainID, ops := range cbb.chainOperations {
		switch chainID.String()[:1] {
		case "a":
			block.AChainOps = ops
		case "b":
			block.BChainOps = ops
		case "c":
			block.CChainOps = ops
		case "m":
			block.MChainOps = ops
		case "p":
			block.PChainOps = ops
		case "q":
			block.QChainOps = ops
		case "x":
			block.XChainOps = ops
		case "z":
			block.ZChainOps = ops
		}
	}
	
	return block
}

// Hash returns the hash of an operation
func (op *Operation) Hash() []byte {
	data := append(op.ChainID[:], []byte(op.OperationType)...)
	data = append(data, op.Payload...)
	data = append(data, op.Signature...)
	data = append(data, []byte(op.Timestamp.String())...)
	return hashing.ComputeHash256(data)
}

// ConsensusRound represents a complete consensus round
type ConsensusRound struct {
	Height         uint64
	Block          *ConsensusBlock
	PChainFinality bool
	QChainFinality bool
	StartTime      time.Time
	EndTime        time.Time
}

// Duration returns how long the consensus round took
func (cr *ConsensusRound) Duration() time.Duration {
	if cr.EndTime.IsZero() {
		return time.Since(cr.StartTime)
	}
	return cr.EndTime.Sub(cr.StartTime)
}

// IsFinalized returns true if the round achieved dual finality
func (cr *ConsensusRound) IsFinalized() bool {
	return cr.PChainFinality && cr.QChainFinality
}