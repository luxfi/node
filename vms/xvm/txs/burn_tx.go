// (c) 2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar"
	"github.com/luxfi/node/vms/secp256k1fx"
)

var (
	_ UnsignedTx             = (*BurnTx)(nil)
	_ secp256k1fx.UnsignedTx = (*BurnTx)(nil)

	ErrInvalidDestChain   = errors.New("invalid destination chain")
	ErrInvalidDestAddress = errors.New("invalid destination address")
	ErrInvalidAmount      = errors.New("invalid burn amount")
)

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

func (t *BurnTx) Visit(v Visitor) error {
	return v.BurnTx(t)
}
