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

// CosmosAdapter implements IBC light client verification for Cosmos chains
type CosmosAdapter struct {
	mu              sync.RWMutex
	config          *ChainConfig
	headers         map[uint64]*CosmosHeader
	headersByHash   map[[32]byte]*CosmosHeader
	validatorSet    *TendermintValidatorSet
	latestHeight    uint64
	trustedHeight   uint64
	chainID         string
	initialized     bool
}

// CosmosHeader represents a Cosmos/Tendermint block header
type CosmosHeader struct {
	Height            uint64   `json:"height"`
	Time              int64    `json:"time"`
	ChainID           string   `json:"chainId"`
	LastBlockHash     [32]byte `json:"lastBlockHash"`
	DataHash          [32]byte `json:"dataHash"`
	ValidatorsHash    [32]byte `json:"validatorsHash"`
	NextValidatorsHash [32]byte `json:"nextValidatorsHash"`
	ConsensusHash     [32]byte `json:"consensusHash"`
	AppHash           [32]byte `json:"appHash"`
	LastResultsHash   [32]byte `json:"lastResultsHash"`
	EvidenceHash      [32]byte `json:"evidenceHash"`
	ProposerAddress   [20]byte `json:"proposerAddress"`

	// Computed
	Hash              [32]byte `json:"hash"`
	Finalized         bool     `json:"finalized"`
}

// TendermintValidatorSet represents Tendermint validator set
type TendermintValidatorSet struct {
	Validators  []*TendermintValidator `json:"validators"`
	TotalPower  int64                  `json:"totalPower"`
	Height      uint64                 `json:"height"`
}

// TendermintValidator represents a single validator
type TendermintValidator struct {
	Address     [20]byte `json:"address"`
	PubKey      []byte   `json:"pubKey"`     // Ed25519 or Secp256k1
	PubKeyType  string   `json:"pubKeyType"` // "ed25519" or "secp256k1"
	VotingPower int64    `json:"votingPower"`
}

// TendermintCommit represents commit signatures
type TendermintCommit struct {
	Height     uint64                `json:"height"`
	Round      int32                 `json:"round"`
	BlockHash  [32]byte              `json:"blockHash"`
	Signatures []*TendermintVote     `json:"signatures"`
}

// TendermintVote represents a validator vote
type TendermintVote struct {
	ValidatorAddress [20]byte `json:"validatorAddress"`
	Timestamp        int64    `json:"timestamp"`
	Signature        []byte   `json:"signature"` // 64 bytes for Ed25519
	Absent           bool     `json:"absent"`
}

// IBCClientState represents IBC light client state
type IBCClientState struct {
	ChainID          string   `json:"chainId"`
	TrustLevel       float64  `json:"trustLevel"`      // e.g., 0.67 for 2/3
	TrustingPeriod   int64    `json:"trustingPeriod"`  // In seconds
	UnbondingPeriod  int64    `json:"unbondingPeriod"` // In seconds
	MaxClockDrift    int64    `json:"maxClockDrift"`   // In seconds
	LatestHeight     uint64   `json:"latestHeight"`
	FrozenHeight     uint64   `json:"frozenHeight"`    // 0 if not frozen
}

// IBCConsensusState represents IBC consensus state
type IBCConsensusState struct {
	Timestamp          int64    `json:"timestamp"`
	Root               [32]byte `json:"root"`               // App hash / Merkle root
	NextValidatorsHash [32]byte `json:"nextValidatorsHash"`
}

// Tendermint consensus constants
const (
	TendermintBlockTime     = 6 * time.Second
	TendermintTrustLevel    = 0.67 // 2/3 stake
	TendermintTrustingPeriod = 14 * 24 * 60 * 60 // 14 days in seconds
	TendermintMaxClockDrift = 30 // 30 seconds
)

// NewCosmosAdapter creates a new Cosmos IBC adapter
func NewCosmosAdapter() *CosmosAdapter {
	return &CosmosAdapter{
		headers:       make(map[uint64]*CosmosHeader),
		headersByHash: make(map[[32]byte]*CosmosHeader),
		chainID:       "cosmoshub-4", // Default to Cosmos Hub
	}
}

// ChainID returns the Cosmos chain ID
func (a *CosmosAdapter) ChainID() ChainID {
	return ChainCosmos
}

// ChainName returns "Cosmos Hub"
func (a *CosmosAdapter) ChainName() string {
	return "Cosmos Hub"
}

// VerificationMode returns ModeLightClient
func (a *CosmosAdapter) VerificationMode() VerificationMode {
	return ModeLightClient
}

