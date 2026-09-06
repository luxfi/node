// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/runtime"
	"github.com/luxfi/util"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/types"
	"github.com/luxfi/zap"
)

// CreateNetworkTx is the sole network constructor — the ∅→Network birth of the
// decomplected model. It creates a network at ANY level of the hierarchy in one
// tx:
//
//   - Parent = the parent network. Primary ⇒ an L1; an L1 ⇒ an L2; recurse for
//     L3/L4. The tx is byte-identical at every level — only Parent differs.
//     level = depth in the parent tree; derivable, never stored.
//   - Owner authorises future admin against the network record.
//   - Security (security.Mode) is the two-axis model: RestakeParent and/or an
//     own validator set (Admission + Manager). One definition, shared with the
//     executor and state.
//   - ManagerChainID (+ address) names the chain hosting a Contract-governed
//     own set's staking contract; ids.Empty ⇒ P-Chain-governed (owner is the
//     authority). Symmetric with ConvertNetworkTx's manager reference.
//
// Chains are NOT created here — CreateChainTx is the sole chain constructor
// (one and only one way). An L1 spawn is a BLOCK of CreateNetworkTx + N
// CreateChainTx (block-atomic), not a tx that materialises chains.
//
// The new network's ID is derived from this tx's hash. The primary network
// records the network but does not track or validate its blocks.

const MaxChainAddressLength = 4096

var (
	_ UnsignedTx                        = (*CreateNetworkTx)(nil)
	_ utils.Sortable[*NetworkValidator] = (*NetworkValidator)(nil)

	ErrZeroWeight                   = errors.New("validator weight must be non-zero")
	ErrAddressTooLong               = errors.New("address is too long")
	ErrValidatorsNotSortedAndUnique = errors.New("validators must be sorted and unique")
	ErrOwnSetMustIncludeValidator   = errors.New("sovereign (non-restaking) network must include at least one genesis validator")
	ErrNoOwnSetButHasValidators     = errors.New("network with no own set must not carry validators")
	ErrContractManagerNeedsAddress  = errors.New("contract-governed own set requires a manager address")
)

// NetworkValidator is a genesis validator value (shared component; encoded
// into / decoded from the tx buffer).
type NetworkValidator struct {
	NodeID                types.JSONByteSlice
	Weight                uint64
	Balance               uint64
	Signer                signer.ProofOfPossession
	RemainingBalanceOwner message.PChainOwner
	DeactivationOwner     message.PChainOwner
}

func (v *NetworkValidator) Compare(o *NetworkValidator) int { return bytes.Compare(v.NodeID, o.NodeID) }

func (v *NetworkValidator) Verify() error {
	if v.Weight == 0 {
		return ErrZeroWeight
	}
	nodeID, err := ids.ToNodeID(v.NodeID)
	if err != nil {
		return err
	}
	if nodeID == ids.EmptyNodeID {
		return errEmptyNodeID
	}
	return verify.All(
		&v.Signer,
		&secp256k1fx.OutputOwners{Threshold: v.RemainingBalanceOwner.Threshold, Addrs: v.RemainingBalanceOwner.Addresses},
		&secp256k1fx.OutputOwners{Threshold: v.DeactivationOwner.Threshold, Addrs: v.DeactivationOwner.Addresses},
	)
}

// ---- shared NetworkValidator list wire (fixed-stride 192) ----
const (
	nvWeight        = 0
	nvBalance       = 8
	nvSignerPub     = 16
	nvSignerPoP     = 64
	nvNodeIDStart   = 160
	nvNodeIDLen     = 164
	nvRemThreshold  = 168
	nvRemAddrStart  = 172
	nvRemAddrCount  = 176
	nvDeacThreshold = 180
	nvDeacAddrStart = 184
	nvDeacAddrCount = 188
	nvStride        = 192
)

