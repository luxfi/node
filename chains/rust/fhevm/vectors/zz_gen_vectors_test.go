// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause-Eco

// Writes the golden vectors the Rust F-Chain is checked against. It is a member
// of the Go package `github.com/luxfi/chains/fhevm`,
// because most of what has to be pinned — the handle derivation, the effect key,
// the committee digest, the record JSON, the database layout — is package
// internal, and a generator that reached for it from outside would have to
// restate it, which is the one thing a differential vector must never do.
//
// It is a test file, so it is not part of the package's ordinary build, and it is
// never added to the reference checkout: `run.sh` beside it copies the reference
// package and this file into a scratch directory and runs it there, so what the
// vectors record is the reference package exactly as it stands.
//
//	./run.sh            # writes vectors.json next to this file
//
// The vectors are the Go chain's own answers: its signatures, its wire bytes, its
// ids, its records, and — the one that subsumes most of the others — the whole
// database a scripted chain leaves behind, which the Rust side must reproduce
// byte for byte by replaying the same blocks.
package fhevm

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/luxfi/chains/fee"
	"github.com/luxfi/chains/mpcvm/fhe"
	"github.com/luxfi/crypto/mldsa"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/runtime"
	vmcore "github.com/luxfi/vm"
)

// The chain every vector is bound to. Fixed, because a signature names one chain
// and a block id commits to one chain.
var vecChainID = ids.ID{'f', 'c', 'h', 'a', 'i', 'n', '-', 't', 'e', 's', 't'}

const vecGenesisTime = int64(1_700_000_000)
const vecScheme = "ckks-n14"
const vecGas = uint64(500_000)
const vecFund = uint64(10_000_000_000)

type vecKey struct {
	priv *mldsa.PrivateKey
	pub  []byte
	addr fee.Account
}

func newVecKey() vecKey {
	priv, err := mldsa.GenerateKey(rand.Reader, mldsa.MLDSA65)
	must(err)
	pub := priv.PublicKey.Bytes()
	return vecKey{priv: priv, pub: pub, addr: addressOf(pub)}
}