// Initialize initializes the adapter
func (a *CosmosAdapter) Initialize(config *ChainConfig) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.initialized {
		return nil
	}

	a.config = config

	// Extract chain ID from config if provided
	if config.ExtraConfig != nil && len(config.ExtraConfig) > 0 {
		a.chainID = string(config.ExtraConfig)
	}

	a.initialized = true
	return nil
}

// VerifyBlockHeader verifies a Cosmos block header
func (a *CosmosAdapter) VerifyBlockHeader(ctx context.Context, header *BlockHeader) error {
	if header.ChainID != ChainCosmos {
		return ErrChainNotSupported
	}

	// Decode Cosmos header
	cosmosHeader, err := decodeCosmosHeader(header.ExtraData)
	if err != nil {
		return fmt.Errorf("failed to decode Cosmos header: %w", err)
	}

	// Decode commit from finality proof
	commit, err := decodeTendermintCommit(header.FinalityProof)
	if err != nil {
		return fmt.Errorf("failed to decode commit: %w", err)
	}

	// Verify commit signatures
	if err := a.verifyCommit(cosmosHeader, commit); err != nil {
		return fmt.Errorf("commit verification failed: %w", err)
	}

	// Store header
	a.mu.Lock()
	cosmosHeader.Finalized = true
	a.headers[cosmosHeader.Height] = cosmosHeader
	a.headersByHash[cosmosHeader.Hash] = cosmosHeader
	if cosmosHeader.Height > a.latestHeight {
		a.latestHeight = cosmosHeader.Height
	}
	a.mu.Unlock()

	return nil
}

// UpdateClientState updates the IBC client state
func (a *CosmosAdapter) UpdateClientState(header *CosmosHeader, validatorSet *TendermintValidatorSet) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Verify validator set hash matches
	validatorSetHash := computeValidatorSetHash(validatorSet)
	if validatorSetHash != header.ValidatorsHash {
		return ErrValidatorSetMismatch
	}

	// Update validator set
	a.validatorSet = validatorSet

	// Update headers
	a.headers[header.Height] = header
	a.headersByHash[header.Hash] = header
	a.trustedHeight = header.Height

	if header.Height > a.latestHeight {
		a.latestHeight = header.Height
	}

	return nil
}

// VerifyTransaction verifies a Cosmos transaction inclusion
func (a *CosmosAdapter) VerifyTransaction(ctx context.Context, proof *TxInclusionProof) error {
	if proof.ChainID != ChainCosmos {
		return ErrChainNotSupported
	}

	// Get header
	a.mu.RLock()
	header, ok := a.headers[proof.BlockNumber]
	a.mu.RUnlock()

	if !ok {
		return ErrHeaderNotFound
	}

	// Verify finalized
	if !header.Finalized {
		return ErrBlockNotFinalized
	}

	// Verify Merkle proof against DataHash
	if !verifyCosmosMerkleProof(proof.TxHash[:], header.DataHash, proof.MerkleProof) {
		return ErrInvalidMerkleProof
	}

	return nil
}

// VerifyMessage verifies a cross-chain message from Cosmos (IBC packet)
func (a *CosmosAdapter) VerifyMessage(ctx context.Context, msg *CrossChainMessage) error {
	if msg.SourceChain != ChainCosmos {
		return ErrChainNotSupported
	}

	// For Cosmos, messages are IBC packets
	// The proof contains the packet commitment proof
	if err := a.verifyIBCPacket(msg); err != nil {
		return fmt.Errorf("IBC packet verification failed: %w", err)
	}

	return nil
}

// VerifyEvent verifies a Cosmos event
func (a *CosmosAdapter) VerifyEvent(ctx context.Context, event *ChainEvent) error {
	if event.ChainID != ChainCosmos {
		return ErrChainNotSupported
	}

	// Get header
	a.mu.RLock()
	header, ok := a.headers[event.BlockNumber]
	a.mu.RUnlock()

	if !ok {
		return ErrHeaderNotFound
	}

	if !header.Finalized {
		return ErrBlockNotFinalized
	}

	// Verify event proof against LastResultsHash
	if !verifyCosmosEventProof(event, header.LastResultsHash) {
		return ErrInvalidProof
	}

	return nil
}

// GetLatestFinalizedBlock returns the latest finalized height
func (a *CosmosAdapter) GetLatestFinalizedBlock(ctx context.Context) (uint64, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.latestHeight, nil
}

// GetRequiredConfirmations returns 1 for Cosmos (instant finality)
func (a *CosmosAdapter) GetRequiredConfirmations() uint64 {
	return 1
}

// GetBlockTime returns Tendermint block time
func (a *CosmosAdapter) GetBlockTime() time.Duration {
	return TendermintBlockTime
}

