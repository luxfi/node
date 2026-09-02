// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"

	genesiscfg "github.com/luxfi/genesis/pkg/genesis"

	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// A genesis validator is seeded with the key every reader parses — the
// uncompressed encoding peer.shouldDisconnect checks a signed IP against — or
// it is not seeded at all. The two functions below are that decision, one per
// shape of genesis a node can be started with, and they answer it alike:
//
//	err != nil              ⇒ skip the validator
//	key == nil, err == nil  ⇒ the entry declares no key; seed it keyless
//
// Declaring no key is a shape the wire allows, and such an entry is seeded
// keyless because keyless is what it says it is. Declaring a key that nothing
// can check is a different thing. Seeding that one keeps its full genesis
// weight while peer.shouldDisconnect returns early on a keyless entry, so it
// would vote for the whole bootstrap window with nobody proving they hold the
// key.
//
// Both are pure so the decision can be checked directly, without a network, a
// config on disk, or a seam to inject one through — as is canonicalStakers,
// which applies the second of them to a whole config and hands back the
// refusals rather than swallowing them.

// genesisStaker is one validator the node seeds its manager with at boot.
type genesisStaker struct {
	NodeID ids.NodeID
	Weight uint64
	BLSKey []byte
}

// refusal names a staker the seeding decision would not seed, and why.
type refusal struct {
	NodeID ids.NodeID
	Err    error
}

// provenKey is the key a genesis TRANSACTION's signer proves possession of.
//
// tx.PublicKey() is the accessor that honours the Signer invariant "Key() only
// after Verify() returns nil". The signer here is a fresh decode of the tx
// buffer, so its parsed key is nil until this call verifies it; reading Key()
// directly registers a validator with full weight and no key.
func provenKey(tx *txs.AddPermissionlessValidatorTx) ([]byte, error) {
	pubKey, hasKey, err := tx.PublicKey()
	if err != nil {
		return nil, err
	}
	if !hasKey {
		return nil, nil
	}
	return bls.PublicKeyToUncompressedBytes(pubKey), nil
}

// declaredKey is the key a canonical CONFIG staker declares, once the staker
// has proved it holds it.
//
// The two fields are 0x-prefixed hex of the COMPRESSED key and of the
// possession proof over it — the same pair genesis/builder decodes with
// formatting.HexNC. Base64 does not fail cleanly on hex, every character being
// in its alphabet, so decoding it that way yielded 72 bytes of garbage plus an
// error that was discarded.
//
// builder.parseProofOfPossession VERIFIES that proof when it builds genesis
// FROM this config, and so does this function, so the field is read one way
// wherever it is read: the two accept the same stakers and refuse the same
// ones. It costs one pairing per staker, once, at boot.
func declaredKey(staker genesiscfg.Staker) ([]byte, error) {
	if staker.Signer == nil || staker.Signer.PublicKey == "" {
		return nil, nil
	}
	pkBytes, err := formatting.Decode(formatting.HexNC, staker.Signer.PublicKey)
	if err != nil {
		return nil, err
	}
	popBytes, err := formatting.Decode(formatting.HexNC, staker.Signer.ProofOfPossession)
	if err != nil {
		return nil, err
	}

	// signer.ProofOfPossession holds both as fixed arrays, so copying into one
	// pads a short field with zeros rather than refusing it. Length is part of
	// what makes a key a key, so it is checked before the copy can hide it.
	if len(pkBytes) != bls.PublicKeyLen {
		return nil, fmt.Errorf("declared key is %d bytes, want %d", len(pkBytes), bls.PublicKeyLen)
	}
	if len(popBytes) != bls.SignatureLen {
		return nil, fmt.Errorf("declared proof of possession is %d bytes, want %d", len(popBytes), bls.SignatureLen)
	}

	pop := &signer.ProofOfPossession{}
	copy(pop.PublicKey[:], pkBytes)
	copy(pop.ProofOfPossession[:], popBytes)

	// Verify parses the key and checks the pairing, and populates Key() only
	// when it returns nil — the same invariant provenKey reads tx.PublicKey()
	// through.
	if err := pop.Verify(); err != nil {
		return nil, err
	}
	return bls.PublicKeyToUncompressedBytes(pop.Key()), nil
}

// canonicalStakers is the canonical config's stakers as this node seeds them,
// together with the ones it refuses.
//
// builder.GetConfig reads the shipped configs and every one of them verifies,
// so the caller cannot be shown a bad staker through it — and there is no seam
// to inject one, by design. Mapping the slice here rather than looping at the
// call site is what makes the refusal checkable: the decision is a value, and a
// staker that is refused is absent from [seeded] rather than quietly present
// with no key.
func canonicalStakers(stakers []genesiscfg.Staker) (seeded []genesisStaker, refused []refusal) {
	for _, staker := range stakers {
		blsKey, err := declaredKey(staker)
		if err != nil {
			refused = append(refused, refusal{NodeID: staker.NodeID, Err: err})
			continue
		}
		weight := staker.Weight
		if weight == 0 {
			weight = 1
		}
		seeded = append(seeded, genesisStaker{
			NodeID: staker.NodeID,
			Weight: weight,
			BLSKey: blsKey,
		})
	}
	return seeded, refused
}

// emptyValidatorSetReason names why a node is starting with no validators.
//
// The declared count is the whole input, and it separates the two ways there: a
// node that declared stakers and has none left refused its own, while a node
// that declared none was handed none — by its genesis and by the canonical
// config it fell back to.
func emptyValidatorSetReason(declared int) string {
	if declared > 0 {
		return "every validator declared in genesis failed its key check; starting with an empty validator set"
	}
	return "neither this node's genesis nor the canonical config declares a validator; starting with an empty validator set"
}
