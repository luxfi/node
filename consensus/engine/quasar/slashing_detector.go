// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/luxfi/ids"
)

// SlashingDetector detects validator misbehavior in Quasar finality
type SlashingDetector struct {
	mu              sync.RWMutex
	validators      map[ids.NodeID]*ValidatorKeys
	
	// Track signatures to detect double-signing
	blsSignatures   map[uint64]map[ids.NodeID]*SignatureRecord
	rtSignatures    map[uint64]map[ids.NodeID]*SignatureRecord
	
	// Track missing signatures
	missingRT       map[uint64]map[ids.NodeID]bool
}

// SignatureRecord tracks signature details for slashing detection
type SignatureRecord struct {
	BlockID         ids.ID
	BlockHash       []byte
	Signature       []byte
	Timestamp       int64
}

// SlashingEvent represents a detected slashing condition
type SlashingEvent struct {
	Type        string
	NodeID      ids.NodeID
	Height      uint64
	BlockID     ids.ID
	Evidence    []byte
	Timestamp   time.Time
}

// PendingBlock represents a block pending finalization
type PendingBlock struct {
	ID        ids.ID
	BlockID   ids.ID
	Height    uint64
	Timestamp time.Time
	Hash      []byte
}

// NewSlashingDetector creates a new slashing detector
func NewSlashingDetector(validators map[ids.NodeID]*ValidatorKeys) *SlashingDetector {
	return &SlashingDetector{
		validators:     validators,
		blsSignatures:  make(map[uint64]map[ids.NodeID]*SignatureRecord),
		rtSignatures:   make(map[uint64]map[ids.NodeID]*SignatureRecord),
		missingRT:      make(map[uint64]map[ids.NodeID]bool),
	}
}

// RecordBLSSignature records a BLS signature for tracking
func (sd *SlashingDetector) RecordBLSSignature(height uint64, nodeID ids.NodeID, blockID ids.ID, blockHash []byte, sig []byte) *SlashingEvent {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	
	if _, exists := sd.blsSignatures[height]; !exists {
		sd.blsSignatures[height] = make(map[ids.NodeID]*SignatureRecord)
	}
	
	// Check for existing signature at this height
	if existing, exists := sd.blsSignatures[height][nodeID]; exists {
		// Check if signing different block
		if existing.BlockID != blockID {
			// Double signing detected!
			// TODO: Implement DoubleSignEvidence marshaling
			evidenceBytes, _ := json.Marshal(map[string]interface{}{
				"height":     height,
				"blockID1":   existing.BlockID,
				"blockID2":   blockID,
				"signature1": existing.Signature,
				"signature2": sig,
			})
			return &SlashingEvent{
				NodeID:    nodeID,
				Type:      "double_sign",
				Height:    height,
				BlockID:   blockID,
				Evidence:  evidenceBytes,
				Timestamp: time.Now(),
			}
		}
		return nil // Same block, no issue
	}
	
	// Record the signature
	sd.blsSignatures[height][nodeID] = &SignatureRecord{
		BlockID:   blockID,
		BlockHash: blockHash,
		Signature: sig,
		Timestamp: currentTimestamp(),
	}
	
	return nil
}

// RecordRingtailShare records a Ringtail signature share
func (sd *SlashingDetector) RecordRingtailShare(height uint64, nodeID ids.NodeID, blockID ids.ID, share []byte) *SlashingEvent {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	
	if _, exists := sd.rtSignatures[height]; !exists {
		sd.rtSignatures[height] = make(map[ids.NodeID]*SignatureRecord)
	}
	
	// Check for existing signature at this height
	if existing, exists := sd.rtSignatures[height][nodeID]; exists {
		// Check if signing different block
		if existing.BlockID != blockID {
			// Double signing detected!
			// TODO: Implement DoubleSignEvidence marshaling
			evidenceBytes, _ := json.Marshal(map[string]interface{}{
				"height":     height,
				"blockID1":   existing.BlockID,
				"blockID2":   blockID,
				"signature1": existing.Signature,
				"signature2": share,
			})
			return &SlashingEvent{
				NodeID:    nodeID,
				Type:      "double_sign",
				Height:    height,
				BlockID:   blockID,
				Evidence:  evidenceBytes,
				Timestamp: time.Now(),
			}
		}
		return nil
	}
	
	// Record the signature
	sd.rtSignatures[height][nodeID] = &SignatureRecord{
		BlockID:   blockID,
		Signature: share,
		Timestamp: currentTimestamp(),
	}
	
	// Clear from missing RT if was marked
	if missing, exists := sd.missingRT[height]; exists {
		delete(missing, nodeID)
	}
	
	return nil
}

