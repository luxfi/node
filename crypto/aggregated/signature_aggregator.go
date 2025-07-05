// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregated

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/utils/crypto/bls"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/crypto/corona"
)

// SignatureType represents the type of aggregated signature
type SignatureType uint8

const (
	SignatureTypeBLS SignatureType = iota
	SignatureTypeCorona
	SignatureTypeCGGMP21
)

// SignatureConfig contains configuration for signature aggregation
type SignatureConfig struct {
	// Network-wide signature type preference
	PreferredType       SignatureType `json:"preferredType"`
	
	// Enable specific signature types
	EnableBLS           bool          `json:"enableBLS"`
	EnableCorona      bool          `json:"enableCorona"`
	EnableCGGMP21       bool          `json:"enableCGGMP21"`
	
	// Fee configuration (in nLUX - nano LUX)
	BLSFee              uint64        `json:"blsFee"`              // 0 = free
	CoronaFee         uint64        `json:"coronaFee"`         // Premium for enhanced privacy
	CGGMP21Fee          uint64        `json:"cggmp21Fee"`          // Premium for threshold signatures
	
	// Performance settings
	ParallelAggregation bool          `json:"parallelAggregation"`
	MaxSignersPerRound  int           `json:"maxSignersPerRound"`
	
	// Security settings
	MinSigners          int           `json:"minSigners"`
	ThresholdRatio      float64       `json:"thresholdRatio"`      // e.g., 0.67 for 2/3
}

// AggregatedSignature represents an aggregated signature with metadata
type AggregatedSignature struct {
	Type            SignatureType          `json:"type"`
	Signature       []byte                 `json:"signature"`
	SignerIDs       []ids.NodeID           `json:"signerIds,omitempty"`
	SignerCount     int                    `json:"signerCount"`
	RingPublicKeys  []*corona.PublicKey  `json:"ringPublicKeys,omitempty"`  // For Corona
	AggregateKey    []byte                 `json:"aggregateKey,omitempty"`    // For BLS
	Threshold       int                    `json:"threshold,omitempty"`        // For CGGMP21
	TotalFee        uint64                 `json:"totalFee"`
}

// SignatureAggregator manages network-wide signature aggregation
type SignatureAggregator struct {
	config      SignatureConfig
	log         logging.Logger
	
	// Signature managers
	blsManager      *BLSManager
	coronaManager *CoronaManager
	cggmpManager    *CGGMP21Manager
	
	// Active aggregation sessions
	sessions    map[string]*AggregationSession
	
	// Fee collector
	feeCollector FeeCollector
	
	mu          sync.RWMutex
}

// AggregationSession represents an active signature aggregation
type AggregationSession struct {
	SessionID    string
	Message      []byte
	SignatureType SignatureType
	
	// Collected signatures
	BLSSignatures      []*bls.Signature
	BLSPublicKeys      []*bls.PublicKey
	CoronaSignatures []*corona.RingSignature
	CoronaRing       []*corona.PublicKey
	
	// Signers
	Signers      map[ids.NodeID]bool
	SignerCount  int
	
	// Status
	StartTime    int64
	Completed    bool
	Result       *AggregatedSignature
}

// NewSignatureAggregator creates a new signature aggregator
func NewSignatureAggregator(config SignatureConfig, log logging.Logger) (*SignatureAggregator, error) {
	sa := &SignatureAggregator{
		config:   config,
		log:      log,
		sessions: make(map[string]*AggregationSession),
	}
	
	// Initialize signature managers based on config
	if config.EnableBLS {
		sa.blsManager = NewBLSManager(log)
	}
	
	if config.EnableCorona {
		sa.coronaManager = NewCoronaManager(log)
	}
	
	if config.EnableCGGMP21 {
		sa.cggmpManager = NewCGGMP21Manager(log)
	}
	
	sa.feeCollector = NewFeeCollector()
	
	log.Info("Signature aggregator initialized",
		"preferredType", config.PreferredType,
		"blsEnabled", config.EnableBLS,
		"coronaEnabled", config.EnableCorona,
		"cggmp21Enabled", config.EnableCGGMP21,
		"blsFee", config.BLSFee,
		"coronaFee", config.CoronaFee,
	)
	
	return sa, nil
}

