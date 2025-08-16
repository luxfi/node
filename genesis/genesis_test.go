// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	_ "embed"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/hashing"
	"github.com/luxfi/node/utils/perms"
	"github.com/luxfi/node/vms/platformvm/genesis"
)

var (
	//go:embed genesis_test.json
	customGenesisConfigJSON  []byte
	invalidGenesisConfigJSON = []byte(`{
		"networkID": 9999}}}}
	}`)

	genesisStakingCfg = &StakingConfig{
		MaxStakeDuration: 365 * 24 * time.Hour,
	}
)

func TestValidateConfig(t *testing.T) {
	tests := map[string]struct {
		networkID   uint32
		config      *Config
		expectedErr error
	}{
		"mainnet": {
			networkID:   constants.LuxMainnetID, // 96369
			config:      &MainnetConfig,
			expectedErr: errNoStakers, // Lux mainnet has no initial stakers
		},
		"testnet": {
			networkID:   constants.LuxTestnetID, // 96368
			config:      &TestnetConfig,
			expectedErr: errNoStakers, // Lux testnet has no initial stakers
		},
		"local": {
			networkID:   12345,
			config:      &LocalConfig,
			expectedErr: nil,
		},
		"mainnet (networkID mismatch)": {
			networkID:   2,
			config:      &MainnetConfig,
			expectedErr: errConflictingNetworkIDs,
		},
		"invalid start time": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.StartTime = 999999999999999
				return &thisConfig
			}(),
			expectedErr: errFutureStartTime,
		},
		"no initial supply": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.Allocations = []Allocation{}
				return &thisConfig
			}(),
			expectedErr: errNoSupply,
		},
		"no initial stakers": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakers = []Staker{}
				return &thisConfig
			}(),
			expectedErr: errNoStakers,
		},
		"invalid initial stake duration": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakeDuration = 0
				return &thisConfig
			}(),
			expectedErr: errNoStakeDuration,
		},
		"too large initial stake duration": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakeDuration = uint64(genesisStakingCfg.MaxStakeDuration+time.Second) / uint64(time.Second)
				return &thisConfig
			}(),
			expectedErr: errStakeDurationTooHigh,
		},
		"invalid stake offset": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				// Add multiple stakers to trigger the offset validation
				thisConfig.InitialStakers = append(thisConfig.InitialStakers, thisConfig.InitialStakers[0])
				thisConfig.InitialStakers = append(thisConfig.InitialStakers, thisConfig.InitialStakers[0])
				// With 3 stakers and huge offset, offsetTimeRequired will exceed duration
				thisConfig.InitialStakeDurationOffset = 100000000
				return &thisConfig
			}(),
			expectedErr: errInitialStakeDurationTooLow,
		},
		"empty initial staked funds": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakedFunds = []ids.ShortID(nil)
				return &thisConfig
			}(),
			expectedErr: errNoInitiallyStakedFunds,
		},
		"duplicate initial staked funds": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.InitialStakedFunds = append(thisConfig.InitialStakedFunds, thisConfig.InitialStakedFunds[0])
				return &thisConfig
			}(),
			expectedErr: errDuplicateInitiallyStakedAddress,
		},
		"initial staked funds not in allocations": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				// Add a staked fund that's not in allocations
				nonExistentAddr := ids.ShortID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
				thisConfig.InitialStakedFunds = append(thisConfig.InitialStakedFunds, nonExistentAddr)
				return &thisConfig
			}(),
			expectedErr: errNoAllocationToStake,
		},
		"empty C-Chain genesis": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.CChainGenesis = ""
				return &thisConfig
			}(),
			expectedErr: errNoCChainGenesis,
		},
		"empty message": {
			networkID: 12345,
			config: func() *Config {
				thisConfig := LocalConfig
				thisConfig.Message = ""
				return &thisConfig
			}(),
			expectedErr: nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := validateConfig(test.networkID, test.config, genesisStakingCfg)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

func TestGenesisFromFile(t *testing.T) {
	tests := map[string]struct {
		networkID       uint32
		customConfig    []byte
		missingFilepath string
		expectedErr     error
		expectedHash    string
	}{
		"mainnet": {
			networkID:    constants.MainnetID,
			customConfig: customGenesisConfigJSON,
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"testnet": {
			networkID:    constants.TestnetID,
			customConfig: customGenesisConfigJSON,
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"testnet (with custom specified)": {
			networkID:    constants.TestnetID,
			customConfig: localGenesisConfigJSON, // won't load
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"local": {
			networkID:    constants.LocalID,
			customConfig: customGenesisConfigJSON,
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"local (with custom specified)": {
			networkID:    constants.LocalID,
			customConfig: customGenesisConfigJSON,
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"custom": {
			networkID:    9999,
			customConfig: customGenesisConfigJSON,
			expectedErr:  nil,
			expectedHash: "5b6e3e72110135c541ad6f6f7f0a495cf749a63af9668be43ec33096b7cf20d4",
		},
		"custom (networkID mismatch)": {
			networkID:    9999,
			customConfig: localGenesisConfigJSON,
			expectedErr:  errConflictingNetworkIDs,
		},
		"custom (invalid format)": {
			networkID:    9999,
			customConfig: invalidGenesisConfigJSON,
			expectedErr:  errInvalidGenesisJSON,
		},
		"custom (missing filepath)": {
			networkID:       9999,
			missingFilepath: "missing.json",
			expectedErr:     os.ErrNotExist,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			// test loading of genesis from file
			var customFile string
			if len(test.customConfig) > 0 {
				customFile = filepath.Join(t.TempDir(), "config.json")
				require.NoError(perms.WriteFile(customFile, test.customConfig, perms.ReadWrite))
			}

			if len(test.missingFilepath) > 0 {
				customFile = test.missingFilepath
			}

			genesisBytes, _, err := FromFile(test.networkID, customFile, genesisStakingCfg)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr == nil {
				genesisHash := hex.EncodeToString(hashing.ComputeHash256(genesisBytes))
				require.Equal(test.expectedHash, genesisHash, "genesis hash mismatch")

				_, err = genesis.Parse(genesisBytes)
				require.NoError(err)
			}
		})
	}
}

func TestGenesisFromFlag(t *testing.T) {
	tests := map[string]struct {
		networkID    uint32
		customConfig []byte
		expectedErr  error
		expectedHash string
	}{
		"mainnet": {
			networkID:   constants.MainnetID,
			expectedErr: errOverridesStandardNetworkConfig,
		},
		"testnet": {
			networkID:   constants.TestnetID,
			expectedErr: errOverridesStandardNetworkConfig,
		},
		"local": {
			networkID:   constants.LocalID,
			expectedErr: errOverridesStandardNetworkConfig,
		},
		"local (with custom specified)": {
			networkID:    constants.LocalID,
			customConfig: customGenesisConfigJSON,
			expectedErr:  errOverridesStandardNetworkConfig,
		},
		"custom": {
			networkID:    9999,
			customConfig: customGenesisConfigJSON,
			expectedErr:  nil,
			expectedHash: "5b6e3e72110135c541ad6f6f7f0a495cf749a63af9668be43ec33096b7cf20d4",
		},
		"custom (networkID mismatch)": {
			networkID:    9999,
			customConfig: localGenesisConfigJSON,
			expectedErr:  errConflictingNetworkIDs,
		},
		"custom (invalid format)": {
			networkID:    9999,
			customConfig: invalidGenesisConfigJSON,
			expectedErr:  errInvalidGenesisJSON,
		},
		"custom (missing content)": {
			networkID:   9999,
			expectedErr: errInvalidGenesisJSON,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			// test loading of genesis content from flag/env-var
			var genBytes []byte
			if len(test.customConfig) == 0 {
				// try loading a default config
				var err error
				switch test.networkID {
				case constants.MainnetID:
					genBytes, err = json.Marshal(&MainnetConfig)
					require.NoError(err)
				case constants.TestnetID:
					genBytes, err = json.Marshal(&TestnetConfig)
					require.NoError(err)
				case constants.LocalID:
					genBytes, err = json.Marshal(&LocalConfig)
					require.NoError(err)
				default:
					genBytes = make([]byte, 0)
				}
			} else {
				genBytes = test.customConfig
			}
			content := base64.StdEncoding.EncodeToString(genBytes)

			genesisBytes, _, err := FromFlag(test.networkID, content, genesisStakingCfg)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr == nil {
				genesisHash := hex.EncodeToString(hashing.ComputeHash256(genesisBytes))
				require.Equal(test.expectedHash, genesisHash, "genesis hash mismatch")

				_, err = genesis.Parse(genesisBytes)
				require.NoError(err)
			}
		})
	}
}

func TestGenesis(t *testing.T) {
	tests := []struct {
		networkID  uint32
		expectedID string
	}{
		{
			networkID:  constants.MainnetID,
			expectedID: "2RubAfTujLNUutcbtqsjpvfcHnvCbDrcH9ido9EbdJkYeYnWgE",
		},
		{
			networkID:  constants.TestnetID,
			expectedID: "ZJhaJoFxhB94e8McJENhDH7JW2cPUWndTGcKehtEWy2fSfYLq",
		},
		{
			networkID:  constants.LocalID,
			expectedID: "2ASgccyNavJxfhGkfb5d3Xt6QyKJmU6YabQnK1dd4Noei1XqBH",
		},
	}
	for _, test := range tests {
		t.Run(constants.NetworkIDToNetworkName[test.networkID], func(t *testing.T) {
			require := require.New(t)

			config := GetConfig(test.networkID)
			genesisBytes, _, err := FromConfig(config)
			require.NoError(err)

			var genesisID ids.ID = hashing.ComputeHash256Array(genesisBytes)
			require.Equal(test.expectedID, genesisID.String())
		})
	}
}

func TestVMGenesis(t *testing.T) {
	type vmTest struct {
		vmID       ids.ID
		expectedID string
	}
	tests := []struct {
		networkID uint32
		vmTest    []vmTest
	}{
		{
			networkID: constants.MainnetID,
			vmTest: []vmTest{
				{
					vmID:       constants.XVMID,
					expectedID: "oBNAjGCioVdXmQqUqXtgaRNJgz8ZDp1qejpY6gwN6Ddct1xgx",
				},
				{
					vmID:       constants.EVMID,
					expectedID: "EWi9aPkGe6EfJ3SobCAmSUXRPLa4brF3cThwPwmHTrD1y13jy",
				},
			},
		},
		{
			networkID: constants.TestnetID,
			vmTest: []vmTest{
				{
					vmID:       constants.XVMID,
					expectedID: "247iq1TNcSm82HvqLgEWwJrNfHTYeKUcEa8A1kYU6mow4QMaTf",
				},
				{
					vmID:       constants.EVMID,
					expectedID: "2JC1RVBy3tGYZ27ZsZfT5y6JXPkKpW1aCepgHN7WwmiwBhCnn5",
				},
			},
		},
		{
			networkID: constants.LocalID,
			vmTest: []vmTest{
				{
					vmID:       constants.XVMID,
					expectedID: "dnETgw1RTLQwSLbuhQEnjisfsvEz84CLgwGTYByyvAjjgLY4B",
				},
				{
					vmID:       constants.EVMID,
					expectedID: "2WokyoVujr7NqByCPvaaSNRqqnKSJM7jahC58RhRyaktPjNPqz",
				},
			},
		},
	}

	for _, test := range tests {
		for _, vmTest := range test.vmTest {
			name := fmt.Sprintf("%s-%s",
				constants.NetworkIDToNetworkName[test.networkID],
				vmTest.vmID,
			)
			t.Run(name, func(t *testing.T) {
				require := require.New(t)

				config := GetConfig(test.networkID)
				genesisBytes, _, err := FromConfig(config)
				require.NoError(err)

				genesisTx, err := VMGenesis(genesisBytes, vmTest.vmID)
				require.NoError(err)

				require.Equal(
					vmTest.expectedID,
					genesisTx.ID().String(),
					"%s genesisID with networkID %d mismatch",
					vmTest.vmID,
					test.networkID,
				)
			})
		}
	}
}

func TestLUXAssetID(t *testing.T) {
	tests := []struct {
		networkID  uint32
		expectedID string
	}{
		{
			networkID:  constants.MainnetID,
			expectedID: "2HkbAcvpo7htbWv4iffXpzbcLLzjDZPBJ32fKRGXYFfBsxoe9F",
		},
		{
			networkID:  constants.TestnetID,
			expectedID: "2we4SyEFuAC5Ch7hgPBjVfz6eut4TyzFd954ZLWCa5mAiAer5Z",
		},
		{
			networkID:  constants.LocalID,
			expectedID: "2QLSdF6mNa6tbRLodYBqQcZY3evjw3etbBswv3ciTbxiebXa7q",
		},
	}

	for _, test := range tests {
		t.Run(constants.NetworkIDToNetworkName[test.networkID], func(t *testing.T) {
			require := require.New(t)

			config := GetConfig(test.networkID)
			_, luxAssetID, err := FromConfig(config)
			require.NoError(err)

			require.Equal(
				test.expectedID,
				luxAssetID.String(),
				"LUX assetID with networkID %d mismatch",
				test.networkID,
			)
		})
	}
}
