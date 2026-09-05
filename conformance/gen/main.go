// SPDX-License-Identifier: BSD-3-Clause-Eco
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/luxfi/crypto/bls"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/txs"
	pwarp "github.com/luxfi/node/vms/platformvm/warp"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/zap"
	"github.com/luxfi/chains/zkvm"
	xblock "github.com/luxfi/node/vms/xvm/block"
	"github.com/luxfi/node/vms/xvm/fxs"
	xtxs "github.com/luxfi/node/vms/xvm/txs"
)

type Expectation struct {
	Valid          bool   `json:"valid"`
	TxType         string `json:"tx_type,omitempty"`
	Status         string `json:"status,omitempty"`
	Reason         string `json:"reason,omitempty"`
	TxID           string `json:"tx_id,omitempty"`
	BlockID        string `json:"block_id,omitempty"`
	ValidationID   string `json:"validation_id,omitempty"`
	Weight         uint64 `json:"weight,omitempty"`
	Balance        uint64 `json:"balance,omitempty"`
	WarpVerified   bool   `json:"warp_verified,omitempty"`
	RequiresReject bool   `json:"requires_reject,omitempty"`
}

type Vector struct {
	ID             string      `json:"id"`
	Chain          string      `json:"chain"` // "P", "X", "Q", "Z"
	Name           string      `json:"name"`
	Description    string      `json:"description"`
	Category       string      `json:"category"`
	WireHex        string      `json:"wire_hex"`
	ExpectedAction string      `json:"expected_action"` // "verify", "execute", "block_accept", "block_reject"
	IsL1ForkGate   bool        `json:"is_l1_fork_gate"`
	GoExpectation  Expectation `json:"go_expectation"`
}

type Corpus struct {
	GeneratedAt time.Time `json:"generated_at"`
	Generator   string    `json:"generator"`
	Count       int       `json:"count"`
	Vectors     []Vector  `json:"vectors"`
}

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
		Asset:  lux.Asset{ID: id(9)},
		In:     &secp256k1fx.TransferInput{Amt: amt, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
	}
}

func base() *lux.BaseTx {
	return &lux.BaseTx{
		NetworkID:    1,
		BlockchainID: id(3),
		Outs:         []*lux.TransferableOutput{out(9, 100, 1)},
		Ins:          []*lux.TransferableInput{in(1, 0, 200)},
	}
}

func owners(b byte) fx.Owner {
	return &secp256k1fx.OutputOwners{Locktime: 0, Threshold: 1, Addrs: []ids.ShortID{short(b)}}
}

func sig(b byte) [65]byte {
	var s [65]byte
	for k := range s {
		s[k] = b
	}
	return s
}

