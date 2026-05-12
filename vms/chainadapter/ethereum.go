// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chainadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// EthereumAdapter implements light client verification for Ethereum
// Uses sync committee signatures for finality verification
type EthereumAdapter struct {
	mu               sync.RWMutex
	config           *ChainConfig
	syncCommittee    *SyncCommittee
	nextSyncCommittee *SyncCommittee
	headers          map[uint64]*EthereumHeader // Slot -> Header
	headersByHash    map[[32]byte]*EthereumHeader
	latestFinalized  uint64
	initialized      bool
}

// EthereumHeader represents an Ethereum beacon chain header
type EthereumHeader struct {
	Slot          uint64   `json:"slot"`
	ProposerIndex uint64   `json:"proposerIndex"`
	ParentRoot    [32]byte `json:"parentRoot"`
	StateRoot     [32]byte `json:"stateRoot"`
	BodyRoot      [32]byte `json:"bodyRoot"`

	// Computed
	Root          [32]byte `json:"root"` // Block root
	Finalized     bool     `json:"finalized"`
}

// SyncCommittee represents an Ethereum sync committee
type SyncCommittee struct {
	Pubkeys         [][48]byte `json:"pubkeys"`         // 512 BLS public keys
	AggregatePubkey [48]byte   `json:"aggregatePubkey"` // Aggregate of all pubkeys
	Period          uint64     `json:"period"`          // Sync committee period
}

// SyncAggregate represents sync committee aggregate signature
type SyncAggregate struct {
	SyncCommitteeBits      [64]byte  `json:"syncCommitteeBits"` // 512 bits
	SyncCommitteeSignature [96]byte  `json:"syncCommitteeSignature"` // BLS signature
}

// LightClientUpdate represents a light client update
type LightClientUpdate struct {
	AttestedHeader          *EthereumHeader  `json:"attestedHeader"`
	NextSyncCommittee       *SyncCommittee   `json:"nextSyncCommittee,omitempty"`
	NextSyncCommitteeBranch [][32]byte       `json:"nextSyncCommitteeBranch,omitempty"`
	FinalizedHeader         *EthereumHeader  `json:"finalizedHeader,omitempty"`
	FinalityBranch          [][32]byte       `json:"finalityBranch,omitempty"`
	SyncAggregate           *SyncAggregate   `json:"syncAggregate"`
	SignatureSlot           uint64           `json:"signatureSlot"`
}

// Ethereum consensus constants
const (
	SlotsPerEpoch          = 32
	EpochsPerSyncPeriod    = 256
	SlotsPerSyncPeriod     = SlotsPerEpoch * EpochsPerSyncPeriod // 8192 slots
	SyncCommitteeSize      = 512
	MinSyncCommitteeVotes  = 342 // ~2/3 of 512
	FinalityDelay          = 2   // 2 epochs for finality
)

// NewEthereumAdapter creates a new Ethereum light client adapter
func NewEthereumAdapter() *EthereumAdapter {
	return &EthereumAdapter{
		headers:       make(map[uint64]*EthereumHeader),
		headersByHash: make(map[[32]byte]*EthereumHeader),
	}
}

// ChainID returns the Ethereum chain ID
func (a *EthereumAdapter) ChainID() ChainID {
	return idEthereum
}

// ChainName returns "Ethereum"
func (a *EthereumAdapter) ChainName() string {
	return "Ethereum"
}

// VerificationMode returns ModeLightClient
func (a *EthereumAdapter) VerificationMode() VerificationMode {
	return ModeLightClient
}

// Initialize initializes the adapter
func (a *EthereumAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.initialized {
		return nil
	}

	a.config = config

	// In production, would bootstrap from a trusted source
	// For now, we'll accept the first sync committee provided
	a.initialized = true
	return nil
}

// VerifyBlockHeader verifies an Ethereum block header
func (a *EthereumAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != idEthereum {
		return ErrChainNotSupported
	}

	// Decode Ethereum header from ExtraData
	ethHeader, err := decodeEthereumHeader(header.ExtraData)
	if err != nil {
		return fmt.Errorf("failed to decode Ethereum header: %w", err)
	}

	// Verify the header against sync committee
	if err := a.verifyHeaderSignature(ethHeader, header.FinalityProof); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	// Store the header
	a.mu.Lock()
	a.headers[ethHeader.Slot] = ethHeader
	a.headersByHash[ethHeader.Root] = ethHeader
	if ethHeader.Finalized && ethHeader.Slot > a.latestFinalized {
		a.latestFinalized = ethHeader.Slot
	}
	a.mu.Unlock()

	return nil
}

