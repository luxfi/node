// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregated

import (
	"errors"
	"sync"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/crypto/bls"
	"github.com/luxfi/node/crypto/ringtail"
	"github.com/luxfi/node/crypto/cggmp21"
)

// BLSManager manages BLS signature operations
type BLSManager struct {
	log logging.Logger
	mu  sync.RWMutex
}

// NewBLSManager creates a new BLS manager
func NewBLSManager(log logging.Logger) *BLSManager {
	return &BLSManager{
		log: log,
	}
}

// CreateKeyPair generates a new BLS key pair
func (m *BLSManager) CreateKeyPair() (*bls.SecretKey, *bls.PublicKey, error) {
	sk, err := bls.NewSecretKey()
	if err != nil {
		return nil, nil, err
	}
	
	pk := bls.PublicFromSecretKey(sk)
	return sk, pk, nil
}

// Sign creates a BLS signature
func (m *BLSManager) Sign(sk *bls.SecretKey, message []byte) (*bls.Signature, error) {
	sig := bls.Sign(sk, message)
	return sig, nil
}

// RingtailManager manages Ringtail signature operations
type RingtailManager struct {
	log logging.Logger
	mu  sync.RWMutex
}

// NewRingtailManager creates a new Ringtail manager
func NewRingtailManager(log logging.Logger) *RingtailManager {
	return &RingtailManager{
		log: log,
	}
}

// CreateKeyPair generates a new Ringtail key pair
func (m *RingtailManager) CreateKeyPair() (*ringtail.PrivateKey, *ringtail.PublicKey, error) {
	factory := &ringtail.Factory{}
	privKey, err := factory.NewPrivateKey()
	if err != nil {
		return nil, nil, err
	}
	
	pubKey, err := factory.ToPublicKey(privKey)
	if err != nil {
		return nil, nil, err
	}
	
	return privKey, pubKey, nil
}

// Sign creates a Ringtail ring signature
func (m *RingtailManager) Sign(
	sk *ringtail.PrivateKey,
	message []byte,
	ring []*ringtail.PublicKey,
) (*ringtail.RingSignature, error) {
	return sk.Sign(message, ring)
}

// CGGMP21Manager manages CGGMP21 threshold signature operations
type CGGMP21Manager struct {
	log     logging.Logger
	parties map[int]*cggmp21.Party
	config  *cggmp21.Config
	mu      sync.RWMutex
}

// NewCGGMP21Manager creates a new CGGMP21 manager
func NewCGGMP21Manager(log logging.Logger) *CGGMP21Manager {
	return &CGGMP21Manager{
		log:     log,
		parties: make(map[int]*cggmp21.Party),
	}
}

// InitializeParty creates a new CGGMP21 party
func (m *CGGMP21Manager) InitializeParty(
	partyID ids.NodeID,
	index int,
	config *cggmp21.Config,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	party, err := cggmp21.NewParty(partyID, index, config, m.log)
	if err != nil {
		return err
	}
	
	m.parties[index] = party
	m.config = config
	
	return nil
}

// GetParty retrieves a CGGMP21 party by index
func (m *CGGMP21Manager) GetParty(index int) (*cggmp21.Party, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	party, exists := m.parties[index]
	if !exists {
		return nil, errors.New("party not found")
	}
	
	return party, nil
}

// FeeCollector manages fee collection for signature operations
type FeeCollector struct {
	collectedFees map[SignatureType]uint64
	mu            sync.RWMutex
}

// NewFeeCollector creates a new fee collector
func NewFeeCollector() FeeCollector {
	return FeeCollector{
		collectedFees: make(map[SignatureType]uint64),
	}
}

// CollectFee records a fee collection
func (fc *FeeCollector) CollectFee(sigType SignatureType, amount uint64) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	
	fc.collectedFees[sigType] += amount
}

// GetCollectedFees returns total collected fees by type
func (fc *FeeCollector) GetCollectedFees() map[SignatureType]uint64 {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	
	fees := make(map[SignatureType]uint64)
	for k, v := range fc.collectedFees {
		fees[k] = v
	}
	
	return fees
}

// GetTotalFees returns the total of all collected fees
func (fc *FeeCollector) GetTotalFees() uint64 {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	
	var total uint64
	for _, amount := range fc.collectedFees {
		total += amount
	}
	
	return total
}

// ResetFees resets the fee collection
func (fc *FeeCollector) ResetFees() map[SignatureType]uint64 {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	
	oldFees := fc.collectedFees
	fc.collectedFees = make(map[SignatureType]uint64)
	
	return oldFees
}