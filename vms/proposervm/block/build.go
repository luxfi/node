// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"crypto/rand"
	"time"

	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/staking"
)

func BuildUnsigned(
	parentID ids.ID,
	timestamp time.Time,
	pChainHeight uint64,
	epoch Epoch,
	blockBytes []byte,
) (SignedBlock, error) {
	// No certificate, no signature: the block bytes are just the unsigned body.
	unsignedBytes := buildUnsignedBuffer(parentID, timestamp.Unix(), pChainHeight, epoch, nil, blockBytes)

	block := &statelessBlock{}
	if err := block.initialize(unsignedBytes); err != nil {
		return nil, err
	}
	return block, nil
}

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
	unsignedBytes := buildUnsignedBuffer(parentID, timestamp.Unix(), pChainHeight, epoch, cert.Raw, blockBytes)

	// The block ID is the hash of the unsigned prefix; the proposer signs a
	// header binding (chainID, parentID, blockID).
	id := hash.ComputeHash256Array(unsignedBytes)

	header, err := BuildHeader(chainID, parentID, id)
	if err != nil {
		return nil, err
	}

	headerHash := hash.ComputeHash256(header.Bytes())
	sig, err := key.Sign(rand.Reader, headerHash, crypto.SHA256)
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
