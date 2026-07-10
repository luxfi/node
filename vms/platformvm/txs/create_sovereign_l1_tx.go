// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"unicode"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/runtime"
	"github.com/luxfi/utils"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

// CreateSovereignL1Tx atomically registers a new sovereign L1 on the P-chain —
// the one-step alternative to CreateNetworkTx + AddChainValidatorTx(×N) +
// CreateChainTx(×K) + ConvertNetworkToL1Tx. The new L1's network ID is derived
// from this tx's hash; the primary network keeps a permanent record of the
// owner, the genesis validator set (NodeIDs + BLS PoPs + weights), the chain
// manifest, and the on-chain manager contract. The primary network does not
// track or validate the L1's blocks.
type CreateSovereignL1Tx struct {
	spendingTx
}

// SovereignL1Chain describes one chain on a newly-minted sovereign L1 (a plain
// value record; encoded into / decoded from the tx's zap buffer).
type SovereignL1Chain struct {
	BlockchainName string   `json:"blockchainName"`
	VMID           ids.ID   `json:"vmID"`
	FxIDs          []ids.ID `json:"fxIDs"`
	GenesisData    []byte   `json:"genesisData"`
}

const MaxSovereignL1Chains = 16

var (
	_ UnsignedTx = (*CreateSovereignL1Tx)(nil)

	ErrSovereignMustIncludeValidators = errors.New("sovereign L1 must include at least one validator")
	ErrSovereignMustIncludeChains     = errors.New("sovereign L1 must include at least one chain")
	ErrSovereignTooManyChains         = errors.New("sovereign L1 exceeds MaxSovereignL1Chains")
	ErrSovereignManagerIdxOutOfRange  = errors.New("managerChainIdx is out of range for chains[]")
	ErrSovereignChainNameTooLong      = errors.New("sovereign L1 chain name exceeds MaxNameLen")
	ErrSovereignChainNameIllegal      = errors.New("sovereign L1 chain name contains illegal characters")
	ErrSovereignVMIDEmpty             = errors.New("sovereign L1 chain VMID must not be empty")
	ErrSovereignFxIDsNotSorted        = errors.New("sovereign L1 chain FxIDs must be sorted and unique")
	ErrSovereignGenesisTooLong        = errors.New("sovereign L1 chain genesis exceeds MaxGenesisLen")
)

func (ch *SovereignL1Chain) Verify() error {
	if len(ch.BlockchainName) > MaxNameLen {
		return ErrSovereignChainNameTooLong
	}
	for _, r := range ch.BlockchainName {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ') {
			return ErrSovereignChainNameIllegal
		}
	}
	if ch.VMID == ids.Empty {
		return ErrSovereignVMIDEmpty
	}
	if !utils.IsSortedAndUnique(ch.FxIDs) {
		return ErrSovereignFxIDsNotSorted
	}
	if len(ch.GenesisData) > MaxGenesisLen {
		return ErrSovereignGenesisTooLong
	}
	return nil
}

// ---- SovereignL1Chain list wire ----
//
// Fixed-stride entry (56 bytes); name/genesis bytes live in shared blobs,
// FxIDs in a shared 32-byte-stride pool.
const (
	sovChVMID     = 0  // 32B
	sovChNameOff  = 32 // u32 (into name blob)
	sovChNameLen  = 36 // u32
	sovChFxStart  = 40 // u32 (into fx pool)
	sovChFxCount  = 44 // u32
	sovChGenOff   = 48 // u32 (into genesis blob)
	sovChGenLen   = 52 // u32
	sovChStride   = 56
)

func writeSovereignChains(b *zap.Builder, chains []*SovereignL1Chain) (listOff, listCount int, nameBlob []byte, fxIDs []ids.ID, genBlob []byte) {
	if len(chains) == 0 {
		return 0, 0, nil, nil, nil
	}
	lb := b.StartList(sovChStride)
	for _, ch := range chains {
		var e [sovChStride]byte
		copy(e[sovChVMID:], ch.VMID[:])
		putU32(e[sovChNameOff:], uint32(len(nameBlob)))
		putU32(e[sovChNameLen:], uint32(len(ch.BlockchainName)))
		nameBlob = append(nameBlob, ch.BlockchainName...)
		putU32(e[sovChFxStart:], uint32(len(fxIDs)))
		putU32(e[sovChFxCount:], uint32(len(ch.FxIDs)))
		fxIDs = append(fxIDs, ch.FxIDs...)
		putU32(e[sovChGenOff:], uint32(len(genBlob)))
		putU32(e[sovChGenLen:], uint32(len(ch.GenesisData)))
		genBlob = append(genBlob, ch.GenesisData...)
		lb.AddBytes(e[:])
	}
	listOff, listCount = lb.Finish()
	return listOff, listCount, nameBlob, fxIDs, genBlob
}

