// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"fmt"

	"github.com/luxfi/ids"
)

// Verify methods enforce executor-side semantic gates for every zap_native
// tx type that embeds Owner / OwnerStub fields. The wire parser (parseAndCheckKind)
// is intentionally permissive — it confirms TxKind + buffer geometry only.
// Semantic gates live HERE so the parser stays pure and the consumer-side
// boundary is one and only one method per tx type.
//
// LP-023 Red round 4 R4V7: executor MUST call tx.Verify() before treating
// any embedded Owner as authoritative. Skipping Verify() opens an
// authorization bypass when the wire-encoded threshold == 0 or threshold
// > len(addrs).
//
// Contract: each Verify() returns a wrapped typed error from
// {ErrOwnerThresholdZero, ErrOwnerThresholdExceedsAddrs, ErrOwnerAddrsEmpty}.
// errors.Is on the typed error MUST match.

// stubFromTuple reconstructs an OwnerStub from the (threshold, locktime,
// address) tuple returned by the embedded-stub accessors. The wire layer
// holds these fields inline in the parent fixed section, so reconstruction
// is a pure value-copy.
func stubFromTuple(threshold uint32, locktime uint64, address ids.ShortID) OwnerStub {
	return OwnerStub{Threshold: threshold, Locktime: locktime, Address: address}
}

// Verify runs SyntacticVerify on the embedded RewardsOwner of an
// AddValidatorTx. Wraps the typed error with the tx kind for context.
func (t AddValidatorTx) Verify() error {
	o := stubFromTuple(t.RewardsOwner())
	if err := o.SyntacticVerify(); err != nil {
		return fmt.Errorf("AddValidatorTx.RewardsOwner: %w", err)
	}
	return nil
}

// Verify runs SyntacticVerify on the embedded DelegationRewardsOwner of an
// AddDelegatorTx.
func (t AddDelegatorTx) Verify() error {
	o := stubFromTuple(t.DelegationRewardsOwner())
	if err := o.SyntacticVerify(); err != nil {
		return fmt.Errorf("AddDelegatorTx.DelegationRewardsOwner: %w", err)
	}
	return nil
}

// Verify runs SyntacticVerify on both embedded owners of an
// AddPermissionlessValidatorTx — the ValidationRewardsOwner AND the
// DelegationRewardsOwner. Either malformed owner fails the whole tx.
func (t AddPermissionlessValidatorTx) Verify() error {
	v := stubFromTuple(t.ValidationRewardsOwner())
	if err := v.SyntacticVerify(); err != nil {
		return fmt.Errorf("AddPermissionlessValidatorTx.ValidationRewardsOwner: %w", err)
	}
	d := stubFromTuple(t.DelegationRewardsOwner())
	if err := d.SyntacticVerify(); err != nil {
		return fmt.Errorf("AddPermissionlessValidatorTx.DelegationRewardsOwner: %w", err)
	}
	return nil
}

// Verify runs SyntacticVerify on the embedded DelegationRewardsOwner of an
// AddPermissionlessDelegatorTx.
func (t AddPermissionlessDelegatorTx) Verify() error {
	o := stubFromTuple(t.DelegationRewardsOwner())
	if err := o.SyntacticVerify(); err != nil {
		return fmt.Errorf("AddPermissionlessDelegatorTx.DelegationRewardsOwner: %w", err)
	}
	return nil
}

// Verify runs SyntacticVerify on the embedded Owner of a CreateChainTx.
func (t CreateChainTx) Verify() error {
	o := stubFromTuple(t.Owner())
	if err := o.SyntacticVerify(); err != nil {
		return fmt.Errorf("CreateChainTx.Owner: %w", err)
	}
	return nil
}

// Verify runs SyntacticVerify on the embedded Owner of a CreateNetworkTx.
func (t CreateNetworkTx) Verify() error {
	o := stubFromTuple(t.Owner())
	if err := o.SyntacticVerify(); err != nil {
		return fmt.Errorf("CreateNetworkTx.Owner: %w", err)
	}
	return nil
}

// Verify runs SyntacticVerify on the embedded Owner of a
// CreateSovereignL1Tx.
func (t CreateSovereignL1Tx) Verify() error {
	o := stubFromTuple(t.Owner())
	if err := o.SyntacticVerify(); err != nil {
		return fmt.Errorf("CreateSovereignL1Tx.Owner: %w", err)
	}
	return nil
}
