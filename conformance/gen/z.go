// SPDX-License-Identifier: BSD-3-Clause-Eco

// The Z-chain half of the corpus, and the Go reference's answers for it.
//
// BUILDING. Unlike Q, the Z-chain exports its own encoder: `Transaction.Marshal`
// and `Block.Marshal` are the bytes the chain writes, and every vector below is
// built by calling them. Nothing here writes a field offset.
//
// ANSWERING. `evalZ` runs the real VM against a SEEDED chain: Initialize writes
// the height-0 block for the genesis it is given, and the vectors name that
// block as their parent, so a refusal is the rule that refused rather than a
// parent nobody has.
//
// The two layers are split where Verify splits them:
//
//	syntactic — what the block and its transactions say about themselves: the
//	            height/parent pairing, the transaction cap, the timestamp
//	            bound, a nullifier spent twice inside one block, and each
//	            transaction's own shape and expiry
//	exec      — what needed the chain: the proofs, the spent set, the parent
//	            and the tip it must sit on, and the state root it commits to
//
// The Z-chain's block id is a hash over CONTENT, not over the wire, and it
// opens with sha256(ChainID ‖ NetworkID) — which is not on the wire either. So
// two chains with identical blocks derive different ids, and that is what the
// Z_GENESIS vector pins.
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/luxfi/chains/zkvm"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/runtime"
	luxvm "github.com/luxfi/vm"
)

// The chain every Z vector belongs to, and the genesis it is born with. Both
// are hashed into every block id on it.
const (
	zChain      = 40
	zGenesisCfg = `{"timestamp":1000}`
)