func sha256Hash(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

func wrapPvmTx(u txs.UnsignedTx) *txs.Tx {
	t := &txs.Tx{Unsigned: u}
	if err := t.Initialize(); err != nil {
		panic(err)
	}
	return t
}

func networkValidator(node byte, weight, balance uint64, pk, pop byte, remaining, deactivation []ids.ShortID) *txs.NetworkValidator {
	v := &txs.NetworkValidator{
		NodeID:  nodeID(node).Bytes(),
		Weight:  weight,
		Balance: balance,
		RemainingBalanceOwner: message.PChainOwner{Threshold: 1, Addresses: remaining},
		DeactivationOwner:     message.PChainOwner{Threshold: uint32(len(deactivation)), Addresses: deactivation},
	}
	sk, err := bls.NewSecretKey()
	if err != nil {
		panic(err)
	}
	popSig, err := signer.NewProofOfPossession(sk)
	if err != nil {
		panic(err)
	}
	v.Signer = *popSig
	return v
}

func writeU32List(b *zap.Builder, xs []uint32) int {
	lb := b.StartList(4)
	for _, x := range xs {
		lb.AddUint32(x)
	}
	off, _ := lb.Finish()
	return off
}

func main() {
	var vectors []Vector

	// =========================================================================
	// P-CHAIN VECTORS
	// =========================================================================

	// 1. P_BASE_TX
	bt, err := txs.NewBaseTx(base())
	if err != nil {
		panic(err)
	}
	pbt := wrapPvmTx(bt)
	vectors = append(vectors, Vector{
		ID:             "P_BASE_TX",
		Chain:          "P",
		Name:           "PlatformVM BaseTx",
		Description:    "Basic value transfer and fee payment on P-Chain",
		Category:       "tx_wire",
		WireHex:        hex.EncodeToString(pbt.Bytes()),
		ExpectedAction: "verify",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:  true,
			TxType: "BaseTx",
			TxID:   pbt.ID().String(),
			Status: "ACCEPTED",
		},
	})

	// 2. P_ADD_VALIDATOR_TX
	v := txs.Validator{NodeID: nodeID(5), Start: 1000, End: 2000, Wght: 50}
	stake := []*lux.TransferableOutput{out(9, 50, 2)}
	av, err := txs.NewAddValidatorTx(base(), v, stake, owners(3), 20000)
	if err != nil {
		panic(err)
	}
	pav := wrapPvmTx(av)
	vectors = append(vectors, Vector{
		ID:             "P_ADD_VALIDATOR_TX",
		Chain:          "P",
		Name:           "PlatformVM AddValidatorTx",
		Description:    "Validator staking registration on primary network",
		Category:       "staking",
		WireHex:        hex.EncodeToString(pav.Bytes()),
		ExpectedAction: "verify",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:  true,
			TxType: "AddValidatorTx",
			TxID:   pav.ID().String(),
			Weight: 50,
			Status: "ACCEPTED",
		},
	})

	// 3. P_CREATE_NETWORK_TX (Sovereign L1 with Open admission)
	nv := []*txs.NetworkValidator{
		networkValidator(5, 100, 900, 11, 12, []ids.ShortID{short(1)}, []ids.ShortID{short(2), short(3)}),
	}
	cn, err := txs.NewCreateNetworkTx(
		base(),
		ids.Empty, // primary network parent -> L1 sovereign network
		owners(7),
		security.Mode{RestakeParent: false, Admission: security.Open, Threshold: 1000, Manager: security.Contract},
		nv,
		id(8),
		[]byte("manager address"),
	)
	if err != nil {
		panic(err)
	}
	pcn := wrapPvmTx(cn)
	vectors = append(vectors, Vector{
		ID:             "P_CREATE_NETWORK_TX",
		Chain:          "P",
		Name:           "PlatformVM CreateNetworkTx (Sovereign L1)",
		Description:    "Establishes a sovereign L1 network with independent validator set",
		Category:       "l1_network",
		WireHex:        hex.EncodeToString(pcn.Bytes()),
		ExpectedAction: "execute",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:  true,
			TxType: "CreateNetworkTx",
			TxID:   pcn.ID().String(),
			Status: "ACCEPTED",
		},
	})

	// 4. P_REGISTER_L1_VALIDATOR_TX (L1 FORK GATE)
	var popBlob [96]byte
	for k := range popBlob {
		popBlob[k] = 15
	}
	warpPayload := []byte("lux:warp:register_l1_validator:0x1234")
	rl, err := txs.NewRegisterL1ValidatorTx(base(), 777, popBlob, warpPayload)
	if err != nil {
		panic(err)
	}
	prl := wrapPvmTx(rl)
	vectors = append(vectors, Vector{
		ID:             "P_REGISTER_L1_VALIDATOR_TX",
		Chain:          "P",
		Name:           "PlatformVM RegisterL1ValidatorTx",
		Description:    "Continuous fee registration for an L1 validator backed by Warp message",
		Category:       "l1_validator_plane",
		WireHex:        hex.EncodeToString(prl.Bytes()),
		ExpectedAction: "execute",
		IsL1ForkGate:   true, // DIVERGENT CELL: Rust initially refuses with L1ValidatorPlaneNotHeld
		GoExpectation: Expectation{
			Valid:        true,
			TxType:       "RegisterL1ValidatorTx",
			TxID:         prl.ID().String(),
			Weight:       777,
			Status:       "ACCEPTED",
			WarpVerified: true,
		},
	})

	// 5. P_SET_L1_VALIDATOR_WEIGHT_TX (L1 FORK GATE)
	sw, err := txs.NewSetL1ValidatorWeightTx(base(), []byte("lux:warp:set_l1_validator_weight:0x5678"))
	if err != nil {
		panic(err)
	}
	psw := wrapPvmTx(sw)
	vectors = append(vectors, Vector{
		ID:             "P_SET_L1_VALIDATOR_WEIGHT_TX",
		Chain:          "P",
		Name:           "PlatformVM SetL1ValidatorWeightTx",
		Description:    "Updates L1 validator weight upon cross-chain Warp message consensus",
		Category:       "l1_validator_plane",
		WireHex:        hex.EncodeToString(psw.Bytes()),
		ExpectedAction: "execute",
		IsL1ForkGate:   true, // DIVERGENT CELL
		GoExpectation: Expectation{
			Valid:        true,
			TxType:       "SetL1ValidatorWeightTx",
			TxID:         psw.ID().String(),
			Status:       "ACCEPTED",
			WarpVerified: true,
		},
	})

	// 6. P_INCREASE_L1_VALIDATOR_BALANCE_TX (L1 FORK GATE)
	inc, err := txs.NewIncreaseL1ValidatorBalanceTx(base(), id(13), 999999)
	if err != nil {
		panic(err)
	}
	pinc := wrapPvmTx(inc)
	vectors = append(vectors, Vector{
		ID:             "P_INCREASE_L1_VALIDATOR_BALANCE_TX",
		Chain:          "P",
		Name:           "PlatformVM IncreaseL1ValidatorBalanceTx",
		Description:    "Extends continuous fee runway for an active L1 validator",
		Category:       "l1_validator_plane",
		WireHex:        hex.EncodeToString(pinc.Bytes()),
		ExpectedAction: "execute",
		IsL1ForkGate:   true, // DIVERGENT CELL
		GoExpectation: Expectation{
			Valid:   true,
			TxType:  "IncreaseL1ValidatorBalanceTx",
			TxID:    pinc.ID().String(),
			Balance: 999999,
			Status:  "ACCEPTED",
		},
	})

	// 7. P_DISABLE_L1_VALIDATOR_TX (L1 FORK GATE)
	dis, err := txs.NewDisableL1ValidatorTx(base(), id(13), &secp256k1fx.Input{SigIndices: []uint32{0, 1}})
	if err != nil {
		panic(err)
	}
	pdis := wrapPvmTx(dis)
	vectors = append(vectors, Vector{
		ID:             "P_DISABLE_L1_VALIDATOR_TX",
		Chain:          "P",
		Name:           "PlatformVM DisableL1ValidatorTx",
		Description:    "Disables an L1 validator, removing active weight while preserving unspent state",
		Category:       "l1_validator_plane",
		WireHex:        hex.EncodeToString(pdis.Bytes()),
		ExpectedAction: "execute",
		IsL1ForkGate:   true, // DIVERGENT CELL
		GoExpectation: Expectation{
			Valid:  true,
			TxType: "DisableL1ValidatorTx",
			TxID:   pdis.ID().String(),
			Status: "ACCEPTED",
		},
	})

	// 8. P_WARP_MESSAGE_VERIFY
	unsignedWarp, err := pwarp.NewUnsignedMessage(1, id(20), []byte("cross-chain-transfer:lux-to-zoo"))
	if err != nil {
		panic(err)
	}
	warpBytes := unsignedWarp.Bytes()
	vectors = append(vectors, Vector{
		ID:             "P_WARP_MESSAGE_VERIFY",
		Chain:          "P",
		Name:           "PlatformVM Warp Message Wire",
		Description:    "Cross-chain aggregate communication message wire encoding",
		Category:       "warp",
		WireHex:        hex.EncodeToString(warpBytes),
		ExpectedAction: "verify",
		IsL1ForkGate:   true, // DIVERGENT CELL: C++ must verify security gaps and signatures
		GoExpectation: Expectation{
			Valid:        true,
			TxType:       "WarpUnsignedMessage",
			Status:       "ACCEPTED",
			WarpVerified: true,
		},
	})

	// 9. P_STANDARD_BLOCK
	sb, err := block.NewStandardBlock(time.Unix(1000, 0), id(1), 5, []*txs.Tx{})
	if err != nil {
		panic(err)
	}
	vectors = append(vectors, Vector{
		ID:             "P_STANDARD_BLOCK",
		Chain:          "P",
		Name:           "PlatformVM StandardBlock",
		Description:    "PlatformVM height-advancing standard block container",
		Category:       "block_wire",
		WireHex:        hex.EncodeToString(sb.Bytes()),
		ExpectedAction: "block_accept",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:   true,
			BlockID: sb.ID().String(),
			Status:  "ACCEPTED",
		},
	})

	// =========================================================================
	// X-CHAIN VECTORS
	// =========================================================================

	xOwners := secp256k1fx.OutputOwners{Locktime: 0, Threshold: 1, Addrs: []ids.ShortID{short(1)}}
	xTransferOut := &secp256k1fx.TransferOutput{Amt: 1000, OutputOwners: xOwners}
	xTransferIn := &secp256k1fx.TransferInput{Amt: 1000, Input: secp256k1fx.Input{SigIndices: []uint32{0}}}
	xBaseFields := lux.BaseTx{
		NetworkID:    1,
		BlockchainID: id(2),
		Outs: []*lux.TransferableOutput{{
			Asset: lux.Asset{ID: id(50)},
			Out:   xTransferOut,
		}},
		Ins: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{TxID: id(100), OutputIndex: 3},
			Asset:  lux.Asset{ID: id(50)},
			In:     xTransferIn,
		}},
		Memo: []byte("xvm memo bytes"),
	}
	xBaseTx := &xtxs.BaseTx{BaseTx: xBaseFields}
	xcred := &secp256k1fx.Credential{Sigs: [][65]byte{sig(1)}}
	xstx := &xtxs.Tx{Unsigned: xBaseTx, Creds: []*fxs.FxCredential{{Credential: xcred}}}
	if err := xstx.Initialize(); err != nil {
		panic(err)
	}

	// 10. X_BASE_TX
	vectors = append(vectors, Vector{
		ID:             "X_BASE_TX",
		Chain:          "X",
		Name:           "X-Chain Base Transaction",
		Description:    "UTXO asset transfer on X-Chain",
		Category:       "tx_wire",
		WireHex:        hex.EncodeToString(xstx.Bytes()),
		ExpectedAction: "verify",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:  true,
			TxType: "BaseTx",
			TxID:   xstx.ID().String(),
			Status: "ACCEPTED",
		},
	})

	// 11. X_CREATE_ASSET_TX
	mintOut := &secp256k1fx.MintOutput{OutputOwners: xOwners}
	caTx := &xtxs.CreateAssetTx{
		BaseTx:       xtxs.BaseTx{BaseTx: xBaseFields},
		Name:         "Lux Asset",
		Symbol:       "LUXA",
		Denomination: 9,
		States: []*xtxs.InitialState{
			{
				FxIndex: 0,
				Outs:    []verify.State{mintOut, xTransferOut},
			},
		},
	}
	caTx.States[0].Sort()
	xcaStx := &xtxs.Tx{Unsigned: caTx, Creds: []*fxs.FxCredential{{Credential: xcred}}}
	if err := xcaStx.Initialize(); err != nil {
		panic(err)
	}
	vectors = append(vectors, Vector{
		ID:             "X_CREATE_ASSET_TX",
		Chain:          "X",
		Name:           "X-Chain CreateAssetTx",
		Description:    "Minting and state initialization for a new asset on X-Chain",
		Category:       "tx_wire",
		WireHex:        hex.EncodeToString(xcaStx.Bytes()),
		ExpectedAction: "execute",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:  true,
			TxType: "CreateAssetTx",
			TxID:   xcaStx.ID().String(),
			Status: "ACCEPTED",
		},
	})

	// 12. X_BLOCK_REJECT (XVM BLOCK::REJECT GATE)
	xblk, err := xblock.NewStandardBlock(id(10), 1, time.Unix(1000, 0), []*xtxs.Tx{xstx})
	if err != nil {
		panic(err)
	}
	vectors = append(vectors, Vector{
		ID:             "X_BLOCK_REJECT",
		Chain:          "X",
		Name:           "X-Chain Block Rejection",
		Description:    "Tests Block::Reject behaviour returning valid transactions to mempool",
		Category:       "block_rejection",
		WireHex:        hex.EncodeToString(xblk.Bytes()),
		ExpectedAction: "block_reject",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:          true,
			BlockID:        xblk.ID().String(),
			Status:         "REJECTED",
			RequiresReject: true,
		},
	})

	// =========================================================================
	// Q-CHAIN VECTORS (Native ZAP encoding)
	// =========================================================================

	// 13. Q_BASE_TX
	qTxData := []byte("quantum-instruction-payload")
	qBld := zap.NewBuilder(zap.HeaderSize + 24 + len(qTxData) + 32)
	qOb := qBld.StartObject(24)
	qOb.SetInt64(0, 1000)
	qOb.SetUint64(8, 100)
	qOb.SetBytes(16, qTxData)
	qOb.FinishAsRoot()
	qBaseBytes := qBld.Finish()
	qTxID := ids.ID(sha256.Sum256(qBaseBytes))

	vectors = append(vectors, Vector{
		ID:             "Q_BASE_TX",
		Chain:          "Q",
		Name:           "QuantumVM Base Transaction",
		Description:    "Post-quantum signed instruction payload on Q-Chain",
		Category:       "tx_wire",
		WireHex:        hex.EncodeToString(qBaseBytes),
		ExpectedAction: "verify",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:  true,
			TxType: "QuantumBaseTx",
			TxID:   qTxID.String(),
			Status: "ACCEPTED",
		},
	})

	// 14. Q_BLOCK
	qBlkBld := zap.NewBuilder(zap.HeaderSize + 104 + len(qBaseBytes) + 64)
	qTxOff := writeU32List(qBlkBld, []uint32{uint32(len(qBaseBytes))})
	qBlkOb := qBlkBld.StartObject(104)
	qBlkOb.SetInt64(0, 1000)
	qBlkOb.SetUint64(8, 1)
	parentID := id(1)
	chainID := id(30)
	qBlkOb.SetBytesFixed(16, parentID[:])
	qBlkOb.SetBytesFixed(48, chainID[:])
	qBlkOb.SetUint32(80, 1)
	qBlkOb.SetList(88, qTxOff, 1)
	qBlkOb.SetBytes(96, qBaseBytes)
	qBlkOb.FinishAsRoot()
	qBlkBytes := qBlkBld.Finish()
	qBlkID := ids.ID(sha256.Sum256(qBlkBytes))

	vectors = append(vectors, Vector{
		ID:             "Q_BLOCK",
		Chain:          "Q",
		Name:           "QuantumVM Canonical Block",
		Description:    "ZAP-encoded block container for post-quantum transactions",
		Category:       "block_wire",
		WireHex:        hex.EncodeToString(qBlkBytes),
		ExpectedAction: "block_accept",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:   true,
			BlockID: qBlkID.String(),
			Status:  "ACCEPTED",
		},
	})

	// =========================================================================
	// Z-CHAIN VECTORS (Native ZAP encoding)
	// =========================================================================

	// 15. Z_SHIELDED_TX
	zTx := &zkvm.Transaction{
		Type:    zkvm.TransactionTypeShield,
		Version: 1,
		Fee:     500,
		Expiry:  100000,
		Nullifiers: [][]byte{
			sha256Hash([]byte("nullifier-1")),
		},
		Outputs: []*zkvm.ShieldedOutput{
			{
				Commitment:      sha256Hash([]byte("commitment-1")),
				EphemeralPubKey: []byte("ephemeral-key-32-bytes-test-xyz!"),
				EncryptedNote:   []byte("encrypted-secret-note-payload"),
			},
		},
	}
	zTxBytes := zTx.Marshal()
	vectors = append(vectors, Vector{
		ID:             "Z_SHIELDED_TX",
		Chain:          "Z",
		Name:           "ZKVM Shielded Transaction",
		Description:    "Zero-knowledge confidential UTXO state transition",
		Category:       "tx_wire",
		WireHex:        hex.EncodeToString(zTxBytes),
		ExpectedAction: "verify",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:  true,
			TxType: "ZKShieldedTx",
			TxID:   zTx.ComputeID().String(),
			Status: "ACCEPTED",
		},
	})

	// 16. Z_BLOCK
	zblk := &zkvm.Block{
		BlockHeight:    1,
		ParentID_:      id(1),
		BlockTimestamp: 1000,
		Txs:            []*zkvm.Transaction{zTx},
	}
	zblkBytes := zblk.Marshal()
	zblk.ID_ = ids.ID(sha256.Sum256(zblkBytes))
	vectors = append(vectors, Vector{
		ID:             "Z_BLOCK",
		Chain:          "Z",
		Name:           "ZKVM Canonical Block",
		Description:    "ZAP-encoded block carrying zero-knowledge shielded transactions",
		Category:       "block_wire",
		WireHex:        hex.EncodeToString(zblkBytes),
		ExpectedAction: "block_accept",
		IsL1ForkGate:   false,
		GoExpectation: Expectation{
			Valid:   true,
			BlockID: zblk.ID().String(),
			Status:  "ACCEPTED",
		},
	})

	corpus := Corpus{
		GeneratedAt: time.Now().UTC(),
		Generator:   "github.com/luxfi/node2/conformance/gen",
		Count:       len(vectors),
		Vectors:     vectors,
	}

	outPath := filepath.Join("..", "corpus", "chain_differential.json")
	if len(os.Args) > 1 {
		outPath = os.Args[1]
	}
	_ = os.MkdirAll(filepath.Dir(outPath), 0755)

	data, err := json.MarshalIndent(corpus, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		panic(err)
	}

	fmt.Printf("Successfully emitted %d differential vectors to %s\n", len(vectors), outPath)
}
