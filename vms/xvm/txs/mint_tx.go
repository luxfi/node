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
	_ UnsignedTx             = (*MintTx)(nil)
	_ secp256k1fx.UnsignedTx = (*MintTx)(nil)

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

func (t *MintTx) InitCtx(ctx *quasar.Context) {
	t.BaseTx.InitCtx(ctx)
}

// Initialize implements quasar.ContextInitializable
func (t *MintTx) Initialize(ctx *quasar.Context) error {
	t.InitCtx(ctx)
	return nil
}

func (t *MintTx) Visit(v Visitor) error {
	return v.MintTx(t)
}
