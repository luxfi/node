// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// RegisterL1ValidatorTx v3 schema — TxKind + ValidationID + Signer (BLS pubkey
// + PoP) + Expiry + RemainingBalanceOwnerID. The legacy struct's Message is a
// Warp payload (variable-length addressed-call envelope) and
// RemainingBalanceOwner is an OutputOwners with a variable AddressIDs list —
// both ship as proper list/object schemas in batch 3. This v3 fixes the
// foundational identity fields validators look up on every block.
//
// Fixed-section layout (size 217 bytes):
//
//	TxKind                    uint8  @ 0
//	ValidationID              32B    @ 1
//	BLSPublicKey              48B    @ 33    (bls.PublicKeyLen)
//	ProofOfPossession         96B    @ 81    (bls.SignatureLen)
//	Expiry                    uint64 @ 177
//	RemainingBalanceOwnerID   ids.ID @ 185   (placeholder 32B, full Owner in batch 3)
const (
	SchemaVersionRegisterL1ValidatorTx uint16 = 3

	OffsetRegisterL1ValidatorTx_ValidationID            = 1
	OffsetRegisterL1ValidatorTx_BLSPublicKey            = OffsetRegisterL1ValidatorTx_ValidationID + 32
	OffsetRegisterL1ValidatorTx_ProofOfPossession       = OffsetRegisterL1ValidatorTx_BLSPublicKey + bls.PublicKeyLen
	OffsetRegisterL1ValidatorTx_Expiry                  = OffsetRegisterL1ValidatorTx_ProofOfPossession + bls.SignatureLen
	OffsetRegisterL1ValidatorTx_RemainingBalanceOwnerID = OffsetRegisterL1ValidatorTx_Expiry + 8
	SizeRegisterL1ValidatorTx                           = OffsetRegisterL1ValidatorTx_RemainingBalanceOwnerID + 32
)

type RegisterL1ValidatorTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t RegisterL1ValidatorTx) ValidationID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetRegisterL1ValidatorTx_ValidationID + i)
	}
	return out
}

// BLSPublicKey returns the compressed-G1 BLS public key (48 bytes) registered
// for this validator. Zero-copy by-value return; backing storage is the ZAP
// buffer.
func (t RegisterL1ValidatorTx) BLSPublicKey() [bls.PublicKeyLen]byte {
	var out [bls.PublicKeyLen]byte
	for i := 0; i < bls.PublicKeyLen; i++ {
		out[i] = t.obj.Uint8(OffsetRegisterL1ValidatorTx_BLSPublicKey + i)
	}
	return out
}

// ProofOfPossession returns the BLS signature over the public key proving
// the registrant controls the matching secret key.
func (t RegisterL1ValidatorTx) ProofOfPossession() [bls.SignatureLen]byte {
	var out [bls.SignatureLen]byte
	for i := 0; i < bls.SignatureLen; i++ {
		out[i] = t.obj.Uint8(OffsetRegisterL1ValidatorTx_ProofOfPossession + i)
	}
	return out
}

func (t RegisterL1ValidatorTx) Expiry() uint64 {
	return t.obj.Uint64(OffsetRegisterL1ValidatorTx_Expiry)
}

// RemainingBalanceOwnerID is a v3 placeholder for the OutputOwners pointer.
// Batch 3 replaces this with a full OutputOwners nested-object schema
// (threshold + AddressIDs list).
func (t RegisterL1ValidatorTx) RemainingBalanceOwnerID() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetRegisterL1ValidatorTx_RemainingBalanceOwnerID + i)
	}
	return out
}

func (t RegisterL1ValidatorTx) Bytes() []byte { return t.msg.Bytes() }
func (t RegisterL1ValidatorTx) IsZero() bool  { return t.msg == nil }

func WrapRegisterL1ValidatorTx(b []byte) (RegisterL1ValidatorTx, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return RegisterL1ValidatorTx{}, err
	}
	obj := msg.Root()
	if TxKind(obj.Uint8(OffsetTxKind)) != TxKindRegisterL1Validator {
		return RegisterL1ValidatorTx{}, ErrWrongTxKind
	}
	return RegisterL1ValidatorTx{msg: msg, obj: obj}, nil
}

func NewRegisterL1ValidatorTx(
	validationID ids.ID,
	blsPublicKey [bls.PublicKeyLen]byte,
	proofOfPossession [bls.SignatureLen]byte,
	expiry uint64,
	remainingBalanceOwnerID ids.ID,
) RegisterL1ValidatorTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeRegisterL1ValidatorTx)
	ob := b.StartObject(SizeRegisterL1ValidatorTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindRegisterL1Validator))
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetRegisterL1ValidatorTx_ValidationID+i, validationID[i])
	}
	for i := 0; i < bls.PublicKeyLen; i++ {
		ob.SetUint8(OffsetRegisterL1ValidatorTx_BLSPublicKey+i, blsPublicKey[i])
	}
	for i := 0; i < bls.SignatureLen; i++ {
		ob.SetUint8(OffsetRegisterL1ValidatorTx_ProofOfPossession+i, proofOfPossession[i])
	}
	ob.SetUint64(OffsetRegisterL1ValidatorTx_Expiry, expiry)
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetRegisterL1ValidatorTx_RemainingBalanceOwnerID+i, remainingBalanceOwnerID[i])
	}
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return RegisterL1ValidatorTx{msg: msg, obj: msg.Root()}
}