func zBytes(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// zProof is a proof of the one system a strict-PQ Z-chain accepts. Whether it
// VERIFIES is the chain's business, and every implementation has to reach the
// same answer about it; what matters here is that it is the shape the chain
// reads, so the refusal comes from the verifier rather than from a missing
// field.
func zProof(kind string, seed byte) *zkvm.ZKProof {
	return &zkvm.ZKProof{
		ProofType:    kind,
		ProofData:    zBytes(seed, 192),
		PublicInputs: [][]byte{zBytes(seed+1, 32), zBytes(seed+2, 32)},
	}
}

func zShieldedOut(seed byte) *zkvm.ShieldedOutput {
	return &zkvm.ShieldedOutput{
		Commitment:      zBytes(seed, 32),
		EncryptedNote:   zBytes(seed+1, 64),
		EphemeralPubKey: zBytes(seed+2, 32),
		OutputProof:     zBytes(seed+3, 64),
	}
}

func zTransparentIn(seed byte, amount uint64) *zkvm.TransparentInput {
	return &zkvm.TransparentInput{
		TxID:      id(seed),
		OutputIdx: 0,
		Amount:    amount,
		Address:   zBytes(seed, 20),
	}
}

func zTransparentOut(seed byte, amount uint64) *zkvm.TransparentOutput {
	return &zkvm.TransparentOutput{
		Amount:  amount,
		Address: zBytes(seed, 20),
		AssetID: id(stakeAsset),
	}
}

// zTransfer is the shape the Z-chain's own well-formedness rules call a
// transfer: shielded in, shielded out. The other four kinds differ from it in
// exactly the fields their rule names.
func zTransfer(nullifier byte) *zkvm.Transaction {
	return &zkvm.Transaction{
		Type:       zkvm.TransactionTypeTransfer,
		Version:    1,
		Nullifiers: [][]byte{zBytes(nullifier, 32)},
		Outputs:    []*zkvm.ShieldedOutput{zShieldedOut(0x50)},
		Proof:      zProof("stark", 0x60),
		Fee:        1,
		Expiry:     1000,
	}
}

func zShield() *zkvm.Transaction {
	return &zkvm.Transaction{
		Type:              zkvm.TransactionTypeShield,
		Version:           1,
		TransparentInputs: []*zkvm.TransparentInput{zTransparentIn(0x70, 500)},
		Outputs:           []*zkvm.ShieldedOutput{zShieldedOut(0x51)},
		Proof:             zProof("stark", 0x61),
		Fee:               1,
		Expiry:            1000,
	}
}

func zUnshield() *zkvm.Transaction {
	return &zkvm.Transaction{
		Type:               zkvm.TransactionTypeUnshield,
		Version:            1,
		Nullifiers:         [][]byte{zBytes(0x11, 32)},
		TransparentOutputs: []*zkvm.TransparentOutput{zTransparentOut(0x71, 400)},
		Proof:              zProof("stark", 0x62),
		Fee:                1,
		Expiry:             1000,
	}
}

func zMint() *zkvm.Transaction {
	tx := zTransfer(0x12)
	tx.Type = zkvm.TransactionTypeMint
	return tx
}

func zBurn() *zkvm.Transaction {
	tx := zTransfer(0x13)
	tx.Type = zkvm.TransactionTypeBurn
	return tx
}

// zBlockWire assembles a block through the chain's own encoder. The state root
// is the chain's own too — `Root.After` over an empty store, which is the state
// every vector meets — so a block that is otherwise correct is not refused for
// a number this file made up.
func zBlockWire(parent ids.ID, height uint64, timestamp int64, txs []*zkvm.Transaction, root []byte) []byte {
	b := &zkvm.Block{
		ParentID_:      parent,
		BlockHeight:    height,
		BlockTimestamp: timestamp,
		Txs:            txs,
		StateRoot:      root,
	}
	wire, err := b.Marshal()
	must(err)
	return wire
}

func zVectors() []Vector {
	root, err := zStateRoot()
	must(err)
	parent, err := zGenesisID()
	must(err)

	// The chain's own genesis: height 0, no parent, the timestamp the genesis
	// file names, and no state root, because the VM writes it that way.
	genesis := zBlockWire(ids.Empty, 0, 1000, nil, nil)

	empty := zBlockWire(parent, 1, 1000, nil, root.of(nil))
	transfer := zTransfer(0x10)
	one := []*zkvm.Transaction{transfer}

	// Two transactions spending the same shielded note. Each is well formed on
	// its own; together they are one note spent twice, which is supply out of
	// nothing if the block does not look across its own transactions.
	twin := zTransfer(0x10)
	twin.Fee = 2

	noInputs := zTransfer(0x14)
	noInputs.Nullifiers = nil
	noOutputs := zTransfer(0x15)
	noOutputs.Outputs = nil
	noProof := zTransfer(0x16)
	noProof.Proof = nil
	noExpiry := zTransfer(0x17)
	noExpiry.Expiry = 0
	badType := zTransfer(0x18)
	badType.Type = zkvm.TransactionTypeUnshield + 7
	expired := zTransfer(0x19)
	expired.Expiry = 1 // below the height 2 block that carries it

	// A proof under a classical, pairing-based system. This chain is strict-PQ,
	// so it is refused for what it IS rather than for failing to verify — a
	// machine that broke bn254 must not be able to forge a shielded proof.
	classical := zTransfer(0x1a)
	classical.Proof = zProof("groth16", 0x63)

	good := zBlockWire(parent, 1, 1000, one, root.of(one))

	// The same block with bytes appended past the frame's declared size. The
	// Z-chain's parser refuses a frame that does not account for every byte it
	// was handed.
	trailing := append(append([]byte{}, good...), 0xFF, 0xFF)

	v := []Vector{
		vec("Z_GENESIS", "Z", "block", genesis),
		vec("Z_BLOCK_EMPTY", "Z", "block", empty),
		vec("Z_BLOCK_TRANSFER", "Z", "block", good),
		vec("Z_BLOCK_SHIELD", "Z", "block", zOne(parent, root, zShield())),
		vec("Z_BLOCK_UNSHIELD", "Z", "block", zOne(parent, root, zUnshield())),
		vec("Z_BLOCK_MINT", "Z", "block", zOne(parent, root, zMint())),
		vec("Z_BLOCK_BURN", "Z", "block", zOne(parent, root, zBurn())),

		// Carries nothing, so nothing but the parent can refuse it: this chain
		// checks a block's transactions BEFORE it looks its parent up, and a
		// vector with a transaction would be refused for the proof and never
		// reach the lookup this vector exists to exercise.
		vec("Z_BLOCK_ORPHAN", "Z", "block",
			zBlockWire(id(1), 1, 1000, nil, root.of(nil))),
		vec("Z_BLOCK_HEIGHT_SKIP", "Z", "block",
			zBlockWire(parent, 5, 1000, nil, root.of(nil))),
		vec("Z_BLOCK_TIME_BEFORE_PARENT", "Z", "block",
			zBlockWire(parent, 1, 999, nil, root.of(nil))),
		// Far enough ahead that no clock this runs on is inside the skew
		// allowance, and far enough from any real date that it stays that way.
		vec("Z_BLOCK_TIME_AHEAD", "Z", "block",
			zBlockWire(parent, 1, 1<<40, nil, root.of(nil))),
		// Height 0 is genesis, and genesis has no parent. A height-0 block
		// that names one is two claims about which block it is.
		vec("Z_BLOCK_GENESIS_WITH_PARENT", "Z", "block",
			zBlockWire(parent, 0, 1000, nil, nil)),
		vec("Z_BLOCK_WRONG_STATE_ROOT", "Z", "block",
			zBlockWire(parent, 1, 1000, nil, zBytes(0xAB, 32))),

		vec("Z_BLOCK_DUPLICATE_NULLIFIER", "Z", "block",
			zBlockWire(parent, 1, 1000, []*zkvm.Transaction{transfer, twin},
				root.of([]*zkvm.Transaction{transfer, twin}))),
		vec("Z_TX_NO_INPUTS", "Z", "block", zOne(parent, root, noInputs)),
		vec("Z_TX_NO_OUTPUTS", "Z", "block", zOne(parent, root, noOutputs)),
		vec("Z_TX_NO_PROOF", "Z", "block", zOne(parent, root, noProof)),
		vec("Z_TX_NO_EXPIRY", "Z", "block", zOne(parent, root, noExpiry)),
		vec("Z_TX_UNKNOWN_TYPE", "Z", "block", zOne(parent, root, badType)),
		vec("Z_TX_EXPIRED", "Z", "block",
			zBlockWire(parent, 2, 1000, []*zkvm.Transaction{expired},
				root.of([]*zkvm.Transaction{expired}))),
		vec("Z_TX_CLASSICAL_PROOF", "Z", "block", zOne(parent, root, classical)),

		vec("Z_BLOCK_TRAILING_BYTES", "Z", "block", trailing),
		vec("Z_BLOCK_TRUNCATED", "Z", "block", good[:len(good)/2]),
		vec("Z_BLOCK_EMPTY_WIRE", "Z", "block", nil),
		vec("Z_BLOCK_ONE_BYTE", "Z", "block", []byte{0x5a}),
	}

	zAssert(v)
	return v
}

func zOne(parent ids.ID, root zRoot, tx *zkvm.Transaction) []byte {
	txs := []*zkvm.Transaction{tx}
	return zBlockWire(parent, 1, 1000, txs, root.of(txs))
}

// zAssert holds the emit to the two claims this half of the corpus rests on.
func zAssert(v []Vector) {
	by := map[string]Result{}
	for _, vec := range v {
		by[vec.ID] = evalZ(vec)
	}

	// One: every vector meant to be a block is one.
	for _, want := range v {
		if strings.HasSuffix(want.ID, "_TRAILING_BYTES") ||
			strings.HasSuffix(want.ID, "_TRUNCATED") ||
			strings.HasSuffix(want.ID, "_EMPTY_WIRE") ||
			strings.HasSuffix(want.ID, "_ONE_BYTE") {
			continue
		}
		if by[want.ID].Parse != "ok" {
			panic(fmt.Sprintf("%s is not the wire the Z-chain writes: %s", want.ID, by[want.ID].Note))
		}
	}

	// Two: the chain is SEEDED, and the state root is the chain's own. An
	// empty block on this chain's genesis has to VERIFY — if it does not, the
	// parent is not found or the root was computed against a different store,
	// and every exec answer below would be about that instead of about the
	// rule under test.
	// Z_BLOCK_EMPTY and Z_BLOCK_ORPHAN are the same block but for the parent
	// they name. One must verify and the other must not, and the difference
	// has to be the ledger.
	if by["Z_BLOCK_EMPTY"].Exec != VOK {
		panic("Z_BLOCK_EMPTY does not verify on the seeded chain: " + by["Z_BLOCK_EMPTY"].Note)
	}
	if by["Z_BLOCK_ORPHAN"].Exec != VLedger {
		panic("Z_BLOCK_ORPHAN was expected to name a parent no chain holds: " + by["Z_BLOCK_ORPHAN"].Note)
	}
}

// zRoot is the Z-chain's own state tree, folded through the chain's own
// genesis step.
//
// It is the chain's `Root` type running the chain's own two calls: Initialize
// commits `After(genesis transactions)` before any block, so a chain born from
// a genesis with no transactions still starts one fold in, not at the zero
// root. Replaying that here is the only way a vector can name the root the
// reference will compute — and it is not taken on trust: emit refuses to write
// a corpus in which the reference does not accept `Z_BLOCK_EMPTY`, which is
// exactly the claim that this fold matched.
type zRoot struct{ r *zkvm.Root }

func (z zRoot) of(txs []*zkvm.Transaction) []byte { return z.r.After(txs) }

func zStateRoot() (zRoot, error) {
	r, err := zkvm.NewRoot(memdb.New(), log.Noop())
	if err != nil {
		return zRoot{}, err
	}
	// The corpus's genesis carries no initial transactions, so this is the
	// same fold `processGenesisTransactions` performs over the same empty set.
	if err := r.Finalize(r.After(nil)); err != nil {
		return zRoot{}, err
	}
	return zRoot{r}, nil
}

// zvm is a Z-chain VM over an empty database, seeded with the corpus's genesis.
// One per process; every vector is a fresh question to the same chain.
var (
	zvmOnce  sync.Once
	zvmChain *zkvm.VM
	zvmErr   error
)

func zvm() (*zkvm.VM, error) {
	zvmOnce.Do(func() { zvmChain, zvmErr = startZvm() })
	return zvmChain, zvmErr
}

func startZvm() (*zkvm.VM, error) {
	// Initialize announces itself, on stdout. Stdout is the result stream the
	// runner reads, so it is pointed at stderr for the duration.
	stdout := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = stdout }()

	vm := &zkvm.VM{}
	err := vm.Initialize(context.Background(), luxvm.Init{
		Runtime: &runtime.Runtime{
			NodeID:    nodeID(1),
			NetworkID: networkID,
			ChainID:   id(zChain),
			Log:       log.Noop(),
		},
		DB:      memdb.New(),
		Log:     log.Noop(),
		Genesis: []byte(zGenesisCfg),
	})
	if err != nil {
		return nil, err
	}
	return vm, nil
}

