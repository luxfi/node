// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"testing"

	"github.com/stretchr/testify/require"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/bls/signer/localsigner"
	"github.com/luxfi/ids"
	"github.com/luxfi/utils"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"

	"github.com/luxfi/node/vms/platformvm/config"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
	"github.com/luxfi/node/vms/platformvm/validators/fee"
)

// TestRegisterOwnSetStoresTheKeyVerifyWouldHave pins the two ways of turning a
// wire signer into a stored key to the SAME key.
//
// registerOwnSet parses vdr.Signer.PublicKey directly instead of calling
// Verify() and reading Key(). Verify() populates Key() with
// bls.PublicKeyFromCompressedBytes(PublicKey[:]) — the identical call on the
// identical bytes — so the two cannot disagree. This test is what says so out
// loud: if anyone ever changes which key registerOwnSet stores, the stored key
// stops matching the one the Verify path produces and this fails.
func TestRegisterOwnSetStoresTheKeyVerifyWouldHave(t *testing.T) {
	require := require.New(t)

	const n = 3
	e, tx := ownSetFixture(t, n, 0)
	netID := tx.Network()

	require.NoError(registerOwnSet(e, netID, tx.Validators(), tx.Security(), tx.ManagerChainID(), tx.ManagerAddress()))

	// The Verify path, computed independently: a fresh decode of the same tx,
	// verified, then read through Key() exactly as the previous code did.
	wantByNodeID := map[ids.NodeID][]byte{}
	for _, vdr := range tx.Validators() {
		nodeID, err := ids.ToNodeID(vdr.NodeID)
		require.NoError(err)

		require.Nil(vdr.Signer.Key(), "a fresh decode must not be assumed to carry a parsed key")
		require.NoError(vdr.Signer.Verify())
		require.NotNil(vdr.Signer.Key())

		wantByNodeID[nodeID] = bls.PublicKeyToUncompressedBytes(vdr.Signer.Key())
	}
	require.Len(wantByNodeID, n)

	for i := 0; i < n; i++ {
		stored, err := e.state.GetL1Validator(netID.Append(uint32(i)))
		require.NoError(err)

		want := wantByNodeID[stored.NodeID]
		require.NotNil(want, "stored a validator the tx never declared")
		require.Equal(want, stored.PublicKey,
			"validator %d: the parsed key and the Verify()+Key() key differ", i)

		// And it is still the encoding every consumer reads.
		require.Len(stored.PublicKey, 2*bls.PublicKeyLen)
		require.NotNil(bls.PublicKeyFromValidUncompressedBytes(stored.PublicKey))
	}
}

// TestForgedProofOfPossessionIsRefusedBeforeExecution is the safety precondition
// for parsing the key instead of verifying it.
//
// Parsing and verifying differ by exactly one thing: the possession pairing. So
// the question is whether a validator whose key PARSES but whose possession
// proof FAILS can reach registerOwnSet. It cannot: SyntacticVerify is the first
// statement of both callers, and it verifies every validator's proof against
// this same tx.
//
// The gate is two things — the per-validator Verify() loop inside
// SyntacticVerify, and the executor CALLING SyntacticVerify — and dropping
// either one makes the parse unsafe. So the tx goes through StandardTx, the
// real entry point, rather than having its SyntacticVerify called here: that
// covers the second half, which a direct call cannot see. Asserting on the
// possession error specifically is what makes it a gate test and not an
// "errors somewhere" test — with the call deleted, execution runs on and fails
// somewhere else, and this fails.
func TestForgedProofOfPossessionIsRefusedBeforeExecution(t *testing.T) {
	require := require.New(t)

	rt := consensustest.Runtime(t, ids.GenerateTestID())

	victim, err := localsigner.New()
	require.NoError(err)
	victimPoP, err := signer.NewProofOfPossession(victim)
	require.NoError(err)

	other, err := localsigner.New()
	require.NoError(err)
	otherPoP, err := signer.NewProofOfPossession(other)
	require.NoError(err)

	// A real public key carrying someone else's possession proof. Both halves
	// are well-formed points, so nothing short of the pairing rejects it.
	forged := *victimPoP
	forged.ProofOfPossession = otherPoP.ProofOfPossession

	// The delta between parsing and verifying, made concrete: the key parses...
	parsed, err := bls.PublicKeyFromCompressedBytes(forged.PublicKey[:])
	require.NoError(err, "the forged signer's key must parse — otherwise this proves nothing")
	require.NotNil(parsed)
	// ...and possession does not hold.
	require.Error(forged.Verify(), "a forged possession proof must not verify")

	nodeID := ids.GenerateTestNodeID()
	vdrs := []*txs.NetworkValidator{{
		NodeID: nodeID[:],
		Weight: 100,
		Signer: forged,
	}}
	utils.Sort(vdrs)

	utx, err := txs.NewConvertNetworkTx(
		&lux.BaseTx{NetworkID: rt.NetworkID, BlockchainID: rt.ChainID},
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		security.Mode{Admission: security.Gated, Manager: security.PChain},
		nil,
		vdrs,
		&secp256k1fx.Input{SigIndices: []uint32{0}},
	)
	require.NoError(err)

	tx := &txs.Tx{Unsigned: utx}
	require.NoError(tx.Initialize())

	// The gate, driven where production drives it. Execution never begins, so
	// registerOwnSet never sees this validator.
	_, _, _, err = StandardTx(
		&Backend{
			Runtime: rt,
			Config:  &config.Internal{ValidatorFeeConfig: fee.Config{Capacity: 1000}},
		},
		nil, // the fee calculator is past the gate, so it is never reached
		tx,
		ownSetState(t),
	)
	require.ErrorIs(err, signer.ErrInvalidProofOfPossession,
		"a validator whose possession proof is forged must be refused before execution")
}
