// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package mldsafx is the node-side bridge for the ML-DSA (FIPS 204)
// feature extension. It re-exports the canonical ML-DSA types from
// github.com/luxfi/utxo/mldsafx so the platformvm and xvm package
// trees see a single import path:
//
//	github.com/luxfi/node/vms/mldsafx
//
// One feature extension. One import path. The underlying types live in
// the cross-chain utxo package because both X-chain UTXO ownership and
// P-chain validator/staker authorisation share the same wire shape.
//
// The post-quantum signature primitive is ML-DSA-65 (NIST PQ Cat 3,
// FIPS 204). Wire-compatible with the existing tx-type machinery: an
// MLDSACredential carries N signatures matching N owners exactly the
// same way secp256k1fx.Credential does — the difference is signature
// size (~3309 bytes per sig vs 65 bytes for secp256k1+v).
//
// This package is OPT-IN. secp256k1fx remains the classical-compat
// default. Under a strict-PQ ChainSecurityProfile (RequireTypedTxAuth
// == true), mempool admission refuses secp256k1fx credentials unless
// the originator is in the ClassicalCompatRegistry.
package mldsafx

import (
	utxomldsafx "github.com/luxfi/utxo/mldsafx"
)

// ID is the wire identifier for the mldsafx feature extension.
// Re-exported so callers depend on a single import path.
var ID = utxomldsafx.ID

// Name is the human-readable extension name.
const Name = utxomldsafx.Name

// Security level constants — re-exported from the cross-chain package
// so platformvm / xvm callers don't need a separate import.
const (
	MLDSA44PubKeyLen = utxomldsafx.MLDSA44PubKeyLen
	MLDSA44SigLen    = utxomldsafx.MLDSA44SigLen

	MLDSA65PubKeyLen = utxomldsafx.MLDSA65PubKeyLen
	MLDSA65SigLen    = utxomldsafx.MLDSA65SigLen

	MLDSA87PubKeyLen = utxomldsafx.MLDSA87PubKeyLen
	MLDSA87SigLen    = utxomldsafx.MLDSA87SigLen
)

// SecurityLevel is the ML-DSA parameter set indicator.
type SecurityLevel = utxomldsafx.SecurityLevel

// Security level enum values.
const (
	SecLevelMLDSA44 = utxomldsafx.SecLevelMLDSA44
	SecLevelMLDSA65 = utxomldsafx.SecLevelMLDSA65
	SecLevelMLDSA87 = utxomldsafx.SecLevelMLDSA87
)

// Credential carries N ML-DSA signatures for spending an output that
// names N owners.
type Credential = utxomldsafx.Credential

// OutputOwners is the ML-DSA equivalent of secp256k1fx.OutputOwners.
// Addrs are full ML-DSA public keys (1952 bytes for ML-DSA-65), not
// 20-byte hash-addresses, because PQ verification needs the full key.
type OutputOwners = utxomldsafx.OutputOwners

// TransferOutput is a PQ-owned token output.
type TransferOutput = utxomldsafx.TransferOutput

// TransferInput references a PQ-owned output by signature index.
type TransferInput = utxomldsafx.TransferInput

// MintOutput represents PQ-controlled mint authority.
type MintOutput = utxomldsafx.MintOutput

// MintOperation is the operation that consumes a MintOutput.
type MintOperation = utxomldsafx.MintOperation

// Input is the canonical input shape (signature indices) used by
// TransferInput and MintOperation.
type Input = utxomldsafx.Input

// Fx is the ML-DSA feature extension. It implements both the X-chain
// Fx interface (VerifyTransfer + VerifyOperation) and the platformvm
// Fx interface (VerifyTransfer + VerifyPermission + CreateOutput).
type Fx = utxomldsafx.Fx

// VM is the minimal VM contract the Fx depends on.
type VM = utxomldsafx.VM

// Factory produces Fx instances for codec/feature-extension wiring.
type Factory = utxomldsafx.Factory

// NewCredential constructs a credential and verifies its shape.
func NewCredential(level SecurityLevel, sigs [][]byte) (*Credential, error) {
	return utxomldsafx.NewCredential(level, sigs)
}

// NewCredential65 constructs an ML-DSA-65 credential.
func NewCredential65(sigs [][]byte) (*Credential, error) {
	return utxomldsafx.NewCredential65(sigs)
}

// NewOutputOwners constructs and verifies an output owners block.
func NewOutputOwners(level SecurityLevel, locktime uint64, threshold uint32, addrs [][]byte) (*OutputOwners, error) {
	return utxomldsafx.NewOutputOwners(level, locktime, threshold, addrs)
}
