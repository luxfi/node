// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package lux owns the luxd-side ZAP wire dispatcher for the cross-fx
// UTXO factory. The root utxo package cannot import the per-fx wire
// adapters (cycle), so each consumer registers an fx-aware
// utxo.ParseUTXOFunc at boot via utxo.RegisterParseUTXO.
//
// This file is the luxd consumer's registration site: an init() that
// wires in the full secp256k1fx / mldsafx / slhdsafx / ed25519fx /
// secp256r1fx / schnorrfx / bls12381fx output set. The dispatcher
// runs in O(1) on (TypeKind, ShapeKind) and stays in its lane —
// each fx package's WrapXxxOutput is the only thing that knows the
// fx-specific shape.
package lux

import (
	"fmt"

	utxo "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/bls12381fx"
	"github.com/luxfi/utxo/ed25519fx"
	"github.com/luxfi/utxo/mldsafx"
	"github.com/luxfi/utxo/schnorrfx"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/utxo/secp256r1fx"
	"github.com/luxfi/utxo/slhdsafx"
	"github.com/luxfi/utxo/wire"
	"github.com/luxfi/vm/components/verify"
)

func init() {
	utxo.RegisterParseUTXO(parseUTXO)
}

// parseUTXO reconstructs a *utxo.UTXO from its ZAP wire envelope. The
// outer envelope is decoded via wire.WrapUTXO; the inner Output is
// dispatched to the appropriate fx-package WrapXxxOutput on the
// (TypeKind, ShapeKind) discriminator.
//
// Failure surface:
//   - wire.WrapUTXO error: outer envelope malformed.
//   - unknown (TypeKind, ShapeKind) pair: caller stuffed a third-party
//     output into a ZAP UTXO. Refused at the boundary.
//   - per-fx WrapXxxOutput error: inner envelope malformed (length,
//     discriminator, or sub-field bounds).
func parseUTXO(wireBytes []byte) (*utxo.UTXO, error) {
	w, err := wire.WrapUTXO(wireBytes)
	if err != nil {
		return nil, fmt.Errorf("wrap utxo envelope: %w", err)
	}

	tk, sk := w.OutputDiscriminator()
	out, err := wrapOutput(w.OutputBytes(), tk, sk)
	if err != nil {
		return nil, err
	}

	return &utxo.UTXO{
		UTXOID: utxo.UTXOID{
			TxID:        w.TxID(),
			OutputIndex: w.OutputIndex(),
		},
		Asset: utxo.Asset{ID: w.AssetID()},
		Out:   out,
	}, nil
}

// LockedOutputHandler reconstructs the cross-fx LockedOutput composite
// (stakeable.LockOut on the platformvm side) from its wire envelope.
// stakeable.LockOut embeds lux.TransferableOut so this package cannot
// import it without a cycle — instead the platformvm/stakeable package
// registers a handler in its init() that knows how to:
//
//  1. wire.WrapLockedOutput(b) to surface (Locktime, TransferOutBytes)
//  2. WrapOutputBytes(TransferOutBytes) to recursively dispatch the
//     inner output through the same fx-aware dispatcher
//  3. construct a *stakeable.LockOut{Locktime, TransferableOut: inner}
//
// Without a registration, locked-output UTXOs (the canonical shape for
// vesting allocations in mainnet/testnet genesis) decode as
// "unknown (TypeKind=0x00, ShapeKind=0x0F)" and silently disappear
// from address balances — observed live as `platform.getBalance = 0`
// for every genesis-funded P-chain address.
type LockedOutputHandler func([]byte) (verify.State, error)

var lockedOutputHandler LockedOutputHandler

// RegisterLockedOutputHandler installs the locked-output wire handler.
// Called from platformvm/stakeable.init(). Panics on double-install so a
// duplicate registration during test fixture setup is loud, not silent.
func RegisterLockedOutputHandler(h LockedOutputHandler) {
	if lockedOutputHandler != nil {
		panic("lux: LockedOutputHandler already registered")
	}
	lockedOutputHandler = h
}

// WrapOutputBytes is the public entry to the fx-aware output dispatcher
// for composite shapes that recurse on a child envelope. Reads the
// envelope's (TypeKind, ShapeKind) prefix and routes through wrapOutput.
//
// The LockedOutput handler uses this on its inner TransferOutBytes —
// since the lock is fx-agnostic (TypeKind=Reserved) the inner envelope
// carries the actual fx discriminator.
func WrapOutputBytes(b []byte) (verify.State, error) {
	tk, sk, err := wire.PeekDiscriminator(b)
	if err != nil {
		return nil, fmt.Errorf("peek inner discriminator: %w", err)
	}
	return wrapOutput(b, tk, sk)
}