func readSovereignChains(obj zap.Object, listOff, namePoolOff, fxPoolOff, genPoolOff int) []*SovereignL1Chain {
	list := obj.ListStride(listOff, sovChStride)
	n := list.Len()
	if n == 0 {
		return nil
	}
	nameBlob := obj.Bytes(namePoolOff)
	fxPool := obj.ListStride(fxPoolOff, 32)
	genBlob := obj.Bytes(genPoolOff)

	out := make([]*SovereignL1Chain, n)
	for i := 0; i < n; i++ {
		e := list.Object(i, sovChStride)
		ch := &SovereignL1Chain{VMID: readID(e, sovChVMID)}
		if ns, nl := e.Uint32(sovChNameOff), e.Uint32(sovChNameLen); nl > 0 && int(ns)+int(nl) <= len(nameBlob) {
			ch.BlockchainName = string(nameBlob[ns : ns+nl])
		}
		ch.FxIDs = sliceIDs(fxPool, e.Uint32(sovChFxStart), e.Uint32(sovChFxCount))
		if gs, gl := e.Uint32(sovChGenOff), e.Uint32(sovChGenLen); gl > 0 && int(gs)+int(gl) <= len(genBlob) {
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

// ---- CreateSovereignL1Tx (kindCreateSovereignL1) ----
const (
	offSov_OwnerThreshold = spendSize      // u32
	offSov_OwnerLocktime  = spendSize + 4  // u64
	offSov_OwnerAddrPtr   = spendSize + 12 // 8B
	offSov_Validators     = spendSize + 20 // 8B list
	offSov_ConvNodeIDPool = spendSize + 28 // 8B bytes
	offSov_ConvAddrPool   = spendSize + 36 // 8B list
	offSov_Chains         = spendSize + 44 // 8B list
	offSov_ChainNamePool  = spendSize + 52 // 8B bytes
	offSov_ChainFxPool    = spendSize + 60 // 8B list
	offSov_ChainGenPool   = spendSize + 68 // 8B bytes
	offSov_ManagerIdx     = spendSize + 76 // u32
	offSov_ManagerAddress = spendSize + 80 // 8B bytes
	sizeSovTx             = spendSize + 88
)

func NewCreateSovereignL1Tx(
	base *lux.BaseTx,
	owner fx.Owner,
	validators []*ConvertNetworkToL1Validator,
	chains []*SovereignL1Chain,
	managerChainIdx uint32,
	managerAddress []byte,
) (*CreateSovereignL1Tx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 1024 + sizeSovTx + len(validators)*convVdrStride + len(chains)*sovChStride)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	oThreshold, oLocktime, oAddrOff, oAddrCount, err := writeOwner(b, owner)
	if err != nil {
		return nil, err
	}
	vdrOff, vdrCount, nodeIDPool, addrPool := writeConvertValidators(b, validators)
	convAddrOff, convAddrCount := 0, 0
	if len(addrPool) > 0 {
		alb := b.StartList(addrStride)
		for _, a := range addrPool {
			alb.AddBytes(a[:])
		}
		convAddrOff, convAddrCount = alb.Finish()
	}
	chOff, chCount, nameBlob, fxIDs, genBlob := writeSovereignChains(b, chains)
	fxOff, fxCount := 0, 0
	if len(fxIDs) > 0 {
		flb := b.StartList(32)
		for _, id := range fxIDs {
			flb.AddBytes(id[:])
		}
		fxOff, fxCount = flb.Finish()
	}

	ob := b.StartObject(sizeSovTx)
	setEnvelope(ob, kindCreateSovereignL1, base, p)
	setOwner(ob, offSov_OwnerThreshold, offSov_OwnerLocktime, offSov_OwnerAddrPtr, oThreshold, oLocktime, oAddrOff, oAddrCount)
	ob.SetList(offSov_Validators, vdrOff, vdrCount)
	ob.SetBytes(offSov_ConvNodeIDPool, nodeIDPool)
	ob.SetList(offSov_ConvAddrPool, convAddrOff, convAddrCount)
	ob.SetList(offSov_Chains, chOff, chCount)
	ob.SetBytes(offSov_ChainNamePool, nameBlob)
	ob.SetList(offSov_ChainFxPool, fxOff, fxCount)
	ob.SetBytes(offSov_ChainGenPool, genBlob)
	ob.SetUint32(offSov_ManagerIdx, managerChainIdx)
	ob.SetBytes(offSov_ManagerAddress, managerAddress)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return &CreateSovereignL1Tx{spendingTx{msg: msg}}, nil
}

func (tx *CreateSovereignL1Tx) Owner() fx.Owner {
	return readOwner(tx.root(), offSov_OwnerThreshold, offSov_OwnerLocktime, offSov_OwnerAddrPtr)
}
func (tx *CreateSovereignL1Tx) Validators() []*ConvertNetworkToL1Validator {
	return readConvertValidators(tx.root(), offSov_Validators, offSov_ConvNodeIDPool, offSov_ConvAddrPool)
}
func (tx *CreateSovereignL1Tx) Chains() []*SovereignL1Chain {
	return readSovereignChains(tx.root(), offSov_Chains, offSov_ChainNamePool, offSov_ChainFxPool, offSov_ChainGenPool)
}
func (tx *CreateSovereignL1Tx) ManagerChainIdx() uint32 { return tx.root().Uint32(offSov_ManagerIdx) }
func (tx *CreateSovereignL1Tx) ManagerAddress() []byte {
	if a := tx.root().Bytes(offSov_ManagerAddress); len(a) > 0 {
		return append([]byte(nil), a...)
	}
	return nil
}

func (tx *CreateSovereignL1Tx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	vdrs := tx.Validators()
	chains := tx.Chains()
	switch {
	case len(vdrs) == 0:
		return ErrSovereignMustIncludeValidators
	case !utils.IsSortedAndUnique(vdrs):
		return ErrConvertValidatorsNotSortedAndUnique
	case len(chains) == 0:
		return ErrSovereignMustIncludeChains
	case len(chains) > MaxSovereignL1Chains:
		return ErrSovereignTooManyChains
	case int(tx.ManagerChainIdx()) >= len(chains):
		return ErrSovereignManagerIdxOutOfRange
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

func (tx *CreateSovereignL1Tx) Visit(visitor Visitor) error {
	return visitor.CreateSovereignL1Tx(tx)
}
