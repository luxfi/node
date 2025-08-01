// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/vms/nftfx"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var (
	_ UnsignedTx             = (*NFTTransferTx)(nil)
	_ secp256k1fx.UnsignedTx = (*NFTTransferTx)(nil)

	ErrInvalidNFTID     = errors.New("invalid NFT ID")
	ErrInvalidRecipient = errors.New("invalid recipient")
	ErrUnsupportedChain = errors.New("unsupported destination chain for NFT")
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

// Initialize implements quasar.ContextInitializable
func (t *NFTTransferTx) Initialize(ctx *quasar.Context) error {
	t.InitCtx(ctx)
	return nil
}

func (t *NFTTransferTx) Visit(v Visitor) error {
	return v.NFTTransferTx(t)
}
