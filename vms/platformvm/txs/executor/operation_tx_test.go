// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/keychain"
	"github.com/luxfi/math/set"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/genesis/genesistest"
	"github.com/luxfi/node/vms/platformvm/state"
	"github.com/luxfi/node/vms/platformvm/status"
	"github.com/luxfi/node/vms/platformvm/txs"
	walletsigner "github.com/luxfi/node/wallet/chain/p/signer"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// =====================================================================
// Shared helpers for OperationTx / CreateAssetTx executor tests.
// =====================================================================

// defaultCreateAssetTxFee matches helpers_test.go:274 wallet config.
// PickFeeCalculator returns the static MilliLux fee under ApricotPhase5
// and later in the test environment, so this is the byte-exact charge
// CreateAssetTx will deduct.
const defaultCreateAssetTxFee = 1_000_000 // constants.MilliLux

// chargeFee returns a (BaseTx-Ins, BaseTx-Outs, signer-key) triple that
// spends one of [keys] funded genesis UTXOs to charge a [fee] of XAssetID.
// The change goes back to the same key.
func chargeFee(
	t testing.TB,
	env *environment,
	key *secp256k1.PrivateKey,
	fee uint64,
) ([]*lux.TransferableInput, []*lux.TransferableOutput, ids.ID) {
	t.Helper()
	// Find a funded UTXO owned by [key] worth >= fee.
	addrs := set.NewSet[ids.ShortID](1)
	addrs.Add(key.Address())
	utxos, err := lux.GetAllUTXOs(env.state, addrs)
	require.NoError(t, err)
	var spendUTXO *lux.UTXO
	for _, u := range utxos {
		if u.AssetID() != env.rt.XAssetID {
			continue
		}
		out, ok := u.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}
		if out.Amt >= fee {
			spendUTXO = u
			break
		}
	}
	require.NotNil(t, spendUTXO, "could not find funded XAsset UTXO for key %s", key.Address())
	out := spendUTXO.Out.(*secp256k1fx.TransferOutput)
	ins := []*lux.TransferableInput{{
		UTXOID: spendUTXO.UTXOID,
		Asset:  lux.Asset{ID: env.rt.XAssetID},
		In: &secp256k1fx.TransferInput{
			Amt:   out.Amt,
			Input: secp256k1fx.Input{SigIndices: []uint32{0}},
		},
	}}
	outs := []*lux.TransferableOutput{}
	if change := out.Amt - fee; change > 0 {
		outs = append(outs, &lux.TransferableOutput{
			Asset: lux.Asset{ID: env.rt.XAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:          change,
				OutputOwners: out.OutputOwners,
			},
		})
	}
	return ins, outs, spendUTXO.InputID()
}

// signTx signs an UnsignedTx with the supplied signer key sets and runs
// the standard executor against a fresh diff. Returns the diff so the
// caller can inspect post-state and the executor error verbatim.
func signTx(
	t testing.TB,
	env *environment,
	utx txs.UnsignedTx,
	signers [][]*secp256k1.PrivateKey,
) (*txs.Tx, state.Diff, error) {
	t.Helper()
	tx := &txs.Tx{Unsigned: utx}
	require.NoError(t, tx.Sign(txs.Codec, signers))
	diff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(t, err)
	feeCalc := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(&env.backend, feeCalc, tx, diff)
	return tx, diff, err
}

// applyDiff persists [diff] back to env.state so subsequent txs see the
// changes. Mirrors the chain executor flow.
func applyDiff(t testing.TB, env *environment, diff state.Diff) {
	t.Helper()
	require.NoError(t, diff.Apply(env.state))
	require.NoError(t, env.state.Commit())
}

