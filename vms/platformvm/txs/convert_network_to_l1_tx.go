// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"errors"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/runtime"
	"github.com/luxfi/utils"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/types"
	"github.com/luxfi/zap"
)

const MaxChainAddressLength = 4096

var (
	_ UnsignedTx                                   = (*ConvertNetworkToL1Tx)(nil)
	_ utils.Sortable[*ConvertNetworkToL1Validator] = (*ConvertNetworkToL1Validator)(nil)

	ErrConvertPermissionlessChain          = errors.New("cannot convert a permissionless chain")
	ErrAddressTooLong                      = errors.New("address is too long")
	ErrConvertMustIncludeValidators        = errors.New("conversion must include at least one validator")
	ErrConvertValidatorsNotSortedAndUnique = errors.New("conversion validators must be sorted and unique")
	ErrZeroWeight                          = errors.New("validator weight must be non-zero")
)

// ConvertNetworkToL1Validator is a plain value record (constructor input +
// accessor output). It is encoded into / decoded from the tx's zap buffer by
// the writeConvertValidators / readConvertValidators helpers below.
type ConvertNetworkToL1Validator struct {
	NodeID                types.JSONByteSlice       `json:"nodeID"`
	Weight                uint64                    `json:"weight"`
	Balance               uint64                    `json:"balance"`
	Signer                signer.ProofOfPossession  `json:"signer"`
	RemainingBalanceOwner message.PChainOwner       `json:"remainingBalanceOwner"`
	DeactivationOwner     message.PChainOwner       `json:"deactivationOwner"`
}

func (v *ConvertNetworkToL1Validator) Compare(o *ConvertNetworkToL1Validator) int {
	return bytes.Compare(v.NodeID, o.NodeID)
}

