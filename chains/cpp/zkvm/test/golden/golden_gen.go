// Copyright (C) 2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco
//
// Reference-vector generator: builds Z-Chain values with the GO implementation
// (github.com/luxfi/chains/zkvm) and prints their canonical wire bytes, ids and
// roots as a C++ header. The C++ port's tests assert against these verbatim, so
// the two implementations cannot drift without a test failing.
//
// Every value below comes OUT of the reference — a block id is read off a block
// the Go VM built and accepted, not recomputed here from a formula this file
// happens to agree with.
//
// Run from the chains checkout:
//
//	GOWORK=off go run ./zkvm_golden_gen.go > golden.hpp
//
// The build tag keeps it OUT of this repo's own module: it depends on the Go
// chain suite, which node2 deliberately does not. A file named on the command
// line is compiled regardless of the tag, which is the whole point of `ignore`.

//go:build ignore

package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"

	"github.com/luxfi/chains/zkvm"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/runtime"
	vmcore "github.com/luxfi/vm"
)

func emit(name string, b []byte) {
	fmt.Printf("inline constexpr const char* %s =\n    \"%x\";\n", name, b)
}

func emitU64(name string, v uint64) {
	fmt.Printf("inline constexpr std::uint64_t %s = %d;\n", name, v)
}

// ---- the fixed identities every vector is built against ----

var testBind = [32]byte{
	0x5A, 0xC1, 0x00, 0xDE, 0xAD, 0xBE, 0xEF, 0x11,
	0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99,
	0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x01, 0x02,
	0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A,
}

// chainID is fixed so the VM's binding — sha256(ChainID ‖ NetworkID) — is a
// constant of the corpus rather than a fresh value per run.
func chainID() ids.ID {
	var id ids.ID
	for i := range id {
		id[i] = byte(i) + 1
	}
	return id
}

const networkID = uint32(96369)

