// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/luxfi/runtime"
	"github.com/luxfi/utils"
	"github.com/luxfi/utxo/secp256k1fx"
)

// CreateAssetTx semantics for the P-only primary network.
//
// PlatformVM uses a single Fx (secp256k1fx). InitialState therefore drops the
// FxIndex/Fx-slice indirection that XVM needs and only carries the initial
// outputs that should be minted for the new asset. The on-the-wire layout
// matches `vms/xvm/txs/create_asset_tx.go` so cross-VM atomic UTXOs and
// historical wallet code see an identical byte shape.
const (
	minNameLen      = 1
	maxNameLen      = 128
	minSymbolLen    = 1
	maxSymbolLen    = 4
	maxDenomination = 32
)

var (
	_ UnsignedTx             = (*CreateAssetTx)(nil)
	_ secp256k1fx.UnsignedTx = (*CreateAssetTx)(nil)

	ErrNilCreateAssetTx       = errors.New("create asset tx is nil")
	ErrNameTooShort           = fmt.Errorf("asset name is too short, minimum size is %d", minNameLen)
	ErrNameTooLong            = fmt.Errorf("asset name is too long, maximum size is %d", maxNameLen)
	ErrSymbolTooShort         = fmt.Errorf("asset symbol is too short, minimum size is %d", minSymbolLen)
	ErrSymbolTooLong          = fmt.Errorf("asset symbol is too long, maximum size is %d", maxSymbolLen)
	ErrNoInitialStates        = errors.New("asset must declare at least one initial state output")
	ErrIllegalNameChar        = errors.New("asset name must be made up of only letters, numbers and spaces")
	ErrIllegalSymbolChar      = errors.New("asset symbol must be all upper-case letters")
	ErrUnexpectedWhitespace   = errors.New("unexpected whitespace in asset name")
	ErrDenominationTooLarge   = errors.New("denomination is too large")
	ErrInitialStatesNotSorted = errors.New("initial states are not sorted and unique")
)

// CreateAssetTx mints a brand new UTXO asset on the P-Chain. The minted asset
// is owned by the outputs declared in [States]; the fee for asset creation is
// paid out of [BaseTx.Ins] / [BaseTx.Outs].
type CreateAssetTx struct {
	BaseTx       `serialize:"true"`
	Name         string          `serialize:"true" json:"name"`
	Symbol       string          `serialize:"true" json:"symbol"`
	Denomination byte            `serialize:"true" json:"denomination"`
	States       []*InitialState `serialize:"true" json:"initialStates"`
}

// InitRuntime initialises the embedded BaseTx and every InitialState output.
func (tx *CreateAssetTx) InitRuntime(rt *runtime.Runtime) {
	tx.BaseTx.InitRuntime(rt)
	for _, state := range tx.States {
		state.InitRuntime(rt)
	}
}

// InitialStates returns the slice of initial states for this asset. The
// returned slice MUST NOT be mutated.
func (tx *CreateAssetTx) InitialStates() []*InitialState {
	return tx.States
}

// SyntacticVerify returns nil iff this tx is well formed.
func (tx *CreateAssetTx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilCreateAssetTx
	case tx.SyntacticallyVerified:
		return nil
	case len(tx.Name) < minNameLen:
		return ErrNameTooShort
	case len(tx.Name) > maxNameLen:
		return ErrNameTooLong
	case len(tx.Symbol) < minSymbolLen:
		return ErrSymbolTooShort
	case len(tx.Symbol) > maxSymbolLen:
		return ErrSymbolTooLong
	case len(tx.States) == 0:
		return ErrNoInitialStates
	case tx.Denomination > maxDenomination:
		return ErrDenominationTooLarge
	case strings.TrimSpace(tx.Name) != tx.Name:
		return ErrUnexpectedWhitespace
	}

	for _, r := range tx.Name {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ') {
			return ErrIllegalNameChar
		}
	}
	for _, r := range tx.Symbol {
		if r > unicode.MaxASCII || !unicode.IsUpper(r) {
			return ErrIllegalSymbolChar
		}
	}

	if err := tx.BaseTx.SyntacticVerify(rt); err != nil {
		return err
	}

	for _, state := range tx.States {
		if err := state.Verify(Codec); err != nil {
			return fmt.Errorf("initial state failed verification: %w", err)
		}
	}
	if !utils.IsSortedAndUnique(tx.States) {
		return ErrInitialStatesNotSorted
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *CreateAssetTx) Visit(v Visitor) error {
	return v.CreateAssetTx(tx)
}
