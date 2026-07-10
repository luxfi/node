// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// roundTrip marshals u into a signed Tx, parses it back from bytes, and returns
// the decoded unsigned tx — proving the struct-is-wire path (no codec).
func roundTrip(t *testing.T, u UnsignedTx) UnsignedTx {
	t.Helper()
	require := require.New(t)
	signed := &Tx{Unsigned: u}
	require.NoError(signed.Initialize())
	got, err := Parse(signed.Bytes())
	require.NoError(err)
	// Unsigned bytes are a genuine prefix of signed bytes.
	require.Equal(u.Bytes(), signed.Bytes()[:len(u.Bytes())])
	return got.Unsigned
}

func spendBase() *lux.BaseTx {
	asset := ids.GenerateTestID()
	a0, a1, a2 := ids.GenerateTestShortID(), ids.GenerateTestShortID(), ids.GenerateTestShortID()
	return &lux.BaseTx{
		NetworkID:    96369,
		BlockchainID: ids.GenerateTestID(),
		Outs: []*lux.TransferableOutput{
			{Asset: lux.Asset{ID: asset}, Out: &secp256k1fx.TransferOutput{
				Amt: 1_000_000, OutputOwners: secp256k1fx.OutputOwners{Locktime: 7, Threshold: 2, Addrs: []ids.ShortID{a0, a1, a2}}}},
			{Asset: lux.Asset{ID: asset}, Out: &stakeable.LockOut{Locktime: 999, TransferableOut: &secp256k1fx.TransferOutput{
				Amt: 42, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{a0}}}}},
		},
		Ins: []*lux.TransferableInput{
			{UTXOID: lux.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 1}, Asset: lux.Asset{ID: asset},
				In: &stakeable.LockIn{Locktime: 999, TransferableIn: &secp256k1fx.TransferInput{Amt: 1_000_042, Input: secp256k1fx.Input{SigIndices: []uint32{0, 1}}}}},
		},
		Memo: []byte("native-zap"),
	}
}

func TestRoundTrip_AdvanceTime(t *testing.T) {
	require := require.New(t)
	got := roundTrip(t, NewAdvanceTimeTx(1_766_708_400)).(*AdvanceTimeTx)
	require.EqualValues(1_766_708_400, got.Time())
}

func TestRoundTrip_BaseTx_MultisigStakeable(t *testing.T) {
	require := require.New(t)
	base := spendBase()
	in, err := NewBaseTx(base)
	require.NoError(err)
	got := roundTrip(t, in).(*BaseTx)
	require.Equal(base.NetworkID, got.NetworkID())
	require.Equal(base.BlockchainID, got.BlockchainID())
	require.Equal(base.Outs, got.Outputs(), "multisig + stakeable outputs round-trip")
	require.Equal(base.Ins, got.Inputs(), "stakeable input round-trips")
	require.Equal([]byte(base.Memo), got.Memo())
}

func sampleConvertValidator() *ConvertNetworkToL1Validator {
	nodeID := ids.GenerateTestNodeID()
	v := &ConvertNetworkToL1Validator{
		NodeID:  nodeID[:],
		Weight:  1000,
		Balance: 500,
		RemainingBalanceOwner: message.PChainOwner{Threshold: 1, Addresses: []ids.ShortID{ids.GenerateTestShortID()}},
		DeactivationOwner:     message.PChainOwner{Threshold: 2, Addresses: []ids.ShortID{ids.GenerateTestShortID(), ids.GenerateTestShortID()}},
	}
	for i := range v.Signer.PublicKey {
		v.Signer.PublicKey[i] = byte(i)
	}
	for i := range v.Signer.ProofOfPossession {
		v.Signer.ProofOfPossession[i] = byte(255 - i)
	}
	return v
}

func TestRoundTrip_ConvertNetworkToL1(t *testing.T) {
	require := require.New(t)
	base := spendBase()
	vdr := sampleConvertValidator()
	in, err := NewConvertNetworkToL1Tx(base, ids.GenerateTestID(), ids.GenerateTestID(), []byte("0xmanager"),
		[]*ConvertNetworkToL1Validator{vdr}, &secp256k1fx.Input{SigIndices: []uint32{0}})
	require.NoError(err)
	got := roundTrip(t, in).(*ConvertNetworkToL1Tx)
	require.Equal(in.Chain(), got.Chain())
	require.Equal(in.ManagerChainID(), got.ManagerChainID())
	require.Equal([]byte("0xmanager"), got.Address())
	require.Equal([]*ConvertNetworkToL1Validator{vdr}, got.Validators(), "nested validator (NodeID+BLS PoP+2 owners) round-trips")
	require.Equal(in.ChainAuth(), got.ChainAuth())
}

func TestRoundTrip_CreateSovereignL1(t *testing.T) {
	require := require.New(t)
	base := spendBase()
	vdr := sampleConvertValidator()
	var owner fx.Owner = &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}
	chains := []*SovereignL1Chain{
		{BlockchainName: "evm chain", VMID: ids.GenerateTestID(), FxIDs: []ids.ID{ids.GenerateTestID()}, GenesisData: []byte(`{"g":1}`)},
	}
	in, err := NewCreateSovereignL1Tx(base, owner, []*ConvertNetworkToL1Validator{vdr}, chains, 0, []byte("mgr"))
	require.NoError(err)
	got := roundTrip(t, in).(*CreateSovereignL1Tx)
	require.Equal(owner, got.Owner())
	require.Equal([]*ConvertNetworkToL1Validator{vdr}, got.Validators())
	require.Equal(chains, got.Chains(), "sovereign L1 chain manifest round-trips")
	require.EqualValues(0, got.ManagerChainIdx())
	require.Equal([]byte("mgr"), got.ManagerAddress())
}

func TestRoundTrip_SignedWithCreds(t *testing.T) {
	require := require.New(t)
	in, err := NewBaseTx(spendBase())
	require.NoError(err)

	var s0, s1 [sigLen]byte
	for i := range s0 {
		s0[i], s1[i] = byte(i), byte(255-i)
	}
	signed := &Tx{Unsigned: in, Creds: []verify.Verifiable{&secp256k1fx.Credential{Sigs: [][sigLen]byte{s0, s1}}}}
	require.NoError(signed.Initialize())

	got, err := Parse(signed.Bytes())
	require.NoError(err)
	require.Equal(signed.Creds, got.Creds, "credentials round-trip through unsigned‖creds")
	require.Equal(signed.TxID, got.TxID)
}

var _ = signer.ProofOfPossession{}
