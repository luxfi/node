// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ringtail"
)

var (
	ErrInsufficientSignatures = errors.New("insufficient signatures for finality")
	ErrBLSAggregationFailed  = errors.New("BLS aggregation failed")
	ErrRingtailAggregationFailed = errors.New("Ringtail aggregation failed")
	ErrDualCertMismatch      = errors.New("BLS and Ringtail certificates do not match")
	ErrQuasarTimeout         = errors.New("Quasar finality timeout")
)

// DualCertificate represents a finalized block with both BLS and Ringtail signatures
type DualCertificate struct {
	BlockID        ids.ID
	BlockHeight    uint64
	Epoch          uint32
	BLSSignature   []byte
	RingtailCert   []byte
	SignerIDs      []ids.NodeID
	Timestamp      time.Time
}

// Aggregator manages parallel BLS and Ringtail signature aggregation
type Aggregator struct {
	mu              sync.RWMutex
	nodeID          ids.NodeID
	threshold       int
	validators      map[ids.NodeID]*ValidatorKeys
	
	// Signature collection buffers
	blsShares       map[uint64]map[ids.NodeID]*bls.Signature
	ringtailShares  map[uint64]map[ids.NodeID]ringtail.Share
	
	// Certificate channels
	certChannels    map[uint64]chan *DualCertificate
	
	// Precomputation management
	precompPool     *PrecomputePool
	
	// Metrics
	metrics         *AggregatorMetrics
}

// ValidatorKeys holds both BLS and Ringtail public keys for a validator
type ValidatorKeys struct {
	NodeID         ids.NodeID
	BLSPublicKey   *bls.PublicKey
	RingtailPubKey []byte
	Epoch          uint32
}

// AggregatorMetrics tracks aggregation performance
type AggregatorMetrics struct {
	BLSAggregations      uint64
	RingtailAggregations uint64
	DualCertsCreated     uint64
	AggregationLatency   time.Duration
}

// NewAggregator creates a new Quasar aggregator
func NewAggregator(nodeID ids.NodeID, threshold int, validators map[ids.NodeID]*ValidatorKeys) *Aggregator {
	return &Aggregator{
		nodeID:         nodeID,
		threshold:      threshold,
		validators:     validators,
		blsShares:      make(map[uint64]map[ids.NodeID]*bls.Signature),
		ringtailShares: make(map[uint64]map[ids.NodeID]ringtail.Share),
		certChannels:   make(map[uint64]chan *DualCertificate),
		precompPool:    NewPrecomputePool(100), // 100 precomputed shares
		metrics:        &AggregatorMetrics{},
	}
}

// AddBLSShare adds a BLS signature share for aggregation
func (a *Aggregator) AddBLSShare(height uint64, nodeID ids.NodeID, sig *bls.Signature) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	if _, exists := a.blsShares[height]; !exists {
		a.blsShares[height] = make(map[ids.NodeID]*bls.Signature)
	}
	
	a.blsShares[height][nodeID] = sig
	
	// Try aggregation if we have enough shares
	return a.tryAggregate(height)
}

// AddRingtailShare adds a Ringtail signature share for aggregation
func (a *Aggregator) AddRingtailShare(height uint64, nodeID ids.NodeID, share ringtail.Share) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	if _, exists := a.ringtailShares[height]; !exists {
		a.ringtailShares[height] = make(map[ids.NodeID]ringtail.Share)
	}
	
	a.ringtailShares[height][nodeID] = share
	
	// Try aggregation if we have enough shares
	return a.tryAggregate(height)
}

