// SPDX-License-Identifier: BSD-3-Clause-Eco

// The Q-chain half of the corpus, and the Go reference's answers for it.
//
// Q-Chain's wire is native ZAP at fixed field offsets, and its exported surface
// is a VM: `ParseBlock` is the one door into it. That shapes both halves of
// this file.
//
// BUILDING. The vectors are written here at the offsets `chains/quantumvm`
// documents, because the encoder itself is unexported. That would normally be
// a second opinion about the wire — except that the Q parser is
// canonical-or-nothing: it re-encodes what it decoded and refuses anything that
// does not come back byte-identical. So a vector the reference ACCEPTS is
// proof that these bytes are exactly what the Go encoder would have written.
// The generator checks that at emit time (see qVectors), and a wire that drifts
// stops the emit rather than becoming a new normal.
//
// ANSWERING. `evalQ` runs the real VM against a SEEDED chain — the Q-chain
// writes its own genesis at Initialize, and genesis is a pure function of the
// chain and the network, so both languages hold the identical block without
// either being handed it. Every vector below that names that genesis as its
// parent therefore gets PAST the parent lookup, and the exec field carries a
// verdict the chain computed rather than a constant standing in for one.
//
// Verify is ONE pass, so the two layers are read out of where its refusal came
// from rather than out of a second entry point this chain does not have. What
// the two layers MEAN is the corpus's question, not this file's: it is defined
// once in conformance/README.md under "`syntactic` where verify is one pass",
// and qBlockAlone below is this reference's answer to it. The Rust evaluator
// answers the same question by calling `on_chain` and then `well_formed`, which
// is where its own verify stops before looking for a parent.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/chains/quantumvm"
	"github.com/luxfi/chains/quantumvm/config"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/runtime"
	luxvm "github.com/luxfi/vm"
	"github.com/luxfi/zap"
)

// The chain every Q vector belongs to. Its genesis id is a function of exactly
// these two numbers, which is why the genesis vector below pins them.
const (
	qChain   = 30
	qPayload = "quantum-instruction-payload"
)

// The block object, at the offsets the Q wire fixes.
const (
	qBlkTime    = 0
	qBlkHeight  = 8
	qBlkParent  = 16
	qBlkChain   = 48
	qBlkNetwork = 80
	qBlkTxLens  = 88
	qBlkTxBlob  = 96
	qBlkSize    = 104
)

// The transaction preimage: what the ML-DSA signature covers, and what the
// transaction's id is the hash of.
const (
	qTxTime  = 0
	qTxNonce = 8
	qTxData  = 16
	qTxSize  = 24
)

// The envelope: the preimage plus the signature over it, which is what rides
// inside a block.
const (
	qEnvBody  = 0
	qEnvAlg   = 8
	qEnvTime  = 16
	qEnvKey   = 24
	qEnvSig   = 32
	qEnvStamp = 40
	qEnvSize  = 48
)

func qBody(timestamp int64, nonce uint64, data []byte) []byte {
	b := zap.NewBuilder(zap.HeaderSize + qTxSize + len(data) + 32)
	ob := b.StartObject(qTxSize)
	ob.SetInt64(qTxTime, timestamp)
	ob.SetUint64(qTxNonce, nonce)
	ob.SetBytes(qTxData, data)
	ob.FinishAsRoot()
	return b.Finish()
}

// qEnvelope wraps a preimage with no signature over it, under the ML-DSA
// parameter set named by `alg`. A corpus cannot carry a signed one: an ML-DSA
// stamp is good only inside its window, so a recorded signature would decide
// the vector by the calendar rather than by the rules. What the corpus CAN pin
// is that a block whose stamps do not check out is refused, and refused at the
// signature layer rather than somewhere earlier.
//
// The parameter set is worth a vector of its own. Algorithm 0 is UNSET, and a
// chain that settles an unset field on its default rather than refusing it
// would verify under a weaker parameter set than the one it advertises.
func qEnvelope(body []byte, alg uint32) []byte {
	b := zap.NewBuilder(zap.HeaderSize + qEnvSize + len(body) + 64)
	ob := b.StartObject(qEnvSize)
	ob.SetBytes(qEnvBody, body)
	ob.SetUint32(qEnvAlg, alg)
	// The zero time, as the reference writes it for a transaction that carries
	// no signature.
	ob.SetInt64(qEnvTime, time.Time{}.UnixNano())
	ob.SetBytes(qEnvKey, nil)
	ob.SetBytes(qEnvSig, nil)
	ob.SetBytes(qEnvStamp, nil)
	ob.FinishAsRoot()
	return b.Finish()
}

