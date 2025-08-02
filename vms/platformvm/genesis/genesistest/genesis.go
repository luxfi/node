// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesistest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	// "github.com/luxfi/node/v2/quasar/consensustest" // TODO: Use when AssetID is available
	"github.com/luxfi/node/v2/upgrade"
	"github.com/luxfi/node/v2/utils/constants"
	"github.com/luxfi/node/v2/utils/crypto/secp256k1"
	"github.com/luxfi/node/v2/utils/units"
	"github.com/luxfi/node/v2/vms/components/lux"
	"github.com/luxfi/node/v2/vms/platformvm/reward"
	"github.com/luxfi/node/v2/vms/platformvm/txs"
	"github.com/luxfi/node/v2/vms/secp256k1fx"

	platformvmgenesis "github.com/luxfi/node/v2/vms/platformvm/genesis"
)

const (
	DefaultValidatorDuration = 28 * 24 * time.Hour
	DefaultValidatorWeight   = 5 * units.MilliLux
	DefaultInitialBalance    = 1 * units.Lux

	ValidatorDelegationShares = reward.PercentDenominator
	XChainName                = "x"
	InitialSupply             = 360 * units.MegaLux
)

var (
	// TODO: Get AssetID from consensustest when available
	// For now, using a placeholder ID
	AssetID = ids.Empty
	LUXAsset = lux.Asset{ID: AssetID}

	DefaultValidatorStartTime     = upgrade.InitiallyActiveTime
	DefaultValidatorStartTimeUnix = uint64(DefaultValidatorStartTime.Unix())
	DefaultValidatorEndTime       = DefaultValidatorStartTime.Add(DefaultValidatorDuration)
	DefaultValidatorEndTimeUnix   = uint64(DefaultValidatorEndTime.Unix())
)

var (
	// Keys that are funded in the genesis
	DefaultFundedKeys = secp256k1.TestKeys()

	// Node IDs of genesis validators
	DefaultNodeIDs []ids.NodeID
)

func init() {
	DefaultNodeIDs = make([]ids.NodeID, len(DefaultFundedKeys))
	for i := range DefaultFundedKeys {
		DefaultNodeIDs[i] = ids.GenerateTestNodeID()
	}
}

type Config struct {
	NetworkID          uint32
	NodeIDs            []ids.NodeID
	ValidatorWeight    uint64
	ValidatorStartTime time.Time
	ValidatorEndTime   time.Time

	FundedKeys     []*secp256k1.PrivateKey
	InitialBalance uint64
}

func New(t testing.TB, c Config) *platformvmgenesis.Genesis {
	if c.NetworkID == 0 {
		c.NetworkID = constants.UnitTestID
	}
	if len(c.NodeIDs) == 0 {
		c.NodeIDs = DefaultNodeIDs
	}
	if c.ValidatorWeight == 0 {
		c.ValidatorWeight = DefaultValidatorWeight
	}
	if c.ValidatorStartTime.IsZero() {
		c.ValidatorStartTime = DefaultValidatorStartTime
	}
	if c.ValidatorEndTime.IsZero() {
		c.ValidatorEndTime = DefaultValidatorEndTime
	}
	if len(c.FundedKeys) == 0 {
		c.FundedKeys = DefaultFundedKeys
	}
	if c.InitialBalance == 0 {
		c.InitialBalance = DefaultInitialBalance
	}

	require := require.New(t)

	genesis := &platformvmgenesis.Genesis{
		UTXOs:         make([]*platformvmgenesis.UTXO, len(c.FundedKeys)),
		Validators:    make([]*txs.Tx, len(c.NodeIDs)),
		Timestamp:     uint64(c.ValidatorStartTime.Unix()),
		InitialSupply: InitialSupply,
	}
	for i, key := range c.FundedKeys {
		genesis.UTXOs[i] = &platformvmgenesis.UTXO{UTXO: lux.UTXO{
			UTXOID: lux.UTXOID{
				TxID:        AssetID,
				OutputIndex: uint32(i),
			},
			Asset: LUXAsset,
			Out: &secp256k1fx.TransferOutput{
				Amt: c.InitialBalance,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs: []ids.ShortID{
						key.Address(),
					},
				},
			},
		}}
	}
	for i, nodeID := range c.NodeIDs {
		key := c.FundedKeys[i%len(c.FundedKeys)]
		owner := secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs: []ids.ShortID{
				key.Address(),
			},
		}
		validator := &txs.AddValidatorTx{
			BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
				NetworkID:    c.NetworkID,
				BlockchainID: constants.PlatformChainID(),
			}},
			Validator: txs.Validator{
				NodeID: nodeID,
				Start:  uint64(c.ValidatorStartTime.Unix()),
				End:    uint64(c.ValidatorEndTime.Unix()),
				Wght:   c.ValidatorWeight,
			},
			StakeOuts: []*lux.TransferableOutput{
				{
					Asset: LUXAsset,
					Out: &secp256k1fx.TransferOutput{
						Amt:          c.ValidatorWeight,
						OutputOwners: owner,
					},
				},
			},
			RewardsOwner:     &owner,
			DelegationShares: ValidatorDelegationShares,
		}
		validatorTx := &txs.Tx{Unsigned: validator}
		require.NoError(validatorTx.Initialize(txs.GenesisCodec))

		genesis.Validators[i] = validatorTx
	}

	chain := &txs.CreateChainTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    c.NetworkID,
			BlockchainID: constants.PlatformChainID(),
		}},
		SubnetID:   constants.PrimaryNetworkID,
		ChainName:  XChainName,
		VMID:       constants.XVMID,
		SubnetAuth: &secp256k1fx.Input{},
	}
	chainTx := &txs.Tx{Unsigned: chain}
	require.NoError(chainTx.Initialize(txs.GenesisCodec))

	genesis.Chains = []*txs.Tx{chainTx}
	return genesis
}

func NewBytes(t testing.TB, c Config) []byte {
	g := New(t, c)
	genesisBytes, err := platformvmgenesis.Codec.Marshal(platformvmgenesis.CodecVersion, g)
	require.NoError(t, err)
	return genesisBytes
}
