// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
)

func TestNetValidatorVerifyNetID(t *testing.T) {
	require := require.New(t)

	// Error path
	{
		vdr := &NetValidator{
			Net: constants.PrimaryNetworkID,
		}

		err := vdr.Verify()
		require.ErrorIs(err, errBadNetID)
	}

	// Happy path
	{
		vdr := &NetValidator{
			Net: ids.GenerateTestID(),
			Validator: Validator{
				Wght: 1,
			},
		}

		require.NoError(vdr.Verify())
	}
}