// ProcessLightClientUpdate processes a light client update
func (a *EthereumAdapter) ProcessLightClientUpdate(update *LightClientUpdate) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Verify sync aggregate has enough participation
	votes := countSyncCommitteeVotes(update.SyncAggregate.SyncCommitteeBits)
	if votes < MinSyncCommitteeVotes {
		return ErrQuorumNotMet
	}

	// Verify sync committee signature
	if a.syncCommittee != nil {
		if err := a.verifySyncAggregateSignature(update); err != nil {
			return fmt.Errorf("sync aggregate verification failed: %w", err)
		}
	}

	// Process finalized header
	if update.FinalizedHeader != nil {
		update.FinalizedHeader.Finalized = true
		a.headers[update.FinalizedHeader.Slot] = update.FinalizedHeader
		a.headersByHash[update.FinalizedHeader.Root] = update.FinalizedHeader
		if update.FinalizedHeader.Slot > a.latestFinalized {
			a.latestFinalized = update.FinalizedHeader.Slot
		}
	}

	// Update sync committee if provided
	if update.NextSyncCommittee != nil {
		// Verify next sync committee branch
		if err := verifyMerkleBranch(
			computeSyncCommitteeRoot(update.NextSyncCommittee),
			update.NextSyncCommitteeBranch,
			update.AttestedHeader.StateRoot,
		); err != nil {
			return fmt.Errorf("next sync committee branch verification failed: %w", err)
		}
		a.nextSyncCommittee = update.NextSyncCommittee
	}

	// Rotate sync committee if we've crossed a period boundary
	currentPeriod := update.AttestedHeader.Slot / SlotsPerSyncPeriod
	if a.syncCommittee != nil && currentPeriod > a.syncCommittee.Period {
		a.syncCommittee = a.nextSyncCommittee
		a.nextSyncCommittee = nil
	}

	return nil
}

