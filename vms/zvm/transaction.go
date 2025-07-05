// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zvm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/luxfi/node/ids"
)

// TransactionType represents the type of transaction
type TransactionType uint8

const (
	TransactionTypeTransfer TransactionType = iota
	TransactionTypeMint
	TransactionTypeBurn
	TransactionTypeShield   // Convert transparent to shielded
	TransactionTypeUnshield // Convert shielded to transparent
)

// Transaction represents a confidential transaction
type Transaction struct {
	ID       ids.ID          `json:"id"`
	Type     TransactionType `json:"type"`
	Version  uint8           `json:"version"`
	
	// Transparent inputs/outputs (for shield/unshield)
	TransparentInputs  []*TransparentInput  `json:"transparentInputs,omitempty"`
	TransparentOutputs []*TransparentOutput `json:"transparentOutputs,omitempty"`
	
	// Shielded components
	Nullifiers [][]byte        `json:"nullifiers"`      // Spent note nullifiers
	Outputs    []*ShieldedOutput `json:"outputs"`        // New shielded outputs
	
	// Zero-knowledge proof
	Proof      *ZKProof        `json:"proof"`
	
	// FHE operations (optional)
	FHEData    *FHEData        `json:"fheData,omitempty"`
	
	// Transaction metadata
	Fee        uint64          `json:"fee"`
	Expiry     uint64          `json:"expiry"`          // Block height
	Memo       []byte          `json:"memo,omitempty"`  // Encrypted memo
	
	// Signature for transparent components
	Signature  []byte          `json:"signature,omitempty"`
}

// TransparentInput represents an unshielded input
type TransparentInput struct {
	TxID       ids.ID `json:"txId"`
	OutputIdx  uint32 `json:"outputIdx"`
	Amount     uint64 `json:"amount"`
	Address    []byte `json:"address"`
}

// TransparentOutput represents an unshielded output
type TransparentOutput struct {
	Amount     uint64 `json:"amount"`
	Address    []byte `json:"address"`
	AssetID    ids.ID `json:"assetId"`
}

// ShieldedOutput represents a confidential output
type ShieldedOutput struct {
	// Commitment to the note (amount and address)
	Commitment []byte `json:"commitment"`
	
	// Encrypted note ciphertext
	EncryptedNote []byte `json:"encryptedNote"`
	
	// Ephemeral public key for note encryption
	EphemeralPubKey []byte `json:"ephemeralPubKey"`
	
	// Output proof (rangeproof for amount)
	OutputProof []byte `json:"outputProof"`
}

// ZKProof represents a zero-knowledge proof
type ZKProof struct {
	ProofType  string `json:"proofType"`  // groth16, plonk, etc.
	ProofData  []byte `json:"proofData"`
	PublicInputs [][]byte `json:"publicInputs"`
	
	// Cached verification result
	verified   *bool
}

// FHEData represents fully homomorphic encryption data
type FHEData struct {
	// Encrypted computation inputs
	EncryptedInputs [][]byte `json:"encryptedInputs"`
	
	// Computation circuit
	CircuitID  string `json:"circuitId"`
	
	// Encrypted result
	EncryptedResult []byte `json:"encryptedResult"`
	
	// Proof of correct computation
	ComputationProof []byte `json:"computationProof"`
}

// Note represents a shielded note (internal representation)
type Note struct {
	Value      *big.Int `json:"value"`      // Encrypted amount
	Address    []byte   `json:"address"`    // Recipient address
	AssetID    ids.ID   `json:"assetId"`    // Asset type
	Randomness []byte   `json:"randomness"` // Note randomness
	Nullifier  []byte   `json:"nullifier"`  // Computed nullifier
}

// ComputeID computes the transaction ID
func (tx *Transaction) ComputeID() ids.ID {
	h := sha256.New()
	
	// Include transaction type and version
	h.Write([]byte{byte(tx.Type), tx.Version})
	
	// Include nullifiers
	for _, nullifier := range tx.Nullifiers {
		h.Write(nullifier)
	}
	
	// Include output commitments
	for _, output := range tx.Outputs {
		h.Write(output.Commitment)
	}
	
	// Include proof
	if tx.Proof != nil {
		h.Write([]byte(tx.Proof.ProofType))
		h.Write(tx.Proof.ProofData)
	}
	
	// Include fee and expiry
	binary.Write(h, binary.BigEndian, tx.Fee)
	binary.Write(h, binary.BigEndian, tx.Expiry)
	
	return ids.ID(h.Sum(nil))
}

// HasFHEOperations returns true if the transaction includes FHE operations
func (tx *Transaction) HasFHEOperations() bool {
	return tx.FHEData != nil && len(tx.FHEData.EncryptedInputs) > 0
}

