// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"
	"unicode"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/runtime"
	"github.com/luxfi/util"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/zap"
)

const (
	MaxNameLen    = 128
	MaxGenesisLen = constants.MiB
)

var (
	_ UnsignedTx = (*CreateChainTx)(nil)

	ErrCantValidatePrimaryNetwork = errors.New("new blockchain can't be validated by primary network")

	errInvalidVMID             = errors.New("invalid VM ID")
	errFxIDsNotSortedAndUnique = errors.New("feature extensions IDs must be sorted and unique")
	errNameTooLong             = errors.New("name too long")
	errGenesisTooLong          = errors.New("genesis too long")
	errIllegalNameCharacter    = errors.New("illegal name character")
)

// CreateChainTx creates a blockchain on a chain. The struct IS the wire: it
// embeds the spending envelope and reads its delta fields by offset.
//
// Delta layout (fixed section, after the 77-byte spending envelope):
//
//	ChainID        32B @ 77   (id: chain that validates this blockchain)
//	VMID           32B @ 109  (id: VM running on the new blockchain)
//	BlockchainName 8B  @ 141  (text ptr: human readable name)
//	FxIDs          8B  @ 149  (id list ptr: feature extensions)
//	GenesisData    8B  @ 157  (bytes ptr: genesis state)
//	ChainAuth      8B  @ 165  (auth ptr: sig-index list)
type CreateChainTx struct {
	spendingTx
}

const (
	offCreateChainID      = spendSize // 77
	offCreateChainVMID    = 109
	offCreateChainName    = 141
	offCreateChainFxIDs   = 149
	offCreateChainGenesis = 157
	offCreateChainAuth    = 165
	sizeCreateChain       = 173
)

// NewCreateChainTx builds the tx into a fresh zap buffer.
func NewCreateChainTx(base *lux.BaseTx, chainID ids.ID, blockchainName string, vmID ids.ID, fxIDs []ids.ID, genesisData []byte, chainAuth verify.Verifiable) (*CreateChainTx, error) {
	b := zap.NewBuilder(zap.HeaderSize + 512 + sizeCreateChain)
	p, err := writeSpending(b, base)
	if err != nil {
		return nil, err
	}
	fxOff, fxCount := writeIDList(b, fxIDs)
	authOff, authCount, err := writeAuth(b, chainAuth)
	if err != nil {
		return nil, err
	}

	ob := b.StartObject(sizeCreateChain)
	setEnvelope(ob, kindCreateChain, base, p)
	setID(ob, offCreateChainID, chainID)
	setID(ob, offCreateChainVMID, vmID)
	ob.SetText(offCreateChainName, blockchainName)
	ob.SetList(offCreateChainFxIDs, fxOff, fxCount)
	ob.SetBytes(offCreateChainGenesis, genesisData)
	ob.SetList(offCreateChainAuth, authOff, authCount)
	ob.FinishAsRoot()

	msg, _ := zap.Parse(b.Finish())
	return &CreateChainTx{spendingTx{msg}}, nil
}

// ChainID is the ID of the chain that validates this blockchain (offset read).
func (tx *CreateChainTx) ChainID() ids.ID { return readID(tx.root(), offCreateChainID) }

// VMID is the ID of the VM running on the new blockchain.
func (tx *CreateChainTx) VMID() ids.ID { return readID(tx.root(), offCreateChainVMID) }

// BlockchainName is a human-readable (non-unique) name for the blockchain.
func (tx *CreateChainTx) BlockchainName() string { return tx.root().Text(offCreateChainName) }

// FxIDs are the IDs of the feature extensions running on the new blockchain.
func (tx *CreateChainTx) FxIDs() []ids.ID { return readIDList(tx.root(), offCreateChainFxIDs) }

// GenesisData is the byte representation of the new blockchain's genesis state.
func (tx *CreateChainTx) GenesisData() []byte {
	if g := tx.root().Bytes(offCreateChainGenesis); len(g) > 0 {
		return append([]byte(nil), g...)
	}
	return nil
}

// ChainAuth authorizes this blockchain to be added to the chain.
func (tx *CreateChainTx) ChainAuth() verify.Verifiable {
	return readAuth(tx.root(), offCreateChainAuth)
}

func (tx *CreateChainTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.ChainID() == constants.PrimaryNetworkID:
		return ErrCantValidatePrimaryNetwork
	case len(tx.BlockchainName()) > MaxNameLen:
		return errNameTooLong
	case tx.VMID() == ids.Empty:
		return errInvalidVMID
	case !utils.IsSortedAndUnique(tx.FxIDs()):
		return errFxIDsNotSortedAndUnique
	case len(tx.GenesisData()) > MaxGenesisLen:
		return errGenesisTooLong
	}

	for _, r := range tx.BlockchainName() {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ') {
			return errIllegalNameCharacter
		}
	}

	if err := verifyBaseTx(tx.baseTx(), rt); err != nil {
		return err
	}
	return tx.ChainAuth().Verify()
}

func (tx *CreateChainTx) Visit(visitor Visitor) error {
	return visitor.CreateChainTx(tx)
}

// Initialize is a no-op; the struct is already the wire.
func (tx *CreateChainTx) Initialize(ctx context.Context) error { return nil }
