// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"crypto/rand"
	"time"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/staking"
)

// proposerSigCtx domain-separates proposervm block signatures (FIPS 204 §5.2)
// from every other ML-DSA signature a validator makes (UTXO auth, consensus
// votes), so a proposer signature can never be replayed as another protocol's
// message. The classical (ECDSA) path gets its cross-chain separation from the
// chainID bound into the signed header.
var proposerSigCtx = []byte("lux-proposervm-block-v1")

func BuildUnsigned(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	epoch Epoch,
	blockBytes []byte,
) (SignedBlock, error) {
	// No identity, no signature: the block bytes are just the unsigned body. This
	// is the pre-fork→post-fork transition block and every K=1 block (no proposer
	// window), so small local nets never exercise a signed path.
	unsignedBytes := buildUnsignedBuffer(parentID, timestamp.Unix(), pChainHeight, epoch, nil, blockBytes)

	block := &statelessBlock{}
	if err := block.initialize(unsignedBytes); err != nil {
		return nil, err
	}
	return block, nil
}

// Build produces a CLASSICAL (secp256k1/ECDSA TLS-leaf) signed block: identity
// slot = [schemeSecp256k1 | cert DER], signature = ECDSA over SHA256(header).
// Used on chains whose validator set is keyed by classical NodeIDs.
func Build(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	epoch Epoch,
	cert *staking.Certificate,
	blockBytes []byte,
	chainID ids.ID,
	key crypto.Signer,
) (SignedBlock, error) {
	return buildSigned(parentID, timestamp, pChainHeight, epoch,
		schemeSecp256k1, cert.Raw, blockBytes, chainID,
		func(headerBytes []byte) ([]byte, error) {
			// ECDSA signs the digest; CheckSignature re-hashes the header to match.
			return key.Sign(rand.Reader, hash.ComputeHash256(headerBytes), crypto.SHA256)
		})
}

// BuildMLDSA produces a strict-PQ (ML-DSA-65, FIPS 204) signed block: identity
// slot = [schemeMLDSA65 | raw ML-DSA-65 pubkey], signature = ML-DSA over the raw
// header with the proposervm domain-separation context. The block's Proposer()
// NodeID derives via DeriveMLDSA, so it EQUALS the strict-PQ NodeID the windower
// elected — the fix for the height≥2 "unexpected proposer" verify failure.
func BuildMLDSA(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	epoch Epoch,
	mldsaPub []byte,
	blockBytes []byte,
	chainID ids.ID,
	key *mldsa.PrivateKey,
) (SignedBlock, error) {
	return buildSigned(parentID, timestamp, pChainHeight, epoch,
		schemeMLDSA65, mldsaPub, blockBytes, chainID,
		func(headerBytes []byte) ([]byte, error) {
			return key.SignCtx(rand.Reader, headerBytes, proposerSigCtx)
		})
}

// buildSigned is the one signed-block core: it composes the [scheme|identity]
// proposer slot, encodes the unsigned body, derives the block ID, builds the
// (chainID, parentID, blockID) header, calls the scheme's signer over it, and
// appends the signature buffer. Build / BuildMLDSA differ ONLY in the scheme tag,
// the identity bytes, and the sign closure — DRY across primitives.
func buildSigned(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	epoch Epoch,
	scheme byte,
	identity []byte,
	blockBytes []byte,
	chainID ids.ID,
	sign func(headerBytes []byte) ([]byte, error),
) (SignedBlock, error) {
	idSlot := make([]byte, 0, 1+len(identity))
	idSlot = append(idSlot, scheme)
	idSlot = append(idSlot, identity...)

	unsignedBytes := buildUnsignedBuffer(parentID, timestamp.Unix(), pChainHeight, epoch, idSlot, blockBytes)

	// The block ID is the hash of the unsigned prefix; the proposer signs a
	// header binding (chainID, parentID, blockID).
	id := hash.ComputeHash256Array(unsignedBytes)

	header, err := BuildHeader(chainID, parentID, id)
	if err != nil {
		return nil, err
	}

	sig, err := sign(header.Bytes())
	if err != nil {
		return nil, err
	}

	signedBytes := concat(unsignedBytes, buildSigBuffer(sig))

	block := &statelessBlock{}
	if err := block.initialize(signedBytes); err != nil {
		return nil, err
	}
	return block, nil
}

func BuildHeader(
	chainID ids.ID,
	parentID ids.ID,
	bodyID ids.ID,
) (Header, error) {
	return &statelessHeader{
		Chain:  chainID,
		Parent: parentID,
		Body:   bodyID,
		bytes:  buildHeaderBuffer(chainID, parentID, bodyID),
	}, nil
}

// BuildOption the option block
// [parentID] is the ID of this option's wrapper parent block
// [innerBytes] is the byte representation of a child option block
func BuildOption(
	parentID ids.ID,
	innerBytes []byte,
) (Block, error) {
	block := &option{}
	if err := block.initialize(buildOptionBuffer(parentID, innerBytes)); err != nil {
		return nil, err
	}
	return block, nil
}
