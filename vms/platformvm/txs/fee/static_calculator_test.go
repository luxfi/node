// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// TestStaticCalculator exercises the static fee visitor over natively-built
// txs (the pre-LP-023 BE hex fixtures no longer parse). Supported tx types map
// to their configured flat fee; the proposal/internal txs the static visitor
// does not price (AdvanceTime, RewardValidator) surface ErrUnsupportedTx.
func TestStaticCalculator(t *testing.T) {
	require := require.New(t)

	cfg := StaticConfig{
		TxFee:                  1000,
		CreateChainTxFee:       2000,
		CreateNetworkTxFee:     3000,
		AddNetworkValidatorFee: 4000,
	}
	calculator := NewSimpleStaticCalculator(cfg)

	// BaseTx → flat TxFee.
	fee, err := calculator.CalculateFee(feeBaseTx(t))
	require.NoError(err)
	require.Equal(cfg.TxFee, fee)

	// CreateChainTx → CreateChainTxFee.
	fee, err = calculator.CalculateFee(feeCreateChainTx(t))
	require.NoError(err)
	require.Equal(cfg.CreateChainTxFee, fee)

	// Unsupported (consensus-internal) txs are refused.
	for _, utx := range []txs.UnsignedTx{
		txs.NewAdvanceTimeTx(1),
		txs.NewRewardValidatorTx(ids.GenerateTestID()),
	} {
		_, err := calculator.CalculateFee(utx)
		require.ErrorIs(err, ErrUnsupportedTx)
	}
}
