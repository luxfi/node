// SPDX-License-Identifier: BSD-3-Clause-Eco

// Emits the wire vectors `tests/wire_vectors.rs` checks against.
//
// This is the other side of the claim "same wire": it builds each transaction
// and block with the Go P-Chain's own constructors and prints the bytes, so
// the Rust test compares against what Go actually writes rather than against
// what this port believes Go writes.
//
// It is not built by cargo and not part of this crate's compilation. To run
// it, drop it into a package inside a checkout of the Go node — the reference
// this port was made from — and run it:
//
//	mkdir -p <go-node>/cmd/pvmvec && cp gen.go <go-node>/cmd/pvmvec/main.go
//	cd <go-node> && go run ./cmd/pvmvec
//
// Each line is a name and the hex of one value's bytes. Paste a line into
// tests/wire_vectors.rs when a vector is added or a layout deliberately
// changes; a layout that changes by accident shows up as a failing test.

package main

import (
	"encoding/hex"
	"fmt"
	"time"

	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/genesis"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	"github.com/luxfi/node/vms/platformvm/fx"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/security"
	"github.com/luxfi/node/vms/platformvm/signer"
	"github.com/luxfi/node/vms/platformvm/warp/message"
	"github.com/luxfi/node/vms/platformvm/txs"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
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

func emit(name string, b []byte, err error) {
	if err != nil {
		panic(fmt.Sprintf("%s: %v", name, err))
	}
	fmt.Printf("%s %s\n", name, hex.EncodeToString(b))
}

func main() {
	bt, err := txs.NewBaseTx(base())
	emit("base", bt.Bytes(), err)

	rv := txs.NewRewardValidatorTx(id(7))
	emit("reward", rv.Bytes(), nil)

	v := txs.Validator{NodeID: nodeID(5), Start: 1000, End: 2000, Wght: 50}
	stake := []*lux.TransferableOutput{out(9, 50, 2)}

	av, err := txs.NewAddValidatorTx(base(), v, stake, owners(3), 20000)
	emit("add_validator", av.Bytes(), err)

	ad, err := txs.NewAddDelegatorTx(base(), v, stake, owners(3))
	emit("add_delegator", ad.Bytes(), err)

	acv, err := txs.NewAddChainValidatorTx(base(), v, id(6), &secp256k1fx.Input{SigIndices: []uint32{1}})
	emit("add_chain_validator", acv.Bytes(), err)

	cc, err := txs.NewCreateChainTx(base(), id(6), "a chain", id(8), []ids.ID{id(1), id(2)}, []byte("genesis bytes"), &secp256k1fx.Input{SigIndices: []uint32{0, 2}})
	emit("create_chain", cc.Bytes(), err)

	it, err := txs.NewImportTx(base(), id(4), []*lux.TransferableInput{in(2, 1, 300)})
	emit("import", it.Bytes(), err)

	et, err := txs.NewExportTx(base(), id(4), []*lux.TransferableOutput{out(9, 40, 1)})
	emit("export", et.Bytes(), err)

	// blocks
	sb, err := block.NewStandardBlock(time.Unix(1000, 0), id(1), 5, []*txs.Tx{})
	emit("block_standard_empty", sb.Bytes(), err)

	cb, err := block.NewCommitBlock(time.Unix(1000, 0), id(1), 5)
	emit("block_commit", cb.Bytes(), err)

	ab, err := block.NewAbortBlock(time.Unix(1000, 0), id(1), 5)
	emit("block_abort", ab.Bytes(), err)

	rcv, err := txs.NewRemoveChainValidatorTx(base(), nodeID(5), id(6), &secp256k1fx.Input{SigIndices: []uint32{0}})
	emit("remove_chain_validator", rcv.Bytes(), err)

	tco, err := txs.NewTransferChainOwnershipTx(base(), id(6), &secp256k1fx.Input{SigIndices: []uint32{0}}, owners(7))
	emit("transfer_chain_ownership", tco.Bytes(), err)

	inc, err := txs.NewIncreaseL1ValidatorBalanceTx(base(), id(13), 999)
	emit("increase_l1_balance", inc.Bytes(), err)

	dis, err := txs.NewDisableL1ValidatorTx(base(), id(13), &secp256k1fx.Input{SigIndices: []uint32{0, 1}})
	emit("disable_l1_validator", dis.Bytes(), err)

	apd, err := txs.NewAddPermissionlessDelegatorTx(base(), v, id(6), stake, owners(3))
	emit("add_permissionless_delegator", apd.Bytes(), err)

	pop := &signer.ProofOfPossession{}
	for k := range pop.PublicKey {
		pop.PublicKey[k] = 11
	}
	for k := range pop.ProofOfPossession {
		pop.ProofOfPossession[k] = 12
	}
	apv, err := txs.NewAddPermissionlessValidatorTx(base(), v, ids.Empty, pop, stake, owners(3), owners(4), 20000)
	emit("add_permissionless_validator", apv.Bytes(), err)

	apve, err := txs.NewAddPermissionlessValidatorTx(base(), v, id(6), &signer.Empty{}, stake, owners(3), owners(4), 20000)
	emit("add_permissionless_validator_empty_signer", apve.Bytes(), err)

	signedBase, err := txs.NewBaseTx(base())
	if err != nil {
		panic(err)
	}
	stx := &txs.Tx{Unsigned: signedBase, Creds: []verify.Verifiable{
		&secp256k1fx.Credential{Sigs: [][65]byte{sig(1), sig(2)}},
		&secp256k1fx.Credential{Sigs: [][65]byte{}},
		&secp256k1fx.Credential{Sigs: [][65]byte{sig(3)}},
	}}
	if err := stx.Initialize(); err != nil {
		panic(err)
	}
	emit("signed_base_three_creds", stx.Bytes(), nil)

	pb, err := block.NewProposalBlock(time.Unix(1000, 0), id(1), 5, rv2(), nil)
	emit("block_proposal", pb.Bytes(), err)

	sbtx, err := block.NewStandardBlock(time.Unix(1000, 0), id(1), 5, []*txs.Tx{stx})
	emit("block_standard_one_tx", sbtx.Bytes(), err)

	tc, err := txs.NewTransformChainTx(
		base(),
		id(6),  // chain
		id(14), // asset
		1_000_000,
		2_000_000,
		100_000,
		120_000,
		1_000,
		500_000,
		3600,
		86400,
		20_000,
		500,
		5,
		800_000,
		&secp256k1fx.Input{SigIndices: []uint32{0, 3}},
	)
	emit("transform_chain", tc.Bytes(), err)

	var popBlob [96]byte
	for k := range popBlob {
		popBlob[k] = 15
	}
	rl, err := txs.NewRegisterL1ValidatorTx(base(), 777, popBlob, []byte("a warp message"))
	emit("register_l1_validator", rl.Bytes(), err)

	sw, err := txs.NewSetL1ValidatorWeightTx(base(), []byte("another warp message"))
	emit("set_l1_validator_weight", sw.Bytes(), err)

	// Two genesis validators, in node-id order, so the sorted-and-unique rule
	// and both address runs are exercised.
	nv := []*txs.NetworkValidator{
		networkValidator(5, 100, 900, 11, 12, []ids.ShortID{short(1)}, []ids.ShortID{short(2), short(3)}),
		networkValidator(6, 200, 800, 13, 14, []ids.ShortID{short(4)}, nil),
	}

	cn, err := txs.NewCreateNetworkTx(
		base(),
		ids.Empty, // parent: the primary network, so this makes an L1
		owners(7),
		security.Mode{RestakeParent: false, Admission: security.Open, Threshold: 1000, Manager: security.Contract},
		nv,
		id(8),
		[]byte("manager address"),
	)
	emit("create_network", cn.Bytes(), err)

	// The simplest network there is: it restakes its parent and runs no set of
	// its own, so it carries no validators.
	cn2, err := txs.NewCreateNetworkTx(
		base(),
		id(6),
		owners(7),
		security.Mode{RestakeParent: true, Admission: security.NoOwnSet},
		nil,
		ids.Empty,
		nil,
	)
	emit("create_network_restaking", cn2.Bytes(), err)

	cv, err := txs.NewConvertNetworkTx(
		base(),
		id(6), // the network being promoted
		ids.Empty,
		id(8),
		security.Mode{RestakeParent: false, Admission: security.Gated, Manager: security.Contract},
		[]byte("manager address"),
		nv,
		&secp256k1fx.Input{SigIndices: []uint32{1, 2}},
	)
	emit("convert_network", cv.Bytes(), err)

	plainUTXO, err := genesisUTXO(0, 100, 0).UTXO.WireBytes()
	emit("utxo_plain", plainUTXO, err)
	lockedUTXO, err := genesisUTXO(1, 250, 5000).UTXO.WireBytes()
	emit("utxo_locked", lockedUTXO, err)

	gvdr := &txs.Tx{Unsigned: apv}
	if err := gvdr.Initialize(); err != nil {
		panic(err)
	}
	gchain := &txs.Tx{Unsigned: cc}
	if err := gchain.Initialize(); err != nil {
		panic(err)
	}
	g := &genesis.Genesis{
		UTXOs:         []*genesis.UTXO{genesisUTXO(0, 100, 0), genesisUTXO(1, 250, 5000)},
		Validators:    []*txs.Tx{gvdr},
		Chains:        []*txs.Tx{gchain},
		Timestamp:     1000,
		InitialSupply: 360_000_000_000_000,
		Message:       "let there be light",
	}
	gb, err := g.Bytes()
	emit("genesis", gb, err)

	// The address a key spends under, and a signature over a known hash, so
	// the Rust side can prove it recovers the same address Go does.
	sk, err := secp256k1.ToPrivateKey(bytes32(0x11))
	if err != nil {
		panic(err)
	}
	addr := sk.PublicKey().Address()
	emit("spend_address", addr[:], nil)
	sighash := bytes32(0x07)
	sigBytes, err := sk.SignHash(sighash)
	emit("spend_signature", sigBytes, err)
}

func bytes32(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}

// genesisUTXO builds one allocation exactly as the P-Chain genesis holds it:
// an unspent output owned by one address, optionally locked until a time.
func genesisUTXO(index uint32, amount, lock uint64) *genesis.UTXO {
	var out lux.TransferableOut = &secp256k1fx.TransferOutput{
		Amt: amount,
		OutputOwners: secp256k1fx.OutputOwners{
			Locktime:  0,
			Threshold: 1,
			Addrs:     []ids.ShortID{short(1)},
		},
	}
	if lock != 0 {
		out = &stakeable.LockOut{Locktime: lock, TransferableOut: out}
	}
	return &genesis.UTXO{
		UTXO: lux.UTXO{
			UTXOID: lux.UTXOID{TxID: ids.Empty, OutputIndex: index},
			Asset:  lux.Asset{ID: id(9)},
			Out:    out,
		},
		Message: []byte("hello"),
	}
}

func networkValidator(node byte, weight, balance uint64, pk, pop byte, remaining, deactivation []ids.ShortID) *txs.NetworkValidator {
	v := &txs.NetworkValidator{
		NodeID:  nodeID(node).Bytes(),
		Weight:  weight,
		Balance: balance,
		RemainingBalanceOwner: message.PChainOwner{Threshold: 1, Addresses: remaining},
		DeactivationOwner:     message.PChainOwner{Threshold: uint32(len(deactivation)), Addresses: deactivation},
	}
	for k := range v.Signer.PublicKey {
		v.Signer.PublicKey[k] = pk
	}
	for k := range v.Signer.ProofOfPossession {
		v.Signer.ProofOfPossession[k] = pop
	}
	return v
}

func sig(b byte) [65]byte {
	var s [65]byte
	for k := range s {
		s[k] = b
	}
	return s
}

func rv2() *txs.Tx {
	t := &txs.Tx{Unsigned: txs.NewRewardValidatorTx(id(7))}
	if err := t.Initialize(); err != nil {
		panic(err)
	}
	return t
}
