// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

// Reusable delta-field encoders every non-proposal tx composes on top of the
// spending envelope: owner (fx.Owner), auth (verify.Verifiable == *secp256k1fx.Input),
// inline Validator (44B) and Signer (145B), id lists, and extra spending lists
// (StakeOuts / ImportedInputs / ExportedOutputs). All build directly on
// luxfi/zap. Construction (write*) lives inside New*Tx; accessors (read*) read
// lazily.

import (
	"fmt"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/signer"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
)

const (
	blsPubLen = 48 // bls.PublicKeyLen
	blsSigLen = 96 // bls.SignatureLen
)

// ---- fixed ids ----

func setID(ob *zap.ObjectBuilder, off int, id ids.ID)         { ob.SetBytesFixed(off, id[:]) }
func setNodeID(ob *zap.ObjectBuilder, off int, id ids.NodeID) { ob.SetBytesFixed(off, id[:]) }

func readNodeID(o zap.Object, off int) ids.NodeID {
	var id ids.NodeID
	copy(id[:], o.BytesFixedSlice(off, ids.NodeIDLen))
	return id
}

// ---- auth (verify.Verifiable == *secp256k1fx.Input) ----

func writeAuth(b *zap.Builder, auth verify.Verifiable) (off, count int, err error) {
	in, ok := auth.(*secp256k1fx.Input)
	if !ok {
		return 0, 0, fmt.Errorf("unsupported auth %T (want *secp256k1fx.Input)", auth)
	}
	if len(in.SigIndices) == 0 {
		return 0, 0, nil
	}
	lb := b.StartList(sigStride)
	for _, s := range in.SigIndices {
		lb.AddUint32(s)
	}
	off, count = lb.Finish()
	return off, count, nil
}

func readAuth(obj zap.Object, ptrOff int) verify.Verifiable {
	arr := obj.ListStride(ptrOff, sigStride)
	return &secp256k1fx.Input{SigIndices: sliceSigs(arr, 0, uint32(arr.Len()))}
}

// ---- owner (fx.Owner == *secp256k1fx.OutputOwners); header = threshold u32 +
// locktime u64 + addr-list ptr (8B) = 20 bytes inline ----

func writeOwner(b *zap.Builder, o fx.Owner) (threshold uint32, locktime uint64, addrOff, addrCount int, err error) {
	oo, ok := o.(*secp256k1fx.OutputOwners)
	if !ok {
		return 0, 0, 0, 0, fmt.Errorf("unsupported owner %T (want *secp256k1fx.OutputOwners)", o)
	}
	if len(oo.Addrs) > 0 {
		lb := b.StartList(addrStride)
		for _, a := range oo.Addrs {
			lb.AddBytes(a[:])
		}
		addrOff, _ = lb.Finish()
		addrCount = len(oo.Addrs) // AddBytes counts bytes, not elements
	}
	return oo.Threshold, oo.Locktime, addrOff, addrCount, nil
}

func setOwner(ob *zap.ObjectBuilder, thresholdOff, locktimeOff, addrPtrOff int, threshold uint32, locktime uint64, addrOff, addrCount int) {
	ob.SetUint32(thresholdOff, threshold)
	ob.SetUint64(locktimeOff, locktime)
	ob.SetList(addrPtrOff, addrOff, addrCount)
}

func readOwner(obj zap.Object, thresholdOff, locktimeOff, addrPtrOff int) fx.Owner {
	arr := obj.ListStride(addrPtrOff, addrStride)
	return &secp256k1fx.OutputOwners{
		Locktime:  obj.Uint64(locktimeOff),
		Threshold: obj.Uint32(thresholdOff),
		Addrs:     sliceAddrs(arr, 0, uint32(arr.Len())),
	}
}

// standalone owner object: threshold u32 @0, locktime u64 @4, addr ptr 8B @12.
const (
	ownerObjThreshold = 0
	ownerObjLocktime  = 4
	ownerObjAddrPtr   = 12
	ownerObjSize      = 20
)

