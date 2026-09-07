// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/txs"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// admitAll is a TxVerifier that accepts everything, so a refusal observed
// through the gate can only have come from the gate.
type admitAll struct{}

func (admitAll) VerifyTx(*txs.Tx) error { return nil }

// TestGateAdmitsCreateChainTx pins the gate against the P-chain tx kinds whose
// discriminator byte collides with a gated zap_native kind. Both namespaces put
// their discriminator at object byte 0 and both are zap Version2 buffers, so
// wrapping node tx bytes as a zap_native tx succeeds on the byte value alone:
// txs.kindCreateChain is 7 and so is zap_native.TxKindRegisterL1Validator.
func TestGateAdmitsCreateChainTx(t *testing.T) {
	require := require.New(t)

	base := &lux.BaseTx{NetworkID: constants.UnitTestID, BlockchainID: ids.GenerateTestID()}
	auth := &secp256k1fx.Input{SigIndices: []uint32{0}}
	unsigned, err := txs.NewCreateChainTx(
		base,
		ids.GenerateTestID(),
		"test-chain",
		constants.XVMID,
		nil,
		nil,
		auth,
	)
	require.NoError(err)

	tx, err := txs.NewSigned(unsigned, nil)
	require.NoError(err)
	require.NotEmpty(tx.Bytes())

	gate := NewZapNativeAdmissionGate(admitAll{})
	require.NoError(gate.VerifyTx(tx), "the gate refused a CreateChainTx")
}
