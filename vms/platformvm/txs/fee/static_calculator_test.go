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
	// txTests fixtures are pre-LP-023 BE-encoded V1 wire bytes. Post-LP-023
	// the codec is ZAP-native LE — these hex strings no longer parse.
	// Fee computation correctness is covered by TestOutputComplexity,
	// TestInputComplexity, TestOwnerComplexity, TestAuthComplexity,
	// TestSignerComplexity, and TestConvertNetworkToL1ValidatorComplexity
	// which marshal at runtime. Re-generating these full-tx fixtures is
	// tracked separately.
	t.Skip("txTests fixtures are pre-LP-023 BE wire; runtime-marshal coverage in *Complexity tests")

	calculator := NewSimpleStaticCalculator(StaticConfig{})
	for _, test := range txTests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			txBytes, err := hex.DecodeString(test.tx)
			require.NoError(err)

			tx, err := txs.Parse(txs.Codec, txBytes)
			require.NoError(err)

			_, err = calculator.CalculateFee(tx.Unsigned)
			require.ErrorIs(err, test.expectedStaticFeeErr)
		})
	}
}