func qBlock(timestamp int64, height uint64, parent, chain ids.ID, network uint32, txs [][]byte) []byte {
	var blob []byte
	lens := make([]uint32, len(txs))
	for i, tx := range txs {
		lens[i] = uint32(len(tx))
		blob = append(blob, tx...)
	}
	return qBlockRaw(timestamp, height, parent, chain, network, lens, blob)
}

// qBlockRaw writes a block whose declared transaction lengths need not match
// the blob they claim to partition. Every well-formed vector goes through
// qBlock; this door exists for the two that deliberately lie about their
// contents.
func qBlockRaw(timestamp int64, height uint64, parent, chain ids.ID, network uint32, lens []uint32, blob []byte) []byte {
	b := zap.NewBuilder(zap.HeaderSize + qBlkSize + len(blob) + 4*len(lens) + 128)
	lb := b.StartList(4)
	for _, l := range lens {
		lb.AddUint32(l)
	}
	off, _ := lb.Finish()

	ob := b.StartObject(qBlkSize)
	ob.SetInt64(qBlkTime, timestamp)
	ob.SetUint64(qBlkHeight, height)
	ob.SetBytesFixed(qBlkParent, parent[:])
	ob.SetBytesFixed(qBlkChain, chain[:])
	ob.SetUint32(qBlkNetwork, network)
	ob.SetList(qBlkTxLens, off, len(lens))
	ob.SetBytes(qBlkTxBlob, blob)
	ob.FinishAsRoot()
	return b.Finish()
}

// qGenesisWire is the height-0 block of this chain: a constant of the chain and
// the network, with no wall-clock time in it, so every node computes ONE id for
// it and holds it without being handed it. That is what lets the vectors below
// name a parent every implementation already has.
func qGenesisWire() []byte {
	return qBlock(0, 0, ids.Empty, id(qChain), networkID, nil)
}

func qGenesisID() ids.ID { return ids.ID(sha256.Sum256(qGenesisWire())) }

