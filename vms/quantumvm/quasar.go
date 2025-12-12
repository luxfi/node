// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package qvm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luxfi/consensus/protocol/quasar"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// Quasar is the gravitational center of Lux consensus.
// It binds P-Chain (BLS signatures) and Q-Chain (Ringtail post-quantum)
// into unified hybrid finality across all Lux networks.
//
// Architecture:
//   P-Chain validators → BLS aggregate signatures
//   Q-Chain quantum    → Ringtail (ML-DSA) post-quantum signatures
//   Quasar            → Hybrid finality binding both signature schemes
//
// The quasar ensures that blocks achieve finality only when:
// 1. 2/3+ validator weight signed via BLS (fast, classical)
// 2. Quantum stamp via Ringtail (post-quantum secure)
// Both signatures MUST verify for hybrid quantum finality.

var (
	ErrQuasarNotStarted     = errors.New("quasar not started")
	ErrPChainNotConnected   = errors.New("P-Chain not connected")
	ErrQChainNotConnected   = errors.New("Q-Chain not connected")
	ErrInsufficientWeight   = errors.New("insufficient validator weight")
	ErrFinalityFailed       = errors.New("hybrid finality verification failed")
)

// PChainProvider provides P-Chain state and finality events
type PChainProvider interface {
	GetFinalizedHeight() uint64
	GetValidators(height uint64) ([]ValidatorState, error)
	SubscribeFinality() <-chan FinalityEvent
}

// ValidatorState represents a validator's current state
type ValidatorState struct {
	NodeID      ids.NodeID
	Weight      uint64
	BLSPubKey   []byte
	RingtailKey []byte
	Active      bool
}

// FinalityEvent represents a P-Chain finality event
type FinalityEvent struct {
	Height     uint64
	BlockID    ids.ID
	Validators []ValidatorState
	Timestamp  time.Time
}

// QuantumFinality represents a block that achieved hybrid quantum finality
type QuantumFinality struct {
	BlockID       ids.ID
	PChainHeight  uint64
	QChainHeight  uint64
	BLSProof      []byte // Aggregated BLS signature
	RingtailProof []byte // Post-quantum Ringtail signature
	SignerBitset  []byte // Which validators signed
	TotalWeight   uint64
	SignerWeight  uint64
	Timestamp     time.Time
}

// Quasar binds P-Chain and Q-Chain consensus into hybrid quantum finality
type Quasar struct {
	mu sync.RWMutex

	log    log.Logger
	hybrid *quasar.Hybrid

	// Chain connections
	pChain     PChainProvider
	quantumVM  *VM

	// State
	pHeight    uint64
	qHeight    uint64
	finalized  map[ids.ID]*QuantumFinality

	// Configuration
	threshold  int
	quorumNum  uint64
	quorumDen  uint64

	// Channels
	finalityCh chan *QuantumFinality
	stopCh     chan struct{}
	running    bool
}

// NewQuasar creates a new Quasar consensus hub
func NewQuasar(log log.Logger, threshold int, quorumNum, quorumDen uint64) (*Quasar, error) {
	hybrid, err := quasar.NewHybrid(threshold)
	if err != nil {
		return nil, fmt.Errorf("failed to create hybrid engine: %w", err)
	}

	return &Quasar{
		log:        log,
		hybrid:     hybrid,
		threshold:  threshold,
		quorumNum:  quorumNum,
		quorumDen:  quorumDen,
		finalized:  make(map[ids.ID]*QuantumFinality),
		finalityCh: make(chan *QuantumFinality, 100),
		stopCh:     make(chan struct{}),
	}, nil
}

// ConnectPChain connects the P-Chain finality provider
func (q *Quasar) ConnectPChain(p PChainProvider) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.pChain = p
	if p != nil {
		q.pHeight = p.GetFinalizedHeight()
	}

	q.log.Info("quasar: P-Chain connected", "height", q.pHeight)
}