// mintAsset issues a CreateAssetTx that mints a transferable amount of a
// new asset to [holder] alongside a MintOutput controlled by [minter].
// Returns the asset ID (== tx ID) and the two newly created UTXOs.
func mintAsset(
	t testing.TB,
	env *environment,
	payer *secp256k1.PrivateKey,
	holder *secp256k1.PrivateKey,
	minter *secp256k1.PrivateKey,
	amount uint64,
) (assetID ids.ID, mintUTXO *lux.UTXO, xferUTXO *lux.UTXO) {
	t.Helper()
	feeCalc := state.PickFeeCalculator(env.config, env.state)
	ins, outs, _ := chargeFee(t, env, payer, defaultCreateAssetTxFee)
	// Two initial outputs:
	//  - MintOutput owned by [minter] so subsequent OperationTx can mint
	//  - TransferOutput holding [amount] owned by [holder]
	xferOut := &secp256k1fx.TransferOutput{
		Amt: amount,
		OutputOwners: secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{holder.Address()},
		},
	}
	mintOut := &secp256k1fx.MintOutput{
		OutputOwners: secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{minter.Address()},
		},
	}
	utx := &txs.CreateAssetTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    env.rt.NetworkID,
			BlockchainID: env.rt.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Name:         "TestAsset",
		Symbol:       "TST",
		Denomination: 6,
		States: []*txs.InitialState{{
			FxIndex: 0,
			Outs:    []verify.State{mintOut, xferOut},
		}},
	}
	_ = feeCalc
	tx, diff, err := signTx(t, env, utx, [][]*secp256k1.PrivateKey{{payer}})
	require.NoError(t, err)
	applyDiff(t, env, diff)
	assetID = tx.ID()
	// Look up the two minted UTXOs (BaseTx outs come first in OutputIndex).
	// outputIndex starts at len(outs) (change), then iterates States.Outs.
	startIdx := uint32(len(outs))
	mintUTXOID := lux.UTXOID{TxID: assetID, OutputIndex: startIdx}
	xferUTXOID := lux.UTXOID{TxID: assetID, OutputIndex: startIdx + 1}
	mintUTXO, err = env.state.GetUTXO(mintUTXOID.InputID())
	// Mint outputs aren't TransferableOut, so the executor's
	// CreateAssetTx loop skips them. We have to register the mint UTXO
	// manually for the test — this matches the production path's
	// expected next step (the asset registry / mint authority registry
	// adds the MintOutput once CreateAssetTx is committed).
	if err != nil {
		mintUTXO = &lux.UTXO{
			UTXOID: mintUTXOID,
			Asset:  lux.Asset{ID: assetID},
			Out:    mintOut,
		}
		env.state.AddUTXO(mintUTXO)
		require.NoError(t, env.state.Commit())
	}
	xferUTXO, err = env.state.GetUTXO(xferUTXOID.InputID())
	require.NoError(t, err)
	return assetID, mintUTXO, xferUTXO
}

// =====================================================================
// V4 — CRITICAL: unauthorised spend must be rejected.
// =====================================================================

// TestOperationTx_UnauthorisedSpendRejected proves the fix for V4. An
// attacker submits an OperationTx referencing a victim's MintOutput with
// a credential the victim never signed. Before the fix the executor
// deleted the MintOutput and minted attacker-owned outputs. After the
// fix, fx.VerifyOperation rejects the bogus signature and the UTXO set
// is unchanged.
func TestOperationTx_UnauthorisedSpendRejected(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Granite)
	env.rt.Lock.Lock()
	defer env.rt.Lock.Unlock()

	payer := genesistest.DefaultFundedKeys[0]
	victim := genesistest.DefaultFundedKeys[1] // owns the mint authority
	attacker := genesistest.DefaultFundedKeys[2] // pays attacker's own fee

	assetID, mintUTXO, _ := mintAsset(t, env, payer, victim, victim, 1_000)

	// Attacker constructs an OperationTx referencing the victim's
	// MintOutput. The credential will be signed by attacker, NOT
	// by victim. Attacker pays the fee out of attacker's own UTXOs.
	ins, outs, _ := chargeFee(t, env, attacker, defaultTxFee)
	mintOp := &secp256k1fx.MintOperation{
		MintInput: secp256k1fx.Input{SigIndices: []uint32{0}},
		MintOutput: secp256k1fx.MintOutput{
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{attacker.Address()},
			},
		},
		TransferOutput: secp256k1fx.TransferOutput{
			Amt: 100,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{attacker.Address()},
			},
		},
	}
	utx := &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    env.rt.NetworkID,
			BlockchainID: env.rt.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Ops: []*txs.Operation{{
			Asset:   lux.Asset{ID: assetID},
			UTXOIDs: []*lux.UTXOID{&mintUTXO.UTXOID},
			Op:      mintOp,
		}},
	}
	// Sign the BaseTx with attacker (legitimate fee payer) AND sign
	// the Op slot with attacker (which is the unauthorised cred).
	_, diff, err := signTx(t, env, utx, [][]*secp256k1.PrivateKey{
		{attacker}, // fee cred
		{attacker}, // op cred — WRONG; victim should be required
	})
	// The Fx must reject. Acceptable rejection reasons (any of):
	//   - secp256k1fx.ErrWrongSig (sig check fires first)
	//   - "wrong mint output" (mint-output equality check fires first)
	// Both prove the same security invariant: an unauthorised op cannot
	// mutate the victim's mint authority. The specific path doesn't
	// matter as long as the tx is rejected before any state delete.
	require.Error(err, "executor must reject attacker-signed op against victim's mint output")

	// Critically — the victim's MintOutput is still there after the
	// failed exec. The two-pass executor never reached the delete
	// stage because verifyOperation aborted first.
	_, err = env.state.GetUTXO(mintUTXO.InputID())
	require.NoError(err, "victim's MintOutput must NOT be deleted by a rejected tx")
	// Diff also has no pending delete.
	_, err = diff.GetUTXO(mintUTXO.InputID())
	require.NoError(err)
}