func (k vecKey) sign(tx *Transaction) *Transaction {
	tx.Auth = k.pub
	sig, err := k.priv.Sign(rand.Reader, tx.SigningBytes(vecChainID), crypto.Hash(0))
	must(err)
	tx.Sig = sig
	tx.id = ids.Empty
	return tx
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func hexOfBytes(b []byte) string { return hex.EncodeToString(b) }

// txVector is one transaction, with every value the Rust side derives from it.
type txVector struct {
	Name         string `json:"name"`
	Type         uint8  `json:"type"`
	Scheme       string `json:"scheme"`
	PayerHex     string `json:"payerHex"`
	SubjectHex   string `json:"subjectHex"`
	GasLimit     uint64 `json:"gasLimit"`
	Nonce        uint64 `json:"nonce"`
	PayloadHex   string `json:"payloadHex"`
	AuthHex      string `json:"authHex"`
	SigHex       string `json:"sigHex"`
	ContentHex   string `json:"contentHex"`
	SigningHex   string `json:"signingHex"`
	BytesHex     string `json:"bytesHex"`
	IDHex        string `json:"idHex"`
	EffectHex    string `json:"effectHex"`
	Gas          uint64 `json:"gas"`
	FeeNLux      uint64 `json:"feeNLux"`
	SyntacticErr string `json:"syntacticErr"`
}

func vectorOf(name string, tx *Transaction) txVector {
	g, gErr := GasFor(tx)
	f, _ := FeeFor(tx)
	sErr := ""
	if err := tx.SyntacticVerify(); err != nil {
		sErr = err.Error()
	}
	_ = gErr
	return txVector{
		Name:         name,
		Type:         tx.Type,
		Scheme:       tx.Scheme,
		PayerHex:     hexOfBytes(tx.Payer[:]),
		SubjectHex:   hexOfBytes(tx.Subject[:]),
		GasLimit:     tx.GasLimit,
		Nonce:        tx.Nonce,
		PayloadHex:   hexOfBytes(tx.Payload),
		AuthHex:      hexOfBytes(tx.Auth),
		SigHex:       hexOfBytes(tx.Sig),
		ContentHex:   hexOfBytes(tx.content()),
		SigningHex:   hexOfBytes(tx.SigningBytes(vecChainID)),
		BytesHex:     hexOfBytes(tx.Bytes()),
		IDHex:        tx.ID().String(),
		EffectHex:    hexOfBytes(effectOf(tx)),
		Gas:          uint64(g),
		FeeNLux:      f,
		SyntacticErr: sErr,
	}
}

func effectOf(tx *Transaction) []byte {
	e := tx.effect()
	return e[:]
}

// dumpDB serializes every key the VM has written, in a fixed order, exactly as
// the package's own replay test does — so what the Rust side is required to
// reproduce is the same listing the Go chain checks itself against.
func dumpDB(vm *VM) (string, string) {
	h := sha256.New()
	var b strings.Builder
	for _, prefix := range []string{
		CiphertextPrefix, PermitPrefix, DecryptPrefix, EpochPrefix, BlockPrefix,
		string(noncePrefix), string(heightPrefix), "fee/",
	} {
		it := vm.state.NewIteratorWithPrefix([]byte(prefix))
		for it.Next() {
			b.WriteString(hex.EncodeToString(it.Key()))
			b.WriteByte('=')
			b.WriteString(hex.EncodeToString(it.Value()))
			b.WriteByte('\n')
			h.Write(it.Key())
			h.Write(it.Value())
		}
		must(it.Error())
		it.Release()
	}
	return hex.EncodeToString(h.Sum(nil)), b.String()
}

func newVecVM(genesis []byte) *VM {
	logger := log.NewNoOpLogger()
	vm := &VM{}
	must(vm.Initialize(context.Background(), vmcore.Init{
		Runtime:  &runtime.Runtime{ChainID: vecChainID, NetworkID: 96369, Log: logger},
		DB:       memdb.New(),
		ToEngine: make(chan vmcore.Message, 8),
		Log:      logger,
		Genesis:  genesis,
	}))
	return vm
}

func acceptOneVec(vm *VM, tx *Transaction) *Block {
	_, err := vm.SubmitTx(tx)
	must(err)
	blkIntf, err := vm.BuildBlock(context.Background())
	must(err)
	blk := blkIntf.(*Block)
	must(blk.Verify(context.Background()))
	must(blk.Accept(context.Background()))
	return blk
}

// TestWriteVectors is the generator. It is a test only so that it can reach the
// package internals a differential vector must be taken from rather than
// restated.
func TestWriteVectors(t *testing.T) {
	out := map[string]any{}

	// ---- ML-DSA-65: a real key, a real signature over a real preimage -------
	signer := newVecKey()
	probe := []byte("fhevm/tx/ cross-language probe")
	probeSig, err := signer.priv.Sign(rand.Reader, probe, crypto.Hash(0))
	must(err)
	out["mldsa"] = map[string]any{
		"publicKeyHex":    hexOfBytes(signer.pub),
		"messageHex":      hexOfBytes(probe),
		"signatureHex":    hexOfBytes(probeSig),
		"addressHex":      hexOfBytes(signer.addr[:]),
		"publicKeySize":   mldsa.GetPublicKeySize(payerAuthMode),
		"signatureSize":   mldsa.GetSignatureSize(payerAuthMode),
		"addressCB58":     signer.addr.String(),
	}

	// ---- the derivations ----------------------------------------------------
	digest := sha256.Sum256([]byte("the-encrypted-body"))
	owner, grantee := newVecKey(), newVecKey()
	handle := deriveHandle(digest, vecScheme)
	permitID := derivePermitID(handle, owner.addr, grantee.addr, fhe.PermitOpDecrypt, 0, 2)
	requestID := deriveRequestID(handle, grantee.addr, 1)

	committee, cKeys := vecCommittee(3)
	cDigest := committeeDigest(1, 2, []byte("epoch-1-network-key"), committee)

	out["derivations"] = map[string]any{
		"schemeName":     vecScheme,
		"digestHex":      hexOfBytes(digest[:]),
		"handleHex":      hexOfBytes(handle[:]),
		"ownerHex":       hexOfBytes(owner.addr[:]),
		"granteeHex":     hexOfBytes(grantee.addr[:]),
		"permitOps":      fhe.PermitOpDecrypt,
		"permitNonce":    uint64(2),
		"permitIDHex":    hexOfBytes(permitID[:]),
		"requestNonce":   uint64(1),
		"requestIDHex":   hexOfBytes(requestID[:]),
		"committee":      committeeJSON(committee),
		"committeeEpoch": uint64(1),
		"committeeThr":   2,
		"committeePKHex": hexOfBytes([]byte("epoch-1-network-key")),
		"committeeDigestHex": hexOfBytes(cDigest[:]),
		"vmIDCB58":       VMID.String(),
		"chainIDCB58":    vecChainID.String(),
	}

	// ---- one transaction of every kind --------------------------------------
	var txs []txVector
	reg := owner.sign(&Transaction{
		Type: TxRegisterCiphertext, Scheme: vecScheme, Payer: owner.addr,
		Subject: handle, GasLimit: vecGas, Nonce: 1,
		Payload: mustJSONBytes(RegisterPayload{Digest: digest, Type: 4, Level: 3, Size: 4096}),
	})
	txs = append(txs, vectorOf("register", reg))

	grant := owner.sign(&Transaction{
		Type: TxGrantPermit, Payer: owner.addr, Subject: handle, GasLimit: vecGas, Nonce: 2,
		Payload: mustJSONBytes(GrantPayload{Grantee: grantee.addr, Operations: fhe.PermitOpDecrypt, Expiry: 0}),
	})
	txs = append(txs, vectorOf("grant", grant))

	revoke := owner.sign(&Transaction{
		Type: TxRevokePermit, Payer: owner.addr, Subject: permitID, GasLimit: vecGas, Nonce: 3,
		Payload: mustJSONBytes(RevokePayload{Reason: "no longer sanctioned"}),
	})
	txs = append(txs, vectorOf("revoke", revoke))

	request := grantee.sign(&Transaction{
		Type: TxRequestDecrypt, Scheme: vecScheme, Payer: grantee.addr, Subject: handle,
		GasLimit: vecGas, Nonce: 1,
		Payload: mustJSONBytes(RequestPayload{
			PermitID: permitID, Callback: [20]byte{0xca, 0x11}, Selector: [4]byte{1, 2, 3, 4},
		}),
	})
	txs = append(txs, vectorOf("request", request))

	fulfil := cKeys[0].sign(&Transaction{
		Type: TxFulfillDecrypt, Payer: cKeys[0].addr, Subject: requestID, GasLimit: vecGas, Nonce: 1,
		Payload: mustJSONBytes(FulfillPayload{Result: sha256.Sum256([]byte("decrypted-result-handle"))}),
	})
	txs = append(txs, vectorOf("fulfill", fulfil))

	advance := cKeys[0].sign(&Transaction{
		Type: TxAdvanceEpoch, Payer: cKeys[0].addr, Subject: cDigest, GasLimit: vecGas, Nonce: 2,
		Payload: mustJSONBytes(AdvancePayload{
			Epoch: 1, Committee: committee, Threshold: 2, PublicKey: []byte("epoch-1-network-key"),
		}),
	})
	txs = append(txs, vectorOf("advance", advance))

	// A transaction with every field non-empty and none of them a real key, so
	// the wire round-trip exercises every offset without a signature in the way.
	sample := &Transaction{
		Type: TxRegisterCiphertext, Scheme: vecScheme,
		Subject: [32]byte{9, 8, 7, 6, 5, 4, 3, 2, 1}, GasLimit: 81000, Nonce: 42,
		Payload: []byte("register-ciphertext-payload"),
		Auth:    []byte("payer-public-key-bytes"),
		Sig:     []byte("payer-signature-bytes"),
	}
	copy(sample.Payer[:], []byte("payer-address-20byte"))
	txs = append(txs, vectorOf("sample", sample))
	out["transactions"] = txs

	// ---- a block ------------------------------------------------------------
	second := *sample
	second.Nonce = 43
	second.Payload = []byte("second")
	second.id = ids.Empty
	blockVM := &VM{chainID: vecChainID}
	blk := &Block{
		parentID:     ids.ID{1, 2, 3},
		height:       7,
		timestamp:    timeAt(1_700_000_000),
		transactions: []*Transaction{sample, &second},
		vm:           blockVM,
	}
	blk.id = blk.computeID()
	out["block"] = map[string]any{
		"parentIDHex": hexOfBytes(blk.parentID[:]),
		"height":      blk.height,
		"timestamp":   blk.timestamp.Unix(),
		"bytesHex":    hexOfBytes(blk.Bytes()),
		"idHex":       hexOfBytes(blk.id[:]),
		"emptyBlockSize": emptyBlockSize,
		"txEntry":     txEntry,
	}

	// ---- the four records, as they are stored -------------------------------
	out["records"] = recordVectors(handle, digest, permitID, requestID, owner.addr, grantee.addr, committee)

	// ---- the runtime parameters F reports -----------------------------------
	cfg := fhe.DefaultThresholdConfig()
	out["params"] = map[string]any{
		"logN":     cfg.CKKSParams.LogN(),
		"logQP":    int(cfg.CKKSParams.LogQ() + cfg.CKKSParams.LogP()),
		"logScale": cfg.CKKSParams.LogDefaultScale(),
	}

	// ---- the gas schedule ---------------------------------------------------
	out["gas"] = gasVectors()

	// ---- and the whole chain: blocks in, database out -----------------------
	out["replay"] = replayVectors()

	enc, err := json.MarshalIndent(out, "", "  ")
	must(err)
	must(os.WriteFile("vectors.json", enc, 0o644))
	fmt.Println("wrote vectors.json")
}

func mustJSONBytes(v any) []byte {
	b, err := json.Marshal(v)
	must(err)
	return b
}

func vecCommittee(n int) ([]fhe.CommitteeMember, []vecKey) {
	type pair struct {
		m fhe.CommitteeMember
		k vecKey
	}
	pairs := make([]pair, n)
	for i := range pairs {
		k := newVecKey()
		pairs[i] = pair{m: fhe.CommitteeMember{NodeID: ids.GenerateTestNodeID(), PublicKey: k.pub, Weight: 1}, k: k}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].m.NodeID.Compare(pairs[j].m.NodeID) < 0 })
	members := make([]fhe.CommitteeMember, n)
	keys := make([]vecKey, n)
	for i, p := range pairs {
		p.m.Index = i
		members[i] = p.m
		keys[i] = p.k
	}
	return members, keys
}

