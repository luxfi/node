// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

// Native-ZAP wire for the P-Chain state DB VALUE types that used to flow
// through the deleted reflection codec (block.GenesisCodec). Each stored
// value type gets one struct-is-wire encoder pair (marshalX/parseX) built
// directly on github.com/luxfi/zap generic object/list primitives — no
// codec, no version prefix, no slot registry. Re-genesis means the on-disk
// format is free to change, so these layouts are the canonical ones.
//
// Every stored FIELD of every value is preserved; only the framing changed
// (reflection walk → fixed-offset zap object). Reader accessors are
// zero-copy views into the DB value buffer; []byte fields are copied out so
// a cached value never aliases the transient DB read.

import (
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

const (
	stAddrStride = 20 // ids.ShortIDLen
	stIDLen      = 32 // ids.IDLen
	stNodeIDLen  = 20 // ids.NodeIDLen
)

// stReadID reads a 32-byte id at the given object offset.
func stReadID(o zap.Object, off int) ids.ID {
	var id ids.ID
	copy(id[:], o.BytesFixedSlice(off, stIDLen))
	return id
}

// stCopyBytes returns a fresh copy of a zap byte view (nil for empty), so a
// long-lived value never aliases the transient DB read buffer.
func stCopyBytes(v []byte) []byte {
	if len(v) == 0 {
		return nil
	}
	return append([]byte(nil), v...)
}

// ---- OutputOwners (fx.Owner == *secp256k1fx.OutputOwners) ----
//
// object (size 20): threshold u32 @0, locktime u64 @4, addr-list ptr @12.
// addr list: 20-byte-stride ids.ShortID entries.

const (
	ownThreshold = 0
	ownLocktime  = 4
	ownAddrs     = 12
	ownSize      = 20
)

func marshalOwner(o fx.Owner) ([]byte, error) {
	oo, err := asOutputOwners(o)
	if err != nil {
		return nil, err
	}
	b := zap.NewBuilder(zap.HeaderSize + ownSize + len(oo.Addrs)*stAddrStride)

	var addrOff, addrCount int
	if len(oo.Addrs) > 0 {
		lb := b.StartList(stAddrStride)
		for i := range oo.Addrs {
			lb.AddBytes(oo.Addrs[i][:])
		}
		// AddBytes counts bytes, not elements — use the real element count.
		addrOff, _ = lb.Finish()
		addrCount = len(oo.Addrs)
	}

	ob := b.StartObject(ownSize)
	ob.SetUint32(ownThreshold, oo.Threshold)
	ob.SetUint64(ownLocktime, oo.Locktime)
	ob.SetList(ownAddrs, addrOff, addrCount)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseOwner(b []byte) (fx.Owner, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return nil, err
	}
	obj := msg.Root()
	arr := obj.ListStride(ownAddrs, stAddrStride)
	n := arr.Len()
	var addrs []ids.ShortID
	if n > 0 {
		addrs = make([]ids.ShortID, n)
		for i := 0; i < n; i++ {
			e := arr.Object(i, stAddrStride)
			for j := 0; j < stAddrStride; j++ {
				addrs[i][j] = e.Uint8(j)
			}
		}
	}
	return &secp256k1fx.OutputOwners{
		Locktime:  obj.Uint64(ownLocktime),
		Threshold: obj.Uint32(ownThreshold),
		Addrs:     addrs,
	}, nil
}

func asOutputOwners(o fx.Owner) (*secp256k1fx.OutputOwners, error) {
	switch oo := o.(type) {
	case *secp256k1fx.OutputOwners:
		return oo, nil
	case *fx.OutputOwnersWrapper:
		return oo.OutputOwners, nil
	default:
		return nil, fmt.Errorf("state: unsupported owner %T (want *secp256k1fx.OutputOwners)", o)
	}
}

// ---- L1Validator (LP-77 validator VALUE; ValidationID is the DB key, not
// serialized). Fixed object; the three owner/key blobs are opaque []byte. ----
//
// object (size 108):
//   ChainID            32B @ 0
//   NodeID             20B @ 32
//   StartTime          u64 @ 52
//   Weight             u64 @ 60
//   MinNonce           u64 @ 68
//   EndAccumulatedFee  u64 @ 76
//   PublicKey             bytes ptr @ 84
//   RemainingBalanceOwner bytes ptr @ 92
//   DeactivationOwner     bytes ptr @ 100

const (
	l1vChainID       = 0
	l1vNodeID        = 32
	l1vStartTime     = 52
	l1vWeight        = 60
	l1vMinNonce      = 68
	l1vEndAccrued    = 76
	l1vPublicKey     = 84
	l1vRemainingOwn  = 92
	l1vDeactivateOwn = 100
	l1vSize          = 108
)

func marshalL1Validator(v L1Validator) ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + l1vSize +
		len(v.PublicKey) + len(v.RemainingBalanceOwner) + len(v.DeactivationOwner))
	ob := b.StartObject(l1vSize)
	ob.SetBytesFixed(l1vChainID, v.ChainID[:])
	ob.SetBytesFixed(l1vNodeID, v.NodeID[:])
	ob.SetUint64(l1vStartTime, v.StartTime)
	ob.SetUint64(l1vWeight, v.Weight)
	ob.SetUint64(l1vMinNonce, v.MinNonce)
	ob.SetUint64(l1vEndAccrued, v.EndAccumulatedFee)
	ob.SetBytes(l1vPublicKey, v.PublicKey)
	ob.SetBytes(l1vRemainingOwn, v.RemainingBalanceOwner)
	ob.SetBytes(l1vDeactivateOwn, v.DeactivationOwner)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// parseL1Validator fills v's serialized fields from b. v.ValidationID (the DB