// qVectors is every Q vector. They are all blocks: `ParseBlock` is the whole of
// the exported surface, so a transaction is compared as the block that carries
// it — which is also the only place a transaction wire ever appears.
func qVectors() []Vector {
	genesis := qGenesisWire()
	parent := qGenesisID()
	body := qBody(genesisTime, 100, []byte(qPayload))
	// The parameter set this chain runs: ML-DSA-65, the one config.DefaultConfig
	// names. A transaction under it reaches the signature check; one under the
	// unset 0 is refused before it.
	env := qEnvelope(body, 2)
	unset := qEnvelope(body, 0)
	one := [][]byte{env}

	// A block that sits correctly on genesis in every way the chain can decide
	// without a signature: the right chain, the right network, height 1, a time
	// after its parent's and well short of the skew allowance.
	canonical := qBlock(genesisTime, 1, parent, id(qChain), networkID, one)

	// The same block naming a parent no chain holds. Its refusal has to come
	// from the parent lookup, not from anything earlier — which is only
	// visible next to the vector above, whose parent IS held.
	orphan := qBlock(genesisTime, 1, id(1), id(qChain), networkID, one)

	// One byte past the content, with the declared size grown to cover it: the
	// same logical block under a second id. The declared size lives at bytes
	// 12..15 of the ZAP header.
	padded := append(append([]byte{}, canonical...), 0)
	size := uint32(len(padded))
	padded[12], padded[13] = byte(size), byte(size>>8)
	padded[14], padded[15] = byte(size>>16), byte(size>>24)

	// The same byte appended WITHOUT growing the declared size: the container
	// now ends before the buffer does.
	trailing := append(append([]byte{}, canonical...), 0xFF, 0xFF, 0xFF, 0xFF)

	// A flipped byte inside the transaction blob. The container is still
	// canonical, so this is not a malformed block — it is a DIFFERENT block,
	// and the point of the vector is that every implementation says so by
	// deriving a different id for it. A chain that hashed anything less than
	// the whole wire would hand back the id above.
	corrupt := append([]byte{}, canonical...)
	corrupt[len(corrupt)-1] ^= 0xFF

	// A block whose transaction blob holds the signature PREIMAGE where the
	// envelope belongs. The container decodes; the transaction inside it does
	// not. This is the shape a hand-written generator produces when it forgets
	// that a block carries envelopes.
	bodyInBlob := qBlock(genesisTime, 1, parent, id(qChain), networkID, [][]byte{body})

	// Lengths that do not partition the blob: one entry claiming more than the
	// blob holds.
	shortBlob := qBlockRaw(genesisTime, 1, parent, id(qChain), networkID,
		[]uint32{uint32(len(env)) + 16}, env)

	// A transaction count no blob that size could hold. Bounding the count by
	// the blob before allocating for it is what keeps a peer from deciding how
	// much memory this node takes.
	absurd := make([]uint32, 4096)
	absurdCount := qBlockRaw(genesisTime, 1, parent, id(qChain), networkID, absurd, env)

	v := []Vector{
		vec("Q_GENESIS", "Q", "block", genesis),
		vec("Q_BLOCK", "Q", "block", canonical),
		vec("Q_BLOCK_ORPHAN", "Q", "block", orphan),
		vec("Q_BLOCK_EMPTY", "Q", "block",
			qBlock(genesisTime, 1, parent, id(qChain), networkID, nil)),
		vec("Q_BLOCK_DUPLICATE_TX", "Q", "block",
			qBlock(genesisTime, 1, parent, id(qChain), networkID, [][]byte{env, env})),
		vec("Q_BLOCK_ALGORITHM_UNSET", "Q", "block",
			qBlock(genesisTime, 1, parent, id(qChain), networkID, [][]byte{unset})),
		vec("Q_BLOCK_FOREIGN_CHAIN", "Q", "block",
			qBlock(genesisTime, 1, parent, id(qChain+1), networkID, one)),
		vec("Q_BLOCK_FOREIGN_NETWORK", "Q", "block",
			qBlock(genesisTime, 1, parent, id(qChain), networkID+1, one)),
		vec("Q_BLOCK_HEIGHT_SKIP", "Q", "block",
			qBlock(genesisTime, 5, parent, id(qChain), networkID, one)),
		vec("Q_BLOCK_TIME_BEFORE_PARENT", "Q", "block",
			qBlock(-1, 1, parent, id(qChain), networkID, one)),
		// Far enough ahead that no clock this runs on is inside the skew
		// allowance, and far enough from any real date that it stays that way.
		vec("Q_BLOCK_TIME_AHEAD", "Q", "block",
			qBlock(1<<40, 1, parent, id(qChain), networkID, one)),
		vec("Q_BLOCK_PADDED", "Q", "block", padded),
		vec("Q_BLOCK_TRAILING_BYTES", "Q", "block", trailing),
		vec("Q_BLOCK_CORRUPT_TAIL", "Q", "block", corrupt),
		vec("Q_BLOCK_BODY_IN_BLOB", "Q", "block", bodyInBlob),
		vec("Q_BLOCK_TXLEN_MISMATCH", "Q", "block", shortBlob),
		vec("Q_BLOCK_TXCOUNT_ABSURD", "Q", "block", absurdCount),
		vec("Q_BLOCK_TRUNCATED", "Q", "block", canonical[:len(canonical)/2]),
		vec("Q_BLOCK_EMPTY_WIRE", "Q", "block", nil),
		vec("Q_BLOCK_ONE_BYTE", "Q", "block", []byte{0x5a}),
	}

	qAssert(v)
	return v
}

// qAssert holds the emit to the two claims this half of the corpus rests on.
// Both are checked against the reference, and a claim that stops being true
// stops the emit rather than becoming a new normal.
func qAssert(v []Vector) {
	by := map[string]Result{}
	for _, vec := range v {
		by[vec.ID] = evalQ(vec)
	}

	// One: the vectors meant to BE this chain's wire are. The reference says
	// so, because its parser re-encodes what it decoded and refuses anything
	// that does not come back byte-identical.
	for _, want := range []string{
		"Q_GENESIS", "Q_BLOCK", "Q_BLOCK_ORPHAN", "Q_BLOCK_EMPTY",
		"Q_BLOCK_DUPLICATE_TX", "Q_BLOCK_ALGORITHM_UNSET",
		"Q_BLOCK_FOREIGN_CHAIN", "Q_BLOCK_FOREIGN_NETWORK",
		"Q_BLOCK_HEIGHT_SKIP", "Q_BLOCK_TIME_BEFORE_PARENT", "Q_BLOCK_TIME_AHEAD",
		"Q_BLOCK_CORRUPT_TAIL",
	} {
		if by[want].Parse != "ok" {
			panic(fmt.Sprintf("%s is not the wire the Q-chain writes: %s", want, by[want].Note))
		}
	}

	// Two: the chain is SEEDED. Q_BLOCK names this chain's genesis as its
	// parent and must get past the parent lookup; Q_BLOCK_ORPHAN names a block
	// nobody holds and must not. Without both halves the exec field would be
	// "parent not found" for every vector, and it would say nothing.
	const missing = "parent block not found"
	if strings.Contains(by["Q_BLOCK"].Note, missing) {
		panic("Q_BLOCK does not sit on the chain's own genesis: " + by["Q_BLOCK"].Note)
	}
	if !strings.Contains(by["Q_BLOCK_ORPHAN"].Note, missing) {
		panic("Q_BLOCK_ORPHAN was expected to name a parent no chain holds: " + by["Q_BLOCK_ORPHAN"].Note)
	}
}

