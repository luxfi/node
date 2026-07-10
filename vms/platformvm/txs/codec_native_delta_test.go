// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/fx"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

func deltaBase() BaseTx {
	return BaseTx{BaseTx: lux.BaseTx{
		NetworkID:    96369,
		BlockchainID: ids.GenerateTestID(),
		Memo:         []byte("d"),
	}}
}

// roundTripUnsigned marshals an unsigned tx through the native manager and
// decodes it back, returning the decoded UnsignedTx.
func roundTripUnsigned(t *testing.T, in UnsignedTx) UnsignedTx {
	t.Helper()
	require := require.New(t)
	b, err := nativeCodec.Marshal(CodecVersion, in)
	require.NoError(err)
	var u UnsignedTx
	_, err = nativeCodec.Unmarshal(b, &u)
	require.NoError(err)
	return u
}

func TestNative_IncreaseL1Balance_RoundTrip(t *testing.T) {
	require := require.New(t)
	in := &IncreaseL1ValidatorBalanceTx{
		BaseTx: deltaBase(), ValidationID: ids.GenerateTestID(), Balance: 7_777,
	}
	got := roundTripUnsigned(t, in).(*IncreaseL1ValidatorBalanceTx)
	require.Equal(in.ValidationID, got.ValidationID)
	require.Equal(in.Balance, got.Balance)
	require.Equal(in.NetworkID, got.NetworkID)
	require.Equal(in.BlockchainID, got.BlockchainID)
}

func TestNative_SetL1Weight_RoundTrip(t *testing.T) {
	require := require.New(t)
	in := &SetL1ValidatorWeightTx{BaseTx: deltaBase(), Message: []byte("warp-weight-msg")}
	got := roundTripUnsigned(t, in).(*SetL1ValidatorWeightTx)
	require.Equal([]byte(in.Message), []byte(got.Message))
}

func TestNative_DisableL1_RoundTrip(t *testing.T) {
	require := require.New(t)
	in := &DisableL1ValidatorTx{
		BaseTx:       deltaBase(),
		ValidationID: ids.GenerateTestID(),
		DisableAuth:  &secp256k1fx.Input{SigIndices: []uint32{0, 3, 7}},
	}
	got := roundTripUnsigned(t, in).(*DisableL1ValidatorTx)
	require.Equal(in.ValidationID, got.ValidationID)
	require.Equal(in.DisableAuth, got.DisableAuth)
}

func TestNative_RemoveChainValidator_RoundTrip(t *testing.T) {
	require := require.New(t)
	in := &RemoveChainValidatorTx{
		BaseTx:    deltaBase(),
		NodeID:    ids.GenerateTestNodeID(),
		Chain:     ids.GenerateTestID(),
		ChainAuth: &secp256k1fx.Input{SigIndices: []uint32{1}},
	}
	got := roundTripUnsigned(t, in).(*RemoveChainValidatorTx)
	require.Equal(in.NodeID, got.NodeID)
	require.Equal(in.Chain, got.Chain)
	require.Equal(in.ChainAuth, got.ChainAuth)
}

func TestNative_CreateNetwork_RoundTrip(t *testing.T) {
	require := require.New(t)
	var owner fx.Owner = &secp256k1fx.OutputOwners{
		Locktime:  5,
		Threshold: 2,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID(), ids.GenerateTestShortID()},
	}
	in := &CreateNetworkTx{BaseTx: deltaBase(), Owner: owner}
	got := roundTripUnsigned(t, in).(*CreateNetworkTx)
	require.Equal(in.Owner, got.Owner)
}
