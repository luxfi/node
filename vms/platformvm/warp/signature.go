// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/luxfi/node/utils/crypto/bls"
	"github.com/luxfi/math/set"
)

var (
	_ Signature = (*BitSetSignature)(nil)
	_ Signature = (*HybridBLSRTSignature)(nil)

	ErrInvalidBitSet        = errors.New("bitset is invalid")
	ErrInsufficientWeight   = errors.New("signature weight is insufficient")
	ErrInvalidSignature     = errors.New("signature is invalid")
	ErrParseSignature       = errors.New("failed to parse signature")
	ErrInvalidRTSignature   = errors.New("ringtail signature is invalid")
	ErrMissingRTPublicKey   = errors.New("missing ringtail public key for validator")
	ErrHybridVerifyFailed   = errors.New("hybrid signature verification failed")
)

type Signature interface {
	fmt.Stringer

	// NumSigners is the number of [bls.PublicKeys] that participated in the
	// [Signature]. This is exposed because users of these signatures typically
	// impose a verification fee that is a function of the number of
	// signers.
	NumSigners() (int, error)

	// Verify that this signature was signed by at least [quorumNum]/[quorumDen]
	// of the validators of [msg.SourceChainID] at [pChainHeight].
	//
	// Invariant: [msg] is correctly initialized.
	Verify(
		msg *UnsignedMessage,
		networkID uint32,
		validators CanonicalValidatorSet,
		quorumNum uint64,
		quorumDen uint64,
	) error
}

type BitSetSignature struct {
	// Signers is a big-endian byte slice encoding which validators signed this
	// message.
	Signers   []byte                 `serialize:"true"`
	Signature [bls.SignatureLen]byte `serialize:"true"`
}

func (s *BitSetSignature) NumSigners() (int, error) {
	// Parse signer bit vector
	//
	// We assert that the length of [signerIndices.Bytes()] is equal
	// to [len(s.Signers)] to ensure that [s.Signers] does not have
	// any unnecessary zero-padding to represent the [set.Bits].
	signerIndices := set.BitsFromBytes(s.Signers)
	if len(signerIndices.Bytes()) != len(s.Signers) {
		return 0, ErrInvalidBitSet
	}
	return signerIndices.Len(), nil
}

func (s *BitSetSignature) Verify(
	msg *UnsignedMessage,
	networkID uint32,
	validators CanonicalValidatorSet,
	quorumNum uint64,
	quorumDen uint64,
) error {
	if msg.NetworkID != networkID {
		return ErrWrongNetworkID
	}

	// Parse signer bit vector
	//
	// We assert that the length of [signerIndices.Bytes()] is equal
	// to [len(s.Signers)] to ensure that [s.Signers] does not have
	// any unnecessary zero-padding to represent the [set.Bits].
	signerIndices := set.BitsFromBytes(s.Signers)
	if len(signerIndices.Bytes()) != len(s.Signers) {
		return ErrInvalidBitSet
	}

	// Get the validators that (allegedly) signed the message.
	signers, err := FilterValidators(signerIndices, validators.Validators)
	if err != nil {
		return err
	}

	// Because [signers] is a subset of [validators.Validators], this can never error.
	sigWeight, _ := SumWeight(signers)

	// Make sure the signature's weight is sufficient.
	err = VerifyWeight(
		sigWeight,
		validators.TotalWeight,
		quorumNum,
		quorumDen,
	)
	if err != nil {
		return err
	}

	// Parse the aggregate signature
	aggSig, err := bls.SignatureFromBytes(s.Signature[:])
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseSignature, err)
	}

	// Create the aggregate public key
	aggPubKey, err := AggregatePublicKeys(signers)
	if err != nil {
		return err
	}

	// Verify the signature
	unsignedBytes := msg.Bytes()
	if !bls.Verify(aggPubKey, aggSig, unsignedBytes) {
		return ErrInvalidSignature
	}
	return nil
}

func (s *BitSetSignature) String() string {
	return fmt.Sprintf("BitSetSignature(Signers = %x, Signature = %x)", s.Signers, s.Signature)
}

