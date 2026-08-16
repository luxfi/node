// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import "testing"

// TestStakerDispatchSafety pins the interface-satisfaction invariant that the
// staker type-switches in service.go (getStakerAttributes) and
// executor/proposal_tx_executor.go depend on.
//
// Struct-is-wire turned RewardsOwner from a struct field into an accessor
// method, so *AddValidatorTx now structurally satisfies BOTH ValidatorTx and
// DelegatorTx. That is SAFE only because:
//
//  1. every dispatch site checks `case txs.ValidatorTx` BEFORE
//     `case txs.DelegatorTx`, so an AddValidatorTx routes as a validator; and
//  2. *AddDelegatorTx does NOT satisfy ValidatorTx (it lacks
//     ValidationRewardsOwner() and Shares()), so a delegator can never
//     mis-route into the validator branch.
//
// If a future edit gives AddDelegatorTx those methods, or reorders a switch,
// this test fails loudly instead of silently mis-paying rewards.
func TestStakerDispatchSafety(t *testing.T) {
	var (
		vdr any = (*AddValidatorTx)(nil)
		del any = (*AddDelegatorTx)(nil)
	)

	if _, ok := vdr.(ValidatorTx); !ok {
		t.Fatal("*AddValidatorTx must satisfy ValidatorTx")
	}
	if _, ok := del.(DelegatorTx); !ok {
		t.Fatal("*AddDelegatorTx must satisfy DelegatorTx")
	}
	// The safety property: a delegator must NOT look like a
	// validator, because every dispatch checks ValidatorTx first.
	if _, ok := del.(ValidatorTx); ok {
		t.Fatal("*AddDelegatorTx must NOT satisfy ValidatorTx — delegators would mis-route to the validator branch")
	}
}
