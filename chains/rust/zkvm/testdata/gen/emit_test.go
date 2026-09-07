// SPDX-License-Identifier: BSD-3-Clause-Eco
// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.

package zkvm

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fp"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/luxfi/chains/zkvm/precompiles"
	"github.com/luxfi/ids"
)

// Two tests, in this order and in one file. Go runs a file's tests in the order
// they are written; across files it runs them in filename order, which put the
// check before the emitter and read a file that did not exist yet.

// Golden-vector emitter. Runs inside the reference package through a `go
// -overlay` map, so nothing is written into the reference checkout: every
// number below is produced by the Go code being ported, not restated beside it.

type vec struct {
	Name string `json:"name"`
	Hex  string `json:"hex"`
}

func h(b []byte) string { return hex.EncodeToString(b) }

func TestEmitGolden(t *testing.T) {
	out := map[string]any{}
	var vecs []vec
	add := func(n string, b []byte) { vecs = append(vecs, vec{n, h(b)}) }

	// ---- the chain binding: sha256(chainID ‖ networkID big-endian u32) ----
	chainID := ids.ID{0xAA, 0xBB, 0xCC}
	const networkID = uint32(96369)
	bh := sha256.New()
	bh.Write(chainID[:])
	binary.Write(bh, binary.BigEndian, networkID)
	var bind [32]byte
	copy(bind[:], bh.Sum(nil))
	add("bind", bind[:])

	vm := &VM{bind: bind}

	// ---- transactions ----
	txFull := &Transaction{
		Type:    TransactionTypeShield,
		Version: 1,
		Fee:     100,
		Expiry:  999,
		TransparentInputs: []*TransparentInput{
			{TxID: ids.ID{4}, OutputIdx: 2, Amount: 50, Address: []byte("addr-in")},
		},
		TransparentOutputs: []*TransparentOutput{
			{Amount: 30, AssetID: ids.ID{5}, Address: []byte("addr-out")},
		},
		Nullifiers: [][]byte{[]byte("null1"), []byte("null2")},
		Outputs: []*ShieldedOutput{
			{Commitment: []byte("c"), EncryptedNote: []byte("n"), EphemeralPubKey: []byte("e"), OutputProof: []byte("p")},
		},
		Proof: &ZKProof{ProofType: "groth16", ProofData: []byte("pd"), PublicInputs: [][]byte{[]byte("pi1"), []byte("pi2")}},
		Memo:  []byte("memo"),
	}
	txMin := &Transaction{
		Type:       TransactionTypeTransfer,
		Fee:        1,
		Nullifiers: [][]byte{[]byte("x")},
	}
	txNoProof := &Transaction{
		Type:               TransactionTypeUnshield,
		Version:            3,
		Fee:                7,
		Expiry:             1 << 40,
		Nullifiers:         [][]byte{{0x00}, {0xff, 0xee}},
		TransparentOutputs: []*TransparentOutput{{Amount: 1 << 63, AssetID: ids.ID{9}, Address: nil}},
	}
	txEmptyLists := &Transaction{Type: TransactionTypeMint, Version: 0, Fee: 0, Expiry: 0}

	for _, tc := range []struct {
		n  string
		tx *Transaction
	}{
		{"tx_full", txFull},
		{"tx_min", txMin},
		{"tx_no_proof", txNoProof},
		{"tx_empty", txEmptyLists},
	} {
		add(tc.n+".wire", tc.tx.Marshal())
		id := tc.tx.ComputeID()
		add(tc.n+".id", id[:])
	}

	// ---- utxo ----
	u := &UTXO{
		TxID:        ids.ID{1, 2, 3},
		OutputIndex: 7,
		Height:      42,
		Commitment:  []byte("commit"),
		Ciphertext:  []byte("cipher"),
		EphemeralPK: []byte("epk"),
	}
	add("utxo.wire", u.Marshal())

	// ---- the state-root fold, over a non-zero committed root ----
	committed := sha256.Sum256([]byte("committed"))
	r := &Root{committed: committed[:]}
	add("root.committed", committed[:])
	add("root.after_full_min", r.After([]*Transaction{txFull, txMin}))
	add("root.after_none", r.After(nil))
	zero := &Root{committed: make([]byte, 32)}
	add("root.after_zero_full", zero.After([]*Transaction{txFull}))

	// ---- block ----
	txFull.ID = txFull.ComputeID()
	txMin.ID = txMin.ComputeID()
	blk := &Block{
		ParentID_:      ids.ID{2},
		BlockHeight:    5,
		BlockTimestamp: 1_700_000_000,
		Txs:            []*Transaction{txFull, txMin},
		StateRoot:      []byte("root"),
		vm:             vm,
	}
	add("block.wire", blk.Marshal())
	bid := blk.computeID()
	add("block.id", bid[:])

	blkEmpty := &Block{ParentID_: ids.Empty, BlockHeight: 0, BlockTimestamp: 0, vm: vm}
	add("block_empty.wire", blkEmpty.Marshal())
	beid := blkEmpty.computeID()
	add("block_empty.id", beid[:])

	// negative timestamps are representable on the wire: i64, not u64.
	blkNeg := &Block{ParentID_: ids.ID{7}, BlockHeight: 1, BlockTimestamp: -3, StateRoot: nil, vm: vm}
	add("block_neg.wire", blkNeg.Marshal())
	bnid := blkNeg.computeID()
	add("block_neg.id", bnid[:])

	// ---- vertex ----
	vtx := &Vertex{
		height:  9,
		epoch:   2,
		parents: []ids.ID{{1}, {2}},
		txs:     []*Transaction{txFull, txMin},
		vm:      vm,
	}
	add("vertex.wire", vtx.serialize())
	vid := vtx.computeID()
	add("vertex.id", vid[:])

	// ---- gnark point encodings, so the reader is checked against gnark ----
	_, _, g1, g2 := bn254.Generators()
	add("g1.gen", g1.Marshal())
	add("g2.gen", g2.Marshal())
	var g1x2 bn254.G1Affine
	g1x2.ScalarMultiplication(&g1, big.NewInt(2))
	add("g1.two", g1x2.Marshal())
	var g2x3 bn254.G2Affine
	g2x3.ScalarMultiplication(&g2, big.NewInt(3))
	add("g2.three", g2x3.Marshal())

	// ---- a Groth16 instance that genuinely satisfies the pairing equation ----
	//
	// With beta = gamma = delta = g2 the check collapses to an exponent
	// identity: e(A,g2) == e(g1,g2)^(a + lc + c). Choosing A = g1^(a+lc+c)
	// satisfies it exactly, so this is a real verifying instance rather than a
	// shape that resembles one — and the ONLY thing being ported is the check.
	buildInstance := func(pubs [][]byte, a, c int64) (vkBytes, proofBytes []byte) {
		witness := make([]fr.Element, len(pubs))
		for i, p := range pubs {
			witness[i].SetBytes(p)
		}
		// k_i are small distinct scalars.
		ks := make([]*big.Int, len(witness)+1)
		for i := range ks {
			ks[i] = big.NewInt(int64(i)*7 + 3)
		}
		K := make([]bn254.G1Affine, len(ks))
		for i := range ks {
			K[i].ScalarMultiplication(&g1, ks[i])
		}
		// lc = k0 + sum(w_i * k_{i+1}) in fr
		var lc fr.Element
		lc.SetBigInt(ks[0])
		for i := range witness {
			var ki, term fr.Element
			ki.SetBigInt(ks[i+1])
			term.Mul(&witness[i], &ki)
			lc.Add(&lc, &term)
		}
		var av, cv, total fr.Element
		av.SetInt64(a)
		cv.SetInt64(c)
		total.Add(&av, &lc)
		total.Add(&total, &cv)

		var alpha, C, A bn254.G1Affine
		var s big.Int
		alpha.ScalarMultiplication(&g1, av.BigInt(&s))
		C.ScalarMultiplication(&g1, cv.BigInt(&s))
		A.ScalarMultiplication(&g1, total.BigInt(&s))

		// vk: Alpha(64) | Beta(128) | Gamma(128) | Delta(128) | numK(4) | K[]
		vkBytes = append(vkBytes, alpha.Marshal()...)
		vkBytes = append(vkBytes, g2.Marshal()...)
		vkBytes = append(vkBytes, g2.Marshal()...)
		vkBytes = append(vkBytes, g2.Marshal()...)
		n := make([]byte, 4)
		binary.BigEndian.PutUint32(n, uint32(len(K)))
		vkBytes = append(vkBytes, n...)
		for i := range K {
			vkBytes = append(vkBytes, K[i].Marshal()...)
		}
		// proof: Ar(64) | Bs(128) | Krs(64)
		proofBytes = append(proofBytes, A.Marshal()...)
		proofBytes = append(proofBytes, g2.Marshal()...)
		proofBytes = append(proofBytes, C.Marshal()...)
		return vkBytes, proofBytes
	}

	// One public input, the chain binding — the smallest real instance.
	pubs1 := [][]byte{bind[:]}
	vk1, pf1 := buildInstance(pubs1, 11, 5)
	add("groth16.vk1", vk1)
	add("groth16.proof1", pf1)

	// A shielded transfer's public inputs: bind ‖ nullifiers ‖ commitments.
	nulls := [][]byte{[]byte("nullifier-one"), []byte("nullifier-two")}
	comms := [][]byte{[]byte("commitment-one")}
	pubsTx := [][]byte{bind[:]}
	pubsTx = append(pubsTx, nulls...)
	pubsTx = append(pubsTx, comms...)
	vkTx, pfTx := buildInstance(pubsTx, 99, 17)
	add("groth16.vk_tx", vkTx)
	add("groth16.proof_tx", pfTx)

	// The transaction those public inputs describe, marshalled, so the Rust
	// side verifies the same bytes rather than a re-typed copy.
	txG := &Transaction{
		Type:       TransactionTypeTransfer,
		Version:    1,
		Fee:        10,
		Expiry:     500,
		Nullifiers: nulls,
		Outputs: []*ShieldedOutput{
			{Commitment: comms[0], EncryptedNote: []byte("note"), EphemeralPubKey: []byte("epk"), OutputProof: []byte("rp")},
		},
		Proof: &ZKProof{ProofType: "groth16", ProofData: pfTx, PublicInputs: pubsTx},
	}
	add("groth16.tx_wire", txG.Marshal())
	gid := txG.ComputeID()
	add("groth16.tx_id", gid[:])

	// Same instance with the proof's A moved by one group element: still a
	// well-formed point, no longer a satisfying assignment.
	var aBad bn254.G1Affine
	_ = aBad.Unmarshal(pf1[:64])
	aBad.Add(&aBad, &g1)
	pfBad := append(append([]byte(nil), aBad.Marshal()...), pf1[64:]...)
	add("groth16.proof1_bad", pfBad)

	// ---- adversarial instances -------------------------------------------
	//
	// One good instance, and then that instance with ONE thing wrong in it.
	// The verdict beside each is not typed here: `judge` runs the reference's
	// own four steps over these exact bytes and records what it answered. A
	// port that reads the same bytes has to reach the same four answers, and
	// where it does not, the difference is a forgery one node accepts and
	// another refuses.
	//
	// The four steps are the ones the verifier runs, in its order: the length
	// bound in verifyGroth16Proof, then the key, then the proof, then the
	// pairing. A case that is refused earlier than another implementation
	// refuses it still agrees on the verdict, which is what is compared.
	witness1 := []fr.Element{}
	{
		var w fr.Element
		w.SetBytes(bind[:])
		witness1 = append(witness1, w)
	}
	judge := func(vkB, pfB []byte) byte {
		if len(pfB) < 256 {
			return 0
		}
		vk, err := deserializeVerifyingKey(vkB)
		if err != nil {
			return 0
		}
		if err := validateVerifyingKey(vk); err != nil {
			return 0
		}
		pf, err := deserializeGroth16Proof(pfB)
		if err != nil {
			return 0
		}
		if err := verifyGroth16Pairing(pf, vk, witness1); err != nil {
			return 0
		}
		return 1
	}
	adv := func(name string, vkB, pfB []byte) {
		add("adv."+name+".vk", vkB)
		add("adv."+name+".proof", pfB)
		add("adv."+name+".verdict", []byte{judge(vkB, pfB)})
	}
	// Zero a range of a copy: gnark writes infinity as all-zero bytes, which
	// is also what an unwritten key looks like, and a pairing DROPS any term
	// whose argument is infinity — so each of these removes one factor from
	// the equation rather than failing to parse.
	zeroed := func(src []byte, from, to int) []byte {
		out := append([]byte(nil), src...)
		for i := from; i < to; i++ {
			out[i] = 0
		}
		return out
	}

	adv("good", vk1, pf1)
	adv("a_moved", vk1, pfBad)
	adv("a_infinity", vk1, zeroed(pf1, 0, 64))
	adv("b_infinity", vk1, zeroed(pf1, 64, 192))
	adv("c_infinity", vk1, zeroed(pf1, 192, 256))
	adv("vk_alpha_infinity", zeroed(vk1, 0, 64), pf1)
	adv("vk_beta_infinity", zeroed(vk1, 64, 192), pf1)
	adv("vk_k0_infinity", zeroed(vk1, 452, 516), pf1)
	adv("vk_all_infinity", make([]byte, 452+2*64), pf1)

	// A coordinate that is not on the curve at all.
	offCurve := append([]byte(nil), pf1...)
	offCurve[31] ^= 1
	adv("a_off_curve", vk1, offCurve)

	// A coordinate that is not a field element: exactly the modulus. Both
	// implementations must refuse the ENCODING rather than reduce it, or one
	// byte string names two different points.
	nonCanonical := append([]byte(nil), pf1...)
	copy(nonCanonical[0:32], fp.Modulus().FillBytes(make([]byte, 32)))
	adv("a_non_canonical", vk1, nonCanonical)

	// On the twist, outside the prime-order subgroup. MapToCurve2 does not
	// clear the cofactor, so what it returns is a curve point that is not a
	// group element — the one case a curve test alone lets through, and the
	// reason G2 gets a subgroup test.
	var offSub bn254.G2Affine
	for i := int64(1); i < 64; i++ {
		var u bn254.E2
		u.A0.SetInt64(i)
		u.A1.SetInt64(i + 1)
		q := bn254.MapToCurve2(&u)
		if q.IsOnCurve() && !q.IsInSubGroup() {
			offSub = q
			break
		}
	}
	if offSub.IsInfinity() {
		t.Fatal("no off-subgroup G2 point found — the subgroup vector would prove nothing")
	}
	offSubProof := append([]byte(nil), pf1...)
	copy(offSubProof[64:192], offSub.Marshal())
	adv("b_off_subgroup", vk1, offSubProof)

	// The two G1 elements of the proof exchanged. Both are well-formed group
	// elements; the equation they satisfy is not this one.
	swapped := append([]byte(nil), pf1...)
	copy(swapped[0:64], pf1[192:256])
	copy(swapped[192:256], pf1[0:64])
	adv("swap_a_c", vk1, swapped)

	// One byte short of a proof.
	adv("proof_short", vk1, pf1[:255])

	// A key that says it carries three points and carries two: the count is
	// the peer's to write, and the bytes after it are not.
	shortK := append([]byte(nil), vk1...)
	binary.BigEndian.PutUint32(shortK[448:452], 3)
	adv("vk_k_count_overruns", shortK, pf1)

	// A key for a two-input circuit, given one input. The key describes the
	// circuit; the witness count is not the transaction's to choose.
	adv("vk_wants_two_inputs", vkTx, pf1)

	// ---- genesis ----------------------------------------------------------
	//
	// A genesis document is an OPERATOR's file, read by every implementation
	// of this chain, and what it produces is the genesis block id, the initial
	// state root and the first UTXOs. Two implementations that read the same
	// file differently do not run the same chain — so the document itself, and
	// everything the reference derives from it, are recorded here.
	//
	// The accepted documents are emitted by Go's own encoder: what is written
	// is exactly the shape the reference reads. For each one, `.doc` is the
	// file; `.verdict` is what ParseGenesis answered; and for a document it
	// accepted, `.stamp`, `.ntx`, and per transaction `.wire` (every decoded
	// field, through the reference's own serializer), `.id_carried` (the id
	// the FILE names) and `.utxo<i>` (the UTXO record genesis seeding builds,
	// which is keyed on that carried id, not on a recomputed one). `.root` is
	// the initial state root and `.block_id` the genesis block's identity.
	genesis := func(name string, doc []byte) {
		add("genesis."+name+".doc", doc)
		g, err := ParseGenesis(doc)
		if err != nil {
			add("genesis."+name+".verdict", []byte{0})
			return
		}
		add("genesis."+name+".verdict", []byte{1})
		stamp := make([]byte, 8)
		binary.BigEndian.PutUint64(stamp, uint64(g.Timestamp))
		add("genesis."+name+".stamp", stamp)
		n := make([]byte, 4)
		binary.BigEndian.PutUint32(n, uint32(len(g.InitialTxs)))
		add("genesis."+name+".ntx", n)
		for i, tx := range g.InitialTxs {
			p := fmt.Sprintf("genesis.%s.tx%d", name, i)
			add(p+".wire", tx.Marshal())
			add(p+".id_carried", tx.ID[:])
			id := tx.ComputeID()
			add(p+".id_computed", id[:])
			// processGenesisTransactions keys each genesis UTXO on the id the
			// FILE carries — not on a recomputed one. A port that recomputes
			// it seeds the shielded set under different keys.
			for j, o := range tx.Outputs {
				u := &UTXO{
					TxID:        tx.ID,
					OutputIndex: uint32(j),
					Commitment:  o.Commitment,
					Ciphertext:  o.EncryptedNote,
					EphemeralPK: o.EphemeralPubKey,
					Height:      0,
				}
				add(fmt.Sprintf("%s.utxo%d", p, j), u.Marshal())
			}
		}
		zeroRoot := &Root{committed: make([]byte, 32)}
		add("genesis."+name+".root", zeroRoot.After(g.InitialTxs))
		gb := &Block{BlockHeight: 0, BlockTimestamp: g.Timestamp, Txs: g.InitialTxs, vm: vm}
		gid := gb.computeID()
		add("genesis."+name+".block_id", gid[:])
	}

	// A transaction whose CARRIED id is not the one its content derives, so
	// the two cannot be confused for one another downstream.
	txCarried := &Transaction{
		ID:         ids.ID{0xFF, 0xEE},
		Type:       TransactionTypeTransfer,
		Version:    2,
		Fee:        3,
		Expiry:     77,
		Nullifiers: [][]byte{[]byte("gn1")},
		Outputs: []*ShieldedOutput{
			{Commitment: []byte("gc1"), EncryptedNote: []byte("ge1"), EphemeralPubKey: []byte("gp1"), OutputProof: []byte("gr1")},
			{Commitment: []byte("gc2"), EncryptedNote: nil, EphemeralPubKey: nil, OutputProof: nil},
		},
		Proof: &ZKProof{ProofType: "stark", ProofData: []byte("P3Q1x"), PublicInputs: [][]byte{[]byte("gi")}},
		Memo:  []byte("gm"),
	}
	full, err := json.Marshal(&Genesis{
		Timestamp:  1_700_000_000,
		InitialTxs: []*Transaction{txFull, txCarried},
		SetupParams: &SetupParams{
			PowersOfTau:     []byte("tau"),
			VerifyingKey:    []byte("vk"),
			PlonkSRS:        []byte("srs"),
			FHEPublicParams: []byte("fhe"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	genesis("full", full)

	oneTx, err := json.Marshal(&Genesis{InitialTxs: []*Transaction{txCarried}})
	if err != nil {
		t.Fatal(err)
	}
	genesis("one_tx", oneTx)

	// Documents an operator might plausibly write, and documents a careless
	// one might. What each is worth is the reference's answer, not mine.
	genesis("empty_bytes", nil)
	genesis("empty_doc", []byte(`{}`))
	genesis("null_doc", []byte(`null`))
	genesis("no_timestamp", []byte(`{"initialTransactions":[]}`))
	genesis("negative_stamp", []byte(`{"timestamp":-3}`))
	genesis("unknown_field", []byte(`{"nonsense":[1,2],"timestamp":5}`))
	// encoding/json matches a field name case-insensitively when no exact
	// match exists. A port keyed on the exact spelling reads 0 here.
	genesis("odd_case", []byte(`{"TimeStamp":7}`))
	genesis("null_txs", []byte(`{"initialTransactions":null,"timestamp":9}`))
	genesis("bad_json", []byte(`{`))
	genesis("stamp_is_string", []byte(`{"timestamp":"5"}`))
	genesis("stamp_is_fractional", []byte(`{"timestamp":1.5}`))
	genesis("stamp_is_exponent", []byte(`{"timestamp":1e3}`))
	// The wire form is NOT the genesis form: a hex string where the document
	// says a transaction is refused.
	genesis("tx_is_hex_string", []byte(`{"initialTransactions":["5a415000"]}`))
	genesis("tx_empty_object", []byte(`{"initialTransactions":[{}]}`))
	genesis("tx_bad_base64", []byte(`{"initialTransactions":[{"nullifiers":["!!"]}]}`))
	genesis("tx_bad_id", []byte(`{"initialTransactions":[{"id":"not-cb58"}]}`))
	genesis("tx_empty_id", []byte(`{"initialTransactions":[{"id":""}]}`))
	genesis("tx_null_id", []byte(`{"initialTransactions":[{"id":null,"fee":4}]}`))
	genesis("tx_type_out_of_range", []byte(`{"initialTransactions":[{"type":250}]}`))
	genesis("tx_type_too_big", []byte(`{"initialTransactions":[{"type":300}]}`))
	genesis("setup_params_wrong_type", []byte(`{"setupParams":5}`))
	genesis("doc_is_array", []byte(`[]`))
	genesis("trailing_bytes", []byte(`{} {}`))

	// ---- the verifier precompiles ------------------------------------------
	//
	// `Run` answers a byte AND an error, and the two are not the same fact: a
	// contract that reads 0x00 with no error was told "that proof does not
	// verify", while an error is "this call could not be judged". Which of the
	// two each input produces is recorded here, because a port that returns an
	// error where the reference returns a verdict makes a call revert on one
	// node and return false on another.
	pc := func(name string, c precompiles.PrecompiledContract, input []byte) {
		add("pc."+name+".input", input)
		outBytes, err := c.Run(input)
		add("pc."+name+".out", outBytes)
		errored := byte(0)
		if err != nil {
			errored = 1
		}
		add("pc."+name+".err", []byte{errored})
		g := make([]byte, 8)
		binary.BigEndian.PutUint64(g, c.RequiredGas(input))
		add("pc."+name+".gas", g)
	}
	u32 := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, v)
		return b
	}
	// Groth16 (0x80): vk_len ‖ vk ‖ proof(256) ‖ n ‖ inputs.
	g16 := func(vkB, pfB []byte, inputs [][]byte) []byte {
		in := append(u32(uint32(len(vkB))), vkB...)
		in = append(in, pfB...)
		in = append(in, u32(uint32(len(inputs)))...)
		for _, p := range inputs {
			var w [32]byte
			copy(w[32-len(p):], p)
			in = append(in, w[:]...)
		}
		return in
	}
	groth := &precompiles.Groth16Verifier{}
	pc("groth16_good", groth, g16(vk1, pf1, [][]byte{bind[:]}))
	pc("groth16_moved", groth, g16(vk1, pfBad, [][]byte{bind[:]}))
	pc("groth16_short", groth, []byte{0, 0, 0})
	pc("groth16_vk_overruns", groth, g16(shortK, pf1, [][]byte{bind[:]}))
	pc("groth16_input_count", groth, g16(vk1, pf1, [][]byte{bind[:], bind[:]}))

	// PLONK (0x81): vk_len ‖ vk ‖ proof_len ‖ proof ‖ n ‖ inputs. Every proof
	// here is refused — the equation is not implemented — but WHICH refusal
	// (verdict or error) is the thing being pinned.
	plonkIn := func(vkB, pfB []byte, n uint32, inputs []byte) []byte {
		in := append(u32(uint32(len(vkB))), vkB...)
		in = append(in, u32(uint32(len(pfB)))...)
		in = append(in, pfB...)
		in = append(in, u32(n)...)
		return append(in, inputs...)
	}
	// A structurally perfect PLONK proof: seven on-curve prime-order G1
	// commitments, three scalars, and a key whose first 128 bytes are a
	// well-formed G2.
	var plonkProof []byte
	for i := 1; i <= 7; i++ {
		var p bn254.G1Affine
		p.ScalarMultiplication(&g1, big.NewInt(int64(i)))
		plonkProof = append(plonkProof, p.Marshal()...)
	}
	plonkProof = append(plonkProof, make([]byte, 96)...)
	plonkVK := append(g2.Marshal(), []byte("circuit commitments")...)
	plonk := &precompiles.PLONKVerifier{}
	pc("plonk_well_formed", plonk, plonkIn(plonkVK, plonkProof, 1, bind[:]))
	pc("plonk_proof_short", plonk, plonkIn(plonkVK, plonkProof[:543], 0, nil))
	pc("plonk_commitment_off_curve", plonk, func() []byte {
		bad := append([]byte(nil), plonkProof...)
		bad[31] ^= 1
		return plonkIn(plonkVK, bad, 0, nil)
	}())
	pc("plonk_vk_short", plonk, plonkIn(plonkVK[:127], plonkProof, 0, nil))
	pc("plonk_srs_off_curve", plonk, func() []byte {
		bad := append([]byte(nil), plonkVK...)
		bad[63] ^= 1
		return plonkIn(bad, plonkProof, 0, nil)
	}())
	pc("plonk_truncated_frame", plonk, []byte{0, 0, 0})
	pc("plonk_vk_len_overruns", plonk, append(u32(9_999), plonkVK...))
	// numInputs * 32 is computed in uint32 and wraps: 2^27 inputs passes the
	// bound check and reaches a verifier that never reads them.
	pc("plonk_input_count_wraps", plonk, plonkIn(plonkVK, plonkProof, 1<<27, nil))

	// STARK (0x82) with no verifier bound: fails closed, and says so with an
	// error rather than a verdict, so an operator can tell a missing binding
	// from a bad proof.
	starkIn := func(pfB, pubB []byte) []byte {
		in := append(u32(uint32(len(pfB))), pfB...)
		in = append(in, u32(uint32(len(pubB)))...)
		return append(in, pubB...)
	}
	stark := &precompiles.STARKVerifier{}
	pc("stark_unbound", stark, starkIn([]byte("P3Q1payload"), bind[:]))
	pc("stark_no_magic", stark, starkIn([]byte("XXXXpayload"), bind[:]))
	pc("stark_truncated_frame", stark, []byte{0, 0, 0})
	pc("halo2_absent", &precompiles.Halo2Verifier{}, []byte("anything"))
	pc("nova_absent", &precompiles.NovaVerifier{}, []byte("anything"))

	out["vectors"] = vecs
	out["network_id"] = networkID
	out["chain_id"] = h(chainID[:])
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	dst := os.Getenv("ZGOLD_OUT")
	if dst == "" {
		t.Fatal("ZGOLD_OUT unset")
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d vectors to %s", len(vecs), dst)
}

func TestCheckGoldenInstance(t *testing.T) {
	raw, err := os.ReadFile(os.Getenv("ZGOLD_OUT"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Vectors []struct{ Name, Hex string } }
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	m := map[string][]byte{}
	for _, v := range doc.Vectors {
		b, _ := hex.DecodeString(v.Hex)
		m[v.Name] = b
	}

	vk, err := deserializeVerifyingKey(m["groth16.vk1"])
	if err != nil {
		t.Fatal("vk:", err)
	}
	if err := validateVerifyingKey(vk); err != nil {
		t.Fatal("vk validate:", err)
	}
	pf, err := deserializeGroth16Proof(m["groth16.proof1"])
	if err != nil {
		t.Fatal("proof:", err)
	}
	var w fr.Element
	w.SetBytes(m["bind"])
	if err := verifyGroth16Pairing(pf, vk, []fr.Element{w}); err != nil {
		t.Fatal("GOOD instance did not verify:", err)
	}
	t.Log("good instance verifies in Go")

	bad, err := deserializeGroth16Proof(m["groth16.proof1_bad"])
	if err != nil {
		t.Fatal("bad proof decode:", err)
	}
	if err := verifyGroth16Pairing(bad, vk, []fr.Element{w}); err == nil {
		t.Fatal("BAD instance verified — the construction proves nothing")
	} else {
		t.Log("bad instance refused in Go:", err)
	}

	// wrong witness count must be refused
	if err := verifyGroth16Pairing(pf, vk, []fr.Element{w, w}); err == nil {
		t.Fatal("witness-count mismatch accepted")
	}
	t.Log("witness-count mismatch refused in Go")
}
