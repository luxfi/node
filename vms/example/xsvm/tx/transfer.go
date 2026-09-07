// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/zap"
)

var _ Unsigned = (*Transfer)(nil)

type Transfer struct {
	// ChainID provides cross chain replay protection
	ChainID ids.ID `json:"chainID"`
	// Nonce provides internal chain replay protection
	Nonce   uint64      `json:"nonce"`
	MaxFee  uint64      `json:"maxFee"`
	AssetID ids.ID      `json:"assetID"`
	Amount  uint64      `json:"amount"`
	To      ids.ShortID `json:"to"`
}

func (t *Transfer) Visit(v Visitor) error {
	return v.Transfer(t)
}

// Marshal encodes the Transfer as one native ZAP object with the tx kind byte
// at offset 0.
func (t *Transfer) Marshal() ([]byte, error) {
	b := zap.NewBuilder(zap.HeaderSize + sizeTransfer)
	ob := b.StartObject(sizeTransfer)
	ob.SetUint8(offKind, uint8(kindTransfer))
	ob.SetBytesFixed(offTransferChainID, t.ChainID[:])
	ob.SetUint64(offTransferNonce, t.Nonce)
	ob.SetUint64(offTransferMaxFee, t.MaxFee)
	ob.SetBytesFixed(offTransferAssetID, t.AssetID[:])
	ob.SetUint64(offTransferAmount, t.Amount)
	ob.SetBytesFixed(offTransferTo, t.To[:])
	ob.FinishAsRoot()
	return b.Finish(), nil
}

func parseTransfer(obj zap.Object) *Transfer {
	t := &Transfer{
		Nonce:  obj.Uint64(offTransferNonce),
		MaxFee: obj.Uint64(offTransferMaxFee),
		Amount: obj.Uint64(offTransferAmount),
	}
	copy(t.ChainID[:], obj.BytesFixedSlice(offTransferChainID, idLen))
	copy(t.AssetID[:], obj.BytesFixedSlice(offTransferAssetID, idLen))
	copy(t.To[:], obj.BytesFixedSlice(offTransferTo, shortLen))
	return t
}
