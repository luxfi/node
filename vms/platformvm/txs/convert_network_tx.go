// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/runtime"
	"github.com/luxfi/util"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

// ConvertNetworkTx PROMOTES an existing network: it changes the network's
// security from inherited (restaked from parent) to sovereign (its own
// validator set + on-chain manager) and re-anchors its Parent. This is the
// endomorphism `Network → Network` — distinct from CreateNetworkTx (the
// `∅ → Network` constructor). Examples: an L2 (parent = an L1, inherited)
// converts to an L1 (parent = Primary, sovereign); an L3 promotes to L1.
//
// Requires the existing network owner's authorization (Auth). The genesis
// validator set is established here (shared NetworkValidator component); the
// manager lives on an existing chain (ManagerChainID) of the network.
type ConvertNetworkTx struct {
	spendingTx
}

var (
	_ UnsignedTx = (*ConvertNetworkTx)(nil)

	ErrConvertPrimaryNetwork      = errors.New("cannot convert the primary network")
	ErrConvertMustHaveValidators  = errors.New("conversion must establish at least one validator")
	ErrConvertMustEstablishOwnSet = errors.New("conversion must establish an own validator set (sovereign mode)")
)

const (
	offCV_Network        = spendSize       // 32B — the network being promoted
	offCV_Parent         = spendSize + 32  // 32B — new parent (Primary ⇒ L1)
	offCV_ManagerChainID = spendSize + 64  // 32B — existing chain hosting manager
	offCV_ManagerAddress = spendSize + 96  // bytes ptr (8B)
	offCV_Validators     = spendSize + 104 // list ptr (8B)
	offCV_ValNodeIDPool  = spendSize + 112 // bytes ptr (8B)
	offCV_ValAddrPool    = spendSize + 120 // list ptr (8B)
	offCV_AuthPtr        = spendSize + 128 // sig-idx list ptr (8B)
	offCV_RestakeParent  = spendSize + 136 // u8 (security.Mode axis 1)
	offCV_Admission      = spendSize + 137 // u8 (security.Admission)
	offCV_Manager        = spendSize + 138 // u8 (security.Manager)
	offCV_Threshold      = spendSize + 139 // u64 (Open-admission min stake)
	sizeCVTx             = spendSize + 147
)

func NewConvertNetworkTx(
	base *lux.BaseTx,
	network, parent, managerChainID ids.ID,
	sec security.Mode,
	managerAddress []byte,
	validators []*NetworkValidator,
	auth verify.Verifiable,
) (*ConvertNetworkTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 512 + sizeCVTx + len(validators)*nvStride)
	p, err := writeSpending(b, base)
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
	authOff, authCount, err := writeAuth(b, auth)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(sizeCVTx)
	setEnvelope(ob, kindConvertNetwork, base, p)
	setID(ob, offCV_Network, network)
	setID(ob, offCV_Parent, parent)
	setID(ob, offCV_ManagerChainID, managerChainID)
	ob.SetBytes(offCV_ManagerAddress, managerAddress)
	ob.SetList(offCV_Validators, vdrOff, vdrCount)
	ob.SetBytes(offCV_ValNodeIDPool, nodeIDPool)
	ob.SetList(offCV_ValAddrPool, valAddrOff, valAddrCount)
	ob.SetList(offCV_AuthPtr, authOff, authCount)
	setSecurity(ob, offCV_RestakeParent, offCV_Admission, offCV_Manager, offCV_Threshold, sec)
	ob.FinishAsRoot()
	msg, _ := zap.Parse(b.Finish())
	return &ConvertNetworkTx{spendingTx{msg: msg}}, nil
}

func (tx *ConvertNetworkTx) Network() ids.ID        { return readID(tx.root(), offCV_Network) }
func (tx *ConvertNetworkTx) Parent() ids.ID         { return readID(tx.root(), offCV_Parent) }
func (tx *ConvertNetworkTx) ManagerChainID() ids.ID { return readID(tx.root(), offCV_ManagerChainID) }
func (tx *ConvertNetworkTx) ManagerAddress() []byte {
	if a := tx.root().Bytes(offCV_ManagerAddress); len(a) > 0 {
		return append([]byte(nil), a...)
	}
	return nil
}
func (tx *ConvertNetworkTx) Validators() []*NetworkValidator {
	return readNetworkValidators(tx.root(), offCV_Validators, offCV_ValNodeIDPool, offCV_ValAddrPool)
}
func (tx *ConvertNetworkTx) Auth() verify.Verifiable { return readAuth(tx.root(), offCV_AuthPtr) }
func (tx *ConvertNetworkTx) Security() security.Mode {
	return readSecurity(tx.root(), offCV_RestakeParent, offCV_Admission, offCV_Manager, offCV_Threshold)
}
func (tx *ConvertNetworkTx) Sovereign() bool { return tx.Security().Sovereign() }

func (tx *ConvertNetworkTx) SyntacticVerify(rt *runtime.Runtime) error {
	if tx == nil {
		return ErrNilTx
	}
	vdrs := tx.Validators()
	sec := tx.Security()
	switch {
	case tx.Network() == constants.PrimaryNetworkID:
		return ErrConvertPrimaryNetwork
	case len(vdrs) == 0:
		return ErrConvertMustHaveValidators
	case !utils.IsSortedAndUnique(vdrs):
		return ErrValidatorsNotSortedAndUnique
	case len(tx.ManagerAddress()) > MaxChainAddressLength:
		return ErrAddressTooLong
	}
	// promotion establishes an own set; the target Mode must be valid + sovereign.
	if err := sec.Valid(); err != nil {
		return err
	}
	if !sec.Sovereign() {
		return ErrConvertMustEstablishOwnSet
	}
	if sec.Manager == security.Contract && len(tx.ManagerAddress()) == 0 {
		return ErrContractManagerNeedsAddress
	}
	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	for _, vdr := range vdrs {
		if err := vdr.Verify(); err != nil {
			return err
		}
	}
	return tx.Auth().Verify()
}

func (tx *ConvertNetworkTx) Visit(visitor Visitor) error { return visitor.ConvertNetworkTx(tx) }