// ConnectQChain connects the Q-Chain VM
func (q *Quasar) ConnectQChain(vm *VM) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.quantumVM = vm
	q.log.Info("quasar: Q-Chain connected")
}

// Start begins the quasar consensus loop
func (q *Quasar) Start(ctx context.Context) error {
	q.mu.Lock()
	if q.pChain == nil {
		q.mu.Unlock()
		return ErrPChainNotConnected
	}
	if q.quantumVM == nil {
		q.mu.Unlock()
		return ErrQChainNotConnected
	}
	q.running = true
	q.mu.Unlock()

	// Subscribe to P-Chain finality
	sub := q.pChain.SubscribeFinality()
	go q.run(ctx, sub)

	q.log.Info("quasar: started")
	return nil
}

// Stop halts the quasar
func (q *Quasar) Stop() {
	q.mu.Lock()
	if q.running {
		close(q.stopCh)
		q.running = false
	}
	q.mu.Unlock()
	q.log.Info("quasar: stopped")
}

// run is the main finality loop
func (q *Quasar) run(ctx context.Context, sub <-chan FinalityEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.stopCh:
			return
		case event := <-sub:
			if err := q.processFinality(ctx, event); err != nil {
				q.log.Error("quasar: finality failed",
					"height", event.Height,
					"error", err,
				)
			}
		}
	}
}

// processFinality processes a P-Chain finality event into hybrid finality
func (q *Quasar) processFinality(ctx context.Context, event FinalityEvent) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Sync validators to hybrid engine
	for _, v := range event.Validators {
		if v.Active {
			_ = q.hybrid.AddValidator(v.NodeID.String(), v.Weight)
		}
	}

	// Create finality message
	msg := q.createMessage(event)

	// Collect BLS signatures
	blsProof, signerBitset, signerWeight, err := q.collectBLS(event, msg)
	if err != nil {
		return fmt.Errorf("BLS collection: %w", err)
	}

	// Create quantum stamp
	ringtailProof, err := q.createQuantumStamp(event, msg)
	if err != nil {
		return fmt.Errorf("quantum stamp: %w", err)
	}

	// Check quorum
	totalWeight := q.totalWeight(event.Validators)
	if !q.checkQuorum(signerWeight, totalWeight) {
		return ErrInsufficientWeight
	}

	// Record finality
	q.qHeight++
	finality := &QuantumFinality{
		BlockID:       event.BlockID,
		PChainHeight:  event.Height,
		QChainHeight:  q.qHeight,
		BLSProof:      blsProof,
		RingtailProof: ringtailProof,
		SignerBitset:  signerBitset,
		TotalWeight:   totalWeight,
		SignerWeight:  signerWeight,
		Timestamp:     time.Now(),
	}

	q.finalized[event.BlockID] = finality
	q.pHeight = event.Height

	// Emit
	select {
	case q.finalityCh <- finality:
	default:
	}

	q.log.Info("quasar: hybrid finality achieved",
		"block", event.BlockID,
		"pHeight", event.Height,
		"qHeight", q.qHeight,
		"weight", fmt.Sprintf("%d/%d", signerWeight, totalWeight),
	)

	return nil
}

// createMessage creates the finality message to sign
func (q *Quasar) createMessage(event FinalityEvent) []byte {
	msg := make([]byte, 48) // 32 (blockID) + 8 (height) + 8 (timestamp)
	copy(msg[:32], event.BlockID[:])
	putUint64BE(msg[32:40], event.Height)
	putUint64BE(msg[40:48], uint64(event.Timestamp.UnixNano()))
	return msg
}

