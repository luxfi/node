// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	
	"github.com/luxfi/node/v2/codec"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/vms/components/verify"
	"github.com/luxfi/node/v2/quasar"
)

var _ UnsignedTx = (*BurnTx)(nil)

// BurnTx burns assets on X-Chain for cross-chain transfers via Teleport Protocol
type BurnTx struct {
	// Base transaction fields
	BaseTx `serialize:"true"`

	// Asset being burned
	AssetID ids.ID `serialize:"true" json:"assetID"`

	// Amount to burn
	Amount uint64 `serialize:"true" json:"amount"`

	// Destination chain ID
	DestChain ids.ID `serialize:"true" json:"destChain"`

	// Destination address (chain-specific format)
	DestAddress []byte `serialize:"true" json:"destAddress"`

	// Optional metadata for the transfer
	TeleportData []byte `serialize:"true" json:"teleportData"`
}

func (t *BurnTx) InitCtx(ctx *quasar.Context) {
	t.BaseTx.InitCtx(ctx)
}

func (t *BurnTx) SyntacticVerify(
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
	case t.AssetID == ids.Empty:
		return ErrInvalidAssetID
	case t.Amount == 0:
		return ErrInvalidAmount
	case t.DestChain == ids.Empty:
		return ErrInvalidDestChain
	case len(t.DestAddress) == 0:
		return ErrInvalidDestAddress
	case t.DestChain == ctx.ChainID:
		return errors.New("cannot burn to same chain")
	}

	if err := t.BaseTx.SyntacticVerify(ctx, c, txFeeAssetID, txFee, txFee, len(t.Ins)); err != nil {
		return err
	}

	// Ensure we're burning the correct amount
	totalIn := uint64(0)
	for _, in := range t.Ins {
		if in.AssetID() != t.AssetID {
			return errors.New("input asset mismatch")
		}
		totalIn += in.Input().Amount()
	}

	// Must burn exact amount (no change outputs for burn asset)
	if totalIn != t.Amount {
		return errors.New("burn amount mismatch")
	}

	return nil
}

func (t *BurnTx) SemanticVerify(vm VM, tx UnsignedTx, creds []verify.Verifiable) error {
	// Verify the transaction is well-formed
	if err := t.BaseTx.SemanticVerify(vm, tx, creds); err != nil {
		return err
	}

	// Additional semantic checks can be added here
	// For example, checking if the destination chain is supported

	return nil
}

func (t *BurnTx) Visit(v Visitor) error {
	return v.BurnTx(t)
}
