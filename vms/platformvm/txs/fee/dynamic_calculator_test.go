// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// TestDynamicCalculator exercises the dynamic (complexity → gas → cost) fee
// path over natively-built txs (the pre-LP-023 BE hex fixtures no longer
// parse). Supported tx types price out to a positive fee under the configured
// weights + price; unsupported tx types surface ErrUnsupportedTx (wrapped as a
// complexity error) rather than a bogus fee.
func TestDynamicCalculator(t *testing.T) {
	require := require.New(t)

	calculator := NewDynamicCalculator(testDynamicWeights, testDynamicPrice)

	for _, utx := range []txs.UnsignedTx{
		feeBaseTx(t),
		feeCreateChainTx(t),
	} {
		fee, err := calculator.CalculateFee(utx)
		require.NoError(err)
		require.NotZero(fee)
	}

	for _, utx := range []txs.UnsignedTx{
		txs.NewAdvanceTimeTx(1),
		txs.NewRewardValidatorTx(ids.GenerateTestID()),
	} {
		_, err := calculator.CalculateFee(utx)
		require.ErrorIs(err, ErrUnsupportedTx)
	}
}
