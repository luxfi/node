// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package zap_native

import (
	"errors"
	"fmt"

	"github.com/luxfi/crypto/bls"
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
// {ErrOwnerThresholdZero, ErrOwnerThresholdExceedsAddrs, ErrOwnerAddrsEmpty,
// ErrZeroValidators, ErrZeroChains, ErrValidatorWeightZero, ErrBadBLSPoP,
// ErrMalformedFxIDsLen}. errors.Is on the typed error MUST match.
//
// LP-023 Red round 6 closes:
//   - R6V4 (CRITICAL): CreateSovereignL1Tx.Verify must reject zero-validator
//     and zero-chain wire buffers — both would create an L1 that halts at
//     activation. Per-validator Weight > 0 walk also fires here.
//   - R6V8 (HIGH): TransferChainOwnershipTx.Verify was missing entirely
//     despite the tx carrying Owner fields. Wire it into the same Verify()
//     contract every other Owner-bearing tx uses.
//   - R6V3 (HIGH): BLS proof-of-possession must be verified for every
//     initial validator at the SyntacticVerify boundary. Wire layer returns
//     opaque 48B pubkey + 96B PoP; no downstream consumer was calling
//     bls.VerifyProofOfPossession. Wired into CreateSovereignL1Tx.Verify
//     so the pairing check fires once per validator on the executor path.
//   - R6V5 (MEDIUM): ChainsListView.Verify walks entries and rejects any
//     entry whose FxIDsLen is not a multiple of FxIDSize. Silent-nil
//     return from BoundChainEntry.FxIDs would otherwise treat a malformed
//     length as "no FxIDs" and silently accept the tx.

// R6V4 + R6V3 + R6V5 typed errors. Each is wrapped by CreateSovereignL1Tx.Verify
// with tx-kind context; consumers match via errors.Is.
var (
	// ErrZeroValidators is returned when the initial-validator set of a
	// CreateSovereignL1Tx is empty. A zero-validator L1 halts consensus
	// at activation (no quorum can ever be reached). Reject at the wire
	// boundary, not at chain bring-up time.
	ErrZeroValidators = errors.New(
		"zap_native: Validators list is empty — L1 cannot bootstrap consensus",
	)

	// ErrZeroChains is returned when the chains-to-create list of a
	// CreateSovereignL1Tx is empty. An L1 with no chains has no surface;
	// the tx commits no state. Reject as malformed.
	ErrZeroChains = errors.New(
		"zap_native: Chains list is empty — L1 has no chains to create",
	)

	// ErrValidatorWeightZero is returned when any validator entry has a
	// stake weight of zero. A zero-weight validator contributes nothing
	// to quorum, so it is either a constructor mistake or a deliberate
	// quorum-skewing attack (filler entries that pad the count but do
	// not contribute to the threshold).
	ErrValidatorWeightZero = errors.New(
		"zap_native: validator Weight must be > 0; zero-weight validator skews quorum",
	)

	// ErrBadBLSPoP is returned when any validator entry's BLS
	// proof-of-possession fails to verify against the embedded BLS
	// public key. The pairing check binds the BLS keypair to the
	// pubkey value; without it an adversary could substitute an
	// arbitrary pubkey/PoP pair and seize validator authority on the
	// new L1 at registration time. Wire layer is opaque about pairing
	// validity; this gate is the only place it fires before the
	// executor commits the validator set.
	ErrBadBLSPoP = errors.New(
		"zap_native: validator BLSPoP failed pairing verification",
	)

	// ErrMalformedFxIDsLen is returned when a chain entry's FxIDsLen is
	// not an exact multiple of FxIDSize. The wire layer's
	// BoundChainEntry.FxIDs silently returns nil for this case
	// (fail-closed at access time), but a downstream consumer that
	// looks at .FxIDsRange() length directly — or treats the empty
	// slice as "no FxIDs allowed" — could be coerced into accepting a
	// tx with a malformed fx-id geometry. Reject at the wire boundary.
	ErrMalformedFxIDsLen = errors.New(
		"zap_native: ChainEntry.FxIDsLen must be an exact multiple of FxIDSize (32)",
	)
)

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

// Verify runs SyntacticVerify on the embedded Owner of a CreateSovereignL1Tx
// AND enforces R6V4 / R6V3 / R6V5: the initial validator set must be
// non-empty, every validator must have non-zero weight, every validator's
// BLS PoP must verify, the chains list must be non-empty, and every chain
// entry's FxIDsLen must be an exact multiple of FxIDSize.
//
// Notes:
//   - Per-validator RegistrationExpiry > now() is intentionally NOT enforced
//     here. SyntacticVerify is clock-independent (executor wall-clock lives
//     in the staking handler), so the expiry check lives there. This file
//     enforces only properties that are invariant under wire encoding.
//   - The BLS pairing check is fast enough at the SyntacticVerify boundary
//     for small validator lists (< 100). For large lists the cost is
//     O(n) pairings, still well under the executor budget.
func (t CreateSovereignL1Tx) Verify() error {
	o := stubFromTuple(t.Owner())
	if err := o.SyntacticVerify(); err != nil {
		return fmt.Errorf("CreateSovereignL1Tx.Owner: %w", err)
	}

	// R6V4: non-empty validators + per-validator weight > 0.
	// R6V3: BLS PoP verification per validator.
	vals := t.Validators()
	n := vals.Len()
	if n == 0 {
		return fmt.Errorf("CreateSovereignL1Tx.Validators: %w", ErrZeroValidators)
	}
	for i := 0; i < n; i++ {
		rec := vals.At(i)
		if rec.Weight() == 0 {
			return fmt.Errorf(
				"CreateSovereignL1Tx.Validators[%d].Weight: %w", i, ErrValidatorWeightZero,
			)
		}
		// BLS PoP gate — R6V3.
		pkBytes := rec.BLSPubKey()
		sigBytes := rec.BLSPoP()
		pk, err := bls.PublicKeyFromCompressedBytes(pkBytes)
		if err != nil {
			return fmt.Errorf(
				"CreateSovereignL1Tx.Validators[%d].BLSPubKey: %w (%v)",
				i, ErrBadBLSPoP, err,
			)
		}
		sig, err := bls.SignatureFromBytes(sigBytes)
		if err != nil {
			return fmt.Errorf(
				"CreateSovereignL1Tx.Validators[%d].BLSPoP: %w (%v)",
				i, ErrBadBLSPoP, err,
			)
		}
		if !bls.VerifyProofOfPossession(pk, sig, pkBytes) {
			return fmt.Errorf(
				"CreateSovereignL1Tx.Validators[%d].BLSPoP: %w",
				i, ErrBadBLSPoP,
			)
		}
	}

	// R6V4: non-empty chains.
	// R6V5: every entry's FxIDsLen must be an exact multiple of FxIDSize.
	chains := t.Chains()
	if chains.Len() == 0 {
		return fmt.Errorf("CreateSovereignL1Tx.Chains: %w", ErrZeroChains)
	}
	if err := chains.Verify(); err != nil {
		return fmt.Errorf("CreateSovereignL1Tx.Chains: %w", err)
	}
	return nil
}

// Verify reconstructs an OwnerStub from a TransferChainOwnershipTx's
// (threshold, locktime, address) tuple and runs SyntacticVerify on it. The
// tx carries Owner fields inline but had no Verify() until LP-023 Red
// round 6 R6V8 — every other Owner-bearing tx is gated, and skipping this
// one allowed an adversary to publish a chain-ownership transfer with
// threshold=0 (no signer required) or threshold>1 (unsatisfiable, DoS).
//
// Note: TransferChainOwnershipTx pins a single-address Owner in v3 (the
// most common configuration), so OwnerStub.SyntacticVerify is the right
// gate — it accepts threshold=1 only.
func (t TransferChainOwnershipTx) Verify() error {
	o := stubFromTuple(t.OwnerThreshold(), t.OwnerLocktime(), t.OwnerAddress())
	if err := o.SyntacticVerify(); err != nil {
		return fmt.Errorf("TransferChainOwnershipTx.Owner: %w", err)
	}
	return nil
}