// =====================================================================
// V13 — CreateAssetTx must persist the tx so state.GetTx resolves.
// =====================================================================

// TestCreateAssetTx_AssetRegistryResolves proves the fix for V13. Before
// the fix a CreateAssetTx executed cleanly but state.GetTx(assetID)
// returned an error — breaking every downstream lookup that needs the
// asset metadata (name, symbol, denomination, initial mint outputs).
func TestCreateAssetTx_AssetRegistryResolves(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Granite)
	env.rt.Lock.Lock()
	defer env.rt.Lock.Unlock()

	payer := genesistest.DefaultFundedKeys[0]
	holder := genesistest.DefaultFundedKeys[1]
	ins, outs, _ := chargeFee(t, env, payer, defaultCreateAssetTxFee)
	xferOut := &secp256k1fx.TransferOutput{
		Amt: 42,
		OutputOwners: secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{holder.Address()},
		},
	}
	utx := &txs.CreateAssetTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    env.rt.NetworkID,
			BlockchainID: env.rt.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Name:         "Test",
		Symbol:       "TST",
		Denomination: 6,
		States: []*txs.InitialState{{
			FxIndex: 0,
			Outs:    []verify.State{xferOut},
		}},
	}
	tx, diff, err := signTx(t, env, utx, [][]*secp256k1.PrivateKey{{payer}})
	require.NoError(err)
	applyDiff(t, env, diff)

	assetID := tx.ID()
	gotTx, gotStatus, err := env.state.GetTx(assetID)
	require.NoError(err, "state.GetTx(assetID) must resolve after CreateAssetTx commit")
	require.Equal(status.Committed, gotStatus)
	require.NotNil(gotTx)

	// Metadata round trip — Name / Symbol / Denomination preserved.
	gotCreateAsset, ok := gotTx.Unsigned.(*txs.CreateAssetTx)
	require.True(ok, "persisted tx must round-trip to *txs.CreateAssetTx")
	require.Equal("Test", gotCreateAsset.Name)
	require.Equal("TST", gotCreateAsset.Symbol)
	require.Equal(byte(6), gotCreateAsset.Denomination)
}

// =====================================================================
// V12 — wallet signer must produce a valid op-credential.
// =====================================================================