func writeNetworkValidators(b *zap.Builder, vdrs []*NetworkValidator) (listOff, listCount int, nodeIDs []byte, addrs []ids.ShortID) {
	if len(vdrs) == 0 {
		return 0, 0, nil, nil
	}
	lb := b.StartList(nvStride)
	for _, v := range vdrs {
		var e [nvStride]byte
		putU64(e[nvWeight:], v.Weight)
		putU64(e[nvBalance:], v.Balance)
		copy(e[nvSignerPub:], v.Signer.PublicKey[:])
		copy(e[nvSignerPoP:], v.Signer.ProofOfPossession[:])
		putU32(e[nvNodeIDStart:], uint32(len(nodeIDs)))
		putU32(e[nvNodeIDLen:], uint32(len(v.NodeID)))
		nodeIDs = append(nodeIDs, v.NodeID...)
		putU32(e[nvRemThreshold:], v.RemainingBalanceOwner.Threshold)
		putU32(e[nvRemAddrStart:], uint32(len(addrs)))
		putU32(e[nvRemAddrCount:], uint32(len(v.RemainingBalanceOwner.Addresses)))
		addrs = append(addrs, v.RemainingBalanceOwner.Addresses...)
		putU32(e[nvDeacThreshold:], v.DeactivationOwner.Threshold)
		putU32(e[nvDeacAddrStart:], uint32(len(addrs)))
		putU32(e[nvDeacAddrCount:], uint32(len(v.DeactivationOwner.Addresses)))
		addrs = append(addrs, v.DeactivationOwner.Addresses...)
		lb.AddBytes(e[:])
	}
	listOff, _ = lb.Finish()
	listCount = len(vdrs) // AddBytes counts bytes, not elements
	return listOff, listCount, nodeIDs, addrs
}

func readNetworkValidators(obj zap.Object, listOff, nodeIDPoolOff, addrPoolOff int) []*NetworkValidator {
	list := obj.ListStride(listOff, nvStride)
	n := list.Len()
	if n == 0 {
		return nil
	}
	nodeIDBlob := obj.Bytes(nodeIDPoolOff)
	addrs := obj.ListStride(addrPoolOff, addrStride)
	out := make([]*NetworkValidator, n)
	for i := 0; i < n; i++ {
		e := list.Object(i, nvStride)
		v := &NetworkValidator{Weight: e.Uint64(nvWeight), Balance: e.Uint64(nvBalance)}
		copy(v.Signer.PublicKey[:], e.BytesFixedSlice(nvSignerPub, 48))
		copy(v.Signer.ProofOfPossession[:], e.BytesFixedSlice(nvSignerPoP, 96))
		if ns, nl := e.Uint32(nvNodeIDStart), e.Uint32(nvNodeIDLen); nl > 0 && int(ns)+int(nl) <= len(nodeIDBlob) {
			v.NodeID = append([]byte(nil), nodeIDBlob[ns:ns+nl]...)
		}
		v.RemainingBalanceOwner = message.PChainOwner{Threshold: e.Uint32(nvRemThreshold), Addresses: sliceAddrs(addrs, e.Uint32(nvRemAddrStart), e.Uint32(nvRemAddrCount))}
		v.DeactivationOwner = message.PChainOwner{Threshold: e.Uint32(nvDeacThreshold), Addresses: sliceAddrs(addrs, e.Uint32(nvDeacAddrStart), e.Uint32(nvDeacAddrCount))}
		out[i] = v
	}
	return out
}

// ---- CreateNetworkTx (kindCreateNetwork) ----
const (
	offCN_Parent         = spendSize       // 32B
	offCN_OwnerThreshold = spendSize + 32  // u32
	offCN_OwnerLocktime  = spendSize + 36  // u64
	offCN_OwnerAddrPtr   = spendSize + 44  // 8B
	offCN_RestakeParent  = spendSize + 52  // u8 (security.Mode axis 1)
	offCN_Admission      = spendSize + 53  // u8 (security.Admission)
	offCN_Manager        = spendSize + 54  // u8 (security.Manager)
	offCN_Threshold      = spendSize + 55  // u64 (Open-admission min stake)
	offCN_Validators     = spendSize + 63  // 8B list
	offCN_ValNodeIDPool  = spendSize + 71  // 8B bytes
	offCN_ValAddrPool    = spendSize + 79  // 8B list
	offCN_ManagerChainID = spendSize + 87  // 32B ids.ID (direct chain ref; ids.Empty ⇒ P-Chain-governed)
	offCN_ManagerAddress = spendSize + 119 // 8B bytes
	sizeCNTx             = spendSize + 127
)

// setSecurity / readSecurity encode a security.Mode across four fixed object
// fields. Shared by CreateNetworkTx and ConvertNetworkTx — one wire encoding.
func setSecurity(ob zap.ObjectBuilder, offRestake, offAdmission, offManager, offThreshold int, m security.Mode) {
	var restake uint8
	if m.RestakeParent {
		restake = 1
	}
	ob.SetUint8(offRestake, restake)
	ob.SetUint8(offAdmission, uint8(m.Admission))
	ob.SetUint8(offManager, uint8(m.Manager))
	ob.SetUint64(offThreshold, m.Threshold)
}

