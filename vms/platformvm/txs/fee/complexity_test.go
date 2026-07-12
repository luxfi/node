// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/platformvm/txs"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// Post-LP-023 notes for this file:
//
//   - The pre-LP-023 whole-tx complexity/fee tests parsed hard-coded
//     big-endian linearcodec hex fixtures (txTests, still defined in
//     calculator_test.go) via txs.Parse(txs.Codec, ...). That codec is
//     gone (struct-is-wire ZAP), and the BE hex no longer parses.
//     TestTxComplexity below rebuilds representative txs natively through
//     the New*Tx constructors and exercises the same visitor + batch +
//     ErrUnsupportedTx paths.
//   - The per-case codec byte cross-check (txs.Codec.Marshal(...) vs
//     actual[gas.Bandwidth]) is removed: it tied the intrinsic fee model to
//     the legacy linearcodec object length. The intrinsic model is a
//     protocol cost constant (Output/Input/Owner/Auth/Signer bandwidth is
//     still asserted below against the expected dimensions), decoupled from
//     the physical ZAP object framing (header + pointer indirection), so
//     there is no equivalent standalone-component marshal to compare
//     against. The expected-dimension assertions are the real coverage of
//     the complexity functions and are preserved in full.
//   - TestConvertNetworkToL1ValidatorComplexity was removed: both the
//     txs.ConvertNetworkToL1Validator type and the
//     ConvertNetworkToL1ValidatorComplexity function it exercised were
//     deleted in the native-wire cutover. Per-validator convert complexity
//     is now computed inline by complexityVisitor.ConvertNetworkTx (over
//     txs.NetworkValidator); there is no exported per-validator entry point
//     to unit test in isolation.

// feeSpendBase is a minimal, well-typed spending envelope (1 input with 1
// signature, 1 output with 1 owner) used to construct native-wire txs for
// the whole-tx complexity/fee tests. Constructors do not run SyntacticVerify,
// so the envelope only needs to be structurally valid.
func feeSpendBase() *lux.BaseTx {
	assetID := ids.GenerateTestID()
	return &lux.BaseTx{
		NetworkID:    10,
		BlockchainID: ids.GenerateTestID(),
		Outs: []*lux.TransferableOutput{{
			Asset: lux.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1000,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				},
			},
		}},
		Ins: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 0},
			Asset:  lux.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt:   1000,
				Input: secp256k1fx.Input{SigIndices: []uint32{0}},
			},
		}},
	}
}

func feeBaseTx(t *testing.T) *txs.BaseTx {
	t.Helper()
	tx, err := txs.NewBaseTx(feeSpendBase())
	require.NoError(t, err)
	return tx
}

func feeCreateChainTx(t *testing.T) *txs.CreateChainTx {
	t.Helper()
	tx, err := txs.NewCreateChainTx(
		feeSpendBase(),
		ids.GenerateTestID(), // chainID
		"chain",
		ids.GenerateTestID(),                        // vmID
		nil,                                         // fxIDs
		[]byte("genesis"),                           // genesisData
		&secp256k1fx.Input{SigIndices: []uint32{0}}, // chainAuth
	)
	require.NoError(t, err)
	return tx
}

// TestTxComplexity exercises the whole-tx complexity visitor over
// natively-built txs (replacing the dead BE hex-fixture path): supported tx
// types yield non-zero bandwidth and compose additively in batch mode, and
// unsupported tx types are refused with ErrUnsupportedTx.
func TestTxComplexity(t *testing.T) {
	require := require.New(t)

	supported := []txs.UnsignedTx{
		feeBaseTx(t),
		feeCreateChainTx(t),
	}

	// Individual: every supported tx computes without error and consumes
	// bandwidth.
	var want gas.Dimensions
	for _, utx := range supported {
		c, err := TxComplexity(utx)
		require.NoError(err)
		require.NotZero(c[gas.Bandwidth])

		want, err = want.Add(&c)
		require.NoError(err)
	}

	// Batch: the variadic call equals the running sum of the individual
	// complexities.
	batch, err := TxComplexity(supported...)
	require.NoError(err)
	require.Equal(want, batch)

	// Unsupported: AdvanceTime / RewardValidator are refused.
	for _, utx := range []txs.UnsignedTx{
		txs.NewAdvanceTimeTx(1),
		txs.NewRewardValidatorTx(ids.GenerateTestID()),
	} {
		_, err := TxComplexity(utx)
		require.ErrorIs(err, ErrUnsupportedTx)
	}
}

func TestOutputComplexity(t *testing.T) {
	tests := []struct {
		name        string
		out         *lux.TransferableOutput
		expected    gas.Dimensions
		expectedErr error
	}{
		{
			name: "any can spend",
			out: &lux.TransferableOutput{
				Out: &secp256k1fx.TransferOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Addrs: make([]ids.ShortID, 0),
					},
				},
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 60,
				gas.DBWrite:   1,
			},
			expectedErr: nil,
		},
		{
			name: "one owner",
			out: &lux.TransferableOutput{
				Out: &secp256k1fx.TransferOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Addrs: make([]ids.ShortID, 1),
					},
				},
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 80,
				gas.DBWrite:   1,
			},
			expectedErr: nil,
		},
		{
			name: "three owners",
			out: &lux.TransferableOutput{
				Out: &secp256k1fx.TransferOutput{
					OutputOwners: secp256k1fx.OutputOwners{
						Addrs: make([]ids.ShortID, 3),
					},
				},
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 120,
				gas.DBWrite:   1,
			},
			expectedErr: nil,
		},
		{
			name: "locked stakeable",
			out: &lux.TransferableOutput{
				Out: &stakeable.LockOut{
					TransferableOut: &secp256k1fx.TransferOutput{
						OutputOwners: secp256k1fx.OutputOwners{
							Addrs: make([]ids.ShortID, 3),
						},
					},
				},
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 132,
				gas.DBWrite:   1,
			},
			expectedErr: nil,
		},
		{
			name: "invalid output type",
			out: &lux.TransferableOutput{
				Out: nil,
			},
			expected:    gas.Dimensions{},
			expectedErr: errUnsupportedOutput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := OutputComplexity(test.out)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}

