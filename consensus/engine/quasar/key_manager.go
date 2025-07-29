// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/ringtail"
)

// KeyManager manages validator keys and rotation for Quasar
type KeyManager struct {
	mu              sync.RWMutex
	nodeID          ids.NodeID
	
	// Current keys
	blsSigner       bls.Signer
	blsPublicKey    *bls.PublicKey
	ringtailSK      []byte
	ringtailPK      []byte
	
	// Key epochs
	currentEpoch    uint32
	epochKeys       map[uint32]*EpochKeys
	
	// Rotation schedule
	rotationInterval time.Duration
	nextRotation     time.Time
	
	// DKG state (for threshold Ringtail)
	dkgInProgress   bool
	dkgParticipants map[ids.NodeID][]byte
	
	// Control
	stopCh          chan struct{}
	rotationTimer   *time.Timer
}

// EpochKeys represents keys for a specific epoch
type EpochKeys struct {
	Epoch           uint32
	GroupPublicKey  []byte // For Ringtail threshold
	ValidatorKeys   map[ids.NodeID]*ValidatorKeys
	ActivationTime  time.Time
	ExpirationTime  time.Time
}

// NewKeyManager creates a new key manager
func NewKeyManager(nodeID ids.NodeID, blsSigner bls.Signer, ringtailSK []byte) *KeyManager {
	blsPK := blsSigner.PublicKey()
	
	// Generate Ringtail public key
	_, ringtailPK, _ := ringtail.KeyGen(ringtailSK)
	
	return &KeyManager{
		nodeID:           nodeID,
		blsSigner:        blsSigner,
		blsPublicKey:     blsPK,
		ringtailSK:       ringtailSK,
		ringtailPK:       ringtailPK,
		currentEpoch:     1,
		epochKeys:        make(map[uint32]*EpochKeys),
		rotationInterval: 24 * time.Hour, // Default: rotate daily
		nextRotation:     time.Now().Add(24 * time.Hour),
		stopCh:           make(chan struct{}),
	}
}

// Start begins key management operations
func (km *KeyManager) Start(ctx context.Context) error {
	// Schedule first rotation
	km.scheduleRotation()
	
	// Start rotation worker
	go km.rotationWorker(ctx)
	
	return nil
}

// Stop stops key management operations
func (km *KeyManager) Stop() {
	close(km.stopCh)
	if km.rotationTimer != nil {
		km.rotationTimer.Stop()
	}
}

// rotationWorker handles scheduled key rotations
func (km *KeyManager) rotationWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-km.stopCh:
			return
		case <-km.rotationTimer.C:
			if err := km.rotateKeys(ctx); err != nil {
				// Log error and reschedule
				km.scheduleRotation()
			}
		}
	}
}

// scheduleRotation schedules the next key rotation
func (km *KeyManager) scheduleRotation() {
	km.mu.Lock()
	defer km.mu.Unlock()
	
	duration := time.Until(km.nextRotation)
	if duration < 0 {
		duration = time.Minute // If we're past rotation time, rotate soon
	}
	
	if km.rotationTimer != nil {
		km.rotationTimer.Stop()
	}
	km.rotationTimer = time.NewTimer(duration)
}

// rotateKeys performs key rotation
func (km *KeyManager) rotateKeys(ctx context.Context) error {
	km.mu.Lock()
	defer km.mu.Unlock()
	
	// Generate new Ringtail keypair
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return err
	}
	
	newSK, newPK, err := ringtail.KeyGen(seed)
	if err != nil {
		return err
	}
	
	// Create new epoch
	newEpoch := km.currentEpoch + 1
	activationTime := time.Now().Add(5 * time.Minute) // 5 minute grace period
	
	epochKeys := &EpochKeys{
		Epoch:          newEpoch,
		ActivationTime: activationTime,
		ExpirationTime: activationTime.Add(km.rotationInterval),
		ValidatorKeys:  make(map[ids.NodeID]*ValidatorKeys),
	}
	
	// In production, this would involve DKG for threshold signing
	// For now, we'll use individual keys
	epochKeys.GroupPublicKey = newPK
	
	// Store new epoch keys
	km.epochKeys[newEpoch] = epochKeys
	
	// Broadcast key rotation announcement
	km.broadcastKeyRotation(newEpoch, newPK)
	
	// Schedule activation
	time.AfterFunc(5*time.Minute, func() {
		km.activateEpoch(newEpoch, newSK, newPK)
	})
	
	// Schedule next rotation
	km.nextRotation = activationTime.Add(km.rotationInterval)
	km.scheduleRotation()
	
	return nil
}

// activateEpoch activates a new key epoch
func (km *KeyManager) activateEpoch(epoch uint32, sk []byte, pk []byte) {
	km.mu.Lock()
	defer km.mu.Unlock()
	
	km.currentEpoch = epoch
	km.ringtailSK = sk
	km.ringtailPK = pk
	
	// Clean up old epochs
	for e := range km.epochKeys {
		if e < epoch-1 { // Keep previous epoch for verification
			delete(km.epochKeys, e)
		}
	}
}

// GetCurrentEpoch returns the current key epoch
func (km *KeyManager) GetCurrentEpoch() uint32 {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.currentEpoch
}

// GetGroupPublicKey returns the group public key for an epoch
func (km *KeyManager) GetGroupPublicKey(epoch uint32) []byte {
	km.mu.RLock()
	defer km.mu.RUnlock()
	
	if epochKeys, exists := km.epochKeys[epoch]; exists {
		return epochKeys.GroupPublicKey
	}
	
	// Return current if epoch not found
	if epoch == km.currentEpoch {
		return km.ringtailPK
	}
	
	return nil
}

// GetBLSSigner returns the current BLS signer and public key
func (km *KeyManager) GetBLSSigner() (bls.Signer, *bls.PublicKey) {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.blsSigner, km.blsPublicKey
}

// GetRingtailKeyPair returns the current Ringtail keypair
func (km *KeyManager) GetRingtailKeyPair() ([]byte, []byte) {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.ringtailSK, km.ringtailPK
}

// broadcastKeyRotation announces a key rotation to the network
func (km *KeyManager) broadcastKeyRotation(epoch uint32, newPK []byte) {
	// In production, this would broadcast a KeyRotation message
	// signed by the old key to prove ownership
}

// InitiateDKG starts distributed key generation for a new epoch
func (km *KeyManager) InitiateDKG(ctx context.Context, participants []ids.NodeID) error {
	km.mu.Lock()
	defer km.mu.Unlock()
	
	if km.dkgInProgress {
		return errors.New("DKG already in progress")
	}
	
	km.dkgInProgress = true
	km.dkgParticipants = make(map[ids.NodeID][]byte)
	
	// In production, this would implement a full DKG protocol
	// For now, it's a placeholder
	
	return nil
}

// ProcessDKGMessage handles DKG protocol messages
func (km *KeyManager) ProcessDKGMessage(from ids.NodeID, msg []byte) error {
	km.mu.Lock()
	defer km.mu.Unlock()
	
	if !km.dkgInProgress {
		return errors.New("no DKG in progress")
	}
	
	// Process DKG message
	// This would involve multiple rounds of communication
	
	return nil
}

// SetRotationInterval updates the key rotation interval
func (km *KeyManager) SetRotationInterval(interval time.Duration) {
	km.mu.Lock()
	defer km.mu.Unlock()
	
	km.rotationInterval = interval
	km.nextRotation = time.Now().Add(interval)
	km.scheduleRotation()
}