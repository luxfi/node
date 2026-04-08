// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"fmt"

	"github.com/luxfi/accel"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/math/set"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/txs"
	txexecutor "github.com/luxfi/node/vms/platformvm/txs/executor"
	"github.com/luxfi/node/vms/platformvm/warp"
)

// VerifyWarpMessages verifies all warp messages in the block. If any of the
// warp messages are invalid, an error is returned.
//
// When GPU acceleration is available and the block contains multiple BLS
// warp signatures, verification is batched through the GPU for throughput.
// Otherwise falls back to sequential CPU verification.
func VerifyWarpMessages(
	ctx context.Context,
	networkID uint32,
	validatorState validators.State,
	pChainHeight uint64,
	b block.Block,
) error {
	// Collect warp messages that need BLS verification.
	type blsJob struct {
		aggPubKey *bls.PublicKey
		aggSig    *bls.Signature
		msgBytes  []byte
		txIdx     int
	}

	var blsJobs []blsJob

	for txIdx, tx := range b.Txs() {
		rawMsg, hasWarp := extractWarpMessageBytes(tx.Unsigned)
		if !hasWarp {
			continue
		}

		msg, err := warp.ParseMessage(rawMsg)
		if err != nil {
			// Return raw error to match sequential path behavior.
			return err
		}

		// Only BitSetSignature uses BLS that can be batched.
		// Other signature types (Ringtail, Hybrid) fall through to sequential.
		bss, ok := msg.Signature.(*warp.BitSetSignature)
		if !ok || !accel.Available() {
			// Sequential path for non-BLS signatures or when GPU unavailable.
			if err := txexecutor.VerifyWarpMessages(ctx, networkID, validatorState, pChainHeight, tx.Unsigned); err != nil {
				return err
			}
			continue
		}

		// Verify non-crypto parts (quorum weight) and extract the aggregate key/sig.
		if msg.NetworkID != networkID {
			return fmt.Errorf("tx %d: %w", txIdx, warp.ErrWrongNetworkID)
		}

		cvs, err := warp.GetCanonicalValidatorSetFromChainID(ctx, validatorState, pChainHeight, msg.SourceChainID)
		if err != nil {
			return fmt.Errorf("tx %d: failed to get validators: %w", txIdx, err)
		}

		signerIndices := set.BitsFromBytes(bss.Signers)
		if len(signerIndices.Bytes()) != len(bss.Signers) {
			return fmt.Errorf("tx %d: %w", txIdx, warp.ErrInvalidBitSet)
		}

		signers, err := warp.FilterValidators(signerIndices, cvs.Validators)
		if err != nil {
			return fmt.Errorf("tx %d: %w", txIdx, err)
		}

		sigWeight, _ := warp.SumWeight(signers)
		if err := warp.VerifyWeight(sigWeight, cvs.TotalWeight, txexecutor.WarpQuorumNumerator, txexecutor.WarpQuorumDenominator); err != nil {
			return fmt.Errorf("tx %d: %w", txIdx, err)
		}

		aggSig, err := bls.SignatureFromBytes(bss.Signature[:])
		if err != nil {
			return fmt.Errorf("tx %d: %w", txIdx, err)
		}

		aggPubKey, err := warp.AggregatePublicKeys(signers)
		if err != nil {
			return fmt.Errorf("tx %d: %w", txIdx, err)
		}

		blsJobs = append(blsJobs, blsJob{
			aggPubKey: aggPubKey,
			aggSig:    aggSig,
			msgBytes:  msg.UnsignedMessage.Bytes(),
			txIdx:     txIdx,
		})
	}

	if len(blsJobs) == 0 {
		return nil
	}

	// Single BLS signature -- no batch benefit, verify directly.
	if len(blsJobs) == 1 {
		j := blsJobs[0]
		if !bls.Verify(j.aggPubKey, j.aggSig, j.msgBytes) {
			return fmt.Errorf("tx %d: %w", j.txIdx, warp.ErrInvalidSignature)
		}
		return nil
	}

	// Batch BLS verification via GPU.
	pks := make([]*bls.PublicKey, len(blsJobs))
	msgs := make([][]byte, len(blsJobs))
	sigs := make([]*bls.Signature, len(blsJobs))
	for i, j := range blsJobs {
		pks[i] = j.aggPubKey
		msgs[i] = j.msgBytes
		sigs[i] = j.aggSig
	}

	results, err := warp.BatchVerifyBLSSignatures(pks, msgs, sigs)
	if err != nil {
		return fmt.Errorf("batch BLS verify: %w", err)
	}

	for i, valid := range results {
		if !valid {
			return fmt.Errorf("tx %d: %w", blsJobs[i].txIdx, warp.ErrInvalidSignature)
		}
	}
	return nil
}

// extractWarpMessageBytes returns the raw warp message bytes from a tx and
// whether the tx carries a warp message. A true second return value means
// the tx type requires warp verification (even if the message bytes are nil/empty).
func extractWarpMessageBytes(unsigned txs.UnsignedTx) ([]byte, bool) {
	switch tx := unsigned.(type) {
	case *txs.RegisterL1ValidatorTx:
		return tx.Message, true
	case *txs.SetL1ValidatorWeightTx:
		return tx.Message, true
	default:
		return nil, false
	}
}