// key) is left untouched — the caller sets it.
func parseL1Validator(b []byte, v *L1Validator) error {
	msg, err := zap.Parse(b)
	if err != nil {
		return err
	}
	obj := msg.Root()
	v.ChainID = stReadID(obj, l1vChainID)
	copy(v.NodeID[:], obj.BytesFixedSlice(l1vNodeID, stNodeIDLen))
	v.StartTime = obj.Uint64(l1vStartTime)
	v.Weight = obj.Uint64(l1vWeight)
	v.MinNonce = obj.Uint64(l1vMinNonce)
	v.EndAccumulatedFee = obj.Uint64(l1vEndAccrued)
	v.PublicKey = stCopyBytes(obj.Bytes(l1vPublicKey))
	v.RemainingBalanceOwner = stCopyBytes(obj.Bytes(l1vRemainingOwn))
	v.DeactivationOwner = stCopyBytes(obj.Bytes(l1vDeactivateOwn))
	return nil
}

// ---- feeState (gas.State: Capacity + Excess, both gas.Gas == uint64) ----

const (
	feeCapacity = 0
	feeExcess   = 8
	feeSize     = 16
)

func marshalFeeState(s gas.State) ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + feeSize)
	ob := b.StartObject(feeSize)
	ob.SetUint64(feeCapacity, uint64(s.Capacity))
	ob.SetUint64(feeExcess, uint64(s.Excess))
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseFeeState(b []byte) (gas.State, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return gas.State{}, err
	}
	obj := msg.Root()
	return gas.State{
		Capacity: gas.Gas(obj.Uint64(feeCapacity)),
		Excess:   gas.Gas(obj.Uint64(feeExcess)),
	}, nil
}

// ---- heightRange (LowerBound + UpperBound, both uint64) ----

const (
	hrLower = 0
	hrUpper = 8
	hrSize  = 16
)

func marshalHeightRange(h *heightRange) ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + hrSize)
	ob := b.StartObject(hrSize)
	ob.SetUint64(hrLower, h.LowerBound)
	ob.SetUint64(hrUpper, h.UpperBound)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseHeightRange(b []byte, h *heightRange) error {
	msg, err := zap.Parse(b)
	if err != nil {
		return err
	}
	obj := msg.Root()
	h.LowerBound = obj.Uint64(hrLower)
	h.UpperBound = obj.Uint64(hrUpper)
	return nil
}

// ---- NetToL1Conversion (ConversionID + ChainID + Addr bytes) ----
//
// object (size 72): ConversionID 32B @0, ChainID 32B @32, Addr bytes ptr @64.

const (
	convConversionID = 0
	convChainID      = 32
	convAddr         = 64
	convSize         = 72
)