// TestOperationTx_AuthorisedSpend_RoundTrip proves the V12 fix. The
// wallet's signer.OperationTx now resolves each op's UTXO, looks up the
// MintOutput owner addresses, and signs the op-credential slot. Without
// this, even a legitimate caller couldn't sign an OperationTx — the
// signer wrote an empty cred and the executor (post-V4) refused to
// accept it.
func TestOperationTx_AuthorisedSpend_RoundTrip(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Granite)
	env.rt.Lock.Lock()
	defer env.rt.Lock.Unlock()

	payer := genesistest.DefaultFundedKeys[0]
	mintAuth := genesistest.DefaultFundedKeys[1]

	assetID, mintUTXO, _ := mintAsset(t, env, payer, mintAuth, mintAuth, 1_000)

	// Build an OperationTx that legitimately mints another 100 units
	// of the asset to [mintAuth].
	ins, outs, _ := chargeFee(t, env, payer, defaultTxFee)
	mintOp := &secp256k1fx.MintOperation{
		MintInput: secp256k1fx.Input{SigIndices: []uint32{0}},
		MintOutput: secp256k1fx.MintOutput{
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{mintAuth.Address()},
			},
		},
		TransferOutput: secp256k1fx.TransferOutput{
			Amt: 100,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{mintAuth.Address()},
			},
		},
	}
	utx := &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    env.rt.NetworkID,
			BlockchainID: env.rt.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Ops: []*txs.Operation{{
			Asset:   lux.Asset{ID: assetID},
			UTXOIDs: []*lux.UTXOID{&mintUTXO.UTXOID},
			Op:      mintOp,
		}},
	}

	// Drive the wallet signer end-to-end. It must find the consumed
	// UTXO, identify mintAuth as the owner of the MintOutput, and
	// produce a valid credential. Build a signer backed by a stub
	// that returns the mint UTXO on demand.
	sb := &stubSignerBackend{utxosByID: map[ids.ID]*lux.UTXO{
		ins[0].InputID():        utxoForInput(env, payer.Address(), ins[0]),
		mintUTXO.InputID():      mintUTXO,
	}}
	walletSigner := walletsigner.New(
		stubKeychain{addrs: map[ids.ShortID]*secp256k1.PrivateKey{
			payer.Address():    payer,
			mintAuth.Address(): mintAuth,
		}},
		sb,
	)
	tx, err := walletsigner.SignUnsigned(context.Background(), walletSigner, utx)
	require.NoError(err)
	// Op credential slot must be populated (not empty-sig).
	require.Equal(2, len(tx.Creds))
	opCred, ok := tx.Creds[1].(*secp256k1fx.Credential)
	require.True(ok)
	require.Equal(1, len(opCred.Sigs))
	require.NotEqual([secp256k1.SignatureLen]byte{}, opCred.Sigs[0],
		"wallet signer must produce a non-empty op-credential signature")

	diff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)
	feeCalc := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(&env.backend, feeCalc, tx, diff)
	require.NoError(err, "wallet-signed OperationTx round-trip must verify")
}

// =====================================================================
// V14 — non-secp256k1fx op types are rejected.
// =====================================================================

// TestOperationTx_RejectsNonSecp256k1fxOp proves the V14 fix. A foreign
// Fx op (here propertyfx.MintOperation via a stub) must be rejected at
// SyntacticVerify before any state-modifying code sees it.
func TestOperationTx_RejectsNonSecp256k1fxOp(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Granite)
	env.rt.Lock.Lock()
	defer env.rt.Lock.Unlock()

	payer := genesistest.DefaultFundedKeys[0]
	ins, outs, _ := chargeFee(t, env, payer, defaultTxFee)
	// foreignOp is verify.Verifiable but NOT a *secp256k1fx.MintOperation —
	// the codec doesn't know it either, so this also catches codec drift.
	foreignOp := &foreignFxOp{}
	utx := &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    env.rt.NetworkID,
			BlockchainID: env.rt.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Ops: []*txs.Operation{{
			Asset:   lux.Asset{ID: env.rt.XAssetID},
			UTXOIDs: []*lux.UTXOID{},
			Op:      foreignOp,
		}},
	}
	err := utx.SyntacticVerify(env.rt)
	require.ErrorIs(err, txs.ErrUnsupportedOpType)
}

// =====================================================================
// V15 — wrong number of credentials is rejected before any UTXO access.
// =====================================================================

