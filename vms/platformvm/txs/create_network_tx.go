// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"errors"
	"unicode"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/runtime"
	"github.com/luxfi/utils"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/types"
	"github.com/luxfi/zap"
)

// CreateNetworkTx is the sole network constructor — the decomplected fold of
// the former CreateNetwork + ConvertNetworkToL1 + CreateSovereignL1 txs. It
// creates a network at ANY level of the hierarchy in one tx:
//
//   - Parent = the parent network. Primary ⇒ an L1; an L1 ⇒ an L2; recurse
//     for L3/L4. The tx is byte-identical at every level — only Parent differs.
//     level = depth in the parent tree; it is derivable, never stored.
//   - Owner authorises future admin against the network record.
//   - Security (security.Mode) is the two-axis model: RestakeParent and/or an
//     own validator set (Admission + Manager). One definition, shared with the
//     executor and state.
//   - Chains are created at genesis (may be empty; more via CreateChainTx).
//   - Manager (chain index + address) is the on-chain validator-manager for a
//     Contract-governed own set.
//
// The new network's ID is derived from this tx's hash. The primary network
// records the network but does not track or validate its blocks.

const (
	MaxChainAddressLength = 4096
	MaxNetworkChains      = 16
)

var (
	_ UnsignedTx                        = (*CreateNetworkTx)(nil)
	_ utils.Sortable[*NetworkValidator] = (*NetworkValidator)(nil)

	ErrZeroWeight                   = errors.New("validator weight must be non-zero")
	ErrAddressTooLong               = errors.New("address is too long")
	ErrValidatorsNotSortedAndUnique = errors.New("validators must be sorted and unique")
	ErrOwnSetMustIncludeValidator   = errors.New("sovereign (non-restaking) network must include at least one genesis validator")
	ErrNoOwnSetButHasValidators     = errors.New("network with no own set must not carry validators")
	ErrContractManagerNeedsAddress  = errors.New("contract-governed own set requires a manager address")
	ErrNetworkTooManyChains         = errors.New("network exceeds MaxNetworkChains")
	ErrNetworkManagerIdxOutOfRange  = errors.New("managerChainIdx out of range for chains[]")
	ErrChainNameTooLong             = errors.New("chain name exceeds MaxNameLen")
	ErrChainNameIllegal             = errors.New("chain name contains illegal characters")
	ErrChainVMIDEmpty               = errors.New("chain VMID must not be empty")
	ErrChainFxIDsNotSorted          = errors.New("chain FxIDs must be sorted and unique")
	ErrChainGenesisTooLong          = errors.New("chain genesis exceeds MaxGenesisLen")
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

// NetworkChain is a genesis-chain value (shared with CreateChainTx).
type NetworkChain struct {
	BlockchainName string
	VMID           ids.ID
	FxIDs          []ids.ID
	GenesisData    []byte
}

func (ch *NetworkChain) Verify() error {
	if len(ch.BlockchainName) > MaxNameLen {
		return ErrChainNameTooLong
	}
	for _, r := range ch.BlockchainName {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ') {
			return ErrChainNameIllegal
		}
	}
	if ch.VMID == ids.Empty {
		return ErrChainVMIDEmpty
	}
	if !utils.IsSortedAndUnique(ch.FxIDs) {
		return ErrChainFxIDsNotSorted
	}
	if len(ch.GenesisData) > MaxGenesisLen {
		return ErrChainGenesisTooLong
	}
	return nil
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

// ---- shared NetworkChain list wire (fixed-stride 56) ----
const (
	ncVMID    = 0
	ncNameOff = 32
	ncNameLen = 36
	ncFxStart = 40
	ncFxCount = 44
	ncGenOff  = 48
	ncGenLen  = 52
	ncStride  = 56
)

func writeNetworkChains(b *zap.Builder, chains []*NetworkChain) (listOff, listCount int, nameBlob []byte, fxIDs []ids.ID, genBlob []byte) {
	if len(chains) == 0 {
		return 0, 0, nil, nil, nil
	}
	lb := b.StartList(ncStride)
	for _, ch := range chains {
		var e [ncStride]byte
		copy(e[ncVMID:], ch.VMID[:])
		putU32(e[ncNameOff:], uint32(len(nameBlob)))
		putU32(e[ncNameLen:], uint32(len(ch.BlockchainName)))
		nameBlob = append(nameBlob, ch.BlockchainName...)
		putU32(e[ncFxStart:], uint32(len(fxIDs)))
		putU32(e[ncFxCount:], uint32(len(ch.FxIDs)))
		fxIDs = append(fxIDs, ch.FxIDs...)
		putU32(e[ncGenOff:], uint32(len(genBlob)))
		putU32(e[ncGenLen:], uint32(len(ch.GenesisData)))
		genBlob = append(genBlob, ch.GenesisData...)
		lb.AddBytes(e[:])
	}
	listOff, _ = lb.Finish()
	listCount = len(chains)
	return listOff, listCount, nameBlob, fxIDs, genBlob
}

func readNetworkChains(obj zap.Object, listOff, namePoolOff, fxPoolOff, genPoolOff int) []*NetworkChain {
	list := obj.ListStride(listOff, ncStride)
	n := list.Len()
	if n == 0 {
		return nil
	}
	nameBlob := obj.Bytes(namePoolOff)
	fxPool := obj.ListStride(fxPoolOff, 32)
	genBlob := obj.Bytes(genPoolOff)
	out := make([]*NetworkChain, n)
	for i := 0; i < n; i++ {
		e := list.Object(i, ncStride)
		ch := &NetworkChain{VMID: readID(e, ncVMID)}
		if ns, nl := e.Uint32(ncNameOff), e.Uint32(ncNameLen); nl > 0 && int(ns)+int(nl) <= len(nameBlob) {
			ch.BlockchainName = string(nameBlob[ns : ns+nl])
		}
		ch.FxIDs = sliceIDs(fxPool, e.Uint32(ncFxStart), e.Uint32(ncFxCount))
		if gs, gl := e.Uint32(ncGenOff), e.Uint32(ncGenLen); gl > 0 && int(gs)+int(gl) <= len(genBlob) {
			ch.GenesisData = append([]byte(nil), genBlob[gs:gs+gl]...)
		}
		out[i] = ch
	}
	return out
}

// sliceIDs slices [start, start+count) from a 32-byte-stride id pool.
func sliceIDs(pool zap.List, start, count uint32) []ids.ID {
	total := uint32(pool.Len())
	if count == 0 || start > total || count > total-start {
		return nil
	}
	out := make([]ids.ID, count)
	for i := uint32(0); i < count; i++ {
		out[i] = readID(pool.Object(int(start+i), 32), 0)
	}
	return out
}

// ---- CreateNetworkTx (kindCreateNetwork) ----
const (
	offCN_Parent          = spendSize       // 32B
	offCN_OwnerThreshold  = spendSize + 32  // u32
	offCN_OwnerLocktime   = spendSize + 36  // u64
	offCN_OwnerAddrPtr    = spendSize + 44  // 8B
	offCN_RestakeParent   = spendSize + 52  // u8 (security.Mode axis 1)
	offCN_Admission       = spendSize + 53  // u8 (security.Admission)
	offCN_Manager         = spendSize + 54  // u8 (security.Manager)
	offCN_Threshold       = spendSize + 55  // u64 (Open-admission min stake)
	offCN_Validators      = spendSize + 63  // 8B list
	offCN_ValNodeIDPool   = spendSize + 71  // 8B bytes
	offCN_ValAddrPool     = spendSize + 79  // 8B list
	offCN_Chains          = spendSize + 87  // 8B list
	offCN_ChNamePool      = spendSize + 95  // 8B bytes
	offCN_ChFxPool        = spendSize + 103 // 8B list
	offCN_ChGenPool       = spendSize + 111 // 8B bytes
	offCN_ManagerChainIdx = spendSize + 119 // u32
	offCN_ManagerAddress  = spendSize + 123 // 8B bytes
	sizeCNTx              = spendSize + 131
)

// setSecurity / readSecurity encode a security.Mode across four fixed object
// fields. Shared by CreateNetworkTx and ConvertNetworkTx — one wire encoding.
func setSecurity(ob *zap.ObjectBuilder, offRestake, offAdmission, offManager, offThreshold int, m security.Mode) {
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
	chains []*NetworkChain,
	managerChainIdx uint32,
	managerAddress []byte,
) (*CreateNetworkTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 1024 + sizeCNTx + len(validators)*nvStride + len(chains)*ncStride)
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
	chOff, chCount, nameBlob, fxIDs, genBlob := writeNetworkChains(b, chains)
	fxOff, fxCount := 0, 0
	if len(fxIDs) > 0 {
		flb := b.StartList(32)
		for _, id := range fxIDs {
			flb.AddBytes(id[:])
		}
		fxOff, _ = flb.Finish()
		fxCount = len(fxIDs)
	}

	ob := b.StartObject(sizeCNTx)
	setEnvelope(ob, kindCreateNetwork, base, p)
	setID(ob, offCN_Parent, parent)
	setOwner(ob, offCN_OwnerThreshold, offCN_OwnerLocktime, offCN_OwnerAddrPtr, oThreshold, oLocktime, oAddrOff, oAddrCount)
	setSecurity(ob, offCN_RestakeParent, offCN_Admission, offCN_Manager, offCN_Threshold, sec)
	ob.SetList(offCN_Validators, vdrOff, vdrCount)
	ob.SetBytes(offCN_ValNodeIDPool, nodeIDPool)
	ob.SetList(offCN_ValAddrPool, valAddrOff, valAddrCount)
	ob.SetList(offCN_Chains, chOff, chCount)
	ob.SetBytes(offCN_ChNamePool, nameBlob)
	ob.SetList(offCN_ChFxPool, fxOff, fxCount)
	ob.SetBytes(offCN_ChGenPool, genBlob)
	ob.SetUint32(offCN_ManagerChainIdx, managerChainIdx)
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
func (tx *CreateNetworkTx) Chains() []*NetworkChain {
	return readNetworkChains(tx.root(), offCN_Chains, offCN_ChNamePool, offCN_ChFxPool, offCN_ChGenPool)
}
func (tx *CreateNetworkTx) ManagerChainIdx() uint32 { return tx.root().Uint32(offCN_ManagerChainIdx) }
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
	chains := tx.Chains()
	sec := tx.Security()
	// enum ranges + cross-axis invariant (RestakeParent || own set) live on Mode.
	if err := sec.Valid(); err != nil {
		return err
	}
	// tx-level consistency between the Mode and the genesis validators it carries:
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
	}
	switch {
	case !utils.IsSortedAndUnique(vdrs):
		return ErrValidatorsNotSortedAndUnique
	case len(chains) > MaxNetworkChains:
		return ErrNetworkTooManyChains
	case len(chains) > 0 && int(tx.ManagerChainIdx()) >= len(chains):
		return ErrNetworkManagerIdxOutOfRange
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
	for _, ch := range chains {
		if err := ch.Verify(); err != nil {
			return err
		}
	}
	return nil
}

func (tx *CreateNetworkTx) Visit(visitor Visitor) error { return visitor.CreateNetworkTx(tx) }

var _ = constants.PrimaryNetworkID