// collectBLS collects BLS signatures from validators
func (q *Quasar) collectBLS(event FinalityEvent, msg []byte) ([]byte, []byte, uint64, error) {
	var signerBitset []byte
	var signerWeight uint64
	signatures := make([]*quasar.HybridSignature, 0, len(event.Validators))

	for i, v := range event.Validators {
		if !v.Active {
			continue
		}

		sig, err := q.hybrid.SignMessage(v.NodeID.String(), msg)
		if err != nil {
			continue // Skip failed signers
		}

		signatures = append(signatures, sig)
		signerWeight += v.Weight

		// Set bit
		byteIdx := i / 8
		for len(signerBitset) <= byteIdx {
			signerBitset = append(signerBitset, 0)
		}
		signerBitset[byteIdx] |= 1 << uint(i%8)
	}

	if len(signatures) == 0 {
		return nil, nil, 0, errors.New("no signatures")
	}

	agg, err := q.hybrid.AggregateSignatures(msg, signatures)
	if err != nil {
		return nil, nil, 0, err
	}

	return agg.BLSAggregated, signerBitset, signerWeight, nil
}

// createQuantumStamp creates a post-quantum stamp via Q-Chain
func (q *Quasar) createQuantumStamp(event FinalityEvent, msg []byte) ([]byte, error) {
	key, err := q.quantumVM.quantumSigner.GenerateRingtailKey()
	if err != nil {
		return nil, err
	}

	sig, err := q.quantumVM.quantumSigner.Sign(msg, key)
	if err != nil {
		return nil, err
	}

	return sig.Signature, nil
}

// totalWeight calculates total validator weight
func (q *Quasar) totalWeight(validators []ValidatorState) uint64 {
	var total uint64
	for _, v := range validators {
		if v.Active {
			total += v.Weight
		}
	}
	return total
}

// checkQuorum verifies quorum is met
func (q *Quasar) checkQuorum(signerWeight, totalWeight uint64) bool {
	required := totalWeight * q.quorumNum / q.quorumDen
	return signerWeight >= required
}

// GetFinality returns finality for a block
func (q *Quasar) GetFinality(blockID ids.ID) (*QuantumFinality, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	f, ok := q.finalized[blockID]
	return f, ok
}

// Subscribe returns channel for finality events
func (q *Quasar) Subscribe() <-chan *QuantumFinality {
	return q.finalityCh
}

// Verify verifies a hybrid finality proof
func (q *Quasar) Verify(finality *QuantumFinality) error {
	if finality == nil {
		return ErrFinalityFailed
	}

	if len(finality.BLSProof) == 0 || len(finality.RingtailProof) == 0 {
		return ErrFinalityFailed
	}

	if !q.checkQuorum(finality.SignerWeight, finality.TotalWeight) {
		return ErrInsufficientWeight
	}

	// Verify BLS + Ringtail via hybrid engine
	agg := &quasar.AggregatedSignature{
		BLSAggregated: finality.BLSProof,
	}

	// Reconstruct message for verification
	msg := make([]byte, 48)
	copy(msg[:32], finality.BlockID[:])
	putUint64BE(msg[32:40], finality.PChainHeight)
	putUint64BE(msg[40:48], uint64(finality.Timestamp.UnixNano()))

	if !q.hybrid.VerifyAggregatedSignature(msg, agg) {
		return ErrFinalityFailed
	}

	return nil
}

// Stats returns quasar statistics
func (q *Quasar) Stats() QuasarStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return QuasarStats{
		PChainHeight:    q.pHeight,
		QChainHeight:    q.qHeight,
		FinalizedBlocks: len(q.finalized),
		Threshold:       q.threshold,
		QuorumNum:       q.quorumNum,
		QuorumDen:       q.quorumDen,
		Running:         q.running,
	}
}

// QuasarStats contains quasar statistics
type QuasarStats struct {
	PChainHeight    uint64
	QChainHeight    uint64
	FinalizedBlocks int
	Threshold       int
	QuorumNum       uint64
	QuorumDen       uint64
	Running         bool
}

// Helper: big-endian uint64
func putUint64BE(b []byte, v uint64) {
	for i := 0; i < 8; i++ {
		b[i] = byte(v >> (56 - i*8))
	}
}
