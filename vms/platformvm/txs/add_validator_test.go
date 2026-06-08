// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/runtime"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/timer/mockable"
	"github.com/luxfi/utxo/secp256k1fx"
)

func TestAddValidatorTxSyntacticVerify(t *testing.T) {
	require := require.New(t)
	clk := mockable.Clock{}
	utxoAssetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	testChainID := ids.GenerateTestID() // Use a test chain ID instead of empty
	rt := &runtime.Runtime{
		NetworkID: constants.UnitTestID,

		ChainID: ids.GenerateTestID(),
	}
	rt = &runtime.Runtime{

		ChainID:  testChainID,
		UTXOAssetID: utxoAssetID,
		NodeID:   nodeID,
	}
	signers := [][]*secp256k1.PrivateKey{preFundedKeys}

	var (
		stx            *Tx
		addValidatorTx *AddValidatorTx
		err            error
	)

	// Case : signed tx is nil
	err = stx.SyntacticVerify(rt)
	require.ErrorIs(err, ErrNilSignedTx)

	// Case : unsigned tx is nil
	err = addValidatorTx.SyntacticVerify(rt)
	require.ErrorIs(err, ErrNilTx)

	validatorWeight := uint64(2022)
	rewardAddress := preFundedKeys[0].Address()
	inputs := []*lux.TransferableInput{{
		UTXOID: lux.UTXOID{
			TxID:        ids.ID{'t', 'x', 'I', 'D'},
			OutputIndex: 2,
		},
		Asset: lux.Asset{ID: utxoAssetID},
		In: &secp256k1fx.TransferInput{
			Amt:   uint64(5678),
			Input: secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}}
	outputs := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: utxoAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(1234),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{preFundedKeys[0].Address()},
			},
		},
	}}
	stakes := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: utxoAssetID},
		Out: &stakeable.LockOut{
			Locktime: uint64(clk.Time().Add(time.Second).Unix()),
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt: validatorWeight,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{preFundedKeys[0].Address()},
				},
			},
		},
	}}
	addValidatorTx = &AddValidatorTx{
		BaseTx: BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    rt.NetworkID,
			BlockchainID: rt.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		Validator: Validator{
			NodeID: rt.NodeID,
			Start:  uint64(clk.Time().Unix()),
			End:    uint64(clk.Time().Add(time.Hour).Unix()),
			Wght:   validatorWeight,
		},
		StakeOuts: stakes,
		RewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		DelegationShares: reward.PercentDenominator,
	}

	// Case: valid tx
	stx, err = NewSigned(addValidatorTx, Codec, signers)
	require.NoError(err)
	require.NoError(stx.SyntacticVerify(rt))

	// Case: Wrong network ID
	addValidatorTx.SyntacticallyVerified = false
	addValidatorTx.NetworkID++
	stx, err = NewSigned(addValidatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(rt)
	require.ErrorIs(err, lux.ErrWrongNetworkID)
	addValidatorTx.NetworkID--

	// Case: Stake owner has no addresses
	addValidatorTx.SyntacticallyVerified = false
	addValidatorTx.StakeOuts[0].
		Out.(*stakeable.LockOut).
		TransferableOut.(*secp256k1fx.TransferOutput).
		Addrs = nil
	stx, err = NewSigned(addValidatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(rt)
	require.ErrorIs(err, secp256k1fx.ErrOutputUnspendable)
	addValidatorTx.StakeOuts = stakes

	// Case: Rewards owner has no addresses
	addValidatorTx.SyntacticallyVerified = false
	addValidatorTx.RewardsOwner.(*secp256k1fx.OutputOwners).Addrs = nil
	stx, err = NewSigned(addValidatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(rt)
	require.ErrorIs(err, secp256k1fx.ErrOutputUnspendable)
	addValidatorTx.RewardsOwner.(*secp256k1fx.OutputOwners).Addrs = []ids.ShortID{rewardAddress}

	// Case: Too many shares
	addValidatorTx.SyntacticallyVerified = false
	addValidatorTx.DelegationShares++ // 1 more than max amount
	stx, err = NewSigned(addValidatorTx, Codec, signers)
	require.NoError(err)
	err = stx.SyntacticVerify(rt)
	require.ErrorIs(err, errTooManyShares)
	addValidatorTx.DelegationShares--
}

func TestAddValidatorTxSyntacticVerifyNotLUX(t *testing.T) {
	require := require.New(t)
	clk := mockable.Clock{}
	utxoAssetID := ids.GenerateTestID()
	nodeID := ids.GenerateTestNodeID()
	testChainID := ids.GenerateTestID() // Use a test chain ID instead of empty
	rt := &runtime.Runtime{
		NetworkID: constants.UnitTestID,

		ChainID: ids.GenerateTestID(),
	}
	rt = &runtime.Runtime{

		ChainID:  testChainID,
		UTXOAssetID: utxoAssetID,
		NodeID:   nodeID,
	}
	signers := [][]*secp256k1.PrivateKey{preFundedKeys}

	var (
		stx            *Tx
		addValidatorTx *AddValidatorTx
		err            error
	)

	assetID := ids.GenerateTestID()
	validatorWeight := uint64(2022)
	rewardAddress := preFundedKeys[0].Address()
	inputs := []*lux.TransferableInput{{
		UTXOID: lux.UTXOID{
			TxID:        ids.ID{'t', 'x', 'I', 'D'},
			OutputIndex: 2,
		},
		Asset: lux.Asset{ID: assetID},
		In: &secp256k1fx.TransferInput{
			Amt:   uint64(5678),
			Input: secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}}
	outputs := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: uint64(1234),
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{preFundedKeys[0].Address()},
			},
		},
	}}
	stakes := []*lux.TransferableOutput{{
		Asset: lux.Asset{ID: assetID},
		Out: &stakeable.LockOut{
			Locktime: uint64(clk.Time().Add(time.Second).Unix()),
			TransferableOut: &secp256k1fx.TransferOutput{
				Amt: validatorWeight,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{preFundedKeys[0].Address()},
				},
			},
		},
	}}
	addValidatorTx = &AddValidatorTx{
		BaseTx: BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    rt.NetworkID,
			BlockchainID: rt.ChainID,
			Ins:          inputs,
			Outs:         outputs,
		}},
		Validator: Validator{
			NodeID: rt.NodeID,
			Start:  uint64(clk.Time().Unix()),
			End:    uint64(clk.Time().Add(time.Hour).Unix()),
			Wght:   validatorWeight,
		},
		StakeOuts: stakes,
		RewardsOwner: &secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{rewardAddress},
		},
		DelegationShares: reward.PercentDenominator,
	}

	stx, err = NewSigned(addValidatorTx, Codec, signers)
	require.NoError(err)

	err = stx.SyntacticVerify(rt)
	require.ErrorIs(err, errStakeMustBeLUX)
}