// StartAggregation starts a new signature aggregation session
func (sa *SignatureAggregator) StartAggregation(
	sessionID string,
	message []byte,
	sigType SignatureType,
	expectedSigners int,
) error {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	
	if _, exists := sa.sessions[sessionID]; exists {
		return errors.New("session already exists")
	}
	
	// Validate signature type is enabled
	switch sigType {
	case SignatureTypeBLS:
		if !sa.config.EnableBLS {
			return errors.New("BLS signatures not enabled")
		}
	case SignatureTypeCorona:
		if !sa.config.EnableCorona {
			return errors.New("Corona signatures not enabled")
		}
	case SignatureTypeCGGMP21:
		if !sa.config.EnableCGGMP21 {
			return errors.New("CGGMP21 signatures not enabled")
		}
	default:
		return errors.New("unknown signature type")
	}
	
	session := &AggregationSession{
		SessionID:     sessionID,
		Message:       message,
		SignatureType: sigType,
		Signers:       make(map[ids.NodeID]bool),
		StartTime:     getCurrentTime(),
	}
	
	sa.sessions[sessionID] = session
	
	sa.log.Debug("Started aggregation session",
		"sessionID", sessionID,
		"type", sigType,
		"expectedSigners", expectedSigners,
	)
	
	return nil
}

// AddSignature adds a signature to an aggregation session
func (sa *SignatureAggregator) AddSignature(
	sessionID string,
	signerID ids.NodeID,
	signature []byte,
	publicKey []byte,
) error {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	
	session, exists := sa.sessions[sessionID]
	if !exists {
		return errors.New("session not found")
	}
	
	if session.Completed {
		return errors.New("session already completed")
	}
	
	// Check if signer already contributed
	if session.Signers[signerID] {
		return errors.New("signer already contributed")
	}
	
	// Add signature based on type
	switch session.SignatureType {
	case SignatureTypeBLS:
		return sa.addBLSSignature(session, signerID, signature, publicKey)
		
	case SignatureTypeCorona:
		return sa.addCoronaSignature(session, signerID, signature, publicKey)
		
	case SignatureTypeCGGMP21:
		return errors.New("CGGMP21 uses different protocol flow")
		
	default:
		return errors.New("unknown signature type")
	}
}

// addBLSSignature adds a BLS signature to the session
func (sa *SignatureAggregator) addBLSSignature(
	session *AggregationSession,
	signerID ids.NodeID,
	signature []byte,
	publicKey []byte,
) error {
	// Parse BLS signature and public key
	sig, err := bls.SignatureFromBytes(signature)
	if err != nil {
		return fmt.Errorf("invalid BLS signature: %w", err)
	}
	
	pk, err := bls.PublicKeyFromBytes(publicKey)
	if err != nil {
		return fmt.Errorf("invalid BLS public key: %w", err)
	}
	
	// Verify individual signature
	if err := bls.Verify(pk, sig, session.Message); err != nil {
		return fmt.Errorf("BLS signature verification failed: %w", err)
	}
	
	// Add to session
	session.BLSSignatures = append(session.BLSSignatures, sig)
	session.BLSPublicKeys = append(session.BLSPublicKeys, pk)
	session.Signers[signerID] = true
	session.SignerCount++
	
	return nil
}