// GetNullifiers returns all nullifiers in the transaction
func (tx *Transaction) GetNullifiers() [][]byte {
	return tx.Nullifiers
}

// GetOutputCommitments returns all output commitments
func (tx *Transaction) GetOutputCommitments() [][]byte {
	commitments := make([][]byte, len(tx.Outputs))
	for i, output := range tx.Outputs {
		commitments[i] = output.Commitment
	}
	return commitments
}

// ValidateBasic performs basic validation
func (tx *Transaction) ValidateBasic() error {
	// Check transaction type
	if tx.Type > TransactionTypeUnshield {
		return errInvalidTransactionType
	}
	
	// Check nullifiers and outputs
	if len(tx.Nullifiers) == 0 && len(tx.TransparentInputs) == 0 {
		return errNoInputs
	}
	
	if len(tx.Outputs) == 0 && len(tx.TransparentOutputs) == 0 {
		return errNoOutputs
	}
	
	// Check proof
	if tx.Proof == nil {
		return errMissingProof
	}
	
	// Type-specific validation
	switch tx.Type {
	case TransactionTypeTransfer:
		// Must have shielded inputs and outputs
		if len(tx.Nullifiers) == 0 || len(tx.Outputs) == 0 {
			return errInvalidTransferTransaction
		}
		
	case TransactionTypeShield:
		// Must have transparent inputs and shielded outputs
		if len(tx.TransparentInputs) == 0 || len(tx.Outputs) == 0 {
			return errInvalidShieldTransaction
		}
		
	case TransactionTypeUnshield:
		// Must have shielded inputs and transparent outputs
		if len(tx.Nullifiers) == 0 || len(tx.TransparentOutputs) == 0 {
			return errInvalidUnshieldTransaction
		}
	}
	
	return nil
}

// ComputeNullifier computes a nullifier for a note
func ComputeNullifier(note *Note, spendingKey []byte) []byte {
	h := sha256.New()
	h.Write(note.Address)
	h.Write(note.Value.Bytes())
	h.Write(note.AssetID[:])
	h.Write(note.Randomness)
	h.Write(spendingKey)
	return h.Sum(nil)
}

// ComputeCommitment computes a note commitment
func ComputeCommitment(note *Note) []byte {
	h := sha256.New()
	h.Write(note.Value.Bytes())
	h.Write(note.Address)
	h.Write(note.AssetID[:])
	h.Write(note.Randomness)
	return h.Sum(nil)
}

// EncryptNote encrypts a note for the recipient
func EncryptNote(note *Note, recipientPubKey []byte, ephemeralPrivKey []byte) ([]byte, []byte, error) {
	// In production, use proper encryption (e.g., ChaCha20-Poly1305)
	// This is a placeholder
	encryptedNote := append(note.Value.Bytes(), note.Address...)
	encryptedNote = append(encryptedNote, note.AssetID[:]...)
	encryptedNote = append(encryptedNote, note.Randomness...)
	
	// Derive ephemeral public key
	ephemeralPubKey := derivePublicKey(ephemeralPrivKey)
	
	return encryptedNote, ephemeralPubKey, nil
}

// DecryptNote decrypts a note using the recipient's key
func DecryptNote(encryptedNote []byte, ephemeralPubKey []byte, recipientPrivKey []byte) (*Note, error) {
	// In production, use proper decryption
	// This is a placeholder
	
	// Extract components (assuming fixed sizes)
	valueBytes := encryptedNote[:32]
	address := encryptedNote[32:64]
	assetID := encryptedNote[64:96]
	randomness := encryptedNote[96:128]
	
	value := new(big.Int).SetBytes(valueBytes)
	
	return &Note{
		Value:      value,
		Address:    address,
		AssetID:    ids.ID(assetID),
		Randomness: randomness,
	}, nil
}

// derivePublicKey derives a public key from private key
func derivePublicKey(privKey []byte) []byte {
	// Placeholder - use proper key derivation
	h := sha256.Sum256(privKey)
	return h[:]
}

// Transaction validation errors
var (
	errInvalidTransactionType     = errors.New("invalid transaction type")
	errNoInputs                   = errors.New("transaction has no inputs")
	errNoOutputs                  = errors.New("transaction has no outputs")
	errMissingProof              = errors.New("transaction missing proof")
	errInvalidTransferTransaction = errors.New("invalid transfer transaction")
	errInvalidShieldTransaction   = errors.New("invalid shield transaction")
	errInvalidUnshieldTransaction = errors.New("invalid unshield transaction")
)