func committeeJSON(c []fhe.CommitteeMember) []map[string]any {
	out := make([]map[string]any, len(c))
	for i, m := range c {
		out[i] = map[string]any{
			"nodeIDHex":    hexOfBytes(m.NodeID[:]),
			"publicKeyHex": hexOfBytes(m.PublicKey),
			"weight":       m.Weight,
			"index":        m.Index,
		}
	}
	return out
}

func recordVectors(handle, digest, permitID, requestID [32]byte, owner, grantee fee.Account, committee []fhe.CommitteeMember) map[string]string {
	ct := &CiphertextRecord{
		CiphertextMeta: fhe.CiphertextMeta{
			Handle: handle, Owner: owner, Type: 4, Level: 3, Epoch: 7,
			RegisteredAt: 1_700_000_000, Size: 4096, ChainID: vecChainID,
		},
		Scheme: vecScheme, Digest: digest,
	}
	pm := &PermitRecord{
		Permit: fhe.Permit{
			PermitID: permitID, Handle: handle, Grantee: grantee, Grantor: owner,
			Operations: fhe.PermitOpDecrypt, Expiry: 99, CreatedAt: 10, ChainID: vecChainID,
		},
		Status: StatusActive,
	}
	dr := &DecryptRecord{
		DecryptRequest: fhe.DecryptRequest{
			RequestID: requestID, CiphertextHandle: handle, Requester: grantee,
			Callback: [20]byte{0xca, 0x11}, CallbackSelector: [4]byte{1, 2, 3, 4},
			SourceChain: vecChainID, Epoch: 1, Nonce: 2, Expiry: 3,
			Status: fhe.RequestCompleted, CreatedAt: 4, CompletedAt: 5,
			ResultHandle: sha256.Sum256([]byte("result")),
		},
		PermitID:     permitID,
		Attestations: []Attestation{{Member: owner, Value: sha256.Sum256([]byte("v"))}},
	}
	ep := &EpochRecord{EpochInfo: fhe.EpochInfo{
		Epoch: 1, StartTime: 2, EndTime: 3, Committee: committee, Threshold: 2,
		PublicKey: []byte("network-fhe-public-key"), Status: fhe.EpochEnded,
	}}
	empty := &EpochRecord{EpochInfo: fhe.EpochInfo{Epoch: 9, Status: fhe.EpochActive}}
	return map[string]string{
		"ciphertext": string(mustJSONBytes(ct)),
		"permit":     string(mustJSONBytes(pm)),
		"decrypt":    string(mustJSONBytes(dr)),
		"epoch":      string(mustJSONBytes(ep)),
		"epochEmpty": string(mustJSONBytes(empty)),
	}
}

