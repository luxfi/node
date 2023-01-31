// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestSubnetValidatorVerifySubnetID(t *testing.T) {
	require := require.New(t)

	// Error path
	{
		vdr := &SubnetValidator{
			Subnet: constants.PrimaryNetworkID,
		}

<<<<<<< HEAD
<<<<<<< HEAD
		require.ErrorIs(vdr.Verify(), errBadSubnetID)
=======
		require.Equal(errBadSubnetID, vdr.Verify())
>>>>>>> a8631aa5c (Add Fx tests (#1838))
=======
		require.ErrorIs(vdr.Verify(), errBadSubnetID)
>>>>>>> 7c09e7074 (Standardize `require` usage and remove `t.Fatal` from platformvm (#2297))
	}

	// Happy path
	{
		vdr := &SubnetValidator{
			Subnet: ids.GenerateTestID(),
			Validator: Validator{
				Wght: 1,
			},
		}

<<<<<<< HEAD
<<<<<<< HEAD
		require.NoError(vdr.Verify())
=======
		require.Equal(nil, vdr.Verify())
>>>>>>> a8631aa5c (Add Fx tests (#1838))
=======
		require.NoError(vdr.Verify())
>>>>>>> 7c09e7074 (Standardize `require` usage and remove `t.Fatal` from platformvm (#2297))
	}
}
