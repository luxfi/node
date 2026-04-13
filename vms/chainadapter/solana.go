// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// SolanaAdapter implements vote attestation verification for Solana
type SolanaAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	slots           map[uint64]*SolanaSlot
	validators      map[[32]byte]*SolanaValidator
	currentEpoch    uint64
	latestFinalized uint64
	initialized     bool
}

// SolanaSlot represents a Solana slot (block)
type SolanaSlot struct {
	Slot           uint64   `json:"slot"`
	ParentSlot     uint64   `json:"parentSlot"`
	Blockhash      [32]byte `json:"blockhash"`
	PreviousHash   [32]byte `json:"previousHash"`
	TransactionRoot [32]byte `json:"transactionRoot"`
	Epoch          uint64   `json:"epoch"`
	LeaderPubkey   [32]byte `json:"leaderPubkey"`

	// Vote state
	VoteCount      int      `json:"voteCount"`
	TotalStake     uint64   `json:"totalStake"`
	VotedStake     uint64   `json:"votedStake"`
	Finalized      bool     `json:"finalized"`
}

// SolanaValidator represents a Solana validator
type SolanaValidator struct {
	Pubkey       [32]byte `json:"pubkey"`
	VoteAccount  [32]byte `json:"voteAccount"`
	Stake        uint64   `json:"stake"`
	Commission   uint8    `json:"commission"`
	Activated    bool     `json:"activated"`
	LastVote     uint64   `json:"lastVote"`
}

// SolanaVote represents a validator vote
type SolanaVote struct {
	Slot         uint64   `json:"slot"`
	Hash         [32]byte `json:"hash"`
	ValidatorPubkey [32]byte `json:"validatorPubkey"`
	Signature    [64]byte `json:"signature"`
	Timestamp    int64    `json:"timestamp"`
}

// SolanaVoteAttestation contains aggregated votes for a slot
type SolanaVoteAttestation struct {
	Slot           uint64         `json:"slot"`
	Blockhash      [32]byte       `json:"blockhash"`
	Votes          []*SolanaVote  `json:"votes"`
	VotedStake     uint64         `json:"votedStake"`
	TotalStake     uint64         `json:"totalStake"`
	Finalized      bool           `json:"finalized"`
}

// Solana consensus constants
const (
	SolanaSlotsPerEpoch     = 432000 // ~2 days
	SolanaSlotDuration      = 400 * time.Millisecond
	OptimisticConfirmation  = 32     // Slots for optimistic confirmation
	FinalizedConfirmation   = 32     // After 2/3 stake votes
	SupermajorityStake      = 67     // 2/3 supermajority in percentage
)

// NewSolanaAdapter creates a new Solana vote attestation adapter
func NewSolanaAdapter() *SolanaAdapter {
	return &SolanaAdapter{
		slots:      make(map[uint64]*SolanaSlot),
		validators: make(map[[32]byte]*SolanaValidator),
	}
}

// ChainID returns the Solana chain ID
func (a *SolanaAdapter) ChainID() ChainID {
	return ChainSolana
}

// ChainName returns "Solana"
func (a *SolanaAdapter) ChainName() string {
	return "Solana"
}

// VerificationMode returns ModeVoteAttestation
func (a *SolanaAdapter) VerificationMode() VerificationMode {
	return ModeVoteAttestation
}

// Initialize initializes the adapter
func (a *SolanaAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.initialized {
		return nil
	}

	a.config = config
	a.initialized = true
	return nil
}

// VerifyBlockHeader verifies a Solana slot header
func (a *SolanaAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainSolana {
		return ErrChainNotSupported
	}

	// Decode Solana slot from ExtraData
	slot, err := decodeSolanaSlot(header.ExtraData)
	if err != nil {
		return fmt.Errorf("failed to decode Solana slot: %w", err)
	}

	// Verify vote attestation from finality proof
	attestation, err := decodeSolanaVoteAttestation(header.FinalityProof)
	if err != nil {
		return fmt.Errorf("failed to decode vote attestation: %w", err)
	}

	// Verify votes reach supermajority
	if err := a.verifyVoteAttestation(slot, attestation); err != nil {
		return fmt.Errorf("vote attestation verification failed: %w", err)
	}

	// Store the slot
	a.mu.Lock()
	a.slots[slot.Slot] = slot
	if slot.Finalized && slot.Slot > a.latestFinalized {
		a.latestFinalized = slot.Slot
	}
	a.mu.Unlock()

	return nil
}

// ProcessVoteAttestation processes a vote attestation for a slot
func (a *SolanaAdapter) ProcessVoteAttestation(attestation *SolanaVoteAttestation) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	slot, ok := a.slots[attestation.Slot]
	if !ok {
		// Create slot entry
		slot = &SolanaSlot{
			Slot:      attestation.Slot,
			Blockhash: attestation.Blockhash,
		}
		a.slots[attestation.Slot] = slot
	}

	// Verify each vote
	for _, vote := range attestation.Votes {
		if err := a.verifyVote(vote); err != nil {
			continue // Skip invalid votes
		}
		slot.VoteCount++
	}

	// Update stake counts
	slot.VotedStake = attestation.VotedStake
	slot.TotalStake = attestation.TotalStake

	// Check for finalization
	stakePercentage := float64(slot.VotedStake) / float64(slot.TotalStake) * 100
	if stakePercentage >= SupermajorityStake {
		slot.Finalized = true
		if slot.Slot > a.latestFinalized {
			a.latestFinalized = slot.Slot
		}
	}

	return nil
}