// tryAggregate attempts to create a dual certificate if enough signatures are collected
func (a *Aggregator) tryAggregate(height uint64) error {
	blsCount := len(a.blsShares[height])
	rtCount := len(a.ringtailShares[height])
	
	// Need threshold signatures for both
	if blsCount < a.threshold || rtCount < a.threshold {
		return nil // Not ready yet
	}
	
	// Ensure we have matching signers for both signatures
	signerIDs := a.getMatchingSigners(height)
	if len(signerIDs) < a.threshold {
		return nil // Not enough matching signatures
	}
	
	// Run BLS and Ringtail aggregation in parallel
	start := time.Now()
	
	var wg sync.WaitGroup
	var blsErr, rtErr error
	var blsAgg []byte
	var rtCert []byte
	
	// BLS aggregation goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		blsAgg, blsErr = a.aggregateBLS(height, signerIDs)
		if blsErr == nil {
			a.metrics.BLSAggregations++
		}
	}()
	
	// Ringtail aggregation goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		rtCert, rtErr = a.aggregateRingtail(height, signerIDs)
		if rtErr == nil {
			a.metrics.RingtailAggregations++
		}
	}()
	
	wg.Wait()
	
	// Check for errors
	if blsErr != nil {
		return fmt.Errorf("BLS aggregation failed: %w", blsErr)
	}
	if rtErr != nil {
		return fmt.Errorf("Ringtail aggregation failed: %w", rtErr)
	}
	
	// Create dual certificate
	cert := &DualCertificate{
		BlockHeight:   height,
		Epoch:         a.getCurrentEpoch(),
		BLSSignature:  blsAgg,
		RingtailCert:  rtCert,
		SignerIDs:     signerIDs,
		Timestamp:     time.Now(),
	}
	
	a.metrics.DualCertsCreated++
	a.metrics.AggregationLatency = time.Since(start)
	
	// Notify waiters
	if ch, exists := a.certChannels[height]; exists {
		select {
		case ch <- cert:
		default:
		}
	}
	
	// Cleanup
	delete(a.blsShares, height)
	delete(a.ringtailShares, height)
	
	return nil
}

// getMatchingSigners returns validators who provided both BLS and Ringtail signatures
func (a *Aggregator) getMatchingSigners(height uint64) []ids.NodeID {
	var signers []ids.NodeID
	
	blsSigners := a.blsShares[height]
	rtSigners := a.ringtailShares[height]
	
	for nodeID := range blsSigners {
		if _, hasRT := rtSigners[nodeID]; hasRT {
			signers = append(signers, nodeID)
		}
	}
	
	return signers
}

// aggregateBLS performs BLS signature aggregation
func (a *Aggregator) aggregateBLS(height uint64, signerIDs []ids.NodeID) ([]byte, error) {
	shares := a.blsShares[height]
	
	// Collect signatures for aggregation
	sigs := make([]*bls.Signature, 0, len(signerIDs))
	for _, nodeID := range signerIDs {
		if sig, exists := shares[nodeID]; exists {
			sigs = append(sigs, sig)
		}
	}
	
	if len(sigs) < a.threshold {
		return nil, ErrInsufficientSignatures
	}
	
	// Aggregate signatures
	aggSig, err := bls.AggregateSignatures(sigs)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBLSAggregationFailed, err)
	}
	
	return bls.SignatureToBytes(aggSig), nil
}

// aggregateRingtail performs Ringtail threshold signature aggregation
func (a *Aggregator) aggregateRingtail(height uint64, signerIDs []ids.NodeID) ([]byte, error) {
	shares := a.ringtailShares[height]
	
	// Collect shares for aggregation
	rtShares := make([]ringtail.Share, 0, len(signerIDs))
	for _, nodeID := range signerIDs {
		if share, exists := shares[nodeID]; exists {
			rtShares = append(rtShares, share)
		}
	}
	
	if len(rtShares) < a.threshold {
		return nil, ErrInsufficientSignatures
	}
	
	// Aggregate shares into certificate
	cert, err := ringtail.Aggregate(rtShares)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRingtailAggregationFailed, err)
	}
	
	return cert, nil
}

// WaitForCertificate waits for a dual certificate to be created for a block height
func (a *Aggregator) WaitForCertificate(ctx context.Context, height uint64, timeout time.Duration) (*DualCertificate, error) {
	a.mu.Lock()
	ch, exists := a.certChannels[height]
	if !exists {
		ch = make(chan *DualCertificate, 1)
		a.certChannels[height] = ch
	}
	a.mu.Unlock()
	
	// Set timeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	
	select {
	case cert := <-ch:
		a.cleanupHeight(height)
		return cert, nil
	case <-timer.C:
		a.cleanupHeight(height)
		return nil, ErrQuasarTimeout
	case <-ctx.Done():
		a.cleanupHeight(height)
		return nil, ctx.Err()
	}
}

// cleanupHeight removes all data for a specific height
func (a *Aggregator) cleanupHeight(height uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	delete(a.blsShares, height)
	delete(a.ringtailShares, height)
	delete(a.certChannels, height)
}

// getCurrentEpoch returns the current Ringtail key epoch
func (a *Aggregator) getCurrentEpoch() uint32 {
	// In production, this would track key rotation epochs
	return 1
}

// UpdateValidatorKeys updates the validator key set (for key rotation)
func (a *Aggregator) UpdateValidatorKeys(validators map[ids.NodeID]*ValidatorKeys) {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	a.validators = validators
}

// GetMetrics returns aggregator performance metrics
func (a *Aggregator) GetMetrics() AggregatorMetrics {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	return *a.metrics
}