// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

<<<<<<< HEAD
	"github.com/luxfi/node/codec"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/vms/components/verify"
)

var (
	_ UnsignedTx = (*MintTx)(nil)
=======
	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var (
	_ UnsignedTx             = (*MintTx)(nil)
	_ secp256k1fx.UnsignedTx = (*MintTx)(nil)
>>>>>>> main

	ErrInvalidSourceChain = errors.New("invalid source chain")
	ErrInvalidBurnProof   = errors.New("invalid burn proof")
	ErrInvalidMintAmount  = errors.New("invalid mint amount")
)

// MintTx mints assets on X-Chain from cross-chain transfers via Teleport Protocol
type MintTx struct {
	// Base transaction fields
	BaseTx `serialize:"true"`

	// Asset being minted
	AssetID ids.ID `serialize:"true" json:"assetID"`

	// Amount to mint
	Amount uint64 `serialize:"true" json:"amount"`

	// Source chain ID
	SourceChain ids.ID `serialize:"true" json:"sourceChain"`

	// Proof of burn on source chain
	BurnProof []byte `serialize:"true" json:"burnProof"`

	// MPC signatures authorizing the mint
	MPCSignatures [][]byte `serialize:"true" json:"mpcSignatures"`
}

<<<<<<< HEAD
func (t *MintTx) InitCtx(ctx *consensus.Context) {
	t.BaseTx.InitCtx(ctx)
}

func (t *MintTx) SyntacticVerify(
	ctx *consensus.Context,
	c codec.Manager,
	txFeeAssetID ids.ID,
	txFee uint64,
	_ uint64,
	_ int,
) error {
	switch {
	case t == nil:
		return ErrNilTx
	case t.AssetID == ids.Empty:
		return ErrInvalidAssetID
	case t.Amount == 0:
		return ErrInvalidMintAmount
	case t.SourceChain == ids.Empty:
		return ErrInvalidSourceChain
	case len(t.BurnProof) == 0:
		return ErrInvalidBurnProof
	case t.SourceChain == ctx.ChainID:
		return errors.New("cannot mint from same chain")
	case len(t.MPCSignatures) < 67: // Requires 67/100 threshold
		return errors.New("insufficient MPC signatures")
	}

	if err := t.BaseTx.SyntacticVerify(ctx, c, txFeeAssetID, txFee, txFee, len(t.Ins)); err != nil {
		return err
	}

	// Ensure outputs match the mint amount
	totalOut := uint64(0)
	for _, out := range t.Outs {
		if out.AssetID() == t.AssetID {
			totalOut += out.Output().Amount()
		}
	}

	// Must mint exact amount
	if totalOut != t.Amount {
		return errors.New("mint amount mismatch")
	}

	return nil
}

func (t *MintTx) SemanticVerify(vm VM, tx UnsignedTx, creds []verify.Verifiable) error {
	// Verify the transaction is well-formed
	if err := t.BaseTx.SemanticVerify(vm, tx, creds); err != nil {
		return err
	}

	// TODO: Verify burn proof from source chain
	// This would involve checking merkle proofs, block headers, etc.

	// TODO: Verify MPC signatures
	// This would involve checking threshold signatures from validators

=======
func (t *MintTx) InitCtx(ctx *quasar.Context) {
	t.BaseTx.InitCtx(ctx)
}

// Initialize implements quasar.ContextInitializable
func (t *MintTx) Initialize(ctx *quasar.Context) error {
	t.InitCtx(ctx)
>>>>>>> main
	return nil
}

func (t *MintTx) Visit(v Visitor) error {
	return v.MintTx(t)
<<<<<<< HEAD
}
=======
}
>>>>>>> main
