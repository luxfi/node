// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/formatting"

	genesiscfg "github.com/luxfi/genesis/pkg/genesis"

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
// config on disk, or a seam to inject one through.

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

// declaredKey is the key a canonical CONFIG staker declares.
//
// The field is a 0x-prefixed hex string of the COMPRESSED key — the same field
// genesis/builder decodes with formatting.HexNC. Base64 does not fail cleanly
// on hex, every character being in its alphabet, so decoding it that way
// yielded 72 bytes of garbage plus an error that was discarded.
//
// The staker declares a possession proof as well, and genesis/builder verifies
// that proof when it builds genesis FROM this config. Here the key is only
// parsed — which is the difference between a key and 72 bytes of garbage, and
// that is the difference this seeding path has to tell apart.
func declaredKey(staker genesiscfg.Staker) ([]byte, error) {
	if staker.Signer == nil || staker.Signer.PublicKey == "" {
		return nil, nil
	}
	pkBytes, err := formatting.Decode(formatting.HexNC, staker.Signer.PublicKey)
	if err != nil {
		return nil, err
	}
	pubKey, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	if err != nil {
		return nil, err
	}
	return bls.PublicKeyToUncompressedBytes(pubKey), nil
}
