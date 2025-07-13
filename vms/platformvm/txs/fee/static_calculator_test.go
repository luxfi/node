// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/vms/platformvm/txs"
)

func TestStaticCalculator(t *testing.T) {
	calculator := NewStaticCalculator(testStaticConfig)
	for _, test := range txTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			txBytes, err := hex.DecodeString(test.tx)
			require.NoError(err)

			tx, err := txs.Parse(txs.Codec, txBytes)
			require.NoError(err)

			fee, err := calculator.CalculateFee(tx.Unsigned)
			require.Equal(test.expectedStaticFee, fee)
			require.ErrorIs(err, test.expectedStaticFeeErr)
		})
	}
}
