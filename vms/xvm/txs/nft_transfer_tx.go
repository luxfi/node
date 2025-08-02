// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/luxfi/node/v2/codec"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/vms/components/verify"
	"github.com/luxfi/node/v2/vms/nftfx"
	"github.com/luxfi/node/v2/quasar"
)

var (
	_ UnsignedTx = (*NFTTransferTx)(nil)

	ErrInvalidNFTID        = errors.New("invalid NFT ID")
	ErrInvalidRecipient    = errors.New("invalid recipient")
	ErrUnsupportedChain    = errors.New("unsupported destination chain for NFT")
)

// NFTTransferTx transfers NFTs from X-Chain to C-Chain or other subnets
// This enables NFTs to move from UTXO model (X-Chain) to account model (C-Chain)
type NFTTransferTx struct {
	// Base transaction fields
	BaseTx `serialize:"true"`

	// NFT being transferred
	NFTTransferOp nftfx.TransferOperation `serialize:"true" json:"nftTransferOp"`

	// Destination chain (usually C-Chain)
	DestChain ids.ID `serialize:"true" json:"destChain"`

	// Recipient address on destination chain
	Recipient []byte `serialize:"true" json:"recipient"`

	// Optional metadata for the transfer
	Metadata []byte `serialize:"true" json:"metadata"`
}

func (t *NFTTransferTx) InitCtx(ctx *quasar.Context) {
	t.BaseTx.InitCtx(ctx)
}

func (t *NFTTransferTx) SyntacticVerify(
	ctx *quasar.Context,
	c codec.Manager,
	txFeeAssetID ids.ID,
	txFee uint64,
	_ uint64,
	_ int,
) error {
	switch {
	case t == nil:
		return ErrNilTx
	case t.DestChain == ids.Empty:
		return ErrInvalidDestChain
	case len(t.Recipient) == 0:
		return ErrInvalidRecipient
	case t.DestChain == ctx.ChainID:
		return errors.New("cannot transfer NFT to same chain")
	}

	if err := t.BaseTx.SyntacticVerify(ctx, c, txFeeAssetID, txFee, txFee, len(t.Ins)); err != nil {
		return err
	}

	// Verify NFT transfer operation
	if err := t.NFTTransferOp.Verify(); err != nil {
		return err
	}

	return nil
}

func (t *NFTTransferTx) SemanticVerify(vm VM, tx UnsignedTx, creds []verify.Verifiable) error {
	// Verify the transaction is well-formed
	if err := t.BaseTx.SemanticVerify(vm, tx, creds); err != nil {
		return err
	}

	// TODO: Verify NFT ownership
	// TODO: Verify destination chain supports NFTs
	// TODO: Check if recipient address is valid for destination chain

	return nil
}

func (t *NFTTransferTx) Visit(v Visitor) error {
	return v.NFTTransferTx(t)
}
