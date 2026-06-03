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

// wrapOutput dispatches the inner Output envelope on (TypeKind,
// ShapeKind). Each branch calls exactly one fx-package WrapXxxOutput.
// Adding a new (fx, shape) is a new branch — no shared codec to grow.
func wrapOutput(b []byte, tk wire.TypeKind, sk wire.ShapeKind) (verify.State, error) {
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
