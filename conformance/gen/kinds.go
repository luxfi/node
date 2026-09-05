// SPDX-License-Identifier: BSD-3-Clause-Eco

// Naming what came off the wire.
//
// The three implementations have to agree about WHICH transaction a buffer is
// before they can agree about anything else, so the kind is a compared field.
// The names below are the wire's names — the same nineteen the kind byte 2..20
// selects in Go, Rust and C++ — with no language's spelling in them.
package main

import (
	"github.com/luxfi/node/vms/platformvm/block"
	"github.com/luxfi/node/vms/platformvm/txs"
	xblock "github.com/luxfi/node/vms/xvm/block"
	"github.com/luxfi/node/vms/xvm/fxs"
	xtxs "github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/node/vms/xvm/txs/executor"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
	xwire "github.com/luxfi/utxo/wire"
)

func pKindName(u txs.UnsignedTx) string {
	switch u.(type) {
	case *txs.RewardValidatorTx:
		return "RewardValidator"
	case *txs.BaseTx:
		return "Base"
	case *txs.ImportTx:
		return "Import"
	case *txs.ExportTx:
		return "Export"
	case *txs.CreateNetworkTx:
		return "CreateNetwork"
	case *txs.CreateChainTx:
		return "CreateChain"
	case *txs.TransferChainOwnershipTx:
		return "TransferChainOwnership"
	case *txs.RemoveChainValidatorTx:
		return "RemoveChainValidator"
	case *txs.TransformChainTx:
		return "TransformChain"
	case *txs.AddValidatorTx:
		return "AddValidator"
	case *txs.AddChainValidatorTx:
		return "AddChainValidator"
	case *txs.AddDelegatorTx:
		return "AddDelegator"
	case *txs.AddPermissionlessValidatorTx:
		return "AddPermissionlessValidator"
	case *txs.AddPermissionlessDelegatorTx:
		return "AddPermissionlessDelegator"
	case *txs.RegisterL1ValidatorTx:
		return "RegisterL1Validator"
	case *txs.SetL1ValidatorWeightTx:
		return "SetL1ValidatorWeight"
	case *txs.IncreaseL1ValidatorBalanceTx:
		return "IncreaseL1ValidatorBalance"
	case *txs.DisableL1ValidatorTx:
		return "DisableL1Validator"
	case *txs.ConvertNetworkTx:
		return "ConvertNetwork"
	default:
		return "unknown"
	}
}

func pBlockKindName(b block.Block) string {
	switch b.(type) {
	case *block.StandardBlock:
		return "StandardBlock"
	case *block.ProposalBlock:
		return "ProposalBlock"
	case *block.CommitBlock:
		return "CommitBlock"
	case *block.AbortBlock:
		return "AbortBlock"
	default:
		return "unknown"
	}
}

func xKindName(u xtxs.UnsignedTx) string {
	switch u.(type) {
	case *xtxs.BaseTx:
		return "Base"
	case *xtxs.CreateAssetTx:
		return "CreateAsset"
	case *xtxs.OperationTx:
		return "Operation"
	case *xtxs.ImportTx:
		return "Import"
	case *xtxs.ExportTx:
		return "Export"
	default:
		return "unknown"
	}
}

// xFxIndex registers the secp256k1 family at position 0, which is where the
// corpus's CreateAssetTx says its initial state lives.
func xFxIndex() *xtxs.FxIndex {
	fi := xtxs.NewFxIndex()
	fi.Set(xwire.TypeKindSecp256k1, 0)
	return fi
}

// xParser is the X-chain's own block parser, over the fxs the X-chain runs.
// Parsing a block is fx-dependent, so this is the node's parser rather than a
// second reading of the same bytes.
var xParser = func() xblock.Parser {
	p, err := xblock.NewParser([]fxs.Fx{&secp256k1fx.Fx{}, &nftfx.Fx{}, &propertyfx.Fx{}})
	if err != nil {
		panic(err)
	}
	return p
}()

// xSyntactic is the X-chain's own syntactic verifier, given the one chain
// identity the corpus is built for. It is the node's type, not a copy of it.
//
// The backend is `xBackend()`, the same one the semantic pass and the executor
// read, so a vector cannot be judged under one wiring and executed under
// another.
func xSyntacticVerifier(tx *xtxs.Tx) *executor.SyntacticVerifier {
	return &executor.SyntacticVerifier{Backend: xBackend(), Tx: tx}
}