// VerifyTransaction verifies an Ethereum transaction inclusion
func (a *EthereumAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error {
	if proof.ChainID != idEthereum {
		return ErrChainNotSupported
	}

	// Get the block header
	a.mu.RLock()
	header, ok := a.headersByHash[proof.BlockHash]
	a.mu.RUnlock()

	if !ok {
		return ErrHeaderNotFound
	}

	// Verify block is finalized
	if !header.Finalized {
		return ErrBlockNotFinalized
	}

	// Verify Merkle proof for transaction
	// Ethereum uses MPT (Merkle Patricia Trie)
	if !verifyMPTProof(proof.TxHash[:], proof.MerkleProof) {
		return ErrInvalidMerkleProof
	}

	return nil
}

// VerifyMessage verifies a cross-chain message from Ethereum
func (a *EthereumAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error {
	if msg.SourceChain != idEthereum {
		return ErrChainNotSupported
	}

	// Parse the source proof (contains event log proof)
	eventProof, err := parseEthereumEventProof(msg.SourceProof)
	if err != nil {
		return fmt.Errorf("failed to parse event proof: %w", err)
	}

	// Verify the event
	event := &ChainEvent{
		ChainID:     idEthereum,
		BlockNumber: msg.SourceBlock,
		TxHash:      msg.SourceTxHash,
		Proof:       eventProof,
	}

	return a.VerifyEvent(ctx, event)
}

// VerifyEvent verifies an Ethereum event/log
func (a *EthereumAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error {
	if event.ChainID != idEthereum {
		return ErrChainNotSupported
	}

	// Get the block containing this event
	a.mu.RLock()
	var header *EthereumHeader
	for _, h := range a.headers {
		if h.Slot == event.BlockNumber {
			header = h
			break
		}
	}
	a.mu.RUnlock()

	if header == nil {
		return ErrHeaderNotFound
	}

	// Verify the block is finalized
	if !header.Finalized {
		return ErrBlockNotFinalized
	}

	// Verify event log proof against receipt root
	if !verifyReceiptProof(event) {
		return ErrInvalidProof
	}

	return nil
}

// GetLatestFinalizedBlock returns the latest finalized slot
func (a *EthereumAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestFinalized, nil
}

// GetRequiredConfirmations returns 1 for Ethereum (sync committee finality)
func (a *EthereumAdapter) GetRequiredConfirmations() uint64 {
	return 1
}

// GetBlockTime returns Ethereum block time (12 seconds)
func (a *EthereumAdapter) GetBlockTime() time.Duration {
	return 12 * time.Second
}

// IsFinalized checks if a slot is finalized
func (a *EthereumAdapter) IsFinalized(ctx context.Context, slot uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	header, ok := a.headers[slot]
	if !ok {
		return false, nil
	}
	return header.Finalized, nil
}

// GetValidatorSet returns the current sync committee as validator set
func (a *EthereumAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.syncCommittee == nil {
		return nil, nil
	}

	validators := make([]*Validator, len(a.syncCommittee.Pubkeys))
	for i, pubkey := range a.syncCommittee.Pubkeys {
		validators[i] = &Validator{
			PublicKey:   pubkey[:],
			VotingPower: 1, // Equal voting power
		}
	}

	return &ValidatorSet{
		ChainID:    idEthereum,
		Epoch:      a.syncCommittee.Period,
		Validators: validators,
		TotalStake: uint64(len(validators)),
		Threshold:  MinSyncCommitteeVotes,
	}, nil
}

// Close closes the adapter
func (a *EthereumAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.headers = nil
	a.headersByHash = nil
	a.syncCommittee = nil
	a.nextSyncCommittee = nil
	a.initialized = false
	return nil
}

// ======== Ethereum-specific verification functions ========

// verifyHeaderSignature verifies a header's sync committee signature
func (a *EthereumAdapter) verifyHeaderSignature(header *EthereumHeader, proof []byte) error {
	if a.syncCommittee == nil {
		// Accept if no sync committee is set (bootstrapping)
		return nil
	}

	// Parse sync aggregate from proof
	if len(proof) < 160 { // 64 bytes bits + 96 bytes signature
		return errors.New("proof too short")
	}

	var aggregate SyncAggregate
	copy(aggregate.SyncCommitteeBits[:], proof[0:64])
	copy(aggregate.SyncCommitteeSignature[:], proof[64:160])

	// Verify enough participation
	votes := countSyncCommitteeVotes(aggregate.SyncCommitteeBits)
	if votes < MinSyncCommitteeVotes {
		return ErrQuorumNotMet
	}

	// In production, would verify BLS signature here
	// aggregate.SyncCommitteeSignature should sign the header root

	return nil
}

// verifySyncAggregateSignature verifies a sync aggregate signature
func (a *EthereumAdapter) verifySyncAggregateSignature(update *LightClientUpdate) error {
	// In production, would verify BLS signature against sync committee
	// For now, just check participation

	votes := countSyncCommitteeVotes(update.SyncAggregate.SyncCommitteeBits)
	if votes < MinSyncCommitteeVotes {
		return ErrQuorumNotMet
	}

	return nil
}

// countSyncCommitteeVotes counts the number of 1 bits in the sync committee bits
func countSyncCommitteeVotes(bits [64]byte) int {
	count := 0
	for _, b := range bits {
		for b != 0 {
			count += int(b & 1)
			b >>= 1
		}
	}
	return count
}

// decodeEthereumHeader decodes an Ethereum beacon header
func decodeEthereumHeader(data []byte) (*EthereumHeader, error) {
	if len(data) < 168 { // Minimum header size
		return nil, fmt.Errorf("header too short: %d bytes", len(data))
	}

	header := &EthereumHeader{}

	// Slot (8 bytes)
	header.Slot = binary.LittleEndian.Uint64(data[0:8])

	// ProposerIndex (8 bytes)
	header.ProposerIndex = binary.LittleEndian.Uint64(data[8:16])

	// ParentRoot (32 bytes)
	copy(header.ParentRoot[:], data[16:48])

	// StateRoot (32 bytes)
	copy(header.StateRoot[:], data[48:80])

	// BodyRoot (32 bytes)
	copy(header.BodyRoot[:], data[80:112])

	// Compute block root
	header.Root = computeBeaconBlockRoot(header)

	return header, nil
}

// computeBeaconBlockRoot computes the beacon block root (SSZ hash tree root)
func computeBeaconBlockRoot(header *EthereumHeader) [32]byte {
	// Simplified: hash all fields together
	// In production, would use proper SSZ hash tree root
	h := sha256.New()
	binary.Write(h, binary.LittleEndian, header.Slot)
	binary.Write(h, binary.LittleEndian, header.ProposerIndex)
	h.Write(header.ParentRoot[:])
	h.Write(header.StateRoot[:])
	h.Write(header.BodyRoot[:])

	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// computeSyncCommitteeRoot computes the sync committee root
func computeSyncCommitteeRoot(committee *SyncCommittee) [32]byte {
	h := sha256.New()
	for _, pubkey := range committee.Pubkeys {
		h.Write(pubkey[:])
	}
	h.Write(committee.AggregatePubkey[:])

	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// verifyMerkleBranch verifies a Merkle branch proof
func verifyMerkleBranch(leaf [32]byte, branch [][32]byte, root [32]byte) error {
	current := leaf
	for _, sibling := range branch {
		h := sha256.New()
		// Ordering based on position (simplified)
		if bytes.Compare(current[:], sibling[:]) < 0 {
			h.Write(current[:])
			h.Write(sibling[:])
		} else {
			h.Write(sibling[:])
			h.Write(current[:])
		}
		copy(current[:], h.Sum(nil))
	}

	if current != root {
		return errors.New("merkle branch verification failed")
	}
	return nil
}

// verifyMPTProof verifies a Merkle Patricia Trie proof
func verifyMPTProof(key []byte, proof [][]byte) bool {
	// Simplified MPT verification
	// In production, would implement full MPT verification
	return len(proof) > 0
}

// verifyReceiptProof verifies a receipt inclusion proof
func verifyReceiptProof(event *ChainEvent) bool {
	// Simplified receipt verification
	// In production, would verify against receipt root
	return len(event.Proof) > 0
}

// parseEthereumEventProof parses an event proof from bytes
func parseEthereumEventProof(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty proof")
	}
	return data, nil
}