// Analyze analyzes a failed certificate for misbehavior
func (sd *SlashingDetector) Analyze(block *PendingBlock, cert *DualCertificate) *SlashingEvent {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	
	height := block.Height
	
	// Get BLS signers
	blsSigners := make(map[ids.NodeID]bool)
	if sigs, exists := sd.blsSignatures[height]; exists {
		for nodeID := range sigs {
			blsSigners[nodeID] = true
		}
	}
	
	// Get Ringtail signers
	rtSigners := make(map[ids.NodeID]bool)
	if sigs, exists := sd.rtSignatures[height]; exists {
		for nodeID := range sigs {
			rtSigners[nodeID] = true
		}
	}
	
	// Check for validators who signed BLS but not Ringtail
	for nodeID := range blsSigners {
		if !rtSigners[nodeID] {
			// Mark as missing Ringtail
			if _, exists := sd.missingRT[height]; !exists {
				sd.missingRT[height] = make(map[ids.NodeID]bool)
			}
			sd.missingRT[height][nodeID] = true
			
			// Create evidence
			blsSig := sd.blsSignatures[height][nodeID]
			evidenceBytes, _ := json.Marshal(map[string]interface{}{
				"height":       height,
				"blockID":      block.ID,
				"blsSignature": blsSig.Signature,
			})
			return &SlashingEvent{
				NodeID:    nodeID,
				Type:      "missing_ringtail",
				Height:    height,
				BlockID:   block.ID,
				Evidence:  evidenceBytes,
				Timestamp: time.Now(),
			}
		}
	}
	
	// Check for invalid signatures in the certificate
	if cert != nil && len(cert.SignerIDs) > 0 {
		// Verify each claimed signer actually signed
		for _, nodeID := range cert.SignerIDs {
			hasBLS := blsSigners[nodeID]
			hasRT := rtSigners[nodeID]
			
			if !hasBLS || !hasRT {
				// Claimed signer didn't actually sign
				evidenceBytes, _ := json.Marshal(map[string]interface{}{
					"height":  height,
					"blockID": block.ID,
					"hasBLS":  hasBLS,
					"hasRT":   hasRT,
				})
				return &SlashingEvent{
					NodeID:    nodeID,
					Type:      "invalid_signature",
					Height:    height,
					BlockID:   block.ID,
					Evidence:  evidenceBytes,
					Timestamp: time.Now(),
				}
			}
		}
	}
	
	return nil
}

// CheckDoubleSign checks if a validator has double-signed at a height
func (sd *SlashingDetector) CheckDoubleSign(height uint64, nodeID ids.NodeID, blockID ids.ID) bool {
	sd.mu.RLock()
	defer sd.mu.RUnlock()
	
	// Check BLS signatures
	if sigs, exists := sd.blsSignatures[height]; exists {
		if record, exists := sigs[nodeID]; exists && record.BlockID != blockID {
			return true
		}
	}
	
	// Check Ringtail signatures
	if sigs, exists := sd.rtSignatures[height]; exists {
		if record, exists := sigs[nodeID]; exists && record.BlockID != blockID {
			return true
		}
	}
	
	return false
}

// CleanupHeight removes tracking data for old heights
func (sd *SlashingDetector) CleanupHeight(height uint64) {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	
	delete(sd.blsSignatures, height)
	delete(sd.rtSignatures, height)
	delete(sd.missingRT, height)
}

// Evidence types for slashing

// DoubleSignEvidence proves a validator signed two different blocks at same height
type DoubleSignEvidence struct {
	Height      uint64
	BlockID1    ids.ID
	BlockID2    ids.ID
	Signature1  []byte
	Signature2  []byte
}

// MissingRingtailEvidence proves a validator signed BLS but not Ringtail
type MissingRingtailEvidence struct {
	Height       uint64
	BlockID      ids.ID
	BLSSignature []byte
}

// InvalidSignatureEvidence proves a validator's signature is invalid
type InvalidSignatureEvidence struct {
	Height  uint64
	BlockID ids.ID
	HasBLS  bool
	HasRT   bool
}

// currentTimestamp returns current unix timestamp
func currentTimestamp() int64 {
	return time.Now().Unix()
}