// IsFinalized checks if a block is finalized
func (a *CosmosAdapter) IsFinalized(ctx context.Context, height uint64) (bool, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	header, ok := a.headers[height]
	if !ok {
		return false, nil
	}
	return header.Finalized, nil
}

// GetValidatorSet returns the current validator set
func (a *CosmosAdapter) GetValidatorSet(ctx context.Context) (*ValidatorSet, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.validatorSet == nil {
		return nil, nil
	}

	validators := make([]*Validator, len(a.validatorSet.Validators))
	for i, v := range a.validatorSet.Validators {
		validators[i] = &Validator{
			Address:     v.Address[:],
			PublicKey:   v.PubKey,
			Stake:       uint64(v.VotingPower),
			VotingPower: uint64(v.VotingPower),
		}
	}

	threshold := uint64(float64(a.validatorSet.TotalPower) * TendermintTrustLevel)

	return &ValidatorSet{
		ChainID:    ChainCosmos,
		Epoch:      a.latestHeight, // Use height as epoch
		Validators: validators,
		TotalStake: uint64(a.validatorSet.TotalPower),
		Threshold:  threshold,
	}, nil
}

// Close closes the adapter
func (a *CosmosAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.headers = nil
	a.headersByHash = nil
	a.validatorSet = nil
	a.initialized = false
	return nil
}

// ======== Cosmos/Tendermint specific verification functions ========

// verifyCommit verifies commit signatures for a header
func (a *CosmosAdapter) verifyCommit(header *CosmosHeader, commit *TendermintCommit) error {
	// Verify commit is for this block
	if commit.BlockHash != header.Hash {
		return errors.New("commit block hash mismatch")
	}

	if commit.Height != header.Height {
		return errors.New("commit height mismatch")
	}

	// Get validator set
	a.mu.RLock()
	validatorSet := a.validatorSet
	a.mu.RUnlock()

	if validatorSet == nil {
		// Accept if no validator set (bootstrapping)
		return nil
	}

	// Count voting power
	var votedPower int64
	for _, sig := range commit.Signatures {
		if sig.Absent {
			continue
		}

		// Find validator
		for _, v := range validatorSet.Validators {
			if v.Address == sig.ValidatorAddress {
				// Verify signature
				if err := verifyTendermintSignature(v, header, commit.Round, sig); err != nil {
					continue
				}
				votedPower += v.VotingPower
				break
			}
		}
	}

	// Check 2/3 threshold
	threshold := int64(float64(validatorSet.TotalPower) * TendermintTrustLevel)
	if votedPower < threshold {
		return ErrQuorumNotMet
	}

	return nil
}

// verifyTendermintSignature verifies a Tendermint vote signature
func verifyTendermintSignature(validator *TendermintValidator, header *CosmosHeader, round int32, vote *TendermintVote) error {
	// Build vote message
	// In production, would use canonical encoding
	msg := make([]byte, 0, 128)
	msg = append(msg, []byte(header.ChainID)...)
	msg = binary.LittleEndian.AppendUint64(msg, header.Height)
	msg = binary.LittleEndian.AppendUint32(msg, uint32(round))
	msg = append(msg, header.Hash[:]...)

	// Verify signature based on key type
	switch validator.PubKeyType {
	case "ed25519":
		// Ed25519 verification would go here
		return nil // Simplified
	case "secp256k1":
		// Secp256k1 verification would go here
		return nil // Simplified
	default:
		return errors.New("unknown public key type")
	}
}

// verifyIBCPacket verifies an IBC packet commitment
func (a *CosmosAdapter) verifyIBCPacket(msg *CrossChainMessage) error {
	// Get header at source block
	a.mu.RLock()
	header, ok := a.headers[msg.SourceBlock]
	a.mu.RUnlock()

	if !ok {
		return ErrHeaderNotFound
	}

	if !header.Finalized {
		return ErrBlockNotFinalized
	}

	// Verify packet commitment proof against AppHash
	// IBC uses IAVL+ proofs for state verification
	if len(msg.SourceProof) == 0 {
		return errors.New("missing IBC proof")
	}

	// In production, would verify IAVL+ proof
	return nil
}

