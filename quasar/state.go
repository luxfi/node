// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"errors"
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/choices"
)

// State represents the consensus state
type State interface {
	// GetBlock returns the block with the given ID
	GetBlock(blkID ids.ID) (Block, error)

	// AddBlock adds a block to the state
	AddBlock(blk Block) error

	// RemoveBlock removes a block from the state
	RemoveBlock(blkID ids.ID) error

	// GetVertex returns the vertex with the given ID
	GetVertex(vtxID ids.ID) (Vertex, error)

	// AddVertex adds a vertex to the state
	AddVertex(vtx Vertex) error

	// RemoveVertex removes a vertex from the state
	RemoveVertex(vtxID ids.ID) error

	// SetPreference sets the preferred frontier
	SetPreference(ids []ids.ID) error

	// Preference returns the current preferred frontier
	Preference() []ids.ID

	// IsAccepted returns true if the block/vertex is accepted
	IsAccepted(id ids.ID) (bool, error)

	// LastAccepted returns the last accepted block/vertex
	LastAccepted() (ids.ID, error)

	// SetLastAccepted sets the last accepted block/vertex
	SetLastAccepted(id ids.ID) error

	// IsQuantum returns true if the block/vertex has quantum finality
	IsQuantum(id ids.ID) (bool, error)

	// SetQuantum marks a block/vertex as having quantum finality
	SetQuantum(id ids.ID) error
}

var (
	// ErrNotFound is returned when an element is not found
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists is returned when an element already exists
	ErrAlreadyExists = errors.New("already exists")

	// ErrInvalidStatus is returned when a status transition is invalid
	ErrInvalidStatus = errors.New("invalid status transition")

	// ErrMissingCertificate is returned when a required certificate is missing
	ErrMissingCertificate = errors.New("missing certificate")
)

// MemoryState is an in-memory implementation of State
type MemoryState struct {
	blocks       map[ids.ID]Block
	vertices     map[ids.ID]Vertex
	statuses     map[ids.ID]choices.Status
	quantum      map[ids.ID]bool
	preference   []ids.ID
	lastAccepted ids.ID
}

// NewMemoryState creates a new in-memory state
func NewMemoryState() *MemoryState {
	return &MemoryState{
		blocks:   make(map[ids.ID]Block),
		vertices: make(map[ids.ID]Vertex),
		statuses: make(map[ids.ID]choices.Status),
		quantum:  make(map[ids.ID]bool),
	}
}

// GetBlock returns the block with the given ID
func (s *MemoryState) GetBlock(blkID ids.ID) (Block, error) {
	blk, ok := s.blocks[blkID]
	if !ok {
		return nil, fmt.Errorf("%w: block %s", ErrNotFound, blkID)
	}
	return blk, nil
}

// AddBlock adds a block to the state
func (s *MemoryState) AddBlock(blk Block) error {
	blkID := blk.ID()
	if _, exists := s.blocks[blkID]; exists {
		return fmt.Errorf("%w: block %s", ErrAlreadyExists, blkID)
	}
	s.blocks[blkID] = blk
	s.statuses[blkID] = blk.Status()
	return nil
}

// RemoveBlock removes a block from the state
func (s *MemoryState) RemoveBlock(blkID ids.ID) error {
	if _, ok := s.blocks[blkID]; !ok {
		return fmt.Errorf("%w: block %s", ErrNotFound, blkID)
	}
	delete(s.blocks, blkID)
	delete(s.statuses, blkID)
	delete(s.quantum, blkID)
	return nil
}

// GetVertex returns the vertex with the given ID
func (s *MemoryState) GetVertex(vtxID ids.ID) (Vertex, error) {
	vtx, ok := s.vertices[vtxID]
	if !ok {
		return nil, fmt.Errorf("%w: vertex %s", ErrNotFound, vtxID)
	}
	return vtx, nil
}

// AddVertex adds a vertex to the state
func (s *MemoryState) AddVertex(vtx Vertex) error {
	vtxID := vtx.ID()
	if _, exists := s.vertices[vtxID]; exists {
		return fmt.Errorf("%w: vertex %s", ErrAlreadyExists, vtxID)
	}
	s.vertices[vtxID] = vtx
	s.statuses[vtxID] = vtx.Status()
	return nil
}

// RemoveVertex removes a vertex from the state
func (s *MemoryState) RemoveVertex(vtxID ids.ID) error {
	if _, ok := s.vertices[vtxID]; !ok {
		return fmt.Errorf("%w: vertex %s", ErrNotFound, vtxID)
	}
	delete(s.vertices, vtxID)
	delete(s.statuses, vtxID)
	delete(s.quantum, vtxID)
	return nil
}

// SetPreference sets the preferred frontier
func (s *MemoryState) SetPreference(ids []ids.ID) error {
	s.preference = ids
	return nil
}

// Preference returns the current preferred frontier
func (s *MemoryState) Preference() []ids.ID {
	return s.preference
}

// IsAccepted returns true if the block/vertex is accepted
func (s *MemoryState) IsAccepted(id ids.ID) (bool, error) {
	status, ok := s.statuses[id]
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return status.IsAccepted(), nil
}

// LastAccepted returns the last accepted block/vertex
func (s *MemoryState) LastAccepted() (ids.ID, error) {
	if s.lastAccepted == ids.Empty {
		return ids.Empty, ErrNotFound
	}
	return s.lastAccepted, nil
}

// SetLastAccepted sets the last accepted block/vertex
func (s *MemoryState) SetLastAccepted(id ids.ID) error {
	s.lastAccepted = id
	return nil
}

// IsQuantum returns true if the block/vertex has quantum finality
func (s *MemoryState) IsQuantum(id ids.ID) (bool, error) {
	if _, ok := s.statuses[id]; !ok {
		return false, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return s.quantum[id], nil
}

// SetQuantum marks a block/vertex as having quantum finality
func (s *MemoryState) SetQuantum(id ids.ID) error {
	if _, ok := s.statuses[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	s.quantum[id] = true
	return nil
}