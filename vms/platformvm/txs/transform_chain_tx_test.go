// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"encoding/json"
	"testing"

	"github.com/luxfi/runtime"

	"github.com/luxfi/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/components/verify/verifymock"
	"github.com/luxfi/node/vms/platformvm/reward"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/utils"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/types"
)

func TestTransformChainTxSerialization(t *testing.T) {
	require := require.New(t)

	addr := ids.ShortID{
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x44, 0x55, 0x66, 0x77,
	}

	utxoAssetID, err := ids.FromString("d1Rdokz7Vq8H5aczkwgkiPCCa6JME7yT2xpqgWTfFKWYVsGbG")
	require.NoError(err)

	customAssetID := ids.ID{
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
		0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31,
	}

	txID := ids.ID{
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
	}
	netID := ids.ID{
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
		0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
	}

	simpleTransformTx := &TransformChainTx{
		BaseTx: BaseTx{
			BaseTx: lux.BaseTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: ids.Empty, // Use empty for serialization test
				Outs:         []*lux.TransferableOutput{},
				Ins: []*lux.TransferableInput{
					{
						UTXOID: lux.UTXOID{
							TxID:        txID,
							OutputIndex: 1,
						},
						Asset: lux.Asset{
							ID: utxoAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: 10 * constants.Lux,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{5},
							},
						},
					},
					{
						UTXOID: lux.UTXOID{
							TxID:        txID,
							OutputIndex: 2,
						},
						Asset: lux.Asset{
							ID: customAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: 0xefffffffffffffff,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{0},
							},
						},
					},
				},
				Memo: types.JSONByteSlice{},
			},
		},
		Chain:                    netID,
		AssetID:                  customAssetID,
		InitialSupply:            0x1000000000000000,
		MaximumSupply:            0xffffffffffffffff,
		MinConsumptionRate:       1_000,
		MaxConsumptionRate:       1_000_000,
		MinValidatorStake:        1,
		MaxValidatorStake:        0xffffffffffffffff,
		MinStakeDuration:         1,
		MaxStakeDuration:         365 * 24 * 60 * 60,
		MinDelegationFee:         reward.PercentDenominator,
		MinDelegatorStake:        1,
		MaxValidatorWeightFactor: 1,
		UptimeRequirement:        .95 * reward.PercentDenominator,
		ChainAuth: &secp256k1fx.Input{
			SigIndices: []uint32{3},
		},
	}
	testChainID := ids.Empty // Use empty chain ID for serialization test to match expected bytes
	rt := &runtime.Runtime{
		NetworkID: constants.MainnetID, // Must match tx.NetworkID

		ChainID:  testChainID,
		UTXOAssetID: utxoAssetID,
	}
	require.NoError(simpleTransformTx.SyntacticVerify(rt))

	expectedUnsignedSimpleTransformTxBytes := []byte{
		0x01, 0x00, 0x1c, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00,
		0x00, 0x00, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa,
		0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa,
		0x99, 0x88, 0x01, 0x00, 0x00, 0x00, 0x51, 0xc2, 0x4f, 0xe7, 0xee, 0x02, 0x01, 0xff, 0x0f, 0x33,
		0x5f, 0x51, 0x99, 0x28, 0xdb, 0x6e, 0xef, 0x62, 0x24, 0x25, 0x45, 0x52, 0xc9, 0x6b, 0x6b, 0x42,
		0x5f, 0xbc, 0x18, 0xfa, 0x24, 0x3b, 0x05, 0x00, 0x00, 0x00, 0x80, 0x96, 0x98, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa,
		0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa,
		0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x02, 0x00, 0x00, 0x00, 0x99, 0x77,
		0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77,
		0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x05, 0x00,
		0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xef, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x11, 0x12,
		0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x31, 0x32,
		0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77,
		0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77,
		0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xe8, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, 0x42,
		0x0f, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01, 0x00, 0x00, 0x00, 0x80, 0x33, 0xe1, 0x01, 0x40, 0x42,
		0x0f, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xf0, 0x7e, 0x0e, 0x00, 0x0a,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00,
	}
	var unsignedSimpleTransformTx UnsignedTx = simpleTransformTx
	unsignedSimpleTransformTxBytes, err := Codec.Marshal(CodecVersion, &unsignedSimpleTransformTx)
	require.NoError(err)
	require.Equal(expectedUnsignedSimpleTransformTxBytes, unsignedSimpleTransformTxBytes)

	complexTransformTx := &TransformChainTx{
		BaseTx: BaseTx{
			BaseTx: lux.BaseTx{
				NetworkID:    constants.MainnetID,
				BlockchainID: ids.Empty, // Use empty for serialization test
				Outs: []*lux.TransferableOutput{
					{
						Asset: lux.Asset{
							ID: utxoAssetID,
						},
						Out: &stakeable.LockOut{
							Locktime: 87654321,
							TransferableOut: &secp256k1fx.TransferOutput{
								Amt: 1,
								OutputOwners: secp256k1fx.OutputOwners{
									Locktime:  12345678,
									Threshold: 0,
									Addrs:     []ids.ShortID{},
								},
							},
						},
					},
					{
						Asset: lux.Asset{
							ID: customAssetID,
						},
						Out: &stakeable.LockOut{
							Locktime: 876543210,
							TransferableOut: &secp256k1fx.TransferOutput{
								Amt: 0xffffffffffffffff,
								OutputOwners: secp256k1fx.OutputOwners{
									Locktime:  0,
									Threshold: 1,
									Addrs: []ids.ShortID{
										addr,
									},
								},
							},
						},
					},
				},
				Ins: []*lux.TransferableInput{
					{
						UTXOID: lux.UTXOID{
							TxID:        txID,
							OutputIndex: 1,
						},
						Asset: lux.Asset{
							ID: utxoAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: constants.KiloLux,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{2, 5},
							},
						},
					},
					{
						UTXOID: lux.UTXOID{
							TxID:        txID,
							OutputIndex: 2,
						},
						Asset: lux.Asset{
							ID: customAssetID,
						},
						In: &stakeable.LockIn{
							Locktime: 876543210,
							TransferableIn: &secp256k1fx.TransferInput{
								Amt: 0xefffffffffffffff,
								Input: secp256k1fx.Input{
									SigIndices: []uint32{0},
								},
							},
						},
					},
					{
						UTXOID: lux.UTXOID{
							TxID:        txID,
							OutputIndex: 3,
						},
						Asset: lux.Asset{
							ID: customAssetID,
						},
						In: &secp256k1fx.TransferInput{
							Amt: 0x1000000000000000,
							Input: secp256k1fx.Input{
								SigIndices: []uint32{},
							},
						},
					},
				},
				Memo: types.JSONByteSlice("😅\nwell that's\x01\x23\x45!"),
			},
		},
		Chain:                    netID,
		AssetID:                  customAssetID,
		InitialSupply:            0x1000000000000000,
		MaximumSupply:            0x1000000000000000,
		MinConsumptionRate:       0,
		MaxConsumptionRate:       0,
		MinValidatorStake:        1,
		MaxValidatorStake:        0x1000000000000000,
		MinStakeDuration:         1,
		MaxStakeDuration:         1,
		MinDelegationFee:         0,
		MinDelegatorStake:        0xffffffffffffffff,
		MaxValidatorWeightFactor: 255,
		UptimeRequirement:        0,
		ChainAuth: &secp256k1fx.Input{
			SigIndices: []uint32{},
		},
	}
	lux.SortTransferableOutputs(complexTransformTx.Outs)
	utils.Sort(complexTransformTx.Ins)
	rt2 := &runtime.Runtime{
		NetworkID: constants.MainnetID, // Must match tx.NetworkID

		ChainID:  testChainID,
		UTXOAssetID: utxoAssetID,
	}
	require.NoError(complexTransformTx.SyntacticVerify(rt2))

	expectedUnsignedComplexTransformTxBytes := []byte{
		0x01, 0x00, 0x1c, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x51, 0xc2,
		0x4f, 0xe7, 0xee, 0x02, 0x01, 0xff, 0x0f, 0x33, 0x5f, 0x51, 0x99, 0x28, 0xdb, 0x6e, 0xef, 0x62,
		0x24, 0x25, 0x45, 0x52, 0xc9, 0x6b, 0x6b, 0x42, 0x5f, 0xbc, 0x18, 0xfa, 0x24, 0x3b, 0x16, 0x00,
		0x00, 0x00, 0xb1, 0x7f, 0x39, 0x05, 0x00, 0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x00, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4e, 0x61, 0xbc, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77,
		0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77,
		0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x16, 0x00, 0x00, 0x00, 0xea, 0xfc, 0x3e, 0x34, 0x00, 0x00,
		0x00, 0x00, 0x07, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0x44, 0x55,
		0x66, 0x77, 0x03, 0x00, 0x00, 0x00, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee,
		0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee,
		0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x01, 0x00, 0x00, 0x00, 0x51, 0xc2, 0x4f, 0xe7, 0xee, 0x02,
		0x01, 0xff, 0x0f, 0x33, 0x5f, 0x51, 0x99, 0x28, 0xdb, 0x6e, 0xef, 0x62, 0x24, 0x25, 0x45, 0x52,
		0xc9, 0x6b, 0x6b, 0x42, 0x5f, 0xbc, 0x18, 0xfa, 0x24, 0x3b, 0x05, 0x00, 0x00, 0x00, 0x00, 0xca,
		0x9a, 0x3b, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x05, 0x00,
		0x00, 0x00, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa,
		0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa,
		0x99, 0x88, 0x02, 0x00, 0x00, 0x00, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77,
		0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77,
		0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x15, 0x00, 0x00, 0x00, 0xea, 0xfc, 0x3e, 0x34, 0x00, 0x00,
		0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xef, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee,
		0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0xff, 0xee,
		0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x03, 0x00, 0x00, 0x00, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33,
		0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33,
		0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x05, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x14, 0x00, 0x00, 0x00, 0xf0, 0x9f,
		0x98, 0x85, 0x0a, 0x77, 0x65, 0x6c, 0x6c, 0x20, 0x74, 0x68, 0x61, 0x74, 0x27, 0x73, 0x01, 0x23,
		0x45, 0x21, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16,
		0x17, 0x18, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36,
		0x37, 0x38, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33,
		0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33, 0x55, 0x31, 0x99, 0x77, 0x55, 0x77, 0x11, 0x33,
		0x55, 0x31, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x10, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00, 0x0a, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00,
	}
	var unsignedComplexTransformTx UnsignedTx = complexTransformTx
	unsignedComplexTransformTxBytes, err := Codec.Marshal(CodecVersion, &unsignedComplexTransformTx)
	require.NoError(err)
	require.Equal(expectedUnsignedComplexTransformTxBytes, unsignedComplexTransformTxBytes)

	// Remove aliaser as BCLookup field doesn't exist in runtime.Runtime
	// This functionality is now handled differently

	rt3 := &runtime.Runtime{
		NetworkID: constants.MainnetID, // Must match tx.NetworkID

		ChainID:  testChainID,
		UTXOAssetID: utxoAssetID,
	}
	unsignedComplexTransformTx.InitRuntime(rt3)

	unsignedComplexTransformTxJSONBytes, err := json.MarshalIndent(unsignedComplexTransformTx, "", "\t")
	require.NoError(err)
	require.JSONEq(`{
	"networkID": 1,
	"blockchainID": "11111111111111111111111111111111LpoYY",
	"outputs": [
		{
			"assetID": "d1Rdokz7Vq8H5aczkwgkiPCCa6JME7yT2xpqgWTfFKWYVsGbG",
			"fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
			"output": {
				"locktime": 87654321,
				"output": {
					"addresses": [],
					"amount": 1,
					"locktime": 12345678,
					"threshold": 0
				}
			}
		},
		{
			"assetID": "2Ab62uWwJw1T6VvmKD36ufsiuGZuX1pGykXAvPX1LtjTRHxwcc",
			"fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
			"output": {
				"locktime": 876543210,
				"output": {
					"addresses": [
						"7EKFm18KvWqcxMCNgpBSN51pJnEr1cVUb"
					],
					"amount": 18446744073709551615,
					"locktime": 0,
					"threshold": 1
				}
			}
		}
	],
	"inputs": [
		{
			"txID": "2wiU5PnFTjTmoAXGZutHAsPF36qGGyLHYHj9G1Aucfmb3JFFGN",
			"outputIndex": 1,
			"assetID": "d1Rdokz7Vq8H5aczkwgkiPCCa6JME7yT2xpqgWTfFKWYVsGbG",
			"fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
			"input": {
				"amount": 1000000000,
				"signatureIndices": [
					2,
					5
				]
			}
		},
		{
			"txID": "2wiU5PnFTjTmoAXGZutHAsPF36qGGyLHYHj9G1Aucfmb3JFFGN",
			"outputIndex": 2,
			"assetID": "2Ab62uWwJw1T6VvmKD36ufsiuGZuX1pGykXAvPX1LtjTRHxwcc",
			"fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
			"input": {
				"locktime": 876543210,
				"input": {
					"amount": 17293822569102704639,
					"signatureIndices": [
						0
					]
				}
			}
		},
		{
			"txID": "2wiU5PnFTjTmoAXGZutHAsPF36qGGyLHYHj9G1Aucfmb3JFFGN",
			"outputIndex": 3,
			"assetID": "2Ab62uWwJw1T6VvmKD36ufsiuGZuX1pGykXAvPX1LtjTRHxwcc",
			"fxID": "spdxUxVJQbX85MGxMHbKw1sHxMnSqJ3QBzDyDYEP3h6TLuxqQ",
			"input": {
				"amount": 1152921504606846976,
				"signatureIndices": []
			}
		}
	],
	"memo": "0xf09f98850a77656c6c2074686174277301234521",
	"chainID": "SkB92YpWm4UpburLz9tEKZw2i67H3FF6YkjaU4BkFUDTG9Xm",
	"assetID": "2Ab62uWwJw1T6VvmKD36ufsiuGZuX1pGykXAvPX1LtjTRHxwcc",
	"initialSupply": 1152921504606846976,
	"maximumSupply": 1152921504606846976,
	"minConsumptionRate": 0,
	"maxConsumptionRate": 0,
	"minValidatorStake": 1,
	"maxValidatorStake": 1152921504606846976,
	"minStakeDuration": 1,
	"maxStakeDuration": 1,
	"minDelegationFee": 0,
	"minDelegatorStake": 18446744073709551615,
	"maxValidatorWeightFactor": 255,
	"uptimeRequirement": 0,
	"chainAuthorization": {
		"signatureIndices": []
	}
}`, string(unsignedComplexTransformTxJSONBytes))
}

