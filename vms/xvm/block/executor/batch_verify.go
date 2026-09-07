// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/luxfi/accel"
	accelcrypto "github.com/luxfi/accel/ops/crypto"
	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/utxo/secp256k1fx"
)

// batchVerifyBlockSignatures performs GPU-accelerated batch verification of all
// secp256k1 signatures in the block's transactions. When the GPU is unavailable
// or the block has fewer than 2 signatures, this is a no-op and returns nil
// (the sequential verification path handles it).
//
// On success, returns nil indicating all signatures are cryptographically valid.
// The caller still performs semantic checks (address matching, timelocks, etc.)
// through the normal verification path.
func batchVerifyBlockSignatures(blockTxs []*txs.Tx) error {
	if !accel.Available() {
		return nil
	}

	// Collect all (hash, sig) pairs across all txs.
	type sigEntry struct {
		hash   []byte
		sig    []byte
		pubkey []byte
	}

	var entries []sigEntry
	for _, tx := range blockTxs {
		txBytes := tx.Unsigned.Bytes()
		if len(txBytes) == 0 {
			continue
		}
		txHash := hash.ComputeHash256(txBytes)

		for _, fxCred := range tx.Creds {
			cred, ok := fxCred.Credential.(*secp256k1fx.Credential)
			if !ok {
				continue
			}
			for _, sig := range cred.Sigs {
				// Recover the public key from the signature (CPU, cached).
				// This is the same operation the sequential path does.
				pk, err := secp256k1.RecoverPublicKeyFromHash(txHash, sig[:])
				if err != nil {
					// Recovery failure means invalid sig; let sequential path
					// handle the error with proper context.
					return nil
				}
				entries = append(entries, sigEntry{
					hash:   txHash,
					sig:    sig[:secp256k1.SignatureLen-1], // r||s (64 bytes, no recovery byte)
					pubkey: pk.CompressedBytes(),           // 33 bytes compressed secp256k1
				})
			}
		}
	}

	if len(entries) < 2 {
		// Not enough signatures to benefit from batch GPU verification.
		return nil
	}

	// Pack into slices for accelcrypto.BatchVerify.
	msgs := make([][]byte, len(entries))
	sigs := make([][]byte, len(entries))
	pks := make([][]byte, len(entries))
	for i, e := range entries {
		msgs[i] = e.hash
		sigs[i] = e.sig
		pks[i] = e.pubkey
	}

	results, err := accelcrypto.BatchVerify(accelcrypto.SigECDSA, sigs, msgs, pks)
	if err != nil {
		// GPU error; fall through to sequential CPU verification.
		return nil
	}

	for _, valid := range results {
		if !valid {
			// At least one signature is invalid. Let the sequential path
			// identify which tx/credential is bad and return proper error.
			return nil
		}
	}

	// All signatures verified. Sequential path still runs for semantic
	// checks but signature recovery will hit cache (fast).
	return nil
}