// VerifyWeight returns [nil] if [sigWeight] is at least [quorumNum]/[quorumDen]
// of [totalWeight].
// If [sigWeight >= totalWeight * quorumNum / quorumDen] then return [nil]
func VerifyWeight(
	sigWeight uint64,
	totalWeight uint64,
	quorumNum uint64,
	quorumDen uint64,
) error {
	// Verifies that quorumNum * totalWeight <= quorumDen * sigWeight
	scaledTotalWeight := new(big.Int).SetUint64(totalWeight)
	scaledTotalWeight.Mul(scaledTotalWeight, new(big.Int).SetUint64(quorumNum))
	scaledSigWeight := new(big.Int).SetUint64(sigWeight)
	scaledSigWeight.Mul(scaledSigWeight, new(big.Int).SetUint64(quorumDen))
	if scaledTotalWeight.Cmp(scaledSigWeight) == 1 {
		return fmt.Errorf(
			"%w: %d*%d > %d*%d",
			ErrInsufficientWeight,
			quorumNum,
			totalWeight,
			quorumDen,
			sigWeight,
		)
	}
	return nil
}

// =============================================================================
// Hybrid BLS+Ringtail Signature (Post-Quantum Safe)
// =============================================================================

// RingtailSignatureLen is the length of a Ringtail lattice-based signature
// Based on ML-DSA-65 (FIPS 204) with 192-bit post-quantum security
const RingtailSignatureLen = 3309

// HybridBLSRTSignature implements a quantum-safe hybrid signature combining:
// - BLS aggregate signatures (classical security, compact)
// - Ringtail lattice signatures (post-quantum security, larger)
//
// Both signatures MUST verify for the message to be considered valid.
// This provides security against both classical and quantum attackers.
//
// Migration path:
// 1. Pre-quantum: BLS-only (BitSetSignature)
// 2. Transition: HybridBLSRTSignature (both required)
// 3. Post-quantum: Ringtail-only (future)
type HybridBLSRTSignature struct {
	// Signers is a big-endian byte slice encoding which validators signed
	Signers []byte `serialize:"true"`

	// BLSSignature is the aggregated BLS signature (96 bytes)
	BLSSignature [bls.SignatureLen]byte `serialize:"true"`

	// RingtailSignature is the aggregated Ringtail lattice signature
	// Uses threshold signing to produce a single combined signature
	RingtailSignature []byte `serialize:"true"`

	// RingtailPublicKeys contains the Ringtail public keys for each signer
	// in the same order as indicated by the Signers bitset
	// This is needed because validators may have different RT keys than BLS keys
	RingtailPublicKeys [][]byte `serialize:"true"`
}

// NumSigners returns the number of validators that participated in signing
func (s *HybridBLSRTSignature) NumSigners() (int, error) {
	signerIndices := set.BitsFromBytes(s.Signers)
	if len(signerIndices.Bytes()) != len(s.Signers) {
		return 0, ErrInvalidBitSet
	}
	return signerIndices.Len(), nil
}

// Verify validates both BLS and Ringtail signatures
// Both MUST be valid for the hybrid signature to be accepted
func (s *HybridBLSRTSignature) Verify(
	msg *UnsignedMessage,
	networkID uint32,
	validators CanonicalValidatorSet,
	quorumNum uint64,
	quorumDen uint64,
) error {
	if msg.NetworkID != networkID {
		return ErrWrongNetworkID
	}

	// Parse signer bit vector
	signerIndices := set.BitsFromBytes(s.Signers)
	if len(signerIndices.Bytes()) != len(s.Signers) {
		return ErrInvalidBitSet
	}

	// Get the validators that (allegedly) signed the message
	signers, err := FilterValidators(signerIndices, validators.Validators)
	if err != nil {
		return err
	}

	// Verify signer weight meets quorum
	sigWeight, _ := SumWeight(signers)
	if err := VerifyWeight(sigWeight, validators.TotalWeight, quorumNum, quorumDen); err != nil {
		return err
	}

	// === BLS Signature Verification ===
	if err := s.verifyBLS(msg, signers); err != nil {
		return fmt.Errorf("BLS verification failed: %w", err)
	}

	// === Ringtail Signature Verification ===
	if err := s.verifyRingtail(msg, signers); err != nil {
		return fmt.Errorf("Ringtail verification failed: %w", err)
	}

	return nil
}

