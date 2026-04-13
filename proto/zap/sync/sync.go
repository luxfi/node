// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package sync provides sync types for merkledb.
// This is the ZAP-based implementation (zero protobuf).
package sync

// Key represents a merkle tree key
type Key struct {
	Length uint64
	Value  []byte
}

func (k *Key) GetLength() uint64 {
	if k != nil {
		return k.Length
	}
	return 0
}

func (k *Key) GetValue() []byte {
	if k != nil {
		return k.Value
	}
	return nil
}

// MaybeBytes represents an optional byte slice
type MaybeBytes struct {
	Value     []byte
	IsNothing bool
}

func (m *MaybeBytes) GetValue() []byte {
	if m != nil {
		return m.Value
	}
	return nil
}

func (m *MaybeBytes) GetIsNothing() bool {
	if m != nil {
		return m.IsNothing
	}
	return false
}

// ProofNode represents a node in a merkle proof
type ProofNode struct {
	Key         *Key
	ValueOrHash *MaybeBytes
	Children    map[uint32][]byte
}

func (p *ProofNode) GetKey() *Key {
	if p != nil {
		return p.Key
	}
	return nil
}

func (p *ProofNode) GetValueOrHash() *MaybeBytes {
	if p != nil {
		return p.ValueOrHash
	}
	return nil
}

func (p *ProofNode) GetChildren() map[uint32][]byte {
	if p != nil {
		return p.Children
	}
	return nil
}

// Proof represents a merkle proof
type Proof struct {
	Key   []byte
	Value *MaybeBytes
	Proof []*ProofNode
}

func (p *Proof) GetKey() []byte {
	if p != nil {
		return p.Key
	}
	return nil
}

func (p *Proof) GetValue() *MaybeBytes {
	if p != nil {
		return p.Value
	}
	return nil
}

func (p *Proof) GetProof() []*ProofNode {
	if p != nil {
		return p.Proof
	}
	return nil
}

// KeyValue represents a key-value pair
type KeyValue struct {
	Key   []byte
	Value []byte
}

func (kv *KeyValue) GetKey() []byte {
	if kv != nil {
		return kv.Key
	}
	return nil
}

func (kv *KeyValue) GetValue() []byte {
	if kv != nil {
		return kv.Value
	}
	return nil
}

// KeyChange represents a change to a key
type KeyChange struct {
	Key   []byte
	Value *MaybeBytes
}

func (kc *KeyChange) GetKey() []byte {
	if kc != nil {
		return kc.Key
	}
	return nil
}

func (kc *KeyChange) GetValue() *MaybeBytes {
	if kc != nil {
		return kc.Value
	}
	return nil
}

// RangeProof represents a range proof
type RangeProof struct {
	StartProof []*ProofNode
	EndProof   []*ProofNode
	KeyValues  []*KeyValue
}

func (rp *RangeProof) GetStartProof() []*ProofNode {
	if rp != nil {
		return rp.StartProof
	}
	return nil
}

func (rp *RangeProof) GetEndProof() []*ProofNode {
	if rp != nil {
		return rp.EndProof
	}
	return nil
}

func (rp *RangeProof) GetKeyValues() []*KeyValue {
	if rp != nil {
		return rp.KeyValues
	}
	return nil
}

// ChangeProof represents a change proof
type ChangeProof struct {
	StartProof     []*ProofNode
	EndProof       []*ProofNode
	KeyChanges     []*KeyChange
	HadRootsInHistory bool
}

func (cp *ChangeProof) GetStartProof() []*ProofNode {
	if cp != nil {
		return cp.StartProof
	}
	return nil
}

func (cp *ChangeProof) GetEndProof() []*ProofNode {
	if cp != nil {
		return cp.EndProof
	}
	return nil
}

func (cp *ChangeProof) GetKeyChanges() []*KeyChange {
	if cp != nil {
		return cp.KeyChanges
	}
	return nil
}

func (cp *ChangeProof) GetHadRootsInHistory() bool {
	if cp != nil {
		return cp.HadRootsInHistory
	}
	return false
}
