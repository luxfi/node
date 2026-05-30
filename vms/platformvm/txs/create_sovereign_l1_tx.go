// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"unicode"

	"github.com/luxfi/runtime"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/utils"
	"github.com/luxfi/vm/types"
)

// CreateSovereignL1Tx atomically registers a new sovereign L1 on the
// primary network's P-chain. Combines what is today four sequential
// transactions —
//
//	CreateNetworkTx + AddChainValidatorTx (×N) + CreateChainTx (×K) + ConvertNetworkToL1Tx
//
// into a single tx that commits or reverts atomically. After this tx
// finalises, the primary network has a permanent record of:
//
//   - the L1's network ID (derived from the tx hash, like CreateNetworkTx)
//   - the L1's genesis validator set (NodeIDs + BLS PoPs + weights)
//   - the L1's chain manifest (VM ID + genesis blob per chain, e.g.
//     EVM + DEX + FHE in one L1)
//   - the on-chain validator-manager contract address (the L1's
//     authority for future validator changes)
//
// The primary network does NOT track-chains or validate the L1's
// blocks. The L1's own validators run their own consensus
// independently from genesis. Subsequent validator-set changes flow
// through the on-chain manager via warp messages, not through
// AddChainValidatorTx.
//
// This is the canonical "fresh sovereign L1 from day 1" pattern.
// L2/L3/L4 share security with their parent L1 via the same flow —
// each child L1 references its parent in Owner.
//
// Replaces the 4-step flow for tenants who want sovereign L1 from
// genesis. The 4-step flow remains supported for existing chains that
// began as P-chain-managed and later converted.
type CreateSovereignL1Tx struct {
	// Metadata, inputs and outputs (fee payment, etc.).
	BaseTx `serialize:"true"`

	// Owner of the network registration record. Equivalent to
	// CreateNetworkTx.Owner. Authorises future admin txs against the
	// network record on P-chain (delisting, owner rotation).
	Owner fx.Owner `serialize:"true" json:"owner"`

	// Genesis validator set. These NodeIDs are seeded into the
	// validator-manager contract at L1 genesis; no separate
	// AddPermissionlessValidator calls required. Same shape as
	// ConvertNetworkToL1Validator so the verify/sort plumbing is
	// shared.
	Validators []*ConvertNetworkToL1Validator `serialize:"true" json:"validators"`

	// Chains to register at L1 genesis. Each entry is one chain on
	// the new L1 (e.g. EVM, DEX, FHE). The new L1's network ID is
	// derived from this tx's hash; chains inherit it implicitly.
	Chains []*SovereignL1Chain `serialize:"true" json:"chains"`

	// Index into Chains[] of the chain that hosts the validator
	// manager contract. The L1's validator-set authority lives at
	// (Chains[ManagerChainIdx], ManagerAddress). After this tx
	// commits, all validator changes go through that contract via
	// warp; the primary P-chain no longer participates in the L1's
	// validator set.
	ManagerChainIdx uint32 `serialize:"true" json:"managerChainIdx"`

	// Address of the validator-manager contract on the manager chain.
	// Bounded by MaxChainAddressLength (shared with ConvertNetworkToL1Tx).
	ManagerAddress types.JSONByteSlice `serialize:"true" json:"managerAddress"`
}

// SovereignL1Chain describes one chain on a newly-minted sovereign L1.
// Mirrors the per-chain fields of CreateChainTx; the parent
// CreateSovereignL1Tx supplies the network ID implicitly (derived from
// the tx hash, like CreateNetworkTx).
type SovereignL1Chain struct {
	// Human-readable name; need not be unique. Subject to the same
	// charset rules as CreateChainTx.BlockchainName.
	BlockchainName string `serialize:"true" json:"blockchainName"`

	// VM ID running on this chain (e.g. EVM, DEX, FHE precompile set).
	VMID ids.ID `serialize:"true" json:"vmID"`

	// Feature-extension IDs running on this chain. Must be sorted+unique.
	FxIDs []ids.ID `serialize:"true" json:"fxIDs"`

	// Genesis state blob for this chain. Bounded by MaxGenesisLen.
	GenesisData []byte `serialize:"true" json:"genesisData"`
}

const (
	// MaxSovereignL1Chains caps the per-tx chain count to keep tx size
	// bounded. Most L1s ship 1–5 chains at genesis (EVM/DEX/FHE/etc.).
	MaxSovereignL1Chains = 16
)

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

// InitRuntime sets the runtime context on the inputs and outputs so
// addresses can be JSON-marshalled into human-readable form.
func (tx *CreateSovereignL1Tx) InitRuntime(rt *runtime.Runtime) {
	tx.BaseTx.InitRuntime(rt)
	// Owner does not have InitRuntime; matches CreateNetworkTx.
}

// SyntacticVerify checks shape-only invariants. State-dependent checks
// (input UTXOs spendable, owner sigs valid, balance funds the genesis
// validators' initial Balance, etc.) live in the StandardTx executor.
func (tx *CreateSovereignL1Tx) SyntacticVerify(rt *runtime.Runtime) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		return nil
	case len(tx.Validators) == 0:
		return ErrSovereignMustIncludeValidators
	case !utils.IsSortedAndUnique(tx.Validators):
		return ErrConvertValidatorsNotSortedAndUnique
	case len(tx.Chains) == 0:
		return ErrSovereignMustIncludeChains
	case len(tx.Chains) > MaxSovereignL1Chains:
		return ErrSovereignTooManyChains
	case int(tx.ManagerChainIdx) >= len(tx.Chains):
		return ErrSovereignManagerIdxOutOfRange
	case len(tx.ManagerAddress) > MaxChainAddressLength:
		return ErrAddressTooLong
	}

	if err := tx.BaseTx.SyntacticVerify(rt); err != nil {
		return err
	}
	if err := tx.Owner.Verify(); err != nil {
		return err
	}
	for _, vdr := range tx.Validators {
		if err := vdr.Verify(); err != nil {
			return err
		}
	}
	for _, ch := range tx.Chains {
		if err := ch.Verify(); err != nil {
			return err
		}
	}

	tx.SyntacticallyVerified = true
	return nil
}

// Visit dispatches to the per-tx-type Visitor (StandardTx executor,
// mempool verifier, etc.). The matching visitor.go change adds the
// CreateSovereignL1Tx method to the interface.
func (tx *CreateSovereignL1Tx) Visit(visitor Visitor) error {
	return visitor.CreateSovereignL1Tx(tx)
}

// Verify validates a single SovereignL1Chain entry.
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

// PrimaryNetworkSafety: the tx cannot register the primary network
// itself. The new network ID is derived from the tx hash exactly like
// CreateNetworkTx — a new ids.ID, never constants.PrimaryNetworkID.
// Executors must enforce `derivedNetworkID != constants.PrimaryNetworkID`
// and reject otherwise. State-dependent check, lives in the executor.
var _ = constants.PrimaryNetworkID
