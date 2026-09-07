// SPDX-License-Identifier: BSD-3-Clause-Eco

// The Z-chain's corpus, built by the Go Z-chain itself.
//
// A Z vector is a BLOCK, always. The Z-chain's transaction parser is not part
// of the reference's exported surface, so a bare-transaction vector could only
// be read by re-implementing the frame here — a fourth opinion about the wire,
// which is the one thing this corpus must not contain. A block carries its
// transactions, so parsing a block IS parsing them, and Verify runs the shape
// check, the block-level spent-set check and the proof gate over every one.
//
// Every vector sits at height 0 with an empty parent. That is the Z-chain's
// analogue of the empty chain the P and X vectors are judged against: the
// parent branch of Verify is skipped, so what the verdict reports is the
// TRANSACTIONS and the block's own shape, not which blocks a particular
// evaluator happened to have in its store.
//
// Determinism: every field is a literal and every state root is read off the
// reference's own fold over a fresh chain, so re-running the generator on the
// same reference produces the same bytes.
package main

import (
	"github.com/luxfi/chains/zkvm"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
)

// The chain every Z vector is built for. The block id opens with
// sha256(ChainID ‖ NetworkID), which is not on the wire, so both evaluators
// have to be told the same two numbers or every hash disagrees.
var zChainID = id(4)

const zNetworkID = uint32(networkID)

// zTimestamp is in the past and fixed, so the clock-skew check is not a race
// against the wall clock.
const zTimestamp = int64(genesisTime)

// zFutureTimestamp is 2100-01-01T00:00:00Z: far enough past the 60-second skew
// bound that the refusal is not a function of when the corpus is read.
const zFutureTimestamp = int64(4102444800)

// zRoot returns the state root a block over txs commits to on a chain that has
// accepted nothing — the reference's own fold, never a formula restated here.
//
// Genesis is committed to the root before any block is folded onto it, so the
// root a fresh chain builds on is the fold over genesis's transactions rather
// than the empty one. Skipping that step here would put every vector's root one
// commit behind the chain's, and every block would be refused for a reason that
// had nothing to do with what it carried.
func zRoot(txs []*zkvm.Transaction) []byte {
	r, err := zkvm.NewRoot(memdb.New(), log.NewNoOpLogger())
	must(err)
	must(r.Finalize(r.After(nil))) // genesis allocates nothing on this chain
	return r.After(txs)
}

func nullifier(b byte) []byte {
	n := make([]byte, 32)
	n[0] = b
	return n
}

func commitment(b byte) []byte {
	c := make([]byte, 32)
	c[0] = b ^ 0xFF
	return c
}

// starkProof is the strict-PQ system's shape. The Z-chain's default profile
// accepts no other, and with no FRI binding registered it accepts none at all —
// which is the shipped posture of the reference and the one both evaluators
// have to reach the same way.
func starkProof() *zkvm.ZKProof {
	return &zkvm.ZKProof{
		ProofType:    "stark",
		ProofData:    []byte("stark-proof-bytes"),
		PublicInputs: [][]byte{nullifier(0xA1), commitment(0xA1)},
	}
}

func groth16Proof() *zkvm.ZKProof {
	return &zkvm.ZKProof{
		ProofType:    "groth16",
		ProofData:    []byte("groth16-proof-bytes"),
		PublicInputs: [][]byte{nullifier(0xA1), commitment(0xA1)},
	}
}

const zExpiry = uint64(1 << 20)

func zTransfer(n byte, proof *zkvm.ZKProof) *zkvm.Transaction {
	return &zkvm.Transaction{
		Type:       zkvm.TransactionTypeTransfer,
		Version:    1,
		Fee:        1_000_000,
		Expiry:     zExpiry,
		Nullifiers: [][]byte{nullifier(n)},
		Outputs:    []*zkvm.ShieldedOutput{{Commitment: commitment(n), EncryptedNote: []byte("note")}},
		Proof:      proof,
	}
}

func zShield() *zkvm.Transaction {
	return &zkvm.Transaction{
		Type:    zkvm.TransactionTypeShield,
		Version: 1,
		Fee:     1_000_000,
		Expiry:  zExpiry,
		TransparentInputs: []*zkvm.TransparentInput{
			{TxID: id(1), OutputIdx: 0, Amount: 100, Address: []byte("payer")},
		},
		Outputs: []*zkvm.ShieldedOutput{{Commitment: commitment(0xB2)}},
		Proof:   starkProof(),
	}
}