// addCoronaSignature adds a Corona signature to the session
func (sa *SignatureAggregator) addCoronaSignature(
	session *AggregationSession,
	signerID ids.NodeID,
	signature []byte,
	publicKey []byte,
) error {
	// Parse Corona signature
	var ringSig corona.RingSignature
	if err := ringSig.UnmarshalBinary(signature); err != nil {
		return fmt.Errorf("invalid Corona signature: %w", err)
	}
	
	// Parse public key
	pk, err := corona.PublicKeyFromBytes(publicKey)
	if err != nil {
		return fmt.Errorf("invalid Corona public key: %w", err)
	}
	
	// Add to ring if not already present
	inRing := false
	for _, ringPK := range session.CoronaRing {
		if ringPK.Equal(pk) {
			inRing = true
			break
		}
	}
	if !inRing {
		session.CoronaRing = append(session.CoronaRing, pk)
	}
	
	// Store signature
	session.CoronaSignatures = append(session.CoronaSignatures, &ringSig)
	session.Signers[signerID] = true
	session.SignerCount++
	
	return nil
}

// FinalizeAggregation completes the aggregation and returns the result
func (sa *SignatureAggregator) FinalizeAggregation(
	sessionID string,
	requiredSigners int,
) (*AggregatedSignature, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	
	session, exists := sa.sessions[sessionID]
	if !exists {
		return nil, errors.New("session not found")
	}
	
	if session.Completed {
		return session.Result, nil
	}
	
	// Check minimum signers
	if session.SignerCount < requiredSigners {
		return nil, fmt.Errorf("insufficient signers: %d < %d", session.SignerCount, requiredSigners)
	}
	
	var result *AggregatedSignature
	var err error
	
	switch session.SignatureType {
	case SignatureTypeBLS:
		result, err = sa.finalizeBLS(session)
		
	case SignatureTypeCorona:
		result, err = sa.finalizeCorona(session)
		
	default:
		return nil, errors.New("unknown signature type")
	}
	
	if err != nil {
		return nil, err
	}
	
	// Calculate total fee
	result.TotalFee = sa.calculateFee(session.SignatureType, session.SignerCount)
	
	// Mark session as completed
	session.Completed = true
	session.Result = result
	
	sa.log.Info("Finalized aggregation",
		"sessionID", sessionID,
		"type", session.SignatureType,
		"signers", session.SignerCount,
		"totalFee", result.TotalFee,
	)
	
	return result, nil
}

// finalizeBLS aggregates BLS signatures
func (sa *SignatureAggregator) finalizeBLS(session *AggregationSession) (*AggregatedSignature, error) {
	if len(session.BLSSignatures) == 0 {
		return nil, errors.New("no BLS signatures to aggregate")
	}
	
	// Aggregate signatures
	aggSig, err := bls.AggregateSignatures(session.BLSSignatures)
	if err != nil {
		return nil, fmt.Errorf("BLS aggregation failed: %w", err)
	}
	
	// Aggregate public keys
	aggPK, err := bls.AggregatePublicKeys(session.BLSPublicKeys)
	if err != nil {
		return nil, fmt.Errorf("BLS public key aggregation failed: %w", err)
	}
	
	// Verify aggregate signature
	if err := bls.Verify(aggPK, aggSig, session.Message); err != nil {
		return nil, fmt.Errorf("aggregate signature verification failed: %w", err)
	}
	
	sigBytes, _ := bls.SignatureToBytes(aggSig)
	pkBytes, _ := bls.PublicKeyToBytes(aggPK)
	
	// Extract signer IDs
	signerIDs := make([]ids.NodeID, 0, len(session.Signers))
	for id := range session.Signers {
		signerIDs = append(signerIDs, id)
	}
	
	return &AggregatedSignature{
		Type:         SignatureTypeBLS,
		Signature:    sigBytes,
		SignerIDs:    signerIDs,
		SignerCount:  session.SignerCount,
		AggregateKey: pkBytes,
	}, nil
}