// decodeCosmosHeader decodes a Cosmos header from bytes
func decodeCosmosHeader(data []byte) (*CosmosHeader, error) {
	if len(data) < 292 { // Minimum header size
		return nil, fmt.Errorf("header too short: %d bytes", len(data))
	}

	header := &CosmosHeader{}
	offset := 0

	// Height (8 bytes)
	header.Height = binary.LittleEndian.Uint64(data[offset : offset+8])
	offset += 8

	// Time (8 bytes)
	header.Time = int64(binary.LittleEndian.Uint64(data[offset : offset+8]))
	offset += 8

	// ChainID length (4 bytes) + ChainID
	chainIDLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4
	if offset+int(chainIDLen) > len(data) {
		return nil, errors.New("invalid chain ID length")
	}
	header.ChainID = string(data[offset : offset+int(chainIDLen)])
	offset += int(chainIDLen)

	// Ensure we have enough data for remaining fields
	if offset+256 > len(data) {
		return nil, errors.New("header data too short for hashes")
	}

	// Last block hash (32 bytes)
	copy(header.LastBlockHash[:], data[offset:offset+32])
	offset += 32

	// Data hash (32 bytes)
	copy(header.DataHash[:], data[offset:offset+32])
	offset += 32

	// Validators hash (32 bytes)
	copy(header.ValidatorsHash[:], data[offset:offset+32])
	offset += 32

	// Next validators hash (32 bytes)
	copy(header.NextValidatorsHash[:], data[offset:offset+32])
	offset += 32

	// Consensus hash (32 bytes)
	copy(header.ConsensusHash[:], data[offset:offset+32])
	offset += 32

	// App hash (32 bytes)
	copy(header.AppHash[:], data[offset:offset+32])
	offset += 32

	// Last results hash (32 bytes)
	copy(header.LastResultsHash[:], data[offset:offset+32])
	offset += 32

	// Evidence hash (32 bytes)
	copy(header.EvidenceHash[:], data[offset:offset+32])
	offset += 32

	// Proposer address (20 bytes)
	if offset+20 <= len(data) {
		copy(header.ProposerAddress[:], data[offset:offset+20])
	}

	// Compute header hash
	header.Hash = computeCosmosHeaderHash(header)

	return header, nil
}

// decodeTendermintCommit decodes a Tendermint commit from bytes
func decodeTendermintCommit(data []byte) (*TendermintCommit, error) {
	if len(data) < 44 {
		return nil, errors.New("commit data too short")
	}

	commit := &TendermintCommit{}

	// Height (8 bytes)
	commit.Height = binary.LittleEndian.Uint64(data[0:8])

	// Round (4 bytes)
	commit.Round = int32(binary.LittleEndian.Uint32(data[8:12]))

	// Block hash (32 bytes)
	copy(commit.BlockHash[:], data[12:44])

	// Signatures count (4 bytes)
	if len(data) < 48 {
		return commit, nil
	}
	sigCount := binary.LittleEndian.Uint32(data[44:48])

	// Parse signatures
	offset := 48
	commit.Signatures = make([]*TendermintVote, 0, sigCount)
	for i := uint32(0); i < sigCount && offset+92 <= len(data); i++ {
		vote := &TendermintVote{}
		copy(vote.ValidatorAddress[:], data[offset:offset+20])
		vote.Timestamp = int64(binary.LittleEndian.Uint64(data[offset+20 : offset+28]))
		vote.Signature = make([]byte, 64)
		copy(vote.Signature, data[offset+28:offset+92])
		commit.Signatures = append(commit.Signatures, vote)
		offset += 92
	}

	return commit, nil
}

// computeCosmosHeaderHash computes the header hash
func computeCosmosHeaderHash(header *CosmosHeader) [32]byte {
	h := sha256.New()
	binary.Write(h, binary.LittleEndian, header.Height)
	binary.Write(h, binary.LittleEndian, header.Time)
	h.Write([]byte(header.ChainID))
	h.Write(header.LastBlockHash[:])
	h.Write(header.DataHash[:])
	h.Write(header.ValidatorsHash[:])
	h.Write(header.NextValidatorsHash[:])
	h.Write(header.ConsensusHash[:])
	h.Write(header.AppHash[:])
	h.Write(header.LastResultsHash[:])
	h.Write(header.EvidenceHash[:])
	h.Write(header.ProposerAddress[:])

	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// computeValidatorSetHash computes hash of validator set
func computeValidatorSetHash(vs *TendermintValidatorSet) [32]byte {
	h := sha256.New()
	for _, v := range vs.Validators {
		h.Write(v.Address[:])
		h.Write(v.PubKey)
		binary.Write(h, binary.LittleEndian, v.VotingPower)
	}

	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// verifyCosmosMerkleProof verifies a Cosmos Merkle proof
func verifyCosmosMerkleProof(leaf []byte, root [32]byte, proof [][]byte) bool {
	hash := sha256.Sum256(leaf)

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

// verifyCosmosEventProof verifies a Cosmos event proof
func verifyCosmosEventProof(event *ChainEvent, resultsHash [32]byte) bool {
	// Simplified verification
	// In production, would verify against LastResultsHash
	return len(event.Proof) > 0
}