func zUnshield() *zkvm.Transaction {
	return &zkvm.Transaction{
		Type:       zkvm.TransactionTypeUnshield,
		Version:    1,
		Fee:        1_000_000,
		Expiry:     zExpiry,
		Nullifiers: [][]byte{nullifier(0xC3)},
		TransparentOutputs: []*zkvm.TransparentOutput{
			{Amount: 90, AssetID: id(2), Address: []byte("payee")},
		},
		Proof: starkProof(),
	}
}

// zBlock assembles a block at height 0 whose state root is the one the
// reference's fold produces for its transactions, so the root check passes and
// the verdict is about everything before it.
func zBlock(txs ...*zkvm.Transaction) []byte {
	b := &zkvm.Block{
		ParentID_:      ids.Empty,
		BlockHeight:    0,
		BlockTimestamp: zTimestamp,
		Txs:            txs,
		StateRoot:      zRoot(txs),
	}
	return b.Marshal()
}

func zVectors() []Vector {
	transfer := zTransfer(0xA1, starkProof())

	// A well-formed transaction whose shape is right and whose proof system is
	// the only one a strict-PQ chain will look at.
	var v []Vector
	v = append(v,
		vec("Z_BLOCK_EMPTY", "Z", "block", zBlock()),
		vec("Z_BLOCK_TRANSFER_STARK", "Z", "block", zBlock(transfer)),
		// The classical path. On the Z-chain's default profile this is refused
		// by name before anything looks at the proof, and a chain that instead
		// verified it would be one a CRQC could mint shielded value on.
		vec("Z_BLOCK_TRANSFER_GROTH16", "Z", "block", zBlock(zTransfer(0xA1, groth16Proof()))),
		vec("Z_BLOCK_SHIELD", "Z", "block", zBlock(zShield())),
		vec("Z_BLOCK_UNSHIELD", "Z", "block", zBlock(zUnshield())),
		vec("Z_BLOCK_MULTI", "Z", "block", zBlock(zTransfer(0xA1, starkProof()), zTransfer(0xB1, starkProof()))),
	)

	// ---- the spent set ----
	//
	// Two ways one shielded note is spent twice inside a single block. Neither
	// is visible to the per-transaction check, which only sees notes already
	// spent in ACCEPTED state, so a chain missing this gate inflates supply.
	dupAcross := []*zkvm.Transaction{zTransfer(0xA1, starkProof()), zTransfer(0xA1, starkProof())}
	dupWithin := &zkvm.Transaction{
		Type:       zkvm.TransactionTypeTransfer,
		Version:    1,
		Fee:        1_000_000,
		Expiry:     zExpiry,
		Nullifiers: [][]byte{nullifier(0xD4), nullifier(0xD4)},
		Outputs:    []*zkvm.ShieldedOutput{{Commitment: commitment(0xD4)}},
		Proof:      starkProof(),
	}
	v = append(v,
		vec("Z_EDGE_DUP_NULLIFIER_ACROSS_TXS", "Z", "block", zBlock(dupAcross...)),
		vec("Z_EDGE_DUP_NULLIFIER_IN_TX", "Z", "block", zBlock(dupWithin)),
	)

	// ---- the shape check ----
	noInputs := zTransfer(0xA1, starkProof())
	noInputs.Nullifiers = nil

	noOutputs := zTransfer(0xA1, starkProof())
	noOutputs.Outputs = nil

	noProof := zTransfer(0xA1, nil)

	noExpiry := zTransfer(0xA1, starkProof())
	noExpiry.Expiry = 0

	badType := zTransfer(0xA1, starkProof())
	badType.Type = zkvm.TransactionType(5)

	// A transfer that moves only transparent value: the per-type rule refuses
	// it even though the generic input/output check is satisfied.
	transferNoShielded := &zkvm.Transaction{
		Type:               zkvm.TransactionTypeTransfer,
		Version:            1,
		Fee:                1_000_000,
		Expiry:             zExpiry,
		TransparentInputs:  []*zkvm.TransparentInput{{TxID: id(1), OutputIdx: 0, Amount: 100, Address: []byte("payer")}},
		TransparentOutputs: []*zkvm.TransparentOutput{{Amount: 90, AssetID: id(2), Address: []byte("payee")}},
		Proof:              starkProof(),
	}

	shieldNoTransparentIn := zShield()
	shieldNoTransparentIn.TransparentInputs = nil
	shieldNoTransparentIn.Nullifiers = [][]byte{nullifier(0xE5)}

	unshieldNoTransparentOut := zUnshield()
	unshieldNoTransparentOut.TransparentOutputs = nil
	unshieldNoTransparentOut.Outputs = []*zkvm.ShieldedOutput{{Commitment: commitment(0xE6)}}

	v = append(v,
		vec("Z_EDGE_NO_INPUTS", "Z", "block", zBlock(noInputs)),
		vec("Z_EDGE_NO_OUTPUTS", "Z", "block", zBlock(noOutputs)),
		vec("Z_EDGE_NO_PROOF", "Z", "block", zBlock(noProof)),
		vec("Z_EDGE_NO_EXPIRY", "Z", "block", zBlock(noExpiry)),
		vec("Z_EDGE_BAD_TX_TYPE", "Z", "block", zBlock(badType)),
		vec("Z_EDGE_TRANSFER_NO_SHIELDED", "Z", "block", zBlock(transferNoShielded)),
		vec("Z_EDGE_SHIELD_NO_TRANSPARENT_IN", "Z", "block", zBlock(shieldNoTransparentIn)),
		vec("Z_EDGE_UNSHIELD_NO_TRANSPARENT_OUT", "Z", "block", zBlock(unshieldNoTransparentOut)),
	)

	// ---- the block's own shape ----
	//
	// Hand-built frames, each a damaged copy of a well-formed one.
	// Carrying nothing, so the admission pass is vacuous and the verdict is
	// about the root alone: the negative control for Z_BLOCK_EMPTY, which is
	// the same block with the root the chain's own fold produces.
	wrongRoot := &zkvm.Block{
		ParentID_:      ids.Empty,
		BlockHeight:    0,
		BlockTimestamp: zTimestamp,
		StateRoot:      make([]byte, 32), // the committed root is a hash, never zero
	}
	parentAtZero := &zkvm.Block{
		ParentID_:      id(7),
		BlockHeight:    0,
		BlockTimestamp: zTimestamp,
		StateRoot:      zRoot(nil),
	}
	future := &zkvm.Block{
		ParentID_:      ids.Empty,
		BlockHeight:    0,
		BlockTimestamp: zFutureTimestamp,
		StateRoot:      zRoot(nil),
	}

	// One over the hundred a block may carry. The bound is what stops a peer
	// making every node verify a block none of them would have built.
	overCap := make([]*zkvm.Transaction, 101)
	for i := range overCap {
		overCap[i] = zTransfer(byte(i), starkProof())
	}

	v = append(v,
		vec("Z_EDGE_BAD_STATE_ROOT", "Z", "block", wrongRoot.Marshal()),
		vec("Z_EDGE_PARENT_AT_HEIGHT_ZERO", "Z", "block", parentAtZero.Marshal()),
		vec("Z_EDGE_FUTURE_TIMESTAMP", "Z", "block", future.Marshal()),
		vec("Z_EDGE_OVER_TX_CAP", "Z", "block", zBlock(overCap...)),
	)

	// ---- canonicality ----
	//
	// One value has one byte string. A frame the buffer does not exactly
	// account for is refused, in both directions.
	good := zBlock(transfer)
	truncated := append([]byte(nil), good[:len(good)/2]...)
	trailing := append(append([]byte(nil), good...), 0x00)

	v = append(v,
		vec("Z_EDGE_TRUNCATED", "Z", "block", truncated),
		vec("Z_EDGE_EMPTY", "Z", "block", []byte{}),
		vec("Z_EDGE_ONE_BYTE", "Z", "block", []byte{0x00}),
		vec("Z_EDGE_TRAILING_BYTES", "Z", "block", trailing),
	)

	v = append(v, Vector{ID: "Z_SEAM_BLOCK_REJECT", Chain: "Z", Op: "seam", Wire: none})
	return v
}