// UpdateValidatorSet updates the validator set for an epoch
func (a *SolanaAdapter) UpdateValidatorSet(validators []*SolanaValidator, epoch uint64) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Clear old validators
	a.validators = make(map[[32]byte]*SolanaValidator)

	// Add new validators
	for _, v := range validators {
		a.validators[v.Pubkey] = v
	}

	a.currentEpoch = epoch
	return nil
}

// VerifyTransaction verifies a Solana transaction inclusion
func (a *SolanaAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error {
	if proof.ChainID != ChainSolana {
		return ErrChainNotSupported
	}

	// Get the slot
	a.mu.RLock()
	slot, ok := a.slots[proof.BlockNumber]
	a.mu.RUnlock()

	if !ok {
		return ErrHeaderNotFound
	}

	// Verify slot is finalized
	if !slot.Finalized {
		return ErrBlockNotFinalized
	}

	// Verify Merkle proof
	if !verifySolanaMerkleProof(proof.TxHash, slot.TransactionRoot, proof.MerkleProof) {
		return ErrInvalidMerkleProof
	}

	return nil
}

// VerifyMessage verifies a cross-chain message from Solana
func (a *SolanaAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error {
	if msg.SourceChain != ChainSolana {
		return ErrChainNotSupported
	}

	// For Solana messages, verify the transaction containing the message
	proof := &TxInclusionProof{
		ChainID:     ChainSolana,
		BlockNumber: msg.SourceBlock,
		TxHash:      msg.SourceTxHash,
	}

	// Parse merkle proof from SourceProof
	if len(msg.SourceProof) > 0 {
		proof.MerkleProof = parseSolanaMerkleProof(msg.SourceProof)
	}

	return a.VerifyTransaction(ctx, proof)
}

// VerifyEvent verifies a Solana program log
func (a *SolanaAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error {
	if event.ChainID != ChainSolana {
		return ErrChainNotSupported
	}

	// Get the slot
	a.mu.RLock()
	slot, ok := a.slots[event.BlockNumber]
	a.mu.RUnlock()

	if !ok {
		return ErrHeaderNotFound
	}

	// Verify slot is finalized
	if !slot.Finalized {
		return ErrBlockNotFinalized
	}

	// Verify program log proof
	if !verifySolanaProgramLog(event) {
		return ErrInvalidProof
	}

	return nil
}

// GetLatestFinalizedBlock returns the latest finalized slot
func (a *SolanaAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestFinalized, nil
}

// GetRequiredConfirmations returns required confirmations
func (a *SolanaAdapter) GetRequiredConfirmations() uint64 {
	if a.config != nil {
		return a.config.RequiredConfirmations
	}
	return OptimisticConfirmation
}

// GetBlockTime returns Solana slot duration
func (a *SolanaAdapter) GetBlockTime() time.Duration {
	return SolanaSlotDuration
}

// IsFinalized checks if a slot is finalized
func (a *SolanaAdapter) IsFinalized(ctx context.Context, slot uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	s, ok := a.slots[slot]
	if !ok {
		return false, nil
	}
	return s.Finalized, nil
}

// GetValidatorSet returns the current validator set
func (a *SolanaAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	validators := make([]*Validator, 0, len(a.validators))
	var totalStake uint64

	for _, v := range a.validators {
		if v.Activated {
			validators = append(validators, &Validator{
				Address:     v.VoteAccount[:],
				PublicKey:   v.Pubkey[:],
				Stake:       v.Stake,
				VotingPower: v.Stake,
			})
			totalStake += v.Stake
		}
	}

	return &ValidatorSet{
		ChainID:    ChainSolana,
		Epoch:      a.currentEpoch,
		Validators: validators,
		TotalStake: totalStake,
		Threshold:  totalStake * SupermajorityStake / 100,
	}, nil
}

// Close closes the adapter
func (a *SolanaAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.slots = nil
	a.validators = nil
	a.initialized = false
	return nil
}

// ======== Solana-specific verification functions ========

// verifyVoteAttestation verifies a vote attestation
func (a *SolanaAdapter) verifyVoteAttestation(slot *SolanaSlot, attestation *SolanaVoteAttestation) error {
	// Verify blockhash matches
	if slot.Blockhash != attestation.Blockhash {
		return errors.New("blockhash mismatch")
	}

	// Verify each vote
	validVotes := 0
	votedStake := uint64(0)

	for _, vote := range attestation.Votes {
		if err := a.verifyVote(vote); err != nil {
			continue
		}

		// Get validator stake
		a.mu.RLock()
		validator, ok := a.validators[vote.ValidatorPubkey]
		a.mu.RUnlock()

		if ok && validator.Activated {
			validVotes++
			votedStake += validator.Stake
		}
	}

	// Check supermajority
	if attestation.TotalStake > 0 {
		stakePercentage := float64(votedStake) / float64(attestation.TotalStake) * 100
		if stakePercentage < SupermajorityStake {
			return ErrQuorumNotMet
		}
	}

	slot.VoteCount = validVotes
	slot.VotedStake = votedStake
	slot.TotalStake = attestation.TotalStake
	slot.Finalized = true

	return nil
}

// verifyVote verifies a single vote signature
func (a *SolanaAdapter) verifyVote(vote *SolanaVote) error {
	// Build vote message
	msg := make([]byte, 40)
	binary.LittleEndian.PutUint64(msg[0:8], vote.Slot)
	copy(msg[8:40], vote.Hash[:])

	// Verify Ed25519 signature
	if !ed25519.Verify(vote.ValidatorPubkey[:], msg, vote.Signature[:]) {
		return ErrInvalidSignature
	}

	return nil
}

// decodeSolanaSlot decodes a Solana slot from bytes
func decodeSolanaSlot(data []byte) (*SolanaSlot, error) {
	if len(data) < 144 {
		return nil, fmt.Errorf("slot data too short: %d bytes", len(data))
	}

	slot := &SolanaSlot{}

	// Slot number (8 bytes)
	slot.Slot = binary.LittleEndian.Uint64(data[0:8])

	// Parent slot (8 bytes)
	slot.ParentSlot = binary.LittleEndian.Uint64(data[8:16])

	// Blockhash (32 bytes)
	copy(slot.Blockhash[:], data[16:48])

	// Previous hash (32 bytes)
	copy(slot.PreviousHash[:], data[48:80])

	// Transaction root (32 bytes)
	copy(slot.TransactionRoot[:], data[80:112])

	// Epoch (8 bytes)
	slot.Epoch = binary.LittleEndian.Uint64(data[112:120])

	// Leader pubkey (32 bytes)
	copy(slot.LeaderPubkey[:], data[120:152])

	return slot, nil
}

// decodeSolanaVoteAttestation decodes a vote attestation from bytes
func decodeSolanaVoteAttestation(data []byte) (*SolanaVoteAttestation, error) {
	if len(data) < 56 {
		return nil, errors.New("attestation data too short")
	}

	att := &SolanaVoteAttestation{}

	// Slot (8 bytes)
	att.Slot = binary.LittleEndian.Uint64(data[0:8])

	// Blockhash (32 bytes)
	copy(att.Blockhash[:], data[8:40])

	// Vote count (4 bytes)
	voteCount := binary.LittleEndian.Uint32(data[40:44])

	// Voted stake (8 bytes)
	att.VotedStake = binary.LittleEndian.Uint64(data[44:52])

	// Total stake (8 bytes)
	att.TotalStake = binary.LittleEndian.Uint64(data[52:60])

	// Parse votes
	offset := 60
	att.Votes = make([]*SolanaVote, 0, voteCount)
	for i := uint32(0); i < voteCount && offset+104 <= len(data); i++ {
		vote := &SolanaVote{
			Slot: binary.LittleEndian.Uint64(data[offset : offset+8]),
		}
		copy(vote.Hash[:], data[offset+8:offset+40])
		copy(vote.ValidatorPubkey[:], data[offset+40:offset+72])
		copy(vote.Signature[:], data[offset+72:offset+136])
		att.Votes = append(att.Votes, vote)
		offset += 136
	}

	// Check finalization
	if att.TotalStake > 0 {
		stakePercentage := float64(att.VotedStake) / float64(att.TotalStake) * 100
		att.Finalized = stakePercentage >= SupermajorityStake
	}

	return att, nil
}

// verifySolanaMerkleProof verifies a Solana Merkle proof
func verifySolanaMerkleProof(txHash, root [32]byte, proof [][]byte) bool {
	hash := txHash

	for _, sibling := range proof {
		h := sha256.New()
		if bytes.Compare(hash[:], sibling) < 0 {
			h.Write(hash[:])
			h.Write(sibling)
		} else {
			h.Write(sibling)
			h.Write(hash[:])
		}
		copy(hash[:], h.Sum(nil))
	}

	return hash == root
}

// parseSolanaMerkleProof parses a Merkle proof from bytes
func parseSolanaMerkleProof(data []byte) [][]byte {
	if len(data) < 4 {
		return nil
	}

	proofCount := binary.LittleEndian.Uint32(data[0:4])
	proof := make([][]byte, 0, proofCount)

	offset := 4
	for i := uint32(0); i < proofCount && offset+32 <= len(data); i++ {
		proof = append(proof, data[offset:offset+32])
		offset += 32
	}

	return proof
}

// verifySolanaProgramLog verifies a Solana program log proof
func verifySolanaProgramLog(event *ChainEvent) bool {
	// Simplified verification
	// In production, would verify against transaction logs
	return len(event.Proof) > 0
}