// MarshalOwner is the ONE canonical byte encoding of an owner — same layout as
// the owner embedded in every tx, lifted to a standalone buffer. Used wherever
// a stable owner identity is needed off the tx wire (e.g. lock-owner hash keys
// in utxo verification, the L1-validator balance/deactivation owner blobs
// stored in P-Chain state). Takes `any` to match the fx.Owned.Owners() surface
// it serves. One encoding everywhere; no codec.
func MarshalOwner(o any) ([]byte, error) {
	oo, ok := o.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, fmt.Errorf("unsupported owner %T (want *secp256k1fx.OutputOwners)", o)
	}
	b := zap.NewBuilder(zap.HeaderSize + 128)
	threshold, locktime, addrOff, addrCount, err := writeOwner(b, oo)
	if err != nil {
		return nil, err
	}
	ob := b.StartObject(ownerObjSize)
	setOwner(ob, ownerObjThreshold, ownerObjLocktime, ownerObjAddrPtr, threshold, locktime, addrOff, addrCount)
	ob.FinishAsRoot()
	return b.Finish(), nil
}

// UnmarshalOwner is the exact inverse of MarshalOwner — it parses the canonical
// standalone owner buffer back into *secp256k1fx.OutputOwners. Same layout, no
// codec; the marshal/unmarshal pair is the one owner byte codec every consumer
// (executor, service, state-owner blobs) shares.
func UnmarshalOwner(b []byte) (*secp256k1fx.OutputOwners, error) {
	msg, err := zap.Parse(b)
	if err != nil {
		return nil, err
	}
	oo, ok := readOwner(msg.Root(), ownerObjThreshold, ownerObjLocktime, ownerObjAddrPtr).(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, fmt.Errorf("unexpected owner encoding")
	}
	return oo, nil
}

// ---- inline Validator (44B: NodeID20 + Start8 + End8 + Wght8) ----

const validatorSize = 44

func setValidator(ob *zap.ObjectBuilder, off int, v Validator) {
	setNodeID(ob, off, v.NodeID)
	ob.SetUint64(off+20, v.Start)
	ob.SetUint64(off+28, v.End)
	ob.SetUint64(off+36, v.Wght)
}

func readValidator(obj zap.Object, off int) Validator {
	return Validator{
		NodeID: readNodeID(obj, off),
		Start:  obj.Uint64(off + 20),
		End:    obj.Uint64(off + 28),
		Wght:   obj.Uint64(off + 36),
	}
}

// ---- inline Signer (145B: kind1 + PublicKey48 + PoP96) ----

const signerSize = 1 + blsPubLen + blsSigLen

func setSigner(ob *zap.ObjectBuilder, off int, s signer.Signer) error {
	switch sig := s.(type) {
	case *signer.Empty:
		ob.SetUint8(off, 0)
	case *signer.ProofOfPossession:
		ob.SetUint8(off, 1)
		ob.SetBytesFixed(off+1, sig.PublicKey[:])
		ob.SetBytesFixed(off+1+blsPubLen, sig.ProofOfPossession[:])
	default:
		return fmt.Errorf("unsupported signer %T", s)
	}
	return nil
}

func readSigner(obj zap.Object, off int) signer.Signer {
	if obj.Uint8(off) == 0 {
		return &signer.Empty{}
	}
	pop := &signer.ProofOfPossession{}
	copy(pop.PublicKey[:], obj.BytesFixedSlice(off+1, blsPubLen))
	copy(pop.ProofOfPossession[:], obj.BytesFixedSlice(off+1+blsPubLen, blsSigLen))
	return pop
}

// ---- id list ([]ids.ID) ----

func writeIDList(b *zap.Builder, list []ids.ID) (off, count int) {
	if len(list) == 0 {
		return 0, 0
	}
	lb := b.StartList(32)
	for _, id := range list {
		lb.AddBytes(id[:])
	}
	off, _ = lb.Finish()
	return off, len(list)
}

func readIDList(obj zap.Object, ptrOff int) []ids.ID {
	l := obj.ListStride(ptrOff, 32)
	n := l.Len()
	if n == 0 {
		return nil
	}
	out := make([]ids.ID, n)
	for i := 0; i < n; i++ {
		out[i] = readID(l.Object(i, 32), 0)
	}
	return out
}

// ---- extra spending lists (a second Outs/Ins beyond the envelope) ----

func writeExtraOuts(b *zap.Builder, outs []*lux.TransferableOutput) (listOff, listCount, addrOff, addrCount int, err error) {
	return writeOutputs(b, outs)
}

func readExtraOuts(obj zap.Object, listOff, addrOff int) []*lux.TransferableOutput {
	return readOutputs(obj, listOff, addrOff)
}

func writeExtraIns(b *zap.Builder, ins []*lux.TransferableInput) (listOff, listCount, sigOff, sigCount int, err error) {
	return writeInputs(b, ins)
}

func readExtraIns(obj zap.Object, listOff, sigOff int) []*lux.TransferableInput {
	return readInputs(obj, listOff, sigOff)
}
