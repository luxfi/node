// Copyright (C) 2019-2022, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxdefi/node/ids"
	"github.com/luxdefi/node/utils/constants"
)

func TestSubnetValidatorVerifySubnetID(t *testing.T) {
	require := require.New(t)

	// Error path
	{
		vdr := &SubnetValidator{
			Subnet: constants.PrimaryNetworkID,
		}

		require.ErrorIs(vdr.Verify(), errBadSubnetID)
	}

	// Happy path
	{
		vdr := &SubnetValidator{
			Subnet: ids.GenerateTestID(),
			Validator: Validator{
				Wght: 1,
			},
		}

		require.NoError(vdr.Verify())
	}
}
