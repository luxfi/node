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

func sampleNetworkValidator() *NetworkValidator {
	nodeID := ids.GenerateTestNodeID()
	v := &NetworkValidator{
		NodeID:                nodeID[:],
		Weight:                1000,
		Balance:               500,
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

// TestRoundTrip_CreateNetwork_SovereignL1 exercises the folded one-tx L1 birth:
// parent=Primary, own validator set, genesis chains, manager — all round-trip.
func TestRoundTrip_CreateNetwork_SovereignL1(t *testing.T) {
	require := require.New(t)
	base := spendBase()
	vdr := sampleNetworkValidator()
	var owner fx.Owner = &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}
	chains := []*NetworkChain{
		{BlockchainName: "evm chain", VMID: ids.GenerateTestID(), FxIDs: []ids.ID{ids.GenerateTestID()}, GenesisData: []byte(`{"g":1}`)},
	}
	in, err := NewCreateNetworkTx(base, ids.Empty /*Primary parent*/, owner, SecuritySovereign,
		[]*NetworkValidator{vdr}, chains, 0, []byte("mgr"))
	require.NoError(err)
	got := roundTrip(t, in).(*CreateNetworkTx)
	require.Equal(ids.Empty, got.Parent())
	require.Equal(owner, got.Owner())
	require.True(got.Sovereign())
	require.Equal([]*NetworkValidator{vdr}, got.Validators(), "genesis validator set round-trips")
	require.Equal(chains, got.Chains(), "genesis chain manifest round-trips")
	require.EqualValues(0, got.ManagerChainIdx())
	require.Equal([]byte("mgr"), got.ManagerAddress())
}

// TestRoundTrip_CreateNetwork_InheritedL2 exercises an L2 restaking its parent
// L1's security: parent=some L1, Inherited (no own validators), still holds a
// local manager and genesis chains.
func TestRoundTrip_CreateNetwork_InheritedL2(t *testing.T) {
	require := require.New(t)
	base := spendBase()
	parentL1 := ids.GenerateTestID()
	var owner fx.Owner = &secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}}
	chains := []*NetworkChain{{BlockchainName: "l2 evm", VMID: ids.GenerateTestID()}}
	in, err := NewCreateNetworkTx(base, parentL1, owner, SecurityInherited, nil, chains, 0, []byte("l2mgr"))
	require.NoError(err)
	got := roundTrip(t, in).(*CreateNetworkTx)
	require.Equal(parentL1, got.Parent(), "L2 records its parent L1 (level = depth)")
	require.False(got.Sovereign())
	require.Empty(got.Validators(), "inherited security carries no own validators")
	require.Equal(chains, got.Chains())
	require.Equal([]byte("l2mgr"), got.ManagerAddress(), "inherited L2 still holds a local manager")
}

// TestRoundTrip_ConvertNetwork exercises the promote endomorphism: an existing
// network re-anchors to Primary (→L1) and establishes its own validator set +
// manager, authorized by its owner.
func TestRoundTrip_ConvertNetwork(t *testing.T) {
	require := require.New(t)
	base := spendBase()
	vdr := sampleNetworkValidator()
	in, err := NewConvertNetworkTx(base,
		ids.GenerateTestID(),         // the L2/L3 network being promoted
		ids.Empty,                    // new parent = Primary ⇒ becomes an L1
		ids.GenerateTestID(),         // manager chain
		[]byte("0xmgr"),              // manager address
		[]*NetworkValidator{vdr},     // sovereign validator set
		&secp256k1fx.Input{SigIndices: []uint32{0}}, // owner auth
	)
	require.NoError(err)
	got := roundTrip(t, in).(*ConvertNetworkTx)
	require.Equal(in.Network(), got.Network())
	require.Equal(ids.Empty, got.Parent(), "re-anchored to Primary ⇒ L1")
	require.Equal(in.ManagerChainID(), got.ManagerChainID())
	require.Equal([]byte("0xmgr"), got.ManagerAddress())
	require.Equal([]*NetworkValidator{vdr}, got.Validators(), "sovereign set established on promote")
	require.Equal(in.Auth(), got.Auth())
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