// qvm is a Q-chain VM over an empty database. Initialize seeds this chain's
// genesis, so it is a chain with a tip rather than an empty store — which is
// what lets a vector's parent be found and the exec field mean something.
//
// One per process: starting it opens a committee, which is work, and every
// vector asks it the same question.
//
// Starting it also PRINTS: the consensus committee announces each validator it
// registers, on stdout. Stdout is the result stream the runner reads, so it is
// pointed at stderr for the duration — the words still reach whoever is
// watching, and they do not land in the middle of a result line.
var (
	qvmOnce  sync.Once
	qvmChain *quantumvm.VM
	qvmErr   error
)

func qvm() (*quantumvm.VM, error) {
	qvmOnce.Do(func() { qvmChain, qvmErr = startQvm() })
	return qvmChain, qvmErr
}

func startQvm() (*quantumvm.VM, error) {
	stdout := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = stdout }()

	vm := &quantumvm.VM{Config: config.DefaultConfig()}
	err := vm.Initialize(context.Background(), luxvm.Init{
		Runtime: &runtime.Runtime{
			NodeID:    nodeID(1),
			NetworkID: networkID,
			ChainID:   id(qChain),
			Log:       log.Noop(),
		},
		DB:  memdb.New(),
		Log: log.Noop(),
	})
	if err != nil {
		return nil, err
	}
	return vm, nil
}

// qBlockAlone says whether Verify reached its refusal BEFORE it read the chain.
//
// The question is the corpus's and is defined once, in conformance/README.md
// under "`syntactic` where verify is one pass". This is only where the Go
// REFERENCE puts that boundary, and it is a walk of Block.Verify in the order
// it runs.
//
// The first line on this chain that asks the store anything is Verify's
// vm.blockAt(parentID). The four refusals it can reach before that are named
// below. Everything else — the height it must follow, the time against the
// parent, the clock, the quantum stamps — is past it, and on this chain the
// clock is past it, which is the whole reason the boundary is a place in the
// code rather than a kind of rule.
//
// Named by the reference's own sentinels, quoted because `luxfi/chains/quantumvm`
// keeps every one of them unexported. Matched as substrings, not whole, because
// this reference wraps each one in the detail that made it fire.
func qBlockAlone(msg string, height uint64) bool {
	switch {
	case strings.Contains(msg, "belongs to another chain"):
		return true
	case strings.Contains(msg, "carries no transactions"):
		return true
	case strings.Contains(msg, "exceeds the wire bound"):
		return true
	case strings.Contains(msg, "invalid block height"):
		// One sentinel, two checks: height 0 is genesis and needs nothing but
		// the block, while a height that does not follow its parent needed the
		// parent to notice.
		return height == 0
	default:
		return false
	}
}

func evalQ(v Vector) Result {
	r := Result{ID: v.ID, Kind: none, Hash: none, Syntactic: none, Exec: none}
	b, ok := wireOf(v)
	if !ok {
		r.Parse = VInternal
		r.Note = "corpus wire is not hex"
		return r
	}
	vm, err := qvm()
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
	r.Kind = "QuantumBlock"
	blockID := blk.ID()
	r.Hash = hex.EncodeToString(blockID[:])

	// One call, both layers. Verify runs the chain binding and the block's own
	// well-formedness before it looks for a parent, so where its refusal came
	// from is what says which layer answered.
	err = blk.Verify(context.Background())
	if err == nil {
		r.Syntactic = VOK
		r.Exec = VOK
		r.Note = "verified against the seeded chain"
		return r
	}
	class := classify(err)
	if qBlockAlone(err.Error(), blk.Height()) {
		r.Syntactic = class
		r.Exec = class
	} else {
		r.Syntactic = VOK
		r.Exec = class
	}
	r.Note = trim(err.Error())
	return r
}
