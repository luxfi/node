// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/address"
	"github.com/luxfi/constants"
	"github.com/luxfi/formatting"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/json"
)

var addrStrArray = []string{
	"A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy",
	"6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv",
	"6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa",
	"Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7",
}

func TestBuildGenesis(t *testing.T) {
	require := require.New(t)

	ss := CreateStaticService()
	addrMap := map[string]string{}
	for _, addrStr := range addrStrArray {
		addr, err := ids.ShortFromString(addrStr)
		require.NoError(err)
		addrMap[addrStr], err = address.FormatBech32(constants.UnitTestHRP, addr[:])
		require.NoError(err)
	}
	args := BuildGenesisArgs{
		Encoding: formatting.Hex,
		GenesisData: Assets{
			{
				Alias:        "asset1",
				Name:         "myFixedCapAsset",
				Symbol:       "MFCA",
				Denomination: 8,
				InitialState: InitialState{
					FixedCap: []Holder{
						{
							Amount:  100000,
							Address: addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
						},
						{
							Amount:  100000,
							Address: addrMap["6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv"],
						},
						{
							Amount:  json.Uint64(startBalance),
							Address: addrMap["6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa"],
						},
						{
							Amount:  json.Uint64(startBalance),
							Address: addrMap["Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7"],
						},
					},
				},
			},
			{
				Alias:  "asset2",
				Name:   "myVarCapAsset",
				Symbol: "MVCA",
				InitialState: InitialState{
					VariableCap: []Owners{
						{
							Threshold: 1,
							Minters: []string{
								addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
								addrMap["6mxBGnjGDCKgkVe7yfrmvMA7xE7qCv3vv"],
							},
						},
						{
							Threshold: 2,
							Minters: []string{
								addrMap["6ncQ19Q2U4MamkCYzshhD8XFjfwAWFzTa"],
								addrMap["Jz9ayEDt7dx9hDx45aXALujWmL9ZUuqe7"],
							},
						},
					},
				},
			},
			{
				Alias: "asset3",
				Name:  "myOtherVarCapAsset",
				InitialState: InitialState{
					VariableCap: []Owners{
						{
							Threshold: 1,
							Minters: []string{
								addrMap["A9bTQjfYGBFK3JPRJqF2eh3JYL7cHocvy"],
							},
						},
					},
				},
			},
		},
	}
	reply := BuildGenesisReply{}
	require.NoError(ss.BuildGenesis(nil, &args, &reply))
}
