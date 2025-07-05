// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package attestationvm

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils"
	"github.com/luxfi/node/utils/logging"
)

const (
	// Database prefixes
	attestationPrefix    = 0x00
	pendingPrefix        = 0x01
	acceptedPrefix       = 0x02
	attestationCountKey  = "attestation_count"
)

// AttestationDB manages attestation storage
type AttestationDB struct {
	db     database.Database
	log    logging.Logger
	
	// Caches
	pendingAttestations  map[ids.ID]*Attestation
	acceptedAttestations map[ids.ID]*Attestation
	attestationCount     uint64
	
	mu sync.RWMutex
}

// NewAttestationDB creates a new attestation database
func NewAttestationDB(db database.Database, log logging.Logger) (*AttestationDB, error) {
	adb := &AttestationDB{
		db:                   db,
		log:                  log,
		pendingAttestations:  make(map[ids.ID]*Attestation),
		acceptedAttestations: make(map[ids.ID]*Attestation),
	}
	
	// Load attestation count
	countBytes, err := db.Get([]byte(attestationCountKey))
	if err == database.ErrNotFound {
		adb.attestationCount = 0
	} else if err != nil {
		return nil, err
	} else {
		adb.attestationCount = binary.BigEndian.Uint64(countBytes)
	}
	
	// Load pending attestations from DB
	if err := adb.loadPendingAttestations(); err != nil {
		return nil, err
	}
	
	return adb, nil
}

// AddPendingAttestation adds an attestation to the pending pool
func (adb *AttestationDB) AddPendingAttestation(att *Attestation) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()
	
	// Compute ID if not set
	if att.ID == ids.Empty {
		att.ID = att.ComputeID()
	}
	
	// Check if already exists
	if _, exists := adb.pendingAttestations[att.ID]; exists {
		return errors.New("attestation already pending")
	}
	
	// Serialize attestation
	attBytes, err := utils.Codec.Marshal(codecVersion, att)
	if err != nil {
		return err
	}
	
	// Store in database
	key := make([]byte, 1+len(att.ID))
	key[0] = pendingPrefix
	copy(key[1:], att.ID[:])
	
	if err := adb.db.Put(key, attBytes); err != nil {
		return err
	}
	
	// Update cache
	adb.pendingAttestations[att.ID] = att
	
	adb.log.Debug("Added pending attestation",
		"id", att.ID.String(),
		"type", att.Type,
		"sourceID", att.SourceID,
	)
	
	return nil
}

// GetPendingAttestations returns pending attestations up to limit
func (adb *AttestationDB) GetPendingAttestations(limit int) ([]*Attestation, error) {
	adb.mu.RLock()
	defer adb.mu.RUnlock()
	
	attestations := make([]*Attestation, 0, limit)
	
	for _, att := range adb.pendingAttestations {
		attestations = append(attestations, att)
		if len(attestations) >= limit {
			break
		}
	}
	
	return attestations, nil
}

// MarkAttestationAccepted moves an attestation from pending to accepted
func (adb *AttestationDB) MarkAttestationAccepted(attID ids.ID) error {
	adb.mu.Lock()
	defer adb.mu.Unlock()
	
	// Get from pending
	att, exists := adb.pendingAttestations[attID]
	if !exists {
		return errors.New("attestation not found in pending")
	}
	
	// Remove from pending DB
	pendingKey := make([]byte, 1+len(attID))
	pendingKey[0] = pendingPrefix
	copy(pendingKey[1:], attID[:])
	
	if err := adb.db.Delete(pendingKey); err != nil {
		return err
	}
	
	// Add to accepted DB
	acceptedKey := make([]byte, 1+len(attID))
	acceptedKey[0] = acceptedPrefix
	copy(acceptedKey[1:], attID[:])
	
	attBytes, err := utils.Codec.Marshal(codecVersion, att)
	if err != nil {
		return err
	}
	
	if err := adb.db.Put(acceptedKey, attBytes); err != nil {
		return err
	}
	
	// Update caches
	delete(adb.pendingAttestations, attID)
	adb.acceptedAttestations[attID] = att
	
	// Increment count
	adb.attestationCount++
	countBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(countBytes, adb.attestationCount)
	if err := adb.db.Put([]byte(attestationCountKey), countBytes); err != nil {
		return err
	}
	
	return nil
}

// GetAttestation retrieves an attestation by ID
func (adb *AttestationDB) GetAttestation(attID ids.ID) (*Attestation, error) {
	adb.mu.RLock()
	defer adb.mu.RUnlock()
	
	// Check pending cache
	if att, exists := adb.pendingAttestations[attID]; exists {
		return att, nil
	}
	
	// Check accepted cache
	if att, exists := adb.acceptedAttestations[attID]; exists {
		return att, nil
	}
	
	// Try loading from DB (accepted)
	acceptedKey := make([]byte, 1+len(attID))
	acceptedKey[0] = acceptedPrefix
	copy(acceptedKey[1:], attID[:])
	
	attBytes, err := adb.db.Get(acceptedKey)
	if err == nil {
		var att Attestation
		if err := utils.Codec.Unmarshal(attBytes, &att); err != nil {
			return nil, err
		}
		return &att, nil
	}
	
	// Try loading from DB (pending)
	pendingKey := make([]byte, 1+len(attID))
	pendingKey[0] = pendingPrefix
	copy(pendingKey[1:], attID[:])
	
	attBytes, err = adb.db.Get(pendingKey)
	if err != nil {
		return nil, errors.New("attestation not found")
	}
	
	var att Attestation
	if err := utils.Codec.Unmarshal(attBytes, &att); err != nil {
		return nil, err
	}
	
	return &att, nil
}

// GetAttestationCount returns the total number of accepted attestations
func (adb *AttestationDB) GetAttestationCount() uint64 {
	adb.mu.RLock()
	defer adb.mu.RUnlock()
	return adb.attestationCount
}

// loadPendingAttestations loads pending attestations from DB to cache
func (adb *AttestationDB) loadPendingAttestations() error {
	// In production, we would iterate through the DB
	// For now, we start with empty pending pool
	return nil
}

// Close closes the attestation database
func (adb *AttestationDB) Close() {
	adb.mu.Lock()
	defer adb.mu.Unlock()
	
	adb.pendingAttestations = nil
	adb.acceptedAttestations = nil
}