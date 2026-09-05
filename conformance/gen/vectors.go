// SPDX-License-Identifier: BSD-3-Clause-Eco

// The corpus, built by the Go P-chain and X-chain themselves.
//
// Every vector's bytes come out of a Go constructor — `txs.New*Tx`,
// `block.New*Block` — and are then signed the way the Go node signs. Nothing
// here writes a byte by hand except the deliberately malformed vectors, which
// are hand-damaged copies of well-formed ones and say so.
//
// That is the whole point of generating from Go: a corpus written by a third
// party would be a fourth opinion about the wire, and there would be nothing
// to say which of the four was the chain.
//
// Determinism: every key is derived from a fixed seed and every field is a
// literal, so re-running the generator on the same reference produces the same
// bytes. A corpus that moved on its own could not tell drift from noise.
package main

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/txs"
	pwarp "github.com/luxfi/node/vms/platformvm/warp"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	xblock "github.com/luxfi/node/vms/xvm/block"
	xtxs "github.com/luxfi/node/vms/xvm/txs"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// The one network and chain every vector is built for, so that a syntactic
// check that reads them reads the same numbers in all three implementations.
const (
	networkID   = 1
	stakeAsset  = 9 // the byte every id(9) is filled with: the fee/stake asset
	genesisTime = 1000
)

func id(b byte) ids.ID {
	var i ids.ID
	for k := range i {
		i[k] = b
	}
	return i
}

func short(b byte) ids.ShortID {
	var s ids.ShortID
	for k := range s {
		s[k] = b
	}
	return s
}

func nodeID(b byte) ids.NodeID {
	var n ids.NodeID
	for k := range n {
		n[k] = b
	}
	return n
}

// signingKey is derived, not drawn: the same 32 bytes every run, so the
// credentials — and therefore the signed bytes, and therefore every tx id in
// the corpus — are reproducible.
func signingKey() *secp256k1.PrivateKey {
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	k, err := secp256k1.ToPrivateKey(seed[:])
	if err != nil {
		panic(err)
	}
	return k
}

// blsKey is likewise derived from a fixed seed. A proof of possession over a
// fresh key would move every vector that carries one on every run.
func blsKey(seed byte) *bls.SecretKey {
	s := make([]byte, 32)
	for i := range s {
		s[i] = seed
	}
	k, err := bls.SecretKeyFromSeed(s)
	if err != nil {
		panic(err)
	}
	return k
}

func out(asset byte, amt uint64, owner byte) *lux.TransferableOutput {
	return &lux.TransferableOutput{
		Asset: lux.Asset{ID: id(asset)},
		Out: &secp256k1fx.TransferOutput{
			Amt: amt,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{short(owner)},
			},
		},
	}
}

func in(tx byte, idx uint32, amt uint64) *lux.TransferableInput {
	return &lux.TransferableInput{
		UTXOID: lux.UTXOID{TxID: id(tx), OutputIndex: idx},
		Asset:  lux.Asset{ID: id(stakeAsset)},
		In:     &secp256k1fx.TransferInput{Amt: amt, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
	}
}

// base is the spending envelope every P-chain vector shares: one input worth
// 200 of the fee asset, one output worth 100 of it. The 100 that is not
// returned is the fee, so a flow check has something to weigh.
func base() *lux.BaseTx {
	return &lux.BaseTx{
		NetworkID:    networkID,
		BlockchainID: id(3),
		Outs:         []*lux.TransferableOutput{out(stakeAsset, 100, 1)},
		Ins:          []*lux.TransferableInput{in(1, 0, 200)},
	}
}

func owners(b byte) fx.Owner {
	return &secp256k1fx.OutputOwners{Locktime: 0, Threshold: 1, Addrs: []ids.ShortID{short(b)}}
}

func auth() verify.Verifiable {
	return &secp256k1fx.Input{SigIndices: []uint32{0}}
}

// networkValidator is the shared component a sovereign L1's genesis set is
// made of: a node, its weight, its fee balance, and the BLS key it proves
// possession of.
func nv(node byte, weight, balance uint64, seed byte) *txs.NetworkValidator {
	pop, err := signer.NewProofOfPossession(blsKey(seed))
	if err != nil {
		panic(err)
	}
	return &txs.NetworkValidator{
		NodeID:                nodeID(node).Bytes(),
		Weight:                weight,
		Balance:               balance,
		Signer:                *pop,
		RemainingBalanceOwner: message.PChainOwner{Threshold: 1, Addresses: []ids.ShortID{short(1)}},
		DeactivationOwner:     message.PChainOwner{Threshold: 1, Addresses: []ids.ShortID{short(2)}},
	}
}

// sign wraps an unsigned tx the way the node does, with real credentials, and
// returns the signed bytes. `signers` is one list per input plus one per
// authorisation, which is what the Go signer expects.
func sign(u txs.UnsignedTx, credentials int) []byte {
	k := signingKey()
	signers := make([][]*secp256k1.PrivateKey, credentials)
	for i := range signers {
		signers[i] = []*secp256k1.PrivateKey{k}
	}
	tx, err := txs.NewSigned(u, signers)
	if err != nil {
		panic(fmt.Errorf("signing %T: %w", u, err))
	}
	return tx.Bytes()
}

func vec(idStr, chain, op string, wire []byte) Vector {
	// A vector with no bytes writes "-" rather than an empty last column: a
	// trailing empty field is invisible to a reader that splits on tabs, and
	// one of the three evaluators reads exactly that way.
	if len(wire) == 0 {
		return Vector{ID: idStr, Chain: chain, Op: op, Wire: none}
	}
	return Vector{ID: idStr, Chain: chain, Op: op, Wire: hex.EncodeToString(wire)}
}

// buildCorpus returns every vector, in a stable order.
func buildCorpus() []Vector {
	var v []Vector
	v = append(v, platformTxs()...)
	v = append(v, platformBlocks()...)
	v = append(v, platformEdges()...)
	v = append(v, xTxs()...)
	v = append(v, xBlocks()...)
	v = append(v, xEdges()...)
	v = append(v, seams()...)
	return v
}

// ---------------------------------------------------------------------------
// P-chain: one vector per transaction kind on the wire.
//
// The kind byte runs 2..20 and every one of the nineteen is here, because a
// differential that skipped a kind would be silent about exactly the kind that
// diverged — which is what happened to the L1 plane.
// ---------------------------------------------------------------------------

func platformTxs() []Vector {
	var v []Vector

	// RewardValidator (kind 2) — the proposal-only tx. It carries no spending
	// envelope and no credentials.
	rv := txs.NewRewardValidatorTx(id(42))
	v = append(v, vec("P_REWARD_VALIDATOR", "P", "tx", sign(rv, 0)))

	// Base (3)
	bt, err := txs.NewBaseTx(base())
	must(err)
	v = append(v, vec("P_BASE", "P", "tx", sign(bt, 1)))

	// Import (4)
	imp, err := txs.NewImportTx(base(), id(4), []*lux.TransferableInput{in(6, 1, 300)})
	must(err)
	v = append(v, vec("P_IMPORT", "P", "tx", sign(imp, 2)))

	// Export (5)
	exp, err := txs.NewExportTx(base(), id(5), []*lux.TransferableOutput{out(stakeAsset, 50, 2)})
	must(err)
	v = append(v, vec("P_EXPORT", "P", "tx", sign(exp, 1)))

	// CreateNetwork (6) — permissioned: leans on its parent's set, so it is
	// NOT the sovereign path.
	cnPerm, err := txs.NewCreateNetworkTx(
		base(), ids.Empty, owners(7),
		security.Mode{RestakeParent: true, Admission: security.NoOwnSet},
		nil, ids.Empty, nil,
	)
	must(err)
	v = append(v, vec("P_CREATE_NETWORK_PERMISSIONED", "P", "tx", sign(cnPerm, 1)))

	// CreateChain (7) — the first of the three transactions a sovereign L1 is
	// born through.
	cc, err := txs.NewCreateChainTx(base(), id(6), "conformance chain", id(21),
		[]ids.ID{id(22)}, []byte("genesis bytes"), auth())
	must(err)
	v = append(v, vec("P_CREATE_CHAIN", "P", "tx", sign(cc, 2)))

	// TransferChainOwnership (8)
	tco, err := txs.NewTransferChainOwnershipTx(base(), id(6), auth(), owners(8))
	must(err)
	v = append(v, vec("P_TRANSFER_CHAIN_OWNERSHIP", "P", "tx", sign(tco, 2)))

	// RemoveChainValidator (9)
	rcv, err := txs.NewRemoveChainValidatorTx(base(), nodeID(5), id(6), auth())
	must(err)
	v = append(v, vec("P_REMOVE_CHAIN_VALIDATOR", "P", "tx", sign(rcv, 2)))

	// TransformChain (10)
	tf, err := txs.NewTransformChainTx(base(), id(6), id(stakeAsset),
		1_000_000, 2_000_000, 100_000, 120_000, 1, 1_000_000, 1, 31_536_000,
		20_000, 1, 5, 800_000, auth())
	must(err)
	v = append(v, vec("P_TRANSFORM_CHAIN", "P", "tx", sign(tf, 2)))

	// AddValidator (11) — the second of the three L1-birth transactions.
	val := txs.Validator{NodeID: nodeID(5), Start: genesisTime, End: genesisTime + 86400*14, Wght: 50}
	av, err := txs.NewAddValidatorTx(base(), val, []*lux.TransferableOutput{out(stakeAsset, 50, 2)}, owners(3), 20_000)
	must(err)
	v = append(v, vec("P_ADD_VALIDATOR", "P", "tx", sign(av, 1)))

	// AddChainValidator (12)
	acv, err := txs.NewAddChainValidatorTx(base(), val, id(6), auth())
	must(err)
	v = append(v, vec("P_ADD_CHAIN_VALIDATOR", "P", "tx", sign(acv, 2)))

	// AddDelegator (13)
	ad, err := txs.NewAddDelegatorTx(base(), val, []*lux.TransferableOutput{out(stakeAsset, 50, 2)}, owners(3))
	must(err)
	v = append(v, vec("P_ADD_DELEGATOR", "P", "tx", sign(ad, 1)))

	// AddPermissionlessValidator (14)
	pop, err := signer.NewProofOfPossession(blsKey(0x11))
	must(err)
	apv, err := txs.NewAddPermissionlessValidatorTx(base(), val, id(6), pop,
		[]*lux.TransferableOutput{out(stakeAsset, 50, 2)}, owners(3), owners(4), 20_000)
	must(err)
	v = append(v, vec("P_ADD_PERMISSIONLESS_VALIDATOR", "P", "tx", sign(apv, 1)))

	// AddPermissionlessDelegator (15)
	apd, err := txs.NewAddPermissionlessDelegatorTx(base(), val, id(6),
		[]*lux.TransferableOutput{out(stakeAsset, 50, 2)}, owners(3))
	must(err)
	v = append(v, vec("P_ADD_PERMISSIONLESS_DELEGATOR", "P", "tx", sign(apd, 1)))

	// RegisterL1Validator (16) — the L1 validator plane starts here.
	var popBlob [bls.SignatureLen]byte
	popSig := bls.Sign(blsKey(0x11), []byte("conformance"))
	copy(popBlob[:], bls.SignatureToBytes(popSig))
	warpBytes := registerWarpMessage()
	rl, err := txs.NewRegisterL1ValidatorTx(base(), 777, popBlob, warpBytes)
	must(err)
	v = append(v, vec("P_REGISTER_L1_VALIDATOR", "P", "tx", sign(rl, 1)))

	// SetL1ValidatorWeight (17)
	sw, err := txs.NewSetL1ValidatorWeightTx(base(), weightWarpMessage())
	must(err)
	v = append(v, vec("P_SET_L1_VALIDATOR_WEIGHT", "P", "tx", sign(sw, 1)))

	// IncreaseL1ValidatorBalance (18)
	inc, err := txs.NewIncreaseL1ValidatorBalanceTx(base(), id(13), 999_999)
	must(err)
	v = append(v, vec("P_INCREASE_L1_VALIDATOR_BALANCE", "P", "tx", sign(inc, 1)))

	// DisableL1Validator (19)
	dis, err := txs.NewDisableL1ValidatorTx(base(), id(13), auth())
	must(err)
	v = append(v, vec("P_DISABLE_L1_VALIDATOR", "P", "tx", sign(dis, 2)))

	// ConvertNetwork (20) — the third and last of the L1-birth transactions,
	// the one that hands a network its own validator set.
	cv, err := txs.NewConvertNetworkTx(base(), id(6), ids.Empty, id(23),
		security.Mode{RestakeParent: false, Admission: security.Open, Manager: security.Contract, Threshold: 1000},
		[]byte("manager address"),
		[]*txs.NetworkValidator{nv(5, 100, 900, 0x21)},
		auth(),
	)
	must(err)
	v = append(v, vec("P_CONVERT_NETWORK", "P", "tx", sign(cv, 2)))

	// The sovereign form of CreateNetwork: seeds a set of its own at birth.
	cnSov, err := txs.NewCreateNetworkTx(
		base(), ids.Empty, owners(7),
		security.Mode{RestakeParent: false, Admission: security.Open, Manager: security.Contract, Threshold: 1000},
		[]*txs.NetworkValidator{nv(5, 100, 900, 0x22)},
		id(23), []byte("manager address"),
	)
	must(err)
	v = append(v, vec("P_CREATE_NETWORK_SOVEREIGN", "P", "tx", sign(cnSov, 1)))

	return v
}

// The two warp messages the L1 plane is driven by.
//
// Each is a full signed warp message — an unsigned message carrying the
// P-chain's own payload, plus a signature over it — because that is what the
// transaction field holds and what every reader of it, down to the fee
// calculator, parses back out. A bare payload would fail before any L1 rule
// was reached, and the differential would then be about a malformed field.
//
// The signature is a real BLS signature by one validator over the unsigned
// message. Nothing here claims the signature is by a validator any of the
// three chains knows: whether it verifies is one of the things being compared.
func warpMessage(payload []byte) []byte {
	unsigned, err := pwarp.NewUnsignedMessage(networkID, id(6), payload)
	must(err)
	sk := blsKey(0x41)
	sig := bls.Sign(sk, unsigned.Bytes())
	var sigBytes [bls.SignatureLen]byte
	copy(sigBytes[:], bls.SignatureToBytes(sig))
	msg, err := pwarp.NewMessage(unsigned, &pwarp.BitSetSignature{
		Signers:   []byte{0x80},
		Signature: sigBytes,
	})
	must(err)
	return msg.Bytes()
}

func registerWarpMessage() []byte {
	m, err := message.NewRegisterL1Validator(
		id(6),
		nodeID(5),
		[bls.PublicKeyLen]byte(bls.PublicKeyToCompressedBytes(bls.PublicFromSecretKey(blsKey(0x11)))),
		uint64(genesisTime+86400*365),
		message.PChainOwner{Threshold: 1, Addresses: []ids.ShortID{short(1)}},
		message.PChainOwner{Threshold: 1, Addresses: []ids.ShortID{short(2)}},
		100,
	)
	must(err)
	return warpMessage(m.Bytes())
}

func weightWarpMessage() []byte {
	m, err := message.NewL1ValidatorWeight(id(13), 1, 200)
	must(err)
	return warpMessage(m.Bytes())
}

// ---------------------------------------------------------------------------
// P-chain blocks: all four kinds.
// ---------------------------------------------------------------------------

func platformBlocks() []Vector {
	var v []Vector
	ts := time.Unix(genesisTime, 0)

	bt, err := txs.NewBaseTx(base())
	must(err)
	signed, err := txs.Parse(sign(bt, 1))
	must(err)

	sb, err := block.NewStandardBlock(ts, id(1), 5, []*txs.Tx{signed})
	must(err)
	v = append(v, vec("P_BLOCK_STANDARD", "P", "block", sb.Bytes()))

	rv := txs.NewRewardValidatorTx(id(42))
	rvSigned, err := txs.Parse(sign(rv, 0))
	must(err)
	pb, err := block.NewProposalBlock(ts, id(1), 6, rvSigned, nil)
	must(err)
	v = append(v, vec("P_BLOCK_PROPOSAL", "P", "block", pb.Bytes()))

	cb, err := block.NewCommitBlock(ts, id(1), 7)
	must(err)
	v = append(v, vec("P_BLOCK_COMMIT", "P", "block", cb.Bytes()))

	ab, err := block.NewAbortBlock(ts, id(1), 8)
	must(err)
	v = append(v, vec("P_BLOCK_ABORT", "P", "block", ab.Bytes()))

	return v
}

// ---------------------------------------------------------------------------
// P-chain edges: conservation, ordering, bounds, and damaged bytes.
//
// These are the vectors a syntactic check is FOR. Each is a well-formed tx
// with exactly one thing wrong, so an implementation that misses the check
// answers OK where the others answer SYNTACTIC and the row names it.
// ---------------------------------------------------------------------------

func platformEdges() []Vector {
	var v []Vector

	// An output of zero. Nothing is transferred and the UTXO set grows.
	zero := base()
	zero.Outs = []*lux.TransferableOutput{out(stakeAsset, 0, 1)}
	bt, err := txs.NewBaseTx(zero)
	must(err)
	v = append(v, vec("P_EDGE_ZERO_OUTPUT", "P", "tx", sign(bt, 1)))

	// Two outputs whose amounts sum past 2^64. A summation without an overflow
	// check wraps to something small and the tx looks funded.
	over := base()
	over.Outs = []*lux.TransferableOutput{
		out(stakeAsset, 1<<63, 1),
		out(stakeAsset, 1<<63, 2),
	}
	over.Ins = []*lux.TransferableInput{in(1, 0, 1)}
	bt, err = txs.NewBaseTx(over)
	must(err)
	v = append(v, vec("P_EDGE_OUTPUT_OVERFLOW", "P", "tx", sign(bt, 1)))

	// Two inputs whose amounts sum past 2^64, the same wrap on the other side.
	overIn := base()
	overIn.Ins = []*lux.TransferableInput{in(1, 0, 1<<63), in(2, 0, 1<<63)}
	bt, err = txs.NewBaseTx(overIn)
	must(err)
	v = append(v, vec("P_EDGE_INPUT_OVERFLOW", "P", "tx", sign(bt, 2)))

	// Outputs out of canonical order. Sorting is what makes a tx's bytes the
	// only bytes for its content; unsorted outputs are a second encoding of
	// one transaction, and therefore a second id.
	unsorted := base()
	unsorted.Outs = []*lux.TransferableOutput{out(200, 100, 1), out(stakeAsset, 100, 2)}
	bt, err = txs.NewBaseTx(unsorted)
	must(err)
	v = append(v, vec("P_EDGE_UNSORTED_OUTPUTS", "P", "tx", sign(bt, 1)))

	// The same input claimed twice: a double spend inside one transaction.
	dup := base()
	dup.Ins = []*lux.TransferableInput{in(1, 0, 200), in(1, 0, 200)}
	bt, err = txs.NewBaseTx(dup)
	must(err)
	v = append(v, vec("P_EDGE_DUPLICATE_INPUT", "P", "tx", sign(bt, 2)))

	// A validator with an empty node id — nobody, staked.
	emptyNode := txs.Validator{NodeID: ids.EmptyNodeID, Start: genesisTime, End: genesisTime + 86400, Wght: 50}
	av, err := txs.NewAddValidatorTx(base(), emptyNode, []*lux.TransferableOutput{out(stakeAsset, 50, 2)}, owners(3), 20_000)
	must(err)
	v = append(v, vec("P_EDGE_EMPTY_NODE_ID", "P", "tx", sign(av, 1)))

	// A stake of zero weight.
	zeroWeight := txs.Validator{NodeID: nodeID(5), Start: genesisTime, End: genesisTime + 86400, Wght: 0}
	av, err = txs.NewAddValidatorTx(base(), zeroWeight, []*lux.TransferableOutput{out(stakeAsset, 50, 2)}, owners(3), 20_000)
	must(err)
	v = append(v, vec("P_EDGE_ZERO_WEIGHT", "P", "tx", sign(av, 1)))

	// A staking period that ends before it starts.
	backwards := txs.Validator{NodeID: nodeID(5), Start: genesisTime + 86400, End: genesisTime, Wght: 50}
	av, err = txs.NewAddValidatorTx(base(), backwards, []*lux.TransferableOutput{out(stakeAsset, 50, 2)}, owners(3), 20_000)
	must(err)
	v = append(v, vec("P_EDGE_BACKWARDS_PERIOD", "P", "tx", sign(av, 1)))

	// A transaction addressed to another network. Replaying it here would let
	// one signature spend on two chains.
	wrongNet := base()
	wrongNet.NetworkID = networkID + 1
	bt, err = txs.NewBaseTx(wrongNet)
	must(err)
	v = append(v, vec("P_EDGE_WRONG_NETWORK", "P", "tx", sign(bt, 1)))

	// Damaged bytes. A parser that reads past the end of a buffer is a remote
	// crash, so the corpus carries the shapes that tempt one.
	good, err := txs.NewBaseTx(base())
	must(err)
	goodBytes := sign(good, 1)

	v = append(v, vec("P_EDGE_TRUNCATED", "P", "tx", goodBytes[:len(goodBytes)/2]))
	v = append(v, vec("P_EDGE_EMPTY", "P", "tx", nil))
	v = append(v, vec("P_EDGE_ONE_BYTE", "P", "tx", []byte{0x03}))

	// A complete transaction with its last byte changed. The buffer still
	// looks the right length, so a parser that trusts a length it read has to
	// notice something else is wrong.
	corrupt := append([]byte(nil), goodBytes...)
	corrupt[len(corrupt)-1] ^= 0xFF
	v = append(v, vec("P_EDGE_CORRUPT_TAIL", "P", "tx", corrupt))

	// Trailing bytes past the end of a complete transaction: two encodings of
	// one transaction, and therefore two ids for it.
	trailing := append(append([]byte(nil), goodBytes...), 0xFF, 0xFF, 0xFF, 0xFF)
	v = append(v, vec("P_EDGE_TRAILING_BYTES", "P", "tx", trailing))

	return v
}

// ---------------------------------------------------------------------------
// X-chain.
// ---------------------------------------------------------------------------

func xBase() lux.BaseTx {
	return lux.BaseTx{
		NetworkID:    networkID,
		BlockchainID: id(2),
		Outs: []*lux.TransferableOutput{{
			Asset: lux.Asset{ID: id(50)},
			Out: &secp256k1fx.TransferOutput{
				Amt: 1000,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime: 0, Threshold: 1, Addrs: []ids.ShortID{short(1)},
				},
			},
		}},
		Ins: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{TxID: id(100), OutputIndex: 3},
			Asset:  lux.Asset{ID: id(50)},
			In:     &secp256k1fx.TransferInput{Amt: 1000, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
		}},
		Memo: []byte("conformance"),
	}
}

// xSign signs an X-chain unsigned tx with the corpus's one key, through the
// X-chain's own signing path.
func xSign(u xtxs.UnsignedTx, credentials int) []byte {
	k := signingKey()
	signers := make([][]*secp256k1.PrivateKey, credentials)
	for i := range signers {
		signers[i] = []*secp256k1.PrivateKey{k}
	}
	tx := &xtxs.Tx{Unsigned: u}
	if err := tx.SignSECP256K1Fx(signers); err != nil {
		panic(fmt.Errorf("signing %T: %w", u, err))
	}
	return tx.Bytes()
}

func xTxs() []Vector {
	var v []Vector

	bt := &xtxs.BaseTx{BaseTx: xBase()}
	v = append(v, vec("X_BASE", "X", "tx", xSign(bt, 1)))

	xOwners := secp256k1fx.OutputOwners{Locktime: 0, Threshold: 1, Addrs: []ids.ShortID{short(1)}}
	ca := &xtxs.CreateAssetTx{
		BaseTx:       xtxs.BaseTx{BaseTx: xBase()},
		Name:         "Conformance Asset",
		Symbol:       "CONF",
		Denomination: 9,
		States: []*xtxs.InitialState{{
			FxIndex: 0,
			Outs: []verify.State{
				&secp256k1fx.MintOutput{OutputOwners: xOwners},
				&secp256k1fx.TransferOutput{Amt: 1000, OutputOwners: xOwners},
			},
		}},
	}
	ca.States[0].Sort()
	v = append(v, vec("X_CREATE_ASSET", "X", "tx", xSign(ca, 1)))

	op := &xtxs.OperationTx{
		BaseTx: xtxs.BaseTx{BaseTx: xBase()},
		Ops: []*xtxs.Operation{{
			Asset:   lux.Asset{ID: id(50)},
			UTXOIDs: []*lux.UTXOID{{TxID: id(100), OutputIndex: 4}},
			FxID:    ids.Empty,
			Op: &secp256k1fx.MintOperation{
				MintInput:  secp256k1fx.Input{SigIndices: []uint32{0}},
				MintOutput: secp256k1fx.MintOutput{OutputOwners: xOwners},
				TransferOutput: secp256k1fx.TransferOutput{
					Amt: 100, OutputOwners: xOwners,
				},
			},
		}},
	}
	v = append(v, vec("X_OPERATION", "X", "tx", xSign(op, 2)))

	imp := &xtxs.ImportTx{
		BaseTx:      xtxs.BaseTx{BaseTx: xBase()},
		SourceChain: id(3),
		ImportedIns: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{TxID: id(101), OutputIndex: 0},
			Asset:  lux.Asset{ID: id(50)},
			In:     &secp256k1fx.TransferInput{Amt: 500, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
		}},
	}
	v = append(v, vec("X_IMPORT", "X", "tx", xSign(imp, 2)))

	exp := &xtxs.ExportTx{
		BaseTx:           xtxs.BaseTx{BaseTx: xBase()},
		DestinationChain: id(3),
		ExportedOuts: []*lux.TransferableOutput{{
			Asset: lux.Asset{ID: id(50)},
			Out:   &secp256k1fx.TransferOutput{Amt: 500, OutputOwners: xOwners},
		}},
	}
	v = append(v, vec("X_EXPORT", "X", "tx", xSign(exp, 1)))

	return v
}