func TestAddValidatorTxNotDelegatorTx(t *testing.T) {
	txIntf := any((*AddValidatorTx)(nil))
	_, ok := txIntf.(DelegatorTx)
	require.False(t, ok)
}

// TestAddValidatorTxSyntacticVerify_ArbitraryPrimaryNetworkIDs asserts
// that AddValidatorTx — the canonical post-LP-018 add-validator-to-a-
// network tx — accepts arbitrary primary networkIDs, not just the Lux
// primary values (1/2/3/1337). A sovereign L1 IS a primary network at
// its own networkID; downstream consumers may operate primary networks
// at any uint32 they choose. This test pins that contract: the tx body
// has no per-chain "Chain" field, so SyntacticVerify must succeed
// against any valid primary networkID.
func TestAddValidatorTxSyntacticVerify_ArbitraryPrimaryNetworkIDs(t *testing.T) {
	// Lux primaries (1/2/3/1337) plus four arbitrary synthetic IDs
	// covering low / mid / high uint32 ranges, to demonstrate the tx
	// is networkID-agnostic without baking any downstream value into
	// this upstream test.
	primaryNetworkIDs := []uint32{
		1,    // Lux mainnet
		2,    // Lux testnet
		3,    // Lux local
		1337, // Lux dev/localnet
		9001,
		123456,
		7_777_777,
		4_000_000_000,
	}

	for _, networkID := range primaryNetworkIDs {
		networkID := networkID
		t.Run(formatPrimaryNetworkID(networkID), func(t *testing.T) {
			require := require.New(t)

			clk := mockable.Clock{}
			utxoAssetID := ids.GenerateTestID()
			nodeID := ids.GenerateTestNodeID()
			testChainID := ids.GenerateTestID()
			rt := &runtime.Runtime{
				NetworkID:   networkID,
				ChainID:     testChainID,
				UTXOAssetID: utxoAssetID,
				NodeID:      nodeID,
			}
			signers := [][]*secp256k1.PrivateKey{preFundedKeys}

			validatorWeight := uint64(2026)
			rewardAddress := preFundedKeys[0].Address()
			inputs := []*lux.TransferableInput{{
				UTXOID: lux.UTXOID{
					TxID:        ids.ID{'l', 'p', '0', '1', '8'},
					OutputIndex: 0,
				},
				Asset: lux.Asset{ID: utxoAssetID},
				In: &secp256k1fx.TransferInput{
					Amt:   uint64(5678),
					Input: secp256k1fx.Input{SigIndices: []uint32{0}},
				},
			}}
			outputs := []*lux.TransferableOutput{{
				Asset: lux.Asset{ID: utxoAssetID},
				Out: &secp256k1fx.TransferOutput{
					Amt: uint64(1234),
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{preFundedKeys[0].Address()},
					},
				},
			}}
			stakes := []*lux.TransferableOutput{{
				Asset: lux.Asset{ID: utxoAssetID},
				Out: &stakeable.LockOut{
					Locktime: uint64(clk.Time().Add(time.Second).Unix()),
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: validatorWeight,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{preFundedKeys[0].Address()},
						},
					},
				},
			}}
			addValidatorTx := &AddValidatorTx{
				BaseTx: BaseTx{BaseTx: lux.BaseTx{
					NetworkID:    rt.NetworkID,
					BlockchainID: rt.ChainID,
					Ins:          inputs,
					Outs:         outputs,
				}},
				Validator: Validator{
					NodeID: rt.NodeID,
					Start:  uint64(clk.Time().Unix()),
					End:    uint64(clk.Time().Add(time.Hour).Unix()),
					Wght:   validatorWeight,
				},
				StakeOuts: stakes,
				RewardsOwner: &secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{rewardAddress},
				},
				DelegationShares: reward.PercentDenominator,
			}

			stx, err := NewSigned(addValidatorTx, Codec, signers)
			require.NoError(err)
			require.NoError(stx.SyntacticVerify(rt))
		})
	}
}

func formatPrimaryNetworkID(id uint32) string {
	switch id {
	case 1:
		return "lux-mainnet-1"
	case 2:
		return "lux-testnet-2"
	case 3:
		return "lux-local-3"
	case 1337:
		return "lux-dev-1337"
	default:
		return fmt.Sprintf("primary-networkID-%d", id)
	}
}