func chainBind() [32]byte {
	h := sha256.New()
	cid := chainID()
	h.Write(cid[:])
	_ = binary.Write(h, binary.BigEndian, networkID)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// ---- groth16 fixtures, identical to the Go tests' own helpers ----

func groth16Key(numK uint32) []byte {
	_, _, g1, g2 := bn254.Generators()
	out := make([]byte, 0, 452+int(numK)*64)
	out = append(out, g1.Marshal()...)
	out = append(out, g2.Marshal()...)
	out = append(out, g2.Marshal()...)
	out = append(out, g2.Marshal()...)
	n := make([]byte, 4)
	binary.BigEndian.PutUint32(n, numK)
	out = append(out, n...)
	for i := uint32(0); i < numK; i++ {
		out = append(out, g1.Marshal()...)
	}
	return out
}

func groth16Frame() []byte {
	_, _, g1, g2 := bn254.Generators()
	out := make([]byte, 0, 256)
	out = append(out, g1.Marshal()...)
	out = append(out, g2.Marshal()...)
	out = append(out, g1.Marshal()...)
	return out
}

// satisfying is the Go test's own construction: with Beta = Gamma = Delta = B =
// g2 the equation collapses by bilinearity to A = alpha + LC + C, which is
// solvable because the fixture chooses the key.
func satisfying(witness []fr.Element) (vk []byte, proof []byte) {
	_, _, g1, g2 := bn254.Generators()

	var alpha, c bn254.G1Affine
	alpha.ScalarMultiplication(&g1, big.NewInt(7))
	c.ScalarMultiplication(&g1, big.NewInt(11))

	k := make([]bn254.G1Affine, len(witness)+1)
	for i := range k {
		k[i].ScalarMultiplication(&g1, big.NewInt(int64(i)+2))
	}

	var lc bn254.G1Affine
	lc.Set(&k[0])
	var scalar big.Int
	for i := range witness {
		var term bn254.G1Affine
		term.ScalarMultiplication(&k[i+1], witness[i].BigInt(&scalar))
		lc.Add(&lc, &term)
	}

	var a bn254.G1Affine
	a.Set(&alpha)
	a.Add(&a, &lc)
	a.Add(&a, &c)

	key := make([]byte, 0, 64+128*3+4+64*len(k))
	key = append(key, alpha.Marshal()...)
	key = append(key, g2.Marshal()...)
	key = append(key, g2.Marshal()...)
	key = append(key, g2.Marshal()...)
	n := make([]byte, 4)
	binary.BigEndian.PutUint32(n, uint32(len(k)))
	key = append(key, n...)
	for i := range k {
		key = append(key, k[i].Marshal()...)
	}

	frame := make([]byte, 0, 256)
	frame = append(frame, a.Marshal()...)
	frame = append(frame, g2.Marshal()...)
	frame = append(frame, c.Marshal()...)
	return key, frame
}

func field(b []byte) fr.Element {
	var e fr.Element
	e.SetBytes(b)
	return e
}

// ---- transactions ----

func shieldTx() *zkvm.Transaction {
	return &zkvm.Transaction{
		Type:               zkvm.TransactionTypeShield,
		Version:            1,
		TransparentInputs:  []*zkvm.TransparentInput{{TxID: ids.ID{1}, OutputIdx: 0, Amount: 100, Address: []byte("payer")}},
		TransparentOutputs: []*zkvm.TransparentOutput{{Amount: 90, AssetID: ids.ID{2}, Address: []byte("payee")}},
		Nullifiers:         [][]byte{[]byte("n")},
		Outputs:            []*zkvm.ShieldedOutput{{Commitment: []byte("c")}},
		Proof:              &zkvm.ZKProof{ProofType: "groth16", ProofData: []byte("p")},
		Fee:                7,
	}
}

func blockTx(fee uint64) *zkvm.Transaction {
	tx := &zkvm.Transaction{
		Type:               zkvm.TransactionTypeShield,
		Version:            1,
		Fee:                fee,
		TransparentInputs:  []*zkvm.TransparentInput{{TxID: ids.ID{1}, OutputIdx: 2, Amount: 100, Address: []byte("payer")}},
		TransparentOutputs: []*zkvm.TransparentOutput{{Amount: 90, AssetID: ids.ID{2}, Address: []byte("payee")}},
		Nullifiers:         [][]byte{[]byte("n1"), []byte("n2")},
		Outputs:            []*zkvm.ShieldedOutput{{Commitment: []byte("c"), EncryptedNote: []byte("n"), EphemeralPubKey: []byte("e"), OutputProof: []byte("p")}},
		Proof:              &zkvm.ZKProof{ProofType: "groth16", ProofData: []byte("pd"), PublicInputs: [][]byte{[]byte("a"), []byte("bb")}},
		Expiry:             1 << 20,
		Memo:               []byte("memo"),
	}
	tx.ID = tx.ComputeID()
	return tx
}

// wireTx is the transaction wire_test.go round-trips: something in every list.
func wireTx() *zkvm.Transaction {
	return &zkvm.Transaction{
		Type:    zkvm.TransactionTypeShield,
		Version: 1,
		Fee:     100,
		Expiry:  999,
		TransparentInputs: []*zkvm.TransparentInput{
			{TxID: ids.ID{4}, OutputIdx: 2, Amount: 50, Address: []byte("addr-in")},
		},
		TransparentOutputs: []*zkvm.TransparentOutput{
			{Amount: 30, AssetID: ids.ID{5}, Address: []byte("addr-out")},
		},
		Nullifiers: [][]byte{[]byte("null1"), []byte("null2")},
		Outputs: []*zkvm.ShieldedOutput{
			{Commitment: []byte("c"), EncryptedNote: []byte("n"), EphemeralPubKey: []byte("e"), OutputProof: []byte("p")},
		},
		Proof: &zkvm.ZKProof{ProofType: "groth16", ProofData: []byte("pd"), PublicInputs: [][]byte{[]byte("pi1"), []byte("pi2")}},
		Memo:  []byte("memo"),
	}
}

// ---- a live chain, so block ids come off blocks the reference accepted ----

// spendingTx is a transfer whose groth16 proof SATISFIES the pairing equation
// for the key emitted beside it, so the reference VM admits it, builds a block
// around it and accepts that block.
func spendingTx(n byte, expiry uint64) (*zkvm.Transaction, []byte) {
	bind := chainBind()
	nullifier := make([]byte, 32)
	nullifier[0] = n
	commitment := make([]byte, 32)
	commitment[0] = n ^ 0xFF

	witness := []fr.Element{field(bind[:]), field(nullifier), field(commitment)}
	key, frame := satisfying(witness)

	tx := &zkvm.Transaction{
		Type:       zkvm.TransactionTypeTransfer,
		Version:    1,
		Fee:        1_000_000,
		Expiry:     expiry,
		Nullifiers: [][]byte{nullifier},
		Outputs:    []*zkvm.ShieldedOutput{{Commitment: commitment, EncryptedNote: []byte("note")}},
		Proof: &zkvm.ZKProof{
			ProofType:    "groth16",
			ProofData:    frame,
			PublicInputs: [][]byte{bind[:], nullifier, commitment},
		},
	}
	tx.ID = tx.ComputeID()
	return tx, key
}

func liveChain() {
	tx, key := spendingTx(0xA1, 1<<20)

	cfg := zkvm.ZConfig{
		StrictPQ:       false,
		MaxTxPerBlock:  100,
		ProofCacheSize: 1000,
		VerifyingKeys:  map[string][]byte{string(zkvm.TransactionTypeTransfer): key},
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}

	logger := log.NewNoOpLogger()
	rt := &runtime.Runtime{ChainID: chainID(), NetworkID: networkID, Log: logger}
	vm := &zkvm.VM{}
	if err := vm.Initialize(context.Background(), vmcore.Init{
		Runtime:  rt,
		DB:       memdb.New(),
		ToEngine: make(chan vmcore.Message, 8),
		Log:      logger,
		Config:   cfgBytes,
		Genesis:  []byte(`{"timestamp":0}`),
	}); err != nil {
		panic(err)
	}

	emit("kSpendingTx", tx.Marshal())
	txID := tx.ComputeID()
	emit("kSpendingTxID", txID[:])
	emit("kSpendingVerifyingKey", key)

	genesisID, err := vm.LastAccepted(context.Background())
	if err != nil {
		panic(err)
	}
	emit("kGenesisBlockID", genesisID[:])

	// The state root a block over this one transaction commits to, read off the
	// reference's own fold: the genesis root, then the transaction on top.
	root, err := zkvm.NewRoot(memdb.New(), logger)
	if err != nil {
		panic(err)
	}
	emit("kGenesisStateRoot", root.After(nil))
	if err := root.Finalize(root.After(nil)); err != nil {
		panic(err)
	}
	stateRoot := root.After([]*zkvm.Transaction{tx})
	emit("kBlockOneStateRoot", stateRoot)

	// The block is stated with a FIXED timestamp — a proposer reads the wall
	// clock, and a corpus that did the same would name a different block every
	// run. Its id is still the reference's: ParseBlock derives it from the
	// content, under the chain binding the VM was initialized with.
	const blockTime = int64(1_600_000_000)
	stated := &zkvm.Block{
		ParentID_:      genesisID,
		BlockHeight:    1,
		BlockTimestamp: blockTime,
		Txs:            []*zkvm.Transaction{tx},
		StateRoot:      stateRoot,
	}
	raw := stated.Marshal()
	emit("kBlockOne", raw)
	fmt.Printf("inline constexpr std::int64_t kBlockOneTimestamp = %d;\n", blockTime)

	parsed, err := vm.ParseBlock(context.Background(), raw)
	if err != nil {
		panic(err)
	}
	blk := parsed.(*zkvm.Block)
	bid := blk.ID()
	emit("kBlockOneID", bid[:])
	emitU64("kBlockOneHeight", blk.BlockHeight)

	if err := blk.Verify(context.Background()); err != nil {
		panic(err)
	}
	if err := blk.Accept(context.Background()); err != nil {
		panic(err)
	}
	tip, err := vm.LastAccepted(context.Background())
	if err != nil {
		panic(err)
	}
	emit("kTipAfterOneBlock", tip[:])
}

func main() {
	fmt.Println("// Copyright (C) 2026, Lux Industries Inc. All rights reserved.")
	fmt.Println("// SPDX-License-Identifier: BSD-3-Clause-Eco")
	fmt.Println("//")
	fmt.Println("// GENERATED by chains/cpp/zkvm/test/golden/golden_gen.go — do not edit.")
	fmt.Println("//")
	fmt.Println("// Every constant is a value produced by the GO Z-Chain")
	fmt.Println("// (github.com/luxfi/chains/zkvm) and its gnark-crypto bn254. They are the")
	fmt.Println("// port's contract: the C++ implementation must produce these bytes, these")
	fmt.Println("// ids and these roots, or the two are different chains.")
	fmt.Println("#pragma once")
	fmt.Println("#include <cstdint>")
	fmt.Println("namespace lux::zkvm::golden {")

	bind := chainBind()
	emit("kChainId", func() []byte { c := chainID(); return c[:] }())
	fmt.Printf("inline constexpr std::uint32_t kNetworkId = %d;\n", networkID)
	emit("kChainBind", bind[:])
	emit("kTestBind", testBind[:])

	// --- transactions ---
	st := shieldTx()
	emit("kShieldTx", st.Marshal())
	stID := st.ComputeID()
	emit("kShieldTxID", stID[:])

	wt := wireTx()
	emit("kWireTx", wt.Marshal())
	wtID := wt.ComputeID()
	emit("kWireTxID", wtID[:])

	bt1, bt2 := blockTx(1), blockTx(2)
	emit("kBlockTxFee1", bt1.Marshal())
	emit("kBlockTxFee2", bt2.Marshal())
	b1 := bt1.ComputeID()
	emit("kBlockTxFee1ID", b1[:])

	// A block frame around two of them, exactly as block_test.go builds it.
	blk := &zkvm.Block{
		ParentID_:      ids.ID{0xEE},
		BlockHeight:    7,
		BlockTimestamp: 1_600_000_000,
		Txs:            []*zkvm.Transaction{bt1, bt2},
		StateRoot:      []byte("state-root"),
	}
	emit("kBlockTwoTx", blk.Marshal())

	// --- utxo ---
	u := &zkvm.UTXO{
		TxID:        ids.ID{1, 2, 3},
		OutputIndex: 7,
		Height:      42,
		Commitment:  []byte("commit"),
		Ciphertext:  []byte("cipher"),
		EphemeralPK: []byte("epk"),
	}
	emit("kUtxo", u.Marshal())

	// --- groth16 fixtures ---
	emit("kGroth16Key2", groth16Key(2))
	emit("kGroth16Key3", groth16Key(3))
	emit("kGroth16Key4", groth16Key(4))
	emit("kGroth16Frame", groth16Frame())

	nullifier := make([]byte, 32)
	nullifier[0] = 0xA1
	commitment := make([]byte, 32)
	commitment[0] = 0xC1
	satVK, satProof := satisfying([]fr.Element{field(testBind[:]), field(nullifier), field(commitment)})
	emit("kSatisfyingKey", satVK)
	emit("kSatisfyingProof", satProof)
	emit("kSatisfyingNullifier", nullifier)
	emit("kSatisfyingCommitment", commitment)

	// --- point encodings: the same points compressed and uncompressed ---
	_, _, g1, g2 := bn254.Generators()
	for i, s := range []int64{1, 2, 3, 7, 11} {
		var p bn254.G1Affine
		p.ScalarMultiplication(&g1, big.NewInt(s))
		emit(fmt.Sprintf("kG1Uncompressed%d", i), p.Marshal())
		cb := p.Bytes()
		emit(fmt.Sprintf("kG1Compressed%d", i), cb[:])

		var q bn254.G2Affine
		q.ScalarMultiplication(&g2, big.NewInt(s))
		emit(fmt.Sprintf("kG2Uncompressed%d", i), q.Marshal())
		qb := q.Bytes()
		emit(fmt.Sprintf("kG2Compressed%d", i), qb[:])
	}
	fmt.Println("inline constexpr int kPointPairs = 5;")

	// --- scalar field: bytes in, reduced element out ---
	frInputs := [][]byte{
		{},
		{0x01},
		{0xFF, 0xFF},
		testBind[:],
		nullifier,
		func() []byte { // one byte string above the group order, so it reduces
			b := make([]byte, 32)
			for i := range b {
				b[i] = 0xFF
			}
			return b
		}(),
		func() []byte { // longer than an element, which the reference also accepts
			b := make([]byte, 48)
			for i := range b {
				b[i] = byte(i + 1)
			}
			return b
		}(),
	}
	for i, in := range frInputs {
		e := field(in)
		out := e.Bytes()
		emit(fmt.Sprintf("kFrIn%d", i), in)
		emit(fmt.Sprintf("kFrOut%d", i), out[:])
	}
	fmt.Printf("inline constexpr int kFrCases = %d;\n", len(frInputs))

	// --- a live chain ---
	liveChain()

	fmt.Println("}  // namespace lux::zkvm::golden")
}