// finalizeCorona creates a linkable ring signature
func (sa *SignatureAggregator) finalizeCorona(session *AggregationSession) (*AggregatedSignature, error) {
	if len(session.CoronaSignatures) == 0 {
		return nil, errors.New("no Corona signatures collected")
	}
	
	// For Corona, we use the first signature as the aggregated result
	// since ring signatures provide anonymity within the ring
	ringSig := session.CoronaSignatures[0]
	sigBytes, _ := ringSig.MarshalBinary()
	
	// Verify against the full ring
	if err := corona.Verify(session.Message, ringSig, session.CoronaRing); err != nil {
		return nil, fmt.Errorf("ring signature verification failed: %w", err)
	}
	
	return &AggregatedSignature{
		Type:           SignatureTypeCorona,
		Signature:      sigBytes,
		SignerCount:    len(session.CoronaRing), // Ring size, not actual signers
		RingPublicKeys: session.CoronaRing,
	}, nil
}

// calculateFee calculates the total fee for signature aggregation
func (sa *SignatureAggregator) calculateFee(sigType SignatureType, signerCount int) uint64 {
	var feePerSigner uint64
	
	switch sigType {
	case SignatureTypeBLS:
		feePerSigner = sa.config.BLSFee // 0 for free
	case SignatureTypeCorona:
		feePerSigner = sa.config.CoronaFee // Premium fee
	case SignatureTypeCGGMP21:
		feePerSigner = sa.config.CGGMP21Fee // Premium fee
	default:
		feePerSigner = 0
	}
	
	return feePerSigner * uint64(signerCount)
}

// VerifyAggregatedSignature verifies an aggregated signature
func (sa *SignatureAggregator) VerifyAggregatedSignature(
	message []byte,
	aggSig *AggregatedSignature,
) error {
	switch aggSig.Type {
	case SignatureTypeBLS:
		return sa.verifyBLSAggregate(message, aggSig)
		
	case SignatureTypeCorona:
		return sa.verifyCoronaAggregate(message, aggSig)
		
	case SignatureTypeCGGMP21:
		return errors.New("CGGMP21 verification not implemented")
		
	default:
		return errors.New("unknown signature type")
	}
}

// verifyBLSAggregate verifies a BLS aggregate signature
func (sa *SignatureAggregator) verifyBLSAggregate(message []byte, aggSig *AggregatedSignature) error {
	sig, err := bls.SignatureFromBytes(aggSig.Signature)
	if err != nil {
		return err
	}
	
	pk, err := bls.PublicKeyFromBytes(aggSig.AggregateKey)
	if err != nil {
		return err
	}
	
	return bls.Verify(pk, sig, message)
}

// verifyCoronaAggregate verifies a Corona ring signature
func (sa *SignatureAggregator) verifyCoronaAggregate(message []byte, aggSig *AggregatedSignature) error {
	var ringSig corona.RingSignature
	if err := ringSig.UnmarshalBinary(aggSig.Signature); err != nil {
		return err
	}
	
	return corona.Verify(message, &ringSig, aggSig.RingPublicKeys)
}

// GetSessionStatus returns the status of an aggregation session
func (sa *SignatureAggregator) GetSessionStatus(sessionID string) (map[string]interface{}, error) {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	
	session, exists := sa.sessions[sessionID]
	if !exists {
		return nil, errors.New("session not found")
	}
	
	status := map[string]interface{}{
		"sessionID":     session.SessionID,
		"signatureType": session.SignatureType,
		"signerCount":   session.SignerCount,
		"completed":     session.Completed,
		"startTime":     session.StartTime,
	}
	
	if session.Completed && session.Result != nil {
		status["totalFee"] = session.Result.TotalFee
	}
	
	return status, nil
}

// Cleanup removes old sessions
func (sa *SignatureAggregator) Cleanup(maxAge int64) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	
	currentTime := getCurrentTime()
	
	for sessionID, session := range sa.sessions {
		if currentTime-session.StartTime > maxAge {
			delete(sa.sessions, sessionID)
		}
	}
}

// Helper function
func getCurrentTime() int64 {
	return time.Now().Unix()
}