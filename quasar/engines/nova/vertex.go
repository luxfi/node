// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package nova

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/choices"
	"github.com/luxfi/node/consensus/engine/dag"
	"github.com/luxfi/node/utils/set"
)

// FlareVertex implements the Vertex interface for flare consensus
// (Previously a concrete implementation of avalanche.Vertex)
type FlareVertex struct {

	id           ids.ID
	parentIDs    []ids.ID
	height       uint64
	timestamp    time.Time
	bytes        []byte
	status       choices.Status
	txs          []ids.ID
	flareScore   uint64
	photonCount  int
	conflictSet  ids.ID
}

// NewFlareVertex creates a new flare vertex
func NewFlareVertex(
	id ids.ID,
	parentIDs []ids.ID,
	height uint64,
	timestamp time.Time,
	bytes []byte,
	txs []ids.ID,
) *FlareVertex {
	return &FlareVertex{
		id:        id,
		parentIDs: parentIDs,
		height:    height,
		timestamp: timestamp,
		bytes:     bytes,
		txs:       txs,
		status:    choices.Processing,
	}
}

// ID returns the vertex ID as string
func (v *FlareVertex) ID() string {
	return v.id.String()
}

// Vertex returns the unique ID of this vertex
func (v *FlareVertex) Vertex() ids.ID {
	return v.id
}

// Parents returns the parent vertex IDs
func (v *FlareVertex) Parents() []ids.ID {
	return v.parentIDs
}

// Height returns the vertex height
func (v *FlareVertex) Height() uint64 {
	return v.height
}

// Timestamp returns the vertex timestamp
func (v *FlareVertex) Timestamp() time.Time {
	return v.timestamp
}

// Epoch returns the epoch of this vertex
func (v *FlareVertex) Epoch() uint32 {
	// Calculate epoch based on height
	return uint32(v.height / 1000)
}

// Bytes returns the vertex bytes
func (v *FlareVertex) Bytes() []byte {
	return v.bytes
}

// Status returns the vertex status
func (v *FlareVertex) Status() choices.Status {
	return v.status
}

// Txs returns the transactions in this vertex
func (v *FlareVertex) Txs() []ids.ID {
	return v.txs
}

// FlareHeight returns the vertex's height in the DAG
func (v *FlareVertex) FlareHeight() uint64 {
	return v.height
}

// FlareScore returns the vertex's consensus score
func (v *FlareVertex) FlareScore() uint64 {
	return v.flareScore
}

// Photons returns the number of photon queries
func (v *FlareVertex) Photons() int {
	return v.photonCount
}

// ConflictSet returns the conflict set this vertex belongs to
func (v *FlareVertex) ConflictSet() ids.ID {
	return v.conflictSet
}

// Accept marks the vertex as accepted (finalized in the DAG)
func (v *FlareVertex) Accept() error {
	if v.status == choices.Accepted {
		return fmt.Errorf("vertex %s already accepted", v.id)
	}
	v.status = choices.Accepted
	return nil
}

// Reject marks the vertex as rejected (excluded from the DAG)
func (v *FlareVertex) Reject() error {
	if v.status == choices.Rejected {
		return fmt.Errorf("vertex %s already rejected", v.id)
	}
	v.status = choices.Rejected
	return nil
}

// Verify ensures the vertex is valid according to flare rules
func (v *FlareVertex) Verify(ctx context.Context) error {
	// Basic verification
	if v.height == 0 && len(v.parentIDs) > 0 {
		return fmt.Errorf("genesis vertex must have no parents")
	}
	if v.height > 0 && len(v.parentIDs) == 0 {
		return fmt.Errorf("non-genesis vertex must have parents")
	}
	
	// Check for duplicate parents
	parentSet := set.NewSet[ids.ID](len(v.parentIDs))
	for _, parent := range v.parentIDs {
		if parentSet.Contains(parent) {
			return fmt.Errorf("duplicate parent %s", parent)
		}
		parentSet.Add(parent)
	}
	
	return nil
}

// IncrementPhotons increments the photon query count
func (v *FlareVertex) IncrementPhotons() {
	v.photonCount++
}

// UpdateFlareScore updates the consensus score
func (v *FlareVertex) UpdateFlareScore(score uint64) {
	v.flareScore = score
}

// SetConflictSet sets the conflict set ID
func (v *FlareVertex) SetConflictSet(conflictSetID ids.ID) {
	v.conflictSet = conflictSetID
}

// FlareVertexWrapper wraps an existing dag.Vertex for flare consensus
type FlareVertexWrapper struct {
	wrappedVertex dag.Vertex
	flareHeight   uint64
	flareScore    uint64
	photonCount   int
	conflictSet   ids.ID
}

// WrapVertex wraps an existing dag vertex for flare consensus
func WrapVertex(vertex dag.Vertex, height uint64) *FlareVertexWrapper {
	return &FlareVertexWrapper{
		wrappedVertex: vertex,
		flareHeight:   height,
	}
}

// Implement dag.Vertex interface for wrapper

// ID returns the unique ID of this element
func (w *FlareVertexWrapper) ID() string {
	return w.wrappedVertex.ID()
}

// Accept accepts this element and changes its status to Accepted
func (w *FlareVertexWrapper) Accept() error {
	return w.wrappedVertex.Accept()
}

// Reject rejects this element and changes its status to Rejected
func (w *FlareVertexWrapper) Reject() error {
	return w.wrappedVertex.Reject()
}

// Status returns the current status
func (w *FlareVertexWrapper) Status() choices.Status {
	return w.wrappedVertex.Status()
}

// Vertex returns the unique ID of this vertex
func (w *FlareVertexWrapper) Vertex() ids.ID {
	return w.wrappedVertex.Vertex()
}

// Parents returns the parents of this vertex
func (w *FlareVertexWrapper) Parents() []ids.ID {
	return w.wrappedVertex.Parents()
}

// Height returns the height of this vertex
func (w *FlareVertexWrapper) Height() uint64 {
	return w.wrappedVertex.Height()
}

// Epoch returns the epoch of this vertex
func (w *FlareVertexWrapper) Epoch() uint32 {
	return w.wrappedVertex.Epoch()
}

// Verify that this vertex is valid
func (w *FlareVertexWrapper) Verify(ctx context.Context) error {
	return w.wrappedVertex.Verify(ctx)
}

// Bytes returns the byte representation of this vertex
func (w *FlareVertexWrapper) Bytes() []byte {
	return w.wrappedVertex.Bytes()
}

// FlareHeight returns the vertex's height in the DAG
func (w *FlareVertexWrapper) FlareHeight() uint64 {
	return w.flareHeight
}

// FlareScore returns the vertex's consensus score
func (w *FlareVertexWrapper) FlareScore() uint64 {
	return w.flareScore
}

// Photons returns the number of photon queries
func (w *FlareVertexWrapper) Photons() int {
	return w.photonCount
}

// ConflictSet returns the conflict set this vertex belongs to
func (w *FlareVertexWrapper) ConflictSet() ids.ID {
	return w.conflictSet
}