// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package servicenodevm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/luxfi/consensus/core/choices"
	"github.com/luxfi/consensus/engine/dag/vertex"
	"github.com/luxfi/ids"
)

var _ vertex.DAGVM = (*VM)(nil)

// NodeEpochKey is the conflict key for the ServiceNode VM: (nodeID, epoch).
// Two vertices conflict iff they touch the same node in the same epoch.
type NodeEpochKey struct {
	NodeID ids.NodeID
	Epoch  uint64
}

// ServiceNodeVertex represents a DAG vertex in the ServiceNode chain.
type ServiceNodeVertex struct {
	id      ids.ID
	bytes   []byte
	height  uint64
	epoch   uint32
	parents []ids.ID
	txIDs   []ids.ID
	status  choices.Status

	registrations []*RegistrationTx
	exits         []*ExitTx
	slashes       []*SlashTx
	proofs        []*UptimeProof
	commits       []*StorageCommitment
	keys          []NodeEpochKey
	vm            *VM
}

func (v *ServiceNodeVertex) ID() ids.ID          { return v.id }
func (v *ServiceNodeVertex) Bytes() []byte        { return v.bytes }
func (v *ServiceNodeVertex) Height() uint64       { return v.height }
func (v *ServiceNodeVertex) Epoch() uint32        { return v.epoch }
func (v *ServiceNodeVertex) Parents() []ids.ID    { return v.parents }
func (v *ServiceNodeVertex) Txs() []ids.ID        { return v.txIDs }
func (v *ServiceNodeVertex) Status() choices.Status { return v.status }

func (v *ServiceNodeVertex) Verify(ctx context.Context) error {
	for _, reg := range v.registrations {
		if reg.StakeAmount == 0 {
			return errors.New("registration missing stake")
		}
		if len(reg.PublicKey) == 0 {
			return errors.New("registration missing public key")
		}
	}
	return nil
}

func (v *ServiceNodeVertex) Accept(ctx context.Context) error {
	v.status = choices.Accepted

	v.vm.mu.Lock()
	defer v.vm.mu.Unlock()

	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	id := v.ID()
	if err := v.vm.db.Put(id[:], b); err != nil {
		return err
	}
	if err := v.vm.db.Put(lastAcceptedKey, id[:]); err != nil {
		return err
	}
	v.vm.lastAcceptedID = id
	delete(v.vm.pendingBlocks, id)

	// Clear consumed pending txs
	v.vm.pendingRegs = v.vm.pendingRegs[:0]
	v.vm.pendingExits = v.vm.pendingExits[:0]
	v.vm.pendingSlashes = v.vm.pendingSlashes[:0]
	v.vm.pendingProofs = v.vm.pendingProofs[:0]
	v.vm.pendingCommits = v.vm.pendingCommits[:0]

	return nil
}

func (v *ServiceNodeVertex) Reject(ctx context.Context) error {
	v.status = choices.Rejected
	v.vm.mu.Lock()
	delete(v.vm.pendingBlocks, v.id)
	v.vm.mu.Unlock()
	return nil
}

// conflictKeySet returns the set of NodeEpochKeys for conflict detection.
func (v *ServiceNodeVertex) conflictKeySet() map[NodeEpochKey]struct{} {
	s := make(map[NodeEpochKey]struct{}, len(v.keys))
	for _, k := range v.keys {
		s[k] = struct{}{}
	}
	return s
}

// Conflicts returns true if this vertex and other touch the same (nodeID, epoch).
func (v *ServiceNodeVertex) Conflicts(other *ServiceNodeVertex) bool {
	ours := v.conflictKeySet()
	for _, k := range other.keys {
		if _, ok := ours[k]; ok {
			return true
		}
	}
	return false
}

// ConflictsVertex performs the same check against the vertex.Vertex interface.
func (v *ServiceNodeVertex) ConflictsVertex(other vertex.Vertex) bool {
	ov, ok := other.(*ServiceNodeVertex)
	if !ok {
		return false
	}
	return v.Conflicts(ov)
}

