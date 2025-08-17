// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// Test constants for dynamic fee calculation
var (
	testDynamicWeights = gas.Dimensions{
		gas.Bandwidth: 1,
		gas.DBRead:    10,
		gas.DBWrite:   100,
		gas.Compute:   1,
	}
	testDynamicPrice = gas.Price(1000)
)

func TestDynamicCalculator(t *testing.T) {
	calculator := NewDynamicCalculator(testDynamicWeights, testDynamicPrice)
	for _, test := range txTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			txBytes, err := hex.DecodeString(test.tx)
			require.NoError(err)

			tx, err := txs.Parse(txs.Codec, txBytes)
			require.NoError(err)

			fee, err := calculator.CalculateFee(tx.Unsigned)
			require.Equal(int(test.expectedDynamicFee), int(fee))
			require.ErrorIs(err, test.expectedDynamicFeeErr)
		})
	}
}