func gasVectors() []map[string]any {
	var out []map[string]any
	ops := []uint8{TxRegisterCiphertext, TxGrantPermit, TxRevokePermit, TxRequestDecrypt, TxFulfillDecrypt, TxAdvanceEpoch}
	schemes := []string{"", "tfhe-n10", "tfhe-n11", "bfv-n13", "bfv-n14", "bgv-n13", "bgv-n14", "ckks-n13", "ckks-n14", "ckks-n15", "paillier"}
	for _, op := range ops {
		for _, s := range schemes {
			tx := &Transaction{Type: op, Scheme: s, Payload: []byte(`{"reason":"x"}`)}
			g, err := GasFor(tx)
			entry := map[string]any{"type": op, "scheme": s, "payloadHex": hexOfBytes(tx.Payload)}
			if err != nil {
				entry["err"] = err.Error()
			} else {
				f, _ := FeeFor(tx)
				entry["gas"] = uint64(g)
				entry["feeNLux"] = f
			}
			out = append(out, entry)
		}
	}
	return out
}

// replayVectors runs the whole lifecycle on a Go node and hands back the genesis,
// the blocks as they went out on the wire, and the database they left behind. The
// Rust side replays exactly these bytes and must produce exactly this database.
func replayVectors() map[string]any {
	owner, grantee := newVecKey(), newVecKey()
	committee, members := vecCommittee(3)

	alloc := map[string]uint64{}
	for _, k := range members {
		alloc[hex.EncodeToString(k.addr[:])] = vecFund
	}
	alloc[hex.EncodeToString(owner.addr[:])] = vecFund
	alloc[hex.EncodeToString(grantee.addr[:])] = vecFund

	genesis := mustJSONBytes(Genesis{
		Version: 1, Timestamp: vecGenesisTime, Alloc: alloc,
		Committee: committee, Threshold: 2, PublicKey: []byte("network-fhe-public-key"),
	})

	vm := newVecVM(genesis)
	vm.clock.Set(timeAt(1_700_000_100))

	digest := sha256.Sum256([]byte("replay-body"))
	handle := deriveHandle(digest, vecScheme)
	acceptOneVec(vm, owner.sign(&Transaction{
		Type: TxRegisterCiphertext, Scheme: vecScheme, Payer: owner.addr, Subject: handle,
		GasLimit: vecGas, Nonce: 1,
		Payload:  mustJSONBytes(RegisterPayload{Digest: digest, Type: 4, Level: 3, Size: 4096}),
	}))
	acceptOneVec(vm, owner.sign(&Transaction{
		Type: TxGrantPermit, Payer: owner.addr, Subject: handle, GasLimit: vecGas, Nonce: 2,
		Payload: mustJSONBytes(GrantPayload{Grantee: grantee.addr, Operations: fhe.PermitOpDecrypt}),
	}))
	permitID := derivePermitID(handle, owner.addr, grantee.addr, fhe.PermitOpDecrypt, 0, 2)
	acceptOneVec(vm, grantee.sign(&Transaction{
		Type: TxRequestDecrypt, Scheme: vecScheme, Payer: grantee.addr, Subject: handle,
		GasLimit: vecGas, Nonce: 1,
		Payload: mustJSONBytes(RequestPayload{
			PermitID: permitID, Callback: [20]byte{0xca, 0x11}, Selector: [4]byte{1, 2, 3, 4},
		}),
	}))
	requestID := deriveRequestID(handle, grantee.addr, 1)
	result := sha256.Sum256([]byte("agreed-result"))
	for i := 0; i < 2; i++ {
		acceptOneVec(vm, members[i].sign(&Transaction{
			Type: TxFulfillDecrypt, Payer: members[i].addr, Subject: requestID,
			GasLimit: vecGas, Nonce: 1,
			Payload:  mustJSONBytes(FulfillPayload{Result: result}),
		}))
	}
	next, _ := vecCommittee(3)
	nextPK := []byte("epoch-1-key")
	nextDigest := committeeDigest(1, 2, nextPK, next)
	for i := 0; i < 2; i++ {
		acceptOneVec(vm, members[i].sign(&Transaction{
			Type: TxAdvanceEpoch, Payer: members[i].addr, Subject: nextDigest,
			GasLimit: vecGas, Nonce: 2,
			Payload: mustJSONBytes(AdvancePayload{
				Epoch: 1, Committee: next, Threshold: 2, PublicKey: nextPK,
			}),
		}))
	}
	acceptOneVec(vm, owner.sign(&Transaction{
		Type: TxRevokePermit, Payer: owner.addr, Subject: permitID, GasLimit: vecGas, Nonce: 3,
		Payload: mustJSONBytes(RevokePayload{Reason: "no longer sanctioned"}),
	}))

	var wire []string
	for h := uint64(1); h <= vm.height; h++ {
		id, err := vm.GetBlockIDAtHeight(context.Background(), h)
		must(err)
		blk, err := vm.GetBlock(context.Background(), id)
		must(err)
		wire = append(wire, hexOfBytes(blk.Bytes()))
	}
	sum, listing := dumpDB(vm)

	return map[string]any{
		"genesisHex":   hexOfBytes(genesis),
		"clockUnix":    int64(1_700_000_100),
		"blocksHex":    wire,
		"height":       vm.height,
		"epoch":        vm.CurrentEpoch(),
		"handleHex":    hexOfBytes(handle[:]),
		"permitIDHex":  hexOfBytes(permitID[:]),
		"requestIDHex": hexOfBytes(requestID[:]),
		"resultHex":    hexOfBytes(result[:]),
		"dumpSumHex":   sum,
		"dumpListing":  listing,
	}
}
