// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// TransferChainOwnershipTx v3 schema — TxKind + Chain + single-address Owner.
//
// The legacy struct's Owner is an fx.Owner interface — almost always
// *secp256k1fx.OutputOwners with {Threshold uint32, Locktime uint64,
// AddressIDs []ids.ShortID}. The variable AddressIDs list ships with the
// proper schema in batch 3. The v3 form pins the single most common
// configuration: threshold-1 / locktime-0 / single owner address. Callers
// that need richer Owner shapes still have the legacy encoder available
// behind LUXD_ENABLE_LEGACY_CODEC.
//
// The legacy struct also has ChainAuth (verify.Verifiable, a credential
// proof produced by the signer). This is itself signature material that
// rides outside the unsigned-tx bytes in the signed wrapper, so it stays
// in the signed-tx schema (separate from the unsigned encoding here).
//
// Fixed-section layout (size 69 bytes):
//
//	TxKind         uint8  @ 0
//	Chain          32B    @ 1     (ids.ID)
//	OwnerThreshold uint32 @ 33
//	OwnerLocktime  uint64 @ 37
//	OwnerAddress   20B    @ 45    (ids.ShortID; single owner v3 stub)
//
// Fields are tightly packed; uint64 reads via binary.LittleEndian.Uint64 are
// alignment-tolerant so no natural-alignment padding is required.
const (
	SchemaVersionTransferChainOwnershipTx uint16 = 3

	OffsetTransferChainOwnershipTx_Chain          = 1
	OffsetTransferChainOwnershipTx_OwnerThreshold = 33
	OffsetTransferChainOwnershipTx_OwnerLocktime  = 37
	OffsetTransferChainOwnershipTx_OwnerAddress   = 45
	SizeTransferChainOwnershipTx                  = OffsetTransferChainOwnershipTx_OwnerAddress + ids.ShortIDLen
)

type TransferChainOwnershipTx struct {
	msg *zap.Message
	obj zap.Object
}

func (t TransferChainOwnershipTx) Chain() ids.ID {
	var out ids.ID
	for i := 0; i < 32; i++ {
		out[i] = t.obj.Uint8(OffsetTransferChainOwnershipTx_Chain + i)
	}
	return out
}

func (t TransferChainOwnershipTx) OwnerThreshold() uint32 {
	return t.obj.Uint32(OffsetTransferChainOwnershipTx_OwnerThreshold)
}

func (t TransferChainOwnershipTx) OwnerLocktime() uint64 {
	return t.obj.Uint64(OffsetTransferChainOwnershipTx_OwnerLocktime)
}

func (t TransferChainOwnershipTx) OwnerAddress() ids.ShortID {
	var out ids.ShortID
	for i := 0; i < ids.ShortIDLen; i++ {
		out[i] = t.obj.Uint8(OffsetTransferChainOwnershipTx_OwnerAddress + i)
	}
	return out
}

func (t TransferChainOwnershipTx) Bytes() []byte { return t.msg.Bytes() }
func (t TransferChainOwnershipTx) IsZero() bool  { return t.msg == nil }

func WrapTransferChainOwnershipTx(b []byte) (TransferChainOwnershipTx, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return TransferChainOwnershipTx{}, err
	}
	obj := msg.Root()
	if TxKind(obj.Uint8(OffsetTxKind)) != TxKindTransferChainOwnership {
		return TransferChainOwnershipTx{}, ErrWrongTxKind
	}
	return TransferChainOwnershipTx{msg: msg, obj: obj}, nil
}

func NewTransferChainOwnershipTx(
	chain ids.ID,
	ownerThreshold uint32,
	ownerLocktime uint64,
	ownerAddress ids.ShortID,
) TransferChainOwnershipTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeTransferChainOwnershipTx)
	ob := b.StartObject(SizeTransferChainOwnershipTx)
	ob.SetUint8(OffsetTxKind, uint8(TxKindTransferChainOwnership))
	for i := 0; i < 32; i++ {
		ob.SetUint8(OffsetTransferChainOwnershipTx_Chain+i, chain[i])
	}
	ob.SetUint32(OffsetTransferChainOwnershipTx_OwnerThreshold, ownerThreshold)
	ob.SetUint64(OffsetTransferChainOwnershipTx_OwnerLocktime, ownerLocktime)
	for i := 0; i < ids.ShortIDLen; i++ {
		ob.SetUint8(OffsetTransferChainOwnershipTx_OwnerAddress+i, ownerAddress[i])
	}
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return TransferChainOwnershipTx{msg: msg, obj: msg.Root()}
}