func (v *ConvertNetworkToL1Validator) Verify() error {
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

// ---- ConvertNetworkToL1Validator list wire ----
//
// Fixed-stride entry (192 bytes); variable NodeID bytes live in a shared byte
// blob, owner addresses in a shared 20-byte-stride pool (each owner slices
// [start, start+count) into it).
const (
	convVdrWeight       = 0   // u64
	convVdrBalance      = 8   // u64
	convVdrSignerPub    = 16  // 48B
	convVdrSignerPoP    = 64  // 96B
	convVdrNodeIDStart  = 160 // u32 (into shared NodeID blob)
	convVdrNodeIDLen    = 164 // u32
	convVdrRemThreshold = 168 // u32
	convVdrRemAddrStart = 172 // u32 (into shared addr pool)
	convVdrRemAddrCount = 176 // u32
	convVdrDeacThresh   = 180 // u32
	convVdrDeacAddrStart = 184 // u32
	convVdrDeacAddrCount = 188 // u32
	convVdrStride       = 192
)

// writeConvertValidators writes the fixed-stride validator list plus the two
// shared pools (NodeID bytes, owner addresses) into the builder, returning the
// list + pool pointers for the parent object.
func writeConvertValidators(b *zap.Builder, vdrs []*ConvertNetworkToL1Validator) (listOff, listCount int, nodeIDs []byte, addrs []ids.ShortID) {
	if len(vdrs) == 0 {
		return 0, 0, nil, nil
	}
	lb := b.StartList(convVdrStride)
	for _, v := range vdrs {
		var e [convVdrStride]byte
		putU64(e[convVdrWeight:], v.Weight)
		putU64(e[convVdrBalance:], v.Balance)
		copy(e[convVdrSignerPub:], v.Signer.PublicKey[:])
		copy(e[convVdrSignerPoP:], v.Signer.ProofOfPossession[:])

		putU32(e[convVdrNodeIDStart:], uint32(len(nodeIDs)))
		putU32(e[convVdrNodeIDLen:], uint32(len(v.NodeID)))
		nodeIDs = append(nodeIDs, v.NodeID...)

		putU32(e[convVdrRemThreshold:], v.RemainingBalanceOwner.Threshold)
		putU32(e[convVdrRemAddrStart:], uint32(len(addrs)))
		putU32(e[convVdrRemAddrCount:], uint32(len(v.RemainingBalanceOwner.Addresses)))
		addrs = append(addrs, v.RemainingBalanceOwner.Addresses...)

		putU32(e[convVdrDeacThresh:], v.DeactivationOwner.Threshold)
		putU32(e[convVdrDeacAddrStart:], uint32(len(addrs)))
		putU32(e[convVdrDeacAddrCount:], uint32(len(v.DeactivationOwner.Addresses)))
		addrs = append(addrs, v.DeactivationOwner.Addresses...)

		lb.AddBytes(e[:])
	}
	listOff, listCount = lb.Finish()
	return listOff, listCount, nodeIDs, addrs
}

// readConvertValidators reconstructs the validator records from the list +
// pools at the given offsets.
func readConvertValidators(obj zap.Object, listOff, nodeIDPoolOff, addrPoolOff int) []*ConvertNetworkToL1Validator {
	list := obj.ListStride(listOff, convVdrStride)
	n := list.Len()
	if n == 0 {
		return nil
	}
	nodeIDBlob := obj.Bytes(nodeIDPoolOff)
	addrs := obj.ListStride(addrPoolOff, addrStride)

	out := make([]*ConvertNetworkToL1Validator, n)
	for i := 0; i < n; i++ {
		e := list.Object(i, convVdrStride)
		v := &ConvertNetworkToL1Validator{
			Weight:  e.Uint64(convVdrWeight),
			Balance: e.Uint64(convVdrBalance),
		}
		copy(v.Signer.PublicKey[:], e.BytesFixedSlice(convVdrSignerPub, 48))
		copy(v.Signer.ProofOfPossession[:], e.BytesFixedSlice(convVdrSignerPoP, 96))

		nStart, nLen := e.Uint32(convVdrNodeIDStart), e.Uint32(convVdrNodeIDLen)
		if nLen > 0 && int(nStart)+int(nLen) <= len(nodeIDBlob) {
			v.NodeID = append([]byte(nil), nodeIDBlob[nStart:nStart+nLen]...)
		}
		v.RemainingBalanceOwner = message.PChainOwner{
			Threshold: e.Uint32(convVdrRemThreshold),
			Addresses: sliceAddrs(addrs, e.Uint32(convVdrRemAddrStart), e.Uint32(convVdrRemAddrCount)),
		}
		v.DeactivationOwner = message.PChainOwner{
			Threshold: e.Uint32(convVdrDeacThresh),
			Addresses: sliceAddrs(addrs, e.Uint32(convVdrDeacAddrStart), e.Uint32(convVdrDeacAddrCount)),
		}
		out[i] = v
	}
	return out
}

// ---- ConvertNetworkToL1Tx (kindConvertNetworkToL1) ----
//
// Envelope + Chain@77 + ManagerChainID@109 + Address(bytes)@141 +
// ValidatorsList@149 + NodeIDPool(bytes)@157 + AddrPool@165 + ChainAuth@173.
const (
	offConv_Chain          = spendSize      // 32B
	offConv_ManagerChainID = spendSize + 32 // 32B
	offConv_Address        = spendSize + 64 // bytes ptr (8B)
	offConv_Validators     = spendSize + 72 // list ptr (8B)
	offConv_NodeIDPool     = spendSize + 80 // bytes ptr (8B)
	offConv_AddrPool       = spendSize + 88 // list ptr (8B)
	offConv_ChainAuth      = spendSize + 96 // sig-idx list ptr (8B)
	sizeConvTx             = spendSize + 104
)

type ConvertNetworkToL1Tx struct {
	spendingTx
}

func NewConvertNetworkToL1Tx(
	base *lux.BaseTx,
	chain, managerChainID ids.ID,
	address []byte,
	validators []*ConvertNetworkToL1Validator,
	chainAuth verify.Verifiable,
) (*ConvertNetworkToL1Tx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 512 + sizeConvTx + len(validators)*convVdrStride)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	vdrOff, vdrCount, nodeIDPool, addrPool := writeConvertValidators(b, validators)
	authOff, authCount, err := writeAuth(b, chainAuth)
	if err != nil {
		return nil, err
	}
	addrOff, addrCount := 0, 0
	if len(addrPool) > 0 {
		alb := b.StartList(addrStride)
		for _, a := range addrPool {
			alb.AddBytes(a[:])
		}
		addrOff, addrCount = alb.Finish()
	}

	ob := b.StartObject(sizeConvTx)
	setEnvelope(ob, kindConvertNetworkToL1, base, p)
	setID(ob, offConv_Chain, chain)
	setID(ob, offConv_ManagerChainID, managerChainID)
	ob.SetBytes(offConv_Address, address)
	ob.SetList(offConv_Validators, vdrOff, vdrCount)
	ob.SetBytes(offConv_NodeIDPool, nodeIDPool)
	ob.SetList(offConv_AddrPool, addrOff, addrCount)
	ob.SetList(offConv_ChainAuth, authOff, authCount)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return &ConvertNetworkToL1Tx{spendingTx{msg: msg}}, nil
}

func (tx *ConvertNetworkToL1Tx) Chain() ids.ID          { return readID(tx.root(), offConv_Chain) }
func (tx *ConvertNetworkToL1Tx) ManagerChainID() ids.ID { return readID(tx.root(), offConv_ManagerChainID) }
func (tx *ConvertNetworkToL1Tx) Address() []byte {
	if a := tx.root().Bytes(offConv_Address); len(a) > 0 {
		return append([]byte(nil), a...)
	}
	return nil
}
func (tx *ConvertNetworkToL1Tx) Validators() []*ConvertNetworkToL1Validator {
	return readConvertValidators(tx.root(), offConv_Validators, offConv_NodeIDPool, offConv_AddrPool)
}
func (tx *ConvertNetworkToL1Tx) ChainAuth() verify.Verifiable {
	return readAuth(tx.root(), offConv_ChainAuth)
}

func (tx *ConvertNetworkToL1Tx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.Chain() == constants.PrimaryNetworkID:
		return ErrConvertPermissionlessChain
	case len(tx.Address()) > MaxChainAddressLength:
		return ErrAddressTooLong
	}
	vdrs := tx.Validators()
	if len(vdrs) == 0 {
		return ErrConvertMustIncludeValidators
	}
	if !utils.IsSortedAndUnique(vdrs) {
		return ErrConvertValidatorsNotSortedAndUnique
	}
	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	for _, vdr := range vdrs {
		if err := vdr.Verify(); err != nil {
			return err
		}
	}
	return tx.ChainAuth().Verify()
}

func (tx *ConvertNetworkToL1Tx) Visit(visitor Visitor) error {
	return visitor.ConvertNetworkToL1Tx(tx)
}