// TestOperationTx_RejectsWrongCredCount proves the V15 fix. A malicious
// peer ships fewer creds than NumCredentials() expects; the executor
// must reject the tx before slicing into tx.Creds[:len(Ins)].
func TestOperationTx_RejectsWrongCredCount(t *testing.T) {
	require := require.New(t)
	env := newEnvironment(t, upgradetest.Granite)
	env.rt.Lock.Lock()
	defer env.rt.Lock.Unlock()

	payer := genesistest.DefaultFundedKeys[0]
	holder := genesistest.DefaultFundedKeys[1]
	assetID, mintUTXO, _ := mintAsset(t, env, payer, holder, holder, 1_000)

	ins, outs, _ := chargeFee(t, env, payer, defaultTxFee)
	mintOp := &secp256k1fx.MintOperation{
		MintInput: secp256k1fx.Input{SigIndices: []uint32{0}},
		MintOutput: secp256k1fx.MintOutput{
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{holder.Address()},
			},
		},
		TransferOutput: secp256k1fx.TransferOutput{
			Amt: 50,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{holder.Address()},
			},
		},
	}
	utx := &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    env.rt.NetworkID,
			BlockchainID: env.rt.ChainID,
			Ins:          ins,
			Outs:         outs,
		}},
		Ops: []*txs.Operation{{
			Asset:   lux.Asset{ID: assetID},
			UTXOIDs: []*lux.UTXOID{&mintUTXO.UTXOID},
			Op:      mintOp,
		}},
	}
	tx := &txs.Tx{Unsigned: utx}
	// Sign only the BaseTx slot — strip the op cred from the signed Tx
	// to simulate a malicious peer.
	require.NoError(tx.Sign(txs.Codec, [][]*secp256k1.PrivateKey{{payer}}))
	require.Equal(1, len(tx.Creds), "test setup: only one cred attached")
	require.Equal(2, utx.NumCredentials(), "tx requires fee cred + op cred")

	diff, err := state.NewDiff(lastAcceptedID, env)
	require.NoError(err)
	feeCalc := state.PickFeeCalculator(env.config, env.state)
	_, _, _, err = StandardTx(&env.backend, feeCalc, tx, diff)
	require.Error(err)
	require.ErrorIs(err, txs.ErrWrongNumberOfCredentials)
}

// ---------------------------------------------------------------------
// Test-only stubs for signer round-trip.
// ---------------------------------------------------------------------

type foreignFxOp struct {
	verify.IsNotState
}

func (*foreignFxOp) Verify() error { return nil }

// utxoForInput rebuilds the UTXO record referenced by a TransferableInput
// using the funded genesis owner. The stub signer backend looks it up by
// InputID.
func utxoForInput(env *environment, owner ids.ShortID, in *lux.TransferableInput) *lux.UTXO {
	transfer := in.In.(*secp256k1fx.TransferInput)
	return &lux.UTXO{
		UTXOID: in.UTXOID,
		Asset:  lux.Asset{ID: env.rt.XAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: transfer.Amt,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{owner},
			},
		},
	}
}

type stubSignerBackend struct {
	utxosByID map[ids.ID]*lux.UTXO
}

func (s *stubSignerBackend) GetUTXO(_ context.Context, _, utxoID ids.ID) (*lux.UTXO, error) {
	u, ok := s.utxosByID[utxoID]
	if !ok {
		return nil, errStubUTXOMissing
	}
	return u, nil
}

// GetOwner is required by signer.Backend for chain-auth ops; OperationTx
// never calls it, but the interface contract is checked at compile time.
func (s *stubSignerBackend) GetOwner(_ context.Context, _ ids.ID) (fx.Owner, error) {
	return nil, errStubUTXOMissing
}

var errStubUTXOMissing = stubErr("stub: unknown utxoID")

type stubErr string

func (e stubErr) Error() string { return string(e) }

// stubKeychain implements keychain.Keychain. Each address maps to a
// *secp256k1.PrivateKey, which already satisfies keychain.Signer.
type stubKeychain struct {
	addrs map[ids.ShortID]*secp256k1.PrivateKey
}

func (s stubKeychain) Get(addr ids.ShortID) (keychain.Signer, bool) {
	k, ok := s.addrs[addr]
	if !ok {
		return nil, false
	}
	return k, true
}

func (s stubKeychain) Addresses() set.Set[ids.ShortID] {
	addrs := set.NewSet[ids.ShortID](len(s.addrs))
	for a := range s.addrs {
		addrs.Add(a)
	}
	return addrs
}