func TestTransformChainTxSyntacticVerify(t *testing.T) {
	type test struct {
		name   string
		txFunc func(*gomock.Controller) *TransformChainTx
		err    error
	}

	var (
		networkID = uint32(1337)
		chainID   = ids.GenerateTestID()
		utxoAssetID  = ids.GenerateTestID()
	)

	rt := &runtime.Runtime{
		NetworkID: networkID, // Must match tx.NetworkID

		ChainID:  chainID,
		UTXOAssetID: utxoAssetID,
	}

	// A BaseTx that already passed syntactic verification.
	verifiedBaseTx := BaseTx{
		SyntacticallyVerified: true,
	}

	// A BaseTx that passes syntactic verification.
	validBaseTx := BaseTx{
		BaseTx: lux.BaseTx{
			NetworkID:    networkID,
			BlockchainID: chainID,
		},
	}

	// A BaseTx that fails syntactic verification.
	invalidBaseTx := BaseTx{}

	tests := []test{
		{
			name: "nil tx",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return nil
			},
			err: ErrNilTx,
		},
		{
			name: "already verified",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx: verifiedBaseTx,
				}
			},
			err: nil,
		},
		{
			name: "invalid netID",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx: validBaseTx,
					Chain:  constants.PrimaryNetworkID,
				}
			},
			err: errCantTransformPrimaryNetwork,
		},
		{
			name: "empty assetID",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:  validBaseTx,
					Chain:   ids.GenerateTestID(),
					AssetID: ids.Empty,
				}
			},
			err: errEmptyAssetID,
		},
		{
			name: "LUX assetID",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:        validBaseTx,
					Chain:         ids.GenerateTestID(),
					AssetID:       utxoAssetID,
					InitialSupply: 1, // Non-zero to hit LUX assetID error first
					MaximumSupply: 1,
				}
			},
			err: errAssetIDCantBeLUX,
		},
		{
			name: "initialSupply == 0",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:        validBaseTx,
					Chain:         ids.GenerateTestID(),
					AssetID:       ids.GenerateTestID(),
					InitialSupply: 0,
				}
			},
			err: errInitialSupplyZero,
		},
		{
			name: "initialSupply > maximumSupply",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:        validBaseTx,
					Chain:         ids.GenerateTestID(),
					AssetID:       ids.GenerateTestID(),
					InitialSupply: 2,
					MaximumSupply: 1,
				}
			},
			err: errInitialSupplyGreaterThanMaxSupply,
		},
		{
			name: "minConsumptionRate > maxConsumptionRate",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:             validBaseTx,
					Chain:              ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      1,
					MaximumSupply:      1,
					MinConsumptionRate: 2,
					MaxConsumptionRate: 1,
				}
			},
			err: errMinConsumptionRateTooLarge,
		},
		{
			name: "maxConsumptionRate > 100%",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:             validBaseTx,
					Chain:              ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      1,
					MaximumSupply:      1,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator + 1,
				}
			},
			err: errMaxConsumptionRateTooLarge,
		},
		{
			name: "minValidatorStake == 0",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:             validBaseTx,
					Chain:              ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      1,
					MaximumSupply:      1,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  0,
				}
			},
			err: errMinValidatorStakeZero,
		},
		{
			name: "minValidatorStake > initialSupply",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:             validBaseTx,
					Chain:              ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      1,
					MaximumSupply:      1,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
				}
			},
			err: errMinValidatorStakeAboveSupply,
		},
		{
			name: "minValidatorStake > maxValidatorStake",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:             validBaseTx,
					Chain:              ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  1,
				}
			},
			err: errMinValidatorStakeAboveMax,
		},
		{
			name: "maxValidatorStake > maximumSupply",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:             validBaseTx,
					Chain:              ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  11,
				}
			},
			err: errMaxValidatorStakeTooLarge,
		},
		{
			name: "minStakeDuration == 0",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:             validBaseTx,
					Chain:              ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  10,
					MinStakeDuration:   0,
				}
			},
			err: errMinStakeDurationZero,
		},
		{
			name: "minStakeDuration > maxStakeDuration",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:             validBaseTx,
					Chain:              ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  10,
					MinStakeDuration:   2,
					MaxStakeDuration:   1,
				}
			},
			err: errMinStakeDurationTooLarge,
		},
		{
			name: "minDelegationFee > 100%",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:             validBaseTx,
					Chain:              ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  10,
					MinStakeDuration:   1,
					MaxStakeDuration:   2,
					MinDelegationFee:   reward.PercentDenominator + 1,
				}
			},
			err: errMinDelegationFeeTooLarge,
		},
		{
			name: "minDelegatorStake == 0",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:             validBaseTx,
					Chain:              ids.GenerateTestID(),
					AssetID:            ids.GenerateTestID(),
					InitialSupply:      10,
					MaximumSupply:      10,
					MinConsumptionRate: 0,
					MaxConsumptionRate: reward.PercentDenominator,
					MinValidatorStake:  2,
					MaxValidatorStake:  10,
					MinStakeDuration:   1,
					MaxStakeDuration:   2,
					MinDelegationFee:   reward.PercentDenominator,
					MinDelegatorStake:  0,
				}
			},
			err: errMinDelegatorStakeZero,
		},
		{
			name: "maxValidatorWeightFactor == 0",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:                   validBaseTx,
					Chain:                    ids.GenerateTestID(),
					AssetID:                  ids.GenerateTestID(),
					InitialSupply:            10,
					MaximumSupply:            10,
					MinConsumptionRate:       0,
					MaxConsumptionRate:       reward.PercentDenominator,
					MinValidatorStake:        2,
					MaxValidatorStake:        10,
					MinStakeDuration:         1,
					MaxStakeDuration:         2,
					MinDelegationFee:         reward.PercentDenominator,
					MinDelegatorStake:        1,
					MaxValidatorWeightFactor: 0,
				}
			},
			err: errMaxValidatorWeightFactorZero,
		},
		{
			name: "uptimeRequirement > 100%",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:                   validBaseTx,
					Chain:                    ids.GenerateTestID(),
					AssetID:                  ids.GenerateTestID(),
					InitialSupply:            10,
					MaximumSupply:            10,
					MinConsumptionRate:       0,
					MaxConsumptionRate:       reward.PercentDenominator,
					MinValidatorStake:        2,
					MaxValidatorStake:        10,
					MinStakeDuration:         1,
					MaxStakeDuration:         2,
					MinDelegationFee:         reward.PercentDenominator,
					MinDelegatorStake:        1,
					MaxValidatorWeightFactor: 1,
					UptimeRequirement:        reward.PercentDenominator + 1,
				}
			},
			err: errUptimeRequirementTooLarge,
		},
		{
			name: "invalid chainAuth",
			txFunc: func(ctrl *gomock.Controller) *TransformChainTx {
				// This NetAuth fails verification.
				invalidNetAuth := verifymock.NewVerifiable(ctrl)
				invalidNetAuth.EXPECT().Verify().Return(errInvalidNetAuth)
				return &TransformChainTx{
					BaseTx:                   validBaseTx,
					Chain:                    ids.GenerateTestID(),
					AssetID:                  ids.GenerateTestID(),
					InitialSupply:            10,
					MaximumSupply:            10,
					MinConsumptionRate:       0,
					MaxConsumptionRate:       reward.PercentDenominator,
					MinValidatorStake:        2,
					MaxValidatorStake:        10,
					MinStakeDuration:         1,
					MaxStakeDuration:         2,
					MinDelegationFee:         reward.PercentDenominator,
					MinDelegatorStake:        1,
					MaxValidatorWeightFactor: 1,
					UptimeRequirement:        reward.PercentDenominator,
					ChainAuth:                invalidNetAuth,
				}
			},
			err: errInvalidNetAuth,
		},
		{
			name: "invalid BaseTx",
			txFunc: func(*gomock.Controller) *TransformChainTx {
				return &TransformChainTx{
					BaseTx:                   invalidBaseTx,
					Chain:                    ids.GenerateTestID(),
					AssetID:                  ids.GenerateTestID(),
					InitialSupply:            10,
					MaximumSupply:            10,
					MinConsumptionRate:       0,
					MaxConsumptionRate:       reward.PercentDenominator,
					MinValidatorStake:        2,
					MaxValidatorStake:        10,
					MinStakeDuration:         1,
					MaxStakeDuration:         2,
					MinDelegationFee:         reward.PercentDenominator,
					MinDelegatorStake:        1,
					MaxValidatorWeightFactor: 1,
					UptimeRequirement:        reward.PercentDenominator,
				}
			},
			err: lux.ErrWrongNetworkID,
		},
		{
			name: "passes verification",
			txFunc: func(ctrl *gomock.Controller) *TransformChainTx {
				// This NetAuth passes verification.
				validNetAuth := verifymock.NewVerifiable(ctrl)
				validNetAuth.EXPECT().Verify().Return(nil)
				return &TransformChainTx{
					BaseTx:                   validBaseTx,
					Chain:                    ids.GenerateTestID(),
					AssetID:                  ids.GenerateTestID(),
					InitialSupply:            10,
					MaximumSupply:            10,
					MinConsumptionRate:       0,
					MaxConsumptionRate:       reward.PercentDenominator,
					MinValidatorStake:        2,
					MaxValidatorStake:        10,
					MinStakeDuration:         1,
					MaxStakeDuration:         2,
					MinDelegationFee:         reward.PercentDenominator,
					MinDelegatorStake:        1,
					MaxValidatorWeightFactor: 1,
					UptimeRequirement:        reward.PercentDenominator,
					ChainAuth:                validNetAuth,
				}
			},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			tx := tt.txFunc(ctrl)
			err := tx.SyntacticVerify(rt)
			require.ErrorIs(t, err, tt.err)
		})
	}
}