func TestInputComplexity(t *testing.T) {
	tests := []struct {
		name        string
		in          *lux.TransferableInput
		cred        verify.Verifiable
		expected    gas.Dimensions
		expectedErr error
	}{
		{
			name: "any can spend",
			in: &lux.TransferableInput{
				In: &secp256k1fx.TransferInput{
					Input: secp256k1fx.Input{
						SigIndices: make([]uint32, 0),
					},
				},
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 0),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 92,
				gas.DBRead:    1,
				gas.DBWrite:   1,
			},
			expectedErr: nil,
		},
		{
			name: "one owner",
			in: &lux.TransferableInput{
				In: &secp256k1fx.TransferInput{
					Input: secp256k1fx.Input{
						SigIndices: make([]uint32, 1),
					},
				},
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 1),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 161,
				gas.DBRead:    1,
				gas.DBWrite:   1,
				gas.Compute:   200,
			},
			expectedErr: nil,
		},
		{
			name: "three owners",
			in: &lux.TransferableInput{
				In: &secp256k1fx.TransferInput{
					Input: secp256k1fx.Input{
						SigIndices: make([]uint32, 3),
					},
				},
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 3),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 299,
				gas.DBRead:    1,
				gas.DBWrite:   1,
				gas.Compute:   600,
			},
			expectedErr: nil,
		},
		{
			name: "locked stakeable",
			in: &lux.TransferableInput{
				In: &stakeable.LockIn{
					TransferableIn: &secp256k1fx.TransferInput{
						Input: secp256k1fx.Input{
							SigIndices: make([]uint32, 3),
						},
					},
				},
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 3),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 311,
				gas.DBRead:    1,
				gas.DBWrite:   1,
				gas.Compute:   600,
			},
			expectedErr: nil,
		},
		{
			name: "invalid input type",
			in: &lux.TransferableInput{
				In: nil,
			},
			cred:        nil,
			expected:    gas.Dimensions{},
			expectedErr: errUnsupportedInput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := InputComplexity(test.in)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}

func TestOwnerComplexity(t *testing.T) {
	tests := []struct {
		name        string
		owner       fx.Owner
		expected    gas.Dimensions
		expectedErr error
	}{
		{
			name: "any can spend",
			owner: &secp256k1fx.OutputOwners{
				Addrs: make([]ids.ShortID, 0),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 16,
			},
			expectedErr: nil,
		},
		{
			name: "one owner",
			owner: &secp256k1fx.OutputOwners{
				Addrs: make([]ids.ShortID, 1),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 36,
			},
			expectedErr: nil,
		},
		{
			name: "three owners",
			owner: &secp256k1fx.OutputOwners{
				Addrs: make([]ids.ShortID, 3),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 76,
			},
			expectedErr: nil,
		},
		{
			name:        "invalid owner type",
			owner:       nil,
			expected:    gas.Dimensions{},
			expectedErr: errUnsupportedOwner,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := OwnerComplexity(test.owner)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}

func TestAuthComplexity(t *testing.T) {
	tests := []struct {
		name        string
		auth        verify.Verifiable
		cred        verify.Verifiable
		expected    gas.Dimensions
		expectedErr error
	}{
		{
			name: "any can spend",
			auth: &secp256k1fx.Input{
				SigIndices: make([]uint32, 0),
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 0),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 8,
			},
			expectedErr: nil,
		},
		{
			name: "one owner",
			auth: &secp256k1fx.Input{
				SigIndices: make([]uint32, 1),
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 1),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 77,
				gas.Compute:   200,
			},
			expectedErr: nil,
		},
		{
			name: "three owners",
			auth: &secp256k1fx.Input{
				SigIndices: make([]uint32, 3),
			},
			cred: &secp256k1fx.Credential{
				Sigs: make([][secp256k1.SignatureLen]byte, 3),
			},
			expected: gas.Dimensions{
				gas.Bandwidth: 215,
				gas.Compute:   600,
			},
			expectedErr: nil,
		},
		{
			name:        "invalid auth type",
			auth:        nil,
			cred:        nil,
			expected:    gas.Dimensions{},
			expectedErr: errUnsupportedAuth,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := AuthComplexity(test.auth)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}

func TestSignerComplexity(t *testing.T) {
	tests := []struct {
		name        string
		signer      signer.Signer
		expected    gas.Dimensions
		expectedErr error
	}{
		{
			name:        "empty",
			signer:      &signer.Empty{},
			expected:    gas.Dimensions{},
			expectedErr: nil,
		},
		{
			name:   "bls pop",
			signer: &signer.ProofOfPossession{},
			expected: gas.Dimensions{
				gas.Bandwidth: 144,
				gas.Compute:   1050,
			},
			expectedErr: nil,
		},
		{
			name:        "invalid signer type",
			signer:      nil,
			expected:    gas.Dimensions{},
			expectedErr: errUnsupportedSigner,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := SignerComplexity(test.signer)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}