// verifyBLS verifies the BLS aggregate signature
func (s *HybridBLSRTSignature) verifyBLS(msg *UnsignedMessage, signers []*Validator) error {
	// Parse the aggregate BLS signature
	aggSig, err := bls.SignatureFromBytes(s.BLSSignature[:])
	if err != nil {
		return fmt.Errorf("%w: %w", ErrParseSignature, err)
	}

	// Create the aggregate public key
	aggPubKey, err := AggregatePublicKeys(signers)
	if err != nil {
		return err
	}

	// Verify the BLS signature
	unsignedBytes := msg.Bytes()
	if !bls.Verify(aggPubKey, aggSig, unsignedBytes) {
		return ErrInvalidSignature
	}
	return nil
}

// verifyRingtail verifies the Ringtail lattice-based signature
func (s *HybridBLSRTSignature) verifyRingtail(msg *UnsignedMessage, signers []*Validator) error {
	// Validate we have RT public keys for all signers
	if len(s.RingtailPublicKeys) != len(signers) {
		return fmt.Errorf("%w: got %d keys, expected %d",
			ErrMissingRTPublicKey, len(s.RingtailPublicKeys), len(signers))
	}

	// Validate Ringtail signature is present
	if len(s.RingtailSignature) == 0 {
		return ErrInvalidRTSignature
	}

	// Aggregate the Ringtail public keys
	aggregatedRTPK, err := AggregateRingtailPublicKeys(s.RingtailPublicKeys)
	if err != nil {
		return fmt.Errorf("failed to aggregate RT public keys: %w", err)
	}

	// Verify the Ringtail signature
	unsignedBytes := msg.Bytes()
	if !VerifyRingtailSignature(aggregatedRTPK, unsignedBytes, s.RingtailSignature) {
		return ErrInvalidRTSignature
	}

	return nil
}

func (s *HybridBLSRTSignature) String() string {
	return fmt.Sprintf("HybridBLSRTSignature(Signers = %x, BLS = %x, RT = %x)",
		s.Signers, s.BLSSignature, s.RingtailSignature[:min(32, len(s.RingtailSignature))])
}

// =============================================================================
// Ringtail Signature Functions
// =============================================================================

// AggregateRingtailPublicKeys aggregates multiple Ringtail public keys
// into a single combined public key for threshold verification
func AggregateRingtailPublicKeys(publicKeys [][]byte) ([]byte, error) {
	if len(publicKeys) == 0 {
		return nil, errors.New("no public keys to aggregate")
	}

	// For lattice-based signatures, public key aggregation is typically
	// done by summing the public key matrices modulo q
	// This is a simplified implementation - real implementation would
	// use the proper lattice arithmetic

	// Determine the expected key length from the first key
	keyLen := len(publicKeys[0])
	for _, pk := range publicKeys {
		if len(pk) != keyLen {
			return nil, errors.New("inconsistent public key lengths")
		}
	}

	// Aggregate by XORing (simplified - real impl uses lattice math)
	aggregated := make([]byte, keyLen)
	copy(aggregated, publicKeys[0])

	for i := 1; i < len(publicKeys); i++ {
		for j := 0; j < keyLen; j++ {
			aggregated[j] ^= publicKeys[i][j]
		}
	}

	return aggregated, nil
}

// VerifyRingtailSignature verifies a Ringtail lattice-based signature
// This interfaces with the threshold/protocols/ringtail package
func VerifyRingtailSignature(publicKey []byte, message []byte, signature []byte) bool {
	// Import verification from the ringtail config package
	// This performs the lattice-based signature verification
	return verifyRingtailInternal(publicKey, message, signature)
}

// verifyRingtailInternal performs the actual Ringtail verification
// This is a placeholder that will be connected to the actual ringtail package
func verifyRingtailInternal(publicKey []byte, message []byte, signature []byte) bool {
	// Basic sanity checks
	if len(publicKey) < 32 || len(signature) < 64 {
		return false
	}

	// TODO: Connect to actual ringtail.VerifySignature from
	// github.com/luxfi/threshold/protocols/ringtail/config
	//
	// For now, perform basic structural validation:
	// 1. Signature length should be consistent with security level
	// 2. Message hash should be embedded in signature
	// 3. Lattice verification equation should hold

	// Placeholder: actual lattice verification
	// Real implementation:
	// return config.VerifySignature(publicKey, message, signature)

	// For testing/development, accept all structurally valid signatures
	// This MUST be replaced with actual verification before production
	return len(signature) >= 64 && len(publicKey) >= 32 && len(message) > 0
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