// wrapOutput dispatches the inner Output envelope on (TypeKind,
// ShapeKind). Each branch calls exactly one fx-package WrapXxxOutput.
// Adding a new (fx, shape) is a new branch — no shared codec to grow.
func wrapOutput(b []byte, tk wire.TypeKind, sk wire.ShapeKind) (verify.State, error) {
	// Composite shapes (fx-agnostic). The lock wrapper carries
	// TypeKind=Reserved because the inner envelope encodes the actual fx
	// family. Dispatch through the registered handler so the
	// vms/platformvm/stakeable package owns the *LockOut reconstruction
	// without this package having to import it (cycle).
	if tk == wire.TypeKindReserved && sk == wire.ShapeKindLockedOutput {
		if lockedOutputHandler == nil {
			return nil, fmt.Errorf("zap utxo dispatch: ShapeKindLockedOutput envelope but no LockedOutputHandler registered (platformvm/stakeable init() did not run)")
		}
		return lockedOutputHandler(b)
	}
	switch tk {
	case wire.TypeKindSecp256k1:
		switch sk {
		case wire.ShapeKindTransferOutput:
			return secp256k1fx.WrapTransferOutput(b)
		case wire.ShapeKindMintOutput:
			return secp256k1fx.WrapMintOutput(b)
		}
	case wire.TypeKindMLDSA:
		switch sk {
		case wire.ShapeKindTransferOutput:
			return mldsafx.WrapTransferOutput(b)
		case wire.ShapeKindMintOutput:
			return mldsafx.WrapMintOutput(b)
		}
	case wire.TypeKindSLHDSA:
		switch sk {
		case wire.ShapeKindTransferOutput:
			return slhdsafx.WrapTransferOutput(b)
		case wire.ShapeKindMintOutput:
			return slhdsafx.WrapMintOutput(b)
		}
	case wire.TypeKindEd25519:
		switch sk {
		case wire.ShapeKindTransferOutput:
			return ed25519fx.WrapTransferOutput(b)
		case wire.ShapeKindMintOutput:
			return ed25519fx.WrapMintOutput(b)
		}
	case wire.TypeKindSecp256r1:
		switch sk {
		case wire.ShapeKindTransferOutput:
			return secp256r1fx.WrapTransferOutput(b)
		case wire.ShapeKindMintOutput:
			return secp256r1fx.WrapMintOutput(b)
		}
	case wire.TypeKindSchnorr:
		switch sk {
		case wire.ShapeKindTransferOutput:
			return schnorrfx.WrapTransferOutput(b)
		case wire.ShapeKindMintOutput:
			return schnorrfx.WrapMintOutput(b)
		}
	case wire.TypeKindBLS12381:
		if sk == wire.ShapeKindAttestationOut {
			return bls12381fx.WrapAttestationOutput(b)
		}
	}
	return nil, fmt.Errorf("zap utxo dispatch: unknown (TypeKind=0x%02x, ShapeKind=0x%02x)", tk, sk)
}

// WrapInputBytes is the public entry to the fx-aware input dispatcher — the
// input-side counterpart of WrapOutputBytes. It reconstructs the polymorphic
// fx Input from its wire envelope; the concrete type (e.g.
// *secp256k1fx.TransferInput) also satisfies luxfi/utxo's TransferableIn, so
// consumers holding the utxo type tree (xvm/txs) can type-assert across.
func WrapInputBytes(b []byte) (TransferableIn, error) {
	return wrapInputBytes(b)
}

// wrapInputBytes is the input-side counterpart of wrapOutput: it
// reconstructs a TransferableIn from its fx wire envelope, dispatching on
// the (TypeKind, ShapeKind) discriminator. Each branch calls exactly one
// fx-package WrapTransferInput / WrapAttestationInput — the fx primitive
// owns its own wire; components/lux stays fx-agnostic. Used by
// TransferableInput.Unmarshal.
func wrapInputBytes(b []byte) (TransferableIn, error) {
	tk, sk, err := wire.PeekDiscriminator(b)
	if err != nil {
		return nil, fmt.Errorf("peek input discriminator: %w", err)
	}
	switch tk {
	case wire.TypeKindSecp256k1:
		if sk == wire.ShapeKindTransferInput {
			return secp256k1fx.WrapTransferInput(b)
		}
	case wire.TypeKindMLDSA:
		if sk == wire.ShapeKindTransferInput {
			return mldsafx.WrapTransferInput(b)
		}
	case wire.TypeKindSLHDSA:
		if sk == wire.ShapeKindTransferInput {
			return slhdsafx.WrapTransferInput(b)
		}
	case wire.TypeKindEd25519:
		if sk == wire.ShapeKindTransferInput {
			return ed25519fx.WrapTransferInput(b)
		}
	case wire.TypeKindSecp256r1:
		if sk == wire.ShapeKindTransferInput {
			return secp256r1fx.WrapTransferInput(b)
		}
	case wire.TypeKindSchnorr:
		if sk == wire.ShapeKindTransferInput {
			return schnorrfx.WrapTransferInput(b)
		}
	}
	// bls12381fx attestations are not value-transfer inputs (no Amount),
	// so they never appear as a TransferableIn.
	return nil, fmt.Errorf("zap input dispatch: unknown (TypeKind=0x%02x, ShapeKind=0x%02x)", tk, sk)
}