func xBlocks() []Vector {
	var v []Vector
	bt := &xtxs.BaseTx{BaseTx: xBase()}
	signed, err := xtxs.Parse(xSign(bt, 1))
	must(err)

	blk, err := xblock.NewStandardBlock(id(10), 1, time.Unix(genesisTime, 0), []*xtxs.Tx{signed})
	must(err)
	v = append(v, vec("X_BLOCK_STANDARD", "X", "block", blk.Bytes()))

	empty, err := xblock.NewStandardBlock(id(10), 1, time.Unix(genesisTime, 0), nil)
	must(err)
	v = append(v, vec("X_BLOCK_EMPTY", "X", "block", empty.Bytes()))

	return v
}

func xEdges() []Vector {
	var v []Vector

	zero := xBase()
	zero.Outs[0].Out = &secp256k1fx.TransferOutput{
		Amt:          0,
		OutputOwners: secp256k1fx.OutputOwners{Locktime: 0, Threshold: 1, Addrs: []ids.ShortID{short(1)}},
	}
	v = append(v, vec("X_EDGE_ZERO_OUTPUT", "X", "tx", xSign(&xtxs.BaseTx{BaseTx: zero}, 1)))

	over := xBase()
	owner := secp256k1fx.OutputOwners{Locktime: 0, Threshold: 1, Addrs: []ids.ShortID{short(1)}}
	over.Outs = []*lux.TransferableOutput{
		{Asset: lux.Asset{ID: id(50)}, Out: &secp256k1fx.TransferOutput{Amt: 1 << 63, OutputOwners: owner}},
		{Asset: lux.Asset{ID: id(50)}, Out: &secp256k1fx.TransferOutput{Amt: 1 << 63, OutputOwners: owner}},
	}
	v = append(v, vec("X_EDGE_OUTPUT_OVERFLOW", "X", "tx", xSign(&xtxs.BaseTx{BaseTx: over}, 1)))

	wrongNet := xBase()
	wrongNet.NetworkID = networkID + 1
	v = append(v, vec("X_EDGE_WRONG_NETWORK", "X", "tx", xSign(&xtxs.BaseTx{BaseTx: wrongNet}, 1)))

	good := xSign(&xtxs.BaseTx{BaseTx: xBase()}, 1)
	v = append(v, vec("X_EDGE_TRUNCATED", "X", "tx", good[:len(good)/2]))
	v = append(v, vec("X_EDGE_EMPTY", "X", "tx", nil))

	corrupt := append([]byte(nil), good...)
	corrupt[len(corrupt)-1] ^= 0xFF
	v = append(v, vec("X_EDGE_CORRUPT_TAIL", "X", "tx", corrupt))

	return v
}

// ---------------------------------------------------------------------------
// Seam vectors.
//
// These carry no bytes. They ask each implementation a question about the
// shape of its block-decision seam, and each answers from its own compiler:
// the method is there and callable, or it is not. A chain that cannot reject
// a block cannot hand back what that block was carrying, and the two sides
// then disagree about what is still pending — which is not visible in any
// transaction's bytes, so no wire vector could ever catch it.
// ---------------------------------------------------------------------------

func seams() []Vector {
	return []Vector{
		{ID: "X_SEAM_BLOCK_REJECT", Chain: "X", Op: "seam", Wire: none},
		{ID: "P_SEAM_BLOCK_REJECT", Chain: "P", Op: "seam", Wire: none},
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