func zGenesisID() (ids.ID, error) {
	vm, err := zvm()
	if err != nil {
		return ids.Empty, err
	}
	return vm.LastAccepted(context.Background())
}

// zBlockAlone says whether a refusal was reached without the chain.
//
// The words are the reference's own sentinels, quoted from
// `chains/zkvm/block.go` and `chains/zkvm/transaction.go`. Everything not named
// here needed the spent set, the proof verifier, the parent or the tip.
func zBlockAlone(msg string) bool {
	for _, w := range []string{
		"invalid block",                     // height 0 with a parent, or over the tx cap
		"block timestamp too far in future", //
		"nullifier spent twice in one block",
		"invalid transaction type",
		"transaction has no inputs",
		"transaction has no outputs",
		"transaction missing proof",
		"transaction names no expiry height",
		"transaction has expired",
		"invalid transfer transaction",
		"invalid shield transaction",
		"invalid unshield transaction",
	} {
		if strings.Contains(msg, w) {
			// "invalid block height" and "invalid block timestamp" both start
			// with "invalid block", and both needed the parent to notice.
			if w == "invalid block" &&
				(strings.Contains(msg, "invalid block height") ||
					strings.Contains(msg, "invalid block timestamp")) {
				continue
			}
			return true
		}
	}
	return false
}

func evalZ(v Vector) Result {
	r := Result{ID: v.ID, Kind: none, Hash: none, Syntactic: none, Exec: none}
	b, ok := wireOf(v)
	if !ok {
		r.Parse = VInternal
		r.Note = "corpus wire is not hex"
		return r
	}
	vm, err := zvm()
	if err != nil {
		r.Parse = VInternal
		r.Note = "the reference VM did not start: " + err.Error()
		return r
	}

	blk, err := vm.ParseBlock(context.Background(), b)
	if err != nil {
		r.Parse = VMalformed
		r.Syntactic = VMalformed
		r.Exec = VMalformed
		r.Note = trim(err.Error())
		return r
	}
	r.Parse = "ok"
	r.Kind = "ShieldedBlock"
	blockID := blk.ID()
	r.Hash = hex.EncodeToString(blockID[:])

	err = blk.Verify(context.Background())
	if err == nil {
		r.Syntactic = VOK
		r.Exec = VOK
		r.Note = "verified against the seeded chain"
		return r
	}
	class := classify(err)
	if zBlockAlone(err.Error()) {
		r.Syntactic = class
		r.Exec = class
	} else {
		r.Syntactic = VOK
		r.Exec = class
	}
	r.Note = trim(err.Error())
	return r
}
