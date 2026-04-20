// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/bls"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/txs"
)

var (
	errGraniteUpgradeNotActive    = errors.New("attempting to use a Granite-upgrade feature prior to activation")
	errValidatorNotFound          = errors.New("validator not found in current staker set")
	errValidatorNoBLSKey          = errors.New("validator has no BLS public key registered")
	errInvalidEvidenceSignature   = errors.New("evidence signature does not verify against validator BLS key")
	errSlashPercentMismatch = errors.New("slash percentage does not match expected value for evidence type")
)

// Default slash percentages per evidence type.
const (
	DoubleVoteSlashPercent = 100_000  // 10% of reward.PercentDenominator (1_000_000)
	DoubleSignSlashPercent = 500_000  // 50%
)

func (e *standardTxExecutor) SlashValidatorTx(tx *txs.SlashValidatorTx) error {
	currentTimestamp := e.state.GetTimestamp()
	if !e.backend.Config.UpgradeConfig.IsGraniteActivated(currentTimestamp) {
		return errGraniteUpgradeNotActive
	}

	if err := e.tx.SyntacticVerify(e.backend.Runtime); err != nil {
		return err
	}

	// Verify slash percentage matches evidence type
	var expectedPercent uint32
	switch tx.Evidence.Type {
	case txs.DoubleVoteEvidence:
		expectedPercent = DoubleVoteSlashPercent
	case txs.DoubleSignEvidence:
		expectedPercent = DoubleSignSlashPercent
	default:
		return fmt.Errorf("unknown evidence type: %d", tx.Evidence.Type)
	}

	if tx.SlashPercentage != expectedPercent {
		return fmt.Errorf(
			"%w: got %d, expected %d for evidence type %d",
			errSlashPercentMismatch,
			tx.SlashPercentage,
			expectedPercent,
			tx.Evidence.Type,
		)
	}

	// Look up the validator in the current staker set
	staker, err := e.state.GetCurrentValidator(constants.PrimaryNetworkID, tx.NodeID)
	if err != nil {
		return fmt.Errorf("%w: %w", errValidatorNotFound, err)
	}

	if staker.PublicKey == nil {
		return errValidatorNoBLSKey
	}

	// Verify both evidence signatures against the validator's BLS key
	if err := verifyEvidenceSignatures(staker.PublicKey, &tx.Evidence); err != nil {
		return err
	}

	// Calculate slash amount using overflow-safe arithmetic.
	// Instead of weight * pct / denom (which overflows for large weights),
	// split into: (weight / denom) * pct + (weight % denom) * pct / denom
	weight := staker.Weight
	pct := uint64(tx.SlashPercentage)
	denom := uint64(reward.PercentDenominator)
	slashAmount := (weight/denom)*pct + (weight%denom)*pct/denom
	if slashAmount == 0 {
		slashAmount = 1 // Slash at least 1 unit
	}

	newWeight := staker.Weight - slashAmount
	if newWeight < e.backend.Config.MinValidatorStake {
		// Remove the validator entirely -- weight below minimum
		e.state.DeleteCurrentValidator(staker)
	} else {
		// Update the validator's weight in the staker set
		e.state.DeleteCurrentValidator(staker)
		updatedStaker := *staker
		updatedStaker.Weight = newWeight
		if err := e.state.PutCurrentValidator(&updatedStaker); err != nil {
			return fmt.Errorf("failed to update slashed validator: %w", err)
		}
	}

	txID := e.tx.ID()
	lux.Consume(e.state, tx.Ins)
	lux.Produce(e.state, txID, tx.Outs)

	return nil
}

// verifyEvidenceSignatures checks that both signatures in the evidence
// verify against the given BLS public key.
func verifyEvidenceSignatures(pk *bls.PublicKey, evidence *txs.SlashEvidence) error {
	sigA, err := bls.SignatureFromBytes(evidence.SignatureA)
	if err != nil {
		return fmt.Errorf("%w: failed to parse signature A: %w", errInvalidEvidenceSignature, err)
	}

	if !bls.Verify(pk, sigA, evidence.MessageA) {
		return fmt.Errorf("%w: signature A does not verify", errInvalidEvidenceSignature)
	}

	sigB, err := bls.SignatureFromBytes(evidence.SignatureB)
	if err != nil {
		return fmt.Errorf("%w: failed to parse signature B: %w", errInvalidEvidenceSignature, err)
	}

	if !bls.Verify(pk, sigB, evidence.MessageB) {
		return fmt.Errorf("%w: signature B does not verify", errInvalidEvidenceSignature)
	}

	return nil
}
