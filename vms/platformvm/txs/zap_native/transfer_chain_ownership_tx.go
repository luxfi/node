// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

// TransferChainOwnershipTx v1 schema — Chain + a single-address Owner stub.
//
// The legacy struct's Owner is an fx.Owner interface — almost always
// *secp256k1fx.OutputOwners with {Threshold uint32, Locktime uint64,
// AddressIDs []ids.ShortID}. The variable AddressIDs list ships with the
// proper schema in batch 3. The v1 form pins the single most common
// configuration: threshold-1 / locktime-0 / single owner address. Callers
// that need richer Owner shapes still have the legacy encoder available
// behind LUXD_ENABLE_LEGACY_CODEC.
//
// The legacy struct also has ChainAuth (verify.Verifiable, a credential
// proof produced by the signer). This is itself signature material that
// rides outside the unsigned-tx bytes in the signed wrapper, so it stays
// in the signed-tx schema (separate from the unsigned encoding here).
//
// Fixed-section layout (size 56 bytes):
//
//	Chain          32B    @ 0    (ids.ID)
//	OwnerThreshold uint32 @ 32
//	OwnerLocktime  uint64 @ 40
//	OwnerAddress   20B    @ 48   (ids.ShortID; single owner v1 stub)
const (
	SchemaVersionTransferChainOwnershipTx uint16 = 2

	OffsetTransferChainOwnershipTx_Chain          = 0
	OffsetTransferChainOwnershipTx_OwnerThreshold = 32
	OffsetTransferChainOwnershipTx_OwnerLocktime  = 40
	OffsetTransferChainOwnershipTx_OwnerAddress   = 48
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
	return TransferChainOwnershipTx{msg: msg, obj: msg.Root()}, nil
}

func NewTransferChainOwnershipTx(
	chain ids.ID,
	ownerThreshold uint32,
	ownerLocktime uint64,
	ownerAddress ids.ShortID,
) TransferChainOwnershipTx {
	b := zap.NewBuilder(zap.HeaderSize + 16 + SizeTransferChainOwnershipTx)
	ob := b.StartObject(SizeTransferChainOwnershipTx)
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