// extractNodeEpochKeys derives conflict keys from all transaction types in the vertex.
func extractNodeEpochKeys(
	regs []*RegistrationTx,
	exits []*ExitTx,
	slashes []*SlashTx,
	proofs []*UptimeProof,
	commits []*StorageCommitment,
	epochID uint64,
) []NodeEpochKey {
	seen := make(map[NodeEpochKey]struct{})
	var keys []NodeEpochKey

	add := func(nodeID ids.NodeID, epoch uint64) {
		k := NodeEpochKey{NodeID: nodeID, Epoch: epoch}
		if _, dup := seen[k]; !dup {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}

	for _, r := range regs {
		add(r.NodeID, epochID)
	}
	for _, e := range exits {
		add(e.NodeID, epochID)
	}
	for _, s := range slashes {
		add(s.NodeID, epochID)
	}
	for _, p := range proofs {
		add(p.NodeID, p.EpochID)
	}
	for _, c := range commits {
		add(c.NodeID, c.EpochID)
	}

	return keys
}

func (v *ServiceNodeVertex) computeID() ids.ID {
	h := sha256.New()
	binary.Write(h, binary.BigEndian, v.height)
	binary.Write(h, binary.BigEndian, v.epoch)
	for _, p := range v.parents {
		h.Write(p[:])
	}
	for _, k := range v.keys {
		h.Write(k.NodeID[:])
		binary.Write(h, binary.BigEndian, k.Epoch)
	}
	return ids.ID(h.Sum(nil))
}

// BuildVertex creates a vertex from pending registrations, exits, proofs, etc.
func (vm *VM) BuildVertex(ctx context.Context) (vertex.Vertex, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if vm.lastAccepted == nil {
		return nil, errors.New("no parent block")
	}

	totalPending := len(vm.pendingRegs) + len(vm.pendingExits) +
		len(vm.pendingSlashes) + len(vm.pendingProofs) + len(vm.pendingCommits)
	if totalPending == 0 {
		return nil, errors.New("no pending transactions")
	}

	epochID := vm.lastAccepted.EpochID
	keys := extractNodeEpochKeys(
		vm.pendingRegs, vm.pendingExits, vm.pendingSlashes,
		vm.pendingProofs, vm.pendingCommits, epochID,
	)

	// Compute tx IDs from all pending items
	var txIDs []ids.ID
	for _, r := range vm.pendingRegs {
		h := sha256.New()
		h.Write(r.NodeID[:])
		h.Write(r.PublicKey)
		txIDs = append(txIDs, ids.ID(h.Sum(nil)))
	}
	for _, e := range vm.pendingExits {
		h := sha256.New()
		h.Write(e.NodeID[:])
		binary.Write(h, binary.BigEndian, e.Timestamp.Unix())
		txIDs = append(txIDs, ids.ID(h.Sum(nil)))
	}
	for _, s := range vm.pendingSlashes {
		h := sha256.New()
		h.Write(s.NodeID[:])
		h.Write([]byte(s.Reason))
		txIDs = append(txIDs, ids.ID(h.Sum(nil)))
	}
	for _, p := range vm.pendingProofs {
		txIDs = append(txIDs, ids.ID(p.Hash()))
	}
	for _, c := range vm.pendingCommits {
		txIDs = append(txIDs, ids.ID(c.Hash()))
	}

	v := &ServiceNodeVertex{
		height:        vm.lastAccepted.BlockHeight + 1,
		epoch:         uint32(epochID),
		parents:       []ids.ID{vm.lastAcceptedID},
		txIDs:         txIDs,
		registrations: vm.pendingRegs,
		exits:         vm.pendingExits,
		slashes:       vm.pendingSlashes,
		proofs:        vm.pendingProofs,
		commits:       vm.pendingCommits,
		keys:          keys,
		status:        choices.Processing,
		vm:            vm,
	}
	v.id = v.computeID()
	v.bytes, _ = json.Marshal(v)
	return v, nil
}

// ParseVertex deserializes a vertex from bytes.
func (vm *VM) ParseVertex(ctx context.Context, b []byte) (vertex.Vertex, error) {
	v := &ServiceNodeVertex{vm: vm}
	if err := json.Unmarshal(b, v); err != nil {
		return nil, err
	}
	v.keys = extractNodeEpochKeys(
		v.registrations, v.exits, v.slashes,
		v.proofs, v.commits, uint64(v.epoch),
	)
	v.id = v.computeID()
	v.bytes = b
	return v, nil
}