func readSecurity(o zap.Object, offRestake, offAdmission, offManager, offThreshold int) security.Mode {
	return security.Mode{
		RestakeParent: o.Uint8(offRestake) != 0,
		Admission:     security.Admission(o.Uint8(offAdmission)),
		Manager:       security.Manager(o.Uint8(offManager)),
		Threshold:     o.Uint64(offThreshold),
	}
}

type CreateNetworkTx struct {
	spendingTx
}

func NewCreateNetworkTx(
	base *lux.BaseTx,
	parent ids.ID,
	owner fx.Owner,
	sec security.Mode,
	validators []*NetworkValidator,
	managerChainID ids.ID,
	managerAddress []byte,
) (*CreateNetworkTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 1024 + sizeCNTx + len(validators)*nvStride)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	oThreshold, oLocktime, oAddrOff, oAddrCount, err := writeOwner(b, owner)
	if err != nil {
		return nil, err
	}
	vdrOff, vdrCount, nodeIDPool, addrPool := writeNetworkValidators(b, validators)
	valAddrOff, valAddrCount := 0, 0
	if len(addrPool) > 0 {
		alb := b.StartList(addrStride)
		for _, a := range addrPool {
			alb.AddBytes(a[:])
		}
		valAddrOff, _ = alb.Finish()
		valAddrCount = len(addrPool)
	}

	ob := b.StartObject(sizeCNTx)
	setEnvelope(ob, kindCreateNetwork, base, p)
	setID(ob, offCN_Parent, parent)
	setOwner(ob, offCN_OwnerThreshold, offCN_OwnerLocktime, offCN_OwnerAddrPtr, oThreshold, oLocktime, oAddrOff, oAddrCount)
	setSecurity(ob, offCN_RestakeParent, offCN_Admission, offCN_Manager, offCN_Threshold, sec)
	ob.SetList(offCN_Validators, vdrOff, vdrCount)
	ob.SetBytes(offCN_ValNodeIDPool, nodeIDPool)
	ob.SetList(offCN_ValAddrPool, valAddrOff, valAddrCount)
	setID(ob, offCN_ManagerChainID, managerChainID)
	ob.SetBytes(offCN_ManagerAddress, managerAddress)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return &CreateNetworkTx{spendingTx{msg: msg}}, nil
}

func (tx *CreateNetworkTx) Parent() ids.ID { return readID(tx.root(), offCN_Parent) }
func (tx *CreateNetworkTx) Owner() fx.Owner {
	return readOwner(tx.root(), offCN_OwnerThreshold, offCN_OwnerLocktime, offCN_OwnerAddrPtr)
}
func (tx *CreateNetworkTx) Security() security.Mode {
	return readSecurity(tx.root(), offCN_RestakeParent, offCN_Admission, offCN_Manager, offCN_Threshold)
}
func (tx *CreateNetworkTx) Sovereign() bool { return tx.Security().Sovereign() }
func (tx *CreateNetworkTx) Validators() []*NetworkValidator {
	return readNetworkValidators(tx.root(), offCN_Validators, offCN_ValNodeIDPool, offCN_ValAddrPool)
}
func (tx *CreateNetworkTx) ManagerChainID() ids.ID { return readID(tx.root(), offCN_ManagerChainID) }
func (tx *CreateNetworkTx) ManagerAddress() []byte {
	if a := tx.root().Bytes(offCN_ManagerAddress); len(a) > 0 {
		return append([]byte(nil), a...)
	}
	return nil
}

func (tx *CreateNetworkTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	vdrs := tx.Validators()
	sec := tx.Security()
	// enum ranges + cross-axis invariant (RestakeParent || own set) live on Mode.
	if err := sec.Valid(); err != nil {
		return err
	}
	switch {
	case !sec.Sovereign() && len(vdrs) != 0:
		// no own set ⇒ the tx must not carry validators.
		return ErrNoOwnSetButHasValidators
	case !sec.RestakeParent && len(vdrs) == 0:
		// a network that does not restake its parent must ship a bootstrap
		// validator to produce its first block.
		return ErrOwnSetMustIncludeValidator
	case sec.Sovereign() && sec.Manager == security.Contract && len(tx.ManagerAddress()) == 0:
		return ErrContractManagerNeedsAddress
	case !utils.IsSortedAndUnique(vdrs):
		return ErrValidatorsNotSortedAndUnique
	case len(tx.ManagerAddress()) > MaxChainAddressLength:
		return ErrAddressTooLong
	}
	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	if err := tx.Owner().Verify(); err != nil {
		return err
	}
	for _, vdr := range vdrs {
		if err := vdr.Verify(); err != nil {
			return err
		}
	}
	return nil
}

func (tx *CreateNetworkTx) Visit(visitor Visitor) error { return visitor.CreateNetworkTx(tx) }