func marshalConversion(c NetToL1Conversion) ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + convSize + len(c.Addr))
	ob := b.StartObject(convSize)
	ob.SetBytesFixed(convConversionID, c.ConversionID[:])
	ob.SetBytesFixed(convChainID, c.ChainID[:])
	ob.SetBytes(convAddr, c.Addr)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseConversion(b []byte) (NetToL1Conversion, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return NetToL1Conversion{}, err
	}
	obj := msg.Root()
	return NetToL1Conversion{
		ConversionID: stReadID(obj, convConversionID),
		ChainID:      stReadID(obj, convChainID),
		Addr:         stCopyBytes(obj.Bytes(convAddr)),
	}, nil
}

// ---- txBytesAndStatus (stored tx: signed bytes + acceptance status) ----
//
// object (size 16): Status u32 @0, Tx bytes ptr @8. The signed tx bytes are
// stored opaque — the tx is a self-delimiting zap buffer re-parsed via
// txs.Parse, so its TxID is preserved with no re-encoding.

const (
	txsStatus = 0
	txsTx     = 8
	txsSize   = 16
)

func marshalTxStatus(stx txBytesAndStatus) ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + txsSize + len(stx.Tx))
	ob := b.StartObject(txsSize)
	ob.SetUint32(txsStatus, uint32(stx.Status))
	ob.SetBytes(txsTx, stx.Tx)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseTxStatus(b []byte) (txBytesAndStatus, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return txBytesAndStatus{}, err
	}
	obj := msg.Root()
	return txBytesAndStatus{
		Tx:     stCopyBytes(obj.Bytes(txsTx)),
		Status: status.Status(obj.Uint32(txsStatus)),
	}, nil
}

// ---- validatorMetadata (staker uptime/reward record; txID is the DB key, not
// serialized). Fixed object of five uint64 fields. ----

const (
	vmdUpDuration      = 0
	vmdLastUpdated     = 8
	vmdPotentialReward = 16
	vmdDelegateeReward = 24
	vmdStakerStartTime = 32
	vmdSize            = 40
)

func marshalValidatorMetadata(m *validatorMetadata) ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + vmdSize)
	ob := b.StartObject(vmdSize)
	ob.SetUint64(vmdUpDuration, uint64(m.UpDuration))
	ob.SetUint64(vmdLastUpdated, m.LastUpdated)
	ob.SetUint64(vmdPotentialReward, m.PotentialReward)
	ob.SetUint64(vmdDelegateeReward, m.PotentialDelegateeReward)
	ob.SetUint64(vmdStakerStartTime, m.StakerStartTime)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// unmarshalValidatorMetadata overlays m's five serialized fields from b. All
// five are always written, so a full buffer overwrites any caller-supplied
// tx-default values (StakerStartTime/LastUpdated); the derived lastUpdated and
// the DB-key txID are handled by the parseValidatorMetadata wrapper.
func unmarshalValidatorMetadata(b []byte, m *validatorMetadata) error {
	msg, err := zap.Parse(b)
	if err != nil {
		return err
	}
	obj := msg.Root()
	m.UpDuration = time.Duration(obj.Uint64(vmdUpDuration))
	m.LastUpdated = obj.Uint64(vmdLastUpdated)
	m.PotentialReward = obj.Uint64(vmdPotentialReward)
	m.PotentialDelegateeReward = obj.Uint64(vmdDelegateeReward)
	m.StakerStartTime = obj.Uint64(vmdStakerStartTime)
	return nil
}

// ---- delegatorMetadata (PotentialReward + StakerStartTime; txID is the DB
// key, not serialized). ----

const (
	dmdPotentialReward = 0
	dmdStakerStartTime = 8
	dmdSize            = 16
)

func marshalDelegatorMetadata(m *delegatorMetadata) ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + dmdSize)
	ob := b.StartObject(dmdSize)
	ob.SetUint64(dmdPotentialReward, m.PotentialReward)
	ob.SetUint64(dmdStakerStartTime, m.StakerStartTime)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func unmarshalDelegatorMetadata(b []byte, m *delegatorMetadata) error {
	msg, err := zap.Parse(b)
	if err != nil {
		return err
	}
	obj := msg.Root()
	m.PotentialReward = obj.Uint64(dmdPotentialReward)
	m.StakerStartTime = obj.Uint64(dmdStakerStartTime)
	return nil
